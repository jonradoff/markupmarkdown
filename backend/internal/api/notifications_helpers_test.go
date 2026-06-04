package api

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractMentions_PlainText(t *testing.T) {
	got := extractMentions("hi @alice and @bob")
	if !contains(got, "alice") || !contains(got, "bob") {
		t.Fatalf("got %v", got)
	}
}

func TestExtractMentions_LowercasesAndDedupes(t *testing.T) {
	got := extractMentions("@Alice @ALICE @alice")
	if len(got) != 1 || got[0] != "alice" {
		t.Fatalf("got %v", got)
	}
}

func TestExtractMentions_IgnoresEmails(t *testing.T) {
	// `user@example.com` should not match — the regex now requires whitespace
	// or specific punctuation before @.
	got := extractMentions("contact: user@example.com")
	if len(got) != 0 {
		t.Fatalf("emails should not match; got %v", got)
	}
}

func TestExtractMentions_AfterPunctuation(t *testing.T) {
	cases := []struct {
		body, want string
	}{
		{"see (@alice) for details", "alice"},
		{"hello, @bob!", "bob"},
		{"> @carol said", "carol"},
		{"\"@dave\"", "dave"},
	}
	for _, c := range cases {
		got := extractMentions(c.body)
		if !contains(got, c.want) {
			t.Errorf("for %q, missing %q in %v", c.body, c.want, got)
		}
	}
}

func TestExtractMentions_StripsTrailingHyphen(t *testing.T) {
	got := extractMentions("hi @user-")
	if len(got) != 1 || got[0] != "user" {
		t.Fatalf("got %v", got)
	}
}

func TestExtractMentions_RespectsCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxMentionsPerBody+5; i++ {
		// distinct logins so they're not deduped
		fmt.Fprintf(&b, "@u%dabc ", i)
	}
	got := extractMentions(b.String())
	if len(got) != maxMentionsPerBody {
		t.Fatalf("got %d, want cap=%d", len(got), maxMentionsPerBody)
	}
}

func TestPreviewSnippet_StripsMarkdownAndTruncates(t *testing.T) {
	got := previewSnippet("**bold** [link](url)")
	if strings.Contains(got, "**") || strings.Contains(got, "[link]") {
		t.Fatalf("raw markdown leaked: %q", got)
	}
	if !strings.Contains(got, "bold") || !strings.Contains(got, "link") {
		t.Fatalf("readable text dropped: %q", got)
	}
}

func TestPreviewSnippet_CollapsesWhitespace(t *testing.T) {
	got := previewSnippet("hello\n\nworld  with   spaces")
	if strings.Contains(got, "\n") {
		t.Fatalf("newlines should be collapsed: %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Fatalf("multi-space should be collapsed: %q", got)
	}
}

func TestPreviewSnippet_TruncatesToMax(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := previewSnippet(long)
	if len([]rune(got)) > 141 {
		t.Fatalf("truncation too lenient: len=%d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("should suffix with ellipsis: %q", got)
	}
}

func contains(list []string, want string) bool {
	for _, x := range list {
		if x == want {
			return true
		}
	}
	return false
}
