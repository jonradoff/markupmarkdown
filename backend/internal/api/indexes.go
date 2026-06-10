package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
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
	// Return the meta IMMEDIATELY without materializing items. The
	// frontend navigates to /i/{slug} and opens the SSE stream, so
	// the user sees progress within a few hundred ms instead of
	// staring at "Loading…" for the duration of the org scan.
	writeJSON(w, http.StatusOK, indexResponse{
		Index: *idx,
		Items: []indexItem{},
	})
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
	// Filter out Forgotten indexes — the index still exists (share
	// link works for others), but the user has dismissed it from
	// their home list.
	hidden, _ := a.store.HiddenItemIDs(r.Context(), user.ID, "index")
	if len(hidden) > 0 {
		live := rows[:0]
		for _, i := range rows {
			if !hidden[i.ID] {
				live = append(live, i)
			}
		}
		rows = live
	}
	writeJSON(w, http.StatusOK, rows)
}

// forgetIndex implements POST /api/indexes/:id/forget. Per-user
// soft-hide — the index isn't deleted (the share link still resolves
// for anyone with it), but it disappears from this user's home list.
// Owner doesn't need to be the creator; anyone who's seen a share
// link can hide it from their own clutter.
func (a *API) forgetIndex(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	idx, err := a.store.GetIndex(r.Context(), id)
	if err != nil {
		internalError(w, "store.get_index_forget", err)
		return
	}
	if idx == nil {
		writeError(w, http.StatusNotFound, "index not found")
		return
	}
	if err := a.store.HideItem(r.Context(), user.ID, "index", id); err != nil {
		internalError(w, "store.hide_index", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

// patchIndex implements PATCH /api/indexes/:id — rename + pin the
// default filter for share-link visitors. Both fields are optional
// in the body; only the fields supplied are updated. Creator-only.
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
		writeError(w, http.StatusForbidden, "only the index's creator can edit it")
		return
	}
	var body struct {
		Title         *string `json:"title,omitempty"`
		DefaultFilter *string `json:"defaultFilter,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
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
	}
	if body.DefaultFilter != nil {
		filter := strings.TrimSpace(*body.DefaultFilter)
		if len(filter) > 100 {
			filter = filter[:100]
		}
		if err := a.store.UpdateIndexDefaultFilter(r.Context(), id, filter); err != nil {
			internalError(w, "store.update_index_default_filter", err)
			return
		}
		idx.DefaultFilter = filter
	}
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
	// User/org indexes can take 30+ s when the org has many repos; bump
	// the timeout accordingly. Repo indexes are bounded by one tree
	// fetch and stay under 10 s.
	timeout := 60 * time.Second
	if idx.Kind == models.IndexKindRepo {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// For non-streaming callers we collect the events into a single
	// payload. The streaming variant emits them as SSE in real time.
	events := make(chan progressEvent, 64)
	var (
		items     []indexItem
		truncated bool
		fetchErr  error
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fetchErr = materializeIndexStreaming(ctx, idx, token, viewerLogin, events)
	}()
	for ev := range events {
		if len(ev.Items) > 0 {
			items = append(items, ev.Items...)
		}
		if ev.Truncated {
			truncated = true
		}
	}
	<-done
	if fetchErr != nil {
		a.writeFetchError(w, r, idx.SourceURL, fetchErr)
		return
	}
	if items == nil {
		items = []indexItem{}
	}
	// Stable sort so the listing isn't reshuffled by goroutine timing.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Repo != items[j].Repo {
			return items[i].Repo < items[j].Repo
		}
		return items[i].PathInRepo < items[j].PathInRepo
	})
	writeJSON(w, http.StatusOK, indexResponse{
		Index:     *idx,
		Items:     items,
		Truncated: truncated,
	})
}

// streamIndexItems implements GET /api/indexes/:id/stream. Emits
// progress events as Server-Sent Events while the index materializes,
// so the frontend can show "Scanning 47/142 repos" and append items
// the moment each repo lands instead of staring at a blank "Loading…"
// for 30+ seconds. Access checks mirror getIndex.
func (a *API) streamIndexItems(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	idx, err := a.store.GetIndex(r.Context(), id)
	if err != nil {
		internalError(w, "store.get_index_stream", err)
		return
	}
	if idx == nil {
		writeError(w, http.StatusNotFound, "index not found")
		return
	}
	user := a.currentUser(r)
	if idx.Kind == models.IndexKindRepo && idx.Private {
		if user == nil {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		ok, accErr := a.checkPrivateRepoAccess(r.Context(), user, idx)
		if accErr != nil || !ok {
			writeError(w, http.StatusForbidden, "you don't have access to this index's source repo")
			return
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx/Cloudflare proxy buffering
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.WriteHeader(http.StatusOK)

	token := ""
	viewerLogin := ""
	if user != nil {
		token = user.AccessToken
		viewerLogin = user.Login
	}
	timeout := 90 * time.Second
	if idx.Kind == models.IndexKindRepo {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	events := make(chan progressEvent, 64)
	done := make(chan error, 1)
	go func() {
		done <- materializeIndexStreaming(ctx, idx, token, viewerLogin, events)
	}()

	emit := func(kind string, payload any) {
		data, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
		flusher.Flush()
	}
	// First event is the index meta so the frontend can render
	// title / source / private-flag without a separate round trip.
	emit("meta", idx)
	// Then a "ready" so the UI can swap "Loading…" for "Starting scan…"
	// before the first GitHub round-trip lands.
	emit("ready", map[string]any{"kind": idx.Kind})

	// Heartbeat every 10s so proxies don't time out an in-flight scan.
	hb := time.NewTicker(10 * time.Second)
	defer hb.Stop()

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				err := <-done
				if err != nil {
					emit("error", map[string]string{"message": err.Error()})
				} else {
					emit("done", map[string]bool{"ok": true})
				}
				return
			}
			emit(ev.Kind, ev)
		case <-hb.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// checkPrivateRepoAccess wraps the repo-access check for private
// indexes — pulled out so both getIndex and streamIndexItems share the
// same gate.
func (a *API) checkPrivateRepoAccess(ctx context.Context, user *models.User, idx *models.Index) (bool, error) {
	return auth.CheckRepoAccess(ctx, user.AccessToken, idx.Owner, idx.Repo)
}

// progressEvent is the unit of work materializeIndexStreaming pushes
// onto its sink channel. Three flavors:
//   - "status": just a human-readable message ("Looking up beamable…")
//   - "scanning": progress markers (Current/Total) during a multi-repo scan
//   - "items": a batch of items to append to the index (typically one
//     repo's worth)
type progressEvent struct {
	Kind      string      `json:"kind"`
	Message   string      `json:"message,omitempty"`
	Current   int         `json:"current,omitempty"`
	Total     int         `json:"total,omitempty"`
	Repo      string      `json:"repo,omitempty"`
	Items     []indexItem `json:"items,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
}

// materializeIndexStreaming performs the live GitHub listing for an
// index and pushes progress events as it goes. Closes `events` on
// return so consumers can range over it. Errors bail the operation
// (vs. per-repo errors which just skip that repo).
//
// For user/org indexes we fan out the per-repo fetches across a
// bounded worker pool so a 142-repo org doesn't pay 142 * RTT
// sequentially. 8 workers is chosen to stay well under GitHub's
// 5000-req/hour authenticated limit even on the worst case.
func materializeIndexStreaming(ctx context.Context, idx *models.Index, token, viewerLogin string, events chan<- progressEvent) (err error) {
	defer close(events)
	send := func(ev progressEvent) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	}

	switch idx.Kind {
	case models.IndexKindRepo:
		send(progressEvent{Kind: "status", Message: "Reading repo tree…"})
		files, truncated, fErr := auth.ListRepoMarkdownFiles(ctx, token, idx.Owner, idx.Repo, "")
		if fErr != nil {
			return fErr
		}
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
		send(progressEvent{Kind: "items", Items: out, Truncated: truncated, Repo: idx.Owner + "/" + idx.Repo})
		return nil

	case models.IndexKindUser, models.IndexKindOrg:
		send(progressEvent{Kind: "status", Message: "Looking up " + idx.Owner + " on GitHub…"})
		var repos []auth.RepoSummary
		var listErr error
		if idx.Kind == models.IndexKindOrg {
			repos, listErr = auth.ListOrgRepos(ctx, token, idx.Owner)
		} else {
			repos, listErr = auth.ListUserRepos(ctx, token, idx.Owner, viewerLogin)
		}
		if listErr != nil {
			return listErr
		}
		// Drop archived repos up-front so the progress total reflects
		// real work, not skipped-immediately rows.
		live := make([]auth.RepoSummary, 0, len(repos))
		for _, r := range repos {
			if !r.Archived {
				live = append(live, r)
			}
		}
		total := len(live)
		send(progressEvent{
			Kind:    "scanning",
			Message: fmt.Sprintf("Scanning %d repo%s for markdown…", total, plural(total)),
			Current: 0, Total: total,
		})

		// Worker pool. 8 in flight is a sweet spot: enough that beamable's
		// ~150 repos complete in under 10 s, low enough to stay under
		// GitHub's secondary rate limits.
		const workers = 8
		var (
			wg     sync.WaitGroup
			mu     sync.Mutex
			doneN  int
		)
		jobs := make(chan auth.RepoSummary)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for r := range jobs {
					files, fErr := auth.ListRepoTopLevelMarkdown(ctx, token, r.Owner.Login, r.Name, r.DefaultBranch)
					mu.Lock()
					doneN++
					n := doneN
					mu.Unlock()
					if fErr != nil {
						// One inaccessible / 404 repo doesn't kill the listing
						// — bump the counter and move on.
						send(progressEvent{
							Kind:    "scanning",
							Message: fmt.Sprintf("Skipped %s (no access)", r.FullName),
							Current: n, Total: total,
							Repo: r.FullName,
						})
						continue
					}
					batch := make([]indexItem, 0, len(files))
					for _, f := range files {
						batch = append(batch, indexItem{
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
					send(progressEvent{
						Kind:    "scanning",
						Message: fmt.Sprintf("Scanned %s (%d markdown file%s)", r.FullName, len(batch), plural(len(batch))),
						Current: n, Total: total,
						Repo:  r.FullName,
						Items: batch,
					})
				}
			}()
		}
		for _, r := range live {
			select {
			case jobs <- r:
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				return ctx.Err()
			}
		}
		close(jobs)
		wg.Wait()
		return nil
	}
	return fmt.Errorf("unknown index kind: %s", idx.Kind)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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
