package api

import (
	"net/http/httptest"
	"testing"

	"markupmarkdown/internal/models"
)

func TestActorKindFor_Cookie(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if got := actorKindFor(r); got != models.ActorHuman {
		t.Fatalf("cookie request should be human; got %q", got)
	}
}

func TestActorKindFor_Token(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	ctx := contextWithTokenInfo(r.Context(), tokenInfo{TokenID: "t1", Scope: models.TokenScopeWrite})
	r = r.WithContext(ctx)
	if got := actorKindFor(r); got != models.ActorAgent {
		t.Fatalf("token request should be agent; got %q", got)
	}
}

func TestStampAgentWrite_OverwritesAuthor(t *testing.T) {
	c := &models.Comment{Author: "Old", AuthorAvatarURL: "x"}
	stampAgentWrite(c, "tok1", "claude-curl")
	if c.Author != "claude-curl" {
		t.Errorf("author not stamped: %q", c.Author)
	}
	if c.TokenID != "tok1" {
		t.Errorf("token id not stamped: %q", c.TokenID)
	}
	if c.AuthorAvatarURL != "" {
		t.Errorf("avatar should be cleared for agents: %q", c.AuthorAvatarURL)
	}
}

func TestStampAgentWriteReply_OverwritesAuthor(t *testing.T) {
	r := &models.Reply{Author: "Old", AuthorAvatarURL: "x"}
	stampAgentWriteReply(r, "tok1", "claude-curl")
	if r.Author != "claude-curl" || r.TokenID != "tok1" || r.AuthorAvatarURL != "" {
		t.Fatalf("unexpected reply state: %+v", r)
	}
}

func TestMarkMine_NoOpWithoutViewer(t *testing.T) {
	comments := []models.Comment{
		{ID: "c1", AuthorID: "u1"},
	}
	markMine(comments, "")
	if comments[0].Mine {
		t.Fatal("Mine should not be set when viewerID is empty")
	}
}

func TestMarkMine_SetsForViewer(t *testing.T) {
	comments := []models.Comment{
		{ID: "c1", AuthorID: "u1", Replies: []models.Reply{{ID: "r1", AuthorID: "u1"}, {ID: "r2", AuthorID: "u2"}}},
		{ID: "c2", AuthorID: "u2", Replies: []models.Reply{{ID: "r3", AuthorID: "u1"}}},
	}
	markMine(comments, "u1")
	if !comments[0].Mine {
		t.Error("c1 should be mine for u1")
	}
	if comments[1].Mine {
		t.Error("c2 should NOT be mine for u1")
	}
	if !comments[0].Replies[0].Mine || comments[0].Replies[1].Mine {
		t.Error("reply mine not stamped correctly on c1")
	}
	if !comments[1].Replies[0].Mine {
		t.Error("r3 (in c2) should be mine for u1")
	}
}

func TestPreferName(t *testing.T) {
	if got := preferName(nil); got != "" {
		t.Errorf("nil user → %q", got)
	}
	if got := preferName(&models.User{Name: "Alice", Login: "alice"}); got != "Alice" {
		t.Errorf("got %q", got)
	}
	if got := preferName(&models.User{Login: "alice"}); got != "alice" {
		t.Errorf("got %q", got)
	}
}

func TestAuthorOr(t *testing.T) {
	if got := authorOr(""); got != anonymous {
		t.Errorf("empty author → %q", got)
	}
	if got := authorOr("  "); got != anonymous {
		t.Errorf("whitespace author → %q", got)
	}
	if got := authorOr(" Alice "); got != "Alice" {
		t.Errorf("got %q", got)
	}
}

func TestMapKeys_PreservesElements(t *testing.T) {
	m := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	got := mapKeys(m)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	seen := map[string]bool{}
	for _, k := range got {
		seen[k] = true
	}
	for k := range m {
		if !seen[k] {
			t.Errorf("missing %q", k)
		}
	}
}
