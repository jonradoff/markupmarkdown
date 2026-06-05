package api

import (
	"context"
	"crypto/sha1" //nolint:gosec // SHA-1 matches Git's blob hash; not used for security
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/models"
)

// gitBlobSHA returns the SHA-1 hash Git would assign to `content` if
// stored as a blob. Format: sha1("blob <size>\0<content>"). We use
// this at SHA-backfill time so legacy docs (ingested before we
// tracked source_sha) can still notice upstream drift — by comparing
// the hash of the stored content to the current GitHub SHA, we know
// whether the original ingest equals the live upstream file even
// though we never stamped a baseline at ingest.
func gitBlobSHA(content string) string {
	h := sha1.New() //nolint:gosec // git-compatible blob hash, not security
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// docByIDFilter — small helper to keep backfill writes inline without
// inventing a new store method.
func docByIDFilter(id string) bson.M { return bson.M{"_id": id} }

// sourceCheckTTL is how long we trust a previous drift check before
// refreshing on doc-open. Short enough that an upstream push is
// noticed within a minute of the next open, long enough that opening
// the same doc in five tabs doesn't fire five GitHub API calls.
const sourceCheckTTL = 60 * time.Second

// sourceCheckInFlight dedupes concurrent drift checks for the same doc
// — multiple tabs opening the same doc shouldn't fire N parallel
// GitHub API calls.
var sourceCheckInFlight sync.Map // map[string]chan struct{}

// maybeRefreshSourceDrift triggers a background drift check on the
// ROOT document of the revision chain. Child revisions inherit their
// drift state from the root — checking the child directly would
// compare AI-revised content against the upstream file (different by
// design) and surface false negatives.
func (a *API) maybeRefreshSourceDrift(doc *models.Document, userToken string) {
	if doc == nil {
		return
	}
	root := a.rootForDrift(doc)
	if root == nil {
		return
	}
	if _, _, _, _, ok := deriveGitHubInfo(root); !ok {
		return
	}
	if root.SourceCheckedAt != nil && time.Since(*root.SourceCheckedAt) < sourceCheckTTL {
		return
	}
	a.runSourceCheck(root, userToken, false)
}

// rootForDrift returns the root document of the revision chain for
// drift-tracking purposes. Returns the passed doc when it has no
// parent, otherwise walks to root.
func (a *API) rootForDrift(doc *models.Document) *models.Document {
	if doc == nil {
		return nil
	}
	if doc.ParentID == "" {
		return doc
	}
	ctx, cancel := context.WithTimeout(contextDetached(), 5*time.Second)
	defer cancel()
	root, err := a.store.RootDocument(ctx, doc.ID)
	if err != nil || root == nil {
		return doc
	}
	return root
}

// runSourceCheck does the actual SHA fetch + persist + broadcast. The
// `force` flag bypasses the in-flight dedupe map so a manual "check
// now" doesn't get swallowed by a concurrent passive check.
func (a *API) runSourceCheck(doc *models.Document, userToken string, force bool) {
	owner, repo, ref, p, ok := deriveGitHubInfo(doc)
	if !ok {
		return
	}
	docID := doc.ID
	hadBaseline := doc.SourceSHA != ""
	if !force {
		if _, loaded := sourceCheckInFlight.LoadOrStore(docID, struct{}{}); loaded {
			return
		}
	}
	go func() {
		if !force {
			defer sourceCheckInFlight.Delete(docID)
		}
		ctx, cancel := context.WithTimeout(contextDetached(), 15*time.Second)
		defer cancel()
		sha, err := auth.FetchGitHubFileSHA(ctx, userToken, owner, repo, ref, p)
		if err != nil && userToken != "" {
			// Token may have lost access; retry anonymously in case the
			// file is public.
			sha, err = auth.FetchGitHubFileSHA(ctx, "", owner, repo, ref, p)
		}
		if err != nil {
			return
		}
		if !hadBaseline {
			// Legacy doc: stamp the git blob SHA of the STORED content
			// as the baseline. If GitHub's current SHA differs, drift
			// is real — set the drift fields so the banner appears.
			a.backfillBaseline(ctx, docID, doc.Content, sha)
			return
		}
		prevDrift := doc.SourceLatestSHA != ""
		if err := a.store.SetDocumentSourceCheck(ctx, docID, sha); err != nil {
			return
		}
		nowDrift := sha != doc.SourceSHA
		if nowDrift != prevDrift {
			a.hub.Broadcast(docID, "doc-updated")
		}
	}()
}

// backfillBaseline stamps a legacy doc with the git blob SHA of its
// stored content as the baseline. If that doesn't equal the live
// upstream SHA, also stamps the drift fields — so a file edited
// upstream BEFORE we ever tracked SHAs still surfaces the banner on
// the next check. Broadcasts doc-updated when drift is detected.
func (a *API) backfillBaseline(ctx context.Context, docID, content, upstreamSHA string) {
	baseline := gitBlobSHA(content)
	now := time.Now().UTC()
	set := bson.M{
		"source_sha":        baseline,
		"source_checked_at": now,
	}
	if upstreamSHA != "" && upstreamSHA != baseline {
		set["source_latest_sha"] = upstreamSHA
		set["source_drifted_at"] = now
	}
	_, _ = a.store.Documents().UpdateOne(ctx,
		docByIDFilter(docID),
		bson.M{"$set": set})
	if upstreamSHA != "" && upstreamSHA != baseline {
		a.hub.Broadcast(docID, "doc-updated")
	}
}

// checkSourceNow is the handler for POST /api/documents/:id/check-source.
// Runs the drift check on the ROOT of the revision chain (so child
// revisions inherit the original's drift state), busts access caches
// to re-verify GitHub access, and returns synchronously so the
// response carries the freshly-computed state.
func (a *API) checkSourceNow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	// Re-verify access against the doc the user is viewing (busting
	// caches first). This catches loss-of-access mid-session.
	if pre, _ := a.store.GetDocument(r.Context(), id); pre != nil {
		if owner, repo, ref, p, ok := deriveGitHubInfo(pre); ok {
			publicFetchCache.invalidate(owner, repo, ref, p)
			if u := a.currentUser(r); u != nil {
				repoAccessCache.invalidate(u.ID, owner, repo)
			}
		}
	}

	doc, accErr := a.checkDocAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}

	// Pick the doc whose drift state we actually care about: the
	// chain's root. Revision children intentionally diverge from
	// upstream — checking them directly is meaningless.
	target := a.rootForDrift(doc)
	if target == nil {
		target = doc
	}
	owner, repo, ref, p, ok := deriveGitHubInfo(target)
	if !ok {
		writeJSON(w, http.StatusOK, sourceCheckResponse(doc, target, false))
		return
	}
	token := ""
	if u := a.currentUser(r); u != nil {
		token = u.AccessToken
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	sha, err := auth.FetchGitHubFileSHA(ctx, token, owner, repo, ref, p)
	if err != nil && token != "" {
		sha, err = auth.FetchGitHubFileSHA(ctx, "", owner, repo, ref, p)
	}
	if err != nil {
		writeJSON(w, http.StatusOK, sourceCheckResponse(doc, target, true))
		return
	}

	if target.SourceSHA == "" {
		a.backfillBaseline(ctx, target.ID, target.Content, sha)
	} else {
		prevDrift := target.SourceLatestSHA != ""
		if err := a.store.SetDocumentSourceCheck(ctx, target.ID, sha); err != nil {
			internalError(w, "store.set_source_check", err)
			return
		}
		nowDrift := sha != target.SourceSHA
		if nowDrift != prevDrift {
			a.hub.Broadcast(target.ID, "doc-updated")
			if target.ID != doc.ID {
				a.hub.Broadcast(doc.ID, "doc-updated")
			}
		}
	}

	updated, _ := a.store.GetDocument(ctx, target.ID)
	if updated == nil {
		updated = target
	}
	writeJSON(w, http.StatusOK, sourceCheckResponse(doc, updated, false))
}

