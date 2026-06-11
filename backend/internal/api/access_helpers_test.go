package api

// Unit tests for the access.go pure helpers — deriveGitHubInfo and
// IsPublicGitHubBlob's empty-component fail-closed branch.

import (
	"context"
	"testing"

	"markupmarkdown/internal/models"
)

func TestDeriveGitHubInfo(t *testing.T) {
	cases := []struct {
		name string
		doc  *models.Document
		want [4]string
		ok   bool
	}{
		{
			name: "non-url origin",
			doc:  &models.Document{Origin: "upload"},
			ok:   false,
		},
		{
			name: "stamped fields preferred over URL parse",
			doc: &models.Document{
				Origin:      "url",
				GitHubOwner: "anthropics",
				GitHubRepo:  "claude-code",
				GitHubRef:   "main",
				GitHubPath:  "README.md",
				// SourceURL deliberately different — stamped fields win.
				SourceURL: "https://github.com/other/other/blob/main/X.md",
			},
			want: [4]string{"anthropics", "claude-code", "main", "README.md"},
			ok:   true,
		},
		{
			name: "fallback to URL parse",
			doc: &models.Document{
				Origin:    "url",
				SourceURL: "https://github.com/anthropics/x/blob/main/README.md",
			},
			want: [4]string{"anthropics", "x", "main", "README.md"},
			ok:   true,
		},
		{
			name: "non-github URL → not isGitHub",
			doc: &models.Document{
				Origin:    "url",
				SourceURL: "https://example.com/file.md",
			},
			ok: false,
		},
		{
			name: "incomplete stamp falls back to URL",
			doc: &models.Document{
				Origin:      "url",
				GitHubOwner: "owner", // repo missing — falls back to parse
				SourceURL:   "https://github.com/a/b/blob/main/X.md",
			},
			want: [4]string{"a", "b", "main", "X.md"},
			ok:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			owner, repo, ref, path, ok := deriveGitHubInfo(c.doc)
			if ok != c.ok {
				t.Errorf("ok=%v, want %v", ok, c.ok)
			}
			if !c.ok {
				return
			}
			got := [4]string{owner, repo, ref, path}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestPublicGitHubCheck_FailsClosedOnMissingComponents(t *testing.T) {
	a := &API{}
	ctx := context.Background()
	for _, c := range []struct {
		name                       string
		owner, repo, ref, path     string
	}{
		{"empty owner", "", "r", "main", "X.md"},
		{"empty repo", "o", "", "main", "X.md"},
		{"empty ref", "o", "r", "", "X.md"},
		{"empty path", "o", "r", "main", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if a.publicGitHubCheck(ctx, c.owner, c.repo, c.ref, c.path) {
				t.Errorf("expected false (fail-closed) for missing components")
			}
		})
	}
}
