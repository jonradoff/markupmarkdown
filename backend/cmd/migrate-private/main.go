// One-shot migration: scan every URL-sourced document, and for any GitHub
// blob URL where a public raw fetch fails, mark the document `private` and
// populate the GitHub owner/repo/ref/path fields so the access gate kicks in.
//
// Safe by design: only ever flips a doc from public → private. Never the
// other direction. Idempotent — re-running just no-ops on already-private
// docs.
//
// Run with the prod env loaded:
//
//   cd backend && MARKUPMARKDOWN_ENV=prod DATABASE_NAME=markupmarkdown \
//     go run ./cmd/migrate-private
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/config"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/store"
)

func main() {
	config.LoadEnvFile(".env")

	env := os.Getenv("MARKUPMARKDOWN_ENV")
	if env == "" {
		env = "dev"
	}
	cfg, err := config.Load(fmt.Sprintf("config/%s.yaml", env))
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	st, err := store.New(cfg.Database.URI, cfg.Database.Name)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close(context.Background())
	log.Printf("Migrating private flags in db=%s", cfg.Database.Name)

	ctx := context.Background()
	docs, err := st.ListDocuments(ctx)
	if err != nil {
		log.Fatalf("list: %v", err)
	}

	checked, flipped, skipped := 0, 0, 0
	for _, d := range docs {
		if d.Origin != "url" || d.Private {
			skipped++
			continue
		}
		owner, repo, ref, p, ok := parseGitHubBlobURL(d.SourceURL)
		if !ok {
			skipped++
			continue
		}
		checked++

		// Try public raw URL. If 200 we leave it alone (truly public).
		// If 4xx, the file requires GitHub auth → mark private.
		raw := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
			owner, repo, ref, p)
		ok, status := tryFetch(raw)
		if ok {
			log.Printf("  PUBLIC  %s  (%s)", d.Title, d.SourceURL)
			continue
		}
		if status < 400 || status >= 500 {
			log.Printf("  SKIP    %s  (raw fetch returned %d, ambiguous)", d.Title, status)
			skipped++
			continue
		}

		// Mark private.
		if _, err := st.Documents().UpdateOne(ctx,
			bson.M{"_id": d.ID},
			bson.M{"$set": bson.M{
				"private":      true,
				"github_owner": owner,
				"github_repo":  repo,
				"github_ref":   ref,
				"github_path":  p,
				"updated_at":   time.Now().UTC(),
			}}); err != nil {
			log.Printf("  ERROR   %s: %v", d.Title, err)
			continue
		}
		flipped++
		log.Printf("  PRIVATE %s/%s  →  %s", owner, repo, d.Title)
	}

	log.Printf("\nDone. checked=%d flipped=%d skipped=%d total=%d",
		checked, flipped, skipped, len(docs))
}

func tryFetch(rawURL string) (ok bool, status int) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
	req.Header.Set("User-Agent", "markupmarkdown-migrate/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300, resp.StatusCode
}

func parseGitHubBlobURL(raw string) (owner, repo, ref, path string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" {
		return
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" {
		return
	}
	return parts[0], parts[1], parts[3], strings.Join(parts[4:], "/"), true
}

// Keep the model import alive for `bson` decoding of existing docs.
var _ = models.Document{}
