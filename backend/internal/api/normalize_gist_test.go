package api

import "testing"

func TestNormalizeGistURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantOut string
		wantOK  bool
	}{
		{
			"landing page is rewritten to /raw",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw",
			true,
		},
		{
			"existing /raw left alone",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw",
			true,
		},
		{
			"specific revision /raw/<sha>/<file> left alone",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw/abc123/sample.md",
			"https://gist.github.com/cdhanna/f64c13646beb0cc18c7928765fffa9c8/raw/abc123/sample.md",
			true,
		},
		{
			"already a raw-host URL is recognized but unchanged",
			"https://gist.githubusercontent.com/cdhanna/f64c136/raw/abc/sample.md",
			"https://gist.githubusercontent.com/cdhanna/f64c136/raw/abc/sample.md",
			true,
		},
		{
			"non-gist github URL is not a gist",
			"https://github.com/owner/repo/blob/main/README.md",
			"",
			false,
		},
		{
			"non-github host",
			"https://example.com/foo.md",
			"",
			false,
		},
		{
			"gist URL with only owner (no gist id) rejected",
			"https://gist.github.com/cdhanna",
			"",
			false,
		},
		{
			"malformed URL",
			"::not a url::",
			"",
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := normalizeGistURL(c.in)
			if ok != c.wantOK {
				t.Errorf("ok=%v want %v", ok, c.wantOK)
			}
			if got != c.wantOut {
				t.Errorf("got=%q want %q", got, c.wantOut)
			}
		})
	}
}
