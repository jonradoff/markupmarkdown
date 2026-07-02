package api_test

// Push-gate tests. Covers both gates:
//   1. Changes-requested reviews block pushback with 409, force=true
//      overrides.
//   2. Agent-authored revisions block pushback until accept-revision
//      lands, force=true overrides.
//
// Uses mockGitHubForPushback for the GitHub side (defined in
// pushback_test.go) so the gate logic is what the test isolates.

import (
	"context"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestPushback_BlockedByChangesRequested(t *testing.T) {
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()
	srv, st, _ := newTestServer(t)
	author := testutil.NewTestUser(t, st)
	reviewer := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, author.ID)
	reviewerSess := testutil.NewTestSession(t, st, reviewer.ID)
	doc := insertPushbackTestDoc(t, st, author.ID, "owner", "repo")

	// Reviewer requests changes.
	doJSON(t, srv, "PUT", "/api/documents/"+doc.ID+"/review",
		map[string]string{"state": "changes_requested"}, withCookie(reviewerSess))

	// Author tries to push — should be 409.
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/pushback",
		map[string]string{"mode": "pr", "branch": "agent/edit", "targetBranch": "main"},
		withCookie(sess))
	if status != 409 {
		t.Fatalf("status=%d body=%s want 409", status, body)
	}
	if !strings.Contains(string(body), "changes_requested") {
		t.Errorf("expected changes_requested kind in body, got %s", body)
	}

	// Same request with force=true succeeds.
	status, body = doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/pushback",
		map[string]any{"mode": "pr", "branch": "agent/edit", "targetBranch": "main", "force": true},
		withCookie(sess))
	if status != 200 {
		t.Errorf("force push status=%d body=%s want 200", status, body)
	}
}

func TestPushback_BlockedByUnAcceptedAgentRevision(t *testing.T) {
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")

	// Simulate an agent-authored revision by stamping the meta.
	_, err := st.Documents().UpdateOne(context.Background(),
		bson.M{"_id": doc.ID},
		bson.M{"$set": bson.M{
			"revision_meta": bson.M{
				"model":       "manual",
				"actor_kind":  string(models.ActorAgent),
				"token_id":    "some-token-id",
				"generated_by":  "bot",
				"generated_at":  doc.CreatedAt,
			},
		}})
	if err != nil {
		t.Fatalf("stamp agent meta: %v", err)
	}

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/pushback",
		map[string]string{"mode": "pr", "branch": "agent/edit", "targetBranch": "main"},
		withCookie(sess))
	if status != 409 {
		t.Fatalf("status=%d body=%s want 409 for un-accepted agent revision", status, body)
	}
	if !strings.Contains(string(body), "agent_revision_not_accepted") {
		t.Errorf("expected agent_revision_not_accepted kind in body, got %s", body)
	}
}

func TestAcceptAgentRevision_ClearsGate(t *testing.T) {
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")

	// Stamp agent meta first.
	_, _ = st.Documents().UpdateOne(context.Background(),
		bson.M{"_id": doc.ID},
		bson.M{"$set": bson.M{
			"revision_meta": bson.M{
				"model":        "manual",
				"actor_kind":   string(models.ActorAgent),
				"token_id":     "tok",
				"generated_by": "bot",
				"generated_at": doc.CreatedAt,
			},
		}})

	// Accept.
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/accept-revision", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("accept status=%d", status)
	}
	updated, _ := st.GetDocument(context.Background(), doc.ID)
	if updated == nil || updated.RevisionMeta == nil || updated.RevisionMeta.AcceptedAt == nil {
		t.Fatalf("accepted_at not stamped on %+v", updated)
	}

	// Push now succeeds without force.
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/pushback",
		map[string]string{"mode": "pr", "branch": "agent/edit", "targetBranch": "main"},
		withCookie(sess))
	if status != 200 {
		t.Errorf("post-accept push status=%d body=%s want 200", status, body)
	}
}

func TestAcceptAgentRevision_RejectsTokenAuth(t *testing.T) {
	// A leaked Bearer token must not be able to self-accept — that
	// would defeat the whole gate. Cookie sessions only.
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeAdmin)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")

	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/accept-revision", nil,
		withBearer(plain))
	if status != 401 && status != 403 {
		t.Errorf("token-auth accept status=%d want 401 or 403", status)
	}
}

