package api_test

// Package-level shared store + API + httptest server so the integration
// suite pays the Atlas connection setup ~once instead of once per test
// (which was ~6s × 100+ tests = the 600s timeout we kept blowing). Each
// test still gets a clean DB via ResetDB at newTestServer entry.

import (
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/store"
	"markupmarkdown/internal/testutil"
)

var (
	sharedStore   *store.Store
	sharedAPI     *api.API
	sharedServer  *httptest.Server
	sharedSkipped bool
)

func TestMain(m *testing.M) {
	st, cleanup, err := testutil.ConnectShared()
	if err != nil {
		// Real configuration failure (refusing to use a prod DB name,
		// etc.) — surface it loudly. We've been bitten by silent skips
		// before.
		_, _ = os.Stderr.WriteString("testutil: ConnectShared failed: " + err.Error() + "\n")
		os.Exit(1)
	}
	if st == nil {
		// MONGODB_URI not set — run only the unit tests; integration
		// tests skip themselves at newTestServer entry.
		sharedSkipped = true
		os.Exit(m.Run())
	}
	sharedStore = st
	a, aerr := testutil.MustAPIFromStore(st)
	if aerr != nil {
		_, _ = os.Stderr.WriteString("testutil: MustAPI: " + aerr.Error() + "\n")
		cleanup()
		os.Exit(1)
	}
	sharedAPI = a
	r := mux.NewRouter()
	sharedAPI.Register(r)
	sharedServer = httptest.NewServer(r)
	code := m.Run()
	sharedServer.Close()
	cleanup()
	os.Exit(code)
}
