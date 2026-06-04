package api_test

// Integration tests for private-doc access gating. Private docs are
// GitHub-sourced and re-verify GitHub access on every read; we mock the
// transport so a private doc the user lacks access to returns 403, and
// one they do have returns 200.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

type rtFn func(*http.Request) *http.Response

func (f rtFn) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r), nil
}

// ghMock installs a transport mock that intercepts non-loopback HTTP
// requests. Localhost / 127.0.0.1 traffic falls through to the previous
// transport so the test server's own connections (httptest.NewServer
// loopback) still work.
func ghMock(t *testing.T, h func(*http.Request) *http.Response) func() {
	t.Helper()
	prevClient := http.DefaultClient.Transport
	prevDefault := http.DefaultTransport
	passthrough := prevDefault
	if passthrough == nil {
		passthrough = http.DefaultTransport // fall back to real default
	}
	rt := rtFn(func(req *http.Request) *http.Response {
		host := req.URL.Hostname()
		if host == "127.0.0.1" || host == "localhost" || host == "::1" {
			// Let the real loopback request through so httptest works.
			res, _ := passthrough.RoundTrip(req)
			return res
		}
		return h(req)
	})
	http.DefaultClient.Transport = rt
	http.DefaultTransport = rt
	return func() {
		http.DefaultClient.Transport = prevClient
		http.DefaultTransport = prevDefault
	}
}

func makeResp(status int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(status)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

func newPrivateDoc(t *testing.T, st interface {
	InsertDocument(ctx context.Context, d *models.Document) error
}, userID, owner, repo string) *models.Document {
	t.Helper()
	now := time.Now().UTC()
	d := &models.Document{
		ID: uuid.NewString(), Title: "Private", Origin: "url",
		Private: true, GitHubOwner: owner, GitHubRepo: repo,
		CreatedByID: userID,
		CreatedAt:   now, UpdatedAt: now,
	}
	if err := st.InsertDocument(context.Background(), d); err != nil {
		t.Fatalf("insert private doc: %v", err)
	}
	return d
}

func TestPrivateDoc_AccessAllowed(t *testing.T) {
	restore := ghMock(t, func(req *http.Request) *http.Response {
		return makeResp(200, `{"id":1}`) // CheckRepoAccess
	})
	t.Cleanup(restore)

	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := newPrivateDoc(t, st, user.ID, "owner-x", "repo-y")

	status, _ := doJSON(t, srv, "GET", "/api/documents/"+doc.ID, nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d, want 200 (access allowed)", status)
	}
}

func TestPrivateDoc_AccessDenied(t *testing.T) {
	restore := ghMock(t, func(req *http.Request) *http.Response {
		return makeResp(404, "{}") // GitHub answers "no such repo"
	})
	t.Cleanup(restore)

	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := newPrivateDoc(t, st, user.ID, "owner-z", "repo-z")

	status, _ := doJSON(t, srv, "GET", "/api/documents/"+doc.ID, nil, withCookie(sess))
	if status != 403 {
		t.Fatalf("status=%d, want 403 (no github access)", status)
	}
}

func TestPrivateDoc_AnonymousReturns401WithSignInAction(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := newPrivateDoc(t, st, user.ID, "o", "r")

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID, nil)
	if status != 401 {
		t.Fatalf("status=%d body=%s", status, body)
	}
}

func TestPrivateDoc_RestoreRejectsLostAccess(t *testing.T) {
	restore := ghMock(t, func(req *http.Request) *http.Response {
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)

	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := newPrivateDoc(t, st, user.ID, "o", "r")
	_ = st.SoftDeleteDocument(context.Background(), doc.ID)

	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/restore", nil, withCookie(sess))
	if status != 403 {
		t.Fatalf("status=%d, want 403 (lost github access)", status)
	}
}

func TestPrivateDoc_TrashHidesDocsWithLostAccess(t *testing.T) {
	restore := ghMock(t, func(req *http.Request) *http.Response {
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)

	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := newPrivateDoc(t, st, user.ID, "o", "r")
	_ = st.SoftDeleteDocument(context.Background(), doc.ID)

	status, body := doJSON(t, srv, "GET", "/api/me/trash", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	// Should be empty — private doc filtered out.
	if string(body) != "[]\n" {
		t.Fatalf("expected empty array, got %s", body)
	}
}

func TestPrivateDoc_ListDocumentsHidesLostAccess(t *testing.T) {
	restore := ghMock(t, func(req *http.Request) *http.Response {
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)

	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	_ = newPrivateDoc(t, st, user.ID, "o", "r")

	status, body := doJSON(t, srv, "GET", "/api/documents", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	if string(body) != "[]\n" {
		t.Fatalf("expected empty (no-access docs hidden), got %s", body)
	}
}
