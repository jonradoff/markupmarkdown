package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"markupmarkdown/internal/models"
)

// This file holds the Tier 1 + Tier 2 tool definitions and handlers
// kept out of server.go so the original file stays readable. Every
// tool here goes through the same scope + rate + access checks the
// REST layer uses; mcpapi.go implements the underlying behaviour.

// --- edit_document ---

func editDocTool() mcp.Tool {
	return mcp.NewTool("edit_document",
		mcp.WithDescription(`Apply a targeted edit to a Markdown document. Creates a new child revision in the chain (the parent is left untouched), carries unresolved comments forward, and re-anchors them against the new content.

Use this when you've decided on a specific change and want to apply it yourself — without going through revise_with_ai. For multi-thread reviewer-driven edits, prefer revise_with_ai with accept=true.`),
		mcp.WithString("document_id", mcp.Required(), mcp.Description("Document UUID to edit.")),
		mcp.WithString("content", mcp.Required(), mcp.Description("The full new document content. Markdown. Sent verbatim — provide the entire document, not a diff.")),
	)
}

func (h *handlers) editDoc(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	if res, ok := requireScope(id, models.TokenScopeAdmin); !ok {
		return res, nil
	}
	docID := req.GetString("document_id", "")
	content := req.GetString("content", "")
	if docID == "" || content == "" {
		return errorResult("`document_id` and `content` are required")
	}
	doc, err := h.api.EditDocument(ctx, id.User.ID, docID, content, id.TokenID, id.Label)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	h.api.LogTokenAction(ctx, id.TokenID, "revision.manual", docID)
	return jsonResult(map[string]any{
		"id":            doc.ID,
		"parentId":      doc.ParentID,
		"title":         doc.Title,
		"url":           h.siteURL + "/d/" + doc.ID,
		"revisionMeta":  doc.RevisionMeta,
		"createdAt":     doc.CreatedAt,
	})
}

// --- merge_from_github ---

func mergeTool() mcp.Tool {
	return mcp.NewTool("merge_from_github",
		mcp.WithDescription(`Reconcile this document with the latest upstream content from its source GitHub file. Runs the 3-way Claude merge (ancestor = the source content the revision was based on, ours = current doc content, theirs = new upstream content) and persists the result in place. Comments are re-anchored against the merged content; ones whose quoted text can't be located become orphans.

Use this when you've detected upstream drift on a doc and want to incorporate the changes while preserving prior AI revisions / manual edits. Trivial cases (no AI revision, or upstream identical to ours) bypass Claude and complete instantly.`),
		mcp.WithString("document_id", mcp.Required(), mcp.Description("Document UUID to merge.")),
	)
}

func (h *handlers) merge(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	if res, ok := requireScope(id, models.TokenScopeAdmin); !ok {
		return res, nil
	}
	docID := req.GetString("document_id", "")
	if docID == "" {
		return errorResult("`document_id` is required")
	}
	if !h.api.AllowMergeRate(id.User.ID) {
		return errorResult("rate limited: too many merges in a short window")
	}
	release, ok := h.api.AcquireReviseSlot(id.User.ID)
	if !ok {
		return errorResult("you already have the maximum (3) AI revisions in flight")
	}
	defer release()
	out, err := h.api.MergeFromGitHub(ctx, id.User.ID, docID, id.User.AccessToken, id.TokenID)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	h.api.LogTokenAction(ctx, id.TokenID, "merge.accept", docID)
	return jsonResult(out)
}

// --- patch_anchor ---

func patchAnchorTool() mcp.Tool {
	return mcp.NewTool("patch_anchor",
		mcp.WithDescription(`Re-anchor a comment that has become an orphan after upstream changes, or convert any comment to a document-level pin (no inline highlight). You can only re-anchor comments you (or an agent token you own) wrote.

Supply either (start, end, quoted_text) for an inline anchor — quoted_text must appear somewhere in the current doc — or doc_level=true to drop the inline anchor entirely.`),
		mcp.WithString("comment_id", mcp.Required(), mcp.Description("Comment UUID to re-anchor.")),
		mcp.WithBoolean("doc_level", mcp.Description("If true, pin as a document-level comment (no inline highlight).")),
		mcp.WithNumber("start", mcp.Description("Inline mode: textContent-relative start offset.")),
		mcp.WithNumber("end", mcp.Description("Inline mode: textContent-relative end offset.")),
		mcp.WithString("quoted_text", mcp.Description("Inline mode: verbatim text the comment now refers to.")),
		mcp.WithString("prefix", mcp.Description("Optional disambiguating prefix context.")),
		mcp.WithString("suffix", mcp.Description("Optional disambiguating suffix context.")),
	)
}

