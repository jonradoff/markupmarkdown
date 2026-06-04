package httperr

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestErrorID_NonEmptyAndUnique(t *testing.T) {
	a := ErrorID()
	b := ErrorID()
	if a == "" || b == "" {
		t.Fatal("empty ID returned")
	}
	if a == b {
		t.Fatal("IDs should not collide (12 hex chars from crypto/rand)")
	}
	if len(a) != 12 {
		t.Fatalf("ID length %d, want 12", len(a))
	}
}

func TestLog_ReturnsIDAndUserMessage(t *testing.T) {
	id, msg := Log("test.where", errors.New("internal detail"))
	if id == "" {
		t.Fatal("Log returned empty ID")
	}
	if !strings.Contains(msg, id) {
		t.Errorf("user message %q should embed the ID %q so support can correlate", msg, id)
	}
	if strings.Contains(msg, "internal detail") {
		t.Errorf("user message should NOT echo the internal error: %q", msg)
	}
}

func TestLog_WritesFullErrorToServerLog(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(originalOutput) })

	id, _ := Log("test.boundary", errors.New("mongo blip 42"))
	out := buf.String()
	if !strings.Contains(out, id) {
		t.Errorf("server log should include the ID for correlation; got %q", out)
	}
	if !strings.Contains(out, "mongo blip 42") {
		t.Errorf("server log should include the full error for ops triage; got %q", out)
	}
	if !strings.Contains(out, "test.boundary") {
		t.Errorf("server log should include the location/where; got %q", out)
	}
}
