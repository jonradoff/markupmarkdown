package auth

// Tests for the index-listing helpers added to github.go for the
// markdown-index feature. All use the existing mockRoundTripper installed
// against http.DefaultTransport so the per-call &http.Client{Timeout:...}
// constructors still flow through our handler.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestLookupAccount_User(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		if !strings.HasSuffix(req.URL.Path, "/users/anthropics") {
			t.Errorf("unexpected path %q", req.URL.Path)
		}
		b, _ := json.Marshal(AccountInfo{
			Login: "anthropics", Type: AccountKindOrg, HTMLURL: "https://github.com/anthropics",
		})
		return makeResp(200, string(b))
	})
	t.Cleanup(restore)

	info, err := LookupAccount(context.Background(), "tok", "anthropics")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if info.Type != AccountKindOrg {
		t.Errorf("got type %q, want Organization", info.Type)
	}
}

func TestLookupAccount_NotFound(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(http.StatusNotFound, "{}")
	})
	t.Cleanup(restore)
	if _, err := LookupAccount(context.Background(), "tok", "nope"); err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestListOrgRepos_PaginatesUntilShortPage(t *testing.T) {
	// First page returns 100 (full), second page returns 2 (short → stop).
	calls := 0
	restore := installMockTransport(func(req *http.Request) *http.Response {
		calls++
		if !strings.Contains(req.URL.Path, "/orgs/anthropics/repos") {
			t.Errorf("unexpected path %q", req.URL.Path)
		}
		var batch []RepoSummary
		switch calls {
		case 1:
			for i := 0; i < 100; i++ {
				batch = append(batch, RepoSummary{ID: int64(i), Name: "r"})
			}
		case 2:
			batch = []RepoSummary{{ID: 200}, {ID: 201}}
		default:
			t.Errorf("unexpected extra call %d", calls)
			batch = nil
		}
		b, _ := json.Marshal(batch)
		return makeResp(200, string(b))
	})
	t.Cleanup(restore)

	repos, err := ListOrgRepos(context.Background(), "tok", "anthropics")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(repos) != 102 {
		t.Errorf("got %d, want 102", len(repos))
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (page 1 full + page 2 short); got %d", calls)
	}
}

func TestListOrgRepos_StopsOnEmptyPage(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(200, "[]")
	})
	t.Cleanup(restore)
	got, err := ListOrgRepos(context.Background(), "tok", "anthropics")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d", len(got))
	}
}

func TestListOrgRepos_PropagatesError(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(500, `{"message":"boom"}`)
	})
	t.Cleanup(restore)
	if _, err := ListOrgRepos(context.Background(), "tok", "anthropics"); err == nil {
		t.Fatal("expected error")
	}
}

func TestListUserRepos_ViewerIsOwner_FoldsPrivate(t *testing.T) {
	// When viewer == owner, the function also calls /user/repos with
	// affiliation=owner&visibility=private and folds those in.
	calls := map[string]int{}
	restore := installMockTransport(func(req *http.Request) *http.Response {
		key := req.URL.Path
		calls[key]++
		switch {
		case strings.HasSuffix(req.URL.Path, "/users/alice/repos"):
			batch := []RepoSummary{{ID: 1, Name: "public", FullName: "alice/public"}}
			b, _ := json.Marshal(batch)
			return makeResp(200, string(b))
		case strings.HasSuffix(req.URL.Path, "/user/repos"):
			batch := []RepoSummary{
				{ID: 1, Name: "public", FullName: "alice/public"}, // dup → dedup
				{ID: 2, Name: "private", FullName: "alice/private", Private: true},
			}
			b, _ := json.Marshal(batch)
			return makeResp(200, string(b))
		}
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)

	repos, err := ListUserRepos(context.Background(), "tok", "alice", "alice")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Expect 2 — public (dedup) + private.
	if len(repos) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(repos), repos)
	}
	var sawPrivate bool
	for _, r := range repos {
		if r.Private {
			sawPrivate = true
		}
	}
	if !sawPrivate {
		t.Error("private repo should be in the folded result")
	}
}

func TestListUserRepos_ViewerIsNotOwner_PublicOnly(t *testing.T) {
	calls := map[string]int{}
	restore := installMockTransport(func(req *http.Request) *http.Response {
		calls[req.URL.Path]++
		if strings.HasSuffix(req.URL.Path, "/users/alice/repos") {
			batch := []RepoSummary{{ID: 1, Name: "public", FullName: "alice/public"}}
			b, _ := json.Marshal(batch)
			return makeResp(200, string(b))
		}
		t.Errorf("non-owner viewer should not hit %q", req.URL.Path)
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)
	repos, err := ListUserRepos(context.Background(), "tok", "alice", "bob")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("got %d", len(repos))
	}
}

