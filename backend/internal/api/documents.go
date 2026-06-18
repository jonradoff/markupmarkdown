package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/safefetch"
)

type createDocumentRequest struct {
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
}

type patchDocumentRequest struct {
	Title *string `json:"title,omitempty"`
}

// listDocuments returns only documents the signed-in user has worked on:
// docs they created, AI-revised, or commented/replied on. Private docs the
// user has lost GitHub access to are filtered out. Unauthenticated callers
// get 401 — the frontend shows a "sign in to see your files" message.
//
// Results are deduplicated by revision lineage: if the user has touched
// any doc in a revision chain (root, intermediate revision, or leaf), the
// list returns ONE entry — the chain's leaf — with revisionCount counting
// every node in the chain. This keeps the list scannable when AI
// revisions multiply the row count of a single underlying document.
func (a *API) listDocuments(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, fetchErrorResponse{
			Error: "Sign in with GitHub to see your recent documents.",
			Kind:  "sign_in_required",
		})
		return
	}

	touched, err := a.store.ListDocumentsForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if touched == nil {
		touched = []models.Document{}
	}
	// Drop docs the user has Forgotten — checked against the leaf the
	// listing would show, not the root, so forgetting a chain hides
	// the chain regardless of which node we walk to.
	hidden, _ := a.store.HiddenItemIDs(r.Context(), user.ID, "doc")

	// Group every touched doc by its chain root. We then keep ONE
	// entry per root — the chain's leaf — so users see "the current
	// state" of every doc they've worked on, not every snapshot.
	seenRoot := map[string]bool{}
	out := make([]summary, 0, len(touched))
	for _, d := range touched {
		root := &d
		if d.ParentID != "" {
			if r, err := a.store.RootDocument(r.Context(), d.ID); err == nil && r != nil {
				root = r
			}
		}
		if seenRoot[root.ID] {
			continue
		}
		seenRoot[root.ID] = true
		// Forget is keyed on the chain root — forgetting any node in
		// the chain hides the whole chain so a later revision doesn't
		// re-surface the entry.
		if hidden[root.ID] {
			continue
		}

		// Walk to leaf so the entry surfaces the current state. If the
		// "root" itself is soft-deleted (Jon's case: deleted the
		// original after AI-revising), substitute the deepest alive
		// descendant so we never list a deleted leaf that 404s on
		// click.
		leaf := root
		descendant, _ := a.store.LatestDescendant(r.Context(), root.ID)
		if descendant != nil {
			leaf = descendant
		} else if leaf.DeletedAt != nil {
			// No alive descendants AND root is deleted → drop the
			// entire chain from the list. The user's gesture (delete
			// every revision) wants this gone.
			continue
		}

		// Access check on the leaf (private docs the viewer lost
		// GitHub access to drop out of the list entirely).
		if leaf.Private {
			ok, err := repoAccessCache.check(r.Context(), user.ID, user.AccessToken, leaf.GitHubOwner, leaf.GitHubRepo)
			if err != nil || !ok {
				continue
			}
		}

		// Chain depth: root → ... → leaf. Cheap walk via ListChildren.
		revisionCount := chainDepth(r.Context(), a.store, root.ID, leaf.ID)

		rootID := ""
		if leaf.ID != root.ID {
			rootID = root.ID
		}
		out = append(out, summary{
			ID:            leaf.ID,
			Title:         leaf.Title,
			SourceURL:     leaf.SourceURL,
			Origin:        leaf.Origin,
			Private:       leaf.Private,
			GitHubOwner:   leaf.GitHubOwner,
			GitHubRepo:    leaf.GitHubRepo,
			CreatedAt:     leaf.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:     leaf.UpdatedAt.UTC().Format(time.RFC3339),
			RevisionCount: revisionCount,
			RootID:        rootID,
		})
	}

	// Source-URL dedup at the display layer. Independently-ingested
	// chains pointing at the same GitHub blob (owner+repo+path)
	// collapse into the most-recently-updated entry; the others get
	// attached as OlderVersions so the UI can render an "N older
	// copies" affordance instead of cluttering Recents with parallel
	// rows for the same file. Uploads (no GitHub key) keep their own
	// rows.
	writeJSON(w, http.StatusOK, dedupBySource(out))
}

// summary is the per-row payload of GET /api/documents. Lifted to
// package level so we can type the dedup helper without leaning on
// gnarly anonymous-struct gymnastics.
type summary struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	SourceURL     string         `json:"sourceUrl,omitempty"`
	Origin        string         `json:"origin"`
	Private       bool           `json:"private,omitempty"`
	GitHubOwner   string         `json:"githubOwner,omitempty"`
	GitHubRepo    string         `json:"githubRepo,omitempty"`
	CreatedAt     string         `json:"createdAt"`
	UpdatedAt     string         `json:"updatedAt"`
	RevisionCount int            `json:"revisionCount,omitempty"`
	RootID        string         `json:"rootId,omitempty"`
	OlderVersions []olderVersion `json:"olderVersions,omitempty"`
}

