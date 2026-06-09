package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// editLockTTL is how long a soft lock stays valid without a refresh.
// Long enough that a typical edit doesn't expire mid-thought; short
// enough that an abandoned tab clears within a reasonable window.
const editLockTTL = 5 * time.Minute

type editLock struct {
	UserID   string
	UserName string
	Expires  time.Time
}

// editLocks is the soft-lock registry. In-process; lost on restart.
// Two viewers seeing different locks for a few seconds after a
// restart is acceptable (the lock is advisory).
var editLocks = struct {
	mu sync.Mutex
	m  map[string]editLock
}{m: map[string]editLock{}}

func currentEditLock(docID string) (editLock, bool) {
	editLocks.mu.Lock()
	defer editLocks.mu.Unlock()
	lock, ok := editLocks.m[docID]
	if !ok {
		return editLock{}, false
	}
	if time.Now().After(lock.Expires) {
		delete(editLocks.m, docID)
		return editLock{}, false
	}
	return lock, true
}

// claimEditLock implements POST /api/documents/:id/edit-lock.
// Returns 200 when claimed (or refreshed by the same user); 409 with
// the holder's name when another user already holds the lock.
func (a *API) claimEditLock(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	_ = doc // access check is the goal; we don't need the doc body

	editLocks.mu.Lock()
	defer editLocks.mu.Unlock()
	if existing, ok := editLocks.m[id]; ok && existing.UserID != user.ID && time.Now().Before(existing.Expires) {
		// Held by someone else.
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    existing.UserName + " is editing this document.",
			"kind":     "edit_lock_held",
			"holder":   existing.UserName,
			"holderId": existing.UserID,
			"expires":  existing.Expires.UTC().Format(time.RFC3339),
		})
		return
	}
	displayName := user.Name
	if displayName == "" {
		displayName = user.Login
	}
	lock := editLock{
		UserID:   user.ID,
		UserName: displayName,
		Expires:  time.Now().Add(editLockTTL),
	}
	prev, hadPrev := editLocks.m[id]
	editLocks.m[id] = lock

	// Broadcast a "lock-changed" event so other viewers' banners
	// appear within an SSE round-trip. Skip the broadcast for pure
	// refresh by the same user (no state change other viewers care
	// about).
	if !hadPrev || prev.UserID != user.ID {
		go a.hub.Broadcast(id, "edit-lock-changed")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"holder":   lock.UserName,
		"holderId": lock.UserID,
		"expires":  lock.Expires.UTC().Format(time.RFC3339),
	})
}

// releaseEditLock implements DELETE /api/documents/:id/edit-lock.
// Only the holder can release; anyone else gets a 403.
func (a *API) releaseEditLock(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	editLocks.mu.Lock()
	existing, ok := editLocks.m[id]
	if !ok {
		editLocks.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if existing.UserID != user.ID {
		editLocks.mu.Unlock()
		writeError(w, http.StatusForbidden, "only the lock holder can release it")
		return
	}
	delete(editLocks.m, id)
	editLocks.mu.Unlock()

	go a.hub.Broadcast(id, "edit-lock-changed")
	w.WriteHeader(http.StatusNoContent)
}

// getEditLock implements GET /api/documents/:id/edit-lock. Used by
// the frontend on mount and on the edit-lock-changed SSE event to
// refresh state without holding an open stream of its own.
func (a *API) getEditLock(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	user := a.currentUser(r)
	lock, ok := currentEditLock(id)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"locked": false})
		return
	}
	mine := user != nil && lock.UserID == user.ID
	writeJSON(w, http.StatusOK, map[string]any{
		"locked":   true,
		"mine":     mine,
		"holder":   lock.UserName,
		"holderId": lock.UserID,
		"expires":  lock.Expires.UTC().Format(time.RFC3339),
	})
}
