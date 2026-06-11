package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/ai"
	"markupmarkdown/internal/auth"
	"markupmarkdown/internal/mcpserver"
	"markupmarkdown/internal/models"
)

// This file implements the Tier 1 + Tier 2 mcpserver.API methods —
// edit, merge, patch-anchor, push-to-github, list-revisions,
// delete-comment. Each mirrors the equivalent REST handler's
// behaviour minus the http.ResponseWriter plumbing.

// --- EditDocument ---

func (a *API) EditDocument(ctx context.Context, userID, docID, content, tokenID, agentLabel string) (*models.Document, error) {
	parent, err := a.mcpDocAccess(ctx, userID, docID)
	if err != nil {
		return nil, err
	}
	body := strings.TrimRight(content, "\n") + "\n"
	if strings.TrimSpace(body) == "" {
		return nil, errors.New("`content` is required")
	}
	if strings.TrimSpace(body) == strings.TrimSpace(parent.Content) {
		return nil, errors.New("edit is identical to the current content — nothing to save")
	}

	u, _ := a.store.GetUser(ctx, userID)
	generatedBy := preferName(u)
	actor := models.ActorKind("")
	if tokenID != "" {
		actor = models.ActorAgent
		if agentLabel != "" {
			generatedBy = agentLabel
		}
	}

	now := time.Now().UTC()
	doc := &models.Document{
		ID:          uuid.NewString(),
		Title:       parent.Title,
		Origin:      parent.Origin,
		SourceURL:   parent.SourceURL,
		Content:     body,
		Private:     parent.Private,
		GitHubOwner: parent.GitHubOwner,
		GitHubRepo:  parent.GitHubRepo,
		GitHubRef:   parent.GitHubRef,
		GitHubPath:  parent.GitHubPath,
		SourceSHA:   parent.SourceSHA,
		ParentID:    parent.ID,
		CreatedByID: userID,
		RevisionMeta: &models.RevisionMeta{
			Model:             "manual",
			GeneratedBy:       generatedBy,
			GeneratedByID:     userID,
			GeneratedAt:       now,
			ActorKind:         actor,
			TokenID:           tokenID,
			AncestorSourceSHA: parent.SourceSHA,
			AncestorContent:   parent.Content,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.store.InsertDocument(ctx, doc); err != nil {
		return nil, sanitizeStoreErr("mcp.edit.insert_document", err)
	}
	a.copyOpenCommentsToChild(ctx, parent.ID, doc)
	return doc, nil
}

// --- MergeFromGitHub ---

func (a *API) MergeFromGitHub(ctx context.Context, userID, docID, accessToken, tokenID string) (*mcpserver.MergeOutput, error) {
	_ = tokenID
	doc, err := a.DocAccess(ctx, userID, docID, accessToken)
	if err != nil {
		return nil, err
	}
	owner, repo, ref, p, ok := deriveGitHubInfo(doc)
	if !ok {
		return nil, errors.New("this document isn't sourced from GitHub")
	}
	token := ""
	if doc.Private {
		token = accessToken
	}
	meta, err := auth.FetchGitHubFileMeta(ctx, token, owner, repo, ref, p)
	if err != nil {
		return nil, fmt.Errorf("fetch upstream: %w", err)
	}
	if meta.SHA == "" {
		return nil, errors.New("GitHub returned no SHA for this file")
	}

	// Resolve ancestor: from revision_meta for a child revision, or
	// the doc's current content for a root (merge collapses to "use
	// upstream").
	ancestorContent := doc.Content
	if doc.RevisionMeta != nil && doc.RevisionMeta.AncestorContent != "" {
		ancestorContent = doc.RevisionMeta.AncestorContent
	}
	trim := strings.TrimSpace

	// Trivial pre-flight paths — skip Claude entirely.
	if trim(ancestorContent) == trim(meta.Content) || trim(doc.Content) == trim(meta.Content) {
		return &mcpserver.MergeOutput{
			DocumentID:        doc.ID,
			UpstreamSourceSHA: meta.SHA,
			Model:             "noop",
			NoMergeNeeded:     true,
		}, a.persistMerge(ctx, doc, doc.Content, meta.Content, meta.SHA, nil)
	}
	if trim(ancestorContent) == trim(doc.Content) {
		// No revision yet — use upstream verbatim.
		if err := a.persistMerge(ctx, doc, meta.Content, meta.Content, meta.SHA, nil); err != nil {
			return nil, err
		}
		return &mcpserver.MergeOutput{
			DocumentID:        doc.ID,
			UpstreamSourceSHA: meta.SHA,
			Model:             "noop",
			NoMergeNeeded:     true,
		}, nil
	}

	// Real merge needs the user's Anthropic key.
	apiKey, err := a.decryptedAnthropicKey(ctx, userID)
	if err != nil {
		return nil, sanitizeStoreErr("mcp.merge.decrypt_key", err)
	}
	if apiKey == "" {
		return nil, errors.New("no Anthropic API key on file for this user — add one before running a merge that needs Claude")
	}
	result, err := ai.Merge(ctx, apiKey, doc.Title, ancestorContent, doc.Content, meta.Content, nil)
	if err != nil {
		return nil, err
	}
	out := &mcpserver.MergeOutput{
		DocumentID:        doc.ID,
		UpstreamSourceSHA: meta.SHA,
		Model:             result.Model,
		TokensIn:          result.TokensIn,
		TokensOut:         result.TokensOut,
	}
	if err := a.persistMerge(ctx, doc, result.Content, meta.Content, meta.SHA, out); err != nil {
		return nil, err
	}
	return out, nil
}

// persistMerge writes the merged content to the doc, re-anchors
// every comment against the new content, bumps the ancestor on
// revision_meta so the NEXT merge has the right baseline, and
// broadcasts. Mirrors the REST mergeAcceptSource handler.
func (a *API) persistMerge(ctx context.Context, doc *models.Document, mergedContent, upstreamContent, upstreamSourceSHA string, out *mcpserver.MergeOutput) error {
	mergedContent = strings.TrimRight(mergedContent, "\n") + "\n"
	comments, err := a.store.ListComments(ctx, doc.ID)
	if err != nil {
		return sanitizeStoreErr("mcp.merge.list_comments", err)
	}
	results := reanchorComments(comments, mergedContent)
	clean, orphan := 0, 0
	now := time.Now().UTC()
	for i, res := range results {
		c := &comments[i]
		switch res.Status {
		case reanchorClean:
			update := bson.M{"$set": bson.M{
				"anchor.start": 0,
				"anchor.end":   0,
				"anchor.exact": res.Exact,
				"updated_at":   now,
			}}
			if c.Orphan {
				update["$unset"] = bson.M{"orphan": "", "original_exact": ""}
			}
			if _, err := a.store.Comments().UpdateOne(ctx, bson.M{"_id": c.ID}, update); err != nil {
				return sanitizeStoreErr("mcp.merge.update_anchor", err)
			}
			clean++
		case reanchorOrphan:
			if c.Orphan {
				orphan++
				continue
			}
			if _, err := a.store.Comments().UpdateOne(ctx,
				bson.M{"_id": c.ID},
				bson.M{"$set": bson.M{
					"orphan":         true,
					"original_exact": res.OriginalExact,
					"updated_at":     now,
				}}); err != nil {
				return sanitizeStoreErr("mcp.merge.mark_orphan", err)
			}
			orphan++
		case reanchorDocLevel:
		}
	}
	set := bson.M{
		"content":           mergedContent,
		"source_sha":        upstreamSourceSHA,
		"source_checked_at": now,
		"updated_at":        now,
	}
	if doc.RevisionMeta != nil {
		set["revision_meta.ancestor_content"] = upstreamContent
		set["revision_meta.ancestor_source_sha"] = upstreamSourceSHA
	}
	if _, err := a.store.Documents().UpdateOne(ctx, bson.M{"_id": doc.ID},
		bson.M{"$set": set, "$unset": bson.M{"source_latest_sha": "", "source_drifted_at": ""}}); err != nil {
		return sanitizeStoreErr("mcp.merge.persist", err)
	}
	a.hub.Broadcast(doc.ID, "doc-updated")
	a.hub.Broadcast(doc.ID, "comments-updated")
	if out != nil {
		out.CleanCount = clean
		out.OrphanCount = orphan
	}
	return nil
}

// --- PatchCommentAnchor ---

func (a *API) PatchCommentAnchor(ctx context.Context, userID, commentID string, opts mcpserver.CommentAnchorOpts) (*models.Comment, error) {
	if _, _, err := a.mcpDocAccessForComment(ctx, userID, commentID); err != nil {
		return nil, err
	}
	existing, err := a.store.GetComment(ctx, commentID)
	if err != nil || existing == nil {
		return nil, errors.New("comment not found")
	}
	doc, err := a.store.GetDocument(ctx, existing.DocumentID)
	if err != nil || doc == nil {
		return nil, errors.New("document not found")
	}
	// require-mine: agent-owned comments stamp AuthorID to the token
	// owner, so the same equality check covers human + bot.
	if existing.AuthorID != userID {
		return nil, errors.New("you can only re-anchor comments you (or a bot you own) created")
	}

	set := bson.M{"updated_at": time.Now().UTC()}
	unset := bson.M{}
	if opts.DocLevel {
		set["anchor.start"] = 0
		set["anchor.end"] = 0
		set["anchor.exact"] = ""
		set["anchor.prefix"] = ""
		set["anchor.suffix"] = ""
		unset["orphan"] = ""
		unset["original_exact"] = ""
	} else {
		if err := validateManualAnchor(patchCommentAnchorRequest{
			Start:  opts.Start,
			End:    opts.End,
			Exact:  opts.Exact,
			Prefix: opts.Prefix,
			Suffix: opts.Suffix,
		}, doc.Content); err != nil {
			return nil, err
		}
		set["anchor.start"] = opts.Start
		set["anchor.end"] = opts.End
		set["anchor.exact"] = opts.Exact
		set["anchor.prefix"] = opts.Prefix
		set["anchor.suffix"] = opts.Suffix
		unset["orphan"] = ""
		unset["original_exact"] = ""
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	if _, err := a.store.Comments().UpdateOne(ctx, bson.M{"_id": commentID}, update); err != nil {
		return nil, sanitizeStoreErr("mcp.patch_anchor.update", err)
	}
	updated, err := a.store.GetComment(ctx, commentID)
	if err != nil {
		return nil, sanitizeStoreErr("mcp.patch_anchor.get", err)
	}
	a.hub.Broadcast(doc.ID, "comments-updated")
	a.resolveAgentIdentity(ctx, updated)
	return updated, nil
}

// --- DeleteComment ---

func (a *API) DeleteComment(ctx context.Context, userID, commentID string) error {
	if _, _, err := a.mcpDocAccessForComment(ctx, userID, commentID); err != nil {
		return err
	}
	c, err := a.store.GetComment(ctx, commentID)
	if err != nil || c == nil {
		return errors.New("comment not found")
	}
	if c.AuthorID != userID {
		return errors.New("you can only delete comments you (or a bot you own) created")
	}
	if err := a.store.DeleteComment(ctx, commentID); err != nil {
		return sanitizeStoreErr("mcp.delete_comment.delete", err)
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	return nil
}

// --- PushToGitHub (PR mode only) ---

func (a *API) PushToGitHub(ctx context.Context, userID, docID, accessToken string, opts mcpserver.PushbackOpts) (*mcpserver.PushbackOutput, error) {
	doc, err := a.DocAccess(ctx, userID, docID, accessToken)
	if err != nil {
		return nil, err
	}
	owner, repo, ref, path, ok := deriveGitHubInfo(doc)
	if !ok {
		return nil, errors.New("this document isn't sourced from GitHub")
	}
	u, _ := a.store.GetUser(ctx, userID)
	if u == nil {
		return nil, errors.New("user not found")
	}

	targetBranch := strings.TrimSpace(opts.TargetBranch)
	if targetBranch == "" {
		info, err := auth.GetRepoInfo(ctx, u.AccessToken, owner, repo)
		if err != nil {
			return nil, fmt.Errorf("repo info: %w", err)
		}
		targetBranch = info.DefaultBranch
		if targetBranch == "" {
			targetBranch = ref
		}
	}
	branch := sanitizeBranch(strings.TrimSpace(opts.Branch))
	if branch == "" {
		branch = sanitizeBranch(defaultBranchName(doc, u))
	}
	commitMsg := strings.TrimSpace(opts.CommitMessage)
	if commitMsg == "" {
		commitMsg = defaultCommitMessage(doc)
	}
	prTitle := strings.TrimSpace(opts.PRTitle)
	if prTitle == "" {
		prTitle = defaultPRTitle(doc)
	}
	prBody := opts.PRBody
	if strings.TrimSpace(prBody) == "" {
		prBody = defaultPRBody(doc, a.cfg.Frontend.URL)
	}

	baseSHA, err := auth.GetBranchSHA(ctx, u.AccessToken, owner, repo, targetBranch)
	if err != nil {
		return nil, fmt.Errorf("base sha: %w", err)
	}
	if err := auth.CreateBranch(ctx, u.AccessToken, owner, repo, branch, baseSHA); err != nil {
		var fe *auth.FetchError
		if errors.As(err, &fe) && fe.StatusCode == http.StatusUnprocessableEntity {
			return nil, fmt.Errorf("branch %q already exists on %s/%s — pick a different name or use the existing PR", branch, owner, repo)
		}
		return nil, fmt.Errorf("create branch: %w", err)
	}
	fileSHA, _ := lookupFileSHA(ctx, u.AccessToken, owner, repo, path, branch)
	put, err := auth.PutFile(ctx, u.AccessToken, owner, repo, path, branch, commitMsg, doc.Content, fileSHA)
	if err != nil {
		return nil, fmt.Errorf("commit file: %w", err)
	}
	pr, err := auth.CreatePull(ctx, u.AccessToken, owner, repo, targetBranch, branch, prTitle, prBody)
	if err != nil {
		return nil, fmt.Errorf("open pr: %w", err)
	}
	return &mcpserver.PushbackOutput{
		Branch:    branch,
		CommitSHA: put.Commit.SHA,
		CommitURL: put.Commit.HTMLURL,
		PRNumber:  pr.Number,
		PRURL:     pr.HTMLURL,
	}, nil
}

// --- ListRevisions ---

func (a *API) ListRevisions(ctx context.Context, userID, docID, accessToken string) (*mcpserver.RevisionChain, error) {
	current, err := a.DocAccess(ctx, userID, docID, accessToken)
	if err != nil {
		return nil, err
	}
	root, err := a.store.RootDocument(ctx, current.ID)
	if err != nil || root == nil {
		root = current
	}
	// Walk the chain via the most-recent-child raw edge, including
	// soft-deleted nodes so revision indices stay stable.
	nodes := []mcpserver.RevisionNode{makeRevisionNode(root, 1, a.cfg.Frontend.URL)}
	currentDepth := 0
	if root.ID == current.ID {
		currentDepth = 1
	}
	seen := map[string]bool{root.ID: true}
	cursor := root.ID
	idx := 1
	for {
		children, err := a.store.ListChildrenRaw(ctx, cursor)
		if err != nil || len(children) == 0 {
			break
		}
		next := children[0]
		for i := 1; i < len(children); i++ {
			if children[i].CreatedAt.After(next.CreatedAt) {
				next = children[i]
			}
		}
		if seen[next.ID] {
			break
		}
		seen[next.ID] = true
		idx++
		nodes = append(nodes, makeRevisionNode(&next, idx, a.cfg.Frontend.URL))
		if next.ID == current.ID {
			currentDepth = idx
		}
		cursor = next.ID
	}
	_ = currentDepth
	return &mcpserver.RevisionChain{
		CurrentID: current.ID,
		Nodes:     nodes,
	}, nil
}

func makeRevisionNode(d *models.Document, idx int, siteURL string) mcpserver.RevisionNode {
	node := mcpserver.RevisionNode{
		ID:            d.ID,
		URL:           strings.TrimRight(siteURL, "/") + "/d/" + d.ID,
		RevisionIndex: idx,
		ParentID:      d.ParentID,
		CreatedAt:     d.CreatedAt,
		Deleted:       d.DeletedAt != nil,
	}
	if d.RevisionMeta != nil {
		node.Model = d.RevisionMeta.Model
		node.AppliedCommentIDs = d.RevisionMeta.AppliedCommentIDs
		node.GeneratedBy = d.RevisionMeta.GeneratedBy
		node.ActorKind = string(d.RevisionMeta.ActorKind)
	}
	return node
}

// ensure base64 is referenced when needed for tests/future expansion
var _ = base64.StdEncoding
