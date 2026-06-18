package api

import (
	"testing"

	"markupmarkdown/internal/models"
)

// parseGitHubBlobURL + deriveGitHubInfo are the two pure helpers that
// decide where a github-sourced doc came from. They're called every
// time a doc loads (access gate + push-back UI), so each branch needs
// coverage.

func TestParseGitHubBlobURL_HappyPath(t *testing.T) {
	owner, repo, ref, path, ok := parseGitHubBlobURL("https://github.com/anthropics/claude-code/blob/main/README.md")
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if owner != "anthropics" || repo != "claude-code" || ref != "main" || path != "README.md" {
		t.Errorf("parts wrong: owner=%q repo=%q ref=%q path=%q", owner, repo, ref, path)
	}
}

func TestParseGitHubBlobURL_NestedPath(t *testing.T) {
	owner, repo, ref, path, ok := parseGitHubBlobURL("https://github.com/owner/repo/blob/feature/branch/docs/sub/dir/file.md")
	if !ok {
		t.Fatal("ok=false, want true for nested path")
	}
	// Note: ref is parts[3]; "feature" is captured as ref and the
	// rest folds into path. That's intentional — this parser is
	// permissive about ref-vs-path boundaries for simple branches.
	if owner != "owner" || repo != "repo" || ref != "feature" {
		t.Errorf("nested parts: owner=%q repo=%q ref=%q", owner, repo, ref)
	}
	if path != "branch/docs/sub/dir/file.md" {
		t.Errorf("nested path=%q wrong (parser folds slashes after ref into path)", path)
	}
}

func TestParseGitHubBlobURL_RejectsNonGitHubHost(t *testing.T) {
	if _, _, _, _, ok := parseGitHubBlobURL("https://gitlab.com/owner/repo/blob/main/README.md"); ok {
		t.Error("ok=true for non-github host, want false")
	}
}

func TestParseGitHubBlobURL_RejectsTooShortPath(t *testing.T) {
	if _, _, _, _, ok := parseGitHubBlobURL("https://github.com/owner/repo"); ok {
		t.Error("ok=true for path without /blob/, want false")
	}
}

func TestParseGitHubBlobURL_RejectsTreeURL(t *testing.T) {
	// /tree/ URLs are dir listings, not blobs — should reject.
	if _, _, _, _, ok := parseGitHubBlobURL("https://github.com/owner/repo/tree/main/dir"); ok {
		t.Error("ok=true for /tree/ URL, want false (parser checks parts[2]==blob)")
	}
}

func TestParseGitHubBlobURL_RejectsMalformed(t *testing.T) {
	// url.Parse accepts almost anything, but the host check still
	// catches schemeless / relative URLs.
	if _, _, _, _, ok := parseGitHubBlobURL("not a url"); ok {
		t.Error("ok=true for garbage input")
	}
	if _, _, _, _, ok := parseGitHubBlobURL(""); ok {
		t.Error("ok=true for empty input")
	}
}

func TestDeriveGitHubInfo_PrefersStampedFields(t *testing.T) {
	doc := &models.Document{
		Origin:      "url",
		SourceURL:   "https://github.com/wrong/wrong/blob/wrong/wrong.md",
		GitHubOwner: "anthropics",
		GitHubRepo:  "claude-code",
		GitHubRef:   "main",
		GitHubPath:  "README.md",
	}
	owner, repo, ref, path, ok := deriveGitHubInfo(doc)
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if owner != "anthropics" || repo != "claude-code" || ref != "main" || path != "README.md" {
		t.Errorf("stamped fields should win, got owner=%q repo=%q ref=%q path=%q", owner, repo, ref, path)
	}
}

func TestDeriveGitHubInfo_FallsBackToSourceURL(t *testing.T) {
	// Legacy doc: no stamped fields, parse from SourceURL.
	doc := &models.Document{
		Origin:    "url",
		SourceURL: "https://github.com/owner/repo/blob/main/file.md",
	}
	owner, repo, ref, path, ok := deriveGitHubInfo(doc)
	if !ok {
		t.Fatal("ok=false, want true (legacy fallback)")
	}
	if owner != "owner" || repo != "repo" || ref != "main" || path != "file.md" {
		t.Errorf("fallback parse wrong: owner=%q repo=%q ref=%q path=%q", owner, repo, ref, path)
	}
}

func TestDeriveGitHubInfo_NonURLOriginReturnsFalse(t *testing.T) {
	// Uploads + manual-create docs aren't github-sourced even if
	// SourceURL happens to be set on them.
	doc := &models.Document{Origin: "upload", SourceURL: "https://github.com/x/y/blob/main/z.md"}
	if _, _, _, _, ok := deriveGitHubInfo(doc); ok {
		t.Error("ok=true for upload-origin doc, want false")
	}
}

func TestDeriveGitHubInfo_NonGitHubURLReturnsFalse(t *testing.T) {
	doc := &models.Document{Origin: "url", SourceURL: "https://example.com/foo.md"}
	if _, _, _, _, ok := deriveGitHubInfo(doc); ok {
		t.Error("ok=true for non-github URL, want false")
	}
}
