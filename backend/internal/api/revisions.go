package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"markupmarkdown/internal/ai"
	"markupmarkdown/internal/models"
)

type previewRevisionResponse struct {
	OriginalContent   string   `json:"originalContent"`
	RevisedContent    string   `json:"revisedContent"`
	Model             string   `json:"model"`
	TokensIn          int64    `json:"tokensIn"`
	TokensOut         int64    `json:"tokensOut"`
	CostEstimateUSD   float64  `json:"costEstimateUsd"`
	AppliedCommentIDs []string `json:"appliedCommentIds"`
	Identical         bool     `json:"identical"`
}

type previewRevisionRequest struct {
	// CommentIDs, when non-empty, restricts the revision to just these
	// resolved comment threads. Empty/missing means "apply all resolved".
	CommentIDs []string `json:"commentIds,omitempty"`
}

// previewRevision runs Claude over the doc + resolved comments and returns the
// proposed revision WITHOUT persisting it. The frontend shows a diff preview;
// `acceptRevision` is what actually creates the new doc.
func (a *API) previewRevision(w http.ResponseWriter, r *http.Request) {
	docID := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, docID)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, fetchErrorResponse{
			Error: "Sign in with GitHub to use AI revision.",
			Kind:  "sign_in_required",
			Actions: []fetchErrorAction{{
				Label: "Sign in with GitHub",
				URL:   "/api/auth/github/login?redirect=" + r.URL.Path,
			}},
		})
		return
	}
	// Tokens need at least write scope to spend the user's Anthropic key
	// on a preview. Cookie sessions always satisfy this.
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}

	apiKey, err := a.decryptedAnthropicKey(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, fetchErrorResponse{
			Error: "Failed to load your Anthropic API key: " + err.Error(),
			Kind:  "anthropic_key_error",
		})
		return
	}
	if apiKey == "" {
		writeJSON(w, http.StatusPreconditionRequired, fetchErrorResponse{
			Error: "Add your Anthropic API key to enable AI revision.",
			Kind:  "anthropic_key_missing",
			Actions: []fetchErrorAction{{
				Label: "Get an API key",
				URL:   "https://console.anthropic.com/account/keys",
			}},
		})
		return
	}

	// Rate-limit: 30 revisions/hour per user (regardless of which doc).
	if !a.rlRevise.Allow("u:" + user.ID) {
		rate429(w, "You've reached the AI-revision rate limit (30/hour). Try again later.")
		return
	}
	// At most 3 concurrent revisions per user.
	releaseSlot := a.reviseSlots.Acquire(user.ID)
	if releaseSlot == nil {
		writeJSON(w, http.StatusTooManyRequests, fetchErrorResponse{
			Error: "You already have the maximum (3) AI revisions in flight. Wait for one to finish.",
			Kind:  "rate_limited",
		})
		return
	}
	defer releaseSlot()

	// SSE connection cap.
	releaseSSE := a.sseCounter.Acquire("u:" + user.ID)
	if releaseSSE == nil {
		writeJSON(w, http.StatusServiceUnavailable, fetchErrorResponse{
			Error: "Too many open streaming connections. Close some tabs and retry.",
			Kind:  "sse_busy",
		})
		return
	}
	defer releaseSSE()

	// Pull comments. Need at least one resolved thread to do anything useful.
	allComments, err := a.store.ListComments(r.Context(), docID)
	if err != nil {
		internalError(w, "store.list_comments", err)
		return
	}

	// Optional client filter: revise only the supplied subset.
	var req previewRevisionRequest
	_ = readJSON(r, &req) // empty body is fine
	selected := map[string]bool{}
	for _, id := range req.CommentIDs {
		selected[id] = true
	}
	filterByIDs := len(selected) > 0

	var resolved []models.Comment
	for _, c := range allComments {
		if !c.Resolved {
			continue
		}
		if filterByIDs && !selected[c.ID] {
			continue
		}
		resolved = append(resolved, c)
	}
	if len(resolved) == 0 {
		message := "Resolve at least one comment before revising. AI revision only applies threads you've marked done."
		if filterByIDs {
			message = "None of the selected comments are resolved threads to apply."
		}
		writeJSON(w, http.StatusBadRequest, fetchErrorResponse{
			Error: message,
			Kind:  "no_resolved_comments",
		})
		return
	}

	revisionComments := make([]ai.ResolvedComment, 0, len(resolved))
	appliedIDs := make([]string, 0, len(resolved))
	for _, c := range resolved {
		appliedIDs = append(appliedIDs, c.ID)
		rc := ai.ResolvedComment{
			Quoted:     c.Anchor.Exact,
			Author:     c.Author,
			Body:       c.Body,
			ResolvedBy: c.ResolvedBy,
		}
		for _, rep := range c.Replies {
			rc.Replies = append(rc.Replies, ai.ResolvedReply{
				Author: rep.Author,
				Body:   rep.Body,
			})
		}
		revisionComments = append(revisionComments, rc)
	}

	// Stream the response as SSE so the user sees text appearing in real
	// time. We send three event types:
	//   delta — { "text": "..." } per chunk
	//   done  — { ...previewRevisionResponse } at the end
	//   error — { ...fetchErrorResponse } if generation fails
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	// Implicit 200 — once headers are out we can't switch status, so error
	// events flow inside the stream body.
	flusher.Flush()

	emit := func(event string, payload any) error {
		b, _ := json.Marshal(payload)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	onDelta := func(chunk string) error {
		return emit("delta", map[string]string{"text": chunk})
	}

	result, err := ai.Revise(r.Context(), apiKey, doc.Title, doc.Content, revisionComments, onDelta)
	if err != nil {
		var rev *ai.RevisionError
		if errors.As(err, &rev) {
			_ = emit("error", a.revisionErrorPayload(rev))
			return
		}
		_ = emit("error", fetchErrorResponse{Error: err.Error(), Kind: "ai_other"})
		return
	}

	identical := strings.TrimSpace(result.Content) == strings.TrimSpace(doc.Content)
	_ = emit("done", previewRevisionResponse{
		OriginalContent:   doc.Content,
		RevisedContent:    result.Content,
		Model:             result.Model,
		TokensIn:          result.TokensIn,
		TokensOut:         result.TokensOut,
		CostEstimateUSD:   estimateCostUSD(result.Model, result.TokensIn, result.TokensOut),
		AppliedCommentIDs: appliedIDs,
		Identical:         identical,
	})
}

