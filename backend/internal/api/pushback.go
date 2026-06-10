package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/models"
)

// branchSlugRE matches characters valid in a GitHub branch name segment.
// We auto-generate branches as `markupmarkdown/<slug>-<short>` and
// strip anything outside this set.
var branchSlugRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// pushbackInfoResponse is the GET-info payload the modal uses to render
// its options. It tells the frontend what the user can actually do
// against this repo today (push direct vs PR-only) and supplies sane
// defaults for the form.
type pushbackInfoResponse struct {
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	Path           string `json:"path"`
	DefaultBranch  string `json:"defaultBranch"`
	SourceBranch   string `json:"sourceBranch"`
	CanPushDirect  bool   `json:"canPushDirect"`
	CanOpenPR      bool   `json:"canOpenPR"`
	SuggestedBranch  string `json:"suggestedBranch"`
	SuggestedMessage string `json:"suggestedMessage"`
	SuggestedPRTitle string `json:"suggestedPRTitle"`
	SuggestedPRBody  string `json:"suggestedPRBody"`
	RepoHTMLURL    string `json:"repoHtmlUrl"`
}

// pushbackInfo handles GET /api/documents/:id/pushback/info. Lets the
// frontend render the modal with accurate per-repo permissions and
// reasonable form defaults.
func (a *API) pushbackInfo(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	owner, repo, ref, path, ok := deriveGitHubInfo(doc)
	if !ok {
		writeError(w, http.StatusBadRequest, "this document isn't sourced from GitHub")
		return
	}

	info, err := auth.GetRepoInfo(r.Context(), user.AccessToken, owner, repo)
	if err != nil {
		a.writeFetchError(w, r, doc.SourceURL, err)
		return
	}
	canPush := info.Permissions.Push || info.Permissions.Maintain || info.Permissions.Admin
	defaultBranch := info.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = ref
	}

	writeJSON(w, http.StatusOK, pushbackInfoResponse{
		Owner:            owner,
		Repo:             repo,
		Path:             path,
		DefaultBranch:    defaultBranch,
		SourceBranch:     ref,
		CanPushDirect:    canPush,
		CanOpenPR:        canPush, // creating a PR requires push to a new branch
		SuggestedBranch:  defaultBranchName(doc, user),
		SuggestedMessage: defaultCommitMessage(doc),
		SuggestedPRTitle: defaultPRTitle(doc),
		SuggestedPRBody:  defaultPRBody(doc, a.cfg.Frontend.URL),
		RepoHTMLURL:      info.HTMLURL,
	})
}

// pushbackRequest is the POST body of the actual pushback. Mode picks
// between opening a PR from a new branch vs committing directly to a
// branch (typically the default branch). The frontend offers both
// options and lets the user pick — direct-commit fails server-side
// if GitHub refuses (branch protection, etc.) and we surface that
// error verbatim.
type pushbackRequest struct {
	Mode          string `json:"mode"` // "pr" or "direct"
	Branch        string `json:"branch"`
	CommitMessage string `json:"commitMessage"`
	PRTitle       string `json:"prTitle,omitempty"`
	PRBody        string `json:"prBody,omitempty"`
	// TargetBranch is what direct-commit writes to and what a PR opens
	// against (the base). Defaults to the repo's default branch when
	// empty.
	TargetBranch string `json:"targetBranch,omitempty"`
}

type pushbackResponse struct {
	Mode       string `json:"mode"`
	Branch     string `json:"branch"`
	CommitSHA  string `json:"commitSha"`
	CommitURL  string `json:"commitUrl"`
	PRNumber   int    `json:"prNumber,omitempty"`
	PRURL      string `json:"prUrl,omitempty"`
}

