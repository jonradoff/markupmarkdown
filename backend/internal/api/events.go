package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// streamEvents opens an SSE stream that emits one event each time the
// comments/replies for this document change.
func (a *API) streamEvents(w http.ResponseWriter, r *http.Request) {
	docID := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, docID); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}

	// Cap concurrent SSE connections per identity.
	releaseSSE := a.sseCounter.Acquire(a.limitKey(r))
	if releaseSSE == nil {
		writeJSON(w, http.StatusServiceUnavailable, fetchErrorResponse{
			Error: "Too many open streaming connections. Close some tabs and retry.",
			Kind:  "sse_busy",
		})
		return
	}
	defer releaseSSE()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sub := a.hub.Subscribe(docID)
	defer a.hub.Unsubscribe(sub)

	// Initial hello so the client knows it's connected. docID has been
	// validated by checkDocAccess (the doc exists in our DB by this ID);
	// SSE stream is text/event-stream, not HTML, so it isn't an XSS sink.
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", docID) //nolint:gosec // see comment above
	flusher.Flush()

	// Heartbeat every 25s to keep proxies from cutting the connection.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.Done():
			// Defensive: we own the Unsubscribe on this goroutine, so we
			// shouldn't see this fire in practice. Still cheap to handle.
			return
		case event := <-sub.Events():
			fmt.Fprintf(w, "event: %s\ndata: %d\n\n", event, time.Now().UnixMilli())
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
