package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// RepoInfo is a slim view of the bits we care about from
// GET /repos/{owner}/{repo}: the default branch (used as the base for
// a pushback PR) and the user's permission level on the repo.
type RepoInfo struct {
	DefaultBranch string `json:"default_branch"`
	Permissions   struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
		Pull     bool `json:"pull"`
	} `json:"permissions"`
	// HTMLURL is the human URL of the repo, returned for convenience
	// so callers can link directly without rebuilding it.
	HTMLURL string `json:"html_url"`
}

// GetRepoInfo fetches default branch + the user's permissions on the
// repo. Used by pushback to decide whether direct-commit and PR-create
// are available for this user/repo combination.
func GetRepoInfo(ctx context.Context, accessToken, owner, repo string) (*RepoInfo, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo))
	info, err := getJSON[RepoInfo](ctx, apiURL, accessToken)
	if err != nil {
		return nil, err
	}
	return info, nil
}

// branchRef and similar are tiny payload structs for the Git Refs API.
type branchRef struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

// GetBranchSHA returns the head SHA of a branch (or any ref). The
// pushback flow uses it to (a) anchor a new feature branch and (b)
// hand the SHA to the Contents PUT call when overwriting a file.
func GetBranchSHA(ctx context.Context, accessToken, owner, repo, branch string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/ref/heads/%s",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(branch))
	ref, err := getJSON[branchRef](ctx, apiURL, accessToken)
	if err != nil {
		return "", err
	}
	return ref.Object.SHA, nil
}

// CreateBranch creates a new branch named newBranch pointed at fromSHA.
// Returns a *FetchError with StatusCode 422 if the branch already
// exists (same surface as other GitHub-API errors so callers can
// branch on it).
func CreateBranch(ctx context.Context, accessToken, owner, repo, newBranch, fromSHA string) error {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/refs",
		url.PathEscape(owner), url.PathEscape(repo))
	body := map[string]string{
		"ref": "refs/heads/" + newBranch,
		"sha": fromSHA,
	}
	return postJSON(ctx, apiURL, accessToken, body, nil)
}

// PutFileResult captures what callers want back from the Contents PUT:
// the commit SHA and HTML URL of the new commit on GitHub.
type PutFileResult struct {
	Commit struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
	} `json:"commit"`
	Content struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
	} `json:"content"`
}

// PutFile creates-or-updates a file on a branch via the Contents API.
// fileSHA is the current blob SHA at path on branch (required when
// updating; pass "" when creating a new file). The content is sent
// base64-encoded as the Contents API requires.
func PutFile(ctx context.Context, accessToken, owner, repo, path, branch, message, content, fileSHA string) (*PutFileResult, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s",
		url.PathEscape(owner), url.PathEscape(repo), strings.TrimPrefix(path, "/"))
	body := map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
	}
	if fileSHA != "" {
		body["sha"] = fileSHA
	}
	var out PutFileResult
	if err := putJSON(ctx, apiURL, accessToken, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreatePullResult is the slim view of the PR object the pushback
// flow returns to the frontend.
type CreatePullResult struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

// CreatePull opens a PR from head → base. Body is optional.
func CreatePull(ctx context.Context, accessToken, owner, repo, base, head, title, body string) (*CreatePullResult, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls",
		url.PathEscape(owner), url.PathEscape(repo))
	payload := map[string]any{
		"title": title,
		"head":  head,
		"base":  base,
		"body":  body,
	}
	var out CreatePullResult
	if err := postJSON(ctx, apiURL, accessToken, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// postJSON / putJSON are tiny helpers for write calls — same pattern
// as getJSON. Out can be nil if the caller doesn't need the response.
func postJSON(ctx context.Context, apiURL, token string, body any, out any) error {
	return sendJSON(ctx, http.MethodPost, apiURL, token, body, out)
}
func putJSON(ctx context.Context, apiURL, token string, body any, out any) error {
	return sendJSON(ctx, http.MethodPut, apiURL, token, body, out)
}
func sendJSON(ctx context.Context, method, apiURL, token string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, apiURL, strings.NewReader(string(buf)))
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &FetchError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
			SSOURL:     parseSSOHeader(resp.Header.Get("X-GitHub-SSO")),
		}
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return err
		}
	}
	return nil
}