type acceptRevisionRequest struct {
	Content           string   `json:"content"`
	Model             string   `json:"model"`
	TokensIn          int64    `json:"tokensIn"`
	TokensOut         int64    `json:"tokensOut"`
	AppliedCommentIDs []string `json:"appliedCommentIds"`
}

// acceptRevision creates a new document as a child of {id}, with the supplied
// revised content. The content comes from the preview call (client-roundtrip
// to avoid double-billing the user for a generation we already paid for).
func (a *API) acceptRevision(w http.ResponseWriter, r *http.Request) {
	parentID := mux.Vars(r)["id"]
	parent, accErr := a.checkDocAccess(r, parentID)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	// Accepting a revision creates a new document and is the most
	// privileged write — tokens need admin scope. Cookie sessions always
	// satisfy this.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	if info, ok := tokenInfoFromRequest(r); ok {
		a.logTokenAction(r.Context(), info.TokenID, "revision.accept", parentID)
	}
	capBody(w, r, maxBodyRevision)

	var req acceptRevisionRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	content := strings.TrimRight(req.Content, "\n") + "\n"
	if strings.TrimSpace(content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = ai.Model
	}

	now := time.Now().UTC()
	authorName := user.Name
	if authorName == "" {
		authorName = user.Login
	}
	doc := &models.Document{
		ID:           uuid.NewString(),
		Title:        parent.Title,
		Origin:       parent.Origin,
		SourceURL:    parent.SourceURL,
		Content:      content,
		Private:      parent.Private,
		GitHubOwner:  parent.GitHubOwner,
		GitHubRepo:   parent.GitHubRepo,
		GitHubRef:    parent.GitHubRef,
		GitHubPath:   parent.GitHubPath,
		// Source SHA of the revision = the SHA the AI revision was
		// generated against (same as parent at the moment of revision).
		// This lets later drift checks compare against the current
		// upstream and offer a merge.
		SourceSHA:    parent.SourceSHA,
		ParentID:     parent.ID,
		// CreatedByID makes the accepted revision show up in the user's
		// home list. Without it the doc is reachable only via the parent
		// doc's revision-tree link.
		CreatedByID:  user.ID,
		RevisionMeta: &models.RevisionMeta{
			Model:             model,
			AppliedCommentIDs: req.AppliedCommentIDs,
			TokensIn:          req.TokensIn,
			TokensOut:         req.TokensOut,
			GeneratedBy:       authorName,
			GeneratedByID:     user.ID,
			GeneratedAt:       now,
			// Ancestor pins the parent state at revision time. The
			// merge engine uses (AncestorContent, child Content, new
			// upstream content) as the 3-way merge inputs to reconcile
			// a revised doc with later upstream changes.
			AncestorSourceSHA: parent.SourceSHA,
			AncestorContent:   parent.Content,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.store.InsertDocument(r.Context(), doc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Carry unresolved comments + replies forward to the new revision.
	// Resolved comments are NOT copied: they're already "applied" — the
	// AI baked them into the new content. Unresolved threads represent
	// ongoing review and would silently disappear otherwise.
	//
	// Comments are re-anchored against the child's new (revised) content.
	// Threads whose quoted text still appears stay as inline highlights;
	// the rest become orphans in the new doc's orphan section, just like
	// after a Sync. Doc-level pins (empty anchor) always carry.
	carried := a.copyOpenCommentsToChild(r.Context(), parent.ID, doc)
	if carried > 0 {
		a.hub.Broadcast(doc.ID, "comments-updated")
	}

	writeJSON(w, http.StatusCreated, doc)
}

// copyOpenCommentsToChild duplicates every unresolved comment from
// parentID onto childDoc, re-anchoring against the child's content.
// Returns the count of comments inserted on the child. Errors per
// comment are logged but don't abort the rest — losing a few re-anchored
// copies is better than losing them all if Mongo blips midway.
func (a *API) copyOpenCommentsToChild(ctx context.Context, parentID string, child *models.Document) int {
	parents, err := a.store.ListComments(ctx, parentID)
	if err != nil {
		return 0
	}
	if len(parents) == 0 {
		return 0
	}
	now := time.Now().UTC()
	inserted := 0
	for _, src := range parents {
		if src.Resolved {
			continue
		}
		copy := buildCarriedComment(src, child, now)
		if err := a.store.InsertComment(ctx, &copy); err == nil {
			inserted++
		}
	}
	return inserted
}

// buildCarriedComment produces the child-doc copy of an unresolved
// parent comment, with new ID, the same author/body/replies, and
// a re-anchored anchor (clean / orphan / doc-level) against the child
// content. Doc-level (empty anchor) carries verbatim. Inline anchors
// are re-checked against render.PlainText(child.Content); a miss
// flips to orphan with OriginalExact preserved.
func buildCarriedComment(src models.Comment, child *models.Document, now time.Time) models.Comment {
	out := models.Comment{
		ID:              uuid.NewString(),
		DocumentID:      child.ID,
		Anchor:          src.Anchor,
		Author:          src.Author,
		AuthorID:        src.AuthorID,
		AuthorAvatarURL: src.AuthorAvatarURL,
		ActorKind:       src.ActorKind,
		TokenID:         src.TokenID,
		Body:            src.Body,
		Resolved:        false,
		CreatedAt:       src.CreatedAt,
		UpdatedAt:       now,
	}
	// Replies carry verbatim, with new IDs to avoid colliding with the
	// parent's reply documents.
	for _, rep := range src.Replies {
		out.Replies = append(out.Replies, models.Reply{
			ID:              uuid.NewString(),
			Author:          rep.Author,
			AuthorID:        rep.AuthorID,
			AuthorAvatarURL: rep.AuthorAvatarURL,
			ActorKind:       rep.ActorKind,
			TokenID:         rep.TokenID,
			Body:            rep.Body,
			CreatedAt:       rep.CreatedAt,
			UpdatedAt:       rep.UpdatedAt,
		})
	}
	if out.Replies == nil {
		out.Replies = []models.Reply{}
	}
	// Doc-level pins always carry; nothing to re-anchor.
	if isDocLevel(out.Anchor) {
		return out
	}
	// Inline: reuse the existing single-comment reanchor decision.
	res := reanchorComments([]models.Comment{out}, child.Content)[0]
	switch res.Status {
	case reanchorClean:
		out.Anchor.Start = 0
		out.Anchor.End = 0
		out.Anchor.Exact = res.Exact
	case reanchorOrphan:
		out.Orphan = true
		out.OriginalExact = res.OriginalExact
	}
	return out
}

func (a *API) revisionErrorPayload(rev *ai.RevisionError) fetchErrorResponse {
	resp := fetchErrorResponse{Error: rev.Message, Kind: "ai_" + string(rev.Kind)}
	if rev.Kind == ai.ErrKindInvalidKey {
		resp.Actions = append(resp.Actions, fetchErrorAction{
			Label: "Get an API key",
			URL:   "https://console.anthropic.com/account/keys",
		})
	}
	return resp
}

// estimateCostUSD returns a rough dollar figure for a single Opus 4.7 call
// using current public pricing ($5 / $25 per million in/out). Best-effort —
// real billing happens on the user's Anthropic account.
func estimateCostUSD(model string, in, out int64) float64 {
	inPrice, outPrice := 5.0, 25.0
	switch model {
	case "claude-sonnet-4-6":
		inPrice, outPrice = 3.0, 15.0
	case "claude-haiku-4-5":
		inPrice, outPrice = 1.0, 5.0
	}
	return (float64(in)*inPrice + float64(out)*outPrice) / 1_000_000
}
