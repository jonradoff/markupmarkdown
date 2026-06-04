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
	os.Setenv("MARKUPMARKDOWN_ENV", testEnv)

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
// plus a cleanup that deletes every document in every collection so the
// next test starts from zero. The cleanup uses DeleteMany rather than
// Drop so indexes survive across packages running in parallel.
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

	st, err := store.New(cfg.Database.URI, cfg.Database.Name)
	if err != nil {
		t.Fatalf("testutil: connect: %v", err)
	}

	clearAll(t, st)

	cleanup := func() {
		clearAll(t, st)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = st.Close(ctx)
	}
	return st, cleanup
}

// clearAll deletes every document in every collection the suite uses.
// Adding a new collection means adding it here. The safety guard in
// LoadTestConfig means this can only run against a database whose name
// contains "test".
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
