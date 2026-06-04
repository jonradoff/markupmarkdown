// Package mcpserver exposes the markupmarkdown API as a Model Context
// Protocol server, mounted at /mcp on the same HTTP listener. Agents
// authenticate with a Personal API token via the Authorization Bearer
// header — every tool routes through the same access checks the REST API
// uses, so private docs stay private and rate limits still apply.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/render"
	"markupmarkdown/internal/store"
)

// API is the subset of the api.API surface we lean on. Defined as an
// interface so we don't pull api into mcpserver's import graph.
type API interface {
	// UserFromBearer resolves the bearer token. Returns scope alongside the
	// user so tool handlers can enforce read/write/admin before the work
	// starts.
	UserFromBearer(ctx context.Context, token string) (user *models.User, tokenID, label string, scope models.TokenScope, err error)
	DocAccess(ctx context.Context, userID, docID string, accessToken string) (*models.Document, error)
	ListDocumentsForUser(ctx context.Context, userID string, includeTrash bool) ([]models.Document, error)
	ListComments(ctx context.Context, docID string) ([]models.Comment, error)
	CreateComment(ctx context.Context, userID, docID, body, quotedText string, occurrence int, tokenID, agentLabel string) (*models.Comment, error)
	ReplyToComment(ctx context.Context, userID, commentID, body, tokenID, agentLabel string) (*models.Comment, error)
	ResolveComment(ctx context.Context, userID, commentID string, reopen bool) (*models.Comment, error)
	// ReviseWithAI requires admin scope when accept=true (creates a new
	// document); write scope is sufficient when accept=false (preview only).
	ReviseWithAI(ctx context.Context, userID, docID string, commentIDs []string, accept bool, tokenID string) (*RevisionOutput, error)

	// AllowCommentRate returns false if the calling user has hit the
	// comment-rate budget. Same bucket the REST handlers use.
	AllowCommentRate(userID string) bool
	// AllowReviseRate / AcquireReviseSlot mirror the REST AI-revision guards.
	AllowReviseRate(userID string) bool
	AcquireReviseSlot(userID string) (release func(), ok bool)
	// LogTokenAction is the same sampled per-(token,action) writer used by
	// REST. Safe to call from tool entry points.
	LogTokenAction(ctx context.Context, tokenID, action, docID string)
	// ValidateCommentBody / ValidateReplyBody share the field-length rules
	// with REST.
	ValidateCommentBody(body string) (string, error)
	ValidateReplyBody(body string) (string, error)
}

type RevisionOutput struct {
	OriginalContent string   `json:"originalContent"`
	RevisedContent  string   `json:"revisedContent"`
	Model           string   `json:"model"`
	TokensIn        int64    `json:"tokensIn"`
	TokensOut       int64    `json:"tokensOut"`
	AppliedIDs      []string `json:"appliedCommentIds"`
	NewDocID        string   `json:"newDocumentId,omitempty"` // set only when accept=true
}

// New constructs the MCP HTTP handler. siteURL is used to render absolute
// links in tool descriptions.
func New(a API, _ *store.Store, siteURL string) http.Handler {
	s := server.NewMCPServer(
		"markupmarkdown",
		"0.1.0",
		server.WithToolCapabilities(false),
	)
	h := &handlers{api: a, siteURL: siteURL}

	s.AddTool(listDocsTool(), h.listDocs)
	s.AddTool(getDocTool(), h.getDoc)
	s.AddTool(listCommentsTool(), h.listComments)
	s.AddTool(addCommentTool(), h.addComment)
	s.AddTool(replyTool(), h.reply)
	s.AddTool(resolveTool(), h.resolve)
	s.AddTool(reopenTool(), h.reopen)
	s.AddTool(reviseTool(), h.revise)

	httpServer := server.NewStreamableHTTPServer(s, server.WithStateLess(true))
	return wrapAuth(httpServer, a)
}

