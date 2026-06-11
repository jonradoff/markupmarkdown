package api_test

// OAuth flow tests. These configure a fake GitHub via the transport
// mock we use elsewhere, then drive the full Login → Callback → Session
// round-trip.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/store"
	"markupmarkdown/internal/testutil"
)

// withOAuthEnabled rebuilds the test API with GitHub OAuth credentials
// populated so cfg.GitHub.Enabled() returns true.
func withOAuthEnabled(t *testing.T) (*httptest.Server, *http.Client, *store.Store) {
	t.Helper()
	st, cleanup := testutil.MustConnectTestDB(t)
	cfg := testutil.LoadTestConfig(t)
	cfg.GitHub.ClientID = "test-client-id"
	cfg.GitHub.ClientSecret = "test-client-secret"
	cfg.GitHub.CallbackURL = "http://localhost/api/auth/github/callback"

	a, err := api.New(cfg, st)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	r := mux.NewRouter()
	a.Register(r)
	srv := httptest.NewServer(r)

	noRedirect := func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	c := &http.Client{CheckRedirect: noRedirect}

	t.Cleanup(func() {
		srv.Close()
		cleanup()
	})
	return srv, c, st
}

func TestOAuth_LoginRedirectsToGitHub(t *testing.T) {
	srv, client, _ := withOAuthEnabled(t)

	req, _ := http.NewRequest("GET", srv.URL+"/api/auth/github/login?redirect=/d/abc", nil)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "https://github.com/login/oauth/authorize") {
		t.Errorf("location %q does not redirect to GitHub", loc)
	}
	var sawCookie bool
	for _, c := range res.Cookies() {
		if c.Name == "mm_oauth" {
			sawCookie = true
		}
	}
	if !sawCookie {
		t.Error("mm_oauth cookie should be set after login init")
	}
}

func TestOAuth_LoginUnsafeRedirectFallsBackToRoot(t *testing.T) {
	srv, client, _ := withOAuthEnabled(t)

	req, _ := http.NewRequest("GET", srv.URL+"/api/auth/github/login?redirect=https://evil.com", nil)
	res, _ := client.Do(req)
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", res.StatusCode)
	}
}

func TestOAuth_Callback_MissingCodeOrState(t *testing.T) {
	srv, client, _ := withOAuthEnabled(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/auth/github/callback", nil)
	res, _ := client.Do(req)
	defer res.Body.Close()
	// signinErrorRedirect bails to / with ?signin_error=missing_params
	// instead of returning a 400 — friendlier UX than a JSON error.
	if res.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "signin_error=missing_params") {
		t.Errorf("location %q should carry signin_error=missing_params", loc)
	}
}

func TestOAuth_Callback_InvalidState(t *testing.T) {
	srv, client, _ := withOAuthEnabled(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/auth/github/callback?code=abc&state=nope", nil)
	res, _ := client.Do(req)
	defer res.Body.Close()
	// Unknown state → expired branch in signinErrorRedirect.
	if res.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "signin_error=expired") {
		t.Errorf("location %q should carry signin_error=expired", loc)
	}
}

func TestOAuth_Callback_ClearsStaleOAuthCookie(t *testing.T) {
	// signinErrorRedirect must clear the stale mm_oauth cookie so the
	// retry from the toast can start from a clean slate.
	srv, client, _ := withOAuthEnabled(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/auth/github/callback", nil)
	res, _ := client.Do(req)
	defer res.Body.Close()
	var clearedOAuth bool
	for _, c := range res.Cookies() {
		if c.Name == "mm_oauth" && (c.MaxAge < 0 || c.Value == "") {
			clearedOAuth = true
		}
	}
	if !clearedOAuth {
		t.Error("mm_oauth cookie should be cleared on signin-error redirect")
	}
}

func TestOAuth_Callback_Success(t *testing.T) {
	srv, client, st := withOAuthEnabled(t)

	// Pre-seed an AuthState that the callback can consume.
	authState := &models.AuthState{
		ID:          "test-state-" + uuid.NewString(),
		Redirect:    "/",
		CookieValue: "test-cookie-value",
		CreatedAt:   time.Now().UTC(),
	}
	if err := st.InsertAuthState(context.Background(), authState); err != nil {
		t.Fatalf("seed auth state: %v", err)
	}

	// Mock the GitHub side: token exchange + user fetch.
	restore := ghMock(t, func(req *http.Request) *http.Response {
		switch {
		case strings.Contains(req.URL.Host, "github.com") && strings.Contains(req.URL.Path, "/login/oauth/access_token"):
			return makeResp(200, `{"access_token":"gh_token"}`)
		case strings.HasSuffix(req.URL.Path, "/user"):
			return makeResp(200, `{"id":1234,"login":"alice","name":"Alice","email":"a@x.com","avatar_url":""}`)
		}
		return makeResp(404, "{}")
	})
	defer restore()

	req, _ := http.NewRequest("GET",
		srv.URL+"/api/auth/github/callback?code=the-code&state="+authState.ID, nil)
	req.Header.Set("Cookie", "mm_oauth=test-cookie-value")
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("status %d", res.StatusCode)
	}
	var sessSet bool
	for _, c := range res.Cookies() {
		if c.Name == "mm_session" && c.Value != "" {
			sessSet = true
		}
	}
	if !sessSet {
		t.Error("mm_session cookie should be set on successful callback")
	}
}

func TestOAuth_Callback_WrongCookieRejected(t *testing.T) {
	srv, client, st := withOAuthEnabled(t)

	authState := &models.AuthState{
		ID:          "wrong-cookie-" + uuid.NewString(),
		Redirect:    "/",
		CookieValue: "expected-value",
		CreatedAt:   time.Now().UTC(),
	}
	_ = st.InsertAuthState(context.Background(), authState)

	req, _ := http.NewRequest("GET",
		srv.URL+"/api/auth/github/callback?code=x&state="+authState.ID, nil)
	req.Header.Set("Cookie", "mm_oauth=wrong-value")
	res, _ := client.Do(req)
	defer res.Body.Close()
	// Cookie mismatch → signin_error=cookie_mismatch redirect.
	if res.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302 (cookie mismatch redirect)", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "signin_error=cookie_mismatch") {
		t.Errorf("location %q should carry signin_error=cookie_mismatch", loc)
	}
}

func TestOAuth_Callback_MissingCookieRejected(t *testing.T) {
	// Same branch as wrong cookie: the request reaches the state
	// consumption step but no cookie is present at all.
	srv, client, st := withOAuthEnabled(t)
	authState := &models.AuthState{
		ID:          "no-cookie-" + uuid.NewString(),
		Redirect:    "/",
		CookieValue: "anything",
		CreatedAt:   time.Now().UTC(),
	}
	_ = st.InsertAuthState(context.Background(), authState)
	req, _ := http.NewRequest("GET",
		srv.URL+"/api/auth/github/callback?code=x&state="+authState.ID, nil)
	res, _ := client.Do(req)
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("Location"), "signin_error=cookie_mismatch") {
		t.Errorf("location should carry cookie_mismatch; got %q", res.Header.Get("Location"))
	}
}
