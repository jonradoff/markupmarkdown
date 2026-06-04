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

	"github.com/gorilla/mux"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/store"
	"markupmarkdown/internal/testutil"
)

// newTestServer wires the API into an httptest server and returns it
// along with a cleanup that closes the server + drops the test DB.
func newTestServer(t *testing.T) (*httptest.Server, *store.Store, *api.API) {
	t.Helper()
	st, cleanup := testutil.MustConnectTestDB(t)

	a := testutil.MustAPI(t, st)
	r := mux.NewRouter()
	a.Register(r)
	srv := httptest.NewServer(r)

	t.Cleanup(func() {
		srv.Close()
		cleanup()
	})
	return srv, st, a
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