// sourceCheckResponse builds the JSON the frontend receives. The
// drift fields ALWAYS come from the root (target) so a child revision
// gets the same banner the root would. rootDocument lets the frontend
// link an "Open original" action when the current doc isn't itself
// the root.
func sourceCheckResponse(current, target *models.Document, failed bool) map[string]any {
	out := map[string]any{
		"sourceSha":       target.SourceSHA,
		"sourceLatestSha": target.SourceLatestSHA,
		"sourceDriftedAt": target.SourceDriftedAt,
	}
	if failed {
		out["checkFailed"] = true
	}
	if current.ID != target.ID {
		out["rootDocument"] = map[string]string{
			"id":    target.ID,
			"title": target.Title,
		}
	}
	return out
}

// reanchor maps each comment's anchor.exact into the new content.
// Outcome:
//   - clean:  exact text still appears somewhere in new source → keep
//             the comment as anchored, defer to the frontend's
//             textContent fallback (start=end=0) to highlight the new
//             rendered position. We don't try to compute new
//             textContent offsets server-side; the markdown source
//             coordinate space is not the same as the rendered
//             textContent space.
//   - orphan: exact text doesn't appear at all → flip Orphan=true and
//             stash OriginalExact for the orphan card.
//
// Doc-level comments (Anchor.Exact == "") are left untouched.
//
// Already-orphan comments are reconsidered too: if the user re-edited
// the source to bring the quote back, the comment un-orphans.
func reanchorComments(comments []models.Comment, newContent string) []reanchorResult {
	out := make([]reanchorResult, len(comments))
	for i := range comments {
		c := &comments[i]
		if isDocLevel(c.Anchor) {
			out[i] = reanchorResult{ID: c.ID, Status: reanchorDocLevel}
			continue
		}
		exact := c.Anchor.Exact
		if c.Orphan && c.OriginalExact != "" {
			exact = c.OriginalExact
		}
		if exact == "" {
			out[i] = reanchorResult{ID: c.ID, Status: reanchorOrphan}
			continue
		}
		if strings.Contains(newContent, exact) {
			out[i] = reanchorResult{ID: c.ID, Status: reanchorClean, Exact: exact}
			continue
		}
		out[i] = reanchorResult{
			ID:            c.ID,
			Status:        reanchorOrphan,
			OriginalExact: pickOriginalExact(c, exact),
		}
	}
	return out
}

