package api_test

// Integration tests for SPAHandler — specifically the IsPublicGitHub
// gate that controls whether a GitHub-sourced doc's title is safe to
// emit into og:title (or whether a generic "Private document" template
// is used instead).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

// makeStaticDir creates a temp dir with a minimal index.html the
// SPAHandler can read + inject meta into.
func makeStaticDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `<!doctype html>
<html><head>
<title>markupmarkdown</title>
</head><body><div id="root"></div></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(body), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	return dir
}

func TestSPAHandler_PublicGitHubDoc_TitleRendered(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()

	doc := &models.Document{
		ID:          "12345678-1234-1234-1234-1234567890ab",
		Title:       "ImportantPublic.md",
		Origin:      "url",
		GitHubOwner: "anthropics",
		GitHubRepo:  "claude-code",
		GitHubRef:   "main",
		GitHubPath:  "ImportantPublic.md",
		Private:     false,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := st.InsertDocument(context.Background(), doc); err != nil {
		t.Fatalf("insert: %v", err)
	}

	h := api.SPAHandler{
		StaticDir: makeStaticDir(t),
		Store:     st,
		SiteURL:   "https://mumd.test",
		IsPublicGitHub: func(_ context.Context, _, _, _, _ string) bool {
			return true
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/d/"+doc.ID, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "ImportantPublic.md") {
		t.Errorf("public doc's title should be in the rendered head; body=%s", body)
	}
	if strings.Contains(body, "Private document") {
		t.Errorf("private placeholder leaked for a public doc; body=%s", body)
	}
}

func TestSPAHandler_PrivateGitHubDoc_GenericTitle(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()

	doc := &models.Document{
		ID:          "abcdef12-3456-7890-abcd-ef1234567890",
		Title:       "SECRET_PLAN.md",
		Origin:      "url",
		GitHubOwner: "anthropics",
		GitHubRepo:  "internal",
		GitHubRef:   "main",
		GitHubPath:  "SECRET_PLAN.md",
		Private:     false, // stored flag could be false, but live check says private
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := st.InsertDocument(context.Background(), doc); err != nil {
		t.Fatalf("insert: %v", err)
	}

	h := api.SPAHandler{
		StaticDir: makeStaticDir(t),
		Store:     st,
		SiteURL:   "https://mumd.test",
		IsPublicGitHub: func(_ context.Context, _, _, _, _ string) bool {
			// Live check says not public — the title MUST be replaced
			// with the generic placeholder so Slack unfurlers don't
			// leak SECRET_PLAN.md to anyone with the link.
			return false
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/d/"+doc.ID, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "SECRET_PLAN.md") {
		t.Errorf("PRIVATE doc title leaked! body=%s", body)
	}
	if !strings.Contains(body, "Private document") {
		t.Errorf("expected the generic private template; body=%s", body)
	}
}

func TestSPAHandler_PrivateFlagDoc_GenericTitle(t *testing.T) {
	// When the stored Private flag is already true, we use the generic
	// template without even calling IsPublicGitHub.
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()

	doc := &models.Document{
		ID:        "fedcba98-7654-3210-fedc-ba9876543210",
		Title:     "internal.md",
		Origin:    "url",
		Private:   true,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := st.InsertDocument(context.Background(), doc); err != nil {
		t.Fatalf("insert: %v", err)
	}

	called := false
	h := api.SPAHandler{
		StaticDir: makeStaticDir(t),
		Store:     st,
		SiteURL:   "https://mumd.test",
		IsPublicGitHub: func(_ context.Context, _, _, _, _ string) bool {
			called = true
			return true
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/d/"+doc.ID, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Error("IsPublicGitHub shouldn't be called when stored Private=true")
	}
	body := rec.Body.String()
	if strings.Contains(body, "internal.md") {
		t.Errorf("title leaked for stored-private doc: %s", body)
	}
	if !strings.Contains(body, "Private document") {
		t.Errorf("expected generic placeholder; got %s", body)
	}
}

func TestSPAHandler_NoIsPublicGitHubInjected_TrustsStoredFlag(t *testing.T) {
	// If IsPublicGitHub is nil, the handler skips the live check and
	// trusts the stored Private flag. A non-Private github-sourced
	// doc still gets its title rendered.
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	doc := &models.Document{
		ID:          "deadbeef-cafe-1234-5678-abcdef012345",
		Title:       "ok.md",
		Origin:      "url",
		GitHubOwner: "a",
		GitHubRepo:  "b",
		Private:     false,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	_ = st.InsertDocument(context.Background(), doc)
	h := api.SPAHandler{
		StaticDir:      makeStaticDir(t),
		Store:          st,
		SiteURL:        "https://mumd.test",
		IsPublicGitHub: nil,
	}
	req := httptest.NewRequest(http.MethodGet, "/d/"+doc.ID, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "ok.md") {
		t.Errorf("nil IsPublicGitHub should defer to stored flag (Private=false → reveal); body=%s", rec.Body.String())
	}
}

func TestSPAHandler_HomepageMetaInjected(t *testing.T) {
	h := api.SPAHandler{
		StaticDir: makeStaticDir(t),
		SiteURL:   "https://mumd.test",
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "og:site_name") {
		t.Errorf("homepage should include OG meta; body=%s", body)
	}
}

func TestSPAHandler_SkillMD(t *testing.T) {
	h := api.SPAHandler{StaticDir: makeStaticDir(t)}
	for _, p := range []string{"/SKILL.md", "/skill.md", "/skill"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
			t.Errorf("%s: content-type %q", p, ct)
		}
	}
}

func TestSPAHandler_RobotsTxt(t *testing.T) {
	h := api.SPAHandler{StaticDir: makeStaticDir(t), SiteURL: "https://mumd.test"}
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Sitemap: https://mumd.test/sitemap.xml") {
		t.Errorf("body=%s", rec.Body.String())
	}
}

func TestSPAHandler_Sitemap(t *testing.T) {
	h := api.SPAHandler{StaticDir: makeStaticDir(t), SiteURL: "https://mumd.test"}
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<urlset") || !strings.Contains(body, "SKILL.md") {
		t.Errorf("malformed sitemap: %s", body)
	}
}

func TestSPAHandler_APIRouteNotHandled(t *testing.T) {
	h := api.SPAHandler{StaticDir: makeStaticDir(t)}
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("API path should not be swallowed by SPA; got %d", rec.Code)
	}
}

func TestSPAHandler_MCPRouteNotHandled(t *testing.T) {
	h := api.SPAHandler{StaticDir: makeStaticDir(t)}
	req := httptest.NewRequest(http.MethodGet, "/mcp/anything", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("MCP path should not be swallowed by SPA; got %d", rec.Code)
	}
}

func TestSPAHandler_UnknownPathFallsToIndex(t *testing.T) {
	h := api.SPAHandler{StaticDir: makeStaticDir(t)}
	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<title>") {
		t.Errorf("expected index.html body")
	}
}

// Avoid unused import in case uuid grows or shrinks.
var _ = uuid.NewString
