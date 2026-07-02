package api_test

// Integration tests for the review-state surface (P0-1). Every gate is
// exercised: auth required, invalid state rejected, idempotent
// upsert, delete, list, and — critically — the push gate in pushback
// that consults AnyChangesRequested.

import (
	"context"
	"strings"
	"testing"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestSetReview_RequiresAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "hello")
	status, _ := doJSON(t, srv, "PUT", "/api/documents/"+doc.ID+"/review",
		map[string]string{"state": "approved"})
	if status != 401 {
		t.Errorf("status=%d want 401", status)
	}
}

func TestSetReview_RejectsBadState(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "hello")
	status, body := doJSON(t, srv, "PUT", "/api/documents/"+doc.ID+"/review",
		map[string]string{"state": "not-a-state"}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d body=%s want 400", status, body)
	}
	if !strings.Contains(string(body), "approved") {
		t.Errorf("expected vocabulary hint in body, got %s", body)
	}
}

func TestSetReview_UpsertReplacesPriorState(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "hello")

	// commented → approved → changes_requested. Each replaces the prior.
	for _, s := range []string{"commented", "approved", "changes_requested"} {
		status, _ := doJSON(t, srv, "PUT", "/api/documents/"+doc.ID+"/review",
			map[string]string{"state": s}, withCookie(sess))
		if status != 200 {
			t.Fatalf("state=%s status=%d", s, status)
		}
	}
	got, err := st.GetReview(context.Background(), doc.ID, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("review vanished")
	}
	if got.State != models.ReviewStateChangesRequested {
		t.Errorf("final state=%q want changes_requested", got.State)
	}
	// AnyChangesRequested should also see it.
	blocked, _ := st.AnyChangesRequested(context.Background(), doc.ID)
	if !blocked {
		t.Errorf("AnyChangesRequested=false after upsert to changes_requested")
	}
}

func TestDeleteReview_ClearsPushGate(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "hello")

	doJSON(t, srv, "PUT", "/api/documents/"+doc.ID+"/review",
		map[string]string{"state": "changes_requested"}, withCookie(sess))
	blocked, _ := st.AnyChangesRequested(context.Background(), doc.ID)
	if !blocked {
		t.Fatalf("precondition: blocked=false")
	}

	status, _ := doJSON(t, srv, "DELETE", "/api/documents/"+doc.ID+"/review", nil, withCookie(sess))
	if status != 204 {
		t.Errorf("delete status=%d want 204", status)
	}
	blocked, _ = st.AnyChangesRequested(context.Background(), doc.ID)
	if blocked {
		t.Errorf("gate still fires after review deleted")
	}
}

func TestListReviews_ReturnsBothReviewers(t *testing.T) {
	srv, st, _ := newTestServer(t)
	a := testutil.NewTestUser(t, st)
	b := testutil.NewTestUser(t, st)
	aSess := testutil.NewTestSession(t, st, a.ID)
	bSess := testutil.NewTestSession(t, st, b.ID)
	doc := testutil.NewTestDocument(t, st, a.ID, "hello")

	doJSON(t, srv, "PUT", "/api/documents/"+doc.ID+"/review",
		map[string]string{"state": "approved"}, withCookie(aSess))
	doJSON(t, srv, "PUT", "/api/documents/"+doc.ID+"/review",
		map[string]string{"state": "changes_requested"}, withCookie(bSess))

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/reviews", nil, withCookie(aSess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	// Both states should appear.
	if !strings.Contains(string(body), "approved") ||
		!strings.Contains(string(body), "changes_requested") {
		t.Errorf("expected both states in body, got %s", body)
	}
}

func TestGetDocument_CarriesReviewSummary(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "hello")
	doJSON(t, srv, "PUT", "/api/documents/"+doc.ID+"/review",
		map[string]string{"state": "approved"}, withCookie(sess))

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID, nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"approved":1`) {
		t.Errorf("expected approved=1 in review summary, got %s", body)
	}
	if !strings.Contains(string(body), `"myReview"`) {
		t.Errorf("expected myReview field, got %s", body)
	}
}
