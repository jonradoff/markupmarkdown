package api_test

// Direct tests for the *API methods in mcpapi_extra.go. These bridge
// MCP tool calls into the rest of the API; they're shared with the
// MCP server through the mcpserver.API interface.
//
// We test them at the API-method layer (not the MCP wire layer) for
// the same reason mcpapi_integration_test.go does: the wire layer is
// already covered by mcpserver/handlers_extra_test.go using a stub
// API, and the layer-cross tests would just duplicate that surface.

import (
	"context"
	"strings"
	"testing"

	"markupmarkdown/internal/mcpserver"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

// --- EditDocument ---

func TestMCPAPI_EditDocument_CreatesChildAndCarriesComments(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	_, rec := testutil.NewAPIToken(t, st, user.ID, models.TokenScopeWrite)
	parent := testutil.NewTestDocument(t, st, user.ID, "Hello world.")
	// Leave an unresolved comment on the parent so we can verify
	// copyOpenCommentsToChild ran.
	_ = testutil.NewTestComment(t, st, parent.ID, user.ID, "Hello", "first")

	child, err := a.EditDocument(context.Background(), user.ID, parent.ID,
		"Hello there, world.", rec.ID, "claude-agent")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if child.ParentID != parent.ID {
		t.Errorf("parentId=%q want %q", child.ParentID, parent.ID)
	}
	if !strings.Contains(child.Content, "Hello there") {
		t.Errorf("content not applied: %q", child.Content)
	}
	if child.RevisionMeta == nil || child.RevisionMeta.Model != "manual" {
		t.Errorf("revision_meta missing or wrong model: %+v", child.RevisionMeta)
	}
	if child.RevisionMeta.ActorKind != models.ActorAgent {
		t.Errorf("actor kind=%q want agent", child.RevisionMeta.ActorKind)
	}

	carried, _ := a.ListComments(context.Background(), child.ID)
	if len(carried) != 1 {
		t.Errorf("carried comments=%d want 1", len(carried))
	}
}

func TestMCPAPI_EditDocument_RejectsBlankContent(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "body")

	_, err := a.EditDocument(context.Background(), user.ID, doc.ID, "   ", "", "")
	if err == nil {
		t.Fatal("expected error for blank content")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("err=%q want 'required'", err)
	}
}

func TestMCPAPI_EditDocument_RejectsNoChange(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")

	// Same body (modulo TrimSpace) → identical content rejected.
	_, err := a.EditDocument(context.Background(), user.ID, doc.ID, "Hello world\n", "", "")
	if err == nil {
		t.Fatal("expected error for no-op edit")
	}
	if !strings.Contains(err.Error(), "identical") {
		t.Errorf("err=%q want mention of identical content", err)
	}
}

func TestMCPAPI_EditDocument_DocNotFound(t *testing.T) {
	_, _, a := newTestServer(t)
	_, err := a.EditDocument(context.Background(), "u", "no-doc", "hi", "", "")
	if err == nil {
		t.Fatal("expected error for missing doc")
	}
}

// --- DeleteComment ---

func TestMCPAPI_DeleteComment_Success(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "Hello", "first")

	if err := a.DeleteComment(context.Background(), user.ID, c.ID); err != nil {
		t.Fatalf("err: %v", err)
	}
	got, _ := st.GetComment(context.Background(), c.ID)
	if got != nil {
		t.Errorf("comment still present after delete")
	}
}

func TestMCPAPI_DeleteComment_NotAuthorRejected(t *testing.T) {
	_, st, a := newTestServer(t)
	author := testutil.NewTestUser(t, st)
	other := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, author.ID, "Hello world")
	c := testutil.NewTestComment(t, st, doc.ID, author.ID, "Hello", "first")

	err := a.DeleteComment(context.Background(), other.ID, c.ID)
	if err == nil {
		t.Fatal("expected error when non-author tries to delete")
	}
	if !strings.Contains(err.Error(), "you can only delete") {
		t.Errorf("err=%q want ownership message", err)
	}
}

