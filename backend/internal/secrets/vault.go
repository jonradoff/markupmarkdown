// Package secrets implements AES-256-GCM encryption for at-rest storage of
// per-user credentials (notably the user's Anthropic API key).
//
// Design choices, in plain terms for technical users:
//   - Algorithm is AES-256-GCM with a random 12-byte nonce per encryption.
//   - The 32-byte master key is held in the env var
//     MARKUPMARKDOWN_ENCRYPTION_KEY (hex-encoded). It is NEVER stored in
//     MongoDB and never sent over the wire. On Fly.io it lives in `fly secrets`.
//   - Encrypted blobs are base64-encoded and stored next to a short
//     plaintext "hint" (last 4 characters of the original key) so users can
//     recognize which key they have on file without us ever exposing the rest.
//   - Without the master key, the ciphertext is useless.
//
// If the master key is not set, the AI features that depend on per-user
// secrets are disabled with a clear error — the rest of the app keeps working.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// keyVersion is the "active" version stamp. Increment when introducing a
// new key. Old key versions are accepted on Decrypt as long as their hex is
// supplied in env.
const keyVersion = "v1"

// keyEnvFmt is the env var name for key version N's hex value.
//   v1 -> MARKUPMARKDOWN_ENCRYPTION_KEY (legacy, no suffix)
//   v2 -> MARKUPMARKDOWN_ENCRYPTION_KEY_V2
// (we keep v1 unsuffixed so existing deployments keep working.)
type aeadEntry struct {
	version string
	aead    cipher.AEAD
}

type Vault struct {
	active aeadEntry
	old    []aeadEntry // accepted on decrypt, never on encrypt
}

// NewVault constructs a Vault. `primary` is the v1 (current) hex-encoded
// 32-byte key. `additional` maps version IDs ("v2", "v3", …) to their hex
// values — these are accepted on decrypt for key rotation.
// Returns (nil, nil) if primary is empty, so callers can disable
// secret-dependent features gracefully.
func NewVault(primary string, additional map[string]string) (*Vault, error) {
	if primary == "" {
		return nil, nil
	}
	active, err := buildAEAD(primary)
	if err != nil {
		return nil, err
	}
	v := &Vault{active: aeadEntry{version: keyVersion, aead: active}}
	for ver, hexKey := range additional {
		a, err := buildAEAD(hexKey)
		if err != nil {
			return nil, fmt.Errorf("additional key %s: %w", ver, err)
		}
		v.old = append(v.old, aeadEntry{version: ver, aead: a})
	}
	return v, nil
}

func buildAEAD(keyHex string) (cipher.AEAD, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("encryption key must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes (got %d) — generate with `openssl rand -hex 32`", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt prefixes the ciphertext with `<version>:` so future rotations can
// pick the right key on Decrypt.
func (v *Vault) Encrypt(plain string) (string, error) {
	if v == nil {
		return "", errors.New("vault not configured")
	}
	nonce := make([]byte, v.active.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := v.active.aead.Seal(nonce, nonce, []byte(plain), nil)
	return v.active.version + ":" + base64.RawStdEncoding.EncodeToString(ct), nil
}

// Decrypt accepts either prefixed (`v1:<b64>`) or unprefixed (legacy) blobs.
func (v *Vault) Decrypt(encoded string) (string, error) {
	if v == nil {
		return "", errors.New("vault not configured")
	}
	version, body := keyVersion, encoded
	if i := strings.IndexByte(encoded, ':'); i > 0 && i <= 8 {
		version = encoded[:i]
		body = encoded[i+1:]
	}
	aead := v.aeadFor(version)
	if aead == nil {
		return "", fmt.Errorf("no key for ciphertext version %q", version)
	}
	ct, err := base64.RawStdEncoding.DecodeString(body)
	if err != nil {
		return "", err
	}
	ns := aead.NonceSize()
	if len(ct) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, payload := ct[:ns], ct[ns:]
	plain, err := aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (v *Vault) aeadFor(version string) cipher.AEAD {
	if version == v.active.version {
		return v.active.aead
	}
	for _, e := range v.old {
		if e.version == version {
			return e.aead
		}
	}
	return nil
}

// Hint returns the human-readable preview of an API key: enough characters at
// the start and end to be recognizable, with the middle redacted.
// For an Anthropic key like "sk-ant-api03-XXXX...YYYY", this yields
// "sk-ant-api…YYYY".
func Hint(key string) string {
	if len(key) <= 8 {
		return "…"
	}
	prefix := key[:10]
	if len(key) < 20 {
		prefix = key[:len(key)/2]
	}
	return prefix + "…" + key[len(key)-4:]
}
