package api_test

// Drift detection for gist-sourced docs. The github_blob path is
// already covered by the source_handlers + source_sync tests; this
// file just covers the gist branch dispatched from checkSourceNow.

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func insertGistDoc(t *testing.T, st interface {
	InsertDocument(ctx context.Context, d *models.Document) error
}, userID, gistID, gistCommit string) *models.Document {
	t.Helper()
	now := time.Now().UTC()
	d := &models.Document{
		ID:          uuid.NewString(),
		Title:       "Gist Doc",
		Origin:      "url",
		SourceKind:  models.SourceKindGist,
		SourceURL:   "https://gist.github.com/cdhanna/" + gistID,
		Content:     "# Hello gist\n",
		GistOwner:   "cdhanna",
		GistID:      gistID,
		GistCommit:  gistCommit,
		GistFileCount: 1,
		GistFilename:  "sample.md",
		CreatedByID:   userID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := st.InsertDocument(context.Background(), d); err != nil {
		t.Fatalf("insert gist doc: %v", err)
	}
	return d
}

func mockGistAPI(latestCommit string) func(*http.Request) *http.Response {
	return func(req *http.Request) *http.Response {
		if !strings.Contains(req.URL.Host, "api.github.com") ||
			!strings.HasPrefix(req.URL.Path, "/gists/") {
			return makeResp(200, "")
		}
		return makeResp(200, `{
			"files": {"sample.md": {"filename":"sample.md"}},
			"history": [{"version": "`+latestCommit+`"}]
		}`)
	}
}

func TestCheckSourceNow_Gist_InSyncClearsDriftMarker(t *testing.T) {
	// Pre-stamp the doc with a stale drift marker (e.g. user dismissed it
	// last week). When the gist's current commit matches the stored
	// baseline, the drift marker should be cleared.
	restore := ghMock(t, mockGistAPI("baseline-sha"))
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertGistDoc(t, st, user.ID, "gist1", "baseline-sha")
	// Stamp a stale drift marker so we can verify it gets cleared.
	_, _ = st.Documents().UpdateOne(context.Background(),
		bson.M{"_id": doc.ID},
		bson.M{"$set": bson.M{
			"source_latest_sha": "stale-drift",
			"source_drifted_at": time.Now().UTC(),
		}})

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/check-source", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"sourceLatestSha":""`) {
		t.Errorf("expected sourceLatestSha cleared, got %s", body)
	}
}

func TestCheckSourceNow_Gist_DriftSurfaced(t *testing.T) {
	// Gist's current commit differs from the stored baseline → drift
	// fields populated, banner appears in the response.
	restore := ghMock(t, mockGistAPI("new-upstream-sha"))
	defer restore()
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	doc := insertGistDoc(t, st, user.ID, "gist2", "baseline-sha")

	status, body := doJSON(t, srv, "POST", "/api/documents/"+doc.ID+"/check-source", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), `"sourceLatestSha":"new-upstream-sha"`) {
		t.Errorf("expected drift sha in response, got %s", body)
	}

	// Persistence check.
	updated, _ := st.GetDocument(context.Background(), doc.ID)
	if updated == nil {
		t.Fatal("doc vanished")
	}
	if updated.SourceLatestSHA != "new-upstream-sha" {
		t.Errorf("source_latest_sha=%q want new-upstream-sha", updated.SourceLatestSHA)
	}
}