// wrapAuth extracts the Bearer token, resolves the user, and stashes it on
// the request context so handlers can pull it out of the mcp context.
func wrapAuth(next http.Handler, a API) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			http.Error(w, `{"error":"Authorization: Bearer <mmk_…> token required"}`, http.StatusUnauthorized)
			return
		}
		tok := strings.TrimSpace(h[7:])
		user, tokenID, label, scope, err := a.UserFromBearer(r.Context(), tok)
		if err != nil || user == nil {
			http.Error(w, `{"error":"invalid, expired, or revoked token"}`, http.StatusUnauthorized)
			return
		}
		ctx := withAuthIdentity(r.Context(), authIdentity{User: user, Label: label, TokenID: tokenID, Scope: scope})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type handlers struct {
	api     API
	siteURL string
}

// --- identity plumbing ---

type authIdentity struct {
	User    *models.User
	Label   string
	TokenID string
	Scope   models.TokenScope
}

// requireScope returns nil + a friendly error tool result when the caller's
// scope is below need. Use at the top of every write tool.
func requireScope(id authIdentity, need models.TokenScope) (*mcp.CallToolResult, bool) {
	have := id.Scope
	if have == "" {
		have = models.TokenScopeWrite // legacy tokens default to write
	}
	if have.AllowsScope(need) {
		return nil, true
	}
	res, _ := errorResult("this token's scope (%s) cannot perform %s actions", string(have), string(need))
	return res, false
}
type authKey struct{}

func withAuthIdentity(ctx context.Context, id authIdentity) context.Context {
	return context.WithValue(ctx, authKey{}, id)
}
func identityFrom(ctx context.Context) (authIdentity, bool) {
	v, ok := ctx.Value(authKey{}).(authIdentity)
	return v, ok
}

// --- helpers ---

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}
func errorResult(format string, args ...any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(fmt.Sprintf(format, args...)), nil
}

// --- tools ---

func listDocsTool() mcp.Tool {
	return mcp.NewTool("list_documents",
		mcp.WithDescription("List markdown documents the calling identity has touched (created, AI-revised, viewed, or commented on). Excludes trash."),
		mcp.WithBoolean("include_trash", mcp.Description("If true, include soft-deleted docs in the result.")),
	)
}

func (h *handlers) listDocs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	includeTrash := req.GetBool("include_trash", false)
	docs, err := h.api.ListDocumentsForUser(ctx, id.User.ID, includeTrash)
	if err != nil {
		return errorResult("list failed: %v", err)
	}
	type summary struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		URL       string `json:"url"`
		Private   bool   `json:"private,omitempty"`
		SourceURL string `json:"sourceUrl,omitempty"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	out := make([]summary, 0, len(docs))
	for _, d := range docs {
		out = append(out, summary{
			ID: d.ID, Title: d.Title, URL: h.siteURL + "/d/" + d.ID,
			Private: d.Private, SourceURL: d.SourceURL, UpdatedAt: d.UpdatedAt,
		})
	}
	return jsonResult(out)
}

func getDocTool() mcp.Tool {
	return mcp.NewTool("get_document",
		mcp.WithDescription("Fetch a document's full markdown content + metadata. Errors if you don't have access (e.g. private doc whose source repo you can't read)."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Document UUID.")),
	)
}

func (h *handlers) getDoc(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	docID := req.GetString("id", "")
	if docID == "" {
		return errorResult("`id` is required")
	}
	doc, err := h.api.DocAccess(ctx, id.User.ID, docID, id.User.AccessToken)
	if err != nil {
		return errorResult("get failed: %v", err)
	}
	if doc == nil {
		return errorResult("document not found or access denied")
	}
	type out struct {
		ID         string    `json:"id"`
		Title      string    `json:"title"`
		Content    string    `json:"content"`
		URL        string    `json:"url"`
		Private    bool      `json:"private,omitempty"`
		SourceURL  string    `json:"sourceUrl,omitempty"`
		ParentID   string    `json:"parentId,omitempty"`
		CreatedAt  time.Time `json:"createdAt"`
		UpdatedAt  time.Time `json:"updatedAt"`
	}
	return jsonResult(out{
		ID: doc.ID, Title: doc.Title, Content: doc.Content,
		URL: h.siteURL + "/d/" + doc.ID, Private: doc.Private,
		SourceURL: doc.SourceURL, ParentID: doc.ParentID,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	})
}

func listCommentsTool() mcp.Tool {
	return mcp.NewTool("list_comments",
		mcp.WithDescription("List the comment threads on a document. Each result includes the anchor (the exact quoted text in the doc the comment refers to), the body (markdown), reply chain, and resolved state. Optionally pre-renders bodies to sanitized HTML."),
		mcp.WithString("document_id", mcp.Required(), mcp.Description("Document UUID.")),
		mcp.WithString("filter", mcp.Description("'open' (default), 'resolved', or 'all'.")),
		mcp.WithBoolean("render_html", mcp.Description("If true, include sanitized HTML rendering of each body alongside the raw markdown.")),
	)
}

func (h *handlers) listComments(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	docID := req.GetString("document_id", "")
	if docID == "" {
		return errorResult("`document_id` is required")
	}
	if _, err := h.api.DocAccess(ctx, id.User.ID, docID, id.User.AccessToken); err != nil {
		return errorResult("access denied: %v", err)
	}
	filter := req.GetString("filter", "open")
	renderHTML := req.GetBool("render_html", false)
	comments, err := h.api.ListComments(ctx, docID)
	if err != nil {
		return errorResult("list failed: %v", err)
	}
	out := comments[:0]
	for _, c := range comments {
		switch filter {
		case "open":
			if c.Resolved { continue }
		case "resolved":
			if !c.Resolved { continue }
		}
		if renderHTML {
			c.BodyHTML = render.HTMLComment(c.Body)
			for j := range c.Replies {
				c.Replies[j].BodyHTML = render.HTMLComment(c.Replies[j].Body)
			}
		}
		out = append(out, c)
	}
	return jsonResult(out)
}

func addCommentTool() mcp.Tool {
	return mcp.NewTool("add_comment",
		mcp.WithDescription(`Leave a margin comment on a span of the document, anchored by the exact text you want to attach to.

