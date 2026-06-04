package api_test

import (
	"strings"
	"testing"

	"markupmarkdown/internal/ai"
	"markupmarkdown/internal/api"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestPreview_RequiresSignIn(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/revise", nil)
	if status != 401 {
		t.Fatalf("status=%d", status)
	}
}

func TestPreview_RequiresAnthropicKey(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/revise",
		map[string]any{}, withCookie(sess))
	if status != 428 {
		t.Fatalf("status=%d body=%s, want 428 (Precondition Required)", status, body)
	}
	if !strings.Contains(string(body), "anthropic_key_missing") {
		t.Errorf("kind missing: %s", body)
	}
}

func TestAccept_RequiresSignIn(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/revisions",
		map[string]string{"content": "x"})
	if status != 401 {
		t.Fatalf("status=%d", status)
	}
}

func TestAccept_RequiresAdminScopeForToken(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	wPlain, _ := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/revisions",
		map[string]string{"content": "x"}, withBearer(wPlain))
	if status != 403 {
		t.Fatalf("status=%d, want 403", status)
	}
}

func TestAccept_RejectsEmptyContent(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/revisions",
		map[string]string{"content": "   "}, withCookie(sess))
	if status != 400 {
		t.Fatalf("status=%d", status)
	}
}

func TestAccept_Succeeds(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	parent := testutil.NewTestDocument(t, st, user.ID, "Hi")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+parent.ID+"/revisions",
		map[string]any{
			"content":           "# Revised\n\nHi.\n",
			"model":             "claude-opus-4-7",
			"tokensIn":          int64(100),
			"tokensOut":         int64(50),
			"appliedCommentIds": []string{"c1", "c2"},
		}, withCookie(sess))
	if status != 201 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	var doc models.Document
	mustDecode(t, body, &doc)
	if doc.ParentID != parent.ID {
		t.Errorf("ParentID = %q", doc.ParentID)
	}
	if doc.RevisionMeta == nil || doc.RevisionMeta.Model != "claude-opus-4-7" {
		t.Errorf("revision meta wrong: %+v", doc.RevisionMeta)
	}
}

func TestAccept_DefaultModelWhenEmpty(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	parent := testutil.NewTestDocument(t, st, user.ID, "Hi")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+parent.ID+"/revisions",
		map[string]any{
			"content": "# Revised\n",
		}, withCookie(sess))
	if status != 201 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	var doc models.Document
	mustDecode(t, body, &doc)
	if doc.RevisionMeta == nil || doc.RevisionMeta.Model != ai.Model {
		t.Errorf("expected default model %q, got %+v", ai.Model, doc.RevisionMeta)
	}
}

// silence vet
var _ api.API
