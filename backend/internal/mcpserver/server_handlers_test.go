package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"markupmarkdown/internal/models"
)

// Extra happy-path + edge tests for the MCP tool handlers, exercising
// branches that the original server_test.go didn't reach.

func ctx2(scope models.TokenScope) context.Context {
	return withAuthIdentity(context.Background(), authIdentity{
		User:    &models.User{ID: "u1", Login: "u1", AccessToken: "tok"},
		TokenID: "t1", Label: "L", Scope: scope,
	})
}

func reqWithArgs(args map[string]any) mcp.CallToolRequest {
	r := mcp.CallToolRequest{}
	r.Params.Arguments = args
	return r
}

func TestGetDoc_Happy(t *testing.T) {
	stub := &stubAPI{doc: &models.Document{ID: "d1", Title: "T", Content: "body"}}
	h := &handlers{api: stub, siteURL: "https://x"}
	r := reqWithArgs(map[string]any{"id": "d1"})
	res, _ := h.getDoc(ctx2(models.TokenScopeRead), r)
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestGetDoc_AccessError(t *testing.T) {
	stub := &stubAPI{docErr: errors.New("nope")}
	h := &handlers{api: stub}
	r := reqWithArgs(map[string]any{"id": "d1"})
	res, _ := h.getDoc(ctx2(models.TokenScopeRead), r)
	if res == nil {
		t.Fatal("expected error tool result")
	}
}

func TestListComments_NoDocID(t *testing.T) {
	stub := &stubAPI{}
	h := &handlers{api: stub}
	res, _ := h.listComments(ctx2(models.TokenScopeRead), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing document_id should error")
	}
}

func TestListComments_AccessDenied(t *testing.T) {
	stub := &stubAPI{docErr: errors.New("blocked")}
	h := &handlers{api: stub}
	res, _ := h.listComments(ctx2(models.TokenScopeRead), reqWithArgs(map[string]any{"document_id": "d"}))
	if res == nil {
		t.Fatal("access error should propagate")
	}
}

func TestListComments_FiltersOpenByDefault(t *testing.T) {
	stub := &stubAPI{
		doc: &models.Document{ID: "d"},
		comments: []models.Comment{
			{ID: "c1", Body: "x", Resolved: false},
			{ID: "c2", Body: "y", Resolved: true},
		},
	}
	h := &handlers{api: stub}
	res, _ := h.listComments(ctx2(models.TokenScopeRead), reqWithArgs(map[string]any{"document_id": "d"}))
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestListComments_FilterResolved(t *testing.T) {
	stub := &stubAPI{
		doc: &models.Document{ID: "d"},
		comments: []models.Comment{
			{ID: "c1", Body: "x", Resolved: false},
			{ID: "c2", Body: "y", Resolved: true},
		},
	}
	h := &handlers{api: stub}
	res, _ := h.listComments(ctx2(models.TokenScopeRead),
		reqWithArgs(map[string]any{"document_id": "d", "filter": "resolved"}))
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestListComments_RenderHTML(t *testing.T) {
	stub := &stubAPI{
		doc: &models.Document{ID: "d"},
		comments: []models.Comment{
			{ID: "c1", Body: "**bold**", Replies: []models.Reply{{ID: "r", Body: "_em_"}}},
		},
	}
	h := &handlers{api: stub}
	res, _ := h.listComments(ctx2(models.TokenScopeRead),
		reqWithArgs(map[string]any{"document_id": "d", "filter": "all", "render_html": true}))
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestListComments_StoreError(t *testing.T) {
	stub := &stubAPI{doc: &models.Document{ID: "d"}, listErr: errors.New("blip")}
	h := &handlers{api: stub}
	res, _ := h.listComments(ctx2(models.TokenScopeRead),
		reqWithArgs(map[string]any{"document_id": "d"}))
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestAddComment_MissingArgs(t *testing.T) {
	stub := &stubAPI{rateOK: true}
	h := &handlers{api: stub}
	res, _ := h.addComment(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing args should error")
	}
}

func TestAddComment_QuotedTooLong(t *testing.T) {
	stub := &stubAPI{rateOK: true}
	h := &handlers{api: stub}
	// 5KB quoted_text exceeds 4KB cap.
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'x'
	}
	res, _ := h.addComment(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"document_id": "d", "quoted_text": string(long), "body": "x",
	}))
	if res == nil {
		t.Fatal("too-long quoted should error")
	}
}

func TestAddComment_BodyValidationFails(t *testing.T) {
	stub := &stubAPI{rateOK: true, valCErr: errors.New("body bad")}
	h := &handlers{api: stub}
	res, _ := h.addComment(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"document_id": "d", "quoted_text": "x", "body": "y",
	}))
	if res == nil {
		t.Fatal("validation error should propagate")
	}
}

func TestAddComment_APIError(t *testing.T) {
	stub := &stubAPI{rateOK: true, cmtErr: errors.New("doc not found")}
	h := &handlers{api: stub}
	res, _ := h.addComment(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"document_id": "d", "quoted_text": "x", "body": "y",
	}))
	if res == nil {
		t.Fatal("api error should propagate")
	}
}

