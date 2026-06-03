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
)

type Vault struct {
	aead cipher.AEAD
}

// NewVault constructs a Vault from a hex-encoded 32-byte master key.
// Returns (nil, nil) if the key is empty, so callers can decide whether to
// disable secret-dependent features.
func NewVault(keyHex string) (*Vault, error) {
	if keyHex == "" {
		return nil, nil
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("encryption key must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes (got %d) — generate one with `openssl rand -hex 32`", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

// Encrypt returns the base64-encoded ciphertext of plain.
func (v *Vault) Encrypt(plain string) (string, error) {
	if v == nil {
		return "", errors.New("vault not configured")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := v.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt.
func (v *Vault) Decrypt(encoded string) (string, error) {
	if v == nil {
		return "", errors.New("vault not configured")
	}
	ct, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	ns := v.aead.NonceSize()
	if len(ct) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, body := ct[:ns], ct[ns:]
	plain, err := v.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
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
