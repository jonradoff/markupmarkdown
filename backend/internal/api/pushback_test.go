package api_test

// Integration coverage for the pushback handlers. The full happy path
// involves five sequential GitHub round-trips (repo info, branch SHA,
// create branch, file meta, put file, create PR) and is exercised by
// the e2e suite. Here we cover the gates + the non-github branches
// for cheap, plus one mocked PR flow that exercises pushbackInfo +
// pushback PR-mode end-to-end.

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

// --- gates ---

func TestPushbackInfo_NonGitHubReturns400(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello upload")
	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/pushback/info", nil, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d body=%s want 400", status, body)
	}
}

func TestPushbackInfo_DocNotFound(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, _ := doJSON(t, srv, "GET", "/api/documents/no-such-doc/pushback/info", nil, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d want 404", status)
	}
}

func TestPushback_NonGitHubReturns400(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello upload")
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/pushback",
		map[string]string{"mode": "pr"}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d want 400 (non-github)", status)
	}
}

func TestPushback_BadModeReturns400(t *testing.T) {
	// GitHub-sourced doc but the request body has an unsupported mode.
	// The mock lets the doc-access check pass (returns public)
	// before we reach the mode validation.
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")
	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/pushback",
		map[string]string{"mode": "force-push", "targetBranch": "main"}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d body=%s want 400 for unsupported mode", status, body)
	}
	if !strings.Contains(string(body), "'pr' or 'direct'") {
		t.Errorf("body should mention valid modes: %s", body)
	}
}

// insertPushbackTestDoc creates a github-sourced doc so the pushback
// handlers reach the real codepath instead of short-circuiting at
// deriveGitHubInfo.
func insertPushbackTestDoc(t *testing.T, st interface {
	InsertDocument(ctx context.Context, d *models.Document) error
}, userID, owner, repo string) *models.Document {
	t.Helper()
	now := time.Now().UTC()
	d := &models.Document{
		ID:          uuid.NewString(),
		Title:       "Pushback Target",
		Origin:      "url",
		SourceURL:   "https://github.com/" + owner + "/" + repo + "/blob/main/README.md",
		Content:     "# Pushback Target\n",
		GitHubOwner: owner,
		GitHubRepo:  repo,
		GitHubRef:   "main",
		GitHubPath:  "README.md",
		CreatedByID: userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := st.InsertDocument(context.Background(), d); err != nil {
		t.Fatalf("insert pushback doc: %v", err)
	}
	return d
}

// --- mocked happy paths ---

// mockGitHubForPushback fakes the six endpoints the pushback flow
// touches, in order. Used for both pushbackInfo (only /repos/) and
// the full pushback PR flow.
func mockGitHubForPushback() func(*http.Request) *http.Response {
	return func(req *http.Request) *http.Response {
		host := req.URL.Host
		path := req.URL.Path
		method := req.Method
		if !strings.Contains(host, "api.github.com") {
			// Non-github outbound (raw.githubusercontent for the
			// public-fetch cache, etc.) — return 200-empty so
			// publicGitHubCheck doesn't flip the doc to private and
			// gate our pushback callers behind sign-in.
			return makeResp(200, "")
		}
		switch {
		case strings.Contains(path, "/git/ref/heads/"):
			return makeResp(200, `{"object":{"sha":"base-sha-1"}}`)
		case strings.HasSuffix(path, "/git/refs"):
			return makeResp(201, `{"ref":"refs/heads/agent/edit"}`)
		case strings.HasSuffix(path, "/pulls"):
			return makeResp(201, `{"number":42,"html_url":"https://github.com/owner/repo/pull/42"}`)
		case strings.Contains(path, "/contents/") && method == "GET":
			// FetchGitHubFileMeta: content is a base64 STRING here.
			return makeResp(200, `{"sha":"file-sha-1","content":"IyBoZWxsbwo=","encoding":"base64"}`)
		case strings.Contains(path, "/contents/") && method == "PUT":
			// PutFile: content is a STRUCT, with the new blob's SHA.
			return makeResp(200, `{"content":{"sha":"new-file-sha","html_url":""},"commit":{"sha":"new-commit-sha","html_url":"https://github.com/owner/repo/commit/new-commit-sha"}}`)
		case strings.HasPrefix(path, "/repos/owner/repo"):
			return makeResp(200, `{"default_branch":"main","html_url":"https://github.com/owner/repo","permissions":{"push":true,"admin":false,"maintain":false}}`)
		}
		return makeResp(200, "")
	}
}

func TestPushbackInfo_HappyPath(t *testing.T) {
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID+"/pushback/info", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"owner":"owner"`) {
		t.Errorf("body missing owner: %s", body)
	}
	if !strings.Contains(string(body), `"defaultBranch":"main"`) {
		t.Errorf("body missing defaultBranch: %s", body)
	}
	if !strings.Contains(string(body), `"canPushDirect":true`) {
		t.Errorf("body should reflect push permission: %s", body)
	}
}

func TestPushback_PRMode_OpensPR(t *testing.T) {
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/pushback",
		map[string]string{
			"mode":         "pr",
			"branch":       "agent/edit",
			"targetBranch": "main",
		}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"prNumber":42`) {
		t.Errorf("response missing pr number: %s", body)
	}
	if !strings.Contains(string(body), `"mode":"pr"`) {
		t.Errorf("response should declare mode: %s", body)
	}
}

func TestPushback_DirectMode_StampsSourceSHA(t *testing.T) {
	// Direct commit to the branch the doc tracks should stamp the
	// new blob SHA as the doc's SourceSHA, clearing drift.
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/pushback",
		map[string]string{
			"mode":         "direct",
			"targetBranch": "main",
		}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"mode":"direct"`) {
		t.Errorf("body should declare direct mode: %s", body)
	}
	// Persistence check: drift-clearing only happens when refsMatch.
	updated, _ := st.GetDocument(context.Background(), doc.ID)
	if updated == nil || updated.SourceSHA != "new-file-sha" {
		t.Errorf("expected source_sha stamped to new blob SHA, got %+v", updated)
	}
}
