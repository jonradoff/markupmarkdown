package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRandomToken_Format(t *testing.T) {
	a := RandomToken(16)
	b := RandomToken(16)
	if a == b {
		t.Fatal("RandomToken should be random")
	}
	if len(a) != 32 {
		t.Fatalf("16 bytes → 32 hex chars; got %d", len(a))
	}
}

func TestRandomToken_Length(t *testing.T) {
	for _, n := range []int{1, 8, 24} {
		if got := len(RandomToken(n)); got != 2*n {
			t.Errorf("len(RandomToken(%d))=%d, want %d", n, got, 2*n)
		}
	}
}

func TestAuthorizeURL_IncludesAllFields(t *testing.T) {
	c := &GitHubClient{
		ClientID:    "abc",
		CallbackURL: "https://example.com/cb",
		Scope:       "repo user:email",
	}
	u := c.AuthorizeURL("the-state")
	for _, want := range []string{
		"client_id=abc",
		"state=the-state",
		"redirect_uri=https%3A%2F%2Fexample.com%2Fcb",
		"scope=repo+user%3Aemail",
		"allow_signup=true",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("URL %q missing %q", u, want)
		}
	}
}

func TestParseSSOHeader_Empty(t *testing.T) {
	if got := parseSSOHeader(""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestParseSSOHeader_WrongPrefix(t *testing.T) {
	if got := parseSSOHeader("denied; url=https://x"); got != "" {
		t.Errorf("got %q (only required prefix should match)", got)
	}
}

func TestParseSSOHeader_Extracts(t *testing.T) {
	got := parseSSOHeader("required; url=https://github.com/orgs/x/sso")
	if got != "https://github.com/orgs/x/sso" {
		t.Errorf("got %q", got)
	}
}

func TestParseSSOHeader_MissingURL(t *testing.T) {
	if got := parseSSOHeader("required"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestFetchError_Plain(t *testing.T) {
	e := &FetchError{StatusCode: 404}
	if !strings.Contains(e.Error(), "404") {
		t.Errorf("got %q", e.Error())
	}
}

func TestFetchError_SSO(t *testing.T) {
	e := &FetchError{StatusCode: 403, SSOURL: "https://example.com/sso"}
	if !strings.Contains(e.Error(), "sso") {
		t.Errorf("got %q", e.Error())
	}
}

func TestCheckRepoAccess_Success(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		if !strings.Contains(req.URL.Path, "/repos/owner/repo") {
			t.Errorf("unexpected path %q", req.URL.Path)
		}
		return makeResp(200, `{"id":1}`)
	})
	t.Cleanup(restore)

	ok, err := CheckRepoAccess(context.Background(), "tok", "owner", "repo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Fatal("expected access ok")
	}
}

func TestCheckRepoAccess_NotFoundOrForbiddenReturnsFalseNil(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusForbidden} {
		restore := installMockTransport(func(req *http.Request) *http.Response {
			return makeResp(code, `{}`)
		})
		ok, err := CheckRepoAccess(context.Background(), "tok", "o", "r")
		restore()
		if err != nil {
			t.Errorf("code %d: err %v", code, err)
		}
		if ok {
			t.Errorf("code %d: should be false", code)
		}
	}
}

func TestCheckRepoAccess_UnauthorizedErrors(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(http.StatusUnauthorized, `{}`)
	})
	t.Cleanup(restore)

	ok, err := CheckRepoAccess(context.Background(), "tok", "o", "r")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if ok {
		t.Fatal("ok should be false")
	}
}

func TestCheckRepoAccess_500Errors(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(500, "boom")
	})
	t.Cleanup(restore)
	if _, err := CheckRepoAccess(context.Background(), "tok", "o", "r"); err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestExchangeCode_Success(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(200, `{"access_token":"abc123"}`)
	})
	t.Cleanup(restore)

	c := &GitHubClient{ClientID: "id", ClientSecret: "secret", CallbackURL: "https://x/cb"}
	tok, err := c.ExchangeCode(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if tok != "abc123" {
		t.Fatalf("got %q", tok)
	}
}