func TestAddComment_Happy(t *testing.T) {
	stub := &stubAPI{
		rateOK: true,
		newCmt: &models.Comment{ID: "c1", Body: "hi"},
	}
	h := &handlers{api: stub}
	res, _ := h.addComment(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"document_id": "d", "quoted_text": "x", "body": "y",
	}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "comment.create" {
		t.Errorf("token action not logged: %q", stub.lastLog)
	}
}

func TestReply_Happy(t *testing.T) {
	stub := &stubAPI{
		rateOK: true,
		repCmt: &models.Comment{ID: "c1", DocumentID: "d", Body: "parent"},
	}
	h := &handlers{api: stub}
	res, _ := h.reply(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"comment_id": "c1", "body": "y",
	}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "reply.create" {
		t.Errorf("token action not logged: %q", stub.lastLog)
	}
}

func TestReply_MissingArgs(t *testing.T) {
	stub := &stubAPI{rateOK: true}
	h := &handlers{api: stub}
	res, _ := h.reply(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing args should error")
	}
}

func TestReply_RateLimited(t *testing.T) {
	stub := &stubAPI{rateOK: false}
	h := &handlers{api: stub}
	res, _ := h.reply(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"comment_id": "c", "body": "x",
	}))
	if res == nil {
		t.Fatal("rate-limited should error")
	}
}

func TestReply_ValidationFails(t *testing.T) {
	stub := &stubAPI{rateOK: true, valRErr: errors.New("bad")}
	h := &handlers{api: stub}
	res, _ := h.reply(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"comment_id": "c", "body": "x",
	}))
	if res == nil {
		t.Fatal("validation should error")
	}
}

func TestReply_APIError(t *testing.T) {
	stub := &stubAPI{rateOK: true, repErr: errors.New("not found")}
	h := &handlers{api: stub}
	res, _ := h.reply(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"comment_id": "c", "body": "x",
	}))
	if res == nil {
		t.Fatal("api error should propagate")
	}
}

func TestResolve_MissingArgs(t *testing.T) {
	stub := &stubAPI{}
	h := &handlers{api: stub}
	res, _ := h.resolve(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing comment_id should error")
	}
}

func TestResolve_Happy(t *testing.T) {
	stub := &stubAPI{resCmt: &models.Comment{ID: "c1", DocumentID: "d", Resolved: true}}
	h := &handlers{api: stub}
	res, _ := h.resolve(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{"comment_id": "c1"}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "comment.resolve" {
		t.Errorf("token action not logged: %q", stub.lastLog)
	}
}

func TestResolve_APIError(t *testing.T) {
	stub := &stubAPI{resErr: errors.New("blocked")}
	h := &handlers{api: stub}
	res, _ := h.resolve(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{"comment_id": "c1"}))
	if res == nil {
		t.Fatal("api error should propagate")
	}
}

func TestReopen_Happy(t *testing.T) {
	stub := &stubAPI{resCmt: &models.Comment{ID: "c1", DocumentID: "d"}}
	h := &handlers{api: stub}
	res, _ := h.reopen(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{"comment_id": "c1"}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "comment.reopen" {
		t.Errorf("token action not logged: %q", stub.lastLog)
	}
}

func TestReopen_MissingArgs(t *testing.T) {
	stub := &stubAPI{}
	h := &handlers{api: stub}
	res, _ := h.reopen(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing comment_id should error")
	}
}

func TestRevise_MissingDocID(t *testing.T) {
	stub := &stubAPI{reviseOK: true, slotOK: true}
	h := &handlers{api: stub}
	res, _ := h.revise(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing document_id should error")
	}
}

func TestRevise_PreviewHappy(t *testing.T) {
	stub := &stubAPI{
		reviseOK: true, slotOK: true,
		reviseOut: &RevisionOutput{Model: "m"},
	}
	h := &handlers{api: stub}
	res, _ := h.revise(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"document_id": "d",
		"accept":      false,
	}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "revision.preview" {
		t.Errorf("token action not logged: %q", stub.lastLog)
	}
}

func TestRevise_AcceptHappy(t *testing.T) {
	stub := &stubAPI{
		reviseOK: true, slotOK: true,
		reviseOut: &RevisionOutput{Model: "m", NewDocID: "new"},
	}
	h := &handlers{api: stub}
	res, _ := h.revise(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{
		"document_id": "d",
		"accept":      true,
	}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "revision.accept" {
		t.Errorf("token action not logged: %q", stub.lastLog)
	}
}

func TestRevise_APIError(t *testing.T) {
	stub := &stubAPI{
		reviseOK: true, slotOK: true,
		reviseErr: errors.New("ai err"),
	}
	h := &handlers{api: stub}
	res, _ := h.revise(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"document_id": "d", "accept": false,
	}))
	if res == nil {
		t.Fatal("api error should propagate")
	}
}

func TestListDocs_IncludeTrash(t *testing.T) {
	stub := &stubAPI{docs: []models.Document{
		{ID: "d1", Title: "T1"},
		{ID: "d2", Title: "T2"},
	}}
	h := &handlers{api: stub, siteURL: "https://x"}
	res, _ := h.listDocs(ctx2(models.TokenScopeRead),
		reqWithArgs(map[string]any{"include_trash": true}))
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestListDocs_ListError(t *testing.T) {
	stub := &stubAPI{docsErr: errors.New("db down")}
	h := &handlers{api: stub}
	res, _ := h.listDocs(ctx2(models.TokenScopeRead), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("api error should propagate")
	}
}
