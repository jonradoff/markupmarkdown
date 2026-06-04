package config

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Database   DatabaseConfig   `yaml:"database"`
	Frontend   FrontendConfig   `yaml:"frontend"`
	Fetch      FetchConfig      `yaml:"fetch"`
	GitHub     GitHubConfig     `yaml:"github"`
	Encryption EncryptionConfig `yaml:"encryption"`
}

type EncryptionConfig struct {
	// MasterKey is a hex-encoded 32-byte AES-256 key. Used to encrypt
	// per-user secrets (currently just the Anthropic API key) at rest.
	MasterKey string `yaml:"master_key"`
	// AdditionalKeys hold prior keys accepted on Decrypt only — see
	// secrets.Vault key-rotation docs. Keys are picked up from any env var
	// named MARKUPMARKDOWN_ENCRYPTION_KEY_V<N>; we wire them in main.go.
	AdditionalKeys map[string]string `yaml:"-"`
}

type GitHubConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	CallbackURL  string `yaml:"callback_url"`
	Scope        string `yaml:"scope"`
}

func (g GitHubConfig) Enabled() bool {
	return g.ClientID != "" && g.ClientSecret != ""
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port string `yaml:"port"`
}

type DatabaseConfig struct {
	URI  string `yaml:"uri"`
	Name string `yaml:"name"`
}

type FrontendConfig struct {
	URL       string `yaml:"url"`
	StaticDir string `yaml:"static_dir"`
}

type FetchConfig struct {
	Timeout  string `yaml:"timeout"`
	MaxBytes int64  `yaml:"max_bytes"`
}

func (f FetchConfig) ParseTimeout() time.Duration {
	if d, err := time.ParseDuration(f.Timeout); err == nil {
		return d
	}
	return 15 * time.Second
}

// LoadEnvFile reads KEY=VALUE pairs from a .env file (if present) and applies
// them to the process environment. Existing env vars are not overridden.
func LoadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
}

var envPattern = regexp.MustCompile(`\$\{([A-Z0-9_]+)(?::([^}]*))?\}`)

func expandEnv(raw []byte) []byte {
	return envPattern.ReplaceAllFunc(raw, func(match []byte) []byte {
		m := envPattern.FindSubmatch(match)
		if len(m) < 2 {
			return match
		}
		key := string(m[1])
		def := ""
		if len(m) >= 3 {
			def = string(m[2])
		}
		if v, ok := os.LookupEnv(key); ok {
			return []byte(v)
		}
		return []byte(def)
	})
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	expanded := expandEnv(raw)

	var cfg Config
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Fetch.MaxBytes == 0 {
		if v := os.Getenv("FETCH_MAX_BYTES"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				cfg.Fetch.MaxBytes = n
			}
		}
		if cfg.Fetch.MaxBytes == 0 {
			cfg.Fetch.MaxBytes = 5 * 1024 * 1024
		}
	}
	return &cfg, nil
}
