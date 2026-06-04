package secrets

import (
	"strings"
	"testing"
)

// validKey32 is a deterministic 32-byte hex key. Tests-only.
const validKey32 = "0000000000000000000000000000000000000000000000000000000000000000"

func TestNewVault_EmptyPrimaryReturnsNilNil(t *testing.T) {
	v, err := NewVault("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Fatal("want nil vault when primary is empty")
	}
}

func TestNewVault_InvalidHexFails(t *testing.T) {
	if _, err := NewVault("zz", nil); err == nil {
		t.Fatal("expected error for non-hex key")
	}
}

func TestNewVault_WrongLengthFails(t *testing.T) {
	if _, err := NewVault("aabb", nil); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestNewVault_AdditionalKeyInvalidFails(t *testing.T) {
	_, err := NewVault(validKey32, map[string]string{"v2": "not-hex"})
	if err == nil {
		t.Fatal("expected error for invalid additional key")
	}
}

func TestVault_EncryptDecrypt_Roundtrip(t *testing.T) {
	v, err := NewVault(validKey32, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	const plain = "sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	ct, err := v.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(ct, "v1:") {
		t.Fatalf("ciphertext missing v1 prefix: %q", ct)
	}
	got, err := v.Decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("got %q, want %q", got, plain)
	}
}

func TestVault_NonceUnique(t *testing.T) {
	v, _ := NewVault(validKey32, nil)
	a, _ := v.Encrypt("hello")
	b, _ := v.Encrypt("hello")
	if a == b {
		t.Fatal("ciphertexts should differ due to random nonce")
	}
}

func TestVault_DecryptLegacyUnprefixed(t *testing.T) {
	v, _ := NewVault(validKey32, nil)
	ct, _ := v.Encrypt("legacy")
	body := strings.TrimPrefix(ct, "v1:")
	got, err := v.Decrypt(body)
	if err != nil {
		t.Fatalf("decrypt unprefixed: %v", err)
	}
	if got != "legacy" {
		t.Fatalf("got %q", got)
	}
}

func TestVault_DecryptUnknownVersionFails(t *testing.T) {
	v, _ := NewVault(validKey32, nil)
	if _, err := v.Decrypt("v99:abc"); err == nil {
		t.Fatal("expected error for unknown version")
	}
}

func TestVault_DecryptCorruptCiphertextFails(t *testing.T) {
	v, _ := NewVault(validKey32, nil)
	if _, err := v.Decrypt("v1:not-base64-!!!"); err == nil {
		t.Fatal("expected error for corrupt base64")
	}
}

func TestVault_DecryptTooShortFails(t *testing.T) {
	v, _ := NewVault(validKey32, nil)
	if _, err := v.Decrypt("v1:aGk"); err == nil {
		t.Fatal("expected error for ciphertext shorter than nonce")
	}
}

func TestVault_DecryptTamperedFails(t *testing.T) {
	v, _ := NewVault(validKey32, nil)
	ct, _ := v.Encrypt("hello")
	// Flip one character in the base64 portion
	colon := strings.Index(ct, ":")
	tampered := ct[:colon+1] + flipFirstChar(ct[colon+1:])
	if _, err := v.Decrypt(tampered); err == nil {
		t.Fatal("expected GCM auth failure on tampered ciphertext")
	}
}

func flipFirstChar(s string) string {
	if s == "" {
		return s
	}
	c := s[0]
	if c == 'A' {
		c = 'B'
	} else {
		c = 'A'
	}
	return string(c) + s[1:]
}

func TestVault_AdditionalKey_AcceptedOnDecrypt(t *testing.T) {
	// The additional-keys map indexes by an explicit version label so old
	// ciphertexts using a prior key can still be opened. We simulate this
	// by manually crafting a v2-prefixed ciphertext from a separate vault.
	const v2Key = "1111111111111111111111111111111111111111111111111111111111111111"
	v2Only, err := NewVault(v2Key, nil)
	if err != nil {
		t.Fatalf("init v2 vault: %v", err)
	}
	v1Prefixed, _ := v2Only.Encrypt("rotated-secret")
	// Re-stamp prefix as v2 so the rotated vault picks the additional key.
	body := v1Prefixed[len("v1:"):]
	v2Prefixed := "v2:" + body

	rotated, err := NewVault(validKey32, map[string]string{"v2": v2Key})
	if err != nil {
		t.Fatalf("new rotated vault: %v", err)
	}
	got, err := rotated.Decrypt(v2Prefixed)
	if err != nil {
		t.Fatalf("decrypt with rotated vault: %v", err)
	}
	if got != "rotated-secret" {
		t.Fatalf("got %q", got)
	}
}

func TestVault_NilReceiverFails(t *testing.T) {
	var v *Vault
	if _, err := v.Encrypt("x"); err == nil {
		t.Fatal("nil receiver should fail")
	}
	if _, err := v.Decrypt("x"); err == nil {
		t.Fatal("nil receiver should fail")
	}
}

func TestHint(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "…"},
		{"short", "…"},
		// Length 11 (<20) → prefix is first half (5 chars).
		{"sk-ant-XYZW", "sk-an…XYZW"},
		// Length ≥20 → prefix is first 10 chars.
		{"sk-ant-api03-1234567890ABCDEF", "sk-ant-api…CDEF"},
	}
	for _, c := range cases {
		if got := Hint(c.in); got != c.want {
			t.Errorf("Hint(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}
