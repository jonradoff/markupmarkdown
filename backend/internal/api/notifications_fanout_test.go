package api_test

import (
	"context"
	"testing"
	"time"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestFanOut_MentionGeneratesNotification(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	ownerSess := testutil.NewTestSession(t, st, owner.ID)

	// Mentionable user named "alice" — owner mentions them.
	other := testutil.NewTestUser(t, st)
	// Override the random Login so the regex matches our text.
	other.Login = "alice-" + other.ID[:6]
	_ = st.UpsertUserByGitHubID(context.Background(), other)

	doc := testutil.NewTestDocument(t, st, owner.ID, "Hello world")

	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/comments", map[string]any{
		"anchor": map[string]any{"start": 0, "end": 5, "exact": "Hello"},
		"body":   "Welcome @" + other.Login + "!",
	}, withCookie(ownerSess))
	if status != 201 {
		t.Fatalf("create: %d", status)
	}

	// Fan-out is async via goroutine. Poll briefly.
	deadline := time.Now().Add(5 * time.Second)
	var notes []models.Notification
	for time.Now().Before(deadline) {
		notes, _, _ = st.ListNotificationsForUser(context.Background(), other.ID, 10)
		if len(notes) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(notes) == 0 {
		t.Fatal("expected at least one notification for the mention")
	}
	if notes[0].Kind != models.NotifyMention {
		t.Errorf("kind = %q, want mention", notes[0].Kind)
	}
}

func TestFanOut_ReplyNotifiesThreadParticipants(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	ownerSess := testutil.NewTestSession(t, st, owner.ID)
	other := testutil.NewTestUser(t, st)
	otherSess := testutil.NewTestSession(t, st, other.ID)

	doc := testutil.NewTestDocument(t, st, owner.ID, "Hello world")
	c := testutil.NewTestComment(t, st, doc.ID, owner.ID, "Hello", "first")
	_ = c
	_ = ownerSess

	// `other` replies → owner should get a notification.
	status, _ := doJSON(t, srv, "POST", "/api/comments/"+c.ID+"/replies",
		map[string]string{"body": "i agree"}, withCookie(otherSess))
	if status != 201 {
		t.Fatalf("reply: %d", status)
	}

	deadline := time.Now().Add(5 * time.Second)
	var notes []models.Notification
	for time.Now().Before(deadline) {
		notes, _, _ = st.ListNotificationsForUser(context.Background(), owner.ID, 10)
		if len(notes) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(notes) == 0 {
		t.Fatal("expected owner to receive a reply notification")
	}
	if notes[0].Kind != models.NotifyReply {
		t.Errorf("kind = %q, want reply", notes[0].Kind)
	}
}
