package testutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/store"
)

// NewTestUser inserts a new fully-populated User and returns it. Use this
// whenever a test needs an authenticated identity.
func NewTestUser(t *testing.T, st *store.Store) *models.User {
	t.Helper()
	now := time.Now().UTC()
	u := &models.User{
		ID:          uuid.NewString(),
		GitHubID:    int64(now.UnixNano() & 0x7fffffff),
		Login:       "user-" + uuid.NewString()[:8],
		Name:        "Test User",
		Email:       "user@example.test",
		AvatarURL:   "https://example.test/avatar.png",
		AccessToken: "fake-gh-token",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := st.UpsertUserByGitHubID(context.Background(), u); err != nil {
		t.Fatalf("testutil: insert user: %v", err)
	}
	return u
}

// NewTestSession opens a session for the given user and returns the
// session ID (which is what mm_session cookie contains).
func NewTestSession(t *testing.T, st *store.Store, userID string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC()
	if err := st.InsertSession(context.Background(), &models.Session{
		ID:        id,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("testutil: insert session: %v", err)
	}
	return id
}

// SessionCookieFor returns a ready-to-attach *http.Cookie for `mm_session`.
func SessionCookieFor(sessionID string) *http.Cookie {
	return &http.Cookie{Name: "mm_session", Value: sessionID, Path: "/"}
}

// NewAPIToken mints a token for the given user at the given scope and
// returns (plaintext, *APIToken). The plaintext is what an agent would
// send in Authorization: Bearer.
func NewAPIToken(t *testing.T, st *store.Store, userID string, scope models.TokenScope) (string, *models.APIToken) {
	t.Helper()
	plaintext := "mmk_" + uuid.NewString() + uuid.NewString() // > 32 chars
	rec := &models.APIToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		Hash:      api.HashToken(plaintext),
		Prefix:    plaintext[:12] + "…",
		Label:     "test-token",
		Scope:     scope,
		CreatedAt: time.Now().UTC(),
	}
	if err := st.InsertAPIToken(context.Background(), rec); err != nil {
		t.Fatalf("testutil: insert token: %v", err)
	}
	return plaintext, rec
}

// NewTestDocument inserts a public doc owned by userID and returns it.
func NewTestDocument(t *testing.T, st *store.Store, userID, content string) *models.Document {
	t.Helper()
	if content == "" {
		content = "# Hello\n\nThis is a test document.\n"
	}
	now := time.Now().UTC()
	d := &models.Document{
		ID:          uuid.NewString(),
		Title:       "Test Document",
		Origin:      "upload",
		Content:     content,
		CreatedByID: userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := st.InsertDocument(context.Background(), d); err != nil {
		t.Fatalf("testutil: insert document: %v", err)
	}
	return d
}

// NewTestComment inserts a human-authored comment on docID by authorID
// anchored at the literal string `quoted`.
func NewTestComment(t *testing.T, st *store.Store, docID, authorID, quoted, body string) *models.Comment {
	t.Helper()
	if quoted == "" {
		quoted = "Hello"
	}
	if body == "" {
		body = "a test comment"
	}
	now := time.Now().UTC()
	c := &models.Comment{
		ID:         uuid.NewString(),
		DocumentID: docID,
		Anchor:     models.Anchor{Start: 0, End: len(quoted), Exact: quoted},
		Author:     "Test User",
		AuthorID:   authorID,
		ActorKind:  models.ActorHuman,
		Body:       body,
		Replies:    []models.Reply{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := st.InsertComment(context.Background(), c); err != nil {
		t.Fatalf("testutil: insert comment: %v", err)
	}
	return c
}

// AsUser returns a request modifier that attaches a session cookie for
// the given session ID. Compose with http requests via:
//
//	req := httptest.NewRequest(...); testutil.AsUser(req, sessionID)
func AsUser(r *http.Request, sessionID string) *http.Request {
	r.AddCookie(SessionCookieFor(sessionID))
	return r
}

// AsToken sets the Authorization: Bearer header for the given plaintext
// token. Returns the same request for fluent chaining.
func AsToken(r *http.Request, plaintext string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+plaintext)
	return r
}

// NewRecorder is a tiny wrapper around httptest.NewRecorder that returns
// the recorder along with a helper to decode JSON into a destination.
func NewRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}
