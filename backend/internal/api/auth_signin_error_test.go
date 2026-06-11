package api

// Unit tests for signinErrorRedirect — the friendly OAuth-error
// recovery branch. Pure HTTP wire; no DB needed.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"markupmarkdown/internal/config"
)

func TestSigninErrorRedirect_FormatsLocation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Frontend.URL = "https://mumd.test"
	a := &API{cfg: cfg}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback", nil)
	a.signinErrorRedirect(w, r, "expired")

	if w.Code != http.StatusFound {
		t.Fatalf("status %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://mumd.test/?signin_error=") {
		t.Errorf("location wrong shape: %q", loc)
	}
	if !strings.Contains(loc, "expired") {
		t.Errorf("reason not embedded: %q", loc)
	}
}

func TestSigninErrorRedirect_URLEncodesReason(t *testing.T) {
	cfg := &config.Config{}
	cfg.Frontend.URL = "https://mumd.test"
	a := &API{cfg: cfg}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback", nil)
	a.signinErrorRedirect(w, r, "spaced reason & special=chars")

	loc := w.Header().Get("Location")
	if strings.Contains(loc, "& special=chars") {
		t.Errorf("reason not URL-encoded: %q", loc)
	}
}

func TestSigninErrorRedirect_ClearsOAuthCookie(t *testing.T) {
	cfg := &config.Config{}
	cfg.Frontend.URL = "https://mumd.test"
	a := &API{cfg: cfg}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback", nil)
	a.signinErrorRedirect(w, r, "cookie_mismatch")

	// Look for the Set-Cookie clearing mm_oauth.
	cookies := w.Result().Cookies()
	var cleared bool
	for _, c := range cookies {
		if c.Name == "mm_oauth" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("mm_oauth should be cleared (MaxAge<0); got cookies=%+v", cookies)
	}
}

func TestSigninErrorRedirect_SecureFlagFollowsFrontendScheme(t *testing.T) {
	for _, tc := range []struct {
		scheme   string
		wantSec  bool
	}{
		{"https://mumd.test", true},
		{"http://localhost:4720", false},
	} {
		cfg := &config.Config{}
		cfg.Frontend.URL = tc.scheme
		a := &API{cfg: cfg}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback", nil)
		a.signinErrorRedirect(w, r, "x")
		var got *http.Cookie
		for _, c := range w.Result().Cookies() {
			if c.Name == "mm_oauth" {
				got = c
			}
		}
		if got == nil {
			t.Errorf("scheme=%q: no mm_oauth cookie cleared", tc.scheme)
			continue
		}
		if got.Secure != tc.wantSec {
			t.Errorf("scheme=%q: Secure=%v, want %v", tc.scheme, got.Secure, tc.wantSec)
		}
	}
}
