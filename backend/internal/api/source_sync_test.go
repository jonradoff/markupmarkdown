package api_test

// Tests for syncDocumentSource — the "pull latest upstream + re-anchor
// comments" handler. Mocks GitHub so we can drive both the clean and
// orphan branches of the re-anchor pass without going to the wire.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"markupmarkdown/internal/testutil"
)

// mockGitHubForSync returns mocks that satisfy
// publicGitHubCheck (so the doc stays public, no access wall) +
// FetchGitHubFileMeta (so the sync handler has fresh content to
// re-anchor against). newContent is what the mocked Contents API
// returns as the upstream body.
func mockGitHubForSync(newContent string) func(*http.Request) *http.Response {
	// Base64 of newContent for the Contents API.
	b64 := toBase64(newContent)
	return func(req *http.Request) *http.Response {
		host := req.URL.Host
		path := req.URL.Path
		if !strings.Contains(host, "api.github.com") {
			return makeResp(200, "")
		}
		switch {
		case strings.Contains(path, "/contents/") && req.Method == "GET":
			return makeResp(200, `{"sha":"new-upstream-sha","content":"`+b64+`","encoding":"base64"}`)
		case strings.HasPrefix(path, "/repos/owner/repo"):
			return makeResp(200, `{"default_branch":"main","permissions":{"push":true}}`)
		}
		return makeResp(200, "")
	}
}

func toBase64(s string) string {
	// std encoding, no padding-stripping. Inline so we don't reach
	// for encoding/base64 in this small helper file.
	const tbl = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	out := make([]byte, 0, ((len(s)+2)/3)*4)
	for i := 0; i < len(s); i += 3 {
		var n uint32
		l := 3
		if i+3 > len(s) {
			l = len(s) - i
		}
		for j := 0; j < l; j++ {
			n |= uint32(s[i+j]) << (16 - 8*j)
		}
		out = append(out, tbl[(n>>18)&0x3f])
		out = append(out, tbl[(n>>12)&0x3f])
		if l > 1 {
			out = append(out, tbl[(n>>6)&0x3f])
		} else {
			out = append(out, '=')
		}
		if l > 2 {
			out = append(out, tbl[n&0x3f])
		} else {
			out = append(out, '=')
		}
	}
	return string(out)
}

func TestSyncDocumentSource_HappyPath_ReanchorsAndPersists(t *testing.T) {
	// Upstream returns a refreshed body that still contains the
	// comment's anchor text → reanchor=clean. Doc content is updated,
	// source_sha is updated, response carries cleanCount=1.
	restore := ghMock(t, mockGitHubForSync("# Hello world\n\nMore body added upstream.\n"))
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertGistlessGitHubDoc(t, st, user.ID, "owner", "repo", "# Hello world\n")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "Hello world", "noting this header")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/sync", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"cleanCount":1`) {
		t.Errorf("expected cleanCount=1, got %s", body)
	}
	if !strings.Contains(string(body), `"orphanCount":0`) {
		t.Errorf("expected orphanCount=0, got %s", body)
	}

	updated, _ := st.GetDocument(context.Background(), doc.ID)
	if updated == nil {
		t.Fatal("doc vanished")
	}
	if updated.SourceSHA != "new-upstream-sha" {
		t.Errorf("source_sha=%q want new-upstream-sha", updated.SourceSHA)
	}
	if !strings.Contains(updated.Content, "More body added upstream") {
		t.Errorf("content not synced from upstream: %q", updated.Content)
	}
	// Sanity: the anchored comment is still present, anchor.exact is
	// preserved or refreshed (the handler may zero start/end and
	// keep exact intact).
	updatedComment, _ := st.GetComment(context.Background(), c.ID)
	if updatedComment == nil {
		t.Errorf("comment vanished after sync")
	}
}

func TestSyncDocumentSource_OrphansCommentWhenAnchorGone(t *testing.T) {
	// Upstream returns content that no longer contains the comment's
	// anchor → reanchor=orphan. Comment gets orphan=true stamped.
	restore := ghMock(t, mockGitHubForSync("# completely new content\n\nno matching text here.\n"))
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertGistlessGitHubDoc(t, st, user.ID, "owner", "repo", "# Old heading text\n")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "Old heading text", "anchored to a heading that's about to disappear")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/sync", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"orphanCount":1`) {
		t.Errorf("expected orphanCount=1, got %s", body)
	}

	updatedComment, _ := st.GetComment(context.Background(), c.ID)
	if updatedComment == nil {
		t.Fatal("comment vanished")
	}
	if !updatedComment.Orphan {
		t.Errorf("expected orphan=true after anchor disappeared, got %+v", updatedComment)
	}
}