func pickOriginalExact(c *models.Comment, exact string) string {
	if c.OriginalExact != "" {
		return c.OriginalExact
	}
	return exact
}

type reanchorStatus int

const (
	reanchorClean reanchorStatus = iota
	reanchorOrphan
	reanchorDocLevel
)

type reanchorResult struct {
	ID            string
	Status        reanchorStatus
	Exact         string
	OriginalExact string
}

// isDocLevel returns true if the anchor represents a document-level
// comment (no inline highlight). Convention: Start==End==0 and Exact=="".
func isDocLevel(a models.Anchor) bool {
	return a.Start == 0 && a.End == 0 && strings.TrimSpace(a.Exact) == ""
}

// syncDocumentSource implements POST /api/documents/:id/sync. Re-fetches
// the source from GitHub, re-anchors every comment, and persists.
func (a *API) syncDocumentSource(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	// Pulling the latest revision changes the doc content — gate it the
	// same way we gate rename / accept-revision.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	owner, repo, ref, p, ok := deriveGitHubInfo(doc)
	if !ok {
		writeError(w, http.StatusBadRequest, "this document isn't sourced from GitHub")
		return
	}

	token := ""
	if u := a.currentUser(r); u != nil && doc.Private {
		token = u.AccessToken
	}
	meta, err := auth.FetchGitHubFileMeta(r.Context(), token, owner, repo, ref, p)
	if err != nil {
		a.writeFetchError(w, r, doc.SourceURL, err)
		return
	}
	if meta.SHA == "" {
		writeError(w, http.StatusBadGateway, "GitHub returned no SHA for this file")
		return
	}

	// Re-anchor every comment, then persist new content + status.
	comments, err := a.store.ListComments(r.Context(), id)
	if err != nil {
		internalError(w, "store.list_comments", err)
		return
	}
	results := reanchorComments(comments, meta.Content)
	cleanCount := 0
	orphanCount := 0
	for i, res := range results {
		c := &comments[i]
		switch res.Status {
		case reanchorClean:
			// Zero out positions so the frontend's textContent
			// fallback re-locates the highlight against the freshly
			// rendered DOM. The exact string is the source of truth.
			set := bson.M{
				"anchor.start": 0,
				"anchor.end":   0,
				"anchor.exact": res.Exact,
				"updated_at":   time.Now().UTC(),
			}
			update := bson.M{"$set": set}
			if c.Orphan {
				update["$unset"] = bson.M{
					"orphan":         "",
					"original_exact": "",
				}
			}
			if _, err := a.store.Comments().UpdateOne(r.Context(),
				bson.M{"_id": c.ID}, update); err != nil {
				internalError(w, "store.update_anchor", err)
				return
			}
			cleanCount++
		case reanchorOrphan:
			if c.Orphan {
				// Already orphan — leave as-is.
				orphanCount++
				continue
			}
			update := bson.M{
				"orphan":         true,
				"original_exact": res.OriginalExact,
				"updated_at":     time.Now().UTC(),
			}
			if _, err := a.store.Comments().UpdateOne(r.Context(),
				bson.M{"_id": c.ID},
				bson.M{"$set": update}); err != nil {
				internalError(w, "store.mark_orphan", err)
				return
			}
			orphanCount++
		case reanchorDocLevel:
			// nothing to do
		}
	}

	if err := a.store.ReplaceDocumentSource(r.Context(), id, meta.Content, meta.SHA); err != nil {
		internalError(w, "store.replace_source", err)
		return
	}

	a.hub.Broadcast(id, "doc-updated")
	a.hub.Broadcast(id, "comments-updated")

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          id,
		"sourceSha":   meta.SHA,
		"cleanCount":  cleanCount,
		"orphanCount": orphanCount,
	})
}

