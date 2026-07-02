package api

// Review-state handlers. A reviewer sets one of {approved,
// changes_requested, commented} on a specific doc revision, and that
// state feeds the pushback gate in pushback.go (changes_requested
// blocks a push unless explicitly overridden). Agents get the same
// surface as humans: leaving a review over MCP is identical to
// leaving one via the cookie session, and the pushback gate treats
// both alike. Reviews live on the specific doc revision; new child
// revisions start fresh, mirroring GitHub's "stale reviews get
// dismissed" behavior.

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"markupmarkdown/internal/models"
)

// setReviewRequest is the body of PUT /api/documents/:id/review.
type setReviewRequest struct {
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
}

// ValidateReviewState is the single point of truth for the accepted
// state vocabulary. Shared with the MCP surface.
func ValidateReviewState(s string) (models.ReviewState, error) {
	switch models.ReviewState(strings.TrimSpace(s)) {
	case models.ReviewStateApproved:
		return models.ReviewStateApproved, nil
	case models.ReviewStateChangesRequested:
		return models.ReviewStateChangesRequested, nil
	case models.ReviewStateCommented:
		return models.ReviewStateCommented, nil
	}
	return "", errors.New("state must be one of: approved, changes_requested, commented")
}

// setReview is PUT /api/documents/:id/review — upserts the current
// user's review state on the doc. Cookie sessions AND write-scope
// Bearer tokens both work; agents show up as agent reviewers.
func (a *API) setReview(w http.ResponseWriter, r *http.Request) {
	docID := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, docID)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	capBody(w, r, maxBodyDefault)

	var req setReviewRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err := ValidateReviewState(req.State)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Note) > 2000 {
		writeError(w, http.StatusBadRequest, "note is too long (max 2000 characters)")
		return
	}

	rec := &models.Review{
		DocumentID: doc.ID,
		UserID:     user.ID,
		State:      state,
		Note:       strings.TrimSpace(req.Note),
		ActorKind:  actorKindFor(r),
	}
	if info, ok := tokenInfoFromRequest(r); ok {
		rec.TokenID = info.TokenID
	}
	if err := a.store.UpsertReview(r.Context(), rec); err != nil {
		internalError(w, "store.upsert_review", err)
		return
	}
	if info, ok := tokenInfoFromRequest(r); ok {
		a.logTokenAction(r.Context(), info.TokenID, "review."+string(state), doc.ID)
	}
	a.hub.Broadcast(doc.ID, "reviews-updated")

	got, _ := a.store.GetReview(r.Context(), doc.ID, user.ID)
	if got == nil {
		got = rec
	}
	a.resolveReviewIdentities(r.Context(), []models.Review{*got})
	got.Mine = true
	writeJSON(w, http.StatusOK, got)
}

// deleteReview is DELETE /api/documents/:id/review — clears the
// current user's review.
func (a *API) deleteReview(w http.ResponseWriter, r *http.Request) {
	docID := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, docID)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if _, err := a.store.DeleteReview(r.Context(), doc.ID, user.ID); err != nil {
		internalError(w, "store.delete_review", err)
		return
	}
	a.hub.Broadcast(doc.ID, "reviews-updated")
	w.WriteHeader(http.StatusNoContent)
}

// listReviews is GET /api/documents/:id/reviews — returns every
// review on this doc, newest first, with display fields resolved.
// The doc GET response already carries a ReviewSummary; this
// endpoint is for the "who reviewed" surface.
func (a *API) listReviews(w http.ResponseWriter, r *http.Request) {
	docID := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, docID); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	reviews, err := a.store.ListReviews(r.Context(), docID)
	if err != nil {
		internalError(w, "store.list_reviews", err)
		return
	}
	a.resolveReviewIdentities(r.Context(), reviews)
	vid := a.viewerID(r)
	for i := range reviews {
		if vid != "" {
			reviews[i].Mine = reviews[i].UserID == vid
		}
	}
	writeJSON(w, http.StatusOK, reviews)
}

// resolveReviewIdentities overlays the display fields on agent-authored
// reviews from the current token + user records, and populates author
// display for human reviews from the user record. Mirrors
// resolveAgentIdentities on comments.
func (a *API) resolveReviewIdentities(ctx context.Context, reviews []models.Review) {
	tokenIDs := map[string]struct{}{}
	userIDs := map[string]struct{}{}
	for _, rv := range reviews {
		if rv.UserID != "" {
			userIDs[rv.UserID] = struct{}{}
		}
		if rv.ActorKind == models.ActorAgent && rv.TokenID != "" {
			tokenIDs[rv.TokenID] = struct{}{}
		}
	}
	tokens, _ := a.store.GetAPITokensByIDs(ctx, mapKeys(tokenIDs))
	users := map[string]*models.User{}
	for uid := range userIDs {
		if u, _ := a.store.GetUser(ctx, uid); u != nil {
			users[uid] = u
		}
	}
	for i := range reviews {
		rv := &reviews[i]
		u := users[rv.UserID]
		if rv.ActorKind == models.ActorAgent {
			if tok := tokens[rv.TokenID]; tok != nil {
				rv.Author = tok.Label
			}
			if u != nil {
				rv.OwnerName = preferName(u)
				rv.OwnerLogin = u.Login
			}
			// Agents get the bot glyph, not the token owner's avatar.
			rv.AuthorAvatarURL = ""
		} else if u != nil {
			rv.Author = preferName(u)
			rv.AuthorAvatarURL = u.AvatarURL
		}
	}
}

// summarizeReviews collapses a review list into the aggregate view
// exposed on document responses.
func summarizeReviews(reviews []models.Review) models.ReviewSummary {
	var s models.ReviewSummary
	for _, r := range reviews {
		switch r.State {
		case models.ReviewStateApproved:
			s.Approved++
		case models.ReviewStateChangesRequested:
			s.ChangesRequested++
		case models.ReviewStateCommented:
			s.Commented++
		}
	}
	return s
}