type olderVersion struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	UpdatedAt     string `json:"updatedAt"`
	RevisionCount int    `json:"revisionCount,omitempty"`
}

// dedupBySource groups summaries by (githubOwner, githubRepo,
// githubPath) — ref intentionally omitted because /blob/main/X.md and
// /blob/master/X.md after a rename are the same logical file. The
// most-recently-updated entry in each group stays; the rest are
// folded into its OlderVersions. Uploads and non-github entries pass
// through untouched. Input order is preserved (newest-touched first).
func dedupBySource(rows []summary) []summary {
	// Two passes so we can preserve the first-seen ordering of kept
	// entries while still collapsing later siblings into them.
	keyOf := func(r summary) string {
		if r.GitHubOwner == "" || r.GitHubRepo == "" {
			return "" // upload / non-github — never deduped
		}
		// We don't store GitHubPath on the summary today; recover it
		// from the source URL. Keys are case-insensitive to match
		// GitHub's own case handling.
		_, _, _, path, ok := parseGitHubBlobURL(r.SourceURL)
		if !ok {
			return ""
		}
		return strings.ToLower(r.GitHubOwner) + "/" +
			strings.ToLower(r.GitHubRepo) + "/" +
			strings.ToLower(path)
	}
	type group struct {
		idx     int      // position in `out` where the kept entry sits
		kept    *summary // pointer to the surviving row
		members []int    // indexes into rows of every doc in this group
	}
	groups := make(map[string]*group)
	out := make([]summary, 0, len(rows))
	for i := range rows {
		k := keyOf(rows[i])
		if k == "" {
			out = append(out, rows[i])
			continue
		}
		if g, ok := groups[k]; ok {
			g.members = append(g.members, i)
			continue
		}
		out = append(out, rows[i])
		g := &group{idx: len(out) - 1, kept: &out[len(out)-1], members: []int{i}}
		groups[k] = g
	}
	// Now walk each group, choose the most-recently-updated as the
	// canonical kept entry, and fold the rest into OlderVersions.
	for _, g := range groups {
		if len(g.members) < 2 {
			continue
		}
		// Pick the member with the latest UpdatedAt as the kept row.
		bestIdx := g.members[0]
		for _, m := range g.members[1:] {
			if rows[m].UpdatedAt > rows[bestIdx].UpdatedAt {
				bestIdx = m
			}
		}
		// Overwrite the kept slot with the chosen row.
		*g.kept = rows[bestIdx]
		// Attach every other member as an older-version entry, newest
		// older first.
		older := make([]olderVersion, 0, len(g.members)-1)
		for _, m := range g.members {
			if m == bestIdx {
				continue
			}
			older = append(older, olderVersion{
				ID:            rows[m].ID,
				Title:         rows[m].Title,
				UpdatedAt:     rows[m].UpdatedAt,
				RevisionCount: rows[m].RevisionCount,
			})
		}
		sort.Slice(older, func(a, b int) bool { return older[a].UpdatedAt > older[b].UpdatedAt })
		g.kept.OlderVersions = older
	}
	return out
}

// chainDepth walks from rootID via the most-recently-created child
// edge to leafID and returns the number of nodes traversed (>=1).
// Safe against cycles; returns 1 if it can't see past the root.
func chainDepth(ctx context.Context, s docStore, rootID, leafID string) int {
	if rootID == leafID {
		return 1
	}
	depth := 1
	current := rootID
	seen := map[string]bool{rootID: true}
	for depth < 64 { // 64 == many more AI revisions than anyone would do
		children, err := s.ListChildren(ctx, current)
		if err != nil || len(children) == 0 {
			break
		}
		next := children[0]
		for i := 1; i < len(children); i++ {
			if children[i].CreatedAt.After(next.CreatedAt) {
				next = children[i]
			}
		}
		if seen[next.ID] {
			break
		}
		seen[next.ID] = true
		depth++
		current = next.ID
		if current == leafID {
			break
		}
	}
	return depth
}

// docStore is the minimal store surface chainDepth needs. Lets us
// test the helper with a stub.
type docStore interface {
	ListChildren(ctx context.Context, parentID string) ([]models.Document, error)
}

