package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"markupmarkdown/internal/models"
)

// manualRevisionRequest is the body of POST /api/documents/:id/manual-revisions.
// A manual edit IS a revision — same chain semantics as Revise with AI,
// minus the AI-generated metadata. Carrying it through the existing
// revision plumbing means the merge engine, leaf-dedup, comment carry-
// forward, and breadcrumb logic all just work, with one mental model.
type manualRevisionRequest struct {
	Content string `json:"content"`
	// Note is optional — a short human-friendly summary of what
	// changed in this edit, surfaced in the revision-history sidebar.
	Note string `json:"note,omitempty"`
}

// createManualRevision creates a new child document representing a
// human-authored edit of the parent. Unresolved comments carry forward
// (same code path acceptRevision uses).
func (a *API) createManualRevision(w http.ResponseWriter, r *http.Request) {
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
	// Manual edits create new docs — same admin-scope bar acceptRevision
	// uses. Cookie sessions always satisfy this.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	if info, ok := tokenInfoFromRequest(r); ok {
		a.logTokenAction(r.Context(), info.TokenID, "revision.manual", parentID)
	}
	capBody(w, r, maxBodyRevision)

	var req manualRevisionRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	content := strings.TrimRight(req.Content, "\n") + "\n"
	if strings.TrimSpace(content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	// No-op edit: don't bloat the chain with a snapshot identical to
	// the parent.
	if strings.TrimSpace(content) == strings.TrimSpace(parent.Content) {
		writeError(w, http.StatusBadRequest, "edit is identical to the current content — nothing to save")
		return
	}

	now := time.Now().UTC()
	authorName := user.Name
	if authorName == "" {
		authorName = user.Login
	}
	_ = strings.TrimSpace(req.Note) // reserved for future revision-history note surfacing

	doc := &models.Document{
		ID:          uuid.NewString(),
		Title:       parent.Title,
		Origin:      parent.Origin,
		SourceURL:   parent.SourceURL,
		Content:     content,
		Private:     parent.Private,
		GitHubOwner: parent.GitHubOwner,
		GitHubRepo:  parent.GitHubRepo,
		GitHubRef:   parent.GitHubRef,
		GitHubPath:  parent.GitHubPath,
		// Source SHA tracks the GitHub source state this manual edit
		// was based on — keeps the drift check meaningful on the new
		// revision.
		SourceSHA:   parent.SourceSHA,
		ParentID:    parent.ID,
		CreatedByID: user.ID,
		RevisionMeta: &models.RevisionMeta{
			// Model="manual" distinguishes human edits from AI revisions
			// in the UI without needing a separate Kind field. The
			// frontend renders "Manual edit by X" for this case.
			Model:             "manual",
			AppliedCommentIDs: nil,
			TokensIn:          0,
			TokensOut:         0,
			GeneratedBy:       authorName,
			GeneratedByID:     user.ID,
			GeneratedAt:       now,
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

	// Carry unresolved comments forward (same logic as acceptRevision).
	if carried := a.copyOpenCommentsToChild(r.Context(), parent.ID, doc); carried > 0 {
		a.hub.Broadcast(doc.ID, "comments-updated")
	}

	writeJSON(w, http.StatusCreated, doc)
}
