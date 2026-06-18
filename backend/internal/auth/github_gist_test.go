package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// rtFn is a tiny http.RoundTripper adapter so test cases can inject
// canned responses without standing up an httptest.Server.
type rtFn func(*http.Request) *http.Response

func (f rtFn) RoundTrip(r *http.Request) (*http.Response, error) { return f(r), nil }

func mockResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// withMockedDefault swaps http.DefaultTransport for the duration of
// the test. FetchGistMeta builds its own http.Client without a custom
// Transport, so DefaultTransport is what its requests flow through.
func withMockedDefault(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	prev := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = prev })
}

func TestFetchGistMeta_HappyPath(t *testing.T) {
	withMockedDefault(t, rtFn(func(r *http.Request) *http.Response {
		if !strings.HasPrefix(r.URL.Path, "/gists/") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		return mockResp(200, `{
			"files": {
				"sample.md":  {"filename":"sample.md","raw_url":"https://example/raw/sample.md","language":"Markdown"},
				"notes.txt":  {"filename":"notes.txt","raw_url":"https://example/raw/notes.txt","language":"Text"}
			},
			"history": [
				{"version": "abc123-newest"},
				{"version": "older-sha"}
			]
		}`)
	}))

	meta, err := FetchGistMeta(context.Background(), "", "f64c136")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if meta.LatestCommit != "abc123-newest" {
		t.Errorf("latestCommit=%q want abc123-newest", meta.LatestCommit)
	}
	if meta.PrimaryFilename != "sample.md" {
		t.Errorf("primaryFilename=%q want sample.md (markdown wins over txt)", meta.PrimaryFilename)
	}
	if len(meta.Files) != 2 {
		t.Errorf("files=%d want 2", len(meta.Files))
	}
}

func TestFetchGistMeta_FallsBackToFirstFileWhenNoMarkdown(t *testing.T) {
	withMockedDefault(t, rtFn(func(r *http.Request) *http.Response {
		return mockResp(200, `{
			"files": {
				"b.txt": {"filename":"b.txt"},
				"a.txt": {"filename":"a.txt"}
			},
			"history": [{"version":"sha1"}]
		}`)
	}))

	meta, err := FetchGistMeta(context.Background(), "", "any")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// pickPrimaryGistFile sorts alphabetically when no .md exists,
	// so a.txt wins over b.txt regardless of map iteration order.
	if meta.PrimaryFilename != "a.txt" {
		t.Errorf("primaryFilename=%q want a.txt (lex-first)", meta.PrimaryFilename)
	}
}

func TestFetchGistMeta_404ReturnsFetchError(t *testing.T) {
	withMockedDefault(t, rtFn(func(r *http.Request) *http.Response {
		return mockResp(404, `{"message":"Not Found"}`)
	}))
	_, err := FetchGistMeta(context.Background(), "", "missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*FetchError)
	if !ok {
		t.Fatalf("err type=%T want *FetchError", err)
	}
	if fe.StatusCode != 404 {
		t.Errorf("status=%d want 404", fe.StatusCode)
	}
}

func TestFetchGistMeta_AuthHeaderForSecretGists(t *testing.T) {
	withMockedDefault(t, rtFn(func(r *http.Request) *http.Response {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			t.Errorf("auth header=%q want Bearer my-token", auth)
		}
		return mockResp(200, `{"files":{},"history":[{"version":"sha"}]}`)
	}))
	_, err := FetchGistMeta(context.Background(), "my-token", "secret")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestFetchGistMeta_EmptyHistoryReturnsEmptyCommit(t *testing.T) {
	withMockedDefault(t, rtFn(func(r *http.Request) *http.Response {
		return mockResp(200, `{"files":{},"history":[]}`)
	}))
	meta, err := FetchGistMeta(context.Background(), "", "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if meta.LatestCommit != "" {
		t.Errorf("latestCommit=%q want empty for empty history", meta.LatestCommit)
	}
}
