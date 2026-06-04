package api

import (
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"markupmarkdown/internal/store"
)

// SkillMD is the raw markdown body of skills/markupmarkdown/SKILL.md, embedded
// at build time. Served verbatim at /skill.md so agents can fetch the
// canonical guide over a stable URL.
var SkillMD = ""

// SPAHandler serves static files from staticDir and falls back to index.html
// for any path that doesn't match a real file (so client-side routing works).
// Special-cases /d/:id to inject Open Graph + <title> meta into the served
// index.html so link unfurls (Slack, iMessage, Discord, X) carry the document
// title. Private docs get a generic placeholder card so titles don't leak.
type SPAHandler struct {
	StaticDir string
	Store     *store.Store
	SiteURL   string
}

func (h SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Never swallow API or MCP routes.
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/mcp") {
		http.NotFound(w, r)
		return
	}

	// Canonical SKILL.md for agents — served as plain markdown. We accept
	// uppercase, lowercase, and the bare /skill alias for forgiveness, but
	// the conventional URL is /SKILL.md.
	if r.URL.Path == "/SKILL.md" || r.URL.Path == "/skill.md" || r.URL.Path == "/skill" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write([]byte(SkillMD))
		return
	}

	cleaned := filepath.Clean(r.URL.Path)
	if cleaned == "/" || cleaned == "." {
		h.serveIndex(w, r, nil)
		return
	}

	if docID, ok := matchDocPath(cleaned); ok {
		var meta *ogMeta
		if h.Store != nil {
			if doc, err := h.Store.GetDocument(r.Context(), docID); err == nil && doc != nil {
				if doc.Private {
					meta = &ogMeta{
						Title:       "Private document · markupmarkdown",
						Description: "This document is private. Sign in with GitHub access to the source repo to view it.",
						URL:         h.SiteURL + r.URL.Path,
					}
				} else {
					meta = &ogMeta{
						Title:       doc.Title + " · markupmarkdown",
						Description: "Comment on this markdown file like a Google Doc.",
						URL:         h.SiteURL + r.URL.Path,
					}
				}
			}
		}
		h.serveIndex(w, r, meta)
		return
	}

	candidate := filepath.Join(h.StaticDir, cleaned)
	if !strings.HasPrefix(candidate, filepath.Clean(h.StaticDir)+string(os.PathSeparator)) &&
		candidate != filepath.Clean(h.StaticDir) {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		h.serveIndex(w, r, nil)
		return
	}
	http.ServeFile(w, r, candidate)
}

type ogMeta struct {
	Title       string
	Description string
	URL         string
}

func (m *ogMeta) renderHead() string {
	if m == nil {
		return ""
	}
	t := html.EscapeString(m.Title)
	d := html.EscapeString(m.Description)
	u := html.EscapeString(m.URL)
	return strings.Join([]string{
		`<title>` + t + `</title>`,
		`<meta name="description" content="` + d + `">`,
		`<meta property="og:title" content="` + t + `">`,
		`<meta property="og:description" content="` + d + `">`,
		`<meta property="og:url" content="` + u + `">`,
		`<meta property="og:type" content="article">`,
		`<meta property="og:site_name" content="markupmarkdown">`,
		`<meta name="twitter:card" content="summary">`,
		`<meta name="twitter:title" content="` + t + `">`,
		`<meta name="twitter:description" content="` + d + `">`,
	}, "\n    ")
}

// serveIndex reads index.html and, if meta is supplied, injects custom <title>
// and OG/Twitter meta tags just before </head>. Falls back to the static file
// on any read error.
func (h SPAHandler) serveIndex(w http.ResponseWriter, r *http.Request, meta *ogMeta) {
	path := filepath.Join(h.StaticDir, "index.html")
	if meta == nil {
		http.ServeFile(w, r, path)
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		http.ServeFile(w, r, path)
		return
	}
	out := string(body)
	// Replace the static <title> first (built by vite as `<title>markupmarkdown</title>`).
	if i := strings.Index(out, "<title>"); i >= 0 {
		if j := strings.Index(out[i:], "</title>"); j > 0 {
			out = out[:i] + out[i+j+len("</title>"):]
		}
	}
	inject := "\n    " + meta.renderHead() + "\n  "
	if i := strings.LastIndex(out, "</head>"); i >= 0 {
		out = out[:i] + inject + out[i:]
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store") // OG tags may differ per doc
	_, _ = w.Write([]byte(out))
}

// matchDocPath returns the doc UUID for paths of the shape /d/{id}.
func matchDocPath(p string) (string, bool) {
	const prefix = "/d/"
	if !strings.HasPrefix(p, prefix) {
		return "", false
	}
	rest := p[len(prefix):]
	if rest == "" || strings.ContainsAny(rest, "/?#") {
		return "", false
	}
	if len(rest) < 8 || len(rest) > 64 {
		return "", false
	}
	return rest, true
}
