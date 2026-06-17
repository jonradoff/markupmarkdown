package api

import (
	"context"
	"testing"
	"time"
)

// Pure cache-state tests that don't need GitHub roundtrips. The cache
// LOOKUP path (check / isPublic happy path) needs a real HTTP call —
// these tests target the bookkeeping side: invalidation, TTL expiry,
// and key-shape correctness — all of which are unit-testable without
// network.

func TestRepoAccessCache_InvalidateRemovesEntry(t *testing.T) {
	c := &accessCache{
		entries: map[repoCacheKey]repoCacheEntry{},
		ttl:     time.Minute,
	}
	key := repoCacheKey{"alice", "foo", "bar"}
	c.entries[key] = repoCacheEntry{Allowed: true, Expires: time.Now().Add(time.Hour)}
	if _, ok := c.entries[key]; !ok {
		t.Fatal("setup: entry missing")
	}
	c.invalidate("alice", "foo", "bar")
	if _, ok := c.entries[key]; ok {
		t.Errorf("expected entry to be gone after invalidate")
	}
}

func TestRepoAccessCache_InvalidateMissingIsNoop(t *testing.T) {
	// Invalidating a key that was never set should not panic and
	// should leave other entries untouched.
	c := &accessCache{
		entries: map[repoCacheKey]repoCacheEntry{
			{"bob", "x", "y"}: {Allowed: true, Expires: time.Now().Add(time.Hour)},
		},
		ttl: time.Minute,
	}
	c.invalidate("noone", "no", "such")
	if _, ok := c.entries[repoCacheKey{"bob", "x", "y"}]; !ok {
		t.Errorf("invalidate(missing) should not touch other entries")
	}
}

func TestRepoAccessCache_KeySensitivityToTuple(t *testing.T) {
	// invalidate uses the full (userID, owner, repo) tuple, so a
	// different user invalidating "the same repo" shouldn't disturb
	// the original user's cached row.
	c := &accessCache{
		entries: map[repoCacheKey]repoCacheEntry{
			{"alice", "foo", "bar"}: {Allowed: true, Expires: time.Now().Add(time.Hour)},
			{"bob", "foo", "bar"}:   {Allowed: false, Expires: time.Now().Add(time.Hour)},
		},
		ttl: time.Minute,
	}
	c.invalidate("bob", "foo", "bar")
	if _, ok := c.entries[repoCacheKey{"alice", "foo", "bar"}]; !ok {
		t.Errorf("alice's entry was wrongly removed when bob's was invalidated")
	}
	if _, ok := c.entries[repoCacheKey{"bob", "foo", "bar"}]; ok {
		t.Errorf("bob's entry should have been removed")
	}
}

func TestPublicFetchCache_InvalidateRemovesEntry(t *testing.T) {
	c := &publicFetchCacheT{
		entries: map[publicFetchKey]publicFetchEntry{},
		ttl:     time.Minute,
	}
	key := publicFetchKey{"foo", "bar", "main", "README.md"}
	c.entries[key] = publicFetchEntry{Public: true, Expires: time.Now().Add(time.Hour)}
	c.invalidate("foo", "bar", "main", "README.md")
	if _, ok := c.entries[key]; ok {
		t.Errorf("expected entry to be gone after invalidate")
	}
}

func TestPublicFetchCache_IsPublicReturnsCachedHit(t *testing.T) {
	// Pre-seed a fresh cached value so isPublic returns it without
	// touching the network. (The network branch is exercised by the
	// integration suite — here we just verify the cache shortcut.)
	c := &publicFetchCacheT{
		entries: map[publicFetchKey]publicFetchEntry{
			{"foo", "bar", "main", "README.md"}: {Public: true, Expires: time.Now().Add(time.Minute)},
		},
		ttl: time.Minute,
	}
	if !c.isPublic(context.Background(), "foo", "bar", "main", "README.md") {
		t.Errorf("expected cached hit to return true")
	}
}

func TestPublicFetchCache_IsPublicExpiredEntryFailsClosed(t *testing.T) {
	// Expired entries fall through to a real HTTP call. We can't easily
	// stub the http.Client used by isPublic without changing the
	// production code, so we just verify the cache doesn't return a
	// stale "true" — by setting Expires in the past and a phony
	// (owner, repo) that obviously can't be reached, the function
	// should fail closed (false).
	c := &publicFetchCacheT{
		entries: map[publicFetchKey]publicFetchEntry{
			{"obviously-not-a-real", "github-repo-12345", "main", "X.md"}: {Public: true, Expires: time.Now().Add(-time.Hour)},
		},
		ttl: time.Minute,
	}
	got := c.isPublic(context.Background(), "obviously-not-a-real", "github-repo-12345", "main", "X.md")
	if got {
		t.Errorf("expired entry must not return stale true (got %v)", got)
	}
}
