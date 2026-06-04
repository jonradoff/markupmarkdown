package api

import (
	"testing"
	"time"

	"markupmarkdown/internal/models"
)

func TestPlainTextFor_NilDocReturnsEmpty(t *testing.T) {
	if got := plainTextFor(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestPlainTextFor_CachesByUpdatedAt(t *testing.T) {
	doc := &models.Document{
		ID:        "doc1-" + time.Now().Format("150405.000000"),
		Content:   "# Hi\n\nBody.",
		UpdatedAt: time.Now(),
	}
	a := plainTextFor(doc)
	b := plainTextFor(doc)
	if a == "" || a != b {
		t.Fatalf("cache should return same value for same (id, updatedAt); got %q vs %q", a, b)
	}
}

func TestPlainTextFor_InvalidatesOnUpdatedAtChange(t *testing.T) {
	doc := &models.Document{
		ID:        "doc2-" + time.Now().Format("150405.000000"),
		Content:   "original",
		UpdatedAt: time.Now(),
	}
	first := plainTextFor(doc)

	doc.Content = "different"
	doc.UpdatedAt = doc.UpdatedAt.Add(time.Second)
	second := plainTextFor(doc)

	if first == second {
		t.Fatalf("expected different result after updatedAt advance")
	}
}
