package api

import (
	"strings"
	"testing"

	"markupmarkdown/internal/config"
)

func TestTrimURLPunctuation(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://example.com/foo.md", "https://example.com/foo.md"},
		{"https://example.com/foo.md.", "https://example.com/foo.md"},
		{"https://example.com/foo.md.,!?", "https://example.com/foo.md"},
		{"https://example.com/foo)", "https://example.com/foo"},
		{"https://example.com/foo]", "https://example.com/foo"},
		{`https://example.com/foo"`, "https://example.com/foo"},
		{"  https://example.com/foo.  ", "https://example.com/foo"},
		{"", ""},
		{"......", ""},
	}
	for _, c := range cases {
		if got := trimURLPunctuation(c.in); got != c.want {
			t.Errorf("trimURLPunctuation(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestLooksLikeMarkdown_Markdown(t *testing.T) {
	cases := []string{
		"# Title\n\nbody",
		"## Heading\n",
		"- item one\n- item two",
		"```\ncode\n```",
		"[label](https://x)",
		"Just some plain text without any markdown markers.",
	}
	for _, body := range cases {
		if !looksLikeMarkdown(body, "https://example.com/foo.md") {
			t.Errorf("expected markdown for %q", body)
		}
	}
}

func TestLooksLikeMarkdown_RejectsHTML(t *testing.T) {
	cases := []string{
		"<!doctype html><html>...</html>",
		"<html><body>hi</body></html>",
		"<?xml version=\"1.0\"?><svg></svg>",
		"<svg viewBox=\"0 0 1 1\"></svg>",
	}
	for _, body := range cases {
		if looksLikeMarkdown(body, "https://example.com/page") {
			t.Errorf("HTML should be rejected: %q", body)
		}
	}
}

func TestLooksLikeMarkdown_RejectsScriptHeavyHTML(t *testing.T) {
	body := strings.Repeat("x", 100) + "<script>alert(1)</script>" + strings.Repeat("y", 100)
	if looksLikeMarkdown(body, "https://example.com/x") {
		t.Errorf("script-heavy HTML should be rejected")
	}
}

func TestLooksLikeMarkdown_Empty(t *testing.T) {
	if looksLikeMarkdown("   ", "https://example.com/x.md") {
		t.Error("empty should be rejected")
	}
}

func TestLooksLikeMarkdown_AcceptsByExtensionEvenIfMinimal(t *testing.T) {
	// Content has no markdown markers but URL ends in .md → accept.
	body := "raw"
	if !looksLikeMarkdown(body, "https://example.com/path/file.md") {
		t.Error("should accept anything from .md URL")
	}
	if !looksLikeMarkdown(body, "https://example.com/path/file.markdown") {
		t.Error("should accept anything from .markdown URL")
	}
}

func TestSelfDocPath_MatchesFrontendHost(t *testing.T) {
	a := &API{cfg: &config.Config{Frontend: config.FrontendConfig{URL: "https://mumd.metavert.io"}}}
	cases := []struct {
		in, want string
	}{
		{"https://mumd.metavert.io/d/abc-123", "abc-123"},
		{"https://mumd.metavert.io/d/abc-123/", "abc-123"},
		{"https://mumd.metavert.io/d/abc-123?x=1", "abc-123"},
		{"https://markupmarkdown.fly.dev/d/xyz", "xyz"},
		{"https://other.example.com/d/abc", ""},
		{"https://mumd.metavert.io/", ""},
		{"https://mumd.metavert.io/d/", ""},
		{"https://mumd.metavert.io/d/abc/extra", ""},
		{"not a url at all", ""},
	}
	for _, c := range cases {
		if got := a.selfDocPath(c.in); got != c.want {
			t.Errorf("selfDocPath(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}
