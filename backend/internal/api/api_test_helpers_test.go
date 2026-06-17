package api_test

// Shared HTTP helpers for the api package integration tests. Kept in the
// _test package so they don't pollute the real api surface.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/store"
	"markupmarkdown/internal/testutil"
)

// newTestServer returns the package-level shared httptest server +
// store + API, after resetting the DB to an empty state for this test.
// The shared resources are set up ONCE in test_main_test.go's TestMain
// — pre-refactor, every call was paying the full ~6s Atlas connection
// setup, which is what made the integration suite blow past the 600s
// timeout. Now each test pays ~50ms for the ResetDB call.
//
// If the shared setup was skipped (MONGODB_URI unset, free-tier CI
// without the secret), this skips the calling test cleanly.
func newTestServer(t *testing.T) (*httptest.Server, *store.Store, *api.API) {
	t.Helper()
	if sharedSkipped || sharedStore == nil {
		t.Skip("testutil: MONGODB_URI not set; integration test skipped")
	}
	testutil.ResetDB(t, sharedStore)
	return sharedServer, sharedStore, sharedAPI
}

// doJSON performs an HTTP call against srv and decodes the response.
// `out` may be nil to ignore the body. Returns the status code.
func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any, mods ...func(*http.Request)) (int, []byte) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, srv.URL+path, reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mods {
		if m == nil {
			continue
		}
		m(req)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)
	return res.StatusCode, respBody
}

// withCookie returns a request modifier that attaches the given session
// cookie.
func withCookie(sessionID string) func(*http.Request) {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "mm_session", Value: sessionID, Path: "/"})
	}
}

// withBearer attaches an Authorization: Bearer header.
func withBearer(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	}
}

func mustDecode(t *testing.T, body []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, string(body))
	}
}