func TestListUserRepos_NoToken_PublicOnly(t *testing.T) {
	// Even when viewerLogin matches owner, no token means we can't hit
	// /user/repos — the function should skip the private fold.
	calls := map[string]int{}
	restore := installMockTransport(func(req *http.Request) *http.Response {
		calls[req.URL.Path]++
		if strings.HasSuffix(req.URL.Path, "/users/alice/repos") {
			b, _ := json.Marshal([]RepoSummary{{ID: 1}})
			return makeResp(200, string(b))
		}
		t.Errorf("anonymous viewer should not hit %q", req.URL.Path)
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)
	if _, err := ListUserRepos(context.Background(), "", "alice", "alice"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestListRepoMarkdownFiles_Success(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		switch {
		case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
			return makeResp(200, `{"object":{"sha":"deadbeef"}}`)
		case strings.Contains(req.URL.Path, "/git/trees/"):
			body := `{
				"sha": "deadbeef",
				"truncated": false,
				"tree": [
					{"path":"README.md","type":"blob","sha":"1","size":10},
					{"path":"docs/guide.markdown","type":"blob","sha":"2","size":20},
					{"path":"src/code.go","type":"blob","sha":"3","size":30},
					{"path":"docs","type":"tree","sha":"4"}
				]
			}`
			return makeResp(200, body)
		}
		t.Errorf("unexpected path %q", req.URL.Path)
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)
	files, trunc, err := ListRepoMarkdownFiles(context.Background(), "tok", "a", "b", "main")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if trunc {
		t.Error("not truncated")
	}
	if len(files) != 2 {
		t.Fatalf("got %d, want 2", len(files))
	}
	// Order preserved from tree response.
	if files[0].Path != "README.md" || files[1].Path != "docs/guide.markdown" {
		t.Errorf("paths wrong: %+v", files)
	}
}

func TestListRepoMarkdownFiles_TruncatedFlag(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		switch {
		case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
			return makeResp(200, `{"object":{"sha":"dead"}}`)
		case strings.Contains(req.URL.Path, "/git/trees/"):
			return makeResp(200, `{"truncated":true,"tree":[{"path":"X.md","type":"blob"}]}`)
		}
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)
	files, trunc, err := ListRepoMarkdownFiles(context.Background(), "tok", "a", "b", "main")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !trunc {
		t.Error("expected truncated=true")
	}
	if len(files) != 1 {
		t.Errorf("got %d", len(files))
	}
}

func TestListRepoMarkdownFiles_EmptyRef_ResolvesDefault(t *testing.T) {
	calls := 0
	restore := installMockTransport(func(req *http.Request) *http.Response {
		calls++
		switch {
		case strings.HasSuffix(req.URL.Path, "/repos/a/b"):
			return makeResp(200, `{"default_branch":"main"}`)
		case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
			return makeResp(200, `{"object":{"sha":"x"}}`)
		case strings.Contains(req.URL.Path, "/git/trees/"):
			return makeResp(200, `{"tree":[]}`)
		}
		t.Errorf("unexpected path %q", req.URL.Path)
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)
	_, _, err := ListRepoMarkdownFiles(context.Background(), "tok", "a", "b", "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Repo info + branch SHA + tree = 3 calls.
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestListRepoMarkdownFiles_BranchFetchError(t *testing.T) {
	// GetBranchSHA uses getJSON (not fetchGitHubJSON), so non-2xx
	// responses surface as a plain fmt.Errorf — not a *FetchError —
	// and the special-case empty-repo branch in ListRepoMarkdownFiles
	// does NOT fire. Error propagates.
	restore := installMockTransport(func(req *http.Request) *http.Response {
		if strings.HasSuffix(req.URL.Path, "/git/ref/heads/main") {
			return makeResp(http.StatusNotFound, `{"message":"Not Found"}`)
		}
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)
	if _, _, err := ListRepoMarkdownFiles(context.Background(), "tok", "a", "b", "main"); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestListRepoTopLevelMarkdown_FiltersSubdirs(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		switch {
		case strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
			return makeResp(200, `{"object":{"sha":"x"}}`)
		case strings.Contains(req.URL.Path, "/git/trees/"):
			body := `{
				"tree": [
					{"path":"README.md","type":"blob"},
					{"path":"CONTRIBUTING.md","type":"blob"},
					{"path":"docs/inner.md","type":"blob"},
					{"path":"sub/dir/nested.markdown","type":"blob"}
				]
			}`
			return makeResp(200, body)
		}
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)
	files, err := ListRepoTopLevelMarkdown(context.Background(), "tok", "a", "b", "main")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d, want 2 (only root-level files)", len(files))
	}
	for _, f := range files {
		if strings.Contains(f.Path, "/") {
			t.Errorf("non-top-level file leaked: %q", f.Path)
		}
	}
}

func TestListRepoTopLevelMarkdown_PropagatesError(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(500, "boom")
	})
	t.Cleanup(restore)
	if _, err := ListRepoTopLevelMarkdown(context.Background(), "tok", "a", "b", "main"); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetRepoInfo_Success(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(200, `{"default_branch":"main","permissions":{"push":true},"html_url":"https://github.com/a/b"}`)
	})
	t.Cleanup(restore)
	info, err := GetRepoInfo(context.Background(), "tok", "a", "b")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if info.DefaultBranch != "main" || !info.Permissions.Push {
		t.Errorf("got %+v", info)
	}
}

func TestGetBranchSHA(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		if strings.HasSuffix(req.URL.Path, "/git/ref/heads/main") {
			return makeResp(200, `{"object":{"sha":"abc123"}}`)
		}
		return makeResp(404, "{}")
	})
	t.Cleanup(restore)
	sha, err := GetBranchSHA(context.Background(), "tok", "a", "b", "main")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sha != "abc123" {
		t.Errorf("got %q", sha)
	}
}

