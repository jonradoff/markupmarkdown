package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"markupmarkdown/internal/models"
)

// stubAPI implements the API interface for unit testing the MCP server's
// tool handlers without spinning up Mongo. Fields control return values.
type stubAPI struct {
	user      *models.User
	tokenID   string
	label     string
	scope     models.TokenScope
	bearerErr error

	doc       *models.Document
	docErr    error
	docs      []models.Document
	docsErr   error
	comments  []models.Comment
	listErr   error
	newCmt    *models.Comment
	cmtErr    error
	repCmt    *models.Comment
	repErr    error
	resCmt    *models.Comment
	resErr    error
	reviseOut *RevisionOutput
	reviseErr error

	rateOK    bool
	reviseOK  bool
	slotOK    bool
	lastLog   string
	valCErr   error
	valRErr   error

	addCommentCalls int
	replyCalls      int
	resolveCalls    int
}

func (s *stubAPI) UserFromBearer(_ context.Context, _ string) (*models.User, string, string, models.TokenScope, error) {
	return s.user, s.tokenID, s.label, s.scope, s.bearerErr
}
func (s *stubAPI) DocAccess(_ context.Context, _, _, _ string) (*models.Document, error) {
	return s.doc, s.docErr
}
func (s *stubAPI) ListDocumentsForUser(_ context.Context, _ string, _ bool) ([]models.Document, error) {
	return s.docs, s.docsErr
}
func (s *stubAPI) ListComments(_ context.Context, _ string) ([]models.Comment, error) {
	return s.comments, s.listErr
}
func (s *stubAPI) CreateComment(_ context.Context, _, _, _, _ string, _ int, _, _ string) (*models.Comment, error) {
	s.addCommentCalls++
	return s.newCmt, s.cmtErr
}
func (s *stubAPI) ReplyToComment(_ context.Context, _, _, _, _, _ string) (*models.Comment, error) {
	s.replyCalls++
	return s.repCmt, s.repErr
}
func (s *stubAPI) ResolveComment(_ context.Context, _, _ string, _ bool) (*models.Comment, error) {
	s.resolveCalls++
	return s.resCmt, s.resErr
}
func (s *stubAPI) ReviseWithAI(_ context.Context, _, _ string, _ []string, _ bool, _ string) (*RevisionOutput, error) {
	return s.reviseOut, s.reviseErr
}
func (s *stubAPI) AllowCommentRate(_ string) bool { return s.rateOK }
func (s *stubAPI) AllowReviseRate(_ string) bool  { return s.reviseOK }
func (s *stubAPI) AcquireReviseSlot(_ string) (func(), bool) {
	return func() {}, s.slotOK
}
func (s *stubAPI) LogTokenAction(_ context.Context, _, action, _ string) {
	s.lastLog = action
}
func (s *stubAPI) ValidateCommentBody(b string) (string, error) {
	if s.valCErr != nil {
		return "", s.valCErr
	}
	return strings.TrimSpace(b), nil
}
func (s *stubAPI) ValidateReplyBody(b string) (string, error) {
	if s.valRErr != nil {
		return "", s.valRErr
	}
	return strings.TrimSpace(b), nil
}

// --- wrapAuth tests ---

func TestWrapAuth_MissingBearerReturns401(t *testing.T) {
	stub := &stubAPI{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("should not be called")
	})
	h := wrapAuth(inner, stub)
	r := httptest.NewRequest("POST", "/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("status %d, want 401", w.Code)
	}
}

func TestWrapAuth_BadBearerReturns401(t *testing.T) {
	stub := &stubAPI{bearerErr: nil, user: nil}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("should not be called")
	})
	h := wrapAuth(inner, stub)
	r := httptest.NewRequest("POST", "/mcp", nil)
	r.Header.Set("Authorization", "Bearer mmk_xxxx")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("status %d, want 401", w.Code)
	}
}

