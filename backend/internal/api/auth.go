package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/models"
)

const (
	sessionCookie  = "mm_session"
	oauthCookie    = "mm_oauth"
	sessionTTL     = 30 * 24 * time.Hour
	oauthCookieTTL = 10 * time.Minute
)

// userKey is the request context key for the authenticated user.
type userKey struct{}

// tokenInfoKey carries the API token used (if any) so write handlers can
// stamp comments with actor_kind="agent" when the token says so.
type tokenInfoKey struct{}

type tokenInfo struct {
	TokenID string
	Label   string
}

func authTokenFromHeader(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

// HashToken returns the SHA-256 hex digest of t. Exported for the MCP server.
func HashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func (a *API) gh() *auth.GitHubClient {
	return &auth.GitHubClient{
		ClientID:     a.cfg.GitHub.ClientID,
		ClientSecret: a.cfg.GitHub.ClientSecret,
		CallbackURL:  a.cfg.GitHub.CallbackURL,
		Scope:        a.cfg.GitHub.Scope,
	}
}

// currentUser resolves the request's identity. Bearer token wins over session
// cookie when both are present so a script with a token attached doesn't
// silently fall back to the browser's logged-in user.
func (a *API) currentUser(r *http.Request) *models.User {
	if tok := authTokenFromHeader(r); tok != "" {
		if u := a.userFromToken(r, tok); u != nil {
			return u
		}
		// Bad bearer is an explicit reject, not a fall-through to cookies.
		return nil
	}
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

// userFromToken looks up the user behind a Bearer token. Side-effects:
// stashes token metadata on the request context (used to stamp agent badges)
// and bumps last_used_at asynchronously.
func (a *API) userFromToken(r *http.Request, tok string) *models.User {
	if !strings.HasPrefix(tok, "mmk_") || len(tok) < 32 {
		return nil
	}
	rec, err := a.store.GetAPITokenByHash(r.Context(), HashToken(tok))
	if err != nil || rec == nil {
		return nil
	}
	u, err := a.store.GetUser(r.Context(), rec.UserID)
	if err != nil || u == nil {
		return nil
	}
	// Stash token info on the request so write handlers can mark agent
	// and stamp the bot identity (token label) on created content.
	*r = *r.WithContext(contextWithTokenInfo(r.Context(), tokenInfo{
		TokenID: rec.ID,
		Label:   rec.Label,
	}))
	// Touch last-used in the background; never block the request on it.
	go a.store.TouchAPIToken(contextDetached(), rec.ID)
	return u
}

func contextWithTokenInfo(parent context.Context, info tokenInfo) context.Context {
	return context.WithValue(parent, tokenInfoKey{}, info)
}

func tokenInfoFromRequest(r *http.Request) (tokenInfo, bool) {
	v, ok := r.Context().Value(tokenInfoKey{}).(tokenInfo)
	return v, ok
}

// contextDetached returns a fresh background context for fire-and-forget work
// that shouldn't be cancelled when the request goroutine ends.
func contextDetached() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = cancel // we deliberately leak — the timeout is the lifetime guard
	return ctx
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
	if !a.enforceRate(w, r, a.rlOAuthStart, "Too many sign-in attempts. Try again in a minute.") {
		return
	}
	state := auth.RandomToken(16)
	cookieValue := auth.RandomToken(24)
	redirect := r.URL.Query().Get("redirect")
	if !isSafeRedirect(redirect) {
		redirect = "/"
	}
	if err := a.store.InsertAuthState(r.Context(), &models.AuthState{
		ID:          state,
		Redirect:    redirect,
		CookieValue: cookieValue,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		internalError(w, "auth.insert_state", err)
		return
	}
	// Bind the state to this browser so an attacker can't trick a victim
	// into completing a callback for the attacker's own pending login.
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookie,
		Value:    cookieValue,
		Path:     "/api/auth/",
		MaxAge:   int(oauthCookieTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode, // must be Lax to survive top-level redirect
		Secure:   strings.HasPrefix(a.cfg.Frontend.URL, "https://"),
	})
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
		internalError(w, "auth.consume_state", err)
		return
	}
	if st == nil {
		writeError(w, http.StatusBadRequest, "invalid or expired state")
		return
	}
	// Reject if this browser didn't initiate the flow.
	cookie, err := r.Cookie(oauthCookie)
	if err != nil || cookie.Value == "" || cookie.Value != st.CookieValue {
		writeError(w, http.StatusBadRequest, "this sign-in request did not originate from your browser")
		return
	}
	// One-shot cookie; clear it.
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookie,
		Value:    "",
		Path:     "/api/auth/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(a.cfg.Frontend.URL, "https://"),
	})

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
		SameSite: http.SameSiteStrictMode,
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
		SameSite: http.SameSiteStrictMode,
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
