package api

// Unit tests for the pure helpers in indexes.go. The handlers themselves
// are exercised via the integration tests; these table-driven tests cover
// the URL parser, naming helpers, and the cache-filter logic without
// needing a database or HTTP server.

import (
	"reflect"
	"strings"
	"testing"

	"markupmarkdown/internal/models"
)

func TestParseIndexURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    parsedIndexTarget
		wantErr bool
	}{
		{
			name: "https repo",
			in:   "https://github.com/anthropics/claude-code",
			want: parsedIndexTarget{Kind: models.IndexKindRepo, Owner: "anthropics", Repo: "claude-code"},
		},
		{
			name: "https user/org root",
			in:   "https://github.com/anthropics",
			want: parsedIndexTarget{Kind: models.IndexKindUser, Owner: "anthropics"},
		},
		{
			name: "https repo with trailing slash",
			in:   "https://github.com/anthropics/claude-code/",
			want: parsedIndexTarget{Kind: models.IndexKindRepo, Owner: "anthropics", Repo: "claude-code"},
		},
		{
			name: "https repo with /tree/<ref>",
			in:   "https://github.com/anthropics/claude-code/tree/main",
			want: parsedIndexTarget{Kind: models.IndexKindRepo, Owner: "anthropics", Repo: "claude-code"},
		},
		{
			name: "https repo with /blob/<ref>/<path>",
			in:   "https://github.com/anthropics/claude-code/blob/main/README.md",
			want: parsedIndexTarget{Kind: models.IndexKindRepo, Owner: "anthropics", Repo: "claude-code"},
		},
		{
			name: "https with query and fragment",
			in:   "https://github.com/anthropics/claude-code?utm=x#readme",
			want: parsedIndexTarget{Kind: models.IndexKindRepo, Owner: "anthropics", Repo: "claude-code"},
		},
		{
			name: "www host accepted",
			in:   "https://www.github.com/anthropics/claude-code",
			want: parsedIndexTarget{Kind: models.IndexKindRepo, Owner: "anthropics", Repo: "claude-code"},
		},
		{
			name: "bare owner/repo string",
			in:   "anthropics/claude-code",
			want: parsedIndexTarget{Kind: models.IndexKindRepo, Owner: "anthropics", Repo: "claude-code"},
		},
		{
			name: "bare owner only",
			in:   "anthropics",
			want: parsedIndexTarget{Kind: models.IndexKindUser, Owner: "anthropics"},
		},
		{
			name: "github.com/ prefix",
			in:   "github.com/anthropics/claude-code",
			want: parsedIndexTarget{Kind: models.IndexKindRepo, Owner: "anthropics", Repo: "claude-code"},
		},
		{
			name: "whitespace stripped",
			in:   "  https://github.com/anthropics  ",
			want: parsedIndexTarget{Kind: models.IndexKindUser, Owner: "anthropics"},
		},
		{
			name:    "empty url",
			in:      "",
			wantErr: true,
		},
		{
			name:    "whitespace-only url",
			in:      "   ",
			wantErr: true,
		},
		{
			name:    "wrong host",
			in:      "https://gitlab.com/foo/bar",
			wantErr: true,
		},
		{
			name:    "github root with no owner",
			in:      "https://github.com/",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseIndexURL(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestBasename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"README.md", "README.md"},
		{"docs/README.md", "README.md"},
		{"a/b/c/file.md", "file.md"},
		{"trailing/", ""},
		{"/leading.md", "leading.md"},
	}
	for _, c := range cases {
		if got := basename(c.in); got != c.want {
			t.Errorf("basename(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestPlural(t *testing.T) {
	if plural(0) != "s" {
		t.Errorf("plural(0)=%q, want s", plural(0))
	}
	if plural(1) != "" {
		t.Errorf("plural(1)=%q, want empty", plural(1))
	}
	if plural(2) != "s" {
		t.Errorf("plural(2)=%q, want s", plural(2))
	}
	if plural(-1) != "s" {
		// -1 is "not 1" so plural is the safe default.
		t.Errorf("plural(-1)=%q, want s", plural(-1))
	}
}

func TestDefaultIndexTitle(t *testing.T) {
	got := defaultIndexTitle(parsedIndexTarget{Kind: models.IndexKindRepo, Owner: "a", Repo: "b"})
	if got != "a/b" {
		t.Errorf("repo: got %q", got)
	}
	got = defaultIndexTitle(parsedIndexTarget{Kind: models.IndexKindUser, Owner: "anthropics"})
	if got != "anthropics" {
		t.Errorf("user: got %q", got)
	}
}

func TestCanonicalIndexURL(t *testing.T) {
	got := canonicalIndexURL(parsedIndexTarget{Owner: "a", Repo: "b"})
	if got != "https://github.com/a/b" {
		t.Errorf("repo: got %q", got)
	}
	got = canonicalIndexURL(parsedIndexTarget{Owner: "a"})
	if got != "https://github.com/a" {
		t.Errorf("user: got %q", got)
	}
}

func TestFilterPrivateForViewer(t *testing.T) {
	items := []indexItem{
		{Title: "public.md", PathInRepo: "public.md"},
		{Title: "secret.md", PathInRepo: "secret.md", Private: true},
		{Title: "another.md", PathInRepo: "another.md"},
		{Title: "internal.md", PathInRepo: "internal.md", Private: true},
	}

	t.Run("anonymous viewer hides private", func(t *testing.T) {
		// Make a copy because filterPrivateForViewer reuses the
		// underlying array — the original slice is clobbered.
		in := append([]indexItem(nil), items...)
		got := filterPrivateForViewer(in, nil, "alice")
		want := []indexItem{
			{Title: "public.md", PathInRepo: "public.md"},
			{Title: "another.md", PathInRepo: "another.md"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("non-scanner viewer hides private", func(t *testing.T) {
		in := append([]indexItem(nil), items...)
		bob := &models.User{Login: "bob"}
		got := filterPrivateForViewer(in, bob, "alice")
		if len(got) != 2 {
			t.Fatalf("got %d items, want 2", len(got))
		}
		for _, it := range got {
			if it.Private {
				t.Errorf("private item %q leaked to non-scanner", it.Title)
			}
		}
	})

	t.Run("scanner sees everything", func(t *testing.T) {
		in := append([]indexItem(nil), items...)
		alice := &models.User{Login: "alice"}
		got := filterPrivateForViewer(in, alice, "alice")
		if !reflect.DeepEqual(got, items) {
			t.Errorf("scanner should see all items; got %+v", got)
		}
	})

	t.Run("scanner login is case-insensitive", func(t *testing.T) {
		in := append([]indexItem(nil), items...)
		alice := &models.User{Login: "Alice"}
		got := filterPrivateForViewer(in, alice, "alice")
		if !reflect.DeepEqual(got, items) {
			t.Errorf("case-mismatch should still equate scanner; got %+v", got)
		}
	})

	t.Run("empty scanner login = filter regardless of viewer", func(t *testing.T) {
		// If we don't know who scanned the cache, we can't grant
		// anyone scanner-equivalent visibility.
		in := append([]indexItem(nil), items...)
		alice := &models.User{Login: "alice"}
		got := filterPrivateForViewer(in, alice, "")
		if len(got) != 2 {
			t.Errorf("got %d items, want 2 (private always filtered when scanner unknown)", len(got))
		}
	})

	t.Run("no private items passes through unchanged", func(t *testing.T) {
		clean := []indexItem{
			{Title: "a.md"},
			{Title: "b.md"},
		}
		in := append([]indexItem(nil), clean...)
		got := filterPrivateForViewer(in, nil, "alice")
		if !reflect.DeepEqual(got, clean) {
			t.Errorf("got %+v", got)
		}
	})
}

func TestRefsMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"main", "main", true},
		{"main", "refs/heads/main", true},
		{"refs/heads/main", "main", true},
		{"refs/heads/main", "refs/heads/main", true},
		{"main", "master", false},
		{"", "main", false},
		{"main", "", false},
		{"", "", false},
		{"refs/heads/feat/x", "feat/x", true},
	}
	for _, c := range cases {
		if got := refsMatch(c.a, c.b); got != c.want {
			t.Errorf("refsMatch(%q,%q)=%v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSanitizeBranch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"feat/x", "feat/x"},
		{"  /feat/x  ", "feat/x"},
		{"feat/x y z", "feat/x-y-z"},
		{"feat//x", "feat/x"},
		{"...x", "x"},
		{"valid-name", "valid-name"},
	}
	for _, c := range cases {
		if got := sanitizeBranch(c.in); got != c.want {
			t.Errorf("sanitizeBranch(%q)=%q, want %q", c.in, got, c.want)
		}
	}
	// Length cap kicks in at 240.
	long := ""
	for i := 0; i < 300; i++ {
		long += "a"
	}
	got := sanitizeBranch(long)
	if len(got) != 240 {
		t.Errorf("sanitizeBranch of 300 'a' should be capped to 240; got %d", len(got))
	}
}

func TestDefaultBranchName(t *testing.T) {
	doc := &models.Document{
		ID:    "1234567890abcdef",
		Title: "My Cool Doc.md",
	}
	user := &models.User{Login: "alice"}
	got := defaultBranchName(doc, user)
	want := "mumd/alice/my-cool-doc-12345678"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// No user: falls back to bare prefix.
	got = defaultBranchName(doc, nil)
	if got != "markupmarkdown/my-cool-doc-12345678" {
		t.Errorf("nil user: got %q", got)
	}

	// Empty title → branch is just prefix/<short-id>.
	bare := &models.Document{ID: "abcd1234", Title: ""}
	got = defaultBranchName(bare, user)
	if got != "mumd/alice/abcd1234" {
		t.Errorf("empty title: got %q", got)
	}

	// Long title is truncated to 40 chars.
	long := &models.Document{ID: "deadbeef", Title: "an extraordinarily long markdown filename that exceeds limits.md"}
	got = defaultBranchName(long, user)
	// Slug portion should be ≤40 chars + the suffix.
	if len(got) == 0 || !strings.Contains(got, "deadbeef") {
		t.Errorf("long title: got %q", got)
	}
}

func TestDefaultCommitMessage(t *testing.T) {
	plain := &models.Document{Title: "X.md"}
	if got := defaultCommitMessage(plain); got != "Update X.md via markupmarkdown" {
		t.Errorf("plain: got %q", got)
	}

	manual := &models.Document{
		Title:        "Y.md",
		RevisionMeta: &models.RevisionMeta{Model: "manual"},
	}
	if got := defaultCommitMessage(manual); got != "Edit Y.md via markupmarkdown" {
		t.Errorf("manual: got %q", got)
	}

	one := &models.Document{
		Title: "Z.md",
		RevisionMeta: &models.RevisionMeta{
			Model:             "opus-4-7",
			AppliedCommentIDs: []string{"a"},
		},
	}
	if got := defaultCommitMessage(one); got != "Apply 1 reviewed comment to Z.md via markupmarkdown" {
		t.Errorf("one: got %q", got)
	}

	many := &models.Document{
		Title: "Z.md",
		RevisionMeta: &models.RevisionMeta{
			Model:             "opus-4-7",
			AppliedCommentIDs: []string{"a", "b", "c"},
		},
	}
	if got := defaultCommitMessage(many); got != "Apply 3 reviewed comments to Z.md via markupmarkdown" {
		t.Errorf("many: got %q", got)
	}
}

func TestDefaultPRTitle_EqualsCommitMessage(t *testing.T) {
	doc := &models.Document{Title: "x.md"}
	if defaultPRTitle(doc) != defaultCommitMessage(doc) {
		t.Errorf("PR title should mirror commit message")
	}
}

func TestDefaultPRBody(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		doc := &models.Document{ID: "abc", Title: "x.md"}
		got := defaultPRBody(doc, "https://example.test")
		if !strings.Contains(got,"/d/abc") {
			t.Errorf("body should link back to /d/abc; got %q", got)
		}
		if !strings.Contains(got,"Direct push") {
			t.Errorf("plain push body should mention direct push; got %q", got)
		}
	})
	t.Run("manual revision", func(t *testing.T) {
		doc := &models.Document{
			ID:    "abc",
			Title: "x.md",
			RevisionMeta: &models.RevisionMeta{
				Model:       "manual",
				GeneratedBy: "Alice",
			},
		}
		got := defaultPRBody(doc, "https://example.test")
		if !strings.Contains(got,"Manual edit by Alice") {
			t.Errorf("manual body should credit author; got %q", got)
		}
	})
	t.Run("ai revision", func(t *testing.T) {
		doc := &models.Document{
			ID:    "abc",
			Title: "x.md",
			RevisionMeta: &models.RevisionMeta{
				Model:             "claude-opus-4-7",
				GeneratedBy:       "Bot Bob",
				AppliedCommentIDs: []string{"a", "b"},
			},
		}
		got := defaultPRBody(doc, "https://example.test")
		if !strings.Contains(got, "claude-opus-4-7") || !strings.Contains(got, "Bot Bob") {
			t.Errorf("ai body should include model + author; got %q", got)
		}
	})
	t.Run("empty frontend URL gets default", func(t *testing.T) {
		doc := &models.Document{ID: "abc", Title: "x.md"}
		got := defaultPRBody(doc, "")
		if !strings.Contains(got,"mumd.metavert.io") {
			t.Errorf("empty URL should default to mumd.metavert.io; got %q", got)
		}
	})
}

