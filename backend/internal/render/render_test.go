package render

import (
	"strings"
	"testing"
)

func TestHTMLComment_BasicMarkdown(t *testing.T) {
	html := HTMLComment("**hi** _there_")
	if !strings.Contains(html, "<strong>hi</strong>") {
		t.Errorf("missing <strong>: %q", html)
	}
	if !strings.Contains(html, "<em>there</em>") {
		t.Errorf("missing <em>: %q", html)
	}
}

func TestHTMLComment_Empty(t *testing.T) {
	if got := HTMLComment(""); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestHTMLComment_StripsScripts(t *testing.T) {
	// bluemonday must scrub <script>.
	html := HTMLComment("hello <script>alert(1)</script> world")
	if strings.Contains(html, "<script") {
		t.Fatalf("scripts not sanitized: %q", html)
	}
	if !strings.Contains(html, "hello") || !strings.Contains(html, "world") {
		t.Fatalf("dropped legitimate content: %q", html)
	}
}

func TestHTMLComment_ExternalLinksGetRelAndTarget(t *testing.T) {
	html := HTMLComment("see [docs](https://example.com)")
	// bluemonday emits "nofollow noopener" (it dedupes "noopener noreferrer").
	if !strings.Contains(html, `rel="nofollow`) {
		t.Errorf("missing nofollow rel: %q", html)
	}
	if !strings.Contains(html, "noopener") {
		t.Errorf("missing noopener rel: %q", html)
	}
	if !strings.Contains(html, `target="_blank"`) {
		t.Errorf("missing _blank target: %q", html)
	}
}

func TestHTMLComment_FencedCode(t *testing.T) {
	html := HTMLComment("```\nfoo()\n```")
	if !strings.Contains(html, "<pre") || !strings.Contains(html, "foo()") {
		t.Errorf("fenced code not rendered: %q", html)
	}
}

func TestPlainText_HeadingsAndParagraphs(t *testing.T) {
	src := "# Title\n\nFirst paragraph.\n\nSecond paragraph.\n"
	got := PlainText(src)
	if !strings.Contains(got, "Title") {
		t.Errorf("missing heading text: %q", got)
	}
	if !strings.Contains(got, "First paragraph") {
		t.Errorf("missing first paragraph: %q", got)
	}
	if !strings.Contains(got, "Second paragraph") {
		t.Errorf("missing second paragraph: %q", got)
	}
}

func TestPlainText_PreservesOrder(t *testing.T) {
	src := "# A\n\nB.\n\n## C\n"
	got := PlainText(src)
	ai := strings.Index(got, "A")
	bi := strings.Index(got, "B")
	ci := strings.Index(got, "C")
	if ai >= bi || bi >= ci {
		t.Fatalf("unexpected order: %q", got)
	}
}

func TestPlainText_CodeBlock(t *testing.T) {
	src := "Here:\n\n```\nfoo()\nbar()\n```\n"
	got := PlainText(src)
	if !strings.Contains(got, "foo()") || !strings.Contains(got, "bar()") {
		t.Fatalf("code block content missing: %q", got)
	}
}

func TestPlainText_LinkAutolinkText(t *testing.T) {
	src := "see https://example.com for info"
	got := PlainText(src)
	if !strings.Contains(got, "example.com") {
		t.Errorf("autolink text missing: %q", got)
	}
}

func TestFindOccurrence_First(t *testing.T) {
	start, end := FindOccurrence("hello world hello world", "hello", 1)
	if start != 0 || end != 5 {
		t.Fatalf("got (%d,%d), want (0,5)", start, end)
	}
}

func TestFindOccurrence_Second(t *testing.T) {
	start, end := FindOccurrence("hello world hello world", "hello", 2)
	if start != 12 || end != 17 {
		t.Fatalf("got (%d,%d), want (12,17)", start, end)
	}
}

func TestFindOccurrence_OutOfRange(t *testing.T) {
	if start, end := FindOccurrence("hello", "hello", 2); start != -1 || end != -1 {
		t.Fatalf("got (%d,%d), want (-1,-1)", start, end)
	}
}

func TestFindOccurrence_NotFound(t *testing.T) {
	if start, end := FindOccurrence("abc", "z", 1); start != -1 || end != -1 {
		t.Fatalf("got (%d,%d), want (-1,-1)", start, end)
	}
}

func TestFindOccurrence_EmptyNeedle(t *testing.T) {
	if start, end := FindOccurrence("abc", "", 1); start != -1 || end != -1 {
		t.Fatalf("got (%d,%d), want (-1,-1)", start, end)
	}
}

func TestFindOccurrence_ZeroN(t *testing.T) {
	if start, end := FindOccurrence("abc", "a", 0); start != -1 || end != -1 {
		t.Fatalf("got (%d,%d), want (-1,-1) for n=0", start, end)
	}
}

func TestCountOccurrences(t *testing.T) {
	cases := []struct {
		hay, needle string
		want        int
	}{
		{"", "x", 0},
		{"abc", "", 0},
		{"hello world hello", "hello", 2},
		{"aaaa", "aa", 2},
	}
	for _, c := range cases {
		if got := CountOccurrences(c.hay, c.needle); got != c.want {
			t.Errorf("CountOccurrences(%q,%q)=%d, want %d", c.hay, c.needle, got, c.want)
		}
	}
}
