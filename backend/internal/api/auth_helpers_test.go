package api

import (
	"net/http/httptest"
	"testing"
)

func TestAuthTokenFromHeader(t *testing.T) {
	cases := []struct {
		header, want string
	}{
		{"", ""},
		{"Basic abc", ""},
		{"Bearer mmk_xyz", "mmk_xyz"},
		{"Bearer    mmk_padded  ", "mmk_padded"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		got := authTokenFromHeader(r)
		if got != c.want {
			t.Errorf("header=%q got=%q want=%q", c.header, got, c.want)
		}
	}
}

func TestHashToken_IsStable(t *testing.T) {
	a := HashToken("mmk_alpha")
	b := HashToken("mmk_alpha")
	c := HashToken("mmk_beta")
	if a != b {
		t.Fatal("hash should be deterministic")
	}
	if a == c {
		t.Fatal("different inputs should hash differently")
	}
	if len(a) != 64 {
		t.Fatalf("sha256 hex should be 64 chars; got %d", len(a))
	}
}

func TestIsSafeRedirect(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"", false},
		{"/", true},
		{"/d/abc", true},
		{"/d/abc?x=1", true},
		{"//evil.com", false},
		{"http://evil.com", false},
		{"https://evil.com", false},
		{"javascript:alert(1)", false},
	}
	for _, c := range cases {
		if got := isSafeRedirect(c.s); got != c.want {
			t.Errorf("isSafeRedirect(%q)=%v, want %v", c.s, got, c.want)
		}
	}
}

func TestContextDetached_HasTimeout(t *testing.T) {
	ctx := contextDetached()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("contextDetached should carry a deadline")
	}
}
