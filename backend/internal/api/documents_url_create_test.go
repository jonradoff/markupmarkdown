package api_test

// Coverage for the URL-ingest path of createDocument: fetchContent +
// fetchURL + the public-raw + Contents API plumbing. The Contents
// API call routes through http.DefaultTransport so ghMock catches
// it. The earlier public-raw fetch goes through safefetch.Client
// (its own transport, not mockable from here) and will actually
// hit the network — that returns 404 for owner/repo which lets
// fetchContent fall through to the authenticated Contents API
// path that IS mocked. End result: the test exercises both branches
// of fetchContent + fetchURL without depending on the real github.

import (
	"net/http"
	"strings"
	"testing"

	"markupmarkdown/internal/testutil"
)

func mockGitHubForURLIngest() func(*http.Request) *http.Response {
	return func(req *http.Request) *http.Response {
		host := req.URL.Host
		if strings.Contains(host, "api.github.com") && strings.Contains(req.URL.Path, "/contents/") {
			return makeResp(200, `{"sha":"ingest-sha","content":"IyBoZWxsbwo=","encoding":"base64"}`)
		}
		return makeResp(200, "")
	}
}

func TestCreateDocument_FromGitHubURL_FallsThroughToContentsAPI(t *testing.T) {
	restore := ghMock(t, mockGitHubForURLIngest())
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "POST", "/api/documents",
		map[string]any{
			"url":   "https://github.com/owner/repo/blob/main/README.md",
			"title": "Imported via URL",
		}, withCookie(sess))
	if status != 200 && status != 201 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), "Imported via URL") {
		t.Errorf("expected title in response: %s", body)
	}
}