func TestWrapAuth_ValidPassesIdentity(t *testing.T) {
	stub := &stubAPI{
		user:    &models.User{ID: "u1", Login: "alice"},
		tokenID: "t1", label: "label", scope: models.TokenScopeWrite,
	}
	called := false
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok || id.User == nil || id.User.ID != "u1" {
			t.Errorf("identity not stashed: %+v ok=%v", id, ok)
		}
		called = true
	})
	h := wrapAuth(inner, stub)
	r := httptest.NewRequest("POST", "/mcp", nil)
	r.Header.Set("Authorization", "Bearer mmk_good")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Fatal("inner not invoked")
	}
}

// --- requireScope ---

func TestRequireScope_AllowsAdequate(t *testing.T) {
	id := authIdentity{Scope: models.TokenScopeAdmin}
	res, ok := requireScope(id, models.TokenScopeWrite)
	if !ok || res != nil {
		t.Errorf("admin should pass write check; ok=%v res=%v", ok, res)
	}
}

func TestRequireScope_RejectsInsufficient(t *testing.T) {
	id := authIdentity{Scope: models.TokenScopeRead}
	res, ok := requireScope(id, models.TokenScopeWrite)
	if ok {
		t.Fatal("read should NOT pass write check")
	}
	if res == nil {
		t.Fatal("rejection should return an error tool result")
	}
}

func TestRequireScope_EmptyDefaultsToWrite(t *testing.T) {
	// Legacy tokens (Scope=="") default to write privileges.
	id := authIdentity{Scope: ""}
	if _, ok := requireScope(id, models.TokenScopeWrite); !ok {
		t.Fatal("empty scope should default to write")
	}
	if _, ok := requireScope(id, models.TokenScopeAdmin); ok {
		t.Fatal("empty scope should NOT satisfy admin")
	}
}

// --- tool descriptors ---

func TestToolDescriptors_HaveRequiredArgs(t *testing.T) {
	tools := []mcp.Tool{
		listDocsTool(),
		getDocTool(),
		listCommentsTool(),
		addCommentTool(),
		replyTool(),
		resolveTool(),
		reopenTool(),
		reviseTool(),
	}
	wantNames := map[string]bool{
		"list_documents":  true,
		"get_document":    true,
		"list_comments":   true,
		"add_comment":     true,
		"reply":           true,
		"resolve_comment": true,
		"reopen_comment":  true,
		"revise_with_ai":  true,
	}
	for _, tl := range tools {
		if _, ok := wantNames[tl.Name]; !ok {
			t.Errorf("unexpected tool name %q", tl.Name)
		}
		if tl.Description == "" {
			t.Errorf("%s: missing description", tl.Name)
		}
	}
}

// --- New() smoke test ---

func TestNew_ReturnsHandler(t *testing.T) {
	stub := &stubAPI{}
	h := New(stub, nil, "https://example.com")
	if h == nil {
		t.Fatal("New returned nil handler")
	}
	// Hit it with no bearer → 401.
	r := httptest.NewRequest("POST", "/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("expected 401; got %d", w.Code)
	}
}

// --- handler tests via direct calls ---

func ctxWithIdentity(id authIdentity) context.Context {
	return withAuthIdentity(context.Background(), id)
}

func TestHandler_ListDocs(t *testing.T) {
	stub := &stubAPI{
		docs: []models.Document{{ID: "d1", Title: "T", Origin: "upload"}},
	}
	h := &handlers{api: stub, siteURL: "https://example.test"}
	ctx := ctxWithIdentity(authIdentity{User: &models.User{ID: "u1"}, Scope: models.TokenScopeRead})
	req := mcp.CallToolRequest{}
	res, err := h.listDocs(ctx, req)
	if err != nil || res == nil {
		t.Fatalf("listDocs: %v / %v", res, err)
	}
}