func TestMCPAPI_DeleteComment_NotFound(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	// mcpDocAccessForComment will return its own not-found error.
	err := a.DeleteComment(context.Background(), user.ID, "no-such-comment")
	if err == nil {
		t.Fatal("expected error for missing comment")
	}
}

// --- PatchCommentAnchor ---

func TestMCPAPI_PatchCommentAnchor_DocLevel(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "Hello", "first")

	updated, err := a.PatchCommentAnchor(context.Background(), user.ID, c.ID,
		mcpserver.CommentAnchorOpts{DocLevel: true})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if updated.Anchor.Start != 0 || updated.Anchor.End != 0 || updated.Anchor.Exact != "" {
		t.Errorf("anchor not cleared: %+v", updated.Anchor)
	}
}

func TestMCPAPI_PatchCommentAnchor_NotAuthor(t *testing.T) {
	_, st, a := newTestServer(t)
	author := testutil.NewTestUser(t, st)
	other := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, author.ID, "Hello world")
	c := testutil.NewTestComment(t, st, doc.ID, author.ID, "Hello", "first")

	_, err := a.PatchCommentAnchor(context.Background(), other.ID, c.ID,
		mcpserver.CommentAnchorOpts{DocLevel: true})
	if err == nil {
		t.Fatal("expected error when non-author tries to re-anchor")
	}
}

func TestMCPAPI_PatchCommentAnchor_CommentNotFound(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	_, err := a.PatchCommentAnchor(context.Background(), user.ID, "no-comment",
		mcpserver.CommentAnchorOpts{DocLevel: true})
	if err == nil {
		t.Fatal("expected error for missing comment")
	}
}

// --- ListRevisions ---

func TestMCPAPI_ListRevisions_SingleNodeForRoot(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")

	chain, err := a.ListRevisions(context.Background(), user.ID, doc.ID, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if chain.CurrentID != doc.ID {
		t.Errorf("currentId=%q want %q", chain.CurrentID, doc.ID)
	}
	if len(chain.Nodes) != 1 {
		t.Fatalf("nodes=%d want 1 for root-only doc", len(chain.Nodes))
	}
	if chain.Nodes[0].RevisionIndex != 1 {
		t.Errorf("revisionIndex=%d want 1", chain.Nodes[0].RevisionIndex)
	}
}

func TestMCPAPI_ListRevisions_WalksChainNewestChild(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	root := testutil.NewTestDocument(t, st, user.ID, "v1\n")

	// Two manual edits → chain of 3 nodes.
	v2, err := a.EditDocument(context.Background(), user.ID, root.ID, "v2\n", "", "")
	if err != nil {
		t.Fatalf("edit 1: %v", err)
	}
	v3, err := a.EditDocument(context.Background(), user.ID, v2.ID, "v3\n", "", "")
	if err != nil {
		t.Fatalf("edit 2: %v", err)
	}

	chain, err := a.ListRevisions(context.Background(), user.ID, v3.ID, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(chain.Nodes) != 3 {
		t.Fatalf("nodes=%d want 3 (root+v2+v3)", len(chain.Nodes))
	}
	// Nodes are root-first, oldest-to-newest.
	if chain.Nodes[0].ID != root.ID || chain.Nodes[2].ID != v3.ID {
		t.Errorf("walk order wrong: first=%s last=%s want root=%s v3=%s",
			chain.Nodes[0].ID, chain.Nodes[2].ID, root.ID, v3.ID)
	}
	if chain.CurrentID != v3.ID {
		t.Errorf("currentId=%q want v3=%q", chain.CurrentID, v3.ID)
	}
}

func TestMCPAPI_ListRevisions_DocNotFound(t *testing.T) {
	_, _, a := newTestServer(t)
	_, err := a.ListRevisions(context.Background(), "u", "no-doc", "")
	if err == nil {
		t.Fatal("expected error for missing doc")
	}
}