// resolveBySource implements GET /api/documents/by-source. Given a
// (owner, repo, ref, path) tuple, returns the existing chain leaf
// doc if one exists, or 404 if not. Used by the human-readable URL
// resolver to land on the same doc when two viewers paste the same
// github blob URL — comments aggregate on one doc rather than
// fracturing across N parallel clones.
func (a *API) resolveBySource(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	owner := strings.TrimSpace(q.Get("owner"))
	repo := strings.TrimSpace(q.Get("repo"))
	ref := strings.TrimSpace(q.Get("ref"))
	path := strings.TrimSpace(q.Get("path"))
	if owner == "" || repo == "" || path == "" {
		writeError(w, http.StatusBadRequest, "owner, repo, and path are required")
		return
	}
	root, err := a.store.FindLatestDocumentBySource(r.Context(), owner, repo, ref, path)
	if err != nil {
		internalError(w, "store.find_doc_by_source", err)
		return
	}
	if root == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "no document matches that source",
			"kind":  "not_found",
		})
		return
	}
	// Walk to leaf so the URL lands on the current state, not an
	// older revision.
	leaf := root
	if d, _ := a.store.LatestDescendant(r.Context(), root.ID); d != nil {
		leaf = d
	}
	// Access check on the leaf — private repo viewers without GitHub
	// access shouldn't even learn the doc exists.
	if leaf.Private {
		user := a.currentUser(r)
		if user == nil {
			writeError(w, http.StatusUnauthorized, "sign in required for this private document")
			return
		}
		ok, _ := repoAccessCache.check(r.Context(), user.ID, user.AccessToken, leaf.GitHubOwner, leaf.GitHubRepo)
		if !ok {
			writeError(w, http.StatusForbidden, "you don't have access to this document's source repo")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":    leaf.ID,
		"title": leaf.Title,
	})
}

