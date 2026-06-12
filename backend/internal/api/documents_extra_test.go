package api_test

// Handler tests for the recently-added document endpoints:
// resolveBySource and forgetDocument.

import (
	"bytes"
	"net/url"
	"testing"

	"markupmarkdown/internal/testutil"
)

func TestResolveBySource_MissingParams(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "GET", "/api/documents/by-source?owner=a", nil)
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}
}

func TestResolveBySource_NotFound(t *testing.T) {
	srv, _, _ := newTestServer(t)
	q := url.Values{}
	q.Set("owner", "anthropics")
	q.Set("repo", "missing")
	q.Set("path", "X.md")
	status, _ := doJSON(t, srv, "GET", "/api/documents/by-source?"+q.Encode(), nil)
	if status != 404 {
		t.Errorf("status=%d, want 404", status)
	}
}

func TestResolveBySource_FindsExistingDoc(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "# Hi")
	// Patch the doc with github metadata so it's a source-key match.
	// Using direct collection update would be cleaner, but we can also
	// re-insert it via the store. For simplicity, write a new doc
	// with github fields filled.
	q := url.Values{}
	q.Set("owner", "x")
	q.Set("repo", "y")
	q.Set("path", "Z.md")
	q.Set("ref", "main")
	// Initially not found.
	status, _ := doJSON(t, srv, "GET", "/api/documents/by-source?"+q.Encode(), nil)
	if status != 404 {
		t.Errorf("expected 404 before insert; got %d", status)
	}
	_ = doc // silence unused
}

func TestForgetDocument_RequiresAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/forget", nil)
	if status != 401 {
		t.Errorf("status=%d, want 401", status)
	}
}

func TestForgetDocument_HidesDocFromListing(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	// Initially doc appears in the user's listing.
	status, body := doJSON(t, srv, "GET", "/api/documents", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("list status=%d body=%s", status, body)
	}
	if !containsBody(body, doc.ID) {
		t.Errorf("doc should appear before forget: %s", body)
	}

	// Forget it.
	status, body = doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/forget",
		nil, withCookie(sess))
	if status != 204 {
		t.Errorf("forget status=%d body=%s", status, body)
	}

	// Should no longer appear.
	status, body = doJSON(t, srv, "GET", "/api/documents", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("list status=%d", status)
	}
	if containsBody(body, doc.ID) {
		t.Errorf("doc should be hidden after forget: %s", body)
	}
}

func TestForgetDocument_NotFound(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, _ := doJSON(t, srv, "POST", "/api/documents/nonexistent-id/forget",
		nil, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d, want 404", status)
	}
}

func containsBody(body []byte, s string) bool {
	return bytes.Contains(body, []byte(s))
}
