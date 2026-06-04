package api_test

import (
	"context"
	"strings"
	"testing"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestCommentsIntegration_ListEmpty(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/comments", nil)
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if string(body) != "[]\n" {
		t.Errorf("got %s", body)
	}
}

func TestCommentsIntegration_CreateAndList(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/comments", map[string]any{
		"anchor": map[string]any{"start": 0, "end": 5, "exact": "Hello"},
		"body":   "Initial comment",
	}, withCookie(sess))
	if status != 201 {
		t.Fatalf("create: status=%d body=%s", status, body)
	}
	var c models.Comment
	mustDecode(t, body, &c)
	if c.Body != "Initial comment" {
		t.Errorf("got %q", c.Body)
	}
	if c.ActorKind != models.ActorHuman {
		t.Errorf("kind = %q", c.ActorKind)
	}
	if !c.Mine {
		t.Error("comment should be Mine for its author")
	}

	// List.
	status, body = doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/comments", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("list: status=%d", status)
	}
	var list []models.Comment
	mustDecode(t, body, &list)
	if len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("list = %+v", list)
	}
}

func TestCommentsIntegration_RejectsBadAnchor(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	// Anchor with end <= start.
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/comments", map[string]any{
		"anchor": map[string]any{"start": 5, "end": 5, "exact": "x"},
		"body":   "x",
	}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}

	// Empty body.
	status, _ = doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/comments", map[string]any{
		"anchor": map[string]any{"start": 0, "end": 5, "exact": "x"},
		"body":   "   ",
	}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}
}

func TestCommentsIntegration_ResolveAndReopen(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")

	status, body := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/resolve", map[string]string{"author": user.Name}, withCookie(sess))
	if status != 200 {
		t.Fatalf("resolve: status=%d body=%s", status, body)
	}
	var got models.Comment
	mustDecode(t, body, &got)
	if !got.Resolved {
		t.Error("should be resolved")
	}

	status, body = doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/reopen", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("reopen: status=%d body=%s", status, body)
	}
	mustDecode(t, body, &got)
	if got.Resolved {
		t.Error("should be reopened")
	}
}

func TestCommentsIntegration_PatchOnlyMine(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	other := testutil.NewTestUser(t, st)
	otherSess := testutil.NewTestSession(t, st, other.ID)
	doc := testutil.NewTestDocument(t, st, owner.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, owner.ID, "", "")

	// Other user CANNOT edit owner's comment.
	status, _ := doJSON(t, srv, "PATCH", "/api/comments/"+c.ID,
		map[string]string{"body": "intruded"}, withCookie(otherSess))
	if status != 403 {
		t.Errorf("non-author edit: status=%d, want 403", status)
	}

	// Author CAN.
	ownerSess := testutil.NewTestSession(t, st, owner.ID)
	status, _ = doJSON(t, srv, "PATCH", "/api/comments/"+c.ID,
		map[string]string{"body": "self-edit"}, withCookie(ownerSess))
	if status != 200 {
		t.Errorf("author edit: status=%d", status)
	}
}

func TestCommentsIntegration_DeleteOnlyMine(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	ownerSess := testutil.NewTestSession(t, st, owner.ID)
	other := testutil.NewTestUser(t, st)
	otherSess := testutil.NewTestSession(t, st, other.ID)
	doc := testutil.NewTestDocument(t, st, owner.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, owner.ID, "", "")

	// Non-author rejected.
	status, _ := doJSON(t, srv, "DELETE", "/api/comments/"+c.ID, nil, withCookie(otherSess))
	if status != 403 {
		t.Errorf("non-author delete: status=%d, want 403", status)
	}

	// Author succeeds.
	status, _ = doJSON(t, srv, "DELETE", "/api/comments/"+c.ID, nil, withCookie(ownerSess))
	if status != 204 {
		t.Errorf("author delete: status=%d", status)
	}
}

