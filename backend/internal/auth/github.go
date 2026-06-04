package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHub OAuth + REST endpoints. gosec G101 flags `tokenURL` as a
// hardcoded credential because of the substring "access_token" — it's a
// public URL constant, not a secret.
const (
	authorizeURL = "https://github.com/login/oauth/authorize"
	tokenURL     = "https://github.com/login/oauth/access_token" //nolint:gosec // public URL, not a credential
	userURL      = "https://api.github.com/user"
	emailsURL    = "https://api.github.com/user/emails"
)

type GitHubClient struct {
	ClientID     string
	ClientSecret string
	CallbackURL  string
	Scope        string
}

type GitHubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

type GitHubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func (c *GitHubClient) AuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", c.CallbackURL)
	q.Set("scope", c.Scope)
	q.Set("state", state)
	q.Set("allow_signup", "true")
	return authorizeURL + "?" + q.Encode()
}

func (c *GitHubClient) ExchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", c.CallbackURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token exchange: http %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", fmt.Errorf("github: %s: %s", out.Error, out.ErrorDescription)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("no access token in github response")
	}
	return out.AccessToken, nil
}

func (c *GitHubClient) FetchUser(ctx context.Context, accessToken string) (*GitHubUser, error) {
	user, err := getJSON[GitHubUser](ctx, userURL, accessToken)
	if err != nil {
		return nil, err
	}
	if user.Email == "" {
		emails, _ := getJSON[[]GitHubEmail](ctx, emailsURL, accessToken)
		if emails != nil {
			for _, e := range *emails {
				if e.Primary && e.Verified {
					user.Email = e.Email
					break
				}
			}
		}
	}
	return user, nil
}

func getJSON[T any](ctx context.Context, url, token string) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github %s: %d: %s", url, resp.StatusCode, string(body))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func RandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CheckRepoAccess returns true if the user's token can read {owner}/{repo}.
// Uses the lightweight GET /repos/{owner}/{repo} endpoint.
// Returns (false, nil) if the user authentically has no access.
// Returns (false, err) on network/auth errors so callers can decide whether
// to treat that as forbidden or surface a different message.
func CheckRepoAccess(ctx context.Context, accessToken, owner, repo string) (bool, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return false, nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return false, fmt.Errorf("github auth invalid")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	return false, fmt.Errorf("github repo check: %d", resp.StatusCode)
}

// FetchError is returned by FetchGitHubFileContent when the GitHub API
// responds with a non-success status, so callers can map it into a
// user-friendly message and the right next-step action.
type FetchError struct {
	StatusCode int
	Body       string
	SSOURL     string // populated when GitHub demands SAML SSO re-auth
}

func (e *FetchError) Error() string {
	if e.SSOURL != "" {
		return fmt.Sprintf("github sso required (%s)", e.SSOURL)
	}
	return fmt.Sprintf("github contents api: %d", e.StatusCode)
}

// parseSSOHeader extracts the SSO authorize URL from a
// `X-GitHub-SSO: required; url=https://...` header value.
func parseSSOHeader(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "required") {
		return ""
	}
	for _, part := range strings.Split(v, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "url=") {
			return strings.TrimPrefix(part, "url=")
		}
	}
	return ""
}

// FileMeta is returned by FetchGitHubFileMeta — pairs the Git blob SHA with
// the file's decoded content, so callers can both render and later
// drift-check against a single API call.
type FileMeta struct {
	SHA     string
	Content string
}

// FetchGitHubFileMeta calls the Contents API with the JSON accept header,
// returning both the blob SHA and decoded content. Works with an empty
// accessToken (public repos, anonymous IP-rate-limited) or an authed token
// (private repos). For drift-detection we care about SHA equality; the
// content is decoded so the same call can drive a sync.
func FetchGitHubFileMeta(ctx context.Context, accessToken, owner, repo, ref, path string) (*FileMeta, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		url.PathEscape(owner), url.PathEscape(repo),
		strings.TrimPrefix(path, "/"), url.QueryEscape(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &FetchError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			SSOURL:     parseSSOHeader(resp.Header.Get("X-GitHub-SSO")),
		}
	}
	var payload struct {
		SHA      string `json:"sha"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Type     string `json:"type"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Type != "" && payload.Type != "file" {
		return nil, fmt.Errorf("github contents: not a file (type=%s)", payload.Type)
	}
	content := payload.Content
	if payload.Encoding == "base64" {
		// GitHub wraps the base64 payload with newlines every 60 chars.
		raw := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' {
				return -1
			}
			return r
		}, payload.Content)
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("github contents: decode base64: %w", err)
		}
		content = string(decoded)
	}
	return &FileMeta{SHA: payload.SHA, Content: content}, nil
}

// FetchGitHubFileSHA is a cheap variant used for drift-detection — same call
// shape as FetchGitHubFileMeta but discards the content body to keep the
// memory + parse cost down. (We still pay the network cost; GitHub doesn't
// expose a HEAD-equivalent on the Contents endpoint.)
func FetchGitHubFileSHA(ctx context.Context, accessToken, owner, repo, ref, path string) (string, error) {
	meta, err := FetchGitHubFileMeta(ctx, accessToken, owner, repo, ref, path)
	if err != nil {
		return "", err
	}
	return meta.SHA, nil
}

// FetchGitHubFileContent uses the GitHub Contents API to load a file from a
// (potentially private) repo. Returns the decoded markdown.
func FetchGitHubFileContent(ctx context.Context, accessToken, owner, repo, ref, path string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		url.PathEscape(owner), url.PathEscape(repo),
		strings.TrimPrefix(path, "/"), url.QueryEscape(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &FetchError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			SSOURL:     parseSSOHeader(resp.Header.Get("X-GitHub-SSO")),
		}
	}
	return string(body), nil
}
