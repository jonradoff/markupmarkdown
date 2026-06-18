// One-shot migration: walk every document and stamp the new SourceKind
// field (and, for gists, the Gist* fields including the current commit
// SHA fetched from the GitHub Gist API).
//
// Idempotent — skips docs that already have source_kind set, so re-runs
// are no-ops. Best-effort on the gist API call: if the API errors (rate
// limit, network), the doc gets source_kind="gist" + owner + id stamped
// but no commit; the next maybeRefreshSourceDrift cycle will backfill.
//
// Run with the prod env loaded:
//
//   cd backend && MARKUPMARKDOWN_ENV=prod DATABASE_NAME=markupmarkdown \
//     go run ./cmd/migrate-source-kind
//
// CRITICAL: uses UpdateOne per doc (not UpdateMany). See
// MEMORY/feedback_never_ship_known_buggy_paths.md — the 2026-06-03
// incident where a bad UpdateMany filter polluted 4143 docs. Per-doc
// updates with explicit logging is the safe pattern; migrate-private
// follows it; we follow migrate-private.
package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/auth"
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
	defer func() { _ = st.Close(context.Background()) }()
	log.Printf("Stamping source_kind on docs in db=%s", cfg.Database.Name)

	ctx := context.Background()
	docs, err := st.ListDocuments(ctx)
	if err != nil {
		log.Fatalf("list: %v", err)
	}

	var checked, stamped, skipped int
	for _, d := range docs {
		// Skip docs that already have source_kind set — re-run safety.
		if d.SourceKind != "" {
			skipped++
			continue
		}
		checked++

		kind, gistOwner, gistID := classify(d)
		set := bson.M{
			"source_kind": kind,
			"updated_at":  time.Now().UTC(),
		}
		if kind == models.SourceKindGist {
			set["gist_owner"] = gistOwner
			set["gist_id"] = gistID
			// Best-effort: fetch the gist's current commit SHA + file
			// listing. If the API rejects (rate limit, secret-gist
			// without a token, etc.) we still stamp the kind/owner/id
			// — the next runtime drift check will backfill the commit.
			if meta, err := auth.FetchGistMeta(ctx, "", gistID); err == nil {
				set["gist_commit"] = meta.LatestCommit
				set["gist_filename"] = meta.PrimaryFilename
				set["gist_file_count"] = len(meta.Files)
				log.Printf("  GIST    %s/%s (%s, %d file(s)) ← %s",
					gistOwner, gistID, meta.LatestCommit, len(meta.Files), d.Title)
			} else {
				log.Printf("  GIST    %s/%s (no meta — %v) ← %s",
					gistOwner, gistID, err, d.Title)
			}
		} else {
			log.Printf("  %-7s %s", strings.ToUpper(kind), d.Title)
		}

		if _, err := st.Documents().UpdateOne(ctx,
			bson.M{"_id": d.ID},
			bson.M{"$set": set}); err != nil {
			log.Printf("  ERROR   %s: %v", d.ID, err)
			continue
		}
		stamped++
	}

	log.Printf("\nDone. checked=%d stamped=%d skipped=%d total=%d",
		checked, stamped, skipped, len(docs))
}

// classify returns (kind, gistOwner, gistID) for a doc. The decision
// tree mirrors the dispatch in fetchContent: upload → upload; URL
// pointed at a gist host → gist; URL with stamped github metadata or
// a github.com/blob shape → github_blob; everything else → url.
func classify(d models.Document) (kind, gistOwner, gistID string) {
	if d.Origin == "upload" {
		return models.SourceKindUpload, "", ""
	}
	if d.Origin != "url" {
		// Defensive: shouldn't happen in practice. Treat as plain URL.
		return models.SourceKindURL, "", ""
	}
	if owner, id, ok := parseGistOwnerID(d.SourceURL); ok {
		return models.SourceKindGist, owner, id
	}
	if d.GitHubOwner != "" {
		return models.SourceKindGitHubBlob, "", ""
	}
	if _, _, _, _, ok := parseGitHubBlobURL(d.SourceURL); ok {
		return models.SourceKindGitHubBlob, "", ""
	}
	return models.SourceKindURL, "", ""
}

// parseGistOwnerID extracts owner + gist id from any of the gist URL
// shapes we accept. Mirrors api.parseGistURL but lives here so the
// migration is self-contained.
func parseGistOwnerID(raw string) (owner, id string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(u.Host)
	if host != "gist.github.com" && host != "gist.githubusercontent.com" {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
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
