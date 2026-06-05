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

	// /robots.txt — allow everything, point at the sitemap.
	if r.URL.Path == "/robots.txt" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		body := "User-agent: *\nAllow: /\n\nSitemap: " + h.SiteURL + "/sitemap.xml\n"
		_, _ = w.Write([]byte(body))
		return
	}

	// /sitemap.xml — homepage + canonical agent guide. Per-doc URLs are
	// noindex by design (private + ephemeral); the homepage and SKILL.md
	// are the entry points crawlers should know about.
	if r.URL.Path == "/sitemap.xml" {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		body := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>` + h.SiteURL + `/</loc><changefreq>weekly</changefreq><priority>1.0</priority></url>
  <url><loc>` + h.SiteURL + `/SKILL.md</loc><changefreq>monthly</changefreq><priority>0.6</priority></url>
</urlset>
`
		_, _ = w.Write([]byte(body))
		return
	}

	cleaned := filepath.Clean(r.URL.Path)
	if cleaned == "/" || cleaned == "." {
		h.serveIndex(w, r, homepageMeta(h.SiteURL))
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
	// OGType controls the og:type; defaults to "article" when empty.
	OGType string
	// Canonical, when set, emits <link rel="canonical">.
	Canonical string
	// JSONLD, when non-empty, is injected as one or more <script
	// type="application/ld+json"> blocks. Each entry is a complete
	// JSON object string (caller is responsible for valid JSON).
	JSONLD []string
	// NoScript renders inside a <noscript> at the bottom of the
	// injection so crawlers that don't run JS — and humans on a
	// broken JS load — still see the homepage prose.
	NoScript string
}

func (m *ogMeta) renderHead() string {
	if m == nil {
		return ""
	}
	t := html.EscapeString(m.Title)
	d := html.EscapeString(m.Description)
	u := html.EscapeString(m.URL)
	ogType := m.OGType
	if ogType == "" {
		ogType = "article"
	}
	parts := []string{
		`<title>` + t + `</title>`,
		`<meta name="description" content="` + d + `">`,
		`<meta property="og:title" content="` + t + `">`,
		`<meta property="og:description" content="` + d + `">`,
		`<meta property="og:url" content="` + u + `">`,
		`<meta property="og:type" content="` + html.EscapeString(ogType) + `">`,
		`<meta property="og:site_name" content="markupmarkdown">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`<meta name="twitter:title" content="` + t + `">`,
		`<meta name="twitter:description" content="` + d + `">`,
	}
	if m.Canonical != "" {
		parts = append(parts, `<link rel="canonical" href="`+html.EscapeString(m.Canonical)+`">`)
	}
	for _, ld := range m.JSONLD {
		// JSON-LD is escaped via </ replacement to avoid </script> in
		// embedded strings closing the script tag prematurely. The
		// caller passes a JSON literal so we don't HTML-escape it.
		safe := strings.ReplaceAll(ld, "</", "<\\/")
		parts = append(parts, `<script type="application/ld+json">`+safe+`</script>`)
	}
	return strings.Join(parts, "\n    ")
}