func TestCommentsIntegration_BotOwnerCanDelete(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	ownerSess := testutil.NewTestSession(t, st, owner.ID)
	doc := testutil.NewTestDocument(t, st, owner.ID, "")

	// Bot creates a comment via token.
	plain, _ := testutil.NewAPIToken(t, st, owner.ID, models.TokenScopeWrite)
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/comments", map[string]any{
		"anchor": map[string]any{"start": 0, "end": 5, "exact": "Hello"},
		"body":   "from the bot",
	}, withBearer(plain))
	if status != 201 {
		t.Fatalf("bot create: %d %s", status, body)
	}
	var c models.Comment
	mustDecode(t, body, &c)
	if c.ActorKind != models.ActorAgent {
		t.Errorf("kind = %q, want agent", c.ActorKind)
	}

	// Owner can delete via the cookie session.
	status, _ = doJSON(t, srv, "DELETE", "/api/comments/"+c.ID, nil, withCookie(ownerSess))
	if status != 204 {
		t.Errorf("owner delete of bot-authored: status=%d", status)
	}
}

func TestCommentsIntegration_RepliesLifecycle(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")

	// Create reply.
	status, body := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/replies",
		map[string]string{"body": "a reply"}, withCookie(sess))
	if status != 201 {
		t.Fatalf("reply create: %d %s", status, body)
	}
	var c2 models.Comment
	mustDecode(t, body, &c2)
	if len(c2.Replies) != 1 {
		t.Fatalf("expected 1 reply: %+v", c2)
	}
	replyID := c2.Replies[0].ID

	// Edit reply.
	status, _ = doJSON(t, srv, "PATCH", "/api/comments/"+c.ID+"/replies/"+replyID,
		map[string]string{"body": "edited"}, withCookie(sess))
	if status != 200 {
		t.Errorf("reply edit: status=%d", status)
	}

	// Delete reply.
	status, _ = doJSON(t, srv, "DELETE", "/api/comments/"+c.ID+"/replies/"+replyID, nil, withCookie(sess))
	if status != 200 {
		t.Errorf("reply delete: status=%d", status)
	}

	// Replies list should be empty now.
	cur, _ := st.GetComment(context.Background(), c.ID)
	if cur != nil && len(cur.Replies) != 0 {
		t.Errorf("replies remaining: %+v", cur.Replies)
	}
}

func TestCommentsIntegration_ReplyEditOnlyMine(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	ownerSess := testutil.NewTestSession(t, st, owner.ID)
	other := testutil.NewTestUser(t, st)
	otherSess := testutil.NewTestSession(t, st, other.ID)
	doc := testutil.NewTestDocument(t, st, owner.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, owner.ID, "", "")
	// Owner posts a reply.
	status, body := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/replies",
		map[string]string{"body": "owner reply"}, withCookie(ownerSess))
	if status != 201 {
		t.Fatalf("reply create: %d %s", status, body)
	}
	var cWithReply models.Comment
	mustDecode(t, body, &cWithReply)
	rid := cWithReply.Replies[0].ID

	// Other user cannot edit owner's reply.
	status, _ = doJSON(t, srv, "PATCH", "/api/comments/"+c.ID+"/replies/"+rid,
		map[string]string{"body": "intrude"}, withCookie(otherSess))
	if status != 403 {
		t.Errorf("non-author reply edit: status=%d, want 403", status)
	}
}

func TestCommentsIntegration_RenderHTMLQuery(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	_ = testutil.NewTestComment(t, st, doc.ID, user.ID, "Hello", "**bold**")

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/comments?render=html", nil)
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	// JSON encodes < as < — decode the response to assert structure.
	var got []models.Comment
	mustDecode(t, body, &got)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if !strings.Contains(got[0].BodyHTML, "<strong>bold</strong>") {
		t.Errorf("expected rendered HTML in bodyHtml: %q", got[0].BodyHTML)
	}
}

func TestCommentsIntegration_TokenScopeEnforcedOnDelete(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")

	// Read token cannot delete (scope insufficient before per-mine check).
	rPlain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeRead)
	status, _ := doJSON(t, srv, "DELETE", "/api/comments/"+c.ID, nil, withBearer(rPlain))
	if status != 403 {
		t.Errorf("read delete: status=%d, want 403", status)
	}
}
