package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/models"
)

// Access reasons returned to the frontend so it can render an appropriate page.
const (
	accessKindSignInRequired = "sign_in_required"
	accessKindNoGitHubAccess = "no_github_access"
	accessKindNotFound       = "not_found"
)

type accessErr struct {
	Status int
	Kind   string
	Owner  string
	Repo   string
}

// checkDocAccess loads a doc and verifies the requester is allowed to read it.
//
// Policy:
//   - Non-GitHub docs (uploads, non-github URLs): always readable.
//   - GitHub-sourced docs: we DO NOT trust the stored Private flag — the
//     flag was set at ingest time, but a repo's visibility can flip from
//     public to private after that, and we still need to gate readers
//     correctly. So for any github-sourced doc we re-verify the raw URL's
//     public reachability (cached). If it's still public, anyone can read.
//     If not, we require a signed-in user with current GitHub access to
//     {owner}/{repo}.
//
// Self-healing: if we discover a doc whose visibility flipped from public
// to private (raw URL now returns 4xx but stored Private=false), we
// asynchronously update the stored flag so the gate stays correct without
// repeatedly hitting GitHub.
func (a *API) checkDocAccess(r *http.Request, docID string) (*models.Document, *accessErr) {
	doc, err := a.store.GetDocument(r.Context(), docID)
	if err != nil {
		return nil, &accessErr{Status: http.StatusInternalServerError, Kind: "server_error"}
	}
	if doc == nil {
		return nil, &accessErr{Status: http.StatusNotFound, Kind: accessKindNotFound}
	}

	// Derive github owner/repo/ref/path. Prefers the fields stored on the
	// doc; falls back to parsing the source URL for legacy docs ingested
	// before those fields existed.
	owner, repo, ref, path, isGitHub := deriveGitHubInfo(doc)

	// Non-github docs are public.
	if !isGitHub {
		return doc, nil
	}

	// For github-sourced docs, the stored Private flag is only a hint. We
	// re-check the raw URL's reachability (with caching) so a stale flag
	// from a since-privated repo doesn't leak the content.
	publicNow := a.publicGitHubCheck(r.Context(), owner, repo, ref, path)
	if publicNow {
		return doc, nil
	}

	// Self-heal: stamp Private=true (and the github metadata if missing)
	// so subsequent requests don't need to re-derive.
	if !doc.Private || doc.GitHubOwner == "" {
		go a.markDocPrivate(doc.ID, owner, repo, ref, path)
	}

	user := a.currentUser(r)
	if user == nil {
		return nil, &accessErr{
			Status: http.StatusUnauthorized,
			Kind:   accessKindSignInRequired,
			Owner:  owner,
			Repo:   repo,
		}
	}
	ok, err := repoAccessCache.check(r.Context(), user.ID, user.AccessToken, owner, repo)
	if err != nil || !ok {
		return nil, &accessErr{
			Status: http.StatusForbidden,
			Kind:   accessKindNoGitHubAccess,
			Owner:  owner,
			Repo:   repo,
		}
	}
	return doc, nil
}

// deriveGitHubInfo returns the (owner, repo, ref, path) for a doc, preferring
// the stamped fields and falling back to parsing the SourceURL. The second
// return is false when the doc is not github-sourced.
func deriveGitHubInfo(doc *models.Document) (string, string, string, string, bool) {
	if doc.Origin != "url" {
		return "", "", "", "", false
	}
	if doc.GitHubOwner != "" && doc.GitHubRepo != "" {
		return doc.GitHubOwner, doc.GitHubRepo, doc.GitHubRef, doc.GitHubPath, true
	}
	owner, repo, ref, path, ok := parseGitHubBlobURL(doc.SourceURL)
	if !ok {
		return "", "", "", "", false
	}
	return owner, repo, ref, path, true
}

// IsPublicGitHubBlob is the exported wrapper around publicGitHubCheck
// — used by the SPA handler to decide whether a doc's title is safe
// to embed in og:title for Slack/Twitter/etc. unfurls. The SPA package
// can't reach the access cache directly, so it gets handed this
// read-only probe at boot.
func (a *API) IsPublicGitHubBlob(ctx context.Context, owner, repo, ref, path string) bool {
	return a.publicGitHubCheck(ctx, owner, repo, ref, path)
}

