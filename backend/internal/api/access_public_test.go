package api

import (
	"context"
	"testing"
)

// IsPublicGitHubBlob is the exported wrapper around publicGitHubCheck
// the SPA handler uses to decide whether a doc title is safe to embed
// in og:title for Slack/Twitter unfurls. We just verify the empty-
// args fast path here — publicGitHubCheck's full HTTP round-trip is
// exercised at the integration tier.

func TestIsPublicGitHubBlob_EmptyArgsFailClosed(t *testing.T) {
	a := &API{}
	cases := []struct {
		name                   string
		owner, repo, ref, path string
	}{
		{"all empty", "", "", "", ""},
		{"owner empty", "", "repo", "main", "README.md"},
		{"repo empty", "owner", "", "main", "README.md"},
		{"ref empty", "owner", "repo", "", "README.md"},
		{"path empty", "owner", "repo", "main", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := a.IsPublicGitHubBlob(context.Background(), c.owner, c.repo, c.ref, c.path); got {
				t.Errorf("expected false (fail-closed) for empty args, got true")
			}
		})
	}
}