func TestExchangeCode_NonSuccessStatus(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(500, "oops")
	})
	t.Cleanup(restore)
	c := &GitHubClient{ClientID: "id", ClientSecret: "secret"}
	if _, err := c.ExchangeCode(context.Background(), "x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestExchangeCode_ErrorBody(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(200, `{"error":"bad_verification_code","error_description":"no"}`)
	})
	t.Cleanup(restore)
	c := &GitHubClient{ClientID: "id", ClientSecret: "secret"}
	if _, err := c.ExchangeCode(context.Background(), "x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestExchangeCode_NoToken(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(200, `{}`)
	})
	t.Cleanup(restore)
	c := &GitHubClient{ClientID: "id", ClientSecret: "secret"}
	if _, err := c.ExchangeCode(context.Background(), "x"); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestFetchUser_UsesProfileEmail(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		if strings.HasSuffix(req.URL.Path, "/user") {
			u := GitHubUser{ID: 42, Login: "alice", Name: "Alice", Email: "a@x.com"}
			b, _ := json.Marshal(u)
			return makeResp(200, string(b))
		}
		return makeResp(404, "")
	})
	t.Cleanup(restore)
	u, err := (&GitHubClient{}).FetchUser(context.Background(), "tok")
	if err != nil || u == nil || u.Email != "a@x.com" {
		t.Fatalf("got %+v err=%v", u, err)
	}
}

func TestFetchUser_FallsBackToEmailsEndpoint(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		if strings.HasSuffix(req.URL.Path, "/user") {
			u := GitHubUser{ID: 7, Login: "bob", Name: "Bob"}
			b, _ := json.Marshal(u)
			return makeResp(200, string(b))
		}
		if strings.HasSuffix(req.URL.Path, "/emails") {
			es := []GitHubEmail{
				{Email: "old@x.com", Primary: false, Verified: true},
				{Email: "primary@x.com", Primary: true, Verified: true},
			}
			b, _ := json.Marshal(es)
			return makeResp(200, string(b))
		}
		return makeResp(404, "")
	})
	t.Cleanup(restore)
	u, err := (&GitHubClient{}).FetchUser(context.Background(), "tok")
	if err != nil || u.Email != "primary@x.com" {
		t.Fatalf("got %+v err=%v", u, err)
	}
}

func TestFetchUser_PropagatesError(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(500, "boom")
	})
	t.Cleanup(restore)
	if _, err := (&GitHubClient{}).FetchUser(context.Background(), "tok"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchGitHubFileContent_Success(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		return makeResp(200, "# raw md")
	})
	t.Cleanup(restore)
	got, err := FetchGitHubFileContent(context.Background(), "tok", "owner", "repo", "main", "README.md")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "# raw md" {
		t.Errorf("got %q", got)
	}
}

func TestFetchGitHubFileContent_SSO403(t *testing.T) {
	restore := installMockTransport(func(req *http.Request) *http.Response {
		hdr := http.Header{}
		hdr.Set("X-GitHub-SSO", "required; url=https://example.com/sso")
		res := makeResp(403, "blocked")
		res.Header = hdr
		return res
	})
	t.Cleanup(restore)

	_, err := FetchGitHubFileContent(context.Background(), "tok", "owner", "repo", "main", "README.md")
	if err == nil {
		t.Fatal("expected error")
	}
	fe, ok := err.(*FetchError)
	if !ok {
		t.Fatalf("want *FetchError, got %T", err)
	}
	if fe.SSOURL != "https://example.com/sso" {
		t.Errorf("got %q", fe.SSOURL)
	}
}

// --- helpers ---

// installMockTransport replaces both http.DefaultClient.Transport AND
// http.DefaultTransport with a mock. CheckRepoAccess / FetchGitHubFileContent
// each construct their own client (so we must cover the default transport
// path for them too). Returns a restore function.
type mockRoundTripper struct {
	handler func(*http.Request) *http.Response
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.handler(req), nil
}

func installMockTransport(h func(*http.Request) *http.Response) func() {
	prevDefaultClient := http.DefaultClient.Transport
	prevDefaultTransport := http.DefaultTransport
	mock := &mockRoundTripper{handler: h}
	http.DefaultClient.Transport = mock
	http.DefaultTransport = mock
	return func() {
		http.DefaultClient.Transport = prevDefaultClient
		http.DefaultTransport = prevDefaultTransport
	}
}

// makeResp builds a real *http.Response with a working body so the
// auth code's io.ReadAll / json.Decode actually see content.
func makeResp(status int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.WriteHeader(status)
	if _, err := io.Copy(rec.Body, bytes.NewReader([]byte(body))); err != nil {
		panic(err)
	}
	return rec.Result()
}
