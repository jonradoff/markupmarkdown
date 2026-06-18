package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"markupmarkdown/internal/auth"
)

// writePushbackError translates an error from the GitHub round-trip
// surface into the structured response shape the SPA renders.

func TestWritePushbackError_WithFetchError_EmbedsStatus(t *testing.T) {
	a := &API{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/documents/x/pushback", nil)
	fe := &auth.FetchError{StatusCode: 422, Body: `{"message":"branch protection"}`}
	a.writePushbackError(rec, req, fe, "couldn't open PR")

	if rec.Code != 400 {
		t.Errorf("code=%d want 400", rec.Code)
	}
	var got fetchErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "github_422" {
		t.Errorf("kind=%q want github_422", got.Kind)
	}
	if got.Error != "couldn't open PR" {
		t.Errorf("error=%q want fallback", got.Error)
	}
	if !strings.Contains(got.Detail, "branch protection") {
		t.Errorf("detail should embed GitHub body, got %q", got.Detail)
	}
}

func TestWritePushbackError_FetchErrorZeroStatusDefaultsToBadGateway(t *testing.T) {
	// A FetchError that never landed an HTTP status (e.g. network
	// failure before the response was framed) should default the
	// embedded github_NNN kind to 502.
	a := &API{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/documents/x/pushback", nil)
	fe := &auth.FetchError{StatusCode: 0, Body: ""}
	a.writePushbackError(rec, req, fe, "could not commit")

	var got fetchErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Kind != "github_502" {
		t.Errorf("kind=%q want github_502 default", got.Kind)
	}
}

func TestWritePushbackError_GenericError_NoKindField(t *testing.T) {
	// Non-FetchError errors go through the fallback branch — no Kind
	// is stamped, just the error message as Detail.
	a := &API{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/documents/x/pushback", nil)
	a.writePushbackError(rec, req, errors.New("something else"), "fallback")

	if rec.Code != 400 {
		t.Errorf("code=%d want 400", rec.Code)
	}
	var got fetchErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Kind != "" {
		t.Errorf("kind=%q want empty for non-FetchError", got.Kind)
	}
	if got.Detail != "something else" {
		t.Errorf("detail=%q want pass-through of err.Error()", got.Detail)
	}
}
