package api

// Unit tests for the SPAHandler pieces that don't need a real store —
// matchDocPath patterns and the homepageMeta + ogMeta head renderer.

import (
	"strings"
	"testing"
)

func TestMatchDocPath(t *testing.T) {
	cases := []struct {
		in     string
		id     string
		wantOK bool
	}{
		// Real-shaped IDs (UUIDs / short IDs ≥8 chars).
		{"/d/abcd1234", "abcd1234", true},
		{"/d/12345678-1234-1234-1234-1234567890ab", "12345678-1234-1234-1234-1234567890ab", true},

		// Too short / too long.
		{"/d/x", "", false},
		{"/d/abc", "", false},
		{"/d/" + strings.Repeat("z", 65), "", false},

		// Wrong prefix.
		{"/document/abcd1234", "", false},
		{"/", "", false},

		// Subpath / query / fragment in id portion → reject (caller
		// strips these for the doc-meta case).
		{"/d/abcd1234/extra", "", false},
		{"/d/abcd1234?x=1", "", false},
		{"/d/abcd1234#frag", "", false},
		{"/d/", "", false},
	}
	for _, c := range cases {
		id, ok := matchDocPath(c.in)
		if ok != c.wantOK {
			t.Errorf("matchDocPath(%q) ok=%v, want %v", c.in, ok, c.wantOK)
		}
		if id != c.id {
			t.Errorf("matchDocPath(%q) id=%q, want %q", c.in, id, c.id)
		}
	}
}

func TestOgMeta_RenderHead_Escapes(t *testing.T) {
	m := &ogMeta{
		Title:       `Sneaky <script>`,
		Description: `"quoted" & ampersand`,
		URL:         "https://x/d/abc",
	}
	got := m.renderHead()
	if strings.Contains(got, "<script>") {
		t.Error("title should be HTML-escaped")
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Error("expected escaped <script>")
	}
	if !strings.Contains(got, `og:title`) {
		t.Error("og:title should be present")
	}
	if !strings.Contains(got, `og:type`) {
		t.Error("og:type should be present")
	}
}

func TestOgMeta_RenderHead_NilSafe(t *testing.T) {
	var m *ogMeta
	if got := m.renderHead(); got != "" {
		t.Errorf("nil meta should render empty; got %q", got)
	}
}

func TestOgMeta_RenderHead_DefaultOGType(t *testing.T) {
	m := &ogMeta{Title: "t", Description: "d", URL: "u"}
	got := m.renderHead()
	if !strings.Contains(got, `og:type" content="article"`) {
		t.Errorf("empty OGType should default to article; got %q", got)
	}
}

func TestOgMeta_RenderHead_Canonical(t *testing.T) {
	m := &ogMeta{Title: "t", Description: "d", URL: "u", Canonical: "https://canon/x"}
	got := m.renderHead()
	if !strings.Contains(got, `<link rel="canonical" href="https://canon/x">`) {
		t.Errorf("canonical link missing; got %q", got)
	}
}

func TestOgMeta_RenderHead_JSONLDEscapesScript(t *testing.T) {
	m := &ogMeta{
		Title: "t", Description: "d", URL: "u",
		JSONLD: []string{`{"hostile":"</script><script>alert(1)</script>"}`},
	}
	got := m.renderHead()
	// The </ inside the JSON-LD string must be escaped so it can't
	// close the surrounding <script> tag.
	if strings.Contains(got, `</script><script>alert(1)`) {
		t.Error("hostile </script> should be escaped, found raw close tag")
	}
	if !strings.Contains(got, `<\/script>`) {
		t.Errorf("expected escaped </script> in JSON-LD; got %q", got)
	}
}

func TestHomepageMeta(t *testing.T) {
	m := homepageMeta("https://example.test")
	if m == nil {
		t.Fatal("nil")
	}
	if !strings.Contains(m.URL, "example.test") {
		t.Errorf("URL should embed site URL: %q", m.URL)
	}
	if len(m.JSONLD) == 0 {
		t.Error("expected JSON-LD payloads")
	}
}

func TestHomepageMeta_EmptySiteURLDefaults(t *testing.T) {
	m := homepageMeta("")
	if !strings.Contains(m.URL, "mumd.metavert.io") {
		t.Errorf("empty siteURL should fall back to default; got %q", m.URL)
	}
}
