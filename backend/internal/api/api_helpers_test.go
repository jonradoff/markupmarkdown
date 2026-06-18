package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Trivial helpers (writeJSON / writeError / readJSON / health) are
// 100% pure: no DB, no auth. They're the canary that the HTTP
// scaffolding behaves as advertised — and the cheapest possible
// coverage win for internal/api.

func TestWriteJSON_EncodesBodyAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusTeapot, map[string]string{"hello": "world"})
	if rec.Code != http.StatusTeapot {
		t.Errorf("code=%d want 418", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type=%q want application/json", ct)
	}
	if !strings.Contains(rec.Body.String(), `"hello":"world"`) {
		t.Errorf("body missing field: %s", rec.Body.String())
	}
}

func TestWriteJSON_NilBodyOmitsPayload(t *testing.T) {
	// The nil-body branch is the "headers-only" path used by 204
	// responses elsewhere.
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusNoContent, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("code=%d want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body, got %q", rec.Body.String())
	}
}

func TestWriteError_WrapsMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "nope")
	if rec.Code != 400 {
		t.Errorf("code=%d want 400", rec.Code)
	}
	var payload map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["error"] != "nope" {
		t.Errorf("error field=%q want nope", payload["error"])
	}
}

func TestReadJSON_DecodesBody(t *testing.T) {
	body := bytes.NewBufferString(`{"name":"jon"}`)
	req := httptest.NewRequest("POST", "/x", body)
	var dst struct {
		Name string `json:"name"`
	}
	if err := readJSON(req, &dst); err != nil {
		t.Fatalf("readJSON: %v", err)
	}
	if dst.Name != "jon" {
		t.Errorf("name=%q want jon", dst.Name)
	}
}

func TestReadJSON_RejectsUnknownFields(t *testing.T) {
	// DisallowUnknownFields is a soft API contract — readers shouldn't
	// silently accept unrecognized keys. Regression guard.
	body := bytes.NewBufferString(`{"name":"jon","sneaky":true}`)
	req := httptest.NewRequest("POST", "/x", body)
	var dst struct {
		Name string `json:"name"`
	}
	if err := readJSON(req, &dst); err == nil {
		t.Errorf("expected error for unknown field, got nil")
	}
}

func TestHealth_ReturnsOK(t *testing.T) {
	a := &API{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	a.health(rec, req)
	if rec.Code != 200 {
		t.Errorf("code=%d want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("body missing status field: %s", rec.Body.String())
	}
}
