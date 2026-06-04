package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestStartPurgeSweep_RemovesExpired(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()

	long := time.Now().UTC().Add(-60 * 24 * time.Hour)
	doc := &models.Document{
		ID: uuid.NewString(), Title: "old", Origin: "upload",
		CreatedAt: long, UpdatedAt: long, DeletedAt: &long,
	}
	_, _ = st.Documents().InsertOne(context.Background(), doc)

	cfg := testutil.LoadTestConfig(t)
	a, err := api.New(cfg, st)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	// Start the sweep goroutine. It runs once immediately, then daily.
	a.StartPurgeSweep()
	// Give it a moment to execute the first iteration.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := st.GetDeletedDocument(context.Background(), doc.ID)
		if got == nil {
			return // purged
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("expired doc was not purged by StartPurgeSweep")
}