func TestCreateBranch_Success(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		if req.Method != http.MethodPost {
			t.Errorf("wrong method %q", req.Method)
		}
		if !strings.HasSuffix(req.URL.Path, "/git/refs") {
			t.Errorf("wrong path %q", req.URL.Path)
		}
		return makeResp(201, `{"ref":"refs/heads/feat"}`)
	})
	t.Cleanup(restore)
	if err := CreateBranch(context.Background(), "tok", "a", "b", "feat", "deadbeef"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestCreateBranch_FailReturnsFetchError(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(422, `{"message":"already exists"}`)
	})
	t.Cleanup(restore)
	err := CreateBranch(context.Background(), "tok", "a", "b", "exists", "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*FetchError); !ok {
		t.Fatalf("want *FetchError, got %T", err)
	}
}

func TestPutFile_Success(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		if req.Method != http.MethodPut {
			t.Errorf("wrong method")
		}
		return makeResp(200, `{"commit":{"sha":"c1","html_url":"https://x"}, "content":{"sha":"c2"}}`)
	})
	t.Cleanup(restore)
	out, err := PutFile(context.Background(), "tok", "a", "b", "README.md", "main", "msg", "hello", "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Commit.SHA != "c1" {
		t.Errorf("got %+v", out)
	}
}

func TestPutFile_WithExistingSHA(t *testing.T) {
	// When fileSHA is non-empty, it's included in the body — the GitHub
	// API treats it as an update vs. create.
	var sawSHA bool
	restore := installMockTransport(func(req *http.Request) *http.Response {
		// crude: just verify the URL path
		if !strings.Contains(req.URL.Path, "/contents/") {
			t.Errorf("path %q", req.URL.Path)
		}
		sawSHA = true
		return makeResp(200, `{"commit":{"sha":"c1"}}`)
	})
	t.Cleanup(restore)
	_, err := PutFile(context.Background(), "tok", "a", "b", "X.md", "main", "msg", "data", "existing-sha")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !sawSHA {
		t.Fatal("expected request to be sent")
	}
}

func TestCreatePull_Success(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(201, `{"number":42,"html_url":"https://x/pr/42","state":"open"}`)
	})
	t.Cleanup(restore)
	out, err := CreatePull(context.Background(), "tok", "a", "b", "main", "feat", "title", "body")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Number != 42 {
		t.Errorf("got %+v", out)
	}
}

func TestCreatePull_GitHubError(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(422, `{"message":"head/base same"}`)
	})
	t.Cleanup(restore)
	if _, err := CreatePull(context.Background(), "tok", "a", "b", "main", "main", "t", "b"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchGitHubFileMeta_Base64Decoded(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		// "Hello, world" base64-encoded with embedded newlines (as
		// GitHub returns).
		return makeResp(200, `{
			"sha":"deadbeef",
			"type":"file",
			"encoding":"base64",
			"content":"SGVsbG8s\nIHdvcmxk"
		}`)
	})
	t.Cleanup(restore)
	meta, err := FetchGitHubFileMeta(context.Background(), "tok", "a", "b", "main", "X.md")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if meta.Content != "Hello, world" {
		t.Errorf("got %q", meta.Content)
	}
	if meta.SHA != "deadbeef" {
		t.Errorf("sha %q", meta.SHA)
	}
}

func TestFetchGitHubFileMeta_NotAFile(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(200, `{"sha":"x","type":"dir"}`)
	})
	t.Cleanup(restore)
	if _, err := FetchGitHubFileMeta(context.Background(), "tok", "a", "b", "main", "docs"); err == nil {
		t.Fatal("expected 'not a file' error")
	}
}

func TestFetchGitHubFileMeta_PassesThroughSSOError(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		hdr := http.Header{}
		hdr.Set("X-GitHub-SSO", "required; url=https://x/sso")
		res := makeResp(403, "blocked")
		res.Header = hdr
		return res
	})
	t.Cleanup(restore)
	_, err := FetchGitHubFileMeta(context.Background(), "tok", "a", "b", "main", "X.md")
	if err == nil {
		t.Fatal("expected error")
	}
	fe, ok := err.(*FetchError)
	if !ok {
		t.Fatalf("want *FetchError, got %T", err)
	}
	if fe.SSOURL == "" {
		t.Errorf("SSOURL should be parsed")
	}
}

func TestFetchGitHubFileSHA(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(200, `{"sha":"sha-only","type":"file","content":""}`)
	})
	t.Cleanup(restore)
	sha, err := FetchGitHubFileSHA(context.Background(), "tok", "a", "b", "main", "X.md")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sha != "sha-only" {
		t.Errorf("got %q", sha)
	}
}
