package api

import (
	"sync"
	"testing"
	"time"
)

func TestHub_BroadcastDelivers(t *testing.T) {
	h := NewHub()
	sub := h.Subscribe("docA")
	defer h.Unsubscribe(sub)

	h.Broadcast("docA", "comments-updated")
	select {
	case ev := <-sub.Events():
		if ev != "comments-updated" {
			t.Fatalf("got %q", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestHub_IsolatesByDocID(t *testing.T) {
	h := NewHub()
	a := h.Subscribe("docA")
	b := h.Subscribe("docB")
	defer h.Unsubscribe(a)
	defer h.Unsubscribe(b)

	h.Broadcast("docA", "x")
	select {
	case <-a.Events():
	case <-time.After(time.Second):
		t.Fatal("a should have received")
	}
	select {
	case ev := <-b.Events():
		t.Fatalf("b should not have received; got %q", ev)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestHub_UnsubscribeNoEvents(t *testing.T) {
	h := NewHub()
	sub := h.Subscribe("doc")
	h.Unsubscribe(sub)

	// Done() should be closed after Unsubscribe.
	select {
	case <-sub.Done():
	case <-time.After(time.Second):
		t.Fatal("Done should be closed after Unsubscribe")
	}

	// Broadcasting after unsubscribe should not panic.
	h.Broadcast("doc", "x")
}

func TestHub_BroadcastConcurrentUnsubscribeNoPanic(t *testing.T) {
	// Stress the race the previous implementation had: many subscribers
	// being torn down while a broadcast iterates the snapshot.
	h := NewHub()
	const N = 50
	subs := make([]*Subscriber, N)
	for i := range subs {
		subs[i] = h.Subscribe("doc")
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			h.Broadcast("doc", "evt")
		}
	}()
	go func() {
		defer wg.Done()
		for i := range subs {
			h.Unsubscribe(subs[i])
		}
	}()
	wg.Wait()
}

func TestHub_BroadcastUnknownDocIsNoOp(t *testing.T) {
	h := NewHub()
	// Should NOT panic when broadcasting to a doc with no subscribers.
	h.Broadcast("nobody", "x")
}

func TestHub_BufferOverflowDrops(t *testing.T) {
	h := NewHub()
	sub := h.Subscribe("doc")
	defer h.Unsubscribe(sub)
	// Channel capacity is 16; sending 100 events should drop excess.
	for i := 0; i < 100; i++ {
		h.Broadcast("doc", "evt")
	}
	// Drain — should be roughly 16 buffered.
	count := 0
	for {
		select {
		case <-sub.Events():
			count++
		default:
			if count == 0 {
				t.Fatal("expected at least one event delivered")
			}
			if count > 20 {
				t.Fatalf("delivered %d; expected drops past buffer cap (16)", count)
			}
			return
		}
	}
}
