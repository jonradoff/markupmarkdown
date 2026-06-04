package api_test

import (
	"strings"
	"testing"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestSecretsIntegration_GetEmpty(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "GET", "/api/me/anthropic-key", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"hasKey":false`) {
		t.Errorf("expected hasKey=false; got %s", body)
	}
	if !strings.Contains(string(body), `"enabled":true`) {
		t.Errorf("expected enabled=true (vault configured); got %s", body)
	}
}

func TestSecretsIntegration_PutRejectsTokenAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeAdmin)

	status, _ := doJSON(t, srv, "PUT", "/api/me/anthropic-key",
		map[string]string{"key": "sk-ant-fake"}, withBearer(plain))
	if status != 403 {
		t.Errorf("status=%d, want 403 (cookie-only)", status)
	}
}

func TestSecretsIntegration_DeleteRejectsTokenAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeAdmin)

	status, _ := doJSON(t, srv, "DELETE", "/api/me/anthropic-key", nil, withBearer(plain))
	if status != 403 {
		t.Errorf("status=%d, want 403 (cookie-only)", status)
	}
}

func TestSecretsIntegration_PutRequiresSignIn(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "PUT", "/api/me/anthropic-key",
		map[string]string{"key": "sk-ant-x"}, nil)
	if status != 401 {
		t.Errorf("status=%d, want 401", status)
	}
}

func TestSecretsIntegration_PutRejectsEmpty(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "PUT", "/api/me/anthropic-key",
		map[string]string{"key": "   "}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400 (empty key)", status)
	}
}

func TestSecretsIntegration_PutRejectsTooLong(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "PUT", "/api/me/anthropic-key",
		map[string]string{"key": strings.Repeat("a", 300)}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400 (too long)", status)
	}
}

// PUT/DELETE with a real Anthropic key requires hitting api.anthropic.com,
// which we don't want from CI. So we test the validation paths above and
// trust the store-level UpsertAnthropicKey integration test to cover the
// roundtrip.
