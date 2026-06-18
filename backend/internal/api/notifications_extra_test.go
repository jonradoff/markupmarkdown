package api_test

// markNotificationsReadForComment is invoked when a viewer activates a
// comment in the doc page. The store layer is already integration-
// tested; this verifies the HTTP shape: auth gate, success on a real
// comment, success even when there are no matching notifications.

import (
	"strings"
	"testing"

	"markupmarkdown/internal/testutil"
)

func TestMarkNotificationsReadForComment_RequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "POST", "/api/me/notifications/comment/some-comment/read", nil)
	if status != 401 {
		t.Errorf("status=%d want 401", status)
	}
}

func TestMarkNotificationsReadForComment_SuccessNoMatchingRows(t *testing.T) {
	// Even when there are no notifications for the (user, comment)
	// pair, the handler should return 200 with {"updated":0} — it's
	// idempotent.
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "POST", "/api/me/notifications/comment/nonexistent-comment/read", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"updated":0`) {
		t.Errorf("expected updated=0 in body, got %s", body)
	}
}
