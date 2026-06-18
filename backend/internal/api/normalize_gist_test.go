package api

import "testing"

func TestParseGistURL(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantOwner  string
		wantID     string
		wantRawURL string
		wantOK     bool
	}{
		{
			"landing page is rewritten to /raw",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8",
			"cdhanna", "f64c13646beb0cc18c7928765fffa9c8",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw",
			true,
		},
		{
			"existing /raw left alone",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw",
			"cdhanna", "f64c13646beb0cc18c7928765fffa9c8",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw",
			true,
		},
		{
			"specific revision /raw/<sha>/<file> left alone",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw/abc123/sample.md",
			"cdhanna", "f64c13646beb0cc18c7928765fffa9c8",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw/abc123/sample.md",
			true,
		},
		{
			"already a raw-host URL is recognized; owner+id extracted from path",
			"https://gist.githubusercontent.com/cdhanna/f64c136/raw/abc/sample.md",
			"cdhanna", "f64c136",
			"https://gist.githubusercontent.com/cdhanna/f64c136/raw/abc/sample.md",
			true,
		},
		{
			"non-gist github URL is not a gist",
			"https://github.com/owner/repo/blob/main/README.md",
			"", "", "",
			false,
		},
		{
			"non-github host",
			"https://example.com/foo.md",
			"", "", "",
			false,
		},
		{
			"gist URL with only owner (no gist id) rejected",
			"https://gist.github.com/cdhanna",
			"", "", "",
			false,
		},
		{
			"malformed URL",
			"::not a url::",
			"", "", "",
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseGistURL(c.in)
			if ok != c.wantOK {
				t.Errorf("ok=%v want %v", ok, c.wantOK)
			}
			if got.Owner != c.wantOwner {
				t.Errorf("owner=%q want %q", got.Owner, c.wantOwner)
			}
			if got.ID != c.wantID {
				t.Errorf("id=%q want %q", got.ID, c.wantID)
			}
			if got.RawURL != c.wantRawURL {
				t.Errorf("rawURL=%q want %q", got.RawURL, c.wantRawURL)
			}
		})
	}
}
