package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"markupmarkdown/internal/ai"
	"markupmarkdown/internal/secrets"
)

type anthropicKeyStatus struct {
	HasKey  bool   `json:"hasKey"`
	Hint    string `json:"hint,omitempty"`
	SetAt   string `json:"setAt,omitempty"`
	Enabled bool   `json:"enabled"` // whether server is configured to accept keys
}

func (a *API) getAnthropicKey(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	resp := anthropicKeyStatus{Enabled: a.vault != nil}
	us, err := a.store.GetUserSecrets(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if us != nil && us.AnthropicKeyCiphertext != "" {
		resp.HasKey = true
		resp.Hint = us.AnthropicKeyHint
		if !us.AnthropicKeySetAt.IsZero() {
			resp.SetAt = us.AnthropicKeySetAt.UTC().Format(time.RFC3339)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type putAnthropicKeyRequest struct {
	Key string `json:"key"`
}

func (a *API) putAnthropicKey(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if a.vault == nil {
		writeError(w, http.StatusServiceUnavailable, "this server is not configured to store API keys (encryption key missing)")
		return
	}
	if !a.rlAPIKeyPut.Allow("u:" + user.ID) {
		rate429(w, "Too many key-update attempts. Try again in an hour.")
		return
	}
	capBody(w, r, maxBodyAuth)

	var req putAnthropicKeyRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Key) > 256 {
		writeError(w, http.StatusBadRequest, "key is too long")
		return
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	// Validate against Anthropic before storing — fast feedback if the key
	// is wrong, and we never persist a key that doesn't work.
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := ai.ValidateAPIKey(ctx, key); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
			"kind":  "invalid_anthropic_key",
		})
		return
	}

	ciphertext, err := a.vault.Encrypt(key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.store.UpsertAnthropicKey(r.Context(), user.ID, ciphertext, secrets.Hint(key)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.getAnthropicKey(w, r)
}

func (a *API) deleteAnthropicKey(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if err := a.store.DeleteAnthropicKey(r.Context(), user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decryptedAnthropicKey loads the user's current key (if any) and decrypts it.
// Returns ("", nil) if the user has no key on file; an error if the vault is
// not configured.
func (a *API) decryptedAnthropicKey(ctx context.Context, userID string) (string, error) {
	if a.vault == nil {
		return "", nil
	}
	us, err := a.store.GetUserSecrets(ctx, userID)
	if err != nil {
		return "", err
	}
	if us == nil || us.AnthropicKeyCiphertext == "" {
		return "", nil
	}
	return a.vault.Decrypt(us.AnthropicKeyCiphertext)
}
