package limits

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestIP_FromXForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := IP(r); got != "1.2.3.4" {
		t.Fatalf("got %q, want 1.2.3.4", got)
	}
}

func TestIP_SingleXForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	if got := IP(r); got != "9.9.9.9" {
		t.Fatalf("got %q, want 9.9.9.9", got)
	}
}

func TestIP_FromRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:54321"
	if got := IP(r); got != "10.0.0.1" {
		t.Fatalf("got %q, want 10.0.0.1", got)
	}
}

func TestIP_MalformedRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "no-port"
	if got := IP(r); got != "no-port" {
		t.Fatalf("got %q, want no-port", got)
	}
}

func TestIP_HeaderTakesPrecedence(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "8.8.8.8")
	if got := IP(r); got != "8.8.8.8" {
		t.Fatalf("got %q", got)
	}
}

// Compile-time guarantee that http.Request exists; satisfies the import.
var _ = http.NoBody

func TestBucket_AllowsWithinBurst(t *testing.T) {
	b := NewBucket(0, 3)
	for i := 0; i < 3; i++ {
		if !b.Allow("k") {
			t.Fatalf("iteration %d denied; expected burst of 3 to be allowed", i)
		}
	}
	if b.Allow("k") {
		t.Fatal("4th call should be denied (burst exhausted, no refill)")
	}
}

func TestBucket_SeparateKeysIndependent(t *testing.T) {
	b := NewBucket(0, 1)
	if !b.Allow("a") {
		t.Fatal("first 'a' denied")
	}
	if !b.Allow("b") {
		t.Fatal("first 'b' denied (separate key should have its own bucket)")
	}
	if b.Allow("a") {
		t.Fatal("second 'a' should be denied")
	}
}

func TestBucket_ConcurrentSafe(t *testing.T) {
	b := NewBucket(100, 100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Allow("shared")
		}()
	}
	wg.Wait()
	// If we got here without a race, we're fine.
}

func TestCounter_TotalCap(t *testing.T) {
	c := NewCounter(2, 5)
	r1 := c.Acquire("a")
	r2 := c.Acquire("b")
	if r1 == nil || r2 == nil {
		t.Fatal("first two acquires should succeed")
	}
	if r3 := c.Acquire("c"); r3 != nil {
		t.Fatal("3rd acquire should be denied: total cap reached")
	}
	r1()
	if r3 := c.Acquire("c"); r3 == nil {
		t.Fatal("release should free a slot for new key")
	}
	r2()
}

func TestCounter_PerKeyCap(t *testing.T) {
	c := NewCounter(100, 2)
	a1 := c.Acquire("a")
	a2 := c.Acquire("a")
	if a1 == nil || a2 == nil {
		t.Fatal("first two 'a' acquires should succeed")
	}
	if a3 := c.Acquire("a"); a3 != nil {
		t.Fatal("3rd 'a' acquire should be denied: per-key cap reached")
	}
	if b := c.Acquire("b"); b == nil {
		t.Fatal("'b' should still be allowed: per-key counted separately")
	}
}

func TestPerKeySemaphore_Acquires(t *testing.T) {
	s := NewPerKeySemaphore(2)
	r1 := s.Acquire("u1")
	r2 := s.Acquire("u1")
	if r1 == nil || r2 == nil {
		t.Fatal("first two acquires should succeed")
	}
	if r3 := s.Acquire("u1"); r3 != nil {
		t.Fatal("3rd acquire should be denied")
	}
	r1()
	if r3 := s.Acquire("u1"); r3 == nil {
		t.Fatal("after release, acquire should succeed")
	}
	r2()
}

func TestPerKeySemaphore_KeysIndependent(t *testing.T) {
	s := NewPerKeySemaphore(1)
	r1 := s.Acquire("a")
	r2 := s.Acquire("b")
	if r1 == nil || r2 == nil {
		t.Fatal("keys should have independent budgets")
	}
}