// pushback handles POST /api/documents/:id/pushback. Commits the
// current doc content to GitHub via the user's OAuth token. PR mode:
// creates a new branch + commits + opens a PR. Direct mode: commits
// straight to TargetBranch (must succeed on GitHub's side — branch
// protection rules are enforced there, not here).
func (a *API) pushback(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	// Pushback writes to the user's GitHub on their behalf — gate it
	// at admin scope the same as rename / accept-revision.
	if !a.enforceScope(w, r, models.TokenScopeAdmin) {
		return
	}
	capBody(w, r, maxBodyDefault)

	owner, repo, ref, path, ok := deriveGitHubInfo(doc)
	if !ok {
		writeError(w, http.StatusBadRequest, "this document isn't sourced from GitHub")
		return
	}

	var req pushbackRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode != "pr" && mode != "direct" {
		writeError(w, http.StatusBadRequest, "mode must be 'pr' or 'direct'")
		return
	}
	commitMsg := strings.TrimSpace(req.CommitMessage)
	if commitMsg == "" {
		commitMsg = defaultCommitMessage(doc)
	}
	targetBranch := strings.TrimSpace(req.TargetBranch)
	if targetBranch == "" {
		// Look up the default branch — for the PR mode, that's our
		// base; for direct, that's the branch we write to.
		info, err := auth.GetRepoInfo(r.Context(), user.AccessToken, owner, repo)
		if err != nil {
			a.writeFetchError(w, r, doc.SourceURL, err)
			return
		}
		targetBranch = info.DefaultBranch
		if targetBranch == "" {
			targetBranch = ref
		}
	}

	if mode == "direct" {
		// Get current file SHA on target so the Contents PUT updates
		// rather than creates. A 404 here means the file doesn't
		// exist yet on that branch — we treat that as a create
		// (empty fileSHA below).
		fileSHA, _ := lookupFileSHA(r.Context(), user.AccessToken, owner, repo, path, targetBranch)
		put, err := auth.PutFile(r.Context(), user.AccessToken, owner, repo, path, targetBranch, commitMsg, doc.Content, fileSHA)
		if err != nil {
			a.writePushbackError(w, r, err, "Couldn't commit directly to "+targetBranch+". Your repo may protect this branch.")
			return
		}
		// If we committed straight to the branch the doc tracks, the
		// freshly-pushed blob IS the new "current upstream" — stamp it
		// as the doc's SourceSHA so the next drift check sees us in
		// sync. Without this the banner sticks around telling the user
		// the source has changed, even though the change was theirs.
		// PR mode + non-tracking branches don't qualify: a PR's commit
		// isn't merged yet, and a commit to a different branch doesn't
		// affect the ref we track.
		if put.Content.SHA != "" && refsMatch(targetBranch, doc.GitHubRef) {
			_ = a.store.UpdateDocumentSourceSHA(r.Context(), doc.ID, put.Content.SHA)
			go a.hub.Broadcast(doc.ID, "doc-updated")
		}
		writeJSON(w, http.StatusOK, pushbackResponse{
			Mode:      "direct",
			Branch:    targetBranch,
			CommitSHA: put.Commit.SHA,
			CommitURL: put.Commit.HTMLURL,
		})
		return
	}

	// PR mode.
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = defaultBranchName(doc, user)
	}
	branch = sanitizeBranch(branch)
	if branch == "" {
		writeError(w, http.StatusBadRequest, "branch name is required")
		return
	}

	// Anchor the new branch at the head of target.
	baseSHA, err := auth.GetBranchSHA(r.Context(), user.AccessToken, owner, repo, targetBranch)
	if err != nil {
		a.writePushbackError(w, r, err, "Couldn't read base branch "+targetBranch+".")
		return
	}
	if err := auth.CreateBranch(r.Context(), user.AccessToken, owner, repo, branch, baseSHA); err != nil {
		// 422 typically means the branch already exists. Tell the user
		// rather than silently force-pushing.
		var fe *auth.FetchError
		if errors.As(err, &fe) && fe.StatusCode == http.StatusUnprocessableEntity {
			writeError(w, http.StatusConflict,
				"Branch '"+branch+"' already exists on "+owner+"/"+repo+". Pick a different name or open the existing PR.")
			return
		}
		a.writePushbackError(w, r, err, "Couldn't create branch "+branch+".")
		return
	}

	// New branch == base SHA → the file SHA at path on the new branch
	// is the same as on target. Look it up once and reuse.
	fileSHA, _ := lookupFileSHA(r.Context(), user.AccessToken, owner, repo, path, branch)
	put, err := auth.PutFile(r.Context(), user.AccessToken, owner, repo, path, branch, commitMsg, doc.Content, fileSHA)
	if err != nil {
		a.writePushbackError(w, r, err, "Couldn't commit to "+branch+".")
		return
	}

	prTitle := strings.TrimSpace(req.PRTitle)
	if prTitle == "" {
		prTitle = defaultPRTitle(doc)
	}
	prBody := req.PRBody
	if strings.TrimSpace(prBody) == "" {
		prBody = defaultPRBody(doc, a.cfg.Frontend.URL)
	}
	pr, err := auth.CreatePull(r.Context(), user.AccessToken, owner, repo, targetBranch, branch, prTitle, prBody)
	if err != nil {
		// Branch + commit went through but PR open failed. Tell the
		// user the branch exists so they can open the PR manually.
		a.writePushbackError(w, r, err,
			"Committed to "+branch+" but couldn't open the PR. Open it manually on GitHub.")
		return
	}
	writeJSON(w, http.StatusOK, pushbackResponse{
		Mode:      "pr",
		Branch:    branch,
		CommitSHA: put.Commit.SHA,
		CommitURL: put.Commit.HTMLURL,
		PRNumber:  pr.Number,
		PRURL:     pr.HTMLURL,
	})
}

