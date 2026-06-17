package api_test

// Integration coverage for the createIndex handler — the entry
// point exercised when a user pastes a GitHub URL into the home
// form. Uses ghMock to short-circuit the outbound GitHub calls so
// each test stays under a few seconds.

import (
	"net/http"
	"strings"
	"testing"

	"markupmarkdown/internal/testutil"
)

// mockGitHubForIndex returns a transport handler that fakes the
// minimum GitHub responses createIndex + materialize need:
//   - GET /users/{name} → returns User (so it's classified as user)
//   - GET /repos/{owner}/{repo} → 200 (repo exists / access ok)
//   - GET /users/{owner}/repos → empty list (no repos to scan)
//   - any other URL → 200 with empty body
func mockGitHubForIndex(accountType string) func(*http.Request) *http.Response {
	return func(req *http.Request) *http.Response {
		host := req.URL.Host
		path := req.URL.Path
		if strings.Contains(host, "api.github.com") {
			switch {
			case strings.HasPrefix(path, "/users/") && !strings.Contains(path, "/repos"):
				return makeResp(200, `{"login":"someone","type":"`+accountType+`"}`)
			case strings.HasPrefix(path, "/repos/"):
				return makeResp(200, `{"id":1,"default_branch":"main"}`)
			case strings.Contains(path, "/repos"):
				// listUserRepos / listOrgRepos paginated endpoint.
				return makeResp(200, `[]`)
			}
		}
		return makeResp(200, "")
	}
}

func TestCreateIndex_RequiresAuth_Coverage(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "POST", "/api/indexes",
		map[string]string{"url": "https://github.com/anthropics"})
	if status != 401 {
		t.Errorf("status=%d want 401 (no session)", status)
	}
}

func TestCreateIndex_BadURL_Coverage(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, body := doJSON(t, srv, "POST", "/api/indexes",
		map[string]string{"url": "not a url"}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d body=%s want 400", status, body)
	}
}

func TestCreateIndex_UserIndex(t *testing.T) {
	restore := ghMock(t, mockGitHubForIndex("User"))
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, body := doJSON(t, srv, "POST", "/api/indexes",
		map[string]string{"url": "https://github.com/someone"}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"kind":"user"`) {
		t.Errorf("expected kind=user in body, got %s", body)
	}
}

func TestCreateIndex_OrgIndex(t *testing.T) {
	restore := ghMock(t, mockGitHubForIndex("Organization"))
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, body := doJSON(t, srv, "POST", "/api/indexes",
		map[string]string{"url": "https://github.com/anthropics"}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"kind":"org"`) {
		t.Errorf("expected kind=org in body, got %s", body)
	}
}

func TestCreateIndex_RepoIndex(t *testing.T) {
	restore := ghMock(t, mockGitHubForIndex("User"))
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, body := doJSON(t, srv, "POST", "/api/indexes",
		map[string]string{"url": "https://github.com/owner/repo"}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"kind":"repo"`) {
		t.Errorf("expected kind=repo in body, got %s", body)
	}
}

func TestCreateIndex_DedupGlobalSameSource(t *testing.T) {
	restore := ghMock(t, mockGitHubForIndex("Organization"))
	defer restore()
	srv, st, _ := newTestServer(t)
	user1 := testutil.NewTestUser(t, st)
	sess1 := testutil.NewTestSession(t, st, user1.ID)
	user2 := testutil.NewTestUser(t, st)
	sess2 := testutil.NewTestSession(t, st, user2.ID)

	// User 1 creates beamable index.
	status1, body1 := doJSON(t, srv, "POST", "/api/indexes",
		map[string]string{"url": "https://github.com/beamable"}, withCookie(sess1))
	if status1 != 200 {
		t.Fatalf("user1 status=%d body=%s", status1, body1)
	}
	// User 2 creates the SAME source → must get the same id (global dedup).
	status2, body2 := doJSON(t, srv, "POST", "/api/indexes",
		map[string]string{"url": "https://github.com/beamable"}, withCookie(sess2))
	if status2 != 200 {
		t.Fatalf("user2 status=%d body=%s", status2, body2)
	}
	// Both responses should contain the same id.
	id1 := extractField(t, body1, "id")
	id2 := extractField(t, body2, "id")
	if id1 != id2 || id1 == "" {
		t.Errorf("expected same canonical id from both users (global dedup), got user1=%q user2=%q", id1, id2)
	}
}

// extractField pulls a top-level string field from a JSON body. Used
// instead of full struct decode for tests that only need 1-2 fields.
func extractField(t *testing.T, body []byte, key string) string {
	t.Helper()
	needle := `"` + key + `":"`
	i := strings.Index(string(body), needle)
	if i < 0 {
		return ""
	}
	rest := string(body[i+len(needle):])
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}
