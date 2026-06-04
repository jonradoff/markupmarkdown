package models

import "testing"

func TestTokenScope_AllowsScope(t *testing.T) {
	cases := []struct {
		have, need TokenScope
		want       bool
	}{
		{TokenScopeRead, TokenScopeRead, true},
		{TokenScopeRead, TokenScopeWrite, false},
		{TokenScopeRead, TokenScopeAdmin, false},

		{TokenScopeWrite, TokenScopeRead, true},
		{TokenScopeWrite, TokenScopeWrite, true},
		{TokenScopeWrite, TokenScopeAdmin, false},

		{TokenScopeAdmin, TokenScopeRead, true},
		{TokenScopeAdmin, TokenScopeWrite, true},
		{TokenScopeAdmin, TokenScopeAdmin, true},
	}
	for _, c := range cases {
		if got := c.have.AllowsScope(c.need); got != c.want {
			t.Errorf("%s.AllowsScope(%s)=%v, want %v", c.have, c.need, got, c.want)
		}
	}
}

func TestTokenScope_UnknownReturnsFalse(t *testing.T) {
	// Unknown scope strings have rank 0 in the map; they should reject
	// every legitimate need (defense in depth — caller should normalize
	// before reaching this).
	var bogus TokenScope = "godmode"
	for _, need := range []TokenScope{TokenScopeRead, TokenScopeWrite, TokenScopeAdmin} {
		if bogus.AllowsScope(need) {
			t.Errorf("unknown scope should not allow %s", need)
		}
	}
}
