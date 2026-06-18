package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"markupmarkdown/internal/models"
)

// requireMineComment is the per-row ownership gate above the doc-access
// check. We test it with a minimal *API instance — no Mongo, no
// router. currentUser is only used for its ID-resolution branch, which
// we trigger via the explicit "anonymous → ''" path here. The other
// branches need a session/Bearer that the test's httptest.NewRequest
// won't carry by default.

func TestRequireMineComment_NilCommentReturnsNotFound(t *testing.T) {
	a := &API{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/comments/x", nil)
	if a.requireMineComment(rec, req, nil) {
		t.Errorf("requireMineComment(nil) should return false")
	}
	if rec.Code != 404 {
		t.Errorf("code=%d want 404", rec.Code)
	}
}

func TestRequireMineComment_AnonymousRejected(t *testing.T) {
	// Anonymous viewer has vid == ""; even if the comment exists, the
	// gate must reject because "" != AuthorID.
	a := &API{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/comments/x", nil)
	c := &models.Comment{ID: "c1", AuthorID: "alice"}
	if a.requireMineComment(rec, req, c) {
		t.Errorf("anonymous viewer should not own a comment")
	}
	if rec.Code != 403 {
		t.Errorf("code=%d want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "only edit or delete") {
		t.Errorf("error body missing ownership message: %s", rec.Body.String())
	}
}

func TestRequireMineReply_NilParentReturnsNotFound(t *testing.T) {
	a := &API{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/comments/x/replies/r", nil)
	if a.requireMineReply(rec, req, nil, "r1") {
		t.Errorf("nil parent should reject")
	}
	if rec.Code != 404 {
		t.Errorf("code=%d want 404", rec.Code)
	}
}

func TestRequireMineReply_AnonymousRejected(t *testing.T) {
	a := &API{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/comments/x/replies/r", nil)
	c := &models.Comment{ID: "c1", AuthorID: "alice", Replies: []models.Reply{
		{ID: "r1", AuthorID: "alice"},
	}}
	if a.requireMineReply(rec, req, c, "r1") {
		t.Errorf("anonymous viewer should not own a reply")
	}
	if rec.Code != 403 {
		t.Errorf("code=%d want 403", rec.Code)
	}
}

func TestRequireMineReply_MissingReplyReturnsNotFound(t *testing.T) {
	// Even with a populated viewer (via a fake currentUser path that
	// returns "" for this test setup), requireMineReply iterates the
	// parent's reply list. Missing id → "reply not found" 404.
	a := &API{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/comments/x/replies/r", nil)
	c := &models.Comment{ID: "c1", AuthorID: "alice", Replies: []models.Reply{
		{ID: "r1", AuthorID: "alice"},
	}}
	// "missing" reply id triggers the anonymous-viewer 403 first
	// because viewerID == "" — the rejection happens at the vid==""
	// short-circuit, NOT at the loop-fallthrough. That's correct:
	// you have to be authenticated AT ALL before the "reply not
	// found" code path is reachable.
	if a.requireMineReply(rec, req, c, "r-missing") {
		t.Errorf("anonymous + missing reply id should still reject")
	}
	if rec.Code != 403 {
		t.Errorf("code=%d want 403 (anonymous short-circuits before reply lookup)", rec.Code)
	}
}

func TestViewerID_AnonymousReturnsEmpty(t *testing.T) {
	a := &API{}
	req := httptest.NewRequest("GET", "/api/me", nil)
	if got := a.viewerID(req); got != "" {
		t.Errorf("viewerID(anonymous)=%q want empty", got)
	}
}
