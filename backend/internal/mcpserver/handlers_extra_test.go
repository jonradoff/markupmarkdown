package mcpserver

import (
	"testing"

	"markupmarkdown/internal/models"
)

// Unit tests for the MCP "extra" handlers (edit_document, merge_from_github,
// patch_anchor, push_to_github, list_revisions, delete_comment). The stub
// API in server_test.go returns nil for each extra method; that's fine —
// we're verifying the per-handler control flow (scope gate, required-args
// rejection, rate-limit + slot acquisition, LogTokenAction).

// --- edit_document ---

func TestEditDoc_RejectsBelowAdminScope(t *testing.T) {
	h := &handlers{api: &stubAPI{}}
	res, _ := h.editDoc(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"document_id": "d", "content": "x",
	}))
	if res == nil {
		t.Fatal("write-scope token must be rejected by admin gate")
	}
}

func TestEditDoc_RequiresDocIDAndContent(t *testing.T) {
	h := &handlers{api: &stubAPI{}}
	res, _ := h.editDoc(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing both fields should error")
	}
	res, _ = h.editDoc(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{"document_id": "d"}))
	if res == nil {
		t.Fatal("missing content should error")
	}
	res, _ = h.editDoc(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{"content": "x"}))
	if res == nil {
		t.Fatal("missing document_id should error")
	}
}

// --- merge_from_github ---

func TestMerge_RejectsBelowAdminScope(t *testing.T) {
	h := &handlers{api: &stubAPI{}}
	res, _ := h.merge(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{"document_id": "d"}))
	if res == nil {
		t.Fatal("write-scope token must be rejected by admin gate")
	}
}

func TestMerge_RequiresDocID(t *testing.T) {
	h := &handlers{api: &stubAPI{reviseOK: true, slotOK: true}}
	res, _ := h.merge(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing document_id should error")
	}
}

func TestMerge_RateLimited(t *testing.T) {
	// reviseOK doubles as the merge-bucket allow flag in stubAPI.
	h := &handlers{api: &stubAPI{reviseOK: false, slotOK: true}}
	res, _ := h.merge(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{"document_id": "d"}))
	if res == nil {
		t.Fatal("merge rate-limit should produce an error tool result")
	}
}

func TestMerge_SlotExhaustion(t *testing.T) {
	h := &handlers{api: &stubAPI{reviseOK: true, slotOK: false}}
	res, _ := h.merge(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{"document_id": "d"}))
	if res == nil {
		t.Fatal("slot-exhausted should produce an error tool result")
	}
}

func TestMerge_HappyLogsAcceptAction(t *testing.T) {
	stub := &stubAPI{reviseOK: true, slotOK: true}
	h := &handlers{api: stub}
	res, _ := h.merge(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{"document_id": "d"}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "merge.accept" {
		t.Errorf("expected merge.accept logged, got %q", stub.lastLog)
	}
}

// --- patch_anchor ---

func TestPatchAnchor_RejectsBelowWriteScope(t *testing.T) {
	h := &handlers{api: &stubAPI{}}
	res, _ := h.patchAnchor(ctx2(models.TokenScopeRead), reqWithArgs(map[string]any{"comment_id": "c"}))
	if res == nil {
		t.Fatal("read-scope token must be rejected by write gate")
	}
}

func TestPatchAnchor_RequiresCommentID(t *testing.T) {
	h := &handlers{api: &stubAPI{}}
	res, _ := h.patchAnchor(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing comment_id should error")
	}
}

func TestPatchAnchor_HappyLogsAnchorPatched(t *testing.T) {
	stub := &stubAPI{}
	h := &handlers{api: stub}
	res, _ := h.patchAnchor(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{
		"comment_id": "c", "doc_level": true,
	}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "comment.anchor.patched" {
		t.Errorf("expected comment.anchor.patched logged, got %q", stub.lastLog)
	}
}

// --- push_to_github ---

func TestPush_RejectsBelowAdminScope(t *testing.T) {
	h := &handlers{api: &stubAPI{user: &models.User{}}}
	res, _ := h.push(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{"document_id": "d"}))
	if res == nil {
		t.Fatal("write-scope token must be rejected by admin gate")
	}
}

func TestPush_RequiresDocID(t *testing.T) {
	h := &handlers{api: &stubAPI{user: &models.User{}}}
	res, _ := h.push(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing document_id should error")
	}
}

func TestPush_HappyLogsPushbackPR(t *testing.T) {
	stub := &stubAPI{user: &models.User{ID: "u", AccessToken: "tok"}}
	h := &handlers{api: stub}
	res, _ := h.push(ctx2(models.TokenScopeAdmin), reqWithArgs(map[string]any{
		"document_id":  "d",
		"branch":       "feature",
		"target_branch": "main",
	}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "pushback.pr" {
		t.Errorf("expected pushback.pr logged, got %q", stub.lastLog)
	}
}

// --- list_revisions ---

func TestListRevisions_RequiresDocID(t *testing.T) {
	h := &handlers{api: &stubAPI{user: &models.User{}}}
	res, _ := h.listRevisions(ctx2(models.TokenScopeRead), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing document_id should error")
	}
}

func TestListRevisions_HappyReturnsResult(t *testing.T) {
	h := &handlers{api: &stubAPI{user: &models.User{ID: "u", AccessToken: "tok"}}}
	res, _ := h.listRevisions(ctx2(models.TokenScopeRead), reqWithArgs(map[string]any{
		"document_id": "d",
	}))
	if res == nil {
		t.Fatal("nil result")
	}
}

// --- delete_comment ---

func TestDeleteComment_RejectsBelowWriteScope(t *testing.T) {
	h := &handlers{api: &stubAPI{}}
	res, _ := h.deleteComment(ctx2(models.TokenScopeRead), reqWithArgs(map[string]any{"comment_id": "c"}))
	if res == nil {
		t.Fatal("read-scope token must be rejected by write gate")
	}
}

func TestDeleteComment_RequiresCommentID(t *testing.T) {
	h := &handlers{api: &stubAPI{}}
	res, _ := h.deleteComment(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{}))
	if res == nil {
		t.Fatal("missing comment_id should error")
	}
}

func TestDeleteComment_HappyLogsDelete(t *testing.T) {
	stub := &stubAPI{}
	h := &handlers{api: stub}
	res, _ := h.deleteComment(ctx2(models.TokenScopeWrite), reqWithArgs(map[string]any{"comment_id": "c"}))
	if res == nil {
		t.Fatal("nil result")
	}
	if stub.lastLog != "comment.delete" {
		t.Errorf("expected comment.delete logged, got %q", stub.lastLog)
	}
}
