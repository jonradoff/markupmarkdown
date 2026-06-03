package api

import "sync"

// Hub fans out per-document realtime events to connected SSE clients.
type Hub struct {
	mu      sync.Mutex
	rooms   map[string]map[*Subscriber]struct{}
}

type Subscriber struct {
	docID string
	ch    chan string
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*Subscriber]struct{})}
}

func (h *Hub) Subscribe(docID string) *Subscriber {
	sub := &Subscriber{
		docID: docID,
		ch:    make(chan string, 16),
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
	close(sub.ch)
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
		case s.ch <- event:
		default:
			// drop on slow consumer; client will refetch on next event anyway
		}
	}
}

func (s *Subscriber) Events() <-chan string { return s.ch }
