package api_test

// Integration coverage for the drift-detection handlers in source.go.
// We cover the paths that DON'T require a real GitHub round-trip
// (auth gating, non-github doc short-circuits, drift-ignore on a
// pre-stamped doc). The full GitHub-fetch happy paths require a
// transport mock and are covered by the e2e suite.

import (
	"context"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/testutil"
)

// --- checkSourceNow ---

func TestCheckSourceNow_RequiresDoc(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, _ := doJSON(t, srv, "POST", "/api/documents/no-such-doc/check-source", nil, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d want 404", status)
	}
}

func TestCheckSourceNow_NonGitHubDocReturnsEmpty(t *testing.T) {
	// An upload-origin doc has no github metadata to check — handler
	// short-circuits and returns the (empty) drift state without
	// reaching out to GitHub.
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello upload")
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/check-source", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	// Non-github doc: sourceSha should be empty in the response.
	if !strings.Contains(string(body), `"sourceSha":""`) {
		t.Errorf("non-github doc should have empty sourceSha, got %s", body)
	}
}

// Note: for an "upload"-origin (non-github) doc, anonymous callers
// can reach the drift handlers because effectiveScope() treats
// missing token + no session as admin. The auth wall is in
// checkDocAccess + enforceScope — both of which a non-private
// upload doc passes unconditionally. That's by design: there's no
// notion of "owner" gating on docs the user uploaded by hand. So
// the "requires auth" tests here only exercise the github-private
// path implicitly (via checkDocAccess), and the auth response
// shape is covered separately in auth tests.

// --- ignoreDriftSource ---

func TestIgnoreDriftSource_NoDriftReturns400(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello")
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/drift/ignore", nil, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d body=%s want 400 (no drift to ignore)", status, body)
	}
}

func TestIgnoreDriftSource_StampsIgnoredSHA(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello")
	// Pre-stamp the doc with a "drifted" state so the handler has
	// something to dismiss.
	_, err := st.Documents().UpdateOne(context.Background(),
		bson.M{"_id": doc.ID},
		bson.M{"$set": bson.M{
			"source_sha":        "abc",
			"source_latest_sha": "def",
		}})
	if err != nil {
		t.Fatalf("seed drift: %v", err)
	}

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/drift/ignore", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}

	updated, _ := st.GetDocument(context.Background(), doc.ID)
	if updated == nil || updated.SourceDriftIgnoredSHA != "def" {
		t.Errorf("ignored sha not stamped: doc=%+v", updated)
	}
}

// --- syncDocumentSource ---

func TestSyncDocumentSource_NonGitHubReturns400(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello")
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/sync", nil, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d body=%s want 400 (non-github)", status, body)
	}
}

// --- mergePreviewSource / mergeAcceptSource ---

func TestMergePreviewSource_NonGitHubReturns400(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello")
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/merge-preview", nil, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d want 400 (non-github)", status)
	}
}

func TestMergeAcceptSource_NonGitHubReturns400(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello")
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/merge-accept", nil, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d want 400 (non-github)", status)
	}
}

// --- patchCommentAnchor (HTTP handler) ---

func TestPatchCommentAnchor_AnonymousIsForbidden(t *testing.T) {
	// Anonymous viewerID is "", which fails requireMineComment's
	// AuthorID equality with a 403 (not 401). The auth wall lives
	// in the ownership check, not enforceScope.
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "Hello", "first")
	status, _ := doJSON(t, srv, "PATCH", "/api/comments/"+c.ID+"/anchor",
		map[string]any{"docLevel": true})
	if status != 403 {
		t.Errorf("status=%d want 403 (anon viewer fails AuthorID equality)", status)
	}
}

func TestPatchCommentAnchor_DocLevelSucceeds(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "Hello", "first")

	status, body := doJSON(t, srv, "PATCH", "/api/comments/"+c.ID+"/anchor",
		map[string]any{"docLevel": true}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	updated, _ := st.GetComment(context.Background(), c.ID)
	if updated == nil {
		t.Fatal("comment vanished")
	}
	if updated.Anchor.Exact != "" || updated.Anchor.Start != 0 {
		t.Errorf("anchor not cleared after docLevel patch: %+v", updated.Anchor)
	}
}

func TestPatchCommentAnchor_NotAuthor_Forbidden(t *testing.T) {
	srv, st, _ := newTestServer(t)
	author := testutil.NewTestUser(t, st)
	other := testutil.NewTestUser(t, st)
	otherSess := testutil.NewTestSession(t, st, other.ID)
	doc := testutil.NewTestDocument(t, st, author.ID, "Hello world")
	c := testutil.NewTestComment(t, st, doc.ID, author.ID, "Hello", "first")

	status, _ := doJSON(t, srv, "PATCH", "/api/comments/"+c.ID+"/anchor",
		map[string]any{"docLevel": true}, withCookie(otherSess))
	if status != 403 {
		t.Errorf("status=%d want 403", status)
	}
}
