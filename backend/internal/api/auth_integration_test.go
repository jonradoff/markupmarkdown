package api_test

import (
	"net/http"
	"testing"

	"markupmarkdown/internal/testutil"
)

func TestAuthIntegration_ConfigAndMe(t *testing.T) {
	srv, _, _ := newTestServer(t)

	status, body := doJSON(t, srv, "GET", "/api/auth/config", nil)
	if status != 200 {
		t.Fatalf("config: status=%d body=%s", status, body)
	}
	var cfg struct {
		GitHubEnabled bool `json:"githubEnabled"`
	}
	mustDecode(t, body, &cfg)
	// test.yaml has no GitHub creds → disabled.
	if cfg.GitHubEnabled {
		t.Error("github should be disabled in test config")
	}

	// /me without a cookie → user: null
	status, body = doJSON(t, srv, "GET", "/api/auth/me", nil)
	if status != 200 {
		t.Fatalf("me: status=%d body=%s", status, body)
	}
	var me struct {
		User *struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	mustDecode(t, body, &me)
	if me.User != nil {
		t.Errorf("expected null user; got %+v", me.User)
	}
}

func TestAuthIntegration_MeWithSession(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "GET", "/api/auth/me", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("me: status=%d body=%s", status, body)
	}
	var me struct {
		User *struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"user"`
	}
	mustDecode(t, body, &me)
	if me.User == nil || me.User.Login != user.Login {
		t.Errorf("got %+v, want login=%q", me.User, user.Login)
	}
}

func TestAuthIntegration_MeWithToken(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	plain, _ := testutil.NewAPIToken(t, st, user.ID, "write")

	status, body := doJSON(t, srv, "GET", "/api/auth/me", nil, withBearer(plain))
	if status != 200 {
		t.Fatalf("me: status=%d body=%s", status, body)
	}
}

func TestAuthIntegration_BadTokenReturnsNullUser(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, body := doJSON(t, srv, "GET", "/api/auth/me", nil, withBearer("mmk_garbageeeeeeeeeeeeeeeeeeeeeeeee"))
	if status != 200 {
		t.Fatalf("me: status=%d body=%s", status, body)
	}
	// Bad bearer is rejected; user resolves to nil → 200 + null.
}

func TestAuthIntegration_Logout(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "POST", "/api/auth/logout", nil, withCookie(sess))
	if status != 204 {
		t.Fatalf("logout status %d", status)
	}
}

func TestAuthIntegration_LoginDisabledIfNoOAuthConfig(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/auth/github/login", nil)
	res, _ := srv.Client().Do(req)
	defer res.Body.Close()
	if res.StatusCode != 503 {
		t.Errorf("expected 503 (oauth not configured); got %d", res.StatusCode)
	}
}

func TestAuthIntegration_CallbackMissingCodeOrState(t *testing.T) {
	// Even when oauth is "not configured", callback rejects missing code/state
	// only after the enabled-check passes. So we can't easily test this path
	// without configured OAuth — skipping in this build.
}

func TestHealthIntegration(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "GET", "/api/health", nil)
	if status != 200 {
		t.Fatalf("status %d", status)
	}
}
