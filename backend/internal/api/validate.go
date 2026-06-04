package api

import (
	"errors"
	"strings"

	"markupmarkdown/internal/models"
)

// ValidateCommentBody enforces the same trim + length rules whether the
// comment arrived from REST or from MCP. Returns a friendly error suitable
// for surfacing to the caller; never reveals server internals.
func ValidateCommentBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", errors.New("body is required")
	}
	if len(body) > maxCommentBodyLen {
		return "", errors.New("comment body too long")
	}
	return body, nil
}

// ValidateReplyBody mirrors ValidateCommentBody for replies.
func ValidateReplyBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", errors.New("body is required")
	}
	if len(body) > maxReplyBodyLen {
		return "", errors.New("reply body too long")
	}
	return body, nil
}

// ValidateAnchor checks the offsets and exact-text length. Same rules for
// REST (Anchor object on the wire) and MCP (anchor computed from quoted_text).
func ValidateAnchor(a models.Anchor) error {
	if a.End <= a.Start {
		return errors.New("invalid anchor range")
	}
	if strings.TrimSpace(a.Exact) == "" {
		return errors.New("anchor.exact is required")
	}
	if len(a.Exact) > maxAnchorExactLen {
		return errors.New("anchor.exact too long")
	}
	return nil
}
