package api

import "sync"

// Hub fans out per-document realtime events to connected SSE clients.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]map[*Subscriber]struct{}
}

// Subscriber receives events for a single docID. `done` is closed on
// Unsubscribe to release any in-flight Broadcast that's about to send on
// `ch`. We deliberately do NOT close `ch` itself — that would race with
// Broadcast's send-with-default-drop pattern and panic. Letting GC reclaim
// the channel after the last reference drops is fine.
type Subscriber struct {
	docID string
	ch    chan string
	done  chan struct{}
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*Subscriber]struct{})}
}

func (h *Hub) Subscribe(docID string) *Subscriber {
	sub := &Subscriber{
		docID: docID,
		ch:    make(chan string, 16),
		done:  make(chan struct{}),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.rooms[docID]; !ok {
		h.rooms[docID] = make(map[*Subscriber]struct{})
	}
	h.rooms[docID][sub] = struct{}{}
	return sub
}

func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if room, ok := h.rooms[sub.docID]; ok {
		delete(room, sub)
		if len(room) == 0 {
			delete(h.rooms, sub.docID)
		}
	}
	close(sub.done)
}

func (h *Hub) Broadcast(docID, event string) {
	h.mu.Lock()
	room := h.rooms[docID]
	subs := make([]*Subscriber, 0, len(room))
	for s := range room {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	for _, s := range subs {
		select {
		case <-s.done:
			// Subscriber unsubscribed between snapshot and send; skip.
		case s.ch <- event:
		default:
			// Slow consumer — drop. Client will refetch on next event.
		}
	}
}

// Events returns the receive-side of the subscriber channel. The channel is
// not closed; callers should also select on Done() to know when to exit.
func (s *Subscriber) Events() <-chan string { return s.ch }

// Done returns a channel that is closed when this subscriber is removed
// from the hub. Use as a select-arm alongside Events() so a stale
// subscriber's goroutine can exit even if no further events arrive.
func (s *Subscriber) Done() <-chan struct{} { return s.done }
