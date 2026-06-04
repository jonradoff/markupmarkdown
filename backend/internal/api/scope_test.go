package api

import (
	"net/http/httptest"
	"testing"

	"markupmarkdown/internal/models"
)

func TestEffectiveScope_CookieSessionImpliesAdmin(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	// No tokenInfo on the context — should be admin.
	if got := effectiveScope(r); got != models.TokenScopeAdmin {
		t.Fatalf("got %q, want admin (cookie path)", got)
	}
}

func TestEffectiveScope_TokenReturnsItsScope(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	ctx := contextWithTokenInfo(r.Context(), tokenInfo{
		TokenID: "t1",
		Label:   "L",
		Scope:   models.TokenScopeRead,
	})
	r = r.WithContext(ctx)
	if got := effectiveScope(r); got != models.TokenScopeRead {
		t.Fatalf("got %q, want read", got)
	}
}

func TestEnforceScope_AllowsAdequate(t *testing.T) {
	a := &API{}
	r := httptest.NewRequest("GET", "/", nil)
	ctx := contextWithTokenInfo(r.Context(), tokenInfo{Scope: models.TokenScopeAdmin})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		t.Fatal("admin should satisfy write")
	}
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestEnforceScope_RejectsInsufficient(t *testing.T) {
	a := &API{}
	r := httptest.NewRequest("GET", "/", nil)
	ctx := contextWithTokenInfo(r.Context(), tokenInfo{Scope: models.TokenScopeRead})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	if a.enforceScope(w, r, models.TokenScopeWrite) {
		t.Fatal("read should NOT satisfy write")
	}
	if w.Code != 403 {
		t.Fatalf("status %d, want 403", w.Code)
	}
}
