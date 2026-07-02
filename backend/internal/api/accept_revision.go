package api

// Agent-proposed revision acceptance (P0-3). Any revision whose
// RevisionMeta.ActorKind == agent lands as "proposed" — the pushback
// flow refuses to ship it to GitHub until a human explicitly accepts
// it here. This is the GitBook change-request pattern applied to the
// markupmarkdown revision chain: the agent-authored revision is real
// and lives in the chain immediately, but the "push to the real
// repo" step is gated on a human's explicit review.

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/models"
)

// acceptAgentRevision is POST /api/documents/:id/accept-revision.
// Cookie-session only — a Bearer-token caller can't self-accept the
// revision they just wrote. That's the whole point of the gate.
//
// The revision may or may not be agent-authored: for human-authored
// revisions this handler is a no-op that returns the doc unchanged
// (idempotent), so the frontend can call it uniformly without
// having to inspect the actor kind first.
func (a *API) acceptAgentRevision(w http.ResponseWriter, r *http.Request) {
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
	// Cookie-only. A leaked Bearer token must not be able to accept
	// its own agent revision — that would defeat the gate entirely.
	if _, hasToken := tokenInfoFromRequest(r); hasToken {
		writeError(w, http.StatusForbidden,
			"agent revisions can only be accepted by cookie-authenticated humans, not tokens")
		return
	}

	// No revision meta → nothing to accept. Return the doc as-is.
	if doc.RevisionMeta == nil {
		writeJSON(w, http.StatusOK, doc)
		return
	}
	// Only agent-authored revisions need acceptance. Human-authored
	// ones are auto-accepted from the moment they're created.
	if doc.RevisionMeta.ActorKind != models.ActorAgent {
		writeJSON(w, http.StatusOK, doc)
		return
	}
	// Already accepted? Idempotent — return current state.
	if doc.RevisionMeta.AcceptedAt != nil {
		writeJSON(w, http.StatusOK, doc)
		return
	}

	now := time.Now().UTC()
	accepter := user.Name
	if accepter == "" {
		accepter = user.Login
	}
	_, err := a.store.Documents().UpdateOne(r.Context(),
		bson.M{"_id": doc.ID},
		bson.M{"$set": bson.M{
			"revision_meta.accepted_at":    now,
			"revision_meta.accepted_by_id": user.ID,
			"revision_meta.accepted_by":    accepter,
			"updated_at":                   now,
		}})
	if err != nil {
		internalError(w, "store.accept_revision", err)
		return
	}
	// Reflect the stamp in the response without another round-trip.
	doc.RevisionMeta.AcceptedAt = &now
	doc.RevisionMeta.AcceptedByID = user.ID
	doc.RevisionMeta.AcceptedBy = accepter
	doc.UpdatedAt = now

	a.hub.Broadcast(doc.ID, "doc-updated")
	writeJSON(w, http.StatusOK, doc)
}
