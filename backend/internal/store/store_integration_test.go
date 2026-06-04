package store_test

// Integration tests for the Mongo store. Skipped when no MONGODB_URI is
// configured (see testutil.MustConnectTestDB). The testutil safety guard
// hard-refuses to run if the DB name doesn't contain "test", so these
// can NEVER touch prod or dev data.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestStoreIntegration_DocumentLifecycle(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	doc := &models.Document{
		ID:        uuid.NewString(),
		Title:     "Original",
		Origin:    "upload",
		Content:   "# Hello",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := st.InsertDocument(ctx, doc); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := st.GetDocument(ctx, doc.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v, doc=%v", err, got)
	}
	if got.Title != "Original" {
		t.Fatalf("got title %q", got.Title)
	}

	if err := st.UpdateDocumentTitle(ctx, doc.ID, "Renamed"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, _ = st.GetDocument(ctx, doc.ID)
	if got.Title != "Renamed" {
		t.Fatalf("title not updated")
	}
}

func TestStoreIntegration_SoftDeleteAndRestore(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	doc := &models.Document{
		ID: uuid.NewString(), Title: "T", Origin: "upload",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = st.InsertDocument(ctx, doc)

	if err := st.SoftDeleteDocument(ctx, doc.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	live, _ := st.GetDocument(ctx, doc.ID)
	if live != nil {
		t.Fatal("GetDocument should not return soft-deleted")
	}
	gone, _ := st.GetDeletedDocument(ctx, doc.ID)
	if gone == nil {
		t.Fatal("GetDeletedDocument should return the soft-deleted doc")
	}

	if err := st.RestoreDocument(ctx, doc.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	live, _ = st.GetDocument(ctx, doc.ID)
	if live == nil {
		t.Fatal("restored doc should appear")
	}
}

func TestStoreIntegration_PurgeExpired(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	long := time.Now().UTC().Add(-100 * 24 * time.Hour)
	doc := &models.Document{
		ID: uuid.NewString(), Title: "Old", Origin: "upload",
		CreatedAt: long, UpdatedAt: long, DeletedAt: &long,
	}
	_, _ = st.Documents().InsertOne(ctx, doc)

	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	n, err := st.PurgeExpiredDeletes(ctx, cutoff)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 purged; got %d", n)
	}
	got, _ := st.GetDeletedDocument(ctx, doc.ID)
	if got != nil {
		t.Fatal("doc should have been hard-deleted")
	}
}

func TestStoreIntegration_PurgeCascadesCommentsAndViews(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	doc := &models.Document{
		ID: uuid.NewString(), Title: "P", Origin: "upload",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = st.InsertDocument(ctx, doc)
	_ = st.InsertComment(ctx, &models.Comment{
		ID: uuid.NewString(), DocumentID: doc.ID,
		Anchor:  models.Anchor{Start: 0, End: 1, Exact: "x"},
		Body:    "c",
		Replies: []models.Reply{},
	})
	_ = st.RecordDocumentView(ctx, doc.ID, "u-purge")

	if err := st.PurgeDocument(ctx, doc.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	cs, _ := st.ListComments(ctx, doc.ID)
	if len(cs) != 0 {
		t.Fatalf("comments should cascade-delete; got %d", len(cs))
	}
	view, _ := st.GetDocumentView(ctx, doc.ID, "u-purge")
	if view != nil {
		t.Fatal("view should cascade-delete")
	}
}

func TestStoreIntegration_CommentsAndReplies(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	doc := &models.Document{
		ID: uuid.NewString(), Title: "D", Origin: "upload",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = st.InsertDocument(ctx, doc)

	c := &models.Comment{
		ID: uuid.NewString(), DocumentID: doc.ID,
		Anchor:    models.Anchor{Start: 0, End: 5, Exact: "Hello"},
		Body:      "Initial",
		Author:    "Alice",
		AuthorID:  "u1",
		ActorKind: models.ActorHuman,
		Replies:   []models.Reply{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := st.InsertComment(ctx, c); err != nil {
		t.Fatalf("insert comment: %v", err)
	}

	got, _ := st.GetComment(ctx, c.ID)
	if got == nil || got.Body != "Initial" {
		t.Fatalf("get comment: %v", got)
	}

	// Update.
	updated, err := st.UpdateComment(ctx, c.ID, bson.M{"body": "Updated"})
	if err != nil || updated == nil || updated.Body != "Updated" {
		t.Fatalf("update comment: %v / %v", err, updated)
	}

	// Append reply.
	r := models.Reply{
		ID:        uuid.NewString(),
		Author:    "Bob",
		AuthorID:  "u2",
		ActorKind: models.ActorHuman,
		Body:      "reply 1",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	c2, err := st.AppendReply(ctx, c.ID, r)
	if err != nil || len(c2.Replies) != 1 {
		t.Fatalf("append: %v / %+v", err, c2)
	}

	// Update reply.
	c3, err := st.UpdateReply(ctx, c.ID, r.ID, "edited reply")
	if err != nil || c3 == nil {
		t.Fatalf("update reply: %v", err)
	}
	if c3.Replies[0].Body != "edited reply" {
		t.Fatalf("reply body not updated: %q", c3.Replies[0].Body)
	}

	// Delete reply.
	c4, err := st.DeleteReply(ctx, c.ID, r.ID)
	if err != nil || c4 == nil {
		t.Fatalf("delete reply: %v", err)
	}
	if len(c4.Replies) != 0 {
		t.Fatalf("reply not deleted: %+v", c4.Replies)
	}

	// List + delete comment.
	all, _ := st.ListComments(ctx, doc.ID)
	if len(all) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(all))
	}
	if err := st.DeleteComment(ctx, c.ID); err != nil {
		t.Fatalf("delete comment: %v", err)
	}
	all, _ = st.ListComments(ctx, doc.ID)
	if len(all) != 0 {
		t.Fatalf("comment not deleted: %d remaining", len(all))
	}
}

func TestStoreIntegration_APITokens_FullLifecycle(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tok := &models.APIToken{
		ID: uuid.NewString(), UserID: "u-tok",
		Hash: "h" + uuid.NewString(), Prefix: "mmk_abc…",
		Label: "alpha", Scope: models.TokenScopeWrite,
		CreatedAt: time.Now().UTC(),
	}
	if err := st.InsertAPIToken(ctx, tok); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Lookup by hash.
	if got, _ := st.GetAPITokenByHash(ctx, tok.Hash); got == nil {
		t.Fatal("GetAPITokenByHash returned nil")
	}
	// Lookup by id.
	if got, _ := st.GetAPITokenByID(ctx, tok.UserID, tok.ID); got == nil {
		t.Fatal("GetAPITokenByID returned nil")
	}
	// List for user.
	list, _ := st.ListAPITokensForUser(ctx, tok.UserID)
	if len(list) != 1 {
		t.Fatalf("want 1; got %d", len(list))
	}

	// Update label.
	if err := st.UpdateAPITokenLabel(ctx, tok.UserID, tok.ID, "beta"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	again, _ := st.GetAPITokenByID(ctx, tok.UserID, tok.ID)
	if again.Label != "beta" {
		t.Fatalf("label not updated; got %q", again.Label)
	}

	// Update fields (scope).
	if err := st.UpdateAPITokenFields(ctx, tok.UserID, tok.ID, bson.M{"scope": models.TokenScopeRead}); err != nil {
		t.Fatalf("update fields: %v", err)
	}
	again, _ = st.GetAPITokenByID(ctx, tok.UserID, tok.ID)
	if again.Scope != models.TokenScopeRead {
		t.Fatalf("scope not updated; got %q", again.Scope)
	}

	// Touch (sets last_used_at).
	st.TouchAPIToken(ctx, tok.ID)

	// GetAPITokensByIDs (batch).
	m, _ := st.GetAPITokensByIDs(ctx, []string{tok.ID, "nope"})
	if m[tok.ID] == nil {
		t.Fatal("batch lookup missing token")
	}

	// Revoke.
	if err := st.RevokeAPIToken(ctx, tok.UserID, tok.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got, _ := st.GetAPITokenByHash(ctx, tok.Hash); got != nil {
		t.Fatal("revoked token should not be returned by hash lookup")
	}
}

func TestStoreIntegration_TokenEvents(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tokenID := uuid.NewString()
	for i := 0; i < 3; i++ {
		_ = st.LogTokenEvent(ctx, &models.TokenEvent{
			ID: uuid.NewString(), TokenID: tokenID,
			UserID: "u", Action: "comment.create",
			DocumentID: "d", At: time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
	}
	events, err := st.ListTokenEvents(ctx, tokenID, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("want 3, got %d", len(events))
	}

	// Limits get clamped to a sensible default when bogus.
	events, _ = st.ListTokenEvents(ctx, tokenID, 0)
	if len(events) != 3 {
		t.Fatalf("clamp default: got %d", len(events))
	}
}

func TestStoreIntegration_Notifications(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = st.InsertNotification(ctx, &models.Notification{
			ID: uuid.NewString(), UserID: "u-notif",
			Kind:          models.NotifyMention,
			DocumentID:    "d", DocumentTitle: "doc",
			CommentID: "c", ActorID: "a", ActorName: "A",
			Preview: "hi", CreatedAt: time.Now().UTC(),
		})
	}
	notes, unread, err := st.ListNotificationsForUser(ctx, "u-notif", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 5 || unread != 5 {
		t.Fatalf("notes=%d unread=%d", len(notes), unread)
	}

	// Mark one read.
	if err := st.MarkNotificationRead(ctx, "u-notif", notes[0].ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	_, unread, _ = st.ListNotificationsForUser(ctx, "u-notif", 10)
	if unread != 4 {
		t.Fatalf("want unread 4, got %d", unread)
	}

	// Mark all read.
	if err := st.MarkAllNotificationsRead(ctx, "u-notif"); err != nil {
		t.Fatalf("mark all: %v", err)
	}
	_, unread, _ = st.ListNotificationsForUser(ctx, "u-notif", 10)
	if unread != 0 {
		t.Fatalf("want 0; got %d", unread)
	}
}

func TestStoreIntegration_UsersByLogin(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	u1 := &models.User{
		ID: uuid.NewString(), GitHubID: 100, Login: "alice",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	u2 := &models.User{
		ID: uuid.NewString(), GitHubID: 101, Login: "bob",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = st.UpsertUserByGitHubID(ctx, u1)
	_ = st.UpsertUserByGitHubID(ctx, u2)

	users, err := st.FindUsersByLogins(ctx, []string{"alice", "carol"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("want 1, got %d", len(users))
	}
	if users[0].Login != "alice" {
		t.Fatalf("got %q", users[0].Login)
	}
}

func TestStoreIntegration_AnthropicKeyRoundtrip(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	uid := "u-secrets"
	if err := st.UpsertAnthropicKey(ctx, uid, "ct-1", "hint…"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _ := st.GetUserSecrets(ctx, uid)
	if got == nil || got.AnthropicKeyCiphertext != "ct-1" || got.AnthropicKeyHint != "hint…" {
		t.Fatalf("get: %+v", got)
	}

	if err := st.DeleteAnthropicKey(ctx, uid); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = st.GetUserSecrets(ctx, uid)
	if got != nil && got.AnthropicKeyCiphertext != "" {
		t.Fatalf("ciphertext should be cleared: %+v", got)
	}
}

func TestStoreIntegration_DocumentViewsAndUnread(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	uid := "u-view"
	docID := uuid.NewString()

	v, _ := st.GetDocumentView(ctx, docID, uid)
	if v != nil {
		t.Fatal("expected no view yet")
	}
	if err := st.RecordDocumentView(ctx, docID, uid); err != nil {
		t.Fatalf("record: %v", err)
	}
	v, _ = st.GetDocumentView(ctx, docID, uid)
	if v == nil {
		t.Fatal("expected view after RecordDocumentView")
	}
}

func TestStoreIntegration_DocumentChildrenAndDescendant(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := &models.Document{
		ID: uuid.NewString(), Title: "P", Origin: "upload",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	child := &models.Document{
		ID: uuid.NewString(), Title: "C", Origin: "upload", ParentID: parent.ID,
		CreatedAt: time.Now().UTC().Add(time.Second), UpdatedAt: time.Now().UTC().Add(time.Second),
	}
	grandchild := &models.Document{
		ID: uuid.NewString(), Title: "GC", Origin: "upload", ParentID: child.ID,
		CreatedAt: time.Now().UTC().Add(2 * time.Second), UpdatedAt: time.Now().UTC().Add(2 * time.Second),
	}
	_ = st.InsertDocument(ctx, parent)
	_ = st.InsertDocument(ctx, child)
	_ = st.InsertDocument(ctx, grandchild)

	children, _ := st.ListChildren(ctx, parent.ID)
	if len(children) != 1 || children[0].ID != child.ID {
		t.Fatalf("ListChildren: %+v", children)
	}

	latest, _ := st.LatestDescendant(ctx, parent.ID)
	if latest == nil || latest.ID != grandchild.ID {
		t.Fatalf("LatestDescendant: %+v", latest)
	}
}

func TestStoreIntegration_DistinctDocIDsForToken(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tokenID := uuid.NewString()
	for i, did := range []string{"d-a", "d-b", "d-a"} {
		_ = st.InsertComment(ctx, &models.Comment{
			ID: uuid.NewString(), DocumentID: did, Body: "c",
			TokenID: tokenID, ActorKind: models.ActorAgent,
			Anchor: models.Anchor{Start: 0, End: i + 1, Exact: "x"},
			Replies: []models.Reply{},
		})
	}
	docs, err := st.DistinctDocIDsForToken(ctx, tokenID)
	if err != nil {
		t.Fatalf("distinct: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 distinct (d-a, d-b); got %v", docs)
	}
}

func TestStoreIntegration_AuthState(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	state := &models.AuthState{
		ID: uuid.NewString(), Redirect: "/d/abc",
		CookieValue: "cv", CreatedAt: time.Now().UTC(),
	}
	if err := st.InsertAuthState(ctx, state); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := st.ConsumeAuthState(ctx, state.ID)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got == nil || got.Redirect != "/d/abc" {
		t.Fatalf("consume returned: %+v", got)
	}
	// Second consume should return nil (one-shot).
	again, _ := st.ConsumeAuthState(ctx, state.ID)
	if again != nil {
		t.Fatal("consume should be one-shot")
	}
}

func TestStoreIntegration_Sessions(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	sess := &models.Session{
		ID: uuid.NewString(), UserID: "u-sess",
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := st.InsertSession(ctx, sess); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, _ := st.GetSession(ctx, sess.ID)
	if got == nil {
		t.Fatal("missing session")
	}
	if err := st.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = st.GetSession(ctx, sess.ID)
	if got != nil {
		t.Fatal("expected session deleted")
	}
}

func TestStoreIntegration_ListDocumentsForUser(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	uid := "u-list"
	mine := &models.Document{
		ID: uuid.NewString(), Title: "mine", Origin: "upload",
		CreatedByID: uid,
		CreatedAt:   time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	other := &models.Document{
		ID: uuid.NewString(), Title: "other", Origin: "upload",
		CreatedByID: "someone-else",
		CreatedAt:   time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = st.InsertDocument(ctx, mine)
	_ = st.InsertDocument(ctx, other)
	// Add a comment by uid on the other doc → other should ALSO be listed.
	_ = st.InsertComment(ctx, &models.Comment{
		ID: uuid.NewString(), DocumentID: other.ID,
		AuthorID: uid, Body: "x",
		Anchor: models.Anchor{Start: 0, End: 1, Exact: "x"}, Replies: []models.Reply{},
	})

	list, err := st.ListDocumentsForUser(ctx, uid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d docs; want 2", len(list))
	}
}

func TestStoreIntegration_TrashForUser(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	uid := "u-trash"
	doc := &models.Document{
		ID: uuid.NewString(), Title: "tr", Origin: "upload",
		CreatedByID: uid,
		CreatedAt:   time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = st.InsertDocument(ctx, doc)
	_ = st.SoftDeleteDocument(ctx, doc.ID)
	trash, _ := st.ListTrashForUser(ctx, uid)
	if len(trash) != 1 {
		t.Fatalf("expected 1 trash item, got %d", len(trash))
	}
}

func TestStoreIntegration_StoreClose(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	t.Cleanup(cleanup)
	if err := st.Close(context.Background()); err != nil {
		// cleanup will also attempt Close; ignore errors there.
		_ = err
	}
}

// silence go vet about unused bson import on tooling-less builds
var _ bson.M