// AccountKind discriminates between a github "user" and an "org" — the
// REST API uses different endpoints to list their repos. /users/{name}
// returns an account with a `type` field that's exactly one of those
// two values.
type AccountKind string

const (
	AccountKindUser AccountKind = "User"
	AccountKindOrg  AccountKind = "Organization"
)

// AccountInfo is the slice of /users/{name} we use to (a) decide
// whether to treat the URL as a user or org index and (b) keep the
// display name + avatar around for the index header.
type AccountInfo struct {
	Login     string      `json:"login"`
	Name      string      `json:"name"`
	AvatarURL string      `json:"avatar_url"`
	Type      AccountKind `json:"type"`
	HTMLURL   string      `json:"html_url"`
}

// LookupAccount fetches /users/{name} which works for both User and
// Organization accounts (GitHub disambiguates via the `type` field).
// We use this to figure out whether github.com/anthropics is a user
// profile or an org — the listing endpoints differ.
func LookupAccount(ctx context.Context, accessToken, name string) (*AccountInfo, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s", url.PathEscape(name))
	return fetchGitHubJSON[AccountInfo](ctx, accessToken, apiURL)
}

// RepoSummary is the slim view of GET /user/repos, /users/{u}/repos,
// /orgs/{o}/repos used by the index-listing flow. We need the
// default_branch to fetch the tree, the full_name as a stable
// identifier, and the visibility/permissions so the UI can show a
// private/public badge.
type RepoSummary struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Fork          bool   `json:"fork"`
	Archived      bool   `json:"archived"`
	PushedAt      string `json:"pushed_at"`
	UpdatedAt     string `json:"updated_at"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// listReposPaginated walks a GitHub list-repos endpoint until either
// (a) we hit an empty page or (b) the safety cap kicks in. GitHub
// returns 30/page by default; we ask for 100 to cut the round-trip
// count. The cap (10 pages = 1000 repos) is well above the size of
// any real org/user we'd index inline — anything beyond that
// genuinely needs a separate scan job.
func listReposPaginated(ctx context.Context, accessToken, base string) ([]RepoSummary, error) {
	const perPage = 100
	const maxPages = 10
	out := make([]RepoSummary, 0, perPage)
	for page := 1; page <= maxPages; page++ {
		sep := "?"
		if strings.Contains(base, "?") {
			sep = "&"
		}
		apiURL := fmt.Sprintf("%s%sper_page=%d&page=%d", base, sep, perPage, page)
		batch, err := fetchGitHubJSON[[]RepoSummary](ctx, accessToken, apiURL)
		if err != nil {
			return nil, err
		}
		if batch == nil || len(*batch) == 0 {
			break
		}
		out = append(out, *batch...)
		if len(*batch) < perPage {
			break
		}
	}
	return out, nil
}

// ListUserRepos returns the user's public repos (anonymous viewer) or
// every repo the viewer's token can see for that owner. We can't ask
// /users/{owner}/repos for private repos owned by `owner` — that
// endpoint only returns public ones even when authenticated. For the
// "I'm signed in and want to see private repos in my own profile"
// case, /user/repos returns the authenticated user's full set
// (public + private). We fold both into a single sorted result.
func ListUserRepos(ctx context.Context, accessToken, owner string, viewerLogin string) ([]RepoSummary, error) {
	all, err := listReposPaginated(ctx, accessToken,
		fmt.Sprintf("https://api.github.com/users/%s/repos?type=owner&sort=updated", url.PathEscape(owner)))
	if err != nil {
		return nil, err
	}
	// If the viewer is the owner, fold in private repos via /user/repos.
	if accessToken != "" && viewerLogin != "" && strings.EqualFold(viewerLogin, owner) {
		mine, mErr := listReposPaginated(ctx, accessToken,
			"https://api.github.com/user/repos?affiliation=owner&visibility=private&sort=updated")
		if mErr == nil {
			seen := make(map[int64]bool, len(all))
			for _, r := range all {
				seen[r.ID] = true
			}
			for _, r := range mine {
				if !seen[r.ID] {
					all = append(all, r)
				}
			}
		}
	}
	return all, nil
}

// ListOrgRepos returns repos owned by an organization the viewer can
// see. Public viewers get public repos; authenticated members get
// everything they're entitled to (GitHub scopes the response to the
// caller's org membership).
func ListOrgRepos(ctx context.Context, accessToken, org string) ([]RepoSummary, error) {
	return listReposPaginated(ctx, accessToken,
		fmt.Sprintf("https://api.github.com/orgs/%s/repos?type=all&sort=updated", url.PathEscape(org)))
}

// TreeEntry is one row of GET /repos/{o}/{r}/git/trees/{sha}?recursive=1.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" | "tree"
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type gitTreeResponse struct {
	SHA       string      `json:"sha"`
	Tree      []TreeEntry `json:"tree"`
	Truncated bool        `json:"truncated"`
}

// ListRepoMarkdownFiles returns every `.md` (or `.markdown`) file in
// the repo's default branch, scanning the git tree with `recursive=1`.
// One round-trip even for thousands-of-files repos — the Contents
// endpoint would require recursing per directory. `truncated` is true
// for monorepos with >100k tree entries; we surface that via the
// returned flag so callers can warn the user.
func ListRepoMarkdownFiles(ctx context.Context, accessToken, owner, repo, ref string) ([]TreeEntry, bool, error) {
	if ref == "" {
		info, err := GetRepoInfo(ctx, accessToken, owner, repo)
		if err != nil {
			return nil, false, err
		}
		ref = info.DefaultBranch
		if ref == "" {
			ref = "main"
		}
	}
	// Resolve ref → tree SHA. /git/trees accepts a branch name directly
	// in modern GitHub, but the documented contract uses the commit SHA;
	// GetBranchSHA gives us that explicitly.
	sha, err := GetBranchSHA(ctx, accessToken, owner, repo, ref)
	if err != nil {
		// Empty repo / ref doesn't exist: surface as an empty listing.
		var fe *FetchError
		if errors.As(err, &fe) && (fe.StatusCode == http.StatusNotFound || fe.StatusCode == http.StatusConflict) {
			return nil, false, nil
		}
		return nil, false, err
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	tree, err := fetchGitHubJSON[gitTreeResponse](ctx, accessToken, apiURL)
	if err != nil {
		return nil, false, err
	}
	out := make([]TreeEntry, 0, 16)
	for _, e := range tree.Tree {
		if e.Type != "blob" {
			continue
		}
		lp := strings.ToLower(e.Path)
		if strings.HasSuffix(lp, ".md") || strings.HasSuffix(lp, ".markdown") {
			out = append(out, e)
		}
	}
	return out, tree.Truncated, nil
}

// ListRepoTopLevelMarkdown is the user/org-index variant: same idea as
// ListRepoMarkdownFiles, but limited to files at the repo root. For
// multi-repo indexes we want one summary line per file rather than
// every README/CONTRIBUTING/SECURITY.md scattered through subdirs.
func ListRepoTopLevelMarkdown(ctx context.Context, accessToken, owner, repo, ref string) ([]TreeEntry, error) {
	all, _, err := ListRepoMarkdownFiles(ctx, accessToken, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	out := make([]TreeEntry, 0, len(all))
	for _, e := range all {
		if !strings.Contains(e.Path, "/") {
			out = append(out, e)
		}
	}
	return out, nil
}

// fetchGitHubJSON is the variant of getJSON used by index-listing
// helpers: it tolerates an empty token (anonymous, public-only) and
// returns FetchError so callers can branch on the status code.
func fetchGitHubJSON[T any](ctx context.Context, accessToken, apiURL string) (*T, error) {
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
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
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
