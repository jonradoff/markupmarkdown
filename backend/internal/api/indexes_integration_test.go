package api_test

// Integration tests for the index HTTP handlers. The handlers that
// actually scan GitHub (createIndex, materialize, etc.) need the
// GitHub HTTP transport mocked; here we focus on the handlers whose
// behavior is observable WITHOUT outbound calls — getIndex, deleteIndex,
// patchIndex, listMyIndexes, forgetIndex — and the gating logic.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func insertTestIndex(t *testing.T, st interface {
	InsertIndex(context.Context, *models.Index) error
}, creatorID string) *models.Index {
	t.Helper()
	idx := &models.Index{
		ID:          uuid.NewString()[:8],
		Kind:        models.IndexKindRepo,
		Owner:       "anthropics",
		Repo:        "claude-code",
		Title:       "anthropics/claude-code",
		SourceURL:   "https://github.com/anthropics/claude-code",
		Private:     false,
		CreatedByID: creatorID,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := st.InsertIndex(context.Background(), idx); err != nil {
		t.Fatalf("insert index: %v", err)
	}
	return idx
}

func TestGetIndex_NotFound(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "GET", "/api/indexes/nope", nil)
	if status != 404 {
		t.Errorf("status=%d", status)
	}
}

func TestGetIndex_PrivateRequiresAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	idx := &models.Index{
		ID:          "priv1234",
		Kind:        models.IndexKindRepo,
		Owner:       "secret",
		Repo:        "vault",
		Title:       "secret/vault",
		SourceURL:   "https://github.com/secret/vault",
		Private:     true,
		CreatedByID: user.ID,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	_ = st.InsertIndex(context.Background(), idx)

	// Anonymous viewer is rejected at the gate.
	status, _ := doJSON(t, srv, "GET", "/api/indexes/priv1234", nil)
	if status != 401 {
		t.Errorf("anon status=%d, want 401", status)
	}
}

func TestListMyIndexes_RequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "GET", "/api/me/indexes", nil)
	if status != 401 {
		t.Errorf("status=%d", status)
	}
}

func TestListMyIndexes_FiltersHidden(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)

	live := insertTestIndex(t, st, user.ID)
	hidden := insertTestIndex(t, st, user.ID)

	// Hide one.
	_ = st.HideItem(context.Background(), user.ID, "index", hidden.ID)

	status, body := doJSON(t, srv, "GET", "/api/me/indexes", nil, withCookie(sess))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), live.ID) {
		t.Errorf("live index should be present: %s", body)
	}
	if strings.Contains(string(body), hidden.ID) {
		t.Errorf("hidden index should be filtered out: %s", body)
	}
}

func TestForgetIndex_RequiresAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	idx := insertTestIndex(t, st, user.ID)

	status, _ := doJSON(t, srv, "POST", "/api/indexes/"+idx.ID+"/forget", nil)
	if status != 401 {
		t.Errorf("status=%d, want 401", status)
	}
}

func TestForgetIndex_HappyPath(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	idx := insertTestIndex(t, st, user.ID)

	status, body := doJSON(t, srv, "POST", "/api/indexes/"+idx.ID+"/forget",
		nil, withCookie(sess))
	if status != 204 {
		t.Errorf("status=%d body=%s", status, body)
	}

	// Should no longer appear in My Indexes.
	_, body = doJSON(t, srv, "GET", "/api/me/indexes", nil, withCookie(sess))
	if strings.Contains(string(body), idx.ID) {
		t.Errorf("forgotten index leaked: %s", body)
	}
}

func TestForgetIndex_NotFound(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, _ := doJSON(t, srv, "POST", "/api/indexes/missing/forget",
		nil, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d, want 404", status)
	}
}

func TestDeleteIndex_RequiresAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	idx := insertTestIndex(t, st, user.ID)
	status, _ := doJSON(t, srv, "DELETE", "/api/indexes/"+idx.ID, nil)
	// no creator-of-record auth → 401 (or 404 if non-existent path).
	if status != 401 {
		t.Errorf("status=%d, want 401", status)
	}
}

func TestDeleteIndex_OnlyCreatorCanDelete(t *testing.T) {
	srv, st, _ := newTestServer(t)
	creator := testutil.NewTestUser(t, st)
	intruder := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, intruder.ID)
	idx := insertTestIndex(t, st, creator.ID)

	status, _ := doJSON(t, srv, "DELETE", "/api/indexes/"+idx.ID,
		nil, withCookie(sess))
	if status != 403 {
		t.Errorf("status=%d, want 403 (non-creator)", status)
	}
}

