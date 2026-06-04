package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/safefetch"
)

type createDocumentRequest struct {
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
}

type patchDocumentRequest struct {
	Title *string `json:"title,omitempty"`
}

// listDocuments returns only documents the signed-in user has worked on:
// docs they created, AI-revised, or commented/replied on. Private docs the
// user has lost GitHub access to are filtered out. Unauthenticated callers
// get 401 — the frontend shows a "sign in to see your files" message.
func (a *API) listDocuments(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, fetchErrorResponse{
			Error: "Sign in with GitHub to see your recent documents.",
			Kind:  "sign_in_required",
		})
		return
	}

	docs, err := a.store.ListDocumentsForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if docs == nil {
		docs = []models.Document{}
	}

	type summary struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		SourceURL   string `json:"sourceUrl,omitempty"`
		Origin      string `json:"origin"`
		Private     bool   `json:"private,omitempty"`
		GitHubOwner string `json:"githubOwner,omitempty"`
		GitHubRepo  string `json:"githubRepo,omitempty"`
		CreatedAt   string `json:"createdAt"`
		UpdatedAt   string `json:"updatedAt"`
	}
	out := make([]summary, 0, len(docs))
	for _, d := range docs {
		// Filter private docs the user has lost access to. Public docs
		// pass through automatically.
		if d.Private {
			ok, err := repoAccessCache.check(r.Context(), user.ID, user.AccessToken, d.GitHubOwner, d.GitHubRepo)
			if err != nil || !ok {
				continue
			}
		}
		out = append(out, summary{
			ID:          d.ID,
			Title:       d.Title,
			SourceURL:   d.SourceURL,
			Origin:      d.Origin,
			Private:     d.Private,
			GitHubOwner: d.GitHubOwner,
			GitHubRepo:  d.GitHubRepo,
			CreatedAt:   d.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   d.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createDocument(w http.ResponseWriter, r *http.Request) {
	if !a.enforceRate(w, r, a.rlCreateDoc, "Too many documents created in a short window. Try again in a minute.") {
		return
	}
	capBody(w, r, maxBodyDocument)

	var req createDocumentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if len(req.Title) > maxTitleLen {
		writeError(w, http.StatusBadRequest, "title too long")
		return
	}
	if len(req.Content) > maxUploadContent {
		writeError(w, http.StatusBadRequest, "content too large (max 5 MB)")
		return
	}
	if req.URL != "" {
		if _, err := safefetch.ValidateURL(req.URL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	doc := &models.Document{
		ID:        uuid.NewString(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if u := a.currentUser(r); u != nil {
		doc.CreatedByID = u.ID
	}

	if req.URL != "" {
		fetched, err := a.fetchContent(r.Context(), r, req.URL)
		if err != nil {
			a.writeFetchError(w, r, req.URL, err)
			return
		}
		doc.Content = fetched.Content
		doc.SourceURL = req.URL
		doc.Origin = "url"
		doc.Private = fetched.Private
		doc.GitHubOwner = fetched.Owner
		doc.GitHubRepo = fetched.Repo
		doc.GitHubRef = fetched.Ref
		doc.GitHubPath = fetched.Path
		doc.Title = req.Title
		if doc.Title == "" {
			doc.Title = titleFromURL(req.URL)
		}
	} else if req.Content != "" {
		doc.Content = req.Content
		doc.Origin = "upload"
		doc.Title = req.Title
		if doc.Title == "" {
			doc.Title = "Untitled"
		}
	} else {
		writeError(w, http.StatusBadRequest, "either url or content is required")
		return
	}

	if err := a.store.InsertDocument(r.Context(), doc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

type documentResponse struct {
	*models.Document
	Parent             *parentSummary    `json:"parent,omitempty"`
	Children           []revisionSummary `json:"children,omitempty"`
	LatestDescendant   *parentSummary    `json:"latestDescendant,omitempty"`
	// PreviouslyViewedAt is the timestamp of the requester's *previous*
	// open of this doc, before this response. The frontend uses this to
	// mark any comment whose updatedAt is newer as "unread". RFC3339 UTC.
	PreviouslyViewedAt string `json:"previouslyViewedAt,omitempty"`
}

type parentSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type revisionSummary struct {
	ID          string                `json:"id"`
	Title       string                `json:"title"`
	CreatedAt   time.Time             `json:"createdAt"`
	RevisionMeta *models.RevisionMeta `json:"revisionMeta,omitempty"`
}

func (a *API) getDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	resp := documentResponse{Document: doc}
	// Read prior view BEFORE bumping it, so the response reflects the
	// state the user is about to see (unread = new since last visit).
	if u := a.currentUser(r); u != nil {
		if prior, _ := a.store.GetDocumentView(r.Context(), doc.ID, u.ID); prior != nil {
			resp.PreviouslyViewedAt = prior.LastViewedAt.UTC().Format(time.RFC3339Nano)
		}
		a.enqueueView(doc.ID, u.ID)
	}
	if doc.ParentID != "" {
		if parent, _ := a.store.GetDocument(r.Context(), doc.ParentID); parent != nil {
			resp.Parent = &parentSummary{ID: parent.ID, Title: parent.Title}
		}
	}
	if children, _ := a.store.ListChildren(r.Context(), doc.ID); len(children) > 0 {
		for _, c := range children {
			resp.Children = append(resp.Children, revisionSummary{
				ID:           c.ID,
				Title:        c.Title,
				CreatedAt:    c.CreatedAt,
				RevisionMeta: c.RevisionMeta,
			})
		}
		if latest, _ := a.store.LatestDescendant(r.Context(), doc.ID); latest != nil && latest.ID != doc.ID {
			resp.LatestDescendant = &parentSummary{ID: latest.ID, Title: latest.Title}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) patchDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	// Renaming a document is admin-only via token; cookie sessions can.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	var req patchDocumentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		if len(title) > maxTitleLen {
			writeError(w, http.StatusBadRequest, "title too long")
			return
		}
		if err := a.store.UpdateDocumentTitle(r.Context(), id, title); err != nil {
			internalError(w, "store.update_title", err)
			return
		}
	}
	doc, err := a.store.GetDocument(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (a *API) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	// Deleting a document is admin-only via token. A read or write token
	// should not be able to nuke documents.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	// Soft delete — the doc stays in the database for ~30 days so the
	// user can restore from Trash. PurgeExpiredDeletes (run periodically)
	// is what eventually removes it for real.
	if err := a.store.SoftDeleteDocument(r.Context(), id); err != nil {
		internalError(w, "store.soft_delete", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listTrash(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, fetchErrorResponse{
			Error: "Sign in with GitHub to see your trash.",
			Kind:  "sign_in_required",
		})
		return
	}
	docs, err := a.store.ListTrashForUser(r.Context(), user.ID)
	if err != nil {
		internalError(w, "store.list_trash", err)
		return
	}
	if docs == nil {
		docs = []models.Document{}
	}
	type summary struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		DeletedAt string `json:"deletedAt"`
		// Days remaining before the purge sweep removes this doc.
		DaysLeft int `json:"daysLeft"`
	}
	const retentionDays = 30
	out := make([]summary, 0, len(docs))
	for _, d := range docs {
		// Skip private docs the requester has lost GitHub access to.
		if d.Private {
			ok, err := repoAccessCache.check(r.Context(), user.ID, user.AccessToken, d.GitHubOwner, d.GitHubRepo)
			if err != nil || !ok {
				continue
			}
		}
		if d.DeletedAt == nil {
			continue
		}
		daysSince := int(time.Since(*d.DeletedAt).Hours() / 24)
		daysLeft := retentionDays - daysSince
		if daysLeft < 0 {
			daysLeft = 0
		}
		out = append(out, summary{
			ID:        d.ID,
			Title:     d.Title,
			DeletedAt: d.DeletedAt.UTC().Format(time.RFC3339),
			DaysLeft:  daysLeft,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) restoreDocument(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	doc, err := a.store.GetDeletedDocument(r.Context(), id)
	if err != nil {
		internalError(w, "store.get_deleted", err)
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "not in trash")
		return
	}
	// Re-run access check on the would-be-restored doc.
	if doc.Private {
		ok, err := repoAccessCache.check(r.Context(), user.ID, user.AccessToken, doc.GitHubOwner, doc.GitHubRepo)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "you no longer have GitHub access to this doc's source")
			return
		}
	}
	if err := a.store.RestoreDocument(r.Context(), id); err != nil {
		internalError(w, "store.restore", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

type fetchErrorAction struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type fetchErrorResponse struct {
	Error   string             `json:"error"`
	Kind    string             `json:"kind,omitempty"`
	Detail  string             `json:"detail,omitempty"`
	Actions []fetchErrorAction `json:"actions,omitempty"`
}

// writeFetchError translates a fetch error into a structured response the
// frontend can render with clear next-step actions. GitHub access failures
// (private repo / org not granted) are the common case worth dressing up.
func (a *API) writeFetchError(w http.ResponseWriter, r *http.Request, srcURL string, err error) {
	var ghErr *auth.FetchError
	if !errors.As(err, &ghErr) {
		writeJSON(w, http.StatusBadRequest, fetchErrorResponse{
			Error: "Couldn't fetch this URL.",
			Kind:  "fetch_other",
			Detail: err.Error(),
		})
		return
	}

	owner, repo, _, _, isGitHub := parseGitHubBlobURL(srcURL)
	user := a.currentUser(r)
	resp := fetchErrorResponse{}

	switch {
	case ghErr.SSOURL != "":
		resp.Error = "Your GitHub org requires SAML SSO before this app can read its repos."
		resp.Kind = "github_sso"
		resp.Actions = append(resp.Actions, fetchErrorAction{
			Label: "Authorize SSO",
			URL:   ghErr.SSOURL,
		})

	case ghErr.StatusCode == http.StatusUnauthorized:
		resp.Error = "Your GitHub session expired. Sign in again to continue."
		resp.Kind = "github_auth"
		resp.Actions = append(resp.Actions, fetchErrorAction{
			Label: "Sign in with GitHub",
			URL:   "/api/auth/github/login?redirect=/",
		})

	case ghErr.StatusCode == http.StatusForbidden || ghErr.StatusCode == http.StatusNotFound:
		// GitHub returns 404 for "no access to a private repo" to avoid leaking
		// existence. If the user IS signed in, the most likely cause is the
		// org hasn't granted this app access yet.
		if user == nil {
			resp.Error = "This looks like a private repo. Sign in with GitHub to read it."
			resp.Kind = "github_auth"
			resp.Actions = append(resp.Actions, fetchErrorAction{
				Label: "Sign in with GitHub",
				URL:   "/api/auth/github/login?redirect=/",
			})
		} else {
			orgHint := ""
			if isGitHub {
				orgHint = fmt.Sprintf(" The repo's owner (`%s`) may need to approve the access request you sent — ask an admin of that organization to approve markupmarkdown under Org Settings → Third-party Access.", owner)
			}
			resp.Error = "GitHub returned no access for this file." + orgHint
			resp.Kind = "github_access"
			if a.cfg.GitHub.ClientID != "" {
				resp.Actions = append(resp.Actions, fetchErrorAction{
					Label: "Manage GitHub access",
					URL:   "https://github.com/settings/connections/applications/" + a.cfg.GitHub.ClientID,
				})
			}
			if isGitHub && repo != "" {
				resp.Actions = append(resp.Actions, fetchErrorAction{
					Label: fmt.Sprintf("Open %s/%s on GitHub", owner, repo),
					URL:   fmt.Sprintf("https://github.com/%s/%s", owner, repo),
				})
			}
		}

	default:
		resp.Error = fmt.Sprintf("GitHub returned an error (%d).", ghErr.StatusCode)
		resp.Kind = "github_other"
		resp.Detail = trimDetail(ghErr.Body)
	}

	writeJSON(w, http.StatusBadRequest, resp)
}

func trimDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// fetchContent fetches the markdown at rawURL. If the URL is a github.com/blob
// URL and the requester is authenticated, it uses the GitHub Contents API with
// their OAuth token (so private repos work). Otherwise it falls back to a plain
// HTTP fetch against the raw URL.
// fetchedDoc is what fetchContent returns, including metadata used to gate
// access to private GitHub-sourced documents on later reads.
type fetchedDoc struct {
	Content string
	Private bool
	Owner   string
	Repo    string
	Ref     string
	Path    string
}

func (a *API) fetchContent(ctx context.Context, r *http.Request, rawURL string) (*fetchedDoc, error) {
	owner, repo, ref, p, isGitHub := parseGitHubBlobURL(rawURL)
	if !isGitHub {
		c, err := a.fetchURL(ctx, rawURL)
		if err != nil {
			return nil, err
		}
		return &fetchedDoc{Content: c}, nil
	}

	// Try the public raw URL first. If that works the file is public and
	// no access gating is needed for future readers.
	rawContent, rawErr := a.fetchURL(ctx, normalizeGitHubURL(rawURL))
	if rawErr == nil {
		return &fetchedDoc{
			Content: rawContent,
			Private: false,
			Owner:   owner, Repo: repo, Ref: ref, Path: p,
		}, nil
	}
	code := statusCodeFromFetchErr(rawErr)
	if code < 400 || code >= 500 {
		return nil, rawErr
	}

	// Public fetch returned 4xx. If the user is signed in, try the
	// authenticated Contents API — success means the file was private and
	// the current user has access.
	user := a.currentUser(r)
	if user != nil && user.AccessToken != "" {
		c, err := auth.FetchGitHubFileContent(ctx, user.AccessToken, owner, repo, ref, p)
		if err != nil {
			return nil, err
		}
		return &fetchedDoc{
			Content: c,
			Private: true,
			Owner:   owner, Repo: repo, Ref: ref, Path: p,
		}, nil
	}

	// Not signed in — wrap as FetchError so the friendly handler offers a
	// "Sign in with GitHub" action.
	return nil, &auth.FetchError{StatusCode: code, Body: rawErr.Error()}
}

// statusCodeFromFetchErr extracts an HTTP status code from an error of the
// form "http %d" returned by fetchURL. Returns 0 if not a status-coded error.
func statusCodeFromFetchErr(err error) int {
	var code int
	if _, scanErr := fmt.Sscanf(err.Error(), "http %d", &code); scanErr != nil {
		return 0
	}
	return code
}

func parseGitHubBlobURL(raw string) (owner, repo, ref, path string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" {
		return
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" {
		return
	}
	return parts[0], parts[1], parts[3], strings.Join(parts[4:], "/"), true
}

func (a *API) fetchURL(ctx context.Context, rawURL string) (string, error) {
	if _, err := safefetch.ValidateURL(rawURL); err != nil {
		return "", err
	}

	client := safefetch.Client(a.cfg.Fetch.ParseTimeout())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "markupmarkdown/0.1")
	req.Header.Set("Accept", "text/markdown, text/plain, text/*;q=0.8, */*;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, a.cfg.Fetch.MaxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if int64(len(body)) > a.cfg.Fetch.MaxBytes {
		return "", fmt.Errorf("file exceeds max size (%d bytes)", a.cfg.Fetch.MaxBytes)
	}
	return string(body), nil
}

// normalizeGitHubURL converts github.com/{owner}/{repo}/blob/{branch}/{path}
// to the corresponding raw.githubusercontent.com URL.
func normalizeGitHubURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Host != "github.com" {
		return raw
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" {
		return raw
	}
	owner, repo, branch := parts[0], parts[1], parts[3]
	rest := strings.Join(parts[4:], "/")
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, rest)
}

func titleFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		return raw
	}
	return base
}
