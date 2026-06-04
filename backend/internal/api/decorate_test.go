package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

// Integration-level tests for resolveAgentIdentities + decorate via the
// listComments handler. Cover the agent-author overlay path, which is
// fiddly enough to deserve direct assertions.

func TestDecorate_AgentDisplayResolvedFromCurrentTokenLabel(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	_, rec := testutil.NewAPIToken(t, st, owner.ID, models.TokenScopeWrite)
	doc := testutil.NewTestDocument(t, st, owner.ID, "Hello world")

	// Write the comment with a snapshotted (incorrect) author so we can
	// verify decorate overrides it with the token's current label.
	c := &models.Comment{
		ID:         uuid.NewString(),
		DocumentID: doc.ID,
		Anchor:     models.Anchor{Start: 0, End: 5, Exact: "Hello"},
		Body:       "from bot",
		Author:     "stale-snapshot",
		AuthorID:   owner.ID,
		TokenID:    rec.ID,
		ActorKind:  models.ActorAgent,
		Replies:    []models.Reply{},
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := st.InsertComment(context.Background(), c); err != nil {
		t.Fatalf("insert: %v", err)
	}

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/comments", nil)
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	var got []models.Comment
	mustDecode(t, body, &got)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Author != rec.Label {
		t.Errorf("Author=%q, want token label %q (overlay should have run)", got[0].Author, rec.Label)
	}
	if got[0].OwnerName == "" || got[0].OwnerLogin == "" {
		t.Errorf("owner fields not populated: %+v", got[0])
	}
}

func TestDecorate_HumanCommentRetainsAvatar(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	owner.AvatarURL = "https://avatars.githubusercontent.com/u/12345?v=4"
	_ = st.UpsertUserByGitHubID(context.Background(), owner)
	sess := testutil.NewTestSession(t, st, owner.ID)
	doc := testutil.NewTestDocument(t, st, owner.ID, "")

	// Create through the real handler so AuthorAvatarURL is stamped.
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/comments",
		map[string]any{
			"anchor": map[string]any{"start": 0, "end": 5, "exact": "Hello"},
			"body":   "human comment",
		}, withCookie(sess))
	if status != 201 {
		t.Fatalf("create: %d", status)
	}

	// List comments; the avatar must survive the decorate overlay.
	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/comments",
		nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("list: %d", status)
	}
	var got []models.Comment
	mustDecode(t, body, &got)
	if len(got) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(got))
	}
	if got[0].AuthorAvatarURL == "" {
		t.Fatal("regression: human comment lost AuthorAvatarURL on decorate")
	}
	if got[0].AuthorAvatarURL != owner.AvatarURL {
		t.Errorf("avatar = %q, want %q", got[0].AuthorAvatarURL, owner.AvatarURL)
	}
}

func TestDecorate_NoOwnerOverlayForHumanComments(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, owner.ID, "Hello")
	c := testutil.NewTestComment(t, st, doc.ID, owner.ID, "", "")

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/comments", nil)
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	var got []models.Comment
	mustDecode(t, body, &got)
	if len(got) != 1 || got[0].ID != c.ID {
		t.Fatalf("got %+v", got)
	}
	if got[0].OwnerName != "" || got[0].OwnerLogin != "" {
		t.Errorf("owner fields should be empty for human comments: %+v", got[0])
	}
}

func TestDecorate_MineFlagFollowsViewerIdentity(t *testing.T) {
	srv, st, _ := newTestServer(t)
	owner := testutil.NewTestUser(t, st)
	other := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, owner.ID, "Hello")
	c := testutil.NewTestComment(t, st, doc.ID, owner.ID, "", "")

	// Anonymous viewer: no mine flag set.
	_, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/comments", nil)
	var anon []models.Comment
	mustDecode(t, body, &anon)
	if anon[0].Mine {
		t.Error("anonymous viewer should not see mine=true")
	}

	// Owner viewer: mine=true.
	ownerSess := testutil.NewTestSession(t, st, owner.ID)
	_, body = doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/comments", nil, withCookie(ownerSess))
	var ownerView []models.Comment
	mustDecode(t, body, &ownerView)
	if !ownerView[0].Mine {
		t.Error("owner should see mine=true")
	}

	// Other viewer: mine=false.
	otherSess := testutil.NewTestSession(t, st, other.ID)
	_, body = doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/comments", nil, withCookie(otherSess))
	var otherView []models.Comment
	mustDecode(t, body, &otherView)
	if otherView[0].Mine {
		t.Error("other user should see mine=false")
	}
	_ = c
}
