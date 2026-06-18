package api_test

// MergeFromGitHub + PushToGitHub coverage. Both bridge MCP into the
// real GitHub round-trip; we exercise the noop pre-flight paths +
// the GitHub PR-mode happy path via ghMock. Real Claude merges are
// covered by integration tests in internal/ai (Merge fast-paths
// short-circuit before the Claude call there too).

import (
	"context"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/mcpserver"
	"markupmarkdown/internal/testutil"
)

// --- MergeFromGitHub ---

func TestMCPAPI_MergeFromGitHub_NonGitHubDoc(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello upload")
	_, err := a.MergeFromGitHub(context.Background(), user.ID, doc.ID, "", "")
	if err == nil {
		t.Fatal("expected error for non-github doc")
	}
	if !strings.Contains(err.Error(), "GitHub") {
		t.Errorf("err=%q want mention of github", err)
	}
}

func TestMCPAPI_MergeFromGitHub_UpstreamMatchesDocIsNoop(t *testing.T) {
	// When upstream content equals our current content, no merge is
	// needed — the noop branch returns model="noop" and NoMergeNeeded.
	// Our mock's FetchGitHubFileMeta returns "# hello\n" (base64
	// "IyBoZWxsbwo="), so seed the doc with that exact content.
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()

	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")
	// Overwrite the doc body so it matches the mocked upstream.
	_, _ = st.Documents().UpdateOne(context.Background(),
		bson.M{"_id": doc.ID},
		bson.M{"$set": bson.M{"content": "# hello\n"}})

	out, err := a.MergeFromGitHub(context.Background(), user.ID, doc.ID, "tok", "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Model != "noop" {
		t.Errorf("model=%q want noop", out.Model)
	}
	if !out.NoMergeNeeded {
		t.Errorf("expected NoMergeNeeded=true on noop path")
	}
}

// --- PushToGitHub ---

func TestMCPAPI_PushToGitHub_NonGitHubDoc(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello upload")
	_, err := a.PushToGitHub(context.Background(), user.ID, doc.ID, "", mcpserver.PushbackOpts{})
	if err == nil {
		t.Fatal("expected error for non-github doc")
	}
}

func TestMCPAPI_PushToGitHub_HappyPath(t *testing.T) {
	// Walk: GetRepoInfo → GetBranchSHA → CreateBranch → lookupFileSHA →
	// PutFile → CreatePull. Every endpoint is mocked.
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()

	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")

	out, err := a.PushToGitHub(context.Background(), user.ID, doc.ID, "tok",
		mcpserver.PushbackOpts{
			Branch:        "agent/edit",
			CommitMessage: "agent commit",
			PRTitle:       "agent PR",
			PRBody:        "body",
			TargetBranch:  "main",
		})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.PRNumber != 42 {
		t.Errorf("prNumber=%d want 42", out.PRNumber)
	}
	if out.CommitSHA != "new-commit-sha" {
		t.Errorf("commitSha=%q want new-commit-sha", out.CommitSHA)
	}
	if out.Branch != "agent/edit" {
		t.Errorf("branch=%q want agent/edit", out.Branch)
	}
}

func TestMCPAPI_PushToGitHub_DefaultsTargetBranchAndCommitMessage(t *testing.T) {
	// When TargetBranch + CommitMessage are blank, the handler looks
	// up the repo's default branch and uses a generated commit msg.
	// We just verify the call succeeds; the values themselves are
	// asserted at a finer level in pushback_test.go.
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()

	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := insertPushbackTestDoc(t, st, user.ID, "owner", "repo")

	out, err := a.PushToGitHub(context.Background(), user.ID, doc.ID, "tok",
		mcpserver.PushbackOpts{Branch: "agent/edit"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.PRNumber != 42 {
		t.Errorf("prNumber=%d want 42", out.PRNumber)
	}
}