// patchCommentAnchorRequest is the body for the manual re-anchor flow.
// Either supply {start, end, exact} (inline anchor) or {docLevel: true}
// to pin the comment as document-level.
type patchCommentAnchorRequest struct {
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Exact    string `json:"exact"`
	Prefix   string `json:"prefix,omitempty"`
	Suffix   string `json:"suffix,omitempty"`
	DocLevel bool   `json:"docLevel,omitempty"`
}

// patchCommentAnchor implements PATCH /api/comments/:id/anchor. Used to
// re-anchor an orphan comment manually, or to convert any comment to
// a doc-level pin.
func (a *API) patchCommentAnchor(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	existing, doc, accErr := a.checkCommentAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if !a.requireMineComment(w, r, existing) {
		return
	}
	capBody(w, r, maxBodyComment)

	var req patchCommentAnchorRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	set := bson.M{"updated_at": time.Now().UTC()}
	unset := bson.M{}

	if req.DocLevel {
		set["anchor.start"] = 0
		set["anchor.end"] = 0
		set["anchor.exact"] = ""
		set["anchor.prefix"] = ""
		set["anchor.suffix"] = ""
		// Doc-level comments aren't "orphan" — they're intentional pins.
		unset["orphan"] = ""
		unset["original_exact"] = ""
	} else {
		exact := req.Exact
		if err := validateManualAnchor(req, doc.Content); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		set["anchor.start"] = req.Start
		set["anchor.end"] = req.End
		set["anchor.exact"] = exact
		set["anchor.prefix"] = req.Prefix
		set["anchor.suffix"] = req.Suffix
		unset["orphan"] = ""
		unset["original_exact"] = ""
	}

	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	if _, err := a.store.Comments().UpdateOne(r.Context(), bson.M{"_id": id}, update); err != nil {
		internalError(w, "store.update_anchor", err)
		return
	}
	c, err := a.store.GetComment(r.Context(), id)
	if err != nil {
		internalError(w, "store.get_comment", err)
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	a.decorate(r, c)
	writeJSON(w, http.StatusOK, c)
}

// validateManualAnchor checks the fields supplied by the manual
// re-anchor flow. start/end are textContent positions from the
// frontend's getSelectionAnchor and so cannot be indexed directly into
// the markdown source content — we only require that exact appears
// somewhere in the source as a sanity check.
func validateManualAnchor(req patchCommentAnchorRequest, content string) error {
	if req.Start < 0 || req.End <= req.Start {
		return errors.New("invalid anchor range")
	}
	if strings.TrimSpace(req.Exact) == "" {
		return errors.New("anchor.exact is required")
	}
	if len(req.Exact) > maxAnchorExactLen {
		return errors.New("anchor.exact too long")
	}
	if !strings.Contains(content, req.Exact) {
		return errors.New("anchor.exact not found in document")
	}
	return nil
}
