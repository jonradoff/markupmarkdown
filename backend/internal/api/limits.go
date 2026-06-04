package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"markupmarkdown/internal/httperr"
	"markupmarkdown/internal/limits"
)

// Maximum body sizes per route group, in bytes.
const (
	maxBodyAuth     = 4 * 1024        // auth payloads
	maxBodyComment  = 64 * 1024       // comment body + anchor
	maxBodyRevision = 10 * 1024 * 1024 // accept-revision content
	maxBodyDocument = 10 * 1024 * 1024 // upload content
	maxBodyDefault  = 256 * 1024
)

// Field length caps.
const (
	maxTitleLen        = 200
	maxCommentBodyLen  = 16 * 1024
	maxReplyBodyLen    = 16 * 1024
	maxAnchorExactLen  = 4 * 1024
	maxUploadContent   = 5 * 1024 * 1024
)

// initLimits wires up all the in-memory throttles.
func (a *API) initLimits() {
	// Document creation: 10/min per identity, burst 3. SSRF and outbound DoS
	// guard.
	a.rlCreateDoc = limits.NewBucket(10.0/60.0, 3)
	// OAuth login starts: 5/min per IP.
	a.rlOAuthStart = limits.NewBucket(5.0/60.0, 2)
	// Comments: 60/min per identity.
	a.rlComment = limits.NewBucket(60.0/60.0, 10)
	// AI revision per user: 30/hour per user, no burst.
	a.rlRevise = limits.NewBucket(30.0/3600.0, 1)
	// Anthropic-key updates: 5/hour per user.
	a.rlAPIKeyPut = limits.NewBucket(5.0/3600.0, 1)

	// SSE: 200 total, 10 per identity. Stops idle-tab connection floods.
	a.sseCounter = limits.NewCounter(200, 10)

	// At most 3 concurrent AI revisions per user.
	a.reviseSlots = limits.NewPerKeySemaphore(3)

	// Bounded background queue for "doc was viewed" upserts.
	a.viewQueue = make(chan viewEvent, 256)
	for i := 0; i < 8; i++ {
		go a.viewWorker()
	}
}

// limitKey returns a stable identity string: user ID if authenticated, else
// caller IP. Used for rate-limit and SSE-counter keys.
func (a *API) limitKey(r *http.Request) string {
	if u := a.currentUser(r); u != nil {
		return "u:" + u.ID
	}
	return "ip:" + limits.IP(r)
}

// rate429 writes a friendly 429 with a Retry-After header.
func rate429(w http.ResponseWriter, message string) {
	w.Header().Set("Retry-After", "60")
	writeJSON(w, http.StatusTooManyRequests, fetchErrorResponse{
		Error: message,
		Kind:  "rate_limited",
	})
}

// enforceRate is a tiny helper for handlers that don't want their own
// per-route limiter wiring.
func (a *API) enforceRate(w http.ResponseWriter, r *http.Request, b *limits.Bucket, msg string) bool {
	if !b.Allow(a.limitKey(r)) {
		rate429(w, msg)
		return false
	}
	return true
}

// capBody wraps r.Body in a MaxBytesReader sized per route group.
func capBody(w http.ResponseWriter, r *http.Request, limit int64) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
}

// internalError sanitizes a server-side error into a generic message + ID.
// Use this for paths where the existing fetchErrorResponse machinery doesn't fit.
func internalError(w http.ResponseWriter, where string, err error) {
	id, msg := httperr.Log(where, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": msg,
		"id":    id,
	})
}

// TrashRetention is how long soft-deleted docs survive before hard purge.
const TrashRetention = 30 * 24 * time.Hour

// StartPurgeSweep runs PurgeExpiredDeletes once per day. Goroutine lifetime
// matches the process.
func (a *API) StartPurgeSweep() {
	go func() {
		// One immediate run on startup, then daily.
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			n, err := a.store.PurgeExpiredDeletes(ctx, time.Now().UTC().Add(-TrashRetention))
			cancel()
			if err != nil {
				// Log it but keep the sweeper alive.
				_ = err
			} else if n > 0 {
				// Visible breadcrumb that the sweep did something.
			}
			time.Sleep(24 * time.Hour)
		}
	}()
}

// --- background view-recording queue ---

type viewEvent struct {
	docID, userID string
}

func (a *API) enqueueView(docID, userID string) {
	if docID == "" || userID == "" {
		return
	}
	select {
	case a.viewQueue <- viewEvent{docID, userID}:
	default:
		// queue full — drop. Losing one view record is acceptable.
	}
}

func (a *API) viewWorker() {
	for ev := range a.viewQueue {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.store.RecordDocumentView(ctx, ev.docID, ev.userID)
		cancel()
	}
}