func TestDeleteIndex_HappyPath(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	idx := insertTestIndex(t, st, user.ID)

	status, _ := doJSON(t, srv, "DELETE", "/api/indexes/"+idx.ID,
		nil, withCookie(sess))
	if status != 204 {
		t.Errorf("status=%d, want 204", status)
	}
	// Subsequent get → 404.
	status, _ = doJSON(t, srv, "GET", "/api/indexes/"+idx.ID, nil)
	if status != 404 {
		t.Errorf("get-after-delete status=%d, want 404", status)
	}
}

func TestDeleteIndex_NotFound(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, _ := doJSON(t, srv, "DELETE", "/api/indexes/nope",
		nil, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d, want 404", status)
	}
}

func TestPatchIndex_OnlyCreatorCanEdit(t *testing.T) {
	srv, st, _ := newTestServer(t)
	creator := testutil.NewTestUser(t, st)
	intruder := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, intruder.ID)
	idx := insertTestIndex(t, st, creator.ID)

	status, _ := doJSON(t, srv, "PATCH", "/api/indexes/"+idx.ID,
		map[string]string{"title": "Hijacked"}, withCookie(sess))
	if status != 403 {
		t.Errorf("status=%d, want 403", status)
	}
}

func TestPatchIndex_RequiresAuth(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	idx := insertTestIndex(t, st, user.ID)
	status, _ := doJSON(t, srv, "PATCH", "/api/indexes/"+idx.ID,
		map[string]string{"title": "x"})
	if status != 401 {
		t.Errorf("status=%d, want 401", status)
	}
}

func TestPatchIndex_NotFound(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, _ := doJSON(t, srv, "PATCH", "/api/indexes/missing",
		map[string]string{"title": "x"}, withCookie(sess))
	if status != 404 {
		t.Errorf("status=%d, want 404", status)
	}
}

func TestPatchIndex_BlankTitleRejected(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	idx := insertTestIndex(t, st, user.ID)
	status, _ := doJSON(t, srv, "PATCH", "/api/indexes/"+idx.ID,
		map[string]string{"title": "   "}, withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400 (blank title)", status)
	}
}

// Note for the next two tests: patchIndex finalizes its response via
// respondIndexWithItems, which serves from the index_items cache when
// present. Without this seed, the handler would try to materialize
// live from github.com and 401 against the fake test user's empty
// access token. We just need the meta update to round-trip; the
// materialization path itself is exercised by tests that mock the
// GitHub transport.

func TestPatchIndex_RenameSucceeds(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	idx := insertTestIndex(t, st, user.ID)
	if err := st.SetCachedIndexItems(context.Background(), idx.ID, []byte("[]"), false, user.Login); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	status, body := doJSON(t, srv, "PATCH", "/api/indexes/"+idx.ID,
		map[string]string{"title": "Renamed"}, withCookie(sess))
	if status != 200 {
		t.Errorf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), "Renamed") {
		t.Errorf("title not present in response: %s", body)
	}
}

func TestPatchIndex_SetDefaultFilter(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	idx := insertTestIndex(t, st, user.ID)
	if err := st.SetCachedIndexItems(context.Background(), idx.ID, []byte("[]"), false, user.Login); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	defaultFilter := "readme"
	status, body := doJSON(t, srv, "PATCH", "/api/indexes/"+idx.ID,
		map[string]*string{"defaultFilter": &defaultFilter}, withCookie(sess))
	if status != 200 {
		t.Errorf("status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), "readme") {
		t.Errorf("defaultFilter not present in response: %s", body)
	}
}

func TestCreateIndex_RequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, _ := doJSON(t, srv, "POST", "/api/indexes",
		map[string]string{"url": "https://github.com/anthropics/claude-code"})
	if status != 401 {
		t.Errorf("status=%d, want 401", status)
	}
}

func TestCreateIndex_BadURL(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	status, _ := doJSON(t, srv, "POST", "/api/indexes",
		map[string]string{"url": "not a github url"}, withCookie(sess))
	// Either 400 (rejected at parse) or 502/400 (rejected at access probe).
	if status != 400 && status != 502 {
		t.Errorf("status=%d, want 400 or 502", status)
	}
}

func TestCreateIndex_RejectsInvalidJSON(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	sess := testutil.NewTestSession(t, st, user.ID)
	// raw string body — bypass marshalling so server sees malformed JSON.
	status, _ := doJSON(t, srv, "POST", "/api/indexes", "not-json", withCookie(sess))
	if status != 400 {
		t.Errorf("status=%d, want 400", status)
	}
}
