package api_test

// Tests for the merge-preview + merge-accept handlers in source.go.
// Both handlers are large but each has cheap-to-test gates and
// noop fast paths. The SSE response is read raw so we can assert on
// the emitted event types.
//
// The GitHub-fetch happy paths need the same ghMock as pushback
// uses; we reuse mockGitHubForPushback here so the response shapes
// match what the handlers expect.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func insertGistlessGitHubDoc(t *testing.T, st interface {
	InsertDocument(ctx context.Context, d *models.Document) error
}, userID, owner, repo, content string) *models.Document {
	t.Helper()
	now := time.Now().UTC()
	d := &models.Document{
		ID:          uuid.NewString(),
		Title:       "Merge Target",
		Origin:      "url",
		SourceURL:   "https://github.com/" + owner + "/" + repo + "/blob/main/README.md",
		Content:     content,
		GitHubOwner: owner,
		GitHubRepo:  repo,
		GitHubRef:   "main",
		GitHubPath:  "README.md",
		CreatedByID: userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := st.InsertDocument(context.Background(), d); err != nil {
		t.Fatalf("insert merge doc: %v", err)
	}
	return d
}

// readSSE blocks until the server closes the stream and returns the
// concatenated event-body text. The merge-preview handler always
// emits exactly one event (done or error) on the noop fast paths
// we exercise here.
func readSSE(t *testing.T, srv *httptest.Server, path string, cookie string, body any) string {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
		rdr = buf
	}
	req, _ := http.NewRequest("POST", srv.URL+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "mm_session", Value: cookie})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sse request: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// --- mergePreviewSource ---

func TestMergePreviewSource_OursMatchesUpstream_NoopDone(t *testing.T) {
	// Set doc content == mocked upstream body ("# hello\n", base64
	// "IyBoZWxsbwo="). Preview should short-circuit with model=noop.
	restore := ghMock(t, mockGitHubForPushback())
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertGistlessGitHubDoc(t, st, user.ID, "owner", "repo", "# hello\n")

	body := readSSE(t, srv, "/api/documents/"+doc.ID+"/merge-preview", sess, nil)
	if !strings.Contains(body, `"model":"noop"`) {
		t.Errorf("expected noop model in SSE body, got %s", body)
	}
	if !strings.Contains(body, `event: done`) {
		t.Errorf("expected done event, got %s", body)
	}
}

// --- mergeAcceptSource ---

func TestMergeAcceptSource_RejectsBlankMergedContent(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "# old\n")

	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/merge-accept",
		map[string]any{
			"mergedContent":     "   ",
			"upstreamSourceSha": "sha",
			"upstreamContent":   "# new\n",
		}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d want 400 (blank merged)", status)
	}
}

func TestMergeAcceptSource_RejectsMissingUpstreamSHA(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "# old\n")

	status, _ := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/merge-accept",
		map[string]any{
			"mergedContent":     "# new content\n",
			"upstreamSourceSha": "",
			"upstreamContent":   "# new\n",
		}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d want 400 (missing sha)", status)
	}
}

func TestMergeAcceptSource_HappyPath_PersistsContentAndClearsDrift(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := testutil.NewTestDocument(t, st, user.ID, "# old\n")
	// Pre-stamp drifted state so we can verify it gets cleared.
	_, _ = st.Documents().UpdateOne(context.Background(),
		bson.M{"_id": doc.ID},
		bson.M{"$set": bson.M{
			"source_sha":        "old-sha",
			"source_latest_sha": "drifted-sha",
		}})

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/merge-accept",
		map[string]any{
			"mergedContent":     "# merged result\n",
			"upstreamSourceSha": "new-sha",
			"upstreamContent":   "# upstream\n",
		}, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}

	updated, _ := st.GetDocument(context.Background(), doc.ID)
	if updated == nil {
		t.Fatal("doc vanished")
	}
	if updated.SourceSHA != "new-sha" {
		t.Errorf("source_sha=%q want new-sha", updated.SourceSHA)
	}
	if updated.SourceLatestSHA != "" {
		t.Errorf("source_latest_sha should be cleared, got %q", updated.SourceLatestSHA)
	}
	if !strings.Contains(updated.Content, "merged result") {
		t.Errorf("content not updated: %q", updated.Content)
	}
}
