package api_test

import (
	"strings"
	"testing"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestTokensIntegration_RequireSignIn(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "GET", "/api/me/tokens", nil)
	if status != 401 {
		t.Fatalf("status=%d, want 401", status)
	}
}

func TestTokensIntegration_CreateListRenameRevoke(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	// Empty list.
	status, body := doJSON(t, srv, "GET", "/api/me/tokens", nil, withCookie(sess))
	if status != 200 || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("empty list: %d %s", status, body)
	}

	// Create.
	status, body = doJSON(t, srv, "POST", "/api/me/tokens", map[string]any{
		"label":         "claude-curl",
		"scope":         "write",
		"expiresInDays": 90,
	}, withCookie(sess))
	if status != 201 {
		t.Fatalf("create: %d %s", status, body)
	}
	var created struct {
		Token    string          `json:"token"`
		Metadata models.APIToken `json:"metadata"`
	}
	mustDecode(t, body, &created)
	if !strings.HasPrefix(created.Token, "mmk_") {
		t.Errorf("plaintext bad: %q", created.Token)
	}
	if created.Metadata.Scope != models.TokenScopeWrite {
		t.Errorf("scope = %q", created.Metadata.Scope)
	}
	if created.Metadata.ExpiresAt == nil {
		t.Error("expiresAt should be set")
	}

	// Rename + scope change.
	id := created.Metadata.ID
	status, _ = doJSON(t, srv, "PATCH", "/api/me/tokens/"+id, map[string]any{
		"label": "renamed",
		"scope": "read",
	}, withCookie(sess))
	if status != 204 {
		t.Fatalf("rename: %d", status)
	}

	// List shows the rename.
	status, body = doJSON(t, srv, "GET", "/api/me/tokens", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("list: %d", status)
	}
	var list []models.APIToken
	mustDecode(t, body, &list)
	if len(list) != 1 || list[0].Label != "renamed" || list[0].Scope != models.TokenScopeRead {
		t.Fatalf("list = %+v", list)
	}

	// Revoke.
	status, _ = doJSON(t, srv, "DELETE", "/api/me/tokens/"+id, nil, withCookie(sess))
	if status != 204 {
		t.Fatalf("revoke: %d", status)
	}

	// List now empty.
	status, body = doJSON(t, srv, "GET", "/api/me/tokens", nil, withCookie(sess))
	if status != 200 || strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("list after revoke: %d %s", status, body)
	}
}

func TestTokensIntegration_CreateRejectsTokenAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeAdmin)

	status, _ := doJSON(t, srv, "POST", "/api/me/tokens", map[string]string{
		"label": "minted-via-token",
	}, withBearer(plain))
	if status != 403 {
		t.Errorf("status=%d, want 403 (cookie-only)", status)
	}
}

func TestTokensIntegration_RenameRejectsTokenAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)
	plain2, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeAdmin)

	status, _ := doJSON(t, srv, "PATCH", "/api/me/tokens/"+rec.ID,
		map[string]string{"label": "hack"}, withBearer(plain2))
	if status != 403 {
		t.Errorf("status=%d, want 403 (cookie-only)", status)
	}
}

func TestTokensIntegration_RejectsBadScope(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "POST", "/api/me/tokens", map[string]string{
		"label": "x", "scope": "god",
	}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}
}

func TestTokensIntegration_LabelTooLong(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "POST", "/api/me/tokens", map[string]string{
		"label": strings.Repeat("a", 200),
	}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}
}

func TestTokensIntegration_PatchNoOpRename(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)

	// Patch with the same label should still 204 cleanly.
	status, _ := doJSON(t, srv, "PATCH", "/api/me/tokens/"+rec.ID,
		map[string]string{"label": rec.Label}, withCookie(sess))
	if status != 204 {
		t.Errorf("no-op rename status=%d", status)
	}
}

func TestTokensIntegration_PatchMissingFields(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)

	status, _ := doJSON(t, srv, "PATCH", "/api/me/tokens/"+rec.ID, map[string]any{}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}
}

func TestTokensIntegration_Activity(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)

	status, body := doJSON(t, srv, "GET", "/api/me/tokens/"+rec.ID+"/activity", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("expected empty array; got %s", body)
	}
}

func TestTokensIntegration_ActivityNotFound(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "GET", "/api/me/tokens/does-not-exist/activity", nil, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d, want 404", status)
	}
}

func TestTokensIntegration_CreateDefaultsScope(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "POST", "/api/me/tokens",
		map[string]string{"label": "no-scope"}, withCookie(sess))
	if status != 201 {
		t.Fatalf("create: %d %s", status, body)
	}
	var created struct {
		Metadata models.APIToken `json:"metadata"`
	}
	mustDecode(t, body, &created)
	if created.Metadata.Scope != models.TokenScopeWrite {
		t.Errorf("default scope = %q, want write", created.Metadata.Scope)
	}
}

func TestTokensIntegration_CreateExpiresNever(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "POST", "/api/me/tokens", map[string]any{
		"label":         "forever",
		"expiresInDays": -1,
	}, withCookie(sess))
	if status != 201 {
		t.Fatalf("create: %d %s", status, body)
	}
	var created struct {
		Metadata models.APIToken `json:"metadata"`
	}
	mustDecode(t, body, &created)
	if created.Metadata.ExpiresAt != nil {
		t.Errorf("expiresAt should be nil for never-expires; got %v", created.Metadata.ExpiresAt)
	}
}
