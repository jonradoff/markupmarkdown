package api

import (
	"strings"
	"testing"

	"markupmarkdown/internal/models"
)

func TestGenerateToken_Format(t *testing.T) {
	plain, hash, prefix := generateToken()
	if !strings.HasPrefix(plain, "mmk_") {
		t.Fatalf("plain should start with mmk_: %q", plain)
	}
	// "mmk_" (4) + 32 random bytes hex (64) = 68 chars
	if len(plain) != 68 {
		t.Fatalf("unexpected plaintext length %d", len(plain))
	}
	if hash != HashToken(plain) {
		t.Fatalf("returned hash != HashToken(plain)")
	}
	// Prefix = first 12 chars + "…"
	if !strings.HasSuffix(prefix, "…") {
		t.Fatalf("prefix missing ellipsis: %q", prefix)
	}
	if !strings.HasPrefix(prefix, plain[:12]) {
		t.Fatalf("prefix should start with first 12 plaintext chars: %q vs %q", prefix, plain[:12])
	}
}

func TestGenerateToken_NonceUnique(t *testing.T) {
	a, _, _ := generateToken()
	b, _, _ := generateToken()
	if a == b {
		t.Fatal("token plaintext should be unique across calls")
	}
}

func TestParseScope_Defaults(t *testing.T) {
	got, ok := parseScope("")
	if !ok || got != tokenScopeDefault {
		t.Fatalf("empty should default to %q; got (%q, %v)", tokenScopeDefault, got, ok)
	}
}

func TestParseScope_AllValid(t *testing.T) {
	for _, s := range []models.TokenScope{
		models.TokenScopeRead,
		models.TokenScopeWrite,
		models.TokenScopeAdmin,
	} {
		got, ok := parseScope(string(s))
		if !ok || got != s {
			t.Errorf("parseScope(%q) = (%q, %v)", s, got, ok)
		}
	}
}

func TestParseScope_Bogus(t *testing.T) {
	if _, ok := parseScope("god"); ok {
		t.Fatal("bogus scope should not parse")
	}
}
