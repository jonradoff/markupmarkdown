package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/models"
)

const (
	sessionCookie = "mm_session"
	sessionTTL    = 30 * 24 * time.Hour
)

// userKey is the request context key for the authenticated user.
type userKey struct{}

func (a *API) gh() *auth.GitHubClient {
	return &auth.GitHubClient{
		ClientID:     a.cfg.GitHub.ClientID,
		ClientSecret: a.cfg.GitHub.ClientSecret,
		CallbackURL:  a.cfg.GitHub.CallbackURL,
		Scope:        a.cfg.GitHub.Scope,
	}
}

func (a *API) currentUser(r *http.Request) *models.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	sess, err := a.store.GetSession(r.Context(), c.Value)
	if err != nil || sess == nil {
		return nil
	}
	u, err := a.store.GetUser(r.Context(), sess.UserID)
	if err != nil {
		return nil
	}
	return u
}

func (a *API) authConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"githubEnabled":  a.cfg.GitHub.Enabled(),
		"githubClientId": a.cfg.GitHub.ClientID,
	})
}

func (a *API) authMe(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	if u == nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (a *API) authLogin(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.GitHub.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "github oauth is not configured")
		return
	}
	state := auth.RandomToken(16)
	redirect := r.URL.Query().Get("redirect")
	if !isSafeRedirect(redirect) {
		redirect = "/"
	}
	if err := a.store.InsertAuthState(r.Context(), &models.AuthState{
		ID:        state,
		Redirect:  redirect,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, a.gh().AuthorizeURL(state), http.StatusFound)
}

func (a *API) authCallback(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.GitHub.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "github oauth is not configured")
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state")
		return
	}
	st, err := a.store.ConsumeAuthState(r.Context(), state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if st == nil {
		writeError(w, http.StatusBadRequest, "invalid or expired state")
		return
	}

	token, err := a.gh().ExchangeCode(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	ghUser, err := a.gh().FetchUser(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	displayName := ghUser.Name
	if displayName == "" {
		displayName = ghUser.Login
	}

	user := &models.User{
		ID:          uuid.NewString(),
		GitHubID:    ghUser.ID,
		Login:       ghUser.Login,
		Name:        displayName,
		Email:       ghUser.Email,
		AvatarURL:   ghUser.AvatarURL,
		AccessToken: token,
	}
	if err := a.store.UpsertUserByGitHubID(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().UTC()
	sess := &models.Session{
		ID:        auth.RandomToken(24),
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(sessionTTL),
	}
	if err := a.store.InsertSession(r.Context(), sess); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sess.ID,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(a.cfg.Frontend.URL, "https://"),
	})

	redirect := st.Redirect
	if !isSafeRedirect(redirect) {
		redirect = "/"
	}
	// Redirect back to the frontend, not to this backend host.
	dest := a.cfg.Frontend.URL + redirect
	http.Redirect(w, r, dest, http.StatusFound)
}

func (a *API) authLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err == nil && c.Value != "" {
		_ = a.store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(a.cfg.Frontend.URL, "https://"),
	})
	w.WriteHeader(http.StatusNoContent)
}

// isSafeRedirect ensures the redirect is a local path (not an open redirect).
func isSafeRedirect(s string) bool {
	if s == "" {
		return false
	}
	if !strings.HasPrefix(s, "/") {
		return false
	}
	if strings.HasPrefix(s, "//") {
		return false
	}
	if _, err := url.Parse(s); err != nil {
		return false
	}
	return true
}
