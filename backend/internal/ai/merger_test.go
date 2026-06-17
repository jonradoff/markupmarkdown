package ai

import (
	"context"
	"errors"
	"testing"
)

// Merge has six fast paths before it talks to Claude:
//   1. apiKey == "" → ErrKindInvalidKey
//   2. ours blank → ErrKindEmpty
//   3. theirs blank → ErrKindEmpty
//   4. ancestor == ours → trivial: theirs wins, model="noop"
//   5. ancestor == theirs → trivial: ours wins, model="noop"
//   6. ours == theirs → trivial: either wins, model="noop"
//
// All six are unit-testable without an Anthropic API key. The actual
// Claude call branch is exercised by integration tests with a mocked
// transport elsewhere.

func TestMerge_RejectsEmptyAPIKey(t *testing.T) {
	_, err := Merge(context.Background(), "", "title", "anc", "ours", "theirs", nil)
	var revErr *RevisionError
	if !errors.As(err, &revErr) || revErr.Kind != ErrKindInvalidKey {
		t.Errorf("got err=%v, want ErrKindInvalidKey", err)
	}
}

func TestMerge_RejectsBlankOurs(t *testing.T) {
	_, err := Merge(context.Background(), "k", "t", "anc", "   ", "theirs", nil)
	var revErr *RevisionError
	if !errors.As(err, &revErr) || revErr.Kind != ErrKindEmpty {
		t.Errorf("got err=%v, want ErrKindEmpty for blank ours", err)
	}
}

func TestMerge_RejectsBlankTheirs(t *testing.T) {
	_, err := Merge(context.Background(), "k", "t", "anc", "ours", "\n\t  ", nil)
	var revErr *RevisionError
	if !errors.As(err, &revErr) || revErr.Kind != ErrKindEmpty {
		t.Errorf("got err=%v, want ErrKindEmpty for blank theirs", err)
	}
}

func TestMerge_AncestorEqualsOurs_TheirsWins(t *testing.T) {
	// Common case: "I haven't revised this yet; just take the new
	// upstream wholesale." No Claude call.
	res, err := Merge(context.Background(), "k", "t",
		"# Original\n\nbody",
		"# Original\n\nbody",
		"# Original\n\nupstream changes",
		nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Content != "# Original\n\nupstream changes" {
		t.Errorf("expected theirs as merge content, got %q", res.Content)
	}
	if res.Model != "noop" {
		t.Errorf("model=%q want noop", res.Model)
	}
}

func TestMerge_AncestorEqualsTheirs_OursWins(t *testing.T) {
	// Upstream hasn't drifted relative to what we know; keep our
	// revision. No Claude call.
	res, err := Merge(context.Background(), "k", "t",
		"# Original\n\nbody",
		"# Original\n\nrevised body",
		"# Original\n\nbody",
		nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Content != "# Original\n\nrevised body" {
		t.Errorf("expected ours as merge content, got %q", res.Content)
	}
	if res.Model != "noop" {
		t.Errorf("model=%q want noop", res.Model)
	}
}

func TestMerge_OursEqualsTheirs_PassThrough(t *testing.T) {
	// Both branches landed identical content; merge is that content.
	// No Claude call.
	res, err := Merge(context.Background(), "k", "t",
		"original",
		"same revised content",
		"same revised content",
		nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Content != "same revised content" {
		t.Errorf("expected matched content passthrough, got %q", res.Content)
	}
	if res.Model != "noop" {
		t.Errorf("model=%q want noop", res.Model)
	}
}

func TestMerge_TrimsWhitespaceForComparison(t *testing.T) {
	// ancestor == ours comparison uses TrimSpace, so trailing newline
	// differences don't keep us out of the noop path.
	res, err := Merge(context.Background(), "k", "t",
		"body\n",
		"body",
		"upstream",
		nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Model != "noop" {
		t.Errorf("expected noop branch via trimmed-equal ancestor==ours, got model=%q", res.Model)
	}
}
