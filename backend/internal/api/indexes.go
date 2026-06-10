package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/models"
)

// createIndexRequest is what the home-page "paste a URL → make an
// index" flow sends. We accept the URL in any GitHub shape we can
// recognize (repo / user / org) — the handler parses + dispatches.
type createIndexRequest struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// indexResponse is the canonical JSON envelope for read-side index
// handlers: the stored metadata plus the freshly-computed items.
type indexResponse struct {
	models.Index
	Items     []indexItem `json:"items"`
	Truncated bool        `json:"truncated,omitempty"`
}

// indexItem is one file (for kind="repo") or one (file, repo) pair
// (for user/org indexes). The browser uses URL + Title to render the
// list; PathInRepo + Repo carry context for the "from anthropics/x"
// label on user/org views.
type indexItem struct {
	Title       string `json:"title"`        // basename (or repo/path when ambiguous)
	URL         string `json:"url"`          // github.com/owner/repo/blob/ref/path
	Repo        string `json:"repo"`         // full_name for user/org variants; empty for repo variants
	RepoURL     string `json:"repoUrl,omitempty"`
	PathInRepo  string `json:"pathInRepo,omitempty"`
	Description string `json:"description,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	Private     bool   `json:"private,omitempty"`
}

// parsedIndexTarget captures what the URL is pointing at: a repo, a
// user profile, or an org. Kind=="" when the URL doesn't look like any
// of these.
type parsedIndexTarget struct {
	Kind  models.IndexKind
	Owner string
	Repo  string // empty for user/org
}

// parseIndexURL recognizes:
//   - https://github.com/owner/repo[/]?       → repo
//   - https://github.com/owner[/]?            → user OR org (caller decides)
// We strip any /tree/<ref>, /blob/<ref>/<path>, query, and fragment so
// users can paste a copied URL from a directory or file view.
func parseIndexURL(raw string) (parsedIndexTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedIndexTarget{}, errors.New("url is required")
	}
	// Accept bare "owner" or "owner/repo" as a convenience for the
	// pasting flow ("anthropics" → org index).
	if !strings.Contains(raw, "://") {
		raw = "https://github.com/" + strings.TrimPrefix(raw, "github.com/")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return parsedIndexTarget{}, errors.New("invalid url")
	}
	host := strings.ToLower(u.Host)
	if host != "github.com" && host != "www.github.com" {
		return parsedIndexTarget{}, errors.New("only github.com URLs are supported")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		return parsedIndexTarget{Kind: models.IndexKindUser, Owner: parts[0]}, nil
	}
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		// Repo index. Strip a trailing /tree/<ref>/... or /blob/<ref>/...
		// if the user pasted one; we just want owner/repo.
		return parsedIndexTarget{Kind: models.IndexKindRepo, Owner: parts[0], Repo: parts[1]}, nil
	}
	return parsedIndexTarget{}, errors.New("URL must point to github.com/owner or github.com/owner/repo")
}

// createIndex implements POST /api/indexes. Parses the URL, resolves
// user-vs-org for top-level URLs, gates on access, and either returns
// the existing index for this (creator, source) tuple or mints a new
// one. Items are NOT materialized here — they're computed per-view.
func (a *API) createIndex(w http.ResponseWriter, r *http.Request) {
	capBody(w, r, 4*1024)
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required to create an index")
		return
	}
	// Indexes are a write of new state — gate the same way create-doc is.
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if !a.enforceRate(w, r, a.rlCreateDoc, "Slow down on creating new indexes.") {
		return
	}
	var req createIndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	target, err := parseIndexURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	// For top-level URLs we don't know yet whether the owner is a user
	// or an org — disambiguate via /users/{name}. Failure surfaces as a
	// 404 the user can act on.
	if target.Kind == models.IndexKindUser {
		info, accErr := auth.LookupAccount(ctx, user.AccessToken, target.Owner)
		if accErr != nil {
			a.writeFetchError(w, r, "https://github.com/"+target.Owner, accErr)
			return
		}
		if info.Type == auth.AccountKindOrg {
			target.Kind = models.IndexKindOrg
		}
	}

	// Probe access on the resource so we don't mint indexes pointing at
	// inaccessible things.
	private := false
	switch target.Kind {
	case models.IndexKindRepo:
		ok, accErr := auth.CheckRepoAccess(ctx, user.AccessToken, target.Owner, target.Repo)
		if accErr != nil {
			a.writeFetchError(w, r, "https://github.com/"+target.Owner+"/"+target.Repo, accErr)
			return
		}
		if !ok {
			writeError(w, http.StatusForbidden, "you don't have access to that repo")
			return
		}
		info, infoErr := auth.GetRepoInfo(ctx, user.AccessToken, target.Owner, target.Repo)
		if infoErr == nil && info != nil {
			// GetRepoInfo doesn't expose "private" directly; if we got 200
			// from CheckRepoAccess with no token, it's public. We probe
			// without a token to confirm.
			anonOK, _ := auth.CheckRepoAccess(ctx, "", target.Owner, target.Repo)
			private = !anonOK
		}
	case models.IndexKindUser, models.IndexKindOrg:
		// Profile / org pages are themselves always public; access is
		// per-repo and resolved at view time.
	}

	// Dedupe — if this user already has an index for the same target,
	// return that one instead of minting a new ID.
	existing, _ := a.store.FindIndexBySource(ctx, user.ID, target.Kind, target.Owner, target.Repo)
	if existing != nil {
		a.respondIndexWithItems(w, r, existing)
		return
	}

	now := time.Now().UTC()
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = defaultIndexTitle(target)
	}
	idx := &models.Index{
		ID:          auth.RandomToken(8),
		Kind:        target.Kind,
		Owner:       target.Owner,
		Repo:        target.Repo,
		Title:       title,
		SourceURL:   canonicalIndexURL(target),
		Private:     private,
		CreatedByID: user.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := a.store.InsertIndex(ctx, idx); err != nil {
		internalError(w, "store.insert_index", err)
		return
	}
	a.respondIndexWithItems(w, r, idx)
}

// getIndex implements GET /api/indexes/:id. Re-verifies access on
// every read so a user who lost permission between views stops
// seeing the listing.
func (a *API) getIndex(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	idx, err := a.store.GetIndex(r.Context(), id)
	if err != nil {
		internalError(w, "store.get_index", err)
		return
	}
	if idx == nil {
		writeError(w, http.StatusNotFound, "index not found")
		return
	}
	user := a.currentUser(r)
	if idx.Kind == models.IndexKindRepo && idx.Private {
		if user == nil {
			writeError(w, http.StatusUnauthorized, "sign in required to view this private index")
			return
		}
		ok, accErr := auth.CheckRepoAccess(r.Context(), user.AccessToken, idx.Owner, idx.Repo)
		if accErr != nil || !ok {
			writeError(w, http.StatusForbidden, "you don't have access to this index's source repo")
			return
		}
	}
	a.respondIndexWithItems(w, r, idx)
}

// listMyIndexes implements GET /api/me/indexes. Sign-in required (we
// have no anonymous "mine" notion).
func (a *API) listMyIndexes(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	rows, err := a.store.ListIndexesForUser(r.Context(), user.ID)
	if err != nil {
		internalError(w, "store.list_my_indexes", err)
		return
	}
	if rows == nil {
		rows = []models.Index{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// deleteIndex implements DELETE /api/indexes/:id. Owner-only.
func (a *API) deleteIndex(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	idx, err := a.store.GetIndex(r.Context(), id)
	if err != nil {
		internalError(w, "store.get_index_delete", err)
		return
	}
	if idx == nil {
		writeError(w, http.StatusNotFound, "index not found")
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	if idx.CreatedByID != user.ID {
		writeError(w, http.StatusForbidden, "only the index's creator can delete it")
		return
	}
	if err := a.store.SoftDeleteIndex(r.Context(), id); err != nil {
		internalError(w, "store.soft_delete_index", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// patchIndex implements PATCH /api/indexes/:id — rename only.
func (a *API) patchIndex(w http.ResponseWriter, r *http.Request) {
	capBody(w, r, 1024)
	id := mux.Vars(r)["id"]
	idx, err := a.store.GetIndex(r.Context(), id)
	if err != nil {
		internalError(w, "store.get_index_patch", err)
		return
	}
	if idx == nil {
		writeError(w, http.StatusNotFound, "index not found")
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	if idx.CreatedByID != user.ID {
		writeError(w, http.StatusForbidden, "only the index's creator can rename it")
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(title) > 200 {
		title = title[:200]
	}
	if err := a.store.UpdateIndexTitle(r.Context(), id, title); err != nil {
		internalError(w, "store.update_index_title", err)
		return
	}
	idx.Title = title
	a.respondIndexWithItems(w, r, idx)
}

// respondIndexWithItems performs the live GitHub fetch for an index's
// items and writes the combined response. Centralized so create+get
// share the materialization path. Errors fetching items don't fail
// the response — they're surfaced via the standard fetch-error
// machinery, but the caller still gets the stored metadata.
func (a *API) respondIndexWithItems(w http.ResponseWriter, r *http.Request, idx *models.Index) {
	user := a.currentUser(r)
	token := ""
	viewerLogin := ""
	if user != nil {
		token = user.AccessToken
		viewerLogin = user.Login
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	items, truncated, err := materializeIndex(ctx, idx, token, viewerLogin)
	if err != nil {
		// Surface fetch failures verbatim — the user pasted a URL that
		// GitHub now refuses, and we want them to see the specific reason.
		a.writeFetchError(w, r, idx.SourceURL, err)
		return
	}
	if items == nil {
		items = []indexItem{}
	}
	writeJSON(w, http.StatusOK, indexResponse{
		Index:     *idx,
		Items:     items,
		Truncated: truncated,
	})
}

// materializeIndex performs the live GitHub listing for an index. For
// repo indexes we walk the git tree once and emit every .md file. For
// user/org indexes we list repos (paginated, capped at 1000) and pull
// each repo's top-level .md files in parallel-ish (sequential for
// simplicity, since we're bounded by GitHub's rate limit anyway).
func materializeIndex(ctx context.Context, idx *models.Index, token, viewerLogin string) ([]indexItem, bool, error) {
	switch idx.Kind {
	case models.IndexKindRepo:
		files, truncated, err := auth.ListRepoMarkdownFiles(ctx, token, idx.Owner, idx.Repo, "")
		if err != nil {
			return nil, false, err
		}
		// Resolve default branch once for the constructed URLs.
		info, _ := auth.GetRepoInfo(ctx, token, idx.Owner, idx.Repo)
		ref := "main"
		if info != nil && info.DefaultBranch != "" {
			ref = info.DefaultBranch
		}
		out := make([]indexItem, 0, len(files))
		for _, f := range files {
			out = append(out, indexItem{
				Title:      basename(f.Path),
				URL:        fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", idx.Owner, idx.Repo, ref, f.Path),
				PathInRepo: f.Path,
			})
		}
		return out, truncated, nil
	case models.IndexKindUser, models.IndexKindOrg:
		var repos []auth.RepoSummary
		var err error
		if idx.Kind == models.IndexKindOrg {
			repos, err = auth.ListOrgRepos(ctx, token, idx.Owner)
		} else {
			repos, err = auth.ListUserRepos(ctx, token, idx.Owner, viewerLogin)
		}
		if err != nil {
			return nil, false, err
		}
		out := make([]indexItem, 0, 64)
		for _, r := range repos {
			if r.Archived {
				// Drop archived repos from the listing by default — they're
				// rarely the ones the user means to share around.
				continue
			}
			files, fErr := auth.ListRepoTopLevelMarkdown(ctx, token, r.Owner.Login, r.Name, r.DefaultBranch)
			if fErr != nil {
				// One inaccessible repo shouldn't kill the whole listing
				// — skip and keep going.
				continue
			}
			for _, f := range files {
				out = append(out, indexItem{
					Title:       basename(f.Path),
					URL:         fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", r.Owner.Login, r.Name, r.DefaultBranch, f.Path),
					Repo:        r.FullName,
					RepoURL:     r.HTMLURL,
					PathInRepo:  f.Path,
					Description: r.Description,
					UpdatedAt:   r.PushedAt,
					Private:     r.Private,
				})
			}
		}
		return out, false, nil
	}
	return nil, false, fmt.Errorf("unknown index kind: %s", idx.Kind)
}

func defaultIndexTitle(t parsedIndexTarget) string {
	if t.Repo != "" {
		return t.Owner + "/" + t.Repo
	}
	return t.Owner
}

func canonicalIndexURL(t parsedIndexTarget) string {
	if t.Repo != "" {
		return "https://github.com/" + t.Owner + "/" + t.Repo
	}
	return "https://github.com/" + t.Owner
}

// basename returns the path's final segment. We use it as the file's
// display title in the index listing — the user can already see the
// repo + path columns alongside it.
func basename(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
