package api_test

// Integration tests for the one-click suggestion-apply flow (P0-2).
// Covers the happy path (creates a revision, resolves the comment,
// stamps the suggestion) and the guard rails (no suggestion attached,
// already applied, anchor no longer present, no-op replacement).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func insertSuggestionComment(t *testing.T, st interface {
	InsertComment(ctx context.Context, c *models.Comment) error
}, docID, authorID, anchorExact, replacement string) *models.Comment {
	t.Helper()
	now := time.Now().UTC()
	c := &models.Comment{
		ID:         uuid.NewString(),
		DocumentID: docID,
		Anchor:     models.Anchor{Exact: anchorExact},
		Author:     "reviewer",
		AuthorID:   authorID,
		ActorKind:  models.ActorHuman,
		Body:       "Consider rewording this.",
		Replies:    []models.Reply{},
		Suggestion: &models.Suggestion{Replacement: replacement},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := st.InsertComment(context.Background(), c); err != nil {
		t.Fatalf("insert suggestion comment: %v", err)
	}
	return c
}

func TestApplySuggestion_HappyPath(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID,
		"# Title\n\nThe quick brown fox jumps.\n")
	comment := insertSuggestionComment(t, st, doc.ID, user.ID,
		"quick brown fox", "swift auburn hare")

	status, body := doJSON(t, srv, "POST",
		"/api/comments/"+comment.ID+"/apply-suggestion", nil, withCookie(sess))
	if status != 201 {
		t.Fatalf("status=%d body=%s want 201", status, body)
	}

	// New child doc exists with the substitution applied.
	if !strings.Contains(string(body), "swift auburn hare") {
		t.Errorf("expected replacement in child doc, got %s", body)
	}
	if !strings.Contains(string(body), `"model":"suggestion"`) {
		t.Errorf("expected revision_meta.model=suggestion, got %s", body)
	}

	// Comment is stamped as applied + resolved.
	updated, _ := st.GetComment(context.Background(), comment.ID)
	if updated == nil {
		t.Fatal("comment vanished")
	}
	if updated.Suggestion == nil || updated.Suggestion.AppliedAt == nil {
		t.Errorf("suggestion.applied_at not stamped: %+v", updated.Suggestion)
	}
	if !updated.Resolved {
		t.Errorf("comment not resolved after apply")
	}
}

func TestApplySuggestion_NoSuggestionAttached(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "hello world")
	// Plain comment (no suggestion).
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "hello", "just prose")
	status, body := doJSON(t, srv, "POST",
		"/api/comments/"+c.ID+"/apply-suggestion", nil, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d body=%s want 400", status, body)
	}
}

func TestApplySuggestion_AlreadyAppliedReturns409(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "the quick fox")
	c := insertSuggestionComment(t, st, doc.ID, user.ID, "quick fox", "slow tortoise")

	// Manually stamp AppliedAt so we hit the guard.
	now := time.Now().UTC()
	_, _ = st.Comments().UpdateOne(context.Background(),
		bson.M{"_id": c.ID},
		bson.M{"$set": bson.M{"suggestion.applied_at": now}})

	status, _ := doJSON(t, srv, "POST",
		"/api/comments/"+c.ID+"/apply-suggestion", nil, withCookie(sess))
	if status != 409 {
		t.Errorf("status=%d want 409 for already-applied", status)
	}
}

func TestApplySuggestion_AnchorMissingReturns422(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "nothing matches here")
	c := insertSuggestionComment(t, st, doc.ID, user.ID, "does not exist", "replacement")
	status, _ := doJSON(t, srv, "POST",
		"/api/comments/"+c.ID+"/apply-suggestion", nil, withCookie(sess))
	if status != 422 {
		t.Errorf("status=%d want 422 for missing anchor", status)
	}
}

func TestApplySuggestion_NoOpReplacementReturns400(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "hello world")
	c := insertSuggestionComment(t, st, doc.ID, user.ID, "hello", "hello") // identical
	status, _ := doJSON(t, srv, "POST",
		"/api/comments/"+c.ID+"/apply-suggestion", nil, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d want 400 for no-op replacement", status)
	}
}
