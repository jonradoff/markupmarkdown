package store_test

// Integration tests covering the new store methods added for indexes,
// hidden items, and source-deduped document lookup.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestStoreIntegration_HiddenItems_HideListUnhide(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.NewString()
	// Hide two doc IDs and one index ID.
	if err := st.HideItem(ctx, userID, "doc", "d1"); err != nil {
		t.Fatalf("hide d1: %v", err)
	}
	if err := st.HideItem(ctx, userID, "doc", "d2"); err != nil {
		t.Fatalf("hide d2: %v", err)
	}
	if err := st.HideItem(ctx, userID, "index", "i1"); err != nil {
		t.Fatalf("hide i1: %v", err)
	}
	// Idempotent: hiding the same item twice is a no-op.
	if err := st.HideItem(ctx, userID, "doc", "d1"); err != nil {
		t.Fatalf("re-hide: %v", err)
	}

	docs, err := st.HiddenItemIDs(ctx, userID, "doc")
	if err != nil {
		t.Fatalf("list docs: %v", err)
	}
	if !docs["d1"] || !docs["d2"] {
		t.Errorf("expected d1+d2 hidden, got %+v", docs)
	}
	if docs["i1"] {
		t.Error("index id leaked into doc-kind listing")
	}

	idxs, _ := st.HiddenItemIDs(ctx, userID, "index")
	if !idxs["i1"] || idxs["d1"] {
		t.Errorf("indexes wrong: %+v", idxs)
	}

	// Different user sees nothing.
	other, _ := st.HiddenItemIDs(ctx, uuid.NewString(), "doc")
	if len(other) != 0 {
		t.Errorf("cross-user leak: %+v", other)
	}

	// Unhide restores the item.
	if err := st.UnhideItem(ctx, userID, "doc", "d1"); err != nil {
		t.Fatalf("unhide: %v", err)
	}
	docs, _ = st.HiddenItemIDs(ctx, userID, "doc")
	if docs["d1"] {
		t.Error("d1 should be unhidden")
	}
	if !docs["d2"] {
		t.Error("d2 should remain")
	}
}

