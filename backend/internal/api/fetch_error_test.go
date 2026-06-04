package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/config"
)

func newGitHubAPI() *API {
	return &API{cfg: &config.Config{GitHub: config.GitHubConfig{ClientID: "abc"}}}
}

func TestWriteFetchError_Generic(t *testing.T) {
	a := &API{cfg: &config.Config{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/documents", nil)
	a.writeFetchError(w, r, "https://example.com/x.md", errors.New("dial fail"))
	if w.Code != 400 {
		t.Fatalf("status %d", w.Code)
	}
	var body fetchErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Kind != "fetch_other" {
		t.Errorf("kind = %q", body.Kind)
	}
}

func TestWriteFetchError_GitHubSSO(t *testing.T) {
	a := newGitHubAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/documents", nil)
	a.writeFetchError(w, r, "https://github.com/foo/bar/blob/main/X.md", &auth.FetchError{
		StatusCode: 403,
		SSOURL:     "https://example.com/sso",
	})
	var body fetchErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Kind != "github_sso" {
		t.Errorf("kind = %q", body.Kind)
	}
	if len(body.Actions) == 0 {
		t.Error("SSO should attach an action")
	}
}

func TestWriteFetchError_GitHubAuthExpired(t *testing.T) {
	a := newGitHubAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/documents", nil)
	a.writeFetchError(w, r, "https://github.com/foo/bar/blob/main/X.md", &auth.FetchError{
		StatusCode: 401,
	})
	var body fetchErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Kind != "github_auth" {
		t.Errorf("kind = %q", body.Kind)
	}
}

func TestWriteFetchError_GitHubAnonymousGetsSignInPrompt(t *testing.T) {
	a := newGitHubAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/documents", nil)
	a.writeFetchError(w, r, "https://github.com/foo/bar/blob/main/X.md", &auth.FetchError{
		StatusCode: 404,
	})
	var body fetchErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Kind != "github_auth" {
		t.Errorf("kind = %q", body.Kind)
	}
}

func TestWriteFetchError_OtherStatusCode(t *testing.T) {
	a := newGitHubAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/documents", nil)
	a.writeFetchError(w, r, "https://github.com/foo/bar/blob/main/X.md", &auth.FetchError{
		StatusCode: 500,
		Body:       "internal",
	})
	var body fetchErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if !strings.Contains(body.Error, "500") {
		t.Errorf("missing status code in error: %q", body.Error)
	}
}
