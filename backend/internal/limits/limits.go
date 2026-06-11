// Package limits provides cheap in-memory throttles: token-bucket rate
// limiters, concurrent-connection counters, and per-user concurrency
// semaphores. All state is process-local — fine for a single-node Fly
// deploy, would need Redis for horizontal scale.
package limits

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IP extracts the originating IP from an incoming request. On Fly.io,
// the trustworthy source is the `Fly-Client-IP` header — Fly's edge
// proxy sets it after stripping/normalizing any client-supplied
// headers. Falling back to X-Forwarded-For is dangerous: Fly appends
// rather than replaces, so an attacker can put a fake IP at the
// leftmost position and trivially defeat per-IP rate limits. We
// prefer Fly-Client-IP, then the RIGHTMOST XFF entry (last hop = Fly's
// edge), and only finally RemoteAddr.
func IP(r *http.Request) string {
	if fly := strings.TrimSpace(r.Header.Get("Fly-Client-IP")); fly != "" {
		return fly
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Rightmost entry is the last hop the request crossed before
		// reaching us — i.e., Fly's own proxy IP for the connecting
		// client. The leftmost (client-supplied) entries are
		// untrustworthy.
		if i := strings.LastIndex(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[i+1:])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Bucket is a sharded token-bucket limiter keyed by an arbitrary string.
type Bucket struct {
	rate    rate.Limit
	burst   int
	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	lim  *rate.Limiter
	seen time.Time
}

func NewBucket(perSecond float64, burst int) *Bucket {
	b := &Bucket{
		rate:    rate.Limit(perSecond),
		burst:   burst,
		entries: make(map[string]*entry),
	}
	go b.gc()
	return b
}

// Allow returns true if the key is under its budget.
func (b *Bucket) Allow(key string) bool {
	b.mu.Lock()
	e, ok := b.entries[key]
	if !ok {
		e = &entry{lim: rate.NewLimiter(b.rate, b.burst)}
		b.entries[key] = e
	}
	e.seen = time.Now()
	b.mu.Unlock()
	return e.lim.Allow()
}

func (b *Bucket) gc() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for now := range t.C {
		b.mu.Lock()
		for k, e := range b.entries {
			if now.Sub(e.seen) > 30*time.Minute {
				delete(b.entries, k)
			}
		}
		b.mu.Unlock()
	}
}

// Counter caps the total number of concurrent holders (e.g., SSE
// connections) and the count per identity.
type Counter struct {
	total     int
	perKey    int
	mu        sync.Mutex
	totalUsed int
	byKey     map[string]int
}

func NewCounter(total, perKey int) *Counter {
	return &Counter{total: total, perKey: perKey, byKey: make(map[string]int)}
}

// Acquire claims a slot for key. Returns release; release == nil means denied.
func (c *Counter) Acquire(key string) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.totalUsed >= c.total {
		return nil
	}
	if c.byKey[key] >= c.perKey {
		return nil
	}
	c.totalUsed++
	c.byKey[key]++
	return func() {
		c.mu.Lock()
		c.totalUsed--
		c.byKey[key]--
		if c.byKey[key] <= 0 {
			delete(c.byKey, key)
		}
		c.mu.Unlock()
	}
}

// PerKeySemaphore caps concurrent operations per key (per-user revisions).
type PerKeySemaphore struct {
	perKey int
	mu     sync.Mutex
	used   map[string]int
}

func NewPerKeySemaphore(perKey int) *PerKeySemaphore {
	return &PerKeySemaphore{perKey: perKey, used: make(map[string]int)}
}

// Acquire returns release or nil if at-cap.
func (s *PerKeySemaphore) Acquire(key string) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used[key] >= s.perKey {
		return nil
	}
	s.used[key]++
	return func() {
		s.mu.Lock()
		s.used[key]--
		if s.used[key] <= 0 {
			delete(s.used, key)
		}
		s.mu.Unlock()
	}
}
