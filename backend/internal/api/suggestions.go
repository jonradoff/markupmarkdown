package api

// One-click apply for structured suggestions on anchored comments
// (P0-2). Empirically the highest-actionability review artifact in
// the doc-collaboration prior art (Brown & Parnin ESEC/FSE '20). A
// suggestion carries a replacement string; applying it creates a
// manual revision that swaps the comment's Anchor.Exact for the
// replacement, then resolves the comment. Doc-level comments (no
// anchor) can't carry suggestions — there's nothing to replace.

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/models"
)

// applySuggestion is POST /api/comments/:id/apply-suggestion. Reads
// the comment's Suggestion, replaces the first occurrence of
// Anchor.Exact in the doc content with Suggestion.Replacement, and
// creates a manual revision + resolves the comment. Idempotent-ish:
// once a suggestion is stamped applied (AppliedAt), a second call is
// a 409 (already applied) so double-clicks don't create duplicate
// revisions.
func (a *API) applySuggestion(w http.ResponseWriter, r *http.Request) {
	commentID := mux.Vars(r)["id"]
	comment, doc, accErr := a.checkCommentAccess(r, commentID)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	// Applying a suggestion creates a new doc — same admin bar as
	// acceptRevision and createManualRevision.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	if comment.Suggestion == nil {
		writeError(w, http.StatusBadRequest, "this comment has no suggestion attached")
		return
	}
	if comment.Suggestion.AppliedAt != nil {
		writeError(w, http.StatusConflict, "this suggestion was already applied")
		return
	}
	if comment.Anchor.Exact == "" {
		writeError(w, http.StatusBadRequest, "doc-level comments can't carry suggestions — there's nothing to replace")
		return
	}

	// Substitution against the source. First-occurrence replace on
	// the exact anchored span, same primitive stat federation of
	// GitHub's suggested changes uses.
	original := comment.Anchor.Exact
	replacement := comment.Suggestion.Replacement
	if !strings.Contains(doc.Content, original) {
		writeError(w, http.StatusUnprocessableEntity,
			"the anchored text no longer appears in the doc — re-anchor or re-write the suggestion")
		return
	}
	// Reject no-op replacements — nothing to commit.
	if original == replacement {
		writeError(w, http.StatusBadRequest, "suggestion replacement is identical to the anchored text")
		return
	}
	newContent := strings.Replace(doc.Content, original, replacement, 1)
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	// New child doc — mirrors createManualRevision's shape. Author
	// name and actor kind reflect the *applier* (usually a human
	// clicking Apply), not the suggestion's original author.
	now := time.Now().UTC()
	authorName := user.Name
	if authorName == "" {
		authorName = user.Login
	}
	childID := uuid.NewString()
	child := &models.Document{
		ID:          childID,
		Title:       doc.Title,
		Origin:      doc.Origin,
		SourceURL:   doc.SourceURL,
		SourceKind:  doc.SourceKind,
		Content:     newContent,
		Private:     doc.Private,
		GitHubOwner: doc.GitHubOwner,
		GitHubRepo:  doc.GitHubRepo,
		GitHubRef:   doc.GitHubRef,
		GitHubPath:  doc.GitHubPath,
		SourceSHA:   doc.SourceSHA,
		ParentID:    doc.ID,
		CreatedByID: user.ID,
		RevisionMeta: &models.RevisionMeta{
			Model:             "suggestion",
			GeneratedBy:       authorName,
			GeneratedByID:     user.ID,
			GeneratedAt:       now,
			ActorKind:         actorKindFor(r),
			AncestorSourceSHA: doc.SourceSHA,
			AncestorContent:   doc.Content,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if info, ok := tokenInfoFromRequest(r); ok {
		child.RevisionMeta.TokenID = info.TokenID
	}
	if err := a.store.InsertDocument(r.Context(), child); err != nil {
		internalError(w, "store.insert_suggestion_child", err)
		return
	}

	// Carry unresolved comments (same primitive as manual revision).
	// The applied comment itself gets marked resolved BEFORE the
	// carry, so it doesn't ride along.
	if err := a.stampSuggestionApplied(r.Context(), comment.ID, user, authorName, child.ID); err != nil {
		// Don't roll back the child — the revision is real; the
		// stamp is metadata. Log and continue.
		internalError(w, "store.stamp_suggestion_applied", err)
		return
	}
	if carried := a.copyOpenCommentsToChild(r.Context(), doc.ID, child); carried > 0 {
		a.hub.Broadcast(child.ID, "comments-updated")
	}
	// Track the token action for observability.
	if info, ok := tokenInfoFromRequest(r); ok {
		a.logTokenAction(r.Context(), info.TokenID, "suggestion.apply", child.ID)
	}
	a.hub.Broadcast(doc.ID, "doc-updated")

	writeJSON(w, http.StatusCreated, child)
}

// stampSuggestionApplied flips the suggestion's applied fields AND
// resolves the comment atomically. The comment stays on the parent
// (where it was written) — the "applied" state travels with the
// carry-forward pipeline via the standard resolve semantics.
func (a *API) stampSuggestionApplied(ctx context.Context, commentID string, user *models.User, appliedBy, childID string) error {
	now := time.Now().UTC()
	_, err := a.store.Comments().UpdateOne(ctx,
		bson.M{"_id": commentID},
		bson.M{"$set": bson.M{
			"suggestion.applied_at":     now,
			"suggestion.applied_by_id":  user.ID,
			"suggestion.applied_by":     appliedBy,
			"suggestion.applied_doc_id": childID,
			"resolved":                  true,
			"resolved_by":               appliedBy,
			"resolved_at":               now,
			"updated_at":                now,
		}})
	return err
}
