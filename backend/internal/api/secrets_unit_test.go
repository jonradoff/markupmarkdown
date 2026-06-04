package api

import (
	"context"
	"strings"
	"testing"

	"markupmarkdown/internal/config"
	"markupmarkdown/internal/secrets"
)

func TestDecryptedAnthropicKey_ReturnsEmptyWhenNoVault(t *testing.T) {
	a := &API{cfg: &config.Config{}, vault: nil}
	got, err := a.decryptedAnthropicKey(context.Background(), "u")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "" {
		t.Errorf("got %q", got)
	}
}

func TestEstimateCostUSD_Opus(t *testing.T) {
	usd := estimateCostUSD("claude-opus-4-7", 1000, 1000)
	want := (1000*5.0 + 1000*25.0) / 1_000_000
	if usd != want {
		t.Errorf("got %v want %v", usd, want)
	}
}

func TestEstimateCostUSD_Sonnet(t *testing.T) {
	usd := estimateCostUSD("claude-sonnet-4-6", 1000, 1000)
	want := (1000*3.0 + 1000*15.0) / 1_000_000
	if usd != want {
		t.Errorf("got %v want %v", usd, want)
	}
}

func TestEstimateCostUSD_Haiku(t *testing.T) {
	usd := estimateCostUSD("claude-haiku-4-5", 1000, 1000)
	want := (1000*1.0 + 1000*5.0) / 1_000_000
	if usd != want {
		t.Errorf("got %v want %v", usd, want)
	}
}

func TestSanitizeStoreErr_RedactsAndCorrelates(t *testing.T) {
	err := sanitizeStoreErr("test.where", errMongo("internal mongo blip"))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	// The sanitized message must include an "id=" correlation token and
	// must NOT echo the internal "mongo blip" text.
	if !strings.Contains(msg, "id=") {
		t.Errorf("missing id correlation in %q", msg)
	}
	if strings.Contains(msg, "mongo blip") {
		t.Errorf("internal detail leaked in %q", msg)
	}
}

type errMongo string

func (e errMongo) Error() string { return string(e) }

// helper for vault construction we don't otherwise exercise.
func TestVaultNilHandled(t *testing.T) {
	if _, err := (*secrets.Vault)(nil).Encrypt("x"); err == nil {
		t.Fatal("nil vault Encrypt should err")
	}
}
