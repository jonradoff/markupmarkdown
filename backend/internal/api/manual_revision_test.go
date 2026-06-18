package api_test

// Integration coverage for createManualRevision — the "I want to edit
// this doc" path that doesn't go through AI. Mirrors EditDocument over
// MCP but lives as an HTTP handler so the web UI can call it.

import (
	"context"
	"strings"
	"testing"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestManualRevision_RequiresAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello")

	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/manual-revisions",
		map[string]string{"content": "Hello world"})
	if status != 401 {
		t.Errorf("status=%d want 401", status)
	}
}

func TestManualRevision_CreatesChildAndCarriesComments(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	parent := testutil.NewTestDocument(t, st, user.ID, "v1 body")
	// Leave an unresolved comment on the parent.
	_ = testutil.NewTestComment(t, st, parent.ID, user.ID, "v1", "this is the original")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+parent.ID+"/manual-revisions",
		map[string]string{"content": "v2 body"}, withCookie(sess))
	if status != 201 {
		t.Fatalf("status=%d body=%s", status, body)
	}

	var child models.Document
	mustDecode(t, body, &child)
	if child.ParentID != parent.ID {
		t.Errorf("parentId=%q want %q", child.ParentID, parent.ID)
	}
	if !strings.Contains(child.Content, "v2 body") {
		t.Errorf("content not applied: %q", child.Content)
	}
	if child.RevisionMeta == nil || child.RevisionMeta.Model != "manual" {
		t.Errorf("revision_meta missing/wrong: %+v", child.RevisionMeta)
	}
	// AncestorContent is json:"-" so it isn't on the wire response —
	// re-fetch from the store to verify the drift-detection field
	// was stamped.
	stored, _ := st.GetDocument(context.Background(), child.ID)
	if stored == nil || stored.RevisionMeta == nil || stored.RevisionMeta.AncestorContent == "" {
		t.Errorf("ancestor_content missing on persisted doc (drift detection would break)")
	}

	// Verify carried comment exists on the child.
	carried, _ := st.ListComments(context.Background(), child.ID)
	if len(carried) != 1 {
		t.Errorf("carried comments=%d want 1", len(carried))
	}
}

func TestManualRevision_RejectsBlankContent(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/manual-revisions",
		map[string]string{"content": "   "}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d body=%s want 400", status, body)
	}
}

func TestManualRevision_RejectsNoOpEdit(t *testing.T) {
	// Identical content (modulo TrimSpace) shouldn't create a new
	// revision — it would bloat the chain.
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/manual-revisions",
		map[string]string{"content": "Hello world\n"}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d want 400 for no-op edit", status)
	}
	if !strings.Contains(string(body), "identical") {
		t.Errorf("body should mention identical content: %s", body)
	}
}

func TestManualRevision_DocNotFound(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, _ := doJSON(t, srv, "POST", "/api/documents/no-such-doc/manual-revisions",
		map[string]string{"content": "x"}, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d want 404", status)
	}
}

func TestManualRevision_BadJSONBody(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello")
	// Unknown field triggers DisallowUnknownFields rejection from readJSON.
	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/manual-revisions",
		map[string]any{"content": "v2", "unknown": true}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d want 400 for unknown field", status)
	}
}
