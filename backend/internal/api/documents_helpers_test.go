package api

import "testing"

func TestParseGitHubBlobURL_Standard(t *testing.T) {
	owner, repo, ref, path, ok := parseGitHubBlobURL("https://github.com/foo/bar/blob/main/docs/README.md")
	if !ok {
		t.Fatal("expected ok")
	}
	if owner != "foo" || repo != "bar" || ref != "main" || path != "docs/README.md" {
		t.Errorf("got %q/%q/%q/%q", owner, repo, ref, path)
	}
}

func TestParseGitHubBlobURL_NonGitHub(t *testing.T) {
	_, _, _, _, ok := parseGitHubBlobURL("https://example.com/foo/bar/blob/main/README.md")
	if ok {
		t.Fatal("non-github should not parse")
	}
}

func TestParseGitHubBlobURL_TooShort(t *testing.T) {
	_, _, _, _, ok := parseGitHubBlobURL("https://github.com/foo")
	if ok {
		t.Fatal("too-short path should not parse")
	}
}

func TestParseGitHubBlobURL_NoBlob(t *testing.T) {
	_, _, _, _, ok := parseGitHubBlobURL("https://github.com/foo/bar/tree/main/x")
	if ok {
		t.Fatal("non-blob path should not parse")
	}
}

func TestNormalizeGitHubURL_RewritesBlob(t *testing.T) {
	got := normalizeGitHubURL("https://github.com/foo/bar/blob/main/docs/README.md")
	want := "https://raw.githubusercontent.com/foo/bar/main/docs/README.md"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeGitHubURL_LeavesRawAlone(t *testing.T) {
	in := "https://raw.githubusercontent.com/foo/bar/main/README.md"
	if got := normalizeGitHubURL(in); got != in {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeGitHubURL_LeavesUnparseableAlone(t *testing.T) {
	in := "%ZZ"
	if got := normalizeGitHubURL(in); got != in {
		t.Errorf("got %q", got)
	}
}

func TestTitleFromURL_Basename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://example.com/path/README.md", "README.md"},
		{"https://example.com/", "https://example.com/"},
		{"%ZZ", "%ZZ"},
	}
	for _, c := range cases {
		if got := titleFromURL(c.in); got != c.want {
			t.Errorf("titleFromURL(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrimDetail_Long(t *testing.T) {
	src := make([]byte, 1000)
	for i := range src {
		src[i] = 'x'
	}
	got := trimDetail(string(src))
	// 400 ASCII chars + "…" (3 bytes UTF-8) = 403 bytes max.
	if len(got) > 403 {
		t.Errorf("expected truncation around 400+ellipsis, got %d chars", len(got))
	}
	if len(got) >= 1000 {
		t.Errorf("not truncated at all: %d", len(got))
	}
}

func TestTrimDetail_Short(t *testing.T) {
	if got := trimDetail("   short   "); got != "short" {
		t.Errorf("got %q", got)
	}
}

func TestStatusCodeFromFetchErr_Parses(t *testing.T) {
	if got := statusCodeFromFetchErr(stubError("http 404")); got != 404 {
		t.Errorf("got %d", got)
	}
	if got := statusCodeFromFetchErr(stubError("network unreachable")); got != 0 {
		t.Errorf("got %d", got)
	}
}

type stubError string

func (e stubError) Error() string { return string(e) }