func (h *handlers) patchAnchor(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	if res, ok := requireScope(id, models.TokenScopeWrite); !ok {
		return res, nil
	}
	cid := req.GetString("comment_id", "")
	if cid == "" {
		return errorResult("`comment_id` is required")
	}
	opts := CommentAnchorOpts{
		Start:    int(req.GetFloat("start", 0)),
		End:      int(req.GetFloat("end", 0)),
		Exact:    req.GetString("quoted_text", ""),
		Prefix:   req.GetString("prefix", ""),
		Suffix:   req.GetString("suffix", ""),
		DocLevel: req.GetBool("doc_level", false),
	}
	c, err := h.api.PatchCommentAnchor(ctx, id.User.ID, cid, opts)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	docID := ""
	if c != nil {
		docID = c.DocumentID
	}
	h.api.LogTokenAction(ctx, id.TokenID, "comment.anchor.patched", docID)
	return jsonResult(c)
}

// --- push_to_github (PR mode only) ---

func pushTool() mcp.Tool {
	return mcp.NewTool("push_to_github",
		mcp.WithDescription(`Push the document's current content back to its source GitHub repo as a pull request. Creates a new branch, commits the file, and opens a PR against the repo's default branch (or target_branch if supplied).

Agent guidance: only push when a human has explicitly asked for it (e.g. resolved a "ship when ready" comment). Never push autonomously off a schedule.

PR-only here for safety. The web UI exposes direct-commit; programmatic direct-commit is intentionally not supported — branch protection is the repo owner's call to enforce server-side on GitHub, not ours to make easy to bypass.`),
		mcp.WithString("document_id", mcp.Required(), mcp.Description("Document UUID to push.")),
		mcp.WithString("branch", mcp.Description("Feature branch name (default: auto-generated from doc title and id).")),
		mcp.WithString("target_branch", mcp.Description("Base branch for the PR (default: the repo's default branch).")),
		mcp.WithString("commit_message", mcp.Description("Commit message (default: built from revision_meta).")),
		mcp.WithString("pr_title", mcp.Description("PR title (default: matches commit message).")),
		mcp.WithString("pr_body", mcp.Description("PR description in Markdown (default: links back to the markupmarkdown doc and summarizes the revision).")),
	)
}

func (h *handlers) push(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	if res, ok := requireScope(id, models.TokenScopeAdmin); !ok {
		return res, nil
	}
	docID := req.GetString("document_id", "")
	if docID == "" {
		return errorResult("`document_id` is required")
	}
	opts := PushbackOpts{
		Branch:        req.GetString("branch", ""),
		CommitMessage: req.GetString("commit_message", ""),
		PRTitle:       req.GetString("pr_title", ""),
		PRBody:        req.GetString("pr_body", ""),
		TargetBranch:  req.GetString("target_branch", ""),
	}
	out, err := h.api.PushToGitHub(ctx, id.User.ID, docID, id.User.AccessToken, opts)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	h.api.LogTokenAction(ctx, id.TokenID, "pushback.pr", docID)
	return jsonResult(out)
}

// --- list_revisions ---

func listRevisionsTool() mcp.Tool {
	return mcp.NewTool("list_revisions",
		mcp.WithDescription(`Return the full revision chain for a document — ancestors + descendants — root-first, with each node's revisionIndex, model (manual/claude-opus-X/…), generatedBy, actorKind (human/agent), and timestamps. Faster than walking parent / children one get_document call at a time.`),
		mcp.WithString("document_id", mcp.Required(), mcp.Description("Any document UUID in the chain (root, leaf, or anywhere in between).")),
	)
}

func (h *handlers) listRevisions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	docID := req.GetString("document_id", "")
	if docID == "" {
		return errorResult("`document_id` is required")
	}
	chain, err := h.api.ListRevisions(ctx, id.User.ID, docID, id.User.AccessToken)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	return jsonResult(chain)
}

// --- delete_comment (mine-only) ---

func deleteCommentTool() mcp.Tool {
	return mcp.NewTool("delete_comment",
		mcp.WithDescription(`Delete a comment thread you authored (or that an agent token you own authored). Cannot delete other users' threads — that mirrors the REST require-mine guard.`),
		mcp.WithString("comment_id", mcp.Required(), mcp.Description("Comment UUID to delete.")),
	)
}

func (h *handlers) deleteComment(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	if res, ok := requireScope(id, models.TokenScopeWrite); !ok {
		return res, nil
	}
	cid := req.GetString("comment_id", "")
	if cid == "" {
		return errorResult("`comment_id` is required")
	}
	if err := h.api.DeleteComment(ctx, id.User.ID, cid); err != nil {
		return errorResult("%s", err.Error())
	}
	h.api.LogTokenAction(ctx, id.TokenID, "comment.delete", "")
	return jsonResult(map[string]string{"deleted": cid})
}