// publicGitHubCheck returns true if the file at {owner}/{repo}/{ref}/{path}
// is currently fetchable from the public raw.githubusercontent.com endpoint.
// Results are cached for 5 minutes per (owner, repo, ref, path) to keep the
// hot path cheap.
func (a *API) publicGitHubCheck(ctx context.Context, owner, repo, ref, path string) bool {
	if owner == "" || repo == "" || ref == "" || path == "" {
		// Missing components → can't construct the raw URL → fail closed.
		return false
	}
	return publicFetchCache.isPublic(ctx, owner, repo, ref, path)
}

// markDocPrivate stamps Private=true and the github metadata on a doc.
// Called fire-and-forget from checkDocAccess when we detect that a
// previously-public repo has flipped to private.
func (a *API) markDocPrivate(docID, owner, repo, ref, path string) {
	ctx := contextDetached()
	_, _ = a.store.Documents().UpdateOne(ctx,
		bson.M{"_id": docID},
		bson.M{"$set": bson.M{
			"private":      true,
			"github_owner": owner,
			"github_repo":  repo,
			"github_ref":   ref,
			"github_path":  path,
			"updated_at":   time.Now().UTC(),
		}})
}

// checkCommentAccess loads a comment and verifies the requester is allowed to
// act on its parent document.
func (a *API) checkCommentAccess(r *http.Request, commentID string) (*models.Comment, *models.Document, *accessErr) {
	c, err := a.store.GetComment(r.Context(), commentID)
	if err != nil {
		return nil, nil, &accessErr{Status: http.StatusInternalServerError, Kind: "server_error"}
	}
	if c == nil {
		return nil, nil, &accessErr{Status: http.StatusNotFound, Kind: accessKindNotFound}
	}
	doc, accErr := a.checkDocAccess(r, c.DocumentID)
	if accErr != nil {
		return nil, nil, accErr
	}
	return c, doc, nil
}

// writeAccessError translates an access denial into a structured response.
func (a *API) writeAccessError(w http.ResponseWriter, r *http.Request, e *accessErr) {
	resp := fetchErrorResponse{Kind: e.Kind}
	switch e.Kind {
	case accessKindNotFound:
		resp.Error = "Document not found."
	case accessKindSignInRequired:
		resp.Error = "Sign in with GitHub to view this document — it was cloned from a private repo."
		// Redirect them back to whatever they were viewing (frontend route),
		// not the API path that triggered the 401.
		ref := r.Header.Get("Referer")
		redirect := "/"
		if u, err := url.Parse(ref); err == nil && u.Path != "" && u.Path != "/" {
			redirect = u.Path
		}
		resp.Actions = []fetchErrorAction{{
			Label: "Sign in with GitHub",
			URL:   "/api/auth/github/login?redirect=" + url.QueryEscape(redirect),
		}}
	case accessKindNoGitHubAccess:
		resp.Error = "You don't have GitHub access to this repo. Ask an admin of " + e.Owner + " to approve markupmarkdown, then try again."
		if a.cfg.GitHub.ClientID != "" {
			resp.Actions = append(resp.Actions, fetchErrorAction{
				Label: "Manage GitHub access",
				URL:   "https://github.com/settings/connections/applications/" + a.cfg.GitHub.ClientID,
			})
		}
		if e.Owner != "" && e.Repo != "" {
			resp.Actions = append(resp.Actions, fetchErrorAction{
				Label: "Open " + e.Owner + "/" + e.Repo + " on GitHub",
				URL:   "https://github.com/" + e.Owner + "/" + e.Repo,
			})
		}
	default:
		resp.Error = "Couldn't load this document."
	}
	writeJSON(w, e.Status, resp)
}

// --- repo-access cache ---
//
// Caches the result of CheckRepoAccess for a short period per (userID, repo)
// so that repeated reads (doc fetch + comments + events stream all hit it)
// don't burn GitHub API quota.

type repoCacheKey struct {
	UserID string
	Owner  string
	Repo   string
}

type repoCacheEntry struct {
	Allowed bool
	Expires time.Time
}

