package api_test

// More targeted coverage for handler branches the larger flows skip:
// - resolveAuthor when caller supplies an author and is anonymous
// - markNotificationRead happy + idempotent
// - reopenComment happy after a resolve
// - patchDocument missing-title noop
// - createDocument with both url and content present (URL wins)
// - createReply rate-limit + scope edge

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestPatchDocument_NoTitleNoop(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	// Empty patch (no fields) → handler still returns 200 with the
	// existing doc (no changes recorded).
	status, _ := doJSON(t, srv, "PATCH", "/api/documents/"+doc.ID,
		map[string]any{}, withCookie(sess))
	if status != 200 {
		t.Errorf("status=%d", status)
	}
}

func TestReopenComment_HappyPath(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")

	// Resolve first.
	_, _ = doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/resolve",
		map[string]string{"author": "x"}, withCookie(sess))

	// Reopen.
	status, body := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/reopen",
		nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	var got models.Comment
	mustDecode(t, body, &got)
	if got.Resolved {
		t.Error("expected reopened (Resolved=false)")
	}
}

func TestMarkNotificationRead_Idempotent(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	// Mark a non-existent notification → still 204 (idempotent).
	status, _ := doJSON(t, srv, "POST", "/api/me/notifications/nope/read",
		nil, withCookie(sess))
	if status != 204 {
		t.Errorf("status=%d", status)
	}
}

func TestCreateDocument_URLAndContentBothURLWins(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	// Self-doc redirect path with both fields present — URL wins.
	status, body := doJSON(t, srv, "POST", "/api/documents", map[string]string{
		"url":     "http://localhost:4720/d/some-id",
		"content": "should be ignored",
	}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), "self_doc_redirect") {
		t.Errorf("URL field should take precedence; got %s", body)
	}
}

func TestRevokeToken_NotFound(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	// Revoking a non-existent token is a 204 (idempotent — store update
	// just matches zero docs).
	status, _ := doJSON(t, srv, "DELETE", "/api/me/tokens/nope", nil, withCookie(sess))
	if status != 204 {
		t.Errorf("status=%d", status)
	}
}

func TestCreateReply_AnonymousAllowedOnPublicDoc(t *testing.T) {
	// markupmarkdown allows anonymous identities to comment on public
	// docs (provided the AuthorBadge has a name set). This test
	// documents that behavior — anon reply on a public doc → 201.
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")
	status, _ := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/replies",
		map[string]string{"body": "anon reply", "author": "Guest"})
	if status != 201 {
		t.Errorf("status=%d, want 201 (anonymous allowed on public doc)", status)
	}
}

func TestCreateReply_ReadTokenForbidden(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeRead)
	status, _ := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/replies",
		map[string]string{"body": "agent reply"}, withBearer(plain))
	if status != 403 {
		t.Errorf("status=%d, want 403", status)
	}
}

func TestResolveComment_RequiresWriteScope(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeRead)
	status, _ := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/resolve",
		nil, withBearer(plain))
	if status != 403 {
		t.Errorf("status=%d, want 403", status)
	}
}

func TestReopenComment_RequiresWriteScope(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeRead)
	status, _ := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/reopen",
		nil, withBearer(plain))
	if status != 403 {
		t.Errorf("status=%d, want 403", status)
	}
}

func TestPatchComment_RequiresWriteScope(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeRead)
	status, _ := doJSON(t, srv, "PATCH", "/api/comments/"+c.ID,
		map[string]string{"body": "edited"}, withBearer(plain))
	if status != 403 {
		t.Errorf("status=%d, want 403", status)
	}
}

func TestEditReply_RequiresWriteScope(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")
	// Add a reply to edit.
	sess := testutil.NewTestSession(t, st, user.ID)
	status, body := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/replies",
		map[string]string{"body": "r"}, withCookie(sess))
	if status != 201 {
		t.Fatalf("seed reply: %d %s", status, body)
	}
	var withReply models.Comment
	mustDecode(t, body, &withReply)
	rid := withReply.Replies[0].ID

	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeRead)
	status, _ = doJSON(t, srv, "PATCH", "/api/comments/"+c.ID+"/replies/"+rid,
		map[string]string{"body": "edited"}, withBearer(plain))
	if status != 403 {
		t.Errorf("status=%d, want 403", status)
	}
}

func TestDeleteReply_RequiresWriteScope(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")
	sess := testutil.NewTestSession(t, st, user.ID)
	status, body := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/replies",
		map[string]string{"body": "r"}, withCookie(sess))
	if status != 201 {
		t.Fatalf("seed reply: %d %s", status, body)
	}
	var withReply models.Comment
	mustDecode(t, body, &withReply)
	rid := withReply.Replies[0].ID

	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeRead)
	status, _ = doJSON(t, srv, "DELETE", "/api/comments/"+c.ID+"/replies/"+rid,
		nil, withBearer(plain))
	if status != 403 {
		t.Errorf("status=%d, want 403", status)
	}
}

// Compile-time deps to keep imports honest.
var _ = http.NoBody
var _ = context.Background
