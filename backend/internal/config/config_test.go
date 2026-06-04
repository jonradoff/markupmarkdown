package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHubConfig_Enabled(t *testing.T) {
	cases := []struct {
		g    GitHubConfig
		want bool
	}{
		{GitHubConfig{}, false},
		{GitHubConfig{ClientID: "x"}, false},
		{GitHubConfig{ClientSecret: "y"}, false},
		{GitHubConfig{ClientID: "x", ClientSecret: "y"}, true},
	}
	for _, c := range cases {
		if got := c.g.Enabled(); got != c.want {
			t.Errorf("%+v.Enabled()=%v, want %v", c.g, got, c.want)
		}
	}
}

func TestFetchConfig_ParseTimeout(t *testing.T) {
	if got := (FetchConfig{Timeout: "3s"}).ParseTimeout(); got != 3*time.Second {
		t.Errorf("got %v", got)
	}
	// Invalid → default 15s.
	if got := (FetchConfig{Timeout: "nonsense"}).ParseTimeout(); got != 15*time.Second {
		t.Errorf("invalid timeout fallback wrong: %v", got)
	}
	// Empty → default 15s.
	if got := (FetchConfig{}).ParseTimeout(); got != 15*time.Second {
		t.Errorf("empty timeout fallback wrong: %v", got)
	}
}

func TestExpandEnv_Substitutes(t *testing.T) {
	t.Setenv("FOO", "bar")
	out := expandEnv([]byte("${FOO}"))
	if string(out) != "bar" {
		t.Errorf("got %q", out)
	}
}

func TestExpandEnv_UsesDefault(t *testing.T) {
	// Make sure the env var is NOT set.
	os.Unsetenv("UNSET_NAME")
	out := expandEnv([]byte("${UNSET_NAME:fallback}"))
	if string(out) != "fallback" {
		t.Errorf("got %q", out)
	}
}

func TestExpandEnv_EmptyDefault(t *testing.T) {
	os.Unsetenv("ALSO_UNSET")
	out := expandEnv([]byte("[${ALSO_UNSET:}]"))
	if string(out) != "[]" {
		t.Errorf("got %q", out)
	}
}

func TestLoadEnvFile_PopulatesEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	const body = "FOO_A=apple\nFOO_B=\"banana\"\n# comment line\n\nFOO_C='cherry'\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// Clear vars so LoadEnvFile picks them up.
	os.Unsetenv("FOO_A")
	os.Unsetenv("FOO_B")
	os.Unsetenv("FOO_C")
	t.Cleanup(func() {
		os.Unsetenv("FOO_A")
		os.Unsetenv("FOO_B")
		os.Unsetenv("FOO_C")
	})

	LoadEnvFile(path)
	if got := os.Getenv("FOO_A"); got != "apple" {
		t.Errorf("FOO_A=%q", got)
	}
	if got := os.Getenv("FOO_B"); got != "banana" {
		t.Errorf("FOO_B=%q (quotes should strip)", got)
	}
	if got := os.Getenv("FOO_C"); got != "cherry" {
		t.Errorf("FOO_C=%q (single quotes should strip)", got)
	}
}

func TestLoadEnvFile_DoesNotOverrideExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("EXISTING=new\n"), 0o600)

	t.Setenv("EXISTING", "preset")
	LoadEnvFile(path)
	if got := os.Getenv("EXISTING"); got != "preset" {
		t.Errorf("LoadEnvFile should not override; got %q", got)
	}
}

func TestLoadEnvFile_MissingFile(t *testing.T) {
	// Should NOT panic when path does not exist.
	LoadEnvFile(filepath.Join(t.TempDir(), "no-such-file"))
}

func TestLoad_Happy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	yaml := `server:
  host: localhost
  port: "4321"
database:
  uri: mongodb://x
  name: mydb-test
frontend:
  url: http://localhost:4000
fetch:
  timeout: 7s
  max_bytes: 1024
encryption:
  master_key: ${MK:default-master-key}
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("MK")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Host != "localhost" || cfg.Server.Port != "4321" {
		t.Errorf("server: %+v", cfg.Server)
	}
	if cfg.Database.Name != "mydb-test" {
		t.Errorf("db: %+v", cfg.Database)
	}
	if cfg.Fetch.MaxBytes != 1024 {
		t.Errorf("fetch.max_bytes: %d", cfg.Fetch.MaxBytes)
	}
	if cfg.Encryption.MasterKey != "default-master-key" {
		t.Errorf("env default not applied: %q", cfg.Encryption.MasterKey)
	}
}

func TestLoad_MaxBytesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	// Omit fetch.max_bytes → should default to 5 MB.
	yaml := `server: {host: x, port: "1"}
database: {uri: m, name: n-test}
frontend: {url: u}
fetch: {timeout: 1s}
`
	_ = os.WriteFile(path, []byte(yaml), 0o600)
	os.Unsetenv("FETCH_MAX_BYTES")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Fetch.MaxBytes != 5*1024*1024 {
		t.Errorf("default max_bytes: %d", cfg.Fetch.MaxBytes)
	}
}

func TestLoad_MaxBytesFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	yaml := `server: {host: x, port: "1"}
database: {uri: m, name: n-test}
frontend: {url: u}
fetch: {timeout: 1s}
`
	_ = os.WriteFile(path, []byte(yaml), 0o600)
	t.Setenv("FETCH_MAX_BYTES", "2048")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Fetch.MaxBytes != 2048 {
		t.Errorf("env-supplied max_bytes lost: %d", cfg.Fetch.MaxBytes)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(path, []byte("not: [valid"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("expected YAML parse error")
	}
	// Sanity: the error mentions parse so consumers know what's wrong.
	if _, err := Load(path); err != nil && !strings.Contains(err.Error(), "parse") {
		t.Errorf("err message: %v", err)
	}
}
