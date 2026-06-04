package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestNotificationsIntegration_RequireSignIn(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "GET", "/api/me/notifications", nil)
	if status != 401 {
		t.Fatalf("status=%d", status)
	}
}

func TestNotificationsIntegration_ListEmpty(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "GET", "/api/me/notifications", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	// Expect {"unread":0,"notifications":[]}
	if !strings.Contains(string(body), `"unread":0`) {
		t.Errorf("got %s", body)
	}
}

func TestNotificationsIntegration_MarkOne(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	n := &models.Notification{
		ID: uuid.NewString(), UserID: user.ID,
		Kind:          models.NotifyMention,
		DocumentID:    "d", DocumentTitle: "doc",
		CommentID: "c", ActorID: "a", ActorName: "A",
		Preview: "hi", CreatedAt: time.Now().UTC(),
	}
	_ = st.InsertNotification(context.Background(), n)

	status, _ := doJSON(t, srv, "POST", "/api/me/notifications/"+n.ID+"/read", nil, withCookie(sess))
	if status != 204 {
		t.Fatalf("status=%d", status)
	}

	// Refetch.
	status, body := doJSON(t, srv, "GET", "/api/me/notifications", nil, withCookie(sess))
	if status != 200 || !strings.Contains(string(body), `"unread":0`) {
		t.Errorf("after mark read: %d %s", status, body)
	}
}

func TestNotificationsIntegration_MarkAll(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	for i := 0; i < 3; i++ {
		_ = st.InsertNotification(context.Background(), &models.Notification{
			ID: uuid.NewString(), UserID: user.ID,
			Kind: models.NotifyMention,
			DocumentID: "d", DocumentTitle: "doc", CommentID: "c",
			ActorID: "a", ActorName: "A", Preview: "hi",
			CreatedAt: time.Now().UTC(),
		})
	}
	status, _ := doJSON(t, srv, "POST", "/api/me/notifications/read", nil, withCookie(sess))
	if status != 204 {
		t.Fatalf("status=%d", status)
	}
	status, body := doJSON(t, srv, "GET", "/api/me/notifications", nil, withCookie(sess))
	if status != 200 || !strings.Contains(string(body), `"unread":0`) {
		t.Errorf("after mark all: %d %s", status, body)
	}
}

func TestMentionCandidates_RequiresDocAccess(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/mention-candidates", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	// Should include at least the requester.
	if !strings.Contains(string(body), user.Login) {
		t.Errorf("expected requester to appear; got %s", body)
	}
}
