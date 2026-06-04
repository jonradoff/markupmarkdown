package api

import (
	"testing"

	"markupmarkdown/internal/ai"
)

func TestRevisionErrorPayload_InvalidKeyIncludesAction(t *testing.T) {
	resp := (&API{}).revisionErrorPayload(&ai.RevisionError{
		Kind:    ai.ErrKindInvalidKey,
		Message: "bad key",
	})
	if resp.Error != "bad key" {
		t.Errorf("Error = %q", resp.Error)
	}
	if resp.Kind != "ai_invalid_key" {
		t.Errorf("Kind = %q", resp.Kind)
	}
	if len(resp.Actions) == 0 {
		t.Error("invalid_key should attach a 'get a key' action")
	}
}

func TestRevisionErrorPayload_OtherHasNoAction(t *testing.T) {
	resp := (&API{}).revisionErrorPayload(&ai.RevisionError{
		Kind:    ai.ErrKindOverloaded,
		Message: "try again",
	})
	if len(resp.Actions) != 0 {
		t.Error("non-key kinds should not attach an action")
	}
	if resp.Kind != "ai_overloaded" {
		t.Errorf("Kind = %q", resp.Kind)
	}
}