The text you supply must appear verbatim in the doc. If it appears more than once, set 'occurrence' (1-based) to disambiguate.

Use this for review feedback, suggested edits, questions, or to flag spans for a future AI revision.`),
		mcp.WithString("document_id", mcp.Required(), mcp.Description("Document UUID.")),
		mcp.WithString("quoted_text", mcp.Required(), mcp.Description("Verbatim substring from the document to anchor the comment to.")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Your comment, in markdown.")),
		mcp.WithNumber("occurrence", mcp.Description("1-based index of which occurrence of quoted_text to use (default 1).")),
	)
}

func (h *handlers) addComment(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	if res, ok := requireScope(id, models.TokenScopeWrite); !ok {
		return res, nil
	}
	if !h.api.AllowCommentRate(id.User.ID) {
		return errorResult("rate limited: too many comments in a short window — slow down")
	}
	docID := req.GetString("document_id", "")
	quoted := req.GetString("quoted_text", "")
	body := req.GetString("body", "")
	occ := int(req.GetFloat("occurrence", 1))
	if docID == "" || quoted == "" || body == "" {
		return errorResult("`document_id`, `quoted_text`, and `body` are required")
	}
	if len(quoted) > 4*1024 {
		return errorResult("`quoted_text` too long (max 4KB)")
	}
	cleanBody, err := h.api.ValidateCommentBody(body)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	c, err := h.api.CreateComment(ctx, id.User.ID, docID, cleanBody, quoted, occ, id.TokenID, id.Label)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	h.api.LogTokenAction(ctx, id.TokenID, "comment.create", docID)
	return jsonResult(c)
}

func replyTool() mcp.Tool {
	return mcp.NewTool("reply",
		mcp.WithDescription("Reply to an existing comment thread."),
		mcp.WithString("comment_id", mcp.Required(), mcp.Description("Comment UUID to reply to.")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Reply, in markdown.")),
	)
}

func (h *handlers) reply(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	if res, ok := requireScope(id, models.TokenScopeWrite); !ok {
		return res, nil
	}
	if !h.api.AllowCommentRate(id.User.ID) {
		return errorResult("rate limited: too many replies in a short window — slow down")
	}
	cid := req.GetString("comment_id", "")
	body := req.GetString("body", "")
	if cid == "" || body == "" {
		return errorResult("`comment_id` and `body` are required")
	}
	cleanBody, err := h.api.ValidateReplyBody(body)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	c, err := h.api.ReplyToComment(ctx, id.User.ID, cid, cleanBody, id.TokenID, id.Label)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	docID := ""
	if c != nil {
		docID = c.DocumentID
	}
	h.api.LogTokenAction(ctx, id.TokenID, "reply.create", docID)
	return jsonResult(c)
}

func resolveTool() mcp.Tool {
	return mcp.NewTool("resolve_comment",
		mcp.WithDescription("Mark a comment thread as Done. Resolved threads are excluded from the default review queue and become eligible inputs for AI revision."),
		mcp.WithString("comment_id", mcp.Required(), mcp.Description("Comment UUID to resolve.")),
	)
}

func (h *handlers) resolve(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	if res, ok := requireScope(id, models.TokenScopeWrite); !ok {
		return res, nil
	}
	cid := req.GetString("comment_id", "")
	if cid == "" {
		return errorResult("`comment_id` is required")
	}
	c, err := h.api.ResolveComment(ctx, id.User.ID, cid, false)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	docID := ""
	if c != nil {
		docID = c.DocumentID
	}
	h.api.LogTokenAction(ctx, id.TokenID, "comment.resolve", docID)
	return jsonResult(c)
}

func reopenTool() mcp.Tool {
	return mcp.NewTool("reopen_comment",
		mcp.WithDescription("Reopen a previously resolved comment thread."),
		mcp.WithString("comment_id", mcp.Required(), mcp.Description("Comment UUID to reopen.")),
	)
}

func (h *handlers) reopen(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	if res, ok := requireScope(id, models.TokenScopeWrite); !ok {
		return res, nil
	}
	cid := req.GetString("comment_id", "")
	if cid == "" {
		return errorResult("`comment_id` is required")
	}
	c, err := h.api.ResolveComment(ctx, id.User.ID, cid, true)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	docID := ""
	if c != nil {
		docID = c.DocumentID
	}
	h.api.LogTokenAction(ctx, id.TokenID, "comment.reopen", docID)
	return jsonResult(c)
}

func reviseTool() mcp.Tool {
	return mcp.NewTool("revise_with_ai",
		mcp.WithDescription(`Run Claude Opus 4.7 over the document and its resolved comments to produce a revised version. Uses YOUR Anthropic API key (the one stored on your markupmarkdown account, never the agent's).

By default (accept=false) returns the revised content as a preview WITHOUT saving — you can show it to a human for approval. With accept=true, also creates a new child document containing the revision and returns its ID.`),
		mcp.WithString("document_id", mcp.Required(), mcp.Description("Document UUID to revise.")),
		mcp.WithArray("comment_ids", mcp.Description("Optional subset of resolved comment UUIDs to apply. Empty = apply all resolved.")),
		mcp.WithBoolean("accept", mcp.Description("If true, save the revision as a new child document and return its ID. Default false (preview only).")),
	)
}

func (h *handlers) revise(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, _ := identityFrom(ctx)
	docID := req.GetString("document_id", "")
	if docID == "" {
		return errorResult("`document_id` is required")
	}
	commentIDs := req.GetStringSlice("comment_ids", nil)
	accept := req.GetBool("accept", false)

	// Preview = write scope; Accept = admin scope (creates a new doc).
	needScope := models.TokenScopeWrite
	if accept {
		needScope = models.TokenScopeAdmin
	}
	if res, ok := requireScope(id, needScope); !ok {
		return res, nil
	}
	// AI revisions are expensive — burn the user's Anthropic budget and a
	// concurrent slot, same as REST.
	if !h.api.AllowReviseRate(id.User.ID) {
		return errorResult("rate limited: you've reached the AI-revision budget (30/hour) — try again later")
	}
	release, ok := h.api.AcquireReviseSlot(id.User.ID)
	if !ok {
		return errorResult("you already have the maximum (3) AI revisions in flight — wait for one to finish")
	}
	defer release()

	out, err := h.api.ReviseWithAI(ctx, id.User.ID, docID, commentIDs, accept, id.TokenID)
	if err != nil {
		return errorResult("%s", err.Error())
	}
	action := "revision.preview"
	if accept {
		action = "revision.accept"
	}
	h.api.LogTokenAction(ctx, id.TokenID, action, docID)
	return jsonResult(out)
}
