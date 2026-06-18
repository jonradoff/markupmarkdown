package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"markupmarkdown/internal/config"
)

// writeAccessError builds the structured error payload the SPA renders
// for a denied document fetch. Four kinds, three of them have distinct
// action lists; that's the branching surface.

func decodeAccessErr(t *testing.T, body []byte) fetchErrorResponse {
	t.Helper()
	var r fetchErrorResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return r
}

func TestWriteAccessError_NotFound(t *testing.T) {
	a := &API{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/documents/x", nil)
	a.writeAccessError(rec, req, &accessErr{Status: http.StatusNotFound, Kind: accessKindNotFound})

	if rec.Code != 404 {
		t.Errorf("code=%d want 404", rec.Code)
	}
	got := decodeAccessErr(t, rec.Body.Bytes())
	if got.Kind != accessKindNotFound {
		t.Errorf("kind=%q want %q", got.Kind, accessKindNotFound)
	}
	if got.Error != "Document not found." {
		t.Errorf("error=%q want fixed not-found message", got.Error)
	}
	if len(got.Actions) != 0 {
		t.Errorf("not-found should have no actions, got %+v", got.Actions)
	}
}

func TestWriteAccessError_SignInRequired_UsesRefererForRedirect(t *testing.T) {
	// The sign-in action should redirect back to whatever path the
	// user was viewing (not the API path that triggered the 401).
	a := &API{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/documents/x", nil)
	req.Header.Set("Referer", "https://mumd.metavert.io/anthropics/claude-code")
	a.writeAccessError(rec, req, &accessErr{
		Status: http.StatusUnauthorized,
		Kind:   accessKindSignInRequired,
	})

	if rec.Code != 401 {
		t.Errorf("code=%d want 401", rec.Code)
	}
	got := decodeAccessErr(t, rec.Body.Bytes())
	if len(got.Actions) != 1 {
		t.Fatalf("expected 1 sign-in action, got %d", len(got.Actions))
	}
	if !strings.Contains(got.Actions[0].URL, "redirect=") {
		t.Errorf("expected redirect query in sign-in URL, got %q", got.Actions[0].URL)
	}
	// The path /anthropics/claude-code should be url-escaped in the redirect.
	if !strings.Contains(got.Actions[0].URL, "%2Fanthropics%2Fclaude-code") {
		t.Errorf("expected escaped referrer path in sign-in URL, got %q", got.Actions[0].URL)
	}
}

func TestWriteAccessError_SignInRequired_FallsBackToRootWhenRefererBlank(t *testing.T) {
	a := &API{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/documents/x", nil)
	// No Referer header set.
	a.writeAccessError(rec, req, &accessErr{
		Status: http.StatusUnauthorized,
		Kind:   accessKindSignInRequired,
	})
	got := decodeAccessErr(t, rec.Body.Bytes())
	if len(got.Actions) != 1 {
		t.Fatalf("expected 1 sign-in action, got %d", len(got.Actions))
	}
	// redirect parameter should encode "/" — i.e. %2F.
	if !strings.Contains(got.Actions[0].URL, "redirect=%2F") {
		t.Errorf("expected redirect=%%2F (root) in URL, got %q", got.Actions[0].URL)
	}
}

func TestWriteAccessError_NoGitHubAccess_NoClientID_NoOAuthAction(t *testing.T) {
	// When ClientID is unset, the "Manage GitHub access" action must
	// not be emitted (avoid a broken settings link).
	a := &API{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/documents/x", nil)
	a.writeAccessError(rec, req, &accessErr{
		Status: http.StatusForbidden,
		Kind:   accessKindNoGitHubAccess,
		Owner:  "anthropics",
		Repo:   "claude-code",
	})

	if rec.Code != 403 {
		t.Errorf("code=%d want 403", rec.Code)
	}
	got := decodeAccessErr(t, rec.Body.Bytes())
	if !strings.Contains(got.Error, "anthropics") {
		t.Errorf("error message should name owner, got %q", got.Error)
	}
	// Only the "Open …/… on GitHub" action — no settings link.
	if len(got.Actions) != 1 {
		t.Fatalf("expected 1 action (just open-on-github), got %+v", got.Actions)
	}
	if got.Actions[0].URL != "https://github.com/anthropics/claude-code" {
		t.Errorf("open-on-github URL=%q wrong", got.Actions[0].URL)
	}
}

func TestWriteAccessError_NoGitHubAccess_WithClientID_BothActions(t *testing.T) {
	a := &API{cfg: &config.Config{GitHub: config.GitHubConfig{ClientID: "fake-client-id"}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/documents/x", nil)
	a.writeAccessError(rec, req, &accessErr{
		Status: http.StatusForbidden,
		Kind:   accessKindNoGitHubAccess,
		Owner:  "anthropics",
		Repo:   "claude-code",
	})
	got := decodeAccessErr(t, rec.Body.Bytes())
	if len(got.Actions) != 2 {
		t.Fatalf("expected 2 actions (settings + open-on-github), got %d: %+v", len(got.Actions), got.Actions)
	}
	if !strings.Contains(got.Actions[0].URL, "fake-client-id") {
		t.Errorf("first action URL should embed client_id, got %q", got.Actions[0].URL)
	}
}

func TestWriteAccessError_NoGitHubAccess_EmptyOwnerRepoOmitsOpenAction(t *testing.T) {
	// When the owner/repo aren't known (e.g. legacy doc with no
	// github metadata), the "Open …/… on GitHub" action is skipped.
	a := &API{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/documents/x", nil)
	a.writeAccessError(rec, req, &accessErr{
		Status: http.StatusForbidden,
		Kind:   accessKindNoGitHubAccess,
		// Owner / Repo intentionally empty.
	})
	got := decodeAccessErr(t, rec.Body.Bytes())
	if len(got.Actions) != 0 {
		t.Errorf("expected zero actions for owner/repo-less denial, got %+v", got.Actions)
	}
}

func TestWriteAccessError_UnknownKindFallsBackGenerically(t *testing.T) {
	a := &API{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/documents/x", nil)
	a.writeAccessError(rec, req, &accessErr{
		Status: http.StatusInternalServerError,
		Kind:   "server_error",
	})
	if rec.Code != 500 {
		t.Errorf("code=%d want 500", rec.Code)
	}
	got := decodeAccessErr(t, rec.Body.Bytes())
	if !strings.Contains(got.Error, "Couldn't load") {
		t.Errorf("expected generic fallback message, got %q", got.Error)
	}
}
