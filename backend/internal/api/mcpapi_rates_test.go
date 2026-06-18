package api_test

// Tiny coverage for the rate-limit wrapper methods on *API that the
// MCP server uses. These are one-line delegations to the underlying
// token buckets — the buckets themselves are exhaustively tested in
// internal/limits — but having a smoke test here ensures the
// REST/MCP shared-budget contract isn't accidentally broken by a
// future field rename.

import (
	"testing"

	"markupmarkdown/internal/testutil"
)

func TestMCPAPI_AllowCommentRate_FirstCallAllowed(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	if !a.AllowCommentRate(user.ID) {
		t.Error("expected first comment-rate call to be allowed on a fresh limiter")
	}
}

func TestMCPAPI_AllowReviseRate_FirstCallAllowed(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	if !a.AllowReviseRate(user.ID) {
		t.Error("expected first revise-rate call to be allowed on a fresh limiter")
	}
}

func TestMCPAPI_AllowMergeRate_FirstCallAllowed(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	if !a.AllowMergeRate(user.ID) {
		t.Error("expected first merge-rate call to be allowed on a fresh limiter")
	}
}

func TestMCPAPI_AcquireReviseSlot_ReturnsReleaserOnSuccess(t *testing.T) {
	// First acquire succeeds and returns a non-nil releaser.
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	release, ok := a.AcquireReviseSlot(user.ID)
	if !ok {
		t.Fatal("expected first slot acquire to succeed")
	}
	if release == nil {
		t.Error("releaser should be non-nil even on the no-op zero path")
	}
	// Call the releaser — should not panic and should free the slot
	// for a subsequent acquire.
	release()
	release2, ok2 := a.AcquireReviseSlot(user.ID)
	if !ok2 {
		t.Error("re-acquire after release should succeed")
	}
	release2()
}

func TestMCPAPI_LogTokenAction_DelegatesWithoutPanic(t *testing.T) {
	// LogTokenAction is sampled (~1/min per token+action); we just
	// confirm it runs without panicking. The sampler's own tests
	// cover the sampling semantics.
	_, _, a := newTestServer(t)
	a.LogTokenAction(t.Context(), "tok-id", "test.action", "doc-id")
}

func TestMCPAPI_ValidateCommentBody_PassesThrough(t *testing.T) {
	_, _, a := newTestServer(t)
	cleaned, err := a.ValidateCommentBody("  hello world  ")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cleaned != "hello world" {
		t.Errorf("cleaned=%q want trimmed", cleaned)
	}
}

func TestMCPAPI_ValidateReplyBody_PassesThrough(t *testing.T) {
	_, _, a := newTestServer(t)
	cleaned, err := a.ValidateReplyBody("  hello  ")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cleaned != "hello" {
		t.Errorf("cleaned=%q want trimmed", cleaned)
	}
}