// refsMatch reports whether branch a and ref b refer to the same
// branch. GitHub URLs sometimes carry `refs/heads/<name>` while the
// pushback request supplies the bare branch name; this helper smooths
// over either form.
func refsMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a = strings.TrimPrefix(a, "refs/heads/")
	b = strings.TrimPrefix(b, "refs/heads/")
	return a == b
}

// lookupFileSHA returns the blob SHA of path on branch, or "" if
// GitHub returns a non-success (file doesn't exist yet — caller treats
// that as a create).
func lookupFileSHA(ctx context.Context, token, owner, repo, path, branch string) (string, error) {
	meta, err := auth.FetchGitHubFileMeta(ctx, token, owner, repo, branch, path)
	if err != nil {
		return "", err
	}
	return meta.SHA, nil
}

// defaultBranchName builds the auto-suggested feature branch name for
// a pushback PR. Format: `markupmarkdown/<title-slug>-<short-id>`.
func defaultBranchName(doc *models.Document, user *models.User) string {
	slug := strings.ToLower(doc.Title)
	slug = strings.TrimSuffix(slug, ".md")
	slug = branchSlugRE.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	short := doc.ID
	if len(short) > 8 {
		short = short[:8]
	}
	prefix := "markupmarkdown"
	if user != nil && user.Login != "" {
		prefix = "mumd/" + user.Login
	}
	if slug == "" {
		return prefix + "/" + short
	}
	return prefix + "/" + slug + "-" + short
}

func defaultCommitMessage(doc *models.Document) string {
	if doc.RevisionMeta != nil && doc.RevisionMeta.Model == "manual" {
		return "Edit " + doc.Title + " via markupmarkdown"
	}
	if doc.RevisionMeta != nil {
		n := len(doc.RevisionMeta.AppliedCommentIDs)
		if n == 1 {
			return "Apply 1 reviewed comment to " + doc.Title + " via markupmarkdown"
		}
		return fmt.Sprintf("Apply %d reviewed comments to %s via markupmarkdown", n, doc.Title)
	}
	return "Update " + doc.Title + " via markupmarkdown"
}

func defaultPRTitle(doc *models.Document) string {
	return defaultCommitMessage(doc)
}

func defaultPRBody(doc *models.Document, frontendURL string) string {
	site := strings.TrimRight(frontendURL, "/")
	if site == "" {
		site = "https://mumd.metavert.io"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "_Pushed from [markupmarkdown](%s/d/%s) — see the review thread and revision history there._\n\n", site, doc.ID)
	if doc.RevisionMeta != nil {
		if doc.RevisionMeta.Model == "manual" {
			fmt.Fprintf(&b, "Manual edit by %s.\n", doc.RevisionMeta.GeneratedBy)
		} else {
			n := len(doc.RevisionMeta.AppliedCommentIDs)
			fmt.Fprintf(&b, "AI revision (%s) by %s, applying %d resolved comment thread(s).\n",
				doc.RevisionMeta.Model, doc.RevisionMeta.GeneratedBy, n)
		}
	} else {
		b.WriteString("Direct push of the document content.\n")
	}
	return b.String()
}

func sanitizeBranch(name string) string {
	name = strings.TrimSpace(name)
	// Disallow leading slashes / dots; collapse anything weird.
	name = strings.TrimLeft(name, "/.")
	parts := strings.Split(name, "/")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		p = branchSlugRE.ReplaceAllString(p, "-")
		p = strings.Trim(p, "-.")
		if p != "" {
			clean = append(clean, p)
		}
	}
	out := strings.Join(clean, "/")
	if len(out) > 240 {
		out = out[:240]
	}
	return out
}

// writePushbackError sanitizes a GitHub error into a structured
// response the frontend can render. Falls back to fallback when the
// error isn't a recognized GitHub shape.
func (a *API) writePushbackError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	var fe *auth.FetchError
	if errors.As(err, &fe) {
		status := fe.StatusCode
		if status == 0 {
			status = http.StatusBadGateway
		}
		writeJSON(w, http.StatusBadRequest, fetchErrorResponse{
			Error:  fallback,
			Kind:   fmt.Sprintf("github_%d", status),
			Detail: trimDetail(fe.Body),
		})
		return
	}
	writeJSON(w, http.StatusBadRequest, fetchErrorResponse{
		Error:  fallback,
		Detail: err.Error(),
	})
	// silence unused-r in case of future logging refactor
	_ = r
	_ = time.Now()
}