func (a *API) createDocument(w http.ResponseWriter, r *http.Request) {
	// Read-only tokens cannot mint new documents (and thus cannot burn the
	// owner's URL-ingest rate budget). Cookie sessions always satisfy this.
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if !a.enforceRate(w, r, a.rlCreateDoc, "Too many documents created in a short window. Try again in a minute.") {
		return
	}
	capBody(w, r, maxBodyDocument)

	var req createDocumentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if len(req.Title) > maxTitleLen {
		writeError(w, http.StatusBadRequest, "title too long")
		return
	}
	if len(req.Content) > maxUploadContent {
		writeError(w, http.StatusBadRequest, "content too large (max 5 MB)")
		return
	}
	if req.URL != "" {
		// Strip sentence-terminator punctuation the user may have caught
		// when copy-pasting a URL out of an email or chat. Without this,
		// we end up storing the period as part of the source URL (and
		// the title), which renders as a broken hyperlink on /d/:id.
		req.URL = trimURLPunctuation(req.URL)
		if _, err := safefetch.ValidateURL(req.URL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// If the user pasted a markupmarkdown doc URL into the URL field,
		// just redirect them to that doc instead of cloning the SPA HTML.
		// We surface this as a structured response the frontend can act
		// on (navigate to /d/:id) rather than as a 201.
		if id := a.selfDocPath(req.URL); id != "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"redirect":   "/d/" + id,
				"kind":       "self_doc_redirect",
				"documentId": id,
			})
			return
		}
		// Source-URL dedup: if this exact GitHub blob URL has already
		// been ingested, redirect to the existing chain leaf instead
		// of minting a parallel clone. Two viewers pasting the same
		// URL now land on the SAME doc so comments aggregate. Same
		// dedup the human-URL resolver already does via /by-source.
		if owner, repo, ref, p, ok := parseGitHubBlobURL(req.URL); ok {
			if root, _ := a.store.FindLatestDocumentBySource(r.Context(), owner, repo, ref, p); root != nil {
				leaf := root
				if d, _ := a.store.LatestDescendant(r.Context(), root.ID); d != nil {
					leaf = d
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"redirect":   "/d/" + leaf.ID,
					"kind":       "self_doc_redirect",
					"documentId": leaf.ID,
				})
				return
			}
		}
	}

	doc := &models.Document{
		ID:        uuid.NewString(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if u := a.currentUser(r); u != nil {
		doc.CreatedByID = u.ID
	}

	if req.URL != "" {
		fetched, err := a.fetchContent(r.Context(), r, req.URL)
		if err != nil {
			a.writeFetchError(w, r, req.URL, err)
			return
		}
		// Reject obvious non-markdown content (HTML pages, JS bundles,
		// SVG) before we save anything. This is a friendlier failure
		// than letting users open a "Google homepage" doc.
		if !looksLikeMarkdown(fetched.Content, req.URL) {
			writeJSON(w, http.StatusBadRequest, fetchErrorResponse{
				Error:  "That URL doesn't look like a markdown file. markupmarkdown is for commenting on .md documents — not for editing arbitrary web pages.",
				Kind:   "not_markdown",
				Detail: "We fetched the URL but the content appears to be HTML, JavaScript, or another non-markdown format. Try a raw .md URL (e.g. GitHub raw or a docs site that serves Markdown).",
			})
			return
		}
		doc.Content = fetched.Content
		doc.SourceURL = req.URL
		doc.Origin = "url"
		doc.Private = fetched.Private
		doc.GitHubOwner = fetched.Owner
		doc.GitHubRepo = fetched.Repo
		doc.GitHubRef = fetched.Ref
		doc.GitHubPath = fetched.Path
		if fetched.SHA != "" {
			doc.SourceSHA = fetched.SHA
			now := time.Now().UTC()
			doc.SourceCheckedAt = &now
		}
		doc.Title = req.Title
		if doc.Title == "" {
			doc.Title = titleFromURL(req.URL)
		}
	} else if req.Content != "" {
		doc.Content = req.Content
		doc.Origin = "upload"
		doc.Title = req.Title
		if doc.Title == "" {
			doc.Title = "Untitled"
		}
	} else {
		writeError(w, http.StatusBadRequest, "either url or content is required")
		return
	}

	if err := a.store.InsertDocument(r.Context(), doc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

type documentResponse struct {
	*models.Document
	Parent             *parentSummary    `json:"parent,omitempty"`
	Children           []revisionSummary `json:"children,omitempty"`
	LatestDescendant   *parentSummary    `json:"latestDescendant,omitempty"`
	// RootDocument points at the root of the revision chain. Set only
	// when the current doc is itself a child revision — frontends use
	// it to render an "Open original" affordance on the source-drift
	// banner (you sync the root, not the AI-revised child).
	RootDocument *parentSummary `json:"rootDocument,omitempty"`
	// RevisionIndex is the 1-based structural position of this doc in
	// its chain (root = v1). Counts soft-deleted ancestors so the
	// numbering stays stable as docs come and go.
	RevisionIndex int `json:"revisionIndex,omitempty"`
	// RevisionTotal is the total number of nodes in the chain — root
	// plus every descendant via the most-recent-child walk. Used by
	// the toolbar to render "v3 of 5".
	RevisionTotal int `json:"revisionTotal,omitempty"`
	// PreviouslyViewedAt is the timestamp of the requester's *previous*
	// open of this doc, before this response. The frontend uses this to
	// mark any comment whose updatedAt is newer as "unread". RFC3339 UTC.
	PreviouslyViewedAt string `json:"previouslyViewedAt,omitempty"`
}

type parentSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// RevisionIndex is the parent's 1-based position in the
	// structural chain (root = v1). Surfaced so the doc-page
	// breadcrumb can say "← Revised from v1" instead of repeating
	// the same filename twice.
	RevisionIndex int `json:"revisionIndex,omitempty"`
}

type revisionSummary struct {
	ID            string               `json:"id"`
	Title         string               `json:"title"`
	CreatedAt     time.Time            `json:"createdAt"`
	RevisionMeta  *models.RevisionMeta `json:"revisionMeta,omitempty"`
	RevisionIndex int                  `json:"revisionIndex,omitempty"`
}

func (a *API) getDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	resp := documentResponse{Document: doc}
	// Kick off an async upstream drift check if our cached state is
	// stale. The current request returns the state we have right now;
	// the refresh result hits subsequent readers (and broadcasts on
	// change so any open viewer's banner appears without a reload).
	userToken := ""
	if u := a.currentUser(r); u != nil {
		userToken = u.AccessToken
	}
	a.maybeRefreshSourceDrift(doc, userToken)

	// For child revisions, mirror the ROOT's drift state into this
	// response's source_* fields so the banner shows on the child too
	// (driven by the original's upstream changes, not the child's
	// intentionally-divergent content). Also surface RootDocument so
	// the frontend can offer "Open original".
	if doc.ParentID != "" {
		if root, err := a.store.RootDocument(r.Context(), doc.ID); err == nil && root != nil && root.ID != doc.ID {
			resp.RootDocument = &parentSummary{ID: root.ID, Title: root.Title}
			rootCopy := *doc
			rootCopy.SourceSHA = root.SourceSHA
			rootCopy.SourceLatestSHA = root.SourceLatestSHA
			rootCopy.SourceDriftedAt = root.SourceDriftedAt
			rootCopy.SourceCheckedAt = root.SourceCheckedAt
			resp.Document = &rootCopy
		}
	}
	// Read prior view BEFORE bumping it, so the response reflects the
	// state the user is about to see (unread = new since last visit).
	if u := a.currentUser(r); u != nil {
		if prior, _ := a.store.GetDocumentView(r.Context(), doc.ID, u.ID); prior != nil {
			resp.PreviouslyViewedAt = prior.LastViewedAt.UTC().Format(time.RFC3339Nano)
		}
		a.enqueueView(doc.ID, u.ID)
	}
	// Build the structural ancestor chain so revisionIndex / parent's
	// revisionIndex are accurate. Walks via raw lookups so deleted
	// ancestors don't break the numbering — v3 is always v3 no matter
	// what's been deleted in the chain above it.
	ancestors, _ := a.store.AncestorChain(r.Context(), doc.ID)
	resp.RevisionIndex = len(ancestors) + 1
	if doc.ParentID != "" {
		if parent, _ := a.store.GetDocument(r.Context(), doc.ParentID); parent != nil {
			resp.Parent = &parentSummary{
				ID:            parent.ID,
				Title:         parent.Title,
				RevisionIndex: len(ancestors), // parent is at depth N-1
			}
		}
	}
	if children, _ := a.store.ListChildren(r.Context(), doc.ID); len(children) > 0 {
		for i, c := range children {
			resp.Children = append(resp.Children, revisionSummary{
				ID:           c.ID,
				Title:        c.Title,
				CreatedAt:    c.CreatedAt,
				RevisionMeta: c.RevisionMeta,
				// Children sit immediately below this doc, so the
				// 1-based vN is resp.RevisionIndex + 1 + ordinal.
				// (Branching is unusual; for linear chains this is
				// the simple sibling index.)
				RevisionIndex: resp.RevisionIndex + i + 1,
			})
		}
		if latest, _ := a.store.LatestDescendant(r.Context(), doc.ID); latest != nil && latest.ID != doc.ID {
			latestAncestors, _ := a.store.AncestorChain(r.Context(), latest.ID)
			resp.LatestDescendant = &parentSummary{
				ID:            latest.ID,
				Title:         latest.Title,
				RevisionIndex: len(latestAncestors) + 1,
			}
		}
	}
	// RevisionTotal: depth of the deepest descendant in the alive
	// chain (using the same walk listDocuments uses), so the toolbar
	// can render "v2 of 4" without re-walking on the client.
	resp.RevisionTotal = resp.RevisionIndex
	if resp.LatestDescendant != nil && resp.LatestDescendant.RevisionIndex > resp.RevisionTotal {
		resp.RevisionTotal = resp.LatestDescendant.RevisionIndex
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) patchDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	// Renaming a document is admin-only via token; cookie sessions can.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	var req patchDocumentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		if len(title) > maxTitleLen {
			writeError(w, http.StatusBadRequest, "title too long")
			return
		}
		if err := a.store.UpdateDocumentTitle(r.Context(), id, title); err != nil {
			internalError(w, "store.update_title", err)
			return
		}
	}
	doc, err := a.store.GetDocument(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (a *API) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	// Deleting a document is admin-only via token. A read or write token
	// should not be able to nuke documents.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	// Soft delete — the doc stays in the database for ~30 days so the
	// user can restore from Trash. PurgeExpiredDeletes (run periodically)
	// is what eventually removes it for real.
	if err := a.store.SoftDeleteDocument(r.Context(), id); err != nil {
		internalError(w, "store.soft_delete", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// forgetDocument is the per-user soft-hide of a doc from this user's
// "Your documents" list. Distinct from delete: the doc still exists
// for everyone else, the user just doesn't want to see it in their
// own recents. Forget is keyed on the chain root so revisions of a
// forgotten chain don't re-surface.
func (a *API) forgetDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	// We still want a real (or formerly-real) doc — guard against
	// arbitrary ID stuffing into the hidden_items collection.
	if _, accErr := a.checkDocAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	root, err := a.store.RootDocument(r.Context(), id)
	rootID := id
	if err == nil && root != nil {
		rootID = root.ID
	}
	if err := a.store.HideItem(r.Context(), user.ID, "doc", rootID); err != nil {
		internalError(w, "store.hide_doc", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listTrash(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, fetchErrorResponse{
			Error: "Sign in with GitHub to see your trash.",
			Kind:  "sign_in_required",
		})
		return
	}
	docs, err := a.store.ListTrashForUser(r.Context(), user.ID)
	if err != nil {
		internalError(w, "store.list_trash", err)
		return
	}
	if docs == nil {
		docs = []models.Document{}
	}
	type summary struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		DeletedAt string `json:"deletedAt"`
		// Days remaining before the purge sweep removes this doc.
		DaysLeft int `json:"daysLeft"`
	}
	const retentionDays = 30
	out := make([]summary, 0, len(docs))
	for _, d := range docs {
		// Skip private docs the requester has lost GitHub access to.
		if d.Private {
			ok, err := repoAccessCache.check(r.Context(), user.ID, user.AccessToken, d.GitHubOwner, d.GitHubRepo)
			if err != nil || !ok {
				continue
			}
		}
		if d.DeletedAt == nil {
			continue
		}
		daysSince := int(time.Since(*d.DeletedAt).Hours() / 24)
		daysLeft := retentionDays - daysSince
		if daysLeft < 0 {
			daysLeft = 0
		}
		out = append(out, summary{
			ID:        d.ID,
			Title:     d.Title,
			DeletedAt: d.DeletedAt.UTC().Format(time.RFC3339),
			DaysLeft:  daysLeft,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) restoreDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	// Restoring is the inverse of delete — same admin scope requirement.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	doc, err := a.store.GetDeletedDocument(r.Context(), id)
	if err != nil {
		internalError(w, "store.get_deleted", err)
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "not in trash")
		return
	}
	// Re-run access check on the would-be-restored doc.
	if doc.Private {
		ok, err := repoAccessCache.check(r.Context(), user.ID, user.AccessToken, doc.GitHubOwner, doc.GitHubRepo)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "you no longer have GitHub access to this doc's source")
			return
		}
	}
	if err := a.store.RestoreDocument(r.Context(), id); err != nil {
		internalError(w, "store.restore", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

type fetchErrorAction struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type fetchErrorResponse struct {
	Error   string             `json:"error"`
	Kind    string             `json:"kind,omitempty"`
	Detail  string             `json:"detail,omitempty"`
	Actions []fetchErrorAction `json:"actions,omitempty"`
}

// writeFetchError translates a fetch error into a structured response the
// frontend can render with clear next-step actions. GitHub access failures
// (private repo / org not granted) are the common case worth dressing up.
func (a *API) writeFetchError(w http.ResponseWriter, r *http.Request, srcURL string, err error) {
	var ghErr *auth.FetchError
	if !errors.As(err, &ghErr) {
		writeJSON(w, http.StatusBadRequest, fetchErrorResponse{
			Error: "Couldn't fetch this URL.",
			Kind:  "fetch_other",
			Detail: err.Error(),
		})
		return
	}

	owner, repo, _, _, isGitHub := parseGitHubBlobURL(srcURL)
	user := a.currentUser(r)
	resp := fetchErrorResponse{}

	switch {
	case ghErr.SSOURL != "":
		resp.Error = "Your GitHub org requires SAML SSO before this app can read its repos."
		resp.Kind = "github_sso"
		resp.Actions = append(resp.Actions, fetchErrorAction{
			Label: "Authorize SSO",
			URL:   ghErr.SSOURL,
		})

	case ghErr.StatusCode == http.StatusUnauthorized:
		resp.Error = "Your GitHub session expired. Sign in again to continue."
		resp.Kind = "github_auth"
		resp.Actions = append(resp.Actions, fetchErrorAction{
			Label: "Sign in with GitHub",
			URL:   "/api/auth/github/login?redirect=/",
		})

	case ghErr.StatusCode == http.StatusForbidden || ghErr.StatusCode == http.StatusNotFound:
		// GitHub returns 404 for "no access to a private repo" to avoid leaking
		// existence. If the user IS signed in, the most likely cause is the
		// org hasn't granted this app access yet.
		if user == nil {
			resp.Error = "This looks like a private repo. Sign in with GitHub to read it."
			resp.Kind = "github_auth"
			resp.Actions = append(resp.Actions, fetchErrorAction{
				Label: "Sign in with GitHub",
				URL:   "/api/auth/github/login?redirect=/",
			})
		} else {
			orgHint := ""
			if isGitHub {
				orgHint = fmt.Sprintf(" The repo's owner (`%s`) may need to approve the access request you sent — ask an admin of that organization to approve markupmarkdown under Org Settings → Third-party Access.", owner)
			}
			resp.Error = "GitHub returned no access for this file." + orgHint
			resp.Kind = "github_access"
			if a.cfg.GitHub.ClientID != "" {
				resp.Actions = append(resp.Actions, fetchErrorAction{
					Label: "Manage GitHub access",
					URL:   "https://github.com/settings/connections/applications/" + a.cfg.GitHub.ClientID,
				})
			}
			if isGitHub && repo != "" {
				resp.Actions = append(resp.Actions, fetchErrorAction{
					Label: fmt.Sprintf("Open %s/%s on GitHub", owner, repo),
					URL:   fmt.Sprintf("https://github.com/%s/%s", owner, repo),
				})
			}
		}

	default:
		resp.Error = fmt.Sprintf("GitHub returned an error (%d).", ghErr.StatusCode)
		resp.Kind = "github_other"
		resp.Detail = trimDetail(ghErr.Body)
	}

	writeJSON(w, http.StatusBadRequest, resp)
}

func trimDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// fetchContent fetches the markdown at rawURL. If the URL is a github.com/blob
// URL and the requester is authenticated, it uses the GitHub Contents API with
// their OAuth token (so private repos work). Otherwise it falls back to a plain
// HTTP fetch against the raw URL.
// fetchedDoc is what fetchContent returns, including metadata used to gate
// access to private GitHub-sourced documents on later reads.
type fetchedDoc struct {
	Content string
	Private bool
	Owner   string
	Repo    string
	Ref     string
	Path    string
	// SHA is the GitHub blob SHA when the source is a github.com URL. Empty
	// for non-GitHub sources. Stored on the document so later drift checks
	// can detect upstream changes.
	SHA string
}

func (a *API) fetchContent(ctx context.Context, r *http.Request, rawURL string) (*fetchedDoc, error) {
	owner, repo, ref, p, isGitHub := parseGitHubBlobURL(rawURL)
	if !isGitHub {
		// Gist landing pages return HTML; redirect to the /raw view so
		// we get the markdown body instead. Public gists are anonymous-
		// readable; secret gists are readable to anyone with the link
		// (also anonymous). So no auth handoff is needed for either.
		fetchURL := rawURL
		if raw, ok := normalizeGistURL(rawURL); ok {
			fetchURL = raw
		}
		c, err := a.fetchURL(ctx, fetchURL)
		if err != nil {
			return nil, err
		}
		return &fetchedDoc{Content: c}, nil
	}

	// Try the public raw URL first. If that works the file is public and
	// no access gating is needed for future readers.
	rawContent, rawErr := a.fetchURL(ctx, normalizeGitHubURL(rawURL))
	if rawErr == nil {
		// Public file — capture SHA via an anonymous Contents API call so
		// later drift checks have a baseline. We treat a failed SHA lookup
		// as non-fatal (rate limit, transient) and proceed without it.
		sha := ""
		if meta, err := auth.FetchGitHubFileMeta(ctx, "", owner, repo, ref, p); err == nil {
			sha = meta.SHA
		}
		return &fetchedDoc{
			Content: rawContent,
			Private: false,
			Owner:   owner, Repo: repo, Ref: ref, Path: p,
			SHA: sha,
		}, nil
	}
	code := statusCodeFromFetchErr(rawErr)
	if code < 400 || code >= 500 {
		return nil, rawErr
	}

	// Public fetch returned 4xx. If the user is signed in, try the
	// authenticated Contents API — success means the file was private and
	// the current user has access.
	user := a.currentUser(r)
	if user != nil && user.AccessToken != "" {
		meta, err := auth.FetchGitHubFileMeta(ctx, user.AccessToken, owner, repo, ref, p)
		if err != nil {
			return nil, err
		}
		return &fetchedDoc{
			Content: meta.Content,
			Private: true,
			Owner:   owner, Repo: repo, Ref: ref, Path: p,
			SHA: meta.SHA,
		}, nil
	}

	// Not signed in — wrap as FetchError so the friendly handler offers a
	// "Sign in with GitHub" action.
	return nil, &auth.FetchError{StatusCode: code, Body: rawErr.Error()}
}

// statusCodeFromFetchErr extracts an HTTP status code from an error of the
// form "http %d" returned by fetchURL. Returns 0 if not a status-coded error.
func statusCodeFromFetchErr(err error) int {
	var code int
	if _, scanErr := fmt.Sscanf(err.Error(), "http %d", &code); scanErr != nil {
		return 0
	}
	return code
}

func parseGitHubBlobURL(raw string) (owner, repo, ref, path string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" {
		return
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" {
		return
	}
	return parts[0], parts[1], parts[3], strings.Join(parts[4:], "/"), true
}

func (a *API) fetchURL(ctx context.Context, rawURL string) (string, error) {
	if _, err := safefetch.ValidateURL(rawURL); err != nil {
		return "", err
	}

	client := safefetch.Client(a.cfg.Fetch.ParseTimeout())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "markupmarkdown/0.1")
	req.Header.Set("Accept", "text/markdown, text/plain, text/*;q=0.8, */*;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, a.cfg.Fetch.MaxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if int64(len(body)) > a.cfg.Fetch.MaxBytes {
		return "", fmt.Errorf("file exceeds max size (%d bytes)", a.cfg.Fetch.MaxBytes)
	}
	return string(body), nil
}

// normalizeGistURL rewrites a gist landing-page URL into the URL
// that returns the raw file content. Returns (rewritten, true) when
// the input is a gist, otherwise ("", false) so callers can keep the
// original URL untouched.
//
// Supported input shapes (single-file gists — multi-file isn't
// disambiguable from the URL alone, but appending /raw still returns
// the first file which is what GitHub's own gist viewer does):
//   - https://gist.github.com/{owner}/{gist_id}
//   - https://gist.github.com/{owner}/{gist_id}/raw  (passthrough)
//   - https://gist.githubusercontent.com/...         (already raw)
func normalizeGistURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if host == "gist.githubusercontent.com" {
		return raw, true // already a raw URL — no rewrite needed
	}
	if host != "gist.github.com" {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	owner, gistID := parts[0], parts[1]
	if owner == "" || gistID == "" {
		return "", false
	}
	// Already has /raw (with or without commit + filename) — leave as-is.
	for _, p := range parts[2:] {
		if p == "raw" {
			return raw, true
		}
	}
	return fmt.Sprintf("https://gist.github.com/%s/%s/raw", owner, gistID), true
}

// normalizeGitHubURL converts github.com/{owner}/{repo}/blob/{branch}/{path}
// to the corresponding raw.githubusercontent.com URL.
func normalizeGitHubURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Host != "github.com" {
		return raw
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" {
		return raw
	}
	owner, repo, branch := parts[0], parts[1], parts[3]
	rest := strings.Join(parts[4:], "/")
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, rest)
}

