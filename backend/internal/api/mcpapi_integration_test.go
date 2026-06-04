package api_test

import (
	"context"
	"testing"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestMCPAPI_UserFromBearer_ValidToken(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)

	got, tokenID, label, scope, err := a.UserFromBearer(context.Background(), plain)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || got.ID != user.ID {
		t.Errorf("user mismatch: %+v", got)
	}
	if tokenID == "" || label == "" {
		t.Errorf("token id/label missing")
	}
	if scope != models.TokenScopeWrite {
		t.Errorf("scope = %q", scope)
	}
}

func TestMCPAPI_UserFromBearer_BadToken(t *testing.T) {
	_, _, a := newTestServer(t)
	u, _, _, _, err := a.UserFromBearer(context.Background(), "not-a-real-token")
	if err != nil || u != nil {
		t.Fatalf("got u=%v err=%v; want nil,nil", u, err)
	}
}

func TestMCPAPI_UserFromBearer_RejectedShortToken(t *testing.T) {
	_, _, a := newTestServer(t)
	u, _, _, _, err := a.UserFromBearer(context.Background(), "mmk_short")
	if err != nil || u != nil {
		t.Fatalf("got u=%v err=%v", u, err)
	}
}

func TestMCPAPI_DocAccess_Public(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	got, err := a.DocAccess(context.Background(), user.ID, doc.ID, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.ID != doc.ID {
		t.Errorf("got %q", got.ID)
	}
}

func TestMCPAPI_DocAccess_NotFound(t *testing.T) {
	_, _, a := newTestServer(t)
	if _, err := a.DocAccess(context.Background(), "u", "no-such-doc", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestMCPAPI_ListDocumentsForUser(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	for i := 0; i < 2; i++ {
		testutil.NewTestDocument(t, st, user.ID, "")
	}
	list, err := a.ListDocumentsForUser(context.Background(), user.ID, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d, want 2", len(list))
	}
}

func TestMCPAPI_ListDocumentsForUser_IncludeTrash(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	d := testutil.NewTestDocument(t, st, user.ID, "")
	_ = st.SoftDeleteDocument(context.Background(), d.ID)

	list, _ := a.ListDocumentsForUser(context.Background(), user.ID, true)
	if len(list) != 1 {
		t.Fatalf("includeTrash should include soft-deleted: %d", len(list))
	}
}

func TestMCPAPI_ListComments(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")
	_ = testutil.NewTestComment(t, st, doc.ID, user.ID, "Hello", "first")

	list, err := a.ListComments(context.Background(), doc.ID)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d", len(list))
	}
}

func TestMCPAPI_CreateComment_Success(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	plain, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)
	_ = plain // not needed at this layer; we call CreateComment directly

	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world. Hello again.")

	c, err := a.CreateComment(context.Background(), user.ID, doc.ID,
		"a thoughtful note", "Hello again", 1, rec.ID, "claude-curl")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c.Body != "a thoughtful note" {
		t.Errorf("body = %q", c.Body)
	}
	if c.ActorKind != models.ActorAgent {
		t.Errorf("kind = %q", c.ActorKind)
	}
	// The agentLabel argument is only the fallback stamp at write time;
	// resolveAgentIdentity then overlays from the current token record,
	// so the response shows the token's stored Label, not the passed-in
	// agentLabel. (The fixture token's label is "test-token".)
	if c.Author != rec.Label {
		t.Errorf("author = %q, want token label %q", c.Author, rec.Label)
	}
}

func TestMCPAPI_CreateComment_QuotedNotFound(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")

	_, err := a.CreateComment(context.Background(), user.ID, doc.ID,
		"note", "not-in-the-doc", 1, rec.ID, "label")
	if err == nil {
		t.Fatal("expected error for missing quote")
	}
}

func TestMCPAPI_CreateComment_OccurrenceOutOfRange(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world Hello again")

	// "Hello" appears twice; occurrence=5 is out of range.
	_, err := a.CreateComment(context.Background(), user.ID, doc.ID,
		"note", "Hello", 5, rec.ID, "label")
	if err == nil {
		t.Fatal("expected error for out-of-range occurrence")
	}
}

func TestMCPAPI_CreateComment_DocNotFound(t *testing.T) {
	_, _, a := newTestServer(t)
	_, err := a.CreateComment(context.Background(), "u", "no-doc", "x", "x", 1, "t", "L")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMCPAPI_ReplyToComment(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")

	got, err := a.ReplyToComment(context.Background(), user.ID, c.ID, "a reply", rec.ID, "L")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Replies) != 1 {
		t.Fatalf("reply count = %d", len(got.Replies))
	}
	// Same identity-resolution behavior as CreateComment: reply.Author
	// reflects the token's stored Label after decoration.
	if got.Replies[0].Author != rec.Label {
		t.Errorf("reply author = %q, want %q", got.Replies[0].Author, rec.Label)
	}
}

func TestMCPAPI_ReplyToComment_NotFound(t *testing.T) {
	_, _, a := newTestServer(t)
	if _, err := a.ReplyToComment(context.Background(), "u", "no-such", "x", "t", "L"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMCPAPI_ResolveAndReopen(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "", "")

	resolved, err := a.ResolveComment(context.Background(), user.ID, c.ID, false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !resolved.Resolved {
		t.Fatal("not resolved")
	}

	reopened, err := a.ResolveComment(context.Background(), user.ID, c.ID, true)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Resolved {
		t.Fatal("not reopened")
	}
}

func TestMCPAPI_ResolveNotFound(t *testing.T) {
	_, _, a := newTestServer(t)
	if _, err := a.ResolveComment(context.Background(), "u", "no-such", false); err == nil {
		t.Fatal("expected error")
	}
}

func TestMCPAPI_AllowRates_RoundTrip(t *testing.T) {
	_, _, a := newTestServer(t)
	// Comment rate bucket has burst 10 → first call should pass.
	if !a.AllowCommentRate("u-x") {
		t.Fatal("first comment rate call should be allowed")
	}
	// Revise rate bucket has burst 1.
	if !a.AllowReviseRate("u-x") {
		t.Fatal("first revise rate call should be allowed")
	}
	if a.AllowReviseRate("u-x") {
		t.Fatal("second revise rate call should be denied (no refill)")
	}
}

func TestMCPAPI_AcquireReviseSlot(t *testing.T) {
	_, _, a := newTestServer(t)
	release, ok := a.AcquireReviseSlot("u-slot")
	if !ok {
		t.Fatal("first slot should acquire")
	}
	defer release()
}

func TestMCPAPI_ValidationHelpers(t *testing.T) {
	_, _, a := newTestServer(t)
	if _, err := a.ValidateCommentBody(""); err == nil {
		t.Error("empty comment should fail")
	}
	if _, err := a.ValidateReplyBody(""); err == nil {
		t.Error("empty reply should fail")
	}
	if got, _ := a.ValidateCommentBody(" hi "); got != "hi" {
		t.Errorf("got %q", got)
	}
}

func TestMCPAPI_LogTokenAction(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)

	// First call writes. The sampler then suppresses for a minute, so we
	// only assert that the call doesn't panic.
	a.LogTokenAction(context.Background(), rec.ID, "comment.create", "doc-x")
	a.LogTokenAction(context.Background(), rec.ID, "comment.create", "doc-x")

	// Empty fields are a no-op.
	a.LogTokenAction(context.Background(), "", "x", "y")
}

// Sanity import to keep the file honest.
var _ api.API
