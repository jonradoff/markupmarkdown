package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestRate429_WritesRetryAfter(t *testing.T) {
	w := httptest.NewRecorder()
	rate429(w, "slow down")
	if w.Code != 429 {
		t.Fatalf("status %d", w.Code)
	}
	if w.Header().Get("Retry-After") != "60" {
		t.Errorf("missing Retry-After header")
	}
	var body fetchErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Kind != "rate_limited" {
		t.Errorf("kind=%q", body.Kind)
	}
}

func TestInternalError_Sanitizes(t *testing.T) {
	w := httptest.NewRecorder()
	internalError(w, "test.where", errors.New("internal mongo blip"))
	if w.Code != 500 {
		t.Fatalf("status %d", w.Code)
	}
	var body struct {
		Error string `json:"error"`
		ID    string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.ID == "" {
		t.Error("missing ID for support correlation")
	}
	if body.Error == "" {
		t.Error("missing user-facing error message")
	}
	// Should NOT echo the internal detail.
	if string(w.Body.Bytes()) == "" || (body.Error != "" && contains(splitWords(body.Error), "mongo")) {
		t.Errorf("internal detail leaked: %s", w.Body.Bytes())
	}
}

func splitWords(s string) []string {
	var out []string
	cur := ""
	for _, ch := range s {
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == ',' || ch == '.' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
		} else {
			cur += string(ch)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
