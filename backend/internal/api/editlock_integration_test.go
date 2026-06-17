package api_test

// Integration tests for the soft edit-lock HTTP handlers — claim,
// release, get. The lock state is in-process (package-level map in
// editlock.go); the handlers themselves are HTTP wrappers around it
// that fanout SSE events on state changes. Covers the full lifecycle
// + the "another holder" 409 path which the unit tests can't reach
// without a real router.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func insertEditLockDoc(t *testing.T, st interface {
	InsertDocument(ctx context.Context, d *models.Document) error
}, userID string) *models.Document {
	t.Helper()
	now := time.Now().UTC()
	d := &models.Document{
		ID:          uuid.NewString(),
		Title:       "Lock Target",
		Origin:      "upload",
		Content:     "# Lock Target\n",
		CreatedByID: userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := st.InsertDocument(context.Background(), d); err != nil {
		t.Fatalf("insert doc: %v", err)
	}
	return d
}

func TestEditLock_GetEmpty(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertEditLockDoc(t, st, user.ID)

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	var payload map[string]any
	mustDecode(t, body, &payload)
	if locked, _ := payload["locked"].(bool); locked {
		t.Errorf("expected locked=false, got %+v", payload)
	}
}

func TestEditLock_ClaimRequiresAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := insertEditLockDoc(t, st, user.ID)

	// No cookie → 401.
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/edit-lock", nil)
	if status != 401 {
		t.Errorf("status=%d, want 401 (no auth)", status)
	}
}

func TestEditLock_ClaimAndRelease(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertEditLockDoc(t, st, user.ID)

	// Claim.
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("claim status=%d body=%s", status, body)
	}
	var payload map[string]any
	mustDecode(t, body, &payload)
	if payload["holderId"] != user.ID {
		t.Errorf("holderId=%v, want %s", payload["holderId"], user.ID)
	}

	// Get should now show locked=true, mine=true.
	status, body = doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("get status=%d body=%s", status, body)
	}
	mustDecode(t, body, &payload)
	if locked, _ := payload["locked"].(bool); !locked {
		t.Errorf("locked=false after claim, want true")
	}
	if mine, _ := payload["mine"].(bool); !mine {
		t.Errorf("mine=false after claim, want true")
	}

	// Release.
	status, _ = doJSON(t, srv, "DELETE", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(sess))
	if status != 204 {
		t.Errorf("release status=%d, want 204", status)
	}

	// Get is back to empty.
	status, body = doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("post-release get status=%d", status)
	}
	mustDecode(t, body, &payload)
	if locked, _ := payload["locked"].(bool); locked {
		t.Errorf("locked=true after release, want false")
	}
}

func TestEditLock_ClaimByDifferentUserReturns409(t *testing.T) {
	srv, st, _ := newTestServer(t)
	holder := testutil.NewTestUser(t, st)
	holderSess := testutil.NewTestSession(t, st, holder.ID)
	other := testutil.NewTestUser(t, st)
	otherSess := testutil.NewTestSession(t, st, other.ID)
	doc := insertEditLockDoc(t, st, holder.ID)

	// Holder claims first.
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(holderSess))
	if status != 200 {
		t.Fatalf("holder claim status=%d", status)
	}

	// Other tries to claim → 409 with holder name in the body.
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(otherSess))
	if status != 409 {
		t.Fatalf("other-claim status=%d, want 409", status)
	}
	if !strings.Contains(string(body), holder.Login) && !strings.Contains(string(body), holder.Name) {
		t.Errorf("expected holder identity in 409 body, got %s", body)
	}
}

func TestEditLock_ReleaseByNonHolderIsForbidden(t *testing.T) {
	srv, st, _ := newTestServer(t)
	holder := testutil.NewTestUser(t, st)
	holderSess := testutil.NewTestSession(t, st, holder.ID)
	other := testutil.NewTestUser(t, st)
	otherSess := testutil.NewTestSession(t, st, other.ID)
	doc := insertEditLockDoc(t, st, holder.ID)

	// Holder claims.
	doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(holderSess))

	// Other tries to release → 403.
	status, _ := doJSON(t, srv, "DELETE", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(otherSess))
	if status != 403 {
		t.Errorf("release-by-other status=%d, want 403", status)
	}
}

func TestEditLock_ReleaseWhenNoLockIsNoop(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertEditLockDoc(t, st, user.ID)

	// No lock → DELETE should return 204 silently.
	status, _ := doJSON(t, srv, "DELETE", "/api/documents/"+doc.ID+"/edit-lock", nil, withCookie(sess))
	if status != 204 {
		t.Errorf("release-no-lock status=%d, want 204", status)
	}
}

// Suppress the "encoding/json" unused-import warning if all the
// mustDecode usages get tree-shaken under build tags.
var _ = json.Unmarshal