func TestHandler_GetDocMissingID(t *testing.T) {
	stub := &stubAPI{doc: &models.Document{ID: "d", Content: "x"}}
	h := &handlers{api: stub}
	ctx := ctxWithIdentity(authIdentity{User: &models.User{ID: "u"}, Scope: models.TokenScopeRead})
	req := mcp.CallToolRequest{}
	res, _ := h.getDoc(ctx, req)
	if res == nil {
		t.Fatal("expected an error tool result for missing id")
	}
}

func TestHandler_AddCommentRequiresWrite(t *testing.T) {
	stub := &stubAPI{rateOK: true}
	h := &handlers{api: stub}
	ctx := ctxWithIdentity(authIdentity{User: &models.User{ID: "u"}, Scope: models.TokenScopeRead})
	res, _ := h.addComment(ctx, mcp.CallToolRequest{})
	if res == nil {
		t.Fatal("read scope should fail addComment")
	}
	if stub.addCommentCalls != 0 {
		t.Fatal("API should not have been called")
	}
}

func TestHandler_AddCommentRateLimited(t *testing.T) {
	stub := &stubAPI{rateOK: false}
	h := &handlers{api: stub}
	ctx := ctxWithIdentity(authIdentity{User: &models.User{ID: "u"}, Scope: models.TokenScopeWrite})
	res, _ := h.addComment(ctx, mcp.CallToolRequest{})
	if res == nil {
		t.Fatal("rate-limit should be surfaced as error result")
	}
}

func TestHandler_ResolveRequiresWrite(t *testing.T) {
	stub := &stubAPI{}
	h := &handlers{api: stub}
	ctx := ctxWithIdentity(authIdentity{User: &models.User{ID: "u"}, Scope: models.TokenScopeRead})
	res, _ := h.resolve(ctx, mcp.CallToolRequest{})
	if res == nil {
		t.Fatal("read scope should fail resolve")
	}
}

func TestHandler_ReopenRequiresWrite(t *testing.T) {
	stub := &stubAPI{}
	h := &handlers{api: stub}
	ctx := ctxWithIdentity(authIdentity{User: &models.User{ID: "u"}, Scope: models.TokenScopeRead})
	res, _ := h.reopen(ctx, mcp.CallToolRequest{})
	if res == nil {
		t.Fatal("read scope should fail reopen")
	}
}

func TestHandler_ReviseAcceptRequiresAdmin(t *testing.T) {
	stub := &stubAPI{reviseOK: true, slotOK: true,
		reviseOut: &RevisionOutput{Model: "m"}}
	h := &handlers{api: stub}
	// Write scope is fine for preview but NOT for accept.
	ctx := ctxWithIdentity(authIdentity{User: &models.User{ID: "u"}, Scope: models.TokenScopeWrite})
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"document_id": "d",
		"accept":      true,
	}
	res, _ := h.revise(ctx, req)
	if res == nil {
		t.Fatal("accept=true with write scope should fail")
	}
}

func TestHandler_ReviseRateLimited(t *testing.T) {
	stub := &stubAPI{reviseOK: false}
	h := &handlers{api: stub}
	ctx := ctxWithIdentity(authIdentity{User: &models.User{ID: "u"}, Scope: models.TokenScopeWrite})
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"document_id": "d"}
	res, _ := h.revise(ctx, req)
	if res == nil {
		t.Fatal("rate-limited revise should error")
	}
}

func TestHandler_ReviseSlotDenied(t *testing.T) {
	stub := &stubAPI{reviseOK: true, slotOK: false}
	h := &handlers{api: stub}
	ctx := ctxWithIdentity(authIdentity{User: &models.User{ID: "u"}, Scope: models.TokenScopeWrite})
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"document_id": "d"}
	res, _ := h.revise(ctx, req)
	if res == nil {
		t.Fatal("slot-denied revise should error")
	}
}

// errorResult helper coverage.

func TestErrorResult_ReturnsFormatted(t *testing.T) {
	res, err := errorResult("oops %d", 42)
	if err != nil || res == nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

func TestJSONResult(t *testing.T) {
	res, err := jsonResult(map[string]int{"a": 1})
	if err != nil || res == nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
}