// homepageMeta returns the SEO payload injected into `/` — title,
// description, JSON-LD, and the noscript fallback prose. Crawlers see
// the value prop, headings, and FAQ in the initial HTML response
// without needing to render JS.
func homepageMeta(siteURL string) *ogMeta {
	if siteURL == "" {
		siteURL = "https://mumd.metavert.io"
	}
	title := "Comment on Markdown Files Like Google Docs · markupmarkdown"
	desc := "Google-Docs-style commenting for any Markdown file. Paste a GitHub URL or upload, drag-select text, leave threaded comments. Realtime sync, @-mentions, AI revision via Claude, and an MCP server so agents review alongside humans."
	app := `{
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  "name": "markupmarkdown",
  "url": "` + siteURL + `",
  "applicationCategory": "DeveloperApplication",
  "operatingSystem": "Any (Web)",
  "description": "` + jsonEscape(desc) + `",
  "offers": {"@type": "Offer", "price": "0", "priceCurrency": "USD"},
  "license": "https://opensource.org/licenses/MIT",
  "softwareVersion": "0.1",
  "isAccessibleForFree": true
}`
	faq := `{
  "@context": "https://schema.org",
  "@type": "FAQPage",
  "mainEntity": [
    {"@type": "Question", "name": "How do I comment on a Markdown file?",
     "acceptedAnswer": {"@type": "Answer", "text": "Paste the URL of any .md file (raw or a github.com/.../blob/.../*.md link) or upload a local file, then drag-select text in the rendered document and click the Comment button. Replies, @-mentions, mark-as-done, and resolve are one click each."}},
    {"@type": "Question", "name": "Can I review Markdown files from private GitHub repos?",
     "acceptedAnswer": {"@type": "Answer", "text": "Yes. Sign in with GitHub and you can open files from any repo you have read access to. Markupmarkdown re-verifies your access on every read, so private docs stay private — losing access means losing visibility."}},
    {"@type": "Question", "name": "How does AI revision work?",
     "acceptedAnswer": {"@type": "Answer", "text": "Resolve the comments you want applied, then click Revise with AI. Claude Opus 4.7 produces a new revision that incorporates the resolved feedback while changing as little of the rest as possible. The output streams as rendered Markdown; you get a word-level diff before accepting. Saving creates a new child document so revisions form a tree."}},
    {"@type": "Question", "name": "What is the MCP server for?",
     "acceptedAnswer": {"@type": "Answer", "text": "Markupmarkdown ships a Model Context Protocol server at /mcp so AI agents (Claude Desktop, Claude Code, custom agents) can read documents, leave threads anchored to text spans, reply to humans, resolve threads, and trigger AI revisions. Agents authenticate via personal access tokens; the same access checks and rate limits apply as the REST API."}},
    {"@type": "Question", "name": "Is markupmarkdown free and self-hostable?",
     "acceptedAnswer": {"@type": "Answer", "text": "Yes. It is MIT-licensed open source. The whole stack is a single Go binary plus a React SPA plus MongoDB — designed to deploy on a single Fly.io machine. Bring your own Anthropic API key for AI revision so the costs and data stay with you."}}
  ]
}`
	noscript := `<h1>Comment on Markdown Files Like Google Docs</h1>
      <p>Paste a GitHub URL or upload a local .md file. Drag-select text in the rendered document, leave a margin comment, get threaded replies, @-mentions, resolve, and AI revision via Claude. Realtime sync via Server-Sent Events. Open source (MIT).</p>
      <h2>For PRDs, RFCs, release notes, and prompt libraries</h2>
      <p>Markdown is where a lot of real product thinking lives — but the review tools are miserable. Markupmarkdown brings Google-Docs-style margin comments to your existing .md files, without dragging your team into a code-review workflow.</p>
      <h2>Humans and AI agents review on the same documents</h2>
      <p>Markupmarkdown ships a Model Context Protocol (MCP) server so AI agents read what humans read, leave threads humans can approve, and apply resolved feedback as new revisions — with explicit human sign-off.</p>
      <h2>Open source, self-hosted, bring your own AI key</h2>
      <p>One Go binary, a React SPA, MongoDB. Deploys to a single Fly.io machine. Anthropic API key stored AES-256-GCM encrypted at rest, deletable any time.</p>
      <p><a href="https://github.com/jonradoff/markupmarkdown">GitHub repository</a> · <a href="/SKILL.md">Agent integration guide</a></p>`
	return &ogMeta{
		Title:       title,
		Description: desc,
		URL:         siteURL + "/",
		OGType:      "website",
		Canonical:   siteURL + "/",
		JSONLD:      []string{app, faq},
		NoScript:    noscript,
	}
}

// jsonEscape escapes a string for safe inclusion in a JSON literal.
// Only used for description text we control — not a general-purpose
// JSON encoder.
func jsonEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return r.Replace(s)
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
	// Inject the <noscript> fallback prose so crawlers (and humans on
	// a broken JS load) see the homepage value prop and FAQ headings
	// without rendering the React app.
	if meta.NoScript != "" {
		ns := "\n    <noscript>\n      " + meta.NoScript + "\n    </noscript>\n  "
		if i := strings.LastIndex(out, "</body>"); i >= 0 {
			out = out[:i] + ns + out[i:]
		}
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
