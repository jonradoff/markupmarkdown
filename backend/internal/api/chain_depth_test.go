package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"markupmarkdown/internal/models"
)

// stubDocStore implements the docStore interface for chainDepth tests
// without spinning up Mongo. Each key in children maps a parent id
// to the list of immediate children that ListChildren should return.
type stubDocStore struct {
	children map[string][]models.Document
	err      map[string]error
	calls    int
}

func (s *stubDocStore) ListChildren(_ context.Context, parentID string) ([]models.Document, error) {
	s.calls++
	if e, ok := s.err[parentID]; ok {
		return nil, e
	}
	return s.children[parentID], nil
}

func mkChild(id string, createdAt time.Time) models.Document {
	return models.Document{ID: id, CreatedAt: createdAt}
}

func TestChainDepth_RootEqualsLeaf(t *testing.T) {
	got := chainDepth(context.Background(), &stubDocStore{}, "same", "same")
	if got != 1 {
		t.Errorf("got=%d want 1", got)
	}
}

func TestChainDepth_LinearChain(t *testing.T) {
	now := time.Now()
	s := &stubDocStore{children: map[string][]models.Document{
		"root":  {mkChild("v2", now)},
		"v2":   {mkChild("v3", now.Add(1 * time.Minute))},
		"v3":   {mkChild("leaf", now.Add(2 * time.Minute))},
		"leaf": nil,
	}}
	got := chainDepth(context.Background(), s, "root", "leaf")
	if got != 4 {
		t.Errorf("got=%d want 4 (root→v2→v3→leaf)", got)
	}
}

func TestChainDepth_PicksMostRecentChildOnFork(t *testing.T) {
	// When a parent has multiple children, chainDepth follows the
	// most-recently-created edge.
	older := time.Now()
	newer := older.Add(10 * time.Minute)
	s := &stubDocStore{children: map[string][]models.Document{
		"root": {
			mkChild("old-branch-leaf", older),
			mkChild("new-branch-mid", newer),
		},
		"new-branch-mid":  {mkChild("leaf", newer.Add(1 * time.Minute))},
		"old-branch-leaf": nil,
		"leaf":            nil,
	}}
	got := chainDepth(context.Background(), s, "root", "leaf")
	if got != 3 {
		t.Errorf("got=%d want 3 (root→new-branch-mid→leaf via newest edge)", got)
	}
}

func TestChainDepth_StopsAtListChildrenError(t *testing.T) {
	s := &stubDocStore{
		children: map[string][]models.Document{
			"root": {mkChild("v2", time.Now())},
		},
		err: map[string]error{"v2": errors.New("listChildren blew up")},
	}
	got := chainDepth(context.Background(), s, "root", "leaf")
	if got != 2 {
		t.Errorf("got=%d want 2 (counted root + v2, then errored)", got)
	}
}

func TestChainDepth_HandlesCycleSafely(t *testing.T) {
	// Pathological: v2's children loop back to root. The seen set
	// catches the revisit so we don't infinite-loop.
	now := time.Now()
	s := &stubDocStore{children: map[string][]models.Document{
		"root": {mkChild("v2", now)},
		"v2":   {mkChild("root", now.Add(1 * time.Minute))},
	}}
	got := chainDepth(context.Background(), s, "root", "leaf")
	if got != 2 {
		t.Errorf("got=%d want 2 (counted root + v2, then hit cycle)", got)
	}
}

func TestChainDepth_RespectsDepthCap(t *testing.T) {
	// Build a chain longer than the 64-node cap. The function should
	// bail at the cap rather than chasing a runaway chain.
	now := time.Now()
	children := map[string][]models.Document{}
	for i := 0; i < 100; i++ {
		id := pad("v", i)
		next := pad("v", i+1)
		children[id] = []models.Document{mkChild(next, now.Add(time.Duration(i)*time.Minute))}
	}
	children["v100"] = nil
	got := chainDepth(context.Background(), &stubDocStore{children: children}, "v0", "v999")
	if got < 60 || got > 65 {
		t.Errorf("got=%d want ~64 (capped)", got)
	}
}

func pad(prefix string, n int) string {
	const digits = "0123456789"
	if n == 0 {
		return prefix + "0"
	}
	out := []byte{}
	for n > 0 {
		out = append([]byte{digits[n%10]}, out...)
		n /= 10
	}
	return prefix + string(out)
}
