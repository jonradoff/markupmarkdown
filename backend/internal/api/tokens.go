package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/v2/bson"

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
	// Scope must be one of read/write/admin. Empty defaults to "write" for
	// agent-style use cases (read + comment).
	Scope string `json:"scope,omitempty"`
	// ExpiresInDays optionally sets an expiration date relative to now. 0
	// or unset means "use the server default" (90 days). A negative value
	// disables expiration. Frontend UI clamps to {30, 90, 365, never}.
	ExpiresInDays int `json:"expiresInDays,omitempty"`
}

type updateTokenRequest struct {
	Label *string `json:"label,omitempty"`
	Scope *string `json:"scope,omitempty"`
}

// tokenScopeDefault is what a token gets if the caller omits the field.
// Matches the legacy behavior of pre-scope tokens.
const tokenScopeDefault = models.TokenScopeWrite

// tokenExpiryDefault is how far in the future an unspecified expiration
// lands. Limits blast radius if a token leaks and the user forgets about it.
const tokenExpiryDefault = 90 * 24 * time.Hour

func parseScope(s string) (models.TokenScope, bool) {
	switch models.TokenScope(s) {
	case models.TokenScopeRead, models.TokenScopeWrite, models.TokenScopeAdmin:
		return models.TokenScope(s), true
	case "":
		return tokenScopeDefault, true
	}
	return "", false
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

	scope, ok := parseScope(req.Scope)
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be one of: read, write, admin")
		return
	}

	now := time.Now().UTC()
	var expiresAt *time.Time
	switch {
	case req.ExpiresInDays == 0:
		t := now.Add(tokenExpiryDefault)
		expiresAt = &t
	case req.ExpiresInDays < 0:
		expiresAt = nil // explicitly never expires
	default:
		t := now.Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}

	plaintext, hash, prefix := generateToken()
	rec := &models.APIToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		Hash:      hash,
		Prefix:    prefix,
		Label:     label,
		Scope:     scope,
		CreatedAt: now,
		ExpiresAt: expiresAt,
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
	if !a.enforceRate(w, r, a.rlTokenEdit, "Too many token edits — slow down.") {
		return
	}
	id := mux.Vars(r)["id"]
	capBody(w, r, maxBodyAuth)
	var req updateTokenRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Label == nil && req.Scope == nil {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}

	// Pre-read existing token so we can skip work on a no-op rename and
	// avoid broadcasting spurious refresh events. Scoped to this user
	// so a 404 is safe to surface as "not found".
	existing, err := a.store.GetAPITokenByID(r.Context(), user.ID, id)
	if err != nil {
		internalError(w, "store.get_token", err)
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "token not found")
		return
	}

	set := bson.M{}
	labelChanged := false
	if req.Label != nil {
		label := strings.TrimSpace(*req.Label)
		if label == "" {
			writeError(w, http.StatusBadRequest, "label cannot be empty")
			return
		}
		if len(label) > 80 {
			writeError(w, http.StatusBadRequest, "label too long")
			return
		}
		if label != existing.Label {
			set["label"] = label
			labelChanged = true
		}
	}
	if req.Scope != nil {
		scope, ok := parseScope(*req.Scope)
		if !ok {
			writeError(w, http.StatusBadRequest, "scope must be one of: read, write, admin")
			return
		}
		if scope != existing.Scope {
			set["scope"] = scope
		}
	}

	if len(set) == 0 {
		// No-op update — return success without broadcasting.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := a.store.UpdateAPITokenFields(r.Context(), user.ID, id, set); err != nil {
		internalError(w, "store.update_token_fields", err)
		return
	}

	// Only broadcast on label change — scope changes don't affect display.
	if labelChanged {
		go func() {
			ctx := contextDetached()
			ids, _ := a.store.DistinctDocIDsForToken(ctx, id)
			for _, did := range ids {
				a.hub.Broadcast(did, "comments-updated")
			}
		}()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) tokenActivity(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if _, hasToken := tokenInfoFromRequest(r); hasToken {
		writeError(w, http.StatusForbidden, "token activity can only be viewed from a signed-in browser session")
		return
	}
	id := mux.Vars(r)["id"]
	// Verify the token belongs to this user before exposing its log.
	tok, err := a.store.GetAPITokenByID(r.Context(), user.ID, id)
	if err != nil {
		internalError(w, "store.get_token", err)
		return
	}
	if tok == nil {
		writeError(w, http.StatusNotFound, "token not found")
		return
	}
	events, err := a.store.ListTokenEvents(r.Context(), id, 50)
	if err != nil {
		internalError(w, "store.list_token_events", err)
		return
	}
	if events == nil {
		events = []models.TokenEvent{}
	}
	writeJSON(w, http.StatusOK, events)
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