// trimURLPunctuation strips sentence-terminator characters from the end
// of a user-pasted URL. We trim everything in the closing-bracket /
// punctuation set that's almost never legitimately the last character
// of a real URL.
func trimURLPunctuation(raw string) string {
	raw = strings.TrimSpace(raw)
	for len(raw) > 0 {
		last := raw[len(raw)-1]
		switch last {
		case '.', ',', ';', ':', '!', '?', ')', ']', '>', '"', '\'':
			raw = raw[:len(raw)-1]
		default:
			return raw
		}
	}
	return raw
}

func titleFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		return raw
	}
	return base
}

// looksLikeMarkdown returns true if the fetched content looks like
// markdown (or plain text we're willing to treat as markdown), and false
// if it looks like HTML, JavaScript, JSON, or a binary blob.
//
// This is intentionally a sniff, not a strict mime check — many GitHub
// raw URLs come back as text/plain with no extension, and we still want
// to accept those.
func looksLikeMarkdown(content, sourceURL string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	low := strings.ToLower(trimmed)

	// Clear-cut rejects: HTML / SVG / XML preambles, anything that's
	// obviously a web page rather than a document.
	for _, p := range []string{
		"<!doctype html",
		"<html",
		"<?xml",
		"<svg",
	} {
		if strings.HasPrefix(low, p) {
			return false
		}
	}
	// Embedded <script> / <head> very early suggests this is a rendered
	// HTML page rather than a doc with incidental tags.
	head := low
	if len(head) > 2048 {
		head = head[:2048]
	}
	if strings.Contains(head, "<script") && strings.Contains(head, "</script>") {
		return false
	}
	if strings.Contains(head, "<head>") && strings.Contains(head, "</head>") {
		return false
	}

	// Accept anything whose URL ends in .md / .markdown / .txt — even if
	// the content sniff would otherwise be ambiguous.
	if u, err := url.Parse(sourceURL); err == nil {
		p := strings.ToLower(u.Path)
		for _, ext := range []string{".md", ".markdown", ".mdx", ".txt"} {
			if strings.HasSuffix(p, ext) {
				return true
			}
		}
	}

	// Fallback heuristic: a markdown doc almost always has SOME of these
	// near the top — a heading, a list marker, a fence, a link/image, or
	// a blank line followed by prose. If we see none of that, we err on
	// the side of rejecting (better to ask than to clone a JS file).
	for _, marker := range []string{
		"# ", "## ", "### ",
		"* ", "- ", "1. ",
		"```",
		"](", // markdown link
		"![", // markdown image
		"> ", // blockquote
	} {
		if strings.Contains(head, marker) {
			return true
		}
	}
	// Last resort: if it's clearly text (no NUL bytes, mostly printable),
	// allow it.
	if !strings.ContainsRune(trimmed, 0) {
		return true
	}
	return false
}

// selfDocPath returns the doc UUID if `raw` looks like a markupmarkdown
// /d/:id URL on our own host. Used to redirect the user to the existing
// doc instead of cloning the SPA's HTML page when they paste their own
// markupmarkdown URL into the URL field.
func (a *API) selfDocPath(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	frontHost := ""
	if fu, err := url.Parse(a.cfg.Frontend.URL); err == nil {
		frontHost = fu.Hostname()
	}
	// Match either the configured frontend hostname or the Fly-default
	// markupmarkdown.fly.dev (since that's the canonical fallback).
	if host != frontHost && host != "markupmarkdown.fly.dev" {
		return ""
	}
	// Path is /d/<id>; tolerate trailing slash + ignore any query/fragment.
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "d" {
		return ""
	}
	return parts[1]
}
