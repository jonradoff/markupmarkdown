// Package testutil provides shared helpers for the markupmarkdown test
// suite. The single most important guarantee here is the database safety
// guard: every test path refuses to run if the resolved DB name does not
// contain "test", so it cannot accidentally touch prod or dev data.
//
// Pattern adapted from the LastSaaS test infrastructure — same shape so a
// reviewer familiar with one project can read the other.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/config"
	"markupmarkdown/internal/secrets"
	"markupmarkdown/internal/store"
)

const testEnv = "test"

// loadEnvTest walks up from the current working directory looking for a
// .env.test file and applies its KEY=VAL pairs into the process env. Used
// by both *_test.go entry points and TestMain.
func loadEnvTest() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 6; i++ {
		envPath := filepath.Join(dir, ".env.test")
		if _, err := os.Stat(envPath); err == nil {
			config.LoadEnvFile(envPath)
			return
		}
		// also try one level up's backend/.env.test
		alt := filepath.Join(dir, "backend", ".env.test")
		if _, err := os.Stat(alt); err == nil {
			config.LoadEnvFile(alt)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}

// findConfigPath returns an absolute path to backend/config/test.yaml,
// walking up the directory tree from CWD. Tests run from
// internal/<pkg>/ so we need to climb a few levels.
func findConfigPath() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 6; i++ {
		for _, candidate := range []string{
			filepath.Join(dir, "config", "test.yaml"),
			filepath.Join(dir, "backend", "config", "test.yaml"),
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// LoadTestConfig returns the test Config, loading .env.test on the way.
// It enforces the DB-name-must-contain-"test" safety guard before
// returning, so any later code that uses cfg.Database.Name is guaranteed
// to be pointing at a test database.
func LoadTestConfig(t *testing.T) *config.Config {
	t.Helper()
	loadEnvTest()
	_ = os.Setenv("MARKUPMARKDOWN_ENV", testEnv)

	path := findConfigPath()
	if path == "" {
		t.Fatalf("testutil: could not find backend/config/test.yaml")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("testutil: load test config: %v", err)
	}

	enforceTestDBName(t, cfg.Database.Name)
	return cfg
}

// enforceTestDBName fails the test if name does not contain "test". This
// is the single guard between the suite and a production database — keep
// it, and never weaken the check.
func enforceTestDBName(t *testing.T, name string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("testutil: REFUSING to run tests — database name %q does not contain 'test'. "+
			"This safety guard prevents accidental use of production databases.", name)
	}
}

// MustConnectTestDB connects to the test database and returns the store
// plus a cleanup that fully drops the database so the next test run
// starts from zero — including indexes, not just documents.
//
// Each invocation appends a unique suffix to the configured DB name
// (e.g. `markupmarkdown-test-7a3f`) so parallel CI runs, multiple
// `go test ./...` invocations on the same machine, and concurrent
// `make test` shells never collide on the same `markupmarkdown-test`
// rows. The base name still has to contain "test" — the safety guard
// is unchanged, and the suffix is also marked with "test" so the
// resulting name still matches.
//
// If MONGODB_URI is unset (e.g. CI without secrets), the test is skipped
// rather than failed — local development should configure .env.test, and
// CI should set the secret.
func MustConnectTestDB(t *testing.T) (*store.Store, func()) {
	t.Helper()
	cfg := LoadTestConfig(t)
	if cfg.Database.URI == "" {
		t.Skip("testutil: MONGODB_URI not set; skipping (configure backend/.env.test or set the env var)")
	}

	// Per-process DB suffix so concurrent test runs don't step on
	// each other. We use crypto/rand because some packages spin up
	// MustConnectTestDB inside parallel subtests and a math/rand
	// global would race them all into the same suffix.
	suffix := randomTestSuffix()
	dbName := cfg.Database.Name + "-" + suffix
	enforceTestDBName(t, dbName)

	st, err := store.New(cfg.Database.URI, dbName)
	if err != nil {
		t.Fatalf("testutil: connect: %v", err)
	}

	clearAll(t, st)

	cleanup := func() {
		// Drop the whole per-run DB so nothing leaks between runs and
		// stale rows can't accumulate in Atlas. Falls back to clearAll
		// if the drop fails (e.g. transient Atlas hiccup) so the next
		// run at least starts from empty collections.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := st.DropDatabase(ctx); err != nil {
			t.Logf("testutil: drop %s: %v (falling back to clearAll)", dbName, err)
			clearAll(t, st)
		}
		_ = st.Close(ctx)
	}
	return st, cleanup
}

// randomTestSuffix returns a short hex token guaranteed to keep the
// resulting DB name matching the enforceTestDBName guard. Prefixed
// with "test" so a future reader scanning Atlas can immediately tell
// the row is ephemeral.
func randomTestSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to a time-based suffix if crypto/rand fails (it
		// shouldn't, but we don't want test setup to panic on a flake).
		return fmt.Sprintf("test%d", time.Now().UnixNano()%1_000_000)
	}
	return "test" + hex.EncodeToString(b[:])
}

// clearAll deletes every document in every collection the suite uses.
// Adding a new collection means adding it here. The safety guard in
// LoadTestConfig means this can only run against a database whose name
// contains "test".
//
// Used as the fallback inside cleanup when DropDatabase fails, and
// also directly by ResetDB between sub-tests within a single test
// process.
func clearAll(t *testing.T, st *store.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, coll := range []*mongo.Collection{
		st.Documents(),
		st.Comments(),
		st.Users(),
		st.Sessions(),
		st.AuthStates(),
		st.UserSecrets(),
		st.DocumentViews(),
		st.Notifications(),
		st.APITokens(),
		st.TokenEvents(),
		// Collections added since the original 80%-coverage snapshot.
		// Forgetting one of these is exactly how the index-dedup
		// flakiness happened — keep this list in sync with store.go's
		// collection accessors.
		st.Indexes(),
		st.IndexItems(),
		st.HiddenItems(),
	} {
		if _, err := coll.DeleteMany(ctx, bson.M{}); err != nil {
			t.Logf("testutil: clear %s: %v", coll.Name(), err)
		}
	}
}

// ResetDB clears all known collections without closing the store. Useful
// between sub-tests when the same store is shared by an entire package.
func ResetDB(t *testing.T, st *store.Store) {
	t.Helper()
	clearAll(t, st)
}

// MustAPI constructs an *api.API wired to the test store and the test
// config. Use this when you want to exercise full HTTP handlers.
func MustAPI(t *testing.T, st *store.Store) *api.API {
	t.Helper()
	cfg := LoadTestConfig(t)
	a, err := api.New(cfg, st)
	if err != nil {
		t.Fatalf("testutil: api.New: %v", err)
	}
	return a
}

// NewTestVault constructs a secrets.Vault using the given hex master key.
// Tests use this to manually encrypt a plaintext that the API can later
// decrypt — usually for seeding an Anthropic key.
func NewTestVault(masterKey string) (interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}, error) {
	return secrets.NewVault(masterKey, nil)
}

// TestEncryptionKey is a deterministic 32-byte hex key suitable for
// secrets.Vault tests. Never use this outside tests.
const TestEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

// LogfMain mirrors LastSaaS's pattern for TestMain-style setup logs so
// failed CI runs are easier to diagnose.
func LogfMain(format string, args ...any) {
	log.Printf("[testutil] "+format, args...)
}
