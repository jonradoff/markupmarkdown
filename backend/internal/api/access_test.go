package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// installMockTransport is duplicated here (we can't import the auth package
// test helpers) but does the same job: redirect any HTTP request through a
// closure.
type accessTestRT struct {
	handler func(*http.Request) *http.Response
}

func (m *accessTestRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.handler(req), nil
}

func swapTransport(h func(*http.Request) *http.Response) func() {
	prevClient := http.DefaultClient.Transport
	prevDefault := http.DefaultTransport
	rt := &accessTestRT{handler: h}
	http.DefaultClient.Transport = rt
	http.DefaultTransport = rt
	return func() {
		http.DefaultClient.Transport = prevClient
		http.DefaultTransport = prevDefault
	}
}

func makeResp(status int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.WriteHeader(status)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

func TestAccessCache_CachesAllowedThenServesFromCache(t *testing.T) {
	calls := 0
	restore := swapTransport(func(req *http.Request) *http.Response {
		calls++
		return makeResp(200, `{"id":1}`)
	})
	t.Cleanup(restore)

	// Use a fresh cache so other tests don't taint us.
	c := &accessCache{
		entries: map[repoCacheKey]repoCacheEntry{},
		ttl:     time.Minute,
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		ok, err := c.check(ctx, "user1", "tok", "ownerA", "repoA")
		if err != nil || !ok {
			t.Fatalf("iter %d: ok=%v err=%v", i, ok, err)
		}
	}
	if calls != 1 {
		t.Errorf("expected 1 GitHub call (cached); got %d", calls)
	}
}

func TestAccessCache_DoesNotCacheErrors(t *testing.T) {
	calls := 0
	restore := swapTransport(func(req *http.Request) *http.Response {
		calls++
		return makeResp(http.StatusUnauthorized, "no")
	})
	t.Cleanup(restore)

	c := &accessCache{entries: map[repoCacheKey]repoCacheEntry{}, ttl: time.Minute}
	for i := 0; i < 2; i++ {
		ok, err := c.check(context.Background(), "u", "t", "o", "r")
		if err == nil {
			t.Errorf("iter %d: expected error", i)
		}
		if ok {
			t.Errorf("iter %d: expected ok=false", i)
		}
	}
	if calls != 2 {
		t.Errorf("errors should not cache; got %d calls (want 2)", calls)
	}
}

func TestAccessCache_DeniedCachesNegative(t *testing.T) {
	calls := 0
	restore := swapTransport(func(req *http.Request) *http.Response {
		calls++
		return makeResp(http.StatusNotFound, "{}")
	})
	t.Cleanup(restore)

	c := &accessCache{entries: map[repoCacheKey]repoCacheEntry{}, ttl: time.Minute}
	for i := 0; i < 3; i++ {
		ok, err := c.check(context.Background(), "u", "t", "o", "r")
		if err != nil {
			t.Errorf("iter %d: err=%v", i, err)
		}
		if ok {
			t.Errorf("iter %d: should be false", i)
		}
	}
	if calls != 1 {
		t.Errorf("denials should cache; got %d calls", calls)
	}
}

func TestAccessCache_ConcurrentSafe(t *testing.T) {
	restore := swapTransport(func(req *http.Request) *http.Response {
		return makeResp(200, "{}")
	})
	t.Cleanup(restore)
	c := &accessCache{entries: map[repoCacheKey]repoCacheEntry{}, ttl: time.Minute}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = c.check(context.Background(), "u", "t", "o", "r")
		}(i)
	}
	wg.Wait()
}

// TestIsSafeRedirect_AdditionalCases covers a couple more branches missed
// by the helpers test.
func TestIsSafeRedirect_AdditionalCases(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"/safe", true},
		{"%ZZ", false}, // unparseable
	}
	for _, c := range cases {
		if got := isSafeRedirect(c.in); got != c.want {
			t.Errorf("isSafeRedirect(%q)=%v, want %v", c.in, got, c.want)
		}
	}
}
