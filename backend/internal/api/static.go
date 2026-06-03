package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// SPAHandler serves static files from staticDir and falls back to index.html
// for any path that doesn't match a real file (so client-side routing works).
type SPAHandler struct {
	StaticDir string
}

func (h SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Never swallow API routes.
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	cleaned := filepath.Clean(r.URL.Path)
	if cleaned == "/" || cleaned == "." {
		http.ServeFile(w, r, filepath.Join(h.StaticDir, "index.html"))
		return
	}

	candidate := filepath.Join(h.StaticDir, cleaned)
	// Don't allow escaping the static dir via ".." segments.
	if !strings.HasPrefix(candidate, filepath.Clean(h.StaticDir)+string(os.PathSeparator)) &&
		candidate != filepath.Clean(h.StaticDir) {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		http.ServeFile(w, r, filepath.Join(h.StaticDir, "index.html"))
		return
	}
	http.ServeFile(w, r, candidate)
}
