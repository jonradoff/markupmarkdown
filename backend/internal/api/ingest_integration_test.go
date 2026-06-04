package api_test

import (
	"strings"
	"testing"

	"markupmarkdown/internal/testutil"
)

func TestSelfDocRedirect_PastedOurOwnURL(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	// The test config sets Frontend.URL = http://localhost:4720.
	status, body := doJSON(t, srv, "POST", "/api/documents", map[string]string{
		"url": "http://localhost:4720/d/some-other-doc-id",
	}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s, want 200 (self-doc redirect)", status, body)
	}
	if !strings.Contains(string(body), "self_doc_redirect") {
		t.Errorf("expected kind=self_doc_redirect; got %s", body)
	}
	if !strings.Contains(string(body), "/d/some-other-doc-id") {
		t.Errorf("expected redirect path; got %s", body)
	}
}

func TestSelfDocRedirect_StripsTrailingPunctuation(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	// Trailing "." should be stripped BEFORE the self-doc check so
	// pasting "http://localhost:4720/d/abc." also redirects cleanly.
	status, body := doJSON(t, srv, "POST", "/api/documents", map[string]string{
		"url": "http://localhost:4720/d/abc.",
	}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"documentId":"abc"`) {
		t.Errorf("expected documentId=abc (period stripped); got %s", body)
	}
}
