package api

import (
	"testing"
)

// row is a tiny helper for assembling fake summary rows in tests
// without retyping every JSON-tag-decorated field.
func row(id, owner, repo, srcURL, updated string) summary {
	return summary{
		ID:          id,
		Title:       id + ".md",
		SourceURL:   srcURL,
		Origin:      "url",
		GitHubOwner: owner,
		GitHubRepo:  repo,
		UpdatedAt:   updated,
	}
}

func TestDedupBySource_NoDuplicates_PassThrough(t *testing.T) {
	in := []summary{
		row("a", "foo", "bar", "https://github.com/foo/bar/blob/main/A.md", "2026-06-01T00:00:00Z"),
		row("b", "foo", "baz", "https://github.com/foo/baz/blob/main/B.md", "2026-06-02T00:00:00Z"),
	}
	out := dedupBySource(in)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	for _, r := range out {
		if r.OlderVersions != nil {
			t.Errorf("%s: expected no OlderVersions, got %d", r.ID, len(r.OlderVersions))
		}
	}
}

func TestDedupBySource_KeepsNewestCollapsesRest(t *testing.T) {
	// Three rows, same (owner, repo, path), different update times.
	in := []summary{
		row("old", "foo", "bar", "https://github.com/foo/bar/blob/main/X.md", "2026-06-01T00:00:00Z"),
		row("newest", "foo", "bar", "https://github.com/foo/bar/blob/main/X.md", "2026-06-10T00:00:00Z"),
		row("middle", "foo", "bar", "https://github.com/foo/bar/blob/main/X.md", "2026-06-05T00:00:00Z"),
	}
	out := dedupBySource(in)
	if len(out) != 1 {
		t.Fatalf("len=%d want 1", len(out))
	}
	if out[0].ID != "newest" {
		t.Errorf("kept ID=%q want newest (most-recently-updated)", out[0].ID)
	}
	if len(out[0].OlderVersions) != 2 {
		t.Fatalf("OlderVersions len=%d want 2", len(out[0].OlderVersions))
	}
	// Newest-older first.
	if out[0].OlderVersions[0].ID != "middle" || out[0].OlderVersions[1].ID != "old" {
		t.Errorf("OlderVersions order wrong: got %s,%s want middle,old",
			out[0].OlderVersions[0].ID, out[0].OlderVersions[1].ID)
	}
}

func TestDedupBySource_PreservesInputOrderForKeptRows(t *testing.T) {
	// First-seen ordering matters: in is sorted newest-touched first,
	// so the output should keep that same order across the kept rows.
	in := []summary{
		row("Z", "foo", "bar", "https://github.com/foo/bar/blob/main/Z.md", "2026-06-10T00:00:00Z"),
		row("A", "foo", "bar", "https://github.com/foo/bar/blob/main/A.md", "2026-06-09T00:00:00Z"),
		// Older sibling of Z; should fold into the existing Z row.
		row("Z_old", "foo", "bar", "https://github.com/foo/bar/blob/main/Z.md", "2026-06-01T00:00:00Z"),
	}
	out := dedupBySource(in)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].ID != "Z" || out[1].ID != "A" {
		t.Errorf("order=[%s,%s] want [Z,A]", out[0].ID, out[1].ID)
	}
	if len(out[0].OlderVersions) != 1 || out[0].OlderVersions[0].ID != "Z_old" {
		t.Errorf("Z should have 1 older copy (Z_old), got %v", out[0].OlderVersions)
	}
}

func TestDedupBySource_CaseInsensitiveOwnerRepoPath(t *testing.T) {
	// GitHub treats owner/repo names case-insensitively; if the same
	// file was ingested as Foo/Bar/X.md and foo/bar/x.md they should
	// still collapse into one row.
	in := []summary{
		row("upper", "Foo", "Bar", "https://github.com/Foo/Bar/blob/main/X.md", "2026-06-10T00:00:00Z"),
		row("lower", "foo", "bar", "https://github.com/foo/bar/blob/main/x.md", "2026-06-09T00:00:00Z"),
	}
	out := dedupBySource(in)
	if len(out) != 1 {
		t.Fatalf("len=%d want 1 (case-insensitive dedup)", len(out))
	}
}

func TestDedupBySource_DifferentPathsKeptApart(t *testing.T) {
	in := []summary{
		row("readme", "foo", "bar", "https://github.com/foo/bar/blob/main/README.md", "2026-06-10T00:00:00Z"),
		row("changelog", "foo", "bar", "https://github.com/foo/bar/blob/main/CHANGELOG.md", "2026-06-09T00:00:00Z"),
	}
	out := dedupBySource(in)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2 (different paths in same repo)", len(out))
	}
}

func TestDedupBySource_RefIgnored(t *testing.T) {
	// /blob/main/X.md and /blob/master/X.md after a default-branch
	// rename are the same logical file. Ref is intentionally not
	// part of the dedup key.
	in := []summary{
		row("main", "foo", "bar", "https://github.com/foo/bar/blob/main/X.md", "2026-06-10T00:00:00Z"),
		row("master", "foo", "bar", "https://github.com/foo/bar/blob/master/X.md", "2026-06-09T00:00:00Z"),
	}
	out := dedupBySource(in)
	if len(out) != 1 {
		t.Fatalf("len=%d want 1 (ref-insensitive dedup after rename)", len(out))
	}
	if out[0].ID != "main" {
		t.Errorf("kept=%s want main (most-recently-updated)", out[0].ID)
	}
}

func TestDedupBySource_UploadsNeverDedupe(t *testing.T) {
	in := []summary{
		{ID: "u1", Title: "X.md", Origin: "upload", UpdatedAt: "2026-06-10T00:00:00Z"},
		{ID: "u2", Title: "X.md", Origin: "upload", UpdatedAt: "2026-06-09T00:00:00Z"},
	}
	out := dedupBySource(in)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2 (uploads with no source URL never collapse)", len(out))
	}
	for _, r := range out {
		if r.OlderVersions != nil {
			t.Errorf("%s: uploads should not gain OlderVersions", r.ID)
		}
	}
}

func TestDedupBySource_UnparseableSourceURLPassesThrough(t *testing.T) {
	// A row with GitHubOwner/Repo set but a SourceURL that doesn't
	// parse as a github blob URL shouldn't blow up — keyOf returns ""
	// and the row passes through untouched.
	in := []summary{
		row("a", "foo", "bar", "not-a-real-url", "2026-06-10T00:00:00Z"),
		row("b", "foo", "bar", "not-a-real-url", "2026-06-09T00:00:00Z"),
	}
	out := dedupBySource(in)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2 (no key → no dedup)", len(out))
	}
}

func TestDedupBySource_EmptyInput(t *testing.T) {
	out := dedupBySource(nil)
	if len(out) != 0 {
		t.Fatalf("empty input: got %d rows, want 0", len(out))
	}
}
