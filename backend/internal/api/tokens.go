package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"markupmarkdown/internal/models"
)

const tokenPlaintextPrefix = "mmk_"

// generateToken returns (plaintext, hash, prefix). Plaintext is shown to
// the user once at creation, then never again — we store hash only.
func generateToken() (plaintext, hash, prefix string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	plaintext = tokenPlaintextPrefix + hex.EncodeToString(b)
	hash = HashToken(plaintext)
	prefix = plaintext[:12] + "…"
	return
}

type createTokenRequest struct {
	Label string `json:"label"`
}

type updateTokenRequest struct {
	Label *string `json:"label,omitempty"`
}

func (a *API) listTokens(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	toks, err := a.store.ListAPITokensForUser(r.Context(), user.ID)
	if err != nil {
		internalError(w, "store.list_tokens", err)
		return
	}
	if toks == nil {
		toks = []models.APIToken{}
	}
	writeJSON(w, http.StatusOK, toks)
}

func (a *API) createToken(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	// Cookie-only — don't let a token mint more tokens. Protects against
	// privilege escalation if a token leaks.
	if _, hasToken := tokenInfoFromRequest(r); hasToken {
		writeError(w, http.StatusForbidden, "tokens can only be created from a signed-in browser session")
		return
	}
	capBody(w, r, maxBodyAuth)

	var req createTokenRequest
	_ = readJSON(r, &req)
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "Untitled token"
	}
	if len(label) > 80 {
		writeError(w, http.StatusBadRequest, "label too long")
		return
	}

	plaintext, hash, prefix := generateToken()
	rec := &models.APIToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		Hash:      hash,
		Prefix:    prefix,
		Label:     label,
		CreatedAt: time.Now().UTC(),
	}
	if err := a.store.InsertAPIToken(r.Context(), rec); err != nil {
		internalError(w, "store.insert_token", err)
		return
	}

	// The plaintext is shown ONCE. Don't log it, don't store it, don't
	// return it again on a subsequent GET.
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":    plaintext,
		"metadata": rec,
	})
}

func (a *API) updateToken(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if _, hasToken := tokenInfoFromRequest(r); hasToken {
		writeError(w, http.StatusForbidden, "tokens can only be edited from a signed-in browser session")
		return
	}
	id := mux.Vars(r)["id"]
	capBody(w, r, maxBodyAuth)
	var req updateTokenRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Label == nil {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}
	label := strings.TrimSpace(*req.Label)
	if label == "" {
		writeError(w, http.StatusBadRequest, "label cannot be empty")
		return
	}
	if len(label) > 80 {
		writeError(w, http.StatusBadRequest, "label too long")
		return
	}
	if err := a.store.UpdateAPITokenLabel(r.Context(), user.ID, id, label); err != nil {
		internalError(w, "store.update_token_label", err)
		return
	}
	// Broadcast a refresh on every doc this token has commented on so open
	// viewers see the new label without reloading.
	go func() {
		ctx := contextDetached()
		ids, _ := a.store.DistinctDocIDsForToken(ctx, id)
		for _, did := range ids {
			a.hub.Broadcast(did, "comments-updated")
		}
	}()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) revokeToken(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	id := mux.Vars(r)["id"]
	if err := a.store.RevokeAPIToken(r.Context(), user.ID, id); err != nil {
		internalError(w, "store.revoke_token", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
