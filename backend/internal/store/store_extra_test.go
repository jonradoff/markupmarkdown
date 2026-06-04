package store_test

// A handful of extra store coverage hits for paths not exercised by the
// main integration suite.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestStoreIntegration_GetUserHappy(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	u := testutil.NewTestUser(t, st)
	got, err := st.GetUser(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != u.ID {
		t.Fatalf("got %+v", got)
	}
}

func TestStoreIntegration_GetUserMissing(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	got, _ := st.GetUser(context.Background(), "nope")
	if got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestStoreIntegration_ListDocuments(t *testing.T) {
	st, cleanup := testutil.MustConnectTestDB(t)
	defer cleanup()
	// Insert two docs and verify ListDocuments returns them (irrespective
	// of user-scoping — this method is the global lister).
	for i := 0; i < 2; i++ {
		_ = st.InsertDocument(context.Background(), &models.Document{
			ID: uuid.NewString(), Title: "t", Origin: "upload",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}
	docs, err := st.ListDocuments(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("expected ≥2, got %d", len(docs))
	}
}
