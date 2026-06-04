package api_test

import (
	"context"
	"strings"
	"testing"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

// Quick coverage-focused tests for branches the larger suites don't
// reach. Each one targets a specific log path.

func TestMentionCandidates_IncludesPriorAuthors(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, owner.ID)
	doc := testutil.NewTestDocument(t, st, owner.ID, "")
	// Another user comments on the doc — they should appear in the
	// mention candidates list.
	commenter := testutil.NewTestUser(t, st)
	commenter.Login = "carol-" + commenter.ID[:6]
	_ = st.UpsertUserByGitHubID(context.Background(), commenter)
	_ = testutil.NewTestComment(t, st, doc.ID, commenter.ID, "Hello", "x")

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/mention-candidates",
		nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	// Owner + commenter should both appear.
	if !strings.Contains(string(body), owner.Login) {
		t.Errorf("owner missing: %s", body)
	}
	if !strings.Contains(string(body), commenter.Login) {
		t.Errorf("commenter missing: %s", body)
	}
}

func TestAnthropicKey_DeleteWhenNoneSet(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	// Delete with no prior key is a no-op 204.
	status, _ := doJSON(t, srv, "DELETE", "/api/me/anthropic-key", nil, withCookie(sess))
	if status != 204 {
		t.Errorf("status=%d", status)
	}
}

func TestTokens_RenameRejectsEmpty(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)

	status, _ := doJSON(t, srv, "PATCH", "/api/me/tokens/"+rec.ID,
		map[string]string{"label": "   "}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}
}

func TestTokens_RenameNotFound(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "PATCH", "/api/me/tokens/nope",
		map[string]string{"label": "x"}, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d, want 404", status)
	}
}

func TestTokens_ListWhenNone(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "GET", "/api/me/tokens", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	if strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("got %s", body)
	}
}

func TestRestore_NotInTrash(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "POST", "/api/documents/no-such/restore", nil, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d, want 404", status)
	}
}

func TestNotifications_LimitParam(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	// limit query param parsed.
	status, _ := doJSON(t, srv, "GET", "/api/me/notifications?limit=5", nil, withCookie(sess))
	if status != 200 {
		t.Errorf("status=%d", status)
	}
}

func TestComments_PatchNoChanges(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")

	// Empty patch → 400.
	status, _ := doJSON(t, srv, "PATCH", "/api/comments/"+c.ID, map[string]any{}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}
}

func TestComments_PatchEmptyBody(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")

	status, _ := doJSON(t, srv, "PATCH", "/api/comments/"+c.ID,
		map[string]string{"body": "   "}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}
}
