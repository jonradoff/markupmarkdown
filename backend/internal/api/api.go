package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"markupmarkdown/internal/config"
	"markupmarkdown/internal/secrets"
	"markupmarkdown/internal/store"
)

type API struct {
	cfg   *config.Config
	store *store.Store
	hub   *Hub
	vault *secrets.Vault
}

func New(cfg *config.Config, st *store.Store) (*API, error) {
	vault, err := secrets.NewVault(cfg.Encryption.MasterKey)
	if err != nil {
		return nil, err
	}
	return &API{cfg: cfg, store: st, hub: NewHub(), vault: vault}, nil
}

func (a *API) Register(r *mux.Router) {
	r.HandleFunc("/api/health", a.health).Methods("GET")

	r.HandleFunc("/api/auth/config", a.authConfig).Methods("GET")
	r.HandleFunc("/api/auth/me", a.authMe).Methods("GET")
	r.HandleFunc("/api/auth/github/login", a.authLogin).Methods("GET")
	r.HandleFunc("/api/auth/github/callback", a.authCallback).Methods("GET")
	r.HandleFunc("/api/auth/logout", a.authLogout).Methods("POST")

	r.HandleFunc("/api/documents", a.listDocuments).Methods("GET")
	r.HandleFunc("/api/documents", a.createDocument).Methods("POST")
	r.HandleFunc("/api/documents/{id}", a.getDocument).Methods("GET")
	r.HandleFunc("/api/documents/{id}", a.patchDocument).Methods("PATCH")
	r.HandleFunc("/api/documents/{id}", a.deleteDocument).Methods("DELETE")

	r.HandleFunc("/api/documents/{id}/comments", a.listComments).Methods("GET")
	r.HandleFunc("/api/documents/{id}/comments", a.createComment).Methods("POST")
	r.HandleFunc("/api/documents/{id}/events", a.streamEvents).Methods("GET")

	r.HandleFunc("/api/comments/{id}", a.patchComment).Methods("PATCH")
	r.HandleFunc("/api/comments/{id}", a.deleteComment).Methods("DELETE")
	r.HandleFunc("/api/comments/{id}/resolve", a.resolveComment).Methods("POST")
	r.HandleFunc("/api/comments/{id}/reopen", a.reopenComment).Methods("POST")
	r.HandleFunc("/api/comments/{id}/replies", a.createReply).Methods("POST")
	r.HandleFunc("/api/comments/{id}/replies/{replyId}", a.updateReply).Methods("PATCH")
	r.HandleFunc("/api/comments/{id}/replies/{replyId}", a.deleteReply).Methods("DELETE")

	r.HandleFunc("/api/me/anthropic-key", a.getAnthropicKey).Methods("GET")
	r.HandleFunc("/api/me/anthropic-key", a.putAnthropicKey).Methods("PUT")
	r.HandleFunc("/api/me/anthropic-key", a.deleteAnthropicKey).Methods("DELETE")

	r.HandleFunc("/api/documents/{id}/revise", a.previewRevision).Methods("POST")
	r.HandleFunc("/api/documents/{id}/revisions", a.acceptRevision).Methods("POST")
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
