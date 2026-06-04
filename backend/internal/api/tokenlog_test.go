package api

import (
	"context"
	"testing"
	"time"
)

func TestLogTokenAction_NoopOnEmpty(t *testing.T) {
	a := &API{}
	// Empty inputs are silently dropped before any store access — must not
	// panic even when the store is nil.
	a.logTokenAction(context.Background(), "", "x", "y")
	a.logTokenAction(context.Background(), "x", "", "y")
}

func TestLogTokenAction_SamplerSuppressesRepeats(t *testing.T) {
	// Pre-populate the sampler so the call returns before spawning its
	// goroutine — that way we exercise the in-process throttle without
	// needing a real store. Then verify the timestamp didn't advance.
	const (
		tokenID = "tok-suppress"
		action  = "test.action"
	)
	key := tokenID + "|" + action
	now := time.Now().UTC()
	sampler.Lock()
	sampler.m[key] = now
	sampler.Unlock()

	a := &API{}
	// Immediate second call MUST be suppressed (no store dereference).
	a.logTokenAction(context.Background(), tokenID, action, "doc")

	sampler.Lock()
	saved := sampler.m[key]
	sampler.Unlock()
	if !saved.Equal(now) {
		t.Errorf("sampler timestamp should not advance during sample window; was %v, now %v", now, saved)
	}
}
