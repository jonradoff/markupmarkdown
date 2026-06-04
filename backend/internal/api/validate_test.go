package api

import (
	"strings"
	"testing"

	"markupmarkdown/internal/models"
)

func TestValidateCommentBody_Trims(t *testing.T) {
	body, err := ValidateCommentBody("  hello  ")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if body != "hello" {
		t.Fatalf("got %q", body)
	}
}

func TestValidateCommentBody_RejectsEmpty(t *testing.T) {
	if _, err := ValidateCommentBody("   "); err == nil {
		t.Fatal("expected rejection of whitespace-only body")
	}
}

func TestValidateCommentBody_RejectsTooLong(t *testing.T) {
	body := strings.Repeat("x", maxCommentBodyLen+1)
	if _, err := ValidateCommentBody(body); err == nil {
		t.Fatal("expected rejection of overlong body")
	}
}

func TestValidateReplyBody_AcceptsAtCap(t *testing.T) {
	body := strings.Repeat("y", maxReplyBodyLen)
	got, err := ValidateReplyBody(body)
	if err != nil {
		t.Fatalf("at cap should succeed: %v", err)
	}
	if got != body {
		t.Fatal("body altered")
	}
}

func TestValidateReplyBody_RejectsOverCap(t *testing.T) {
	body := strings.Repeat("y", maxReplyBodyLen+1)
	if _, err := ValidateReplyBody(body); err == nil {
		t.Fatal("over cap should fail")
	}
}

func TestValidateReplyBody_RejectsEmpty(t *testing.T) {
	if _, err := ValidateReplyBody(""); err == nil {
		t.Fatal("empty should fail")
	}
}

func TestValidateAnchor_HappyPath(t *testing.T) {
	a := models.Anchor{Start: 0, End: 5, Exact: "hello"}
	if err := ValidateAnchor(a); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestValidateAnchor_InvalidRange(t *testing.T) {
	if err := ValidateAnchor(models.Anchor{Start: 10, End: 5, Exact: "x"}); err == nil {
		t.Fatal("end <= start should fail")
	}
	if err := ValidateAnchor(models.Anchor{Start: 5, End: 5, Exact: "x"}); err == nil {
		t.Fatal("end == start should fail")
	}
}

func TestValidateAnchor_EmptyExact(t *testing.T) {
	if err := ValidateAnchor(models.Anchor{Start: 0, End: 5, Exact: "   "}); err == nil {
		t.Fatal("whitespace exact should fail")
	}
}

func TestValidateAnchor_TooLongExact(t *testing.T) {
	a := models.Anchor{Start: 0, End: maxAnchorExactLen + 1, Exact: strings.Repeat("a", maxAnchorExactLen+1)}
	if err := ValidateAnchor(a); err == nil {
		t.Fatal("overlong exact should fail")
	}
}
