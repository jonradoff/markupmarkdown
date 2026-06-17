package api

import (
	"testing"
	"time"
)

// currentEditLock is the package-level helper that the four edit-lock
// HTTP handlers all consult before issuing a lock decision. Its branches:
//   - no entry → (zero, false)
//   - entry not yet expired → (entry, true)
//   - entry past its TTL → auto-expire, (zero, false)
// All three are unit-testable without a DB or HTTP wiring.

func setEditLock(docID string, lock editLock) {
	editLocks.mu.Lock()
	defer editLocks.mu.Unlock()
	editLocks.m[docID] = lock
}

func clearEditLock(docID string) {
	editLocks.mu.Lock()
	defer editLocks.mu.Unlock()
	delete(editLocks.m, docID)
}

func TestCurrentEditLock_Missing(t *testing.T) {
	clearEditLock("doc-missing")
	_, ok := currentEditLock("doc-missing")
	if ok {
		t.Errorf("expected (zero, false) for missing docID, got ok=true")
	}
}

func TestCurrentEditLock_Active(t *testing.T) {
	docID := "doc-active"
	want := editLock{
		UserID:   "alice",
		UserName: "Alice",
		Expires:  time.Now().Add(10 * time.Minute),
	}
	setEditLock(docID, want)
	t.Cleanup(func() { clearEditLock(docID) })

	got, ok := currentEditLock(docID)
	if !ok {
		t.Fatal("expected ok=true for an unexpired lock")
	}
	if got.UserID != "alice" || got.UserName != "Alice" {
		t.Errorf("lock contents lost: got=%+v want %+v", got, want)
	}
}

func TestCurrentEditLock_ExpiredAutoCleared(t *testing.T) {
	docID := "doc-expired"
	setEditLock(docID, editLock{
		UserID:   "bob",
		UserName: "Bob",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
	t.Cleanup(func() { clearEditLock(docID) })

	_, ok := currentEditLock(docID)
	if ok {
		t.Errorf("expected ok=false for expired lock")
	}
	// Side effect: the expired entry was deleted from the map.
	editLocks.mu.Lock()
	_, stillThere := editLocks.m[docID]
	editLocks.mu.Unlock()
	if stillThere {
		t.Errorf("expired lock should be deleted from the map on read")
	}
}

func TestCurrentEditLock_KeyedByDocID(t *testing.T) {
	// A lock on doc-a is invisible from currentEditLock("doc-b").
	setEditLock("doc-a", editLock{UserID: "u", UserName: "U", Expires: time.Now().Add(time.Hour)})
	t.Cleanup(func() { clearEditLock("doc-a") })
	if _, ok := currentEditLock("doc-b"); ok {
		t.Errorf("doc-b lookup wrongly matched doc-a's lock")
	}
}