func TestStoreIntegration_FindIndexBySource(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC()
	idx := &models.Index{
		ID:        uuid.NewString(),
		Kind:      models.IndexKindRepo,
		Owner:     "anthropics",
		Repo:      "claude-code",
		Title:     "anthropics/claude-code",
		SourceURL: "https://github.com/anthropics/claude-code",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.InsertIndex(ctx, idx); err != nil {
		t.Fatalf("insert: %v", err)
	}

	found, err := st.FindIndexBySource(ctx, models.IndexKindRepo, "anthropics", "claude-code")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil || found.ID != idx.ID {
		t.Fatalf("got %+v", found)
	}

	// Different repo → no match.
	none, _ := st.FindIndexBySource(ctx, models.IndexKindRepo, "anthropics", "missing")
	if none != nil {
		t.Errorf("got %+v, want nil", none)
	}

	// Empty owner returns nil without querying.
	none, _ = st.FindIndexBySource(ctx, models.IndexKindRepo, "", "x")
	if none != nil {
		t.Errorf("empty owner should short-circuit; got %+v", none)
	}
}

func TestStoreIntegration_FindIndexBySource_OldestWins(t *testing.T) {
	// Two indexes targeting the same source; FindIndexBySource returns
	// the OLDEST (first creator) as the canonical row.
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Use Repo-kind so both records have a non-empty `repo` field
	// (the model tags it as omitempty, so empty-string filter wouldn't
	// match a missing field).
	older := &models.Index{
		ID: "old-canonical", Kind: models.IndexKindRepo, Owner: "ownertest", Repo: "repotest", Title: "older",
		SourceURL: "https://github.com/ownertest/repotest",
		CreatedByID: "user-a",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}
	newer := &models.Index{
		ID: "new-clone", Kind: models.IndexKindRepo, Owner: "ownertest", Repo: "repotest", Title: "newer",
		SourceURL: "https://github.com/ownertest/repotest",
		CreatedByID: "user-b",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := st.InsertIndex(ctx, newer); err != nil {
		t.Fatalf("insert newer: %v", err)
	}
	if err := st.InsertIndex(ctx, older); err != nil {
		t.Fatalf("insert older: %v", err)
	}

	found, err := st.FindIndexBySource(ctx, models.IndexKindRepo, "ownertest", "repotest")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil || found.ID != "old-canonical" {
		t.Fatalf("expected oldest canonical winner; got %+v", found)
	}
}

func TestStoreIntegration_FindIndexBySource_SoftDeletedExcluded(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	idx := &models.Index{
		ID: "del", Kind: models.IndexKindRepo, Owner: "a", Repo: "b",
		SourceURL: "https://github.com/a/b",
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	_ = st.InsertIndex(ctx, idx)
	if err := st.SoftDeleteIndex(ctx, idx.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	got, _ := st.FindIndexBySource(ctx, models.IndexKindRepo, "a", "b")
	if got != nil {
		t.Errorf("deleted index leaked: %+v", got)
	}
}

func TestStoreIntegration_CachedIndexItems_RoundTrip(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	indexID := uuid.NewString()
	// Empty: returns nil, nil.
	got, err := st.GetCachedIndexItems(ctx, indexID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}

	// Upsert + read back.
	items := []byte(`[{"title":"X"}]`)
	if err := st.SetCachedIndexItems(ctx, indexID, items, true, "alice"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err = st.GetCachedIndexItems(ctx, indexID)
	if err != nil {
		t.Fatalf("get2: %v", err)
	}
	if got == nil {
		t.Fatal("cache should exist")
	}
	if string(got.ItemsJSON) != string(items) {
		t.Errorf("items_json mismatch: got %q", got.ItemsJSON)
	}
	if !got.Truncated || got.ViewerLogin != "alice" {
		t.Errorf("got %+v", got)
	}

	// Re-set with different audience — upsert should overwrite the row.
	if err := st.SetCachedIndexItems(ctx, indexID, []byte(`[]`), false, "bob"); err != nil {
		t.Fatalf("set2: %v", err)
	}
	got, _ = st.GetCachedIndexItems(ctx, indexID)
	if got.ViewerLogin != "bob" || got.Truncated {
		t.Errorf("overwrite failed: %+v", got)
	}
}

func TestStoreIntegration_UpdateIndexDefaultFilter(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	idx := &models.Index{
		ID: uuid.NewString(), Kind: models.IndexKindRepo, Owner: "a", Repo: "b",
		SourceURL: "https://github.com/a/b",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	_ = st.InsertIndex(ctx, idx)

	if err := st.UpdateIndexDefaultFilter(ctx, idx.ID, "readme"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ := st.GetIndex(ctx, idx.ID)
	if got.DefaultFilter != "readme" {
		t.Errorf("got %q", got.DefaultFilter)
	}

	// Clearing: passing "" unsets the field.
	if err := st.UpdateIndexDefaultFilter(ctx, idx.ID, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = st.GetIndex(ctx, idx.ID)
	if got.DefaultFilter != "" {
		t.Errorf("clear failed; got %q", got.DefaultFilter)
	}
}

func TestStoreIntegration_FindLatestDocumentBySource(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	owner, repo, ref, path := "anthropics", "x", "main", "README.md"

	// Insert two root docs for the same blob; the more-recently-
	// updated one is the canonical "latest".
	older := &models.Document{
		ID:           uuid.NewString(),
		Title:        "Old",
		Origin:       "url",
		GitHubOwner:  owner,
		GitHubRepo:   repo,
		GitHubRef:    ref,
		GitHubPath:   path,
		CreatedAt:    time.Now().Add(-2 * time.Hour),
		UpdatedAt:    time.Now().Add(-2 * time.Hour),
	}
	newer := &models.Document{
		ID:          uuid.NewString(),
		Title:       "New",
		Origin:      "url",
		GitHubOwner: owner,
		GitHubRepo:  repo,
		GitHubRef:   ref,
		GitHubPath:  path,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	_ = st.InsertDocument(ctx, older)
	_ = st.InsertDocument(ctx, newer)

	got, err := st.FindLatestDocumentBySource(ctx, owner, repo, ref, path)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil || got.ID != newer.ID {
		t.Fatalf("got %+v", got)
	}

	// Missing path → nil without query.
	none, _ := st.FindLatestDocumentBySource(ctx, owner, repo, ref, "")
	if none != nil {
		t.Errorf("got %+v", none)
	}
	none, _ = st.FindLatestDocumentBySource(ctx, "", repo, ref, path)
	if none != nil {
		t.Errorf("empty owner should short-circuit; got %+v", none)
	}

	// Different ref → no match.
	none, _ = st.FindLatestDocumentBySource(ctx, owner, repo, "other-branch", path)
	if none != nil {
		t.Errorf("wrong ref leaked: %+v", none)
	}
}

func TestStoreIntegration_FindLatestDocumentBySource_SkipsChildren(t *testing.T) {
	// Only chain roots (no parent_id) should be eligible. A child node
	// pointing at the same blob path is filtered out.
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	root := &models.Document{
		ID: uuid.NewString(), Title: "R", Origin: "url",
		GitHubOwner: "a", GitHubRepo: "b", GitHubRef: "main", GitHubPath: "X.md",
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	child := &models.Document{
		ID: uuid.NewString(), Title: "C", Origin: "url",
		ParentID:    root.ID,
		GitHubOwner: "a", GitHubRepo: "b", GitHubRef: "main", GitHubPath: "X.md",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = st.InsertDocument(ctx, root)
	_ = st.InsertDocument(ctx, child)

	got, _ := st.FindLatestDocumentBySource(ctx, "a", "b", "main", "X.md")
	if got == nil || got.ID != root.ID {
		t.Fatalf("expected root, got %+v", got)
	}
}

func TestStoreIntegration_FindLatestDocumentBySource_SoftDeleted(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	ctx := context.Background()

	doc := &models.Document{
		ID: uuid.NewString(), Title: "X", Origin: "url",
		GitHubOwner: "a", GitHubRepo: "b", GitHubRef: "main", GitHubPath: "Y.md",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	_ = st.InsertDocument(ctx, doc)
	_ = st.SoftDeleteDocument(ctx, doc.ID)

	got, _ := st.FindLatestDocumentBySource(ctx, "a", "b", "main", "Y.md")
	if got != nil {
		t.Errorf("soft-deleted doc leaked: %+v", got)
	}
}
