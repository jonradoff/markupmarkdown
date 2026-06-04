package api_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestDocumentsIntegration_ListRequiresSignIn(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "GET", "/api/documents", nil)
	if status != 401 {
		t.Fatalf("anonymous list status=%d, want 401", status)
	}
}

func TestDocumentsIntegration_CreateFromContent(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, body := doJSON(t, srv, "POST", "/api/documents", map[string]string{
		"content": "# Hello\n\nWorld\n",
		"title":   "MyDoc",
	}, withCookie(sess))
	if status != 201 {
		t.Fatalf("create: status=%d body=%s", status, body)
	}
	var doc models.Document
	mustDecode(t, body, &doc)
	if doc.Title != "MyDoc" || doc.Origin != "upload" {
		t.Fatalf("got %+v", doc)
	}
	// CreatedByID has json:"-" so it's not in the response. Read from the
	// store directly to verify the field was stamped.
	stored, err := st.GetDocument(context.Background(), doc.ID)
	if err != nil || stored == nil {
		t.Fatalf("re-fetch: %v / %v", err, stored)
	}
	if stored.CreatedByID != user.ID {
		t.Errorf("CreatedByID = %q, want %q", stored.CreatedByID, user.ID)
	}
}

func TestDocumentsIntegration_CreateRejectsEmptyBody(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "POST", "/api/documents", map[string]string{}, withCookie(sess))
	if status != 400 {
		t.Fatalf("status=%d, want 400 (need url or content)", status)
	}
}

func TestDocumentsIntegration_CreateRejectsBadURL(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "POST", "/api/documents", map[string]string{
		"url": "javascript:alert(1)",
	}, withCookie(sess))
	if status != 400 {
		t.Fatalf("status=%d, want 400", status)
	}
}

func TestDocumentsIntegration_TitleTooLong(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	status, _ := doJSON(t, srv, "POST", "/api/documents", map[string]string{
		"content": "x",
		"title":   strings.Repeat("a", 300),
	}, withCookie(sess))
	if status != 400 {
		t.Fatalf("status=%d, want 400 (title too long)", status)
	}
}

func TestDocumentsIntegration_GetReturnsDoc(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	status, body := doJSON(t, srv, "GET", "/api/documents/"+doc.ID, nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("get: status=%d body=%s", status, body)
	}
	var got models.Document
	mustDecode(t, body, &got)
	if got.ID != doc.ID {
		t.Fatalf("got %q", got.ID)
	}
}

func TestDocumentsIntegration_GetMissingReturns404(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "GET", "/api/documents/nope-no-such-id", nil)
	if status != 404 {
		t.Fatalf("status=%d, want 404", status)
	}
}

func TestDocumentsIntegration_PatchRenameRequiresAdminScopeOrCookie(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	// Cookie session can rename.
	status, _ := doJSON(t, srv, "PATCH", "/api/documents/"+doc.ID,
		map[string]string{"title": "Renamed"}, withCookie(sess))
	if status != 200 {
		t.Fatalf("cookie rename: status=%d", status)
	}

	// Write-scope token CANNOT rename.
	plain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)
	status, _ = doJSON(t, srv, "PATCH", "/api/documents/"+doc.ID,
		map[string]string{"title": "BadRename"}, withBearer(plain))
	if status != 403 {
		t.Fatalf("write-token rename should be 403; got %d", status)
	}

	// Admin-scope token CAN rename.
	adminPlain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeAdmin)
	status, _ = doJSON(t, srv, "PATCH", "/api/documents/"+doc.ID,
		map[string]string{"title": "AdminRename"}, withBearer(adminPlain))
	if status != 200 {
		t.Fatalf("admin-token rename: status=%d", status)
	}
}

func TestDocumentsIntegration_PatchRejectsEmptyTitle(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	status, _ := doJSON(t, srv, "PATCH", "/api/documents/"+doc.ID,
		map[string]string{"title": "   "}, withCookie(sess))
	if status != 400 {
		t.Fatalf("status=%d, want 400", status)
	}
}

func TestDocumentsIntegration_DeleteRequiresAdmin(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	// Write-scope rejected.
	wPlain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)
	status, _ := doJSON(t, srv, "DELETE", "/api/documents/"+doc.ID, nil, withBearer(wPlain))
	if status != 403 {
		t.Fatalf("write delete: status=%d, want 403", status)
	}

	// Admin succeeds → soft-delete.
	aPlain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeAdmin)
	status, _ = doJSON(t, srv, "DELETE", "/api/documents/"+doc.ID, nil, withBearer(aPlain))
	if status != 204 {
		t.Fatalf("admin delete: status=%d", status)
	}
}

func TestDocumentsIntegration_TrashAndRestore(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	// Delete it.
	status, _ := doJSON(t, srv, "DELETE", "/api/documents/"+doc.ID, nil, withCookie(sess))
	if status != 204 {
		t.Fatalf("delete status=%d", status)
	}

	// Trash list shows it.
	status, body := doJSON(t, srv, "GET", "/api/me/trash", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("trash: status=%d body=%s", status, body)
	}
	var trash []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	mustDecode(t, body, &trash)
	if len(trash) != 1 || trash[0].ID != doc.ID {
		t.Fatalf("trash list: %+v", trash)
	}

	// Restore.
	status, _ = doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/restore", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("restore: status=%d", status)
	}

	// Now appears in /api/documents again.
	status, body = doJSON(t, srv, "GET", "/api/documents", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("list: status=%d", status)
	}
	if !strings.Contains(string(body), doc.ID) {
		t.Fatalf("restored doc not in list: %s", body)
	}
}

func TestDocumentsIntegration_RestoreRequiresAdminScopeViaToken(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")
	// Pre-soft-delete it.
	_ = st.SoftDeleteDocument(context.Background(), doc.ID)

	// Read-scope token cannot restore.
	rPlain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeRead)
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/restore", nil, withBearer(rPlain))
	if status != 403 {
		t.Fatalf("read restore: status=%d, want 403", status)
	}

	// Admin token can.
	aPlain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeAdmin)
	status, _ = doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/restore", nil, withBearer(aPlain))
	if status != 200 {
		t.Fatalf("admin restore: status=%d", status)
	}
}

func TestDocumentsIntegration_ListShowsMyDocs(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	for i := 0; i < 3; i++ {
		testutil.NewTestDocument(t, st, user.ID, fmt.Sprintf("doc %d", i))
	}
	status, body := doJSON(t, srv, "GET", "/api/documents", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("list: status=%d", status)
	}
	var docs []map[string]any
	mustDecode(t, body, &docs)
	if len(docs) != 3 {
		t.Fatalf("want 3 docs; got %d", len(docs))
	}
}

func TestDocumentsIntegration_CreateRejectedForReadToken(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	rPlain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeRead)

	status, body := doJSON(t, srv, "POST", "/api/documents", map[string]string{
		"content": "x",
	}, withBearer(rPlain))
	if status != 403 {
		t.Fatalf("status=%d body=%s, want 403", status, body)
	}
}
