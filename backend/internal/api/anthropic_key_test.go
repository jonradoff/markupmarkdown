package api_test

// Tests exercising the Anthropic-key happy + sad paths. We need the
// vault wired up; the test config supplies a hex master key. We mock the
// outbound HTTP to anthropic.com so ValidateAPIKey returns 200 without
// burning a real account.

import (
	"net/http"
	"strings"
	"testing"

	"markupmarkdown/internal/testutil"
)

func TestAnthropicKey_PutValidateAndRoundtrip(t *testing.T) {
	// Mock the Anthropic API: any POST to /v1/messages returns a valid
	// response so ValidateAPIKey succeeds.
	restore := ghMock(t, func(req *http.Request) *http.Response {
		return makeResp(200, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-opus-4-7","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	t.Cleanup(restore)

	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	// Put a key. (Anthropic SDK retries — it might take longer than a
	// single round trip, but our mock always returns 200.)
	status, body := doJSON(t, srv, "PUT", "/api/me/anthropic-key",
		map[string]string{"key": "sk-ant-fake-1234"}, withCookie(sess))
	if status != 200 {
		t.Fatalf("put: status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"hasKey":true`) {
		t.Errorf("expected hasKey=true; got %s", body)
	}

	// Delete it.
	status, _ = doJSON(t, srv, "DELETE", "/api/me/anthropic-key", nil, withCookie(sess))
	if status != 204 {
		t.Fatalf("delete: %d", status)
	}

	// Now get is back to hasKey=false.
	status, body = doJSON(t, srv, "GET", "/api/me/anthropic-key", nil, withCookie(sess))
	if status != 200 || !strings.Contains(string(body), `"hasKey":false`) {
		t.Fatalf("after delete: %d %s", status, body)
	}
}

func TestAnthropicKey_PutInvalidKey(t *testing.T) {
	// Mock Anthropic to 401 → ValidateAPIKey errors out.
	restore := ghMock(t, func(req *http.Request) *http.Response {
		return makeResp(401, `{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`)
	})
	t.Cleanup(restore)

	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "PUT", "/api/me/anthropic-key",
		map[string]string{"key": "sk-ant-rejected"}, withCookie(sess))
	if status != 400 {
		t.Fatalf("status=%d body=%s, want 400", status, body)
	}
}

func TestPreviewRevisionRequiresResolvedComments(t *testing.T) {
	// Stub Anthropic so the key validation in ValidateAPIKey passes when
	// the user first sets their key.
	restore := ghMock(t, func(req *http.Request) *http.Response {
		return makeResp(200, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-opus-4-7","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	t.Cleanup(restore)

	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	// Set a key so we get past the precondition.
	status, _ := doJSON(t, srv, "PUT", "/api/me/anthropic-key",
		map[string]string{"key": "sk-ant-fake"}, withCookie(sess))
	if status != 200 {
		t.Fatalf("put key: %d", status)
	}

	doc := testutil.NewTestDocument(t, st, user.ID, "Hi")
	// No resolved comments → 400.
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/revise",
		map[string]any{}, withCookie(sess))
	if status != 400 {
		t.Fatalf("status=%d body=%s, want 400 (no resolved)", status, body)
	}
	if !strings.Contains(string(body), "no_resolved_comments") {
		t.Errorf("expected kind=no_resolved_comments; got %s", body)
	}
}