type accessCache struct {
	mu      sync.Mutex
	entries map[repoCacheKey]repoCacheEntry
	ttl     time.Duration
}

var repoAccessCache = &accessCache{
	entries: map[repoCacheKey]repoCacheEntry{},
	ttl:     2 * time.Minute,
}

// reset wipes every cached entry. Test-only — called between
// integration tests that share a package-level *api.API so previous
// tests' cached access decisions don't leak forward. Not exported
// because production code never wants to nuke the cache wholesale.
func (c *accessCache) reset() {
	c.mu.Lock()
	c.entries = map[repoCacheKey]repoCacheEntry{}
	c.mu.Unlock()
}

// invalidate drops the cached entry for the (userID, owner, repo) tuple
// so the next check call hits GitHub. Used by the manual "check now"
// flow so a user kicked out of a repo gets booted within a tab focus.
func (c *accessCache) invalidate(userID, owner, repo string) {
	c.mu.Lock()
	delete(c.entries, repoCacheKey{userID, owner, repo})
	c.mu.Unlock()
}

func (c *accessCache) check(ctx context.Context, userID, token, owner, repo string) (bool, error) {
	key := repoCacheKey{userID, owner, repo}
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && time.Now().Before(e.Expires) {
		c.mu.Unlock()
		return e.Allowed, nil
	}
	c.mu.Unlock()

	ok, err := auth.CheckRepoAccess(ctx, token, owner, repo)
	if err != nil {
		// Don't cache errors — let next request retry.
		return false, err
	}
	c.mu.Lock()
	c.entries[key] = repoCacheEntry{Allowed: ok, Expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return ok, nil
}

// --- public-fetch cache ---
//
// For github-sourced docs, we re-verify the raw URL's reachability on
// every read so we catch repos that have flipped from public to private
// after ingest. To keep the hot path cheap, results are cached for
// 5 minutes per (owner, repo, ref, path).
//
// We deliberately use a HEAD against raw.githubusercontent.com (not the
// rate-limited Contents API) — anonymous, no token, no quota.

type publicFetchKey struct {
	Owner string
	Repo  string
	Ref   string
	Path  string
}

type publicFetchEntry struct {
	Public  bool
	Expires time.Time
}

type publicFetchCacheT struct {
	mu      sync.Mutex
	entries map[publicFetchKey]publicFetchEntry
	ttl     time.Duration
}

var publicFetchCache = &publicFetchCacheT{
	entries: map[publicFetchKey]publicFetchEntry{},
	ttl:     5 * time.Minute,
}

// reset wipes the cache. Same rationale + same scope as
// accessCache.reset — only the integration test harness should
// ever call this.
func (c *publicFetchCacheT) reset() {
	c.mu.Lock()
	c.entries = map[publicFetchKey]publicFetchEntry{}
	c.mu.Unlock()
}

// invalidate drops the cached entry for the given file so the next
// caller forces a fresh check. Used by the manual "check now" path
// when the user might have lost access since the last cached lookup.
func (c *publicFetchCacheT) invalidate(owner, repo, ref, path string) {
	c.mu.Lock()
	delete(c.entries, publicFetchKey{owner, repo, ref, path})
	c.mu.Unlock()
}

// isPublic returns true if the file is currently reachable via the
// anonymous raw.githubusercontent.com endpoint. False on 4xx or any
// network/error condition (fail closed — better to ask for auth than
// to leak content).
func (c *publicFetchCacheT) isPublic(ctx context.Context, owner, repo, ref, path string) bool {
	key := publicFetchKey{owner, repo, ref, path}
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && time.Now().Before(e.Expires) {
		c.mu.Unlock()
		return e.Public
	}
	c.mu.Unlock()

	raw := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, ref, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		// Don't cache transient construction failures.
		return false
	}
	req.Header.Set("User-Agent", "markupmarkdown-access/0.1")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Network error: don't cache.
		return false
	}
	defer resp.Body.Close()

	public := resp.StatusCode >= 200 && resp.StatusCode < 300
	c.mu.Lock()
	c.entries[key] = publicFetchEntry{Public: public, Expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return public
}

