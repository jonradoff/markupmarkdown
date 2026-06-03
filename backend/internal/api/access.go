package api

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

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
// Public docs are open to everyone. Private (GitHub-sourced) docs require an
// authenticated user who currently has GitHub access to {owner}/{repo}.
func (a *API) checkDocAccess(r *http.Request, docID string) (*models.Document, *accessErr) {
	doc, err := a.store.GetDocument(r.Context(), docID)
	if err != nil {
		return nil, &accessErr{Status: http.StatusInternalServerError, Kind: "server_error"}
	}
	if doc == nil {
		return nil, &accessErr{Status: http.StatusNotFound, Kind: accessKindNotFound}
	}
	if !doc.Private {
		return doc, nil
	}
	user := a.currentUser(r)
	if user == nil {
		return nil, &accessErr{
			Status: http.StatusUnauthorized,
			Kind:   accessKindSignInRequired,
			Owner:  doc.GitHubOwner,
			Repo:   doc.GitHubRepo,
		}
	}
	ok, err := repoAccessCache.check(r.Context(), user.ID, user.AccessToken, doc.GitHubOwner, doc.GitHubRepo)
	if err != nil || !ok {
		return nil, &accessErr{
			Status: http.StatusForbidden,
			Kind:   accessKindNoGitHubAccess,
			Owner:  doc.GitHubOwner,
			Repo:   doc.GitHubRepo,
		}
	}
	return doc, nil
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

