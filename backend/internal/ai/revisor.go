// Package ai wraps the Anthropic API for AI-assisted markdown revision.
//
// The revisor takes an original markdown document plus a list of resolved
// comment threads, and asks Claude Opus 4.7 to produce a revision that
// incorporates the agreed feedback while changing as little of the source as
// possible. Streaming is used internally so very large documents don't hit
// HTTP-level timeouts.
package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Model is the Claude model used for revisions. Opus 4.7 is selected because
// document revision is intelligence-sensitive — we want literal interpretation
// of the comment threads and conservative edits to the surrounding prose.
const Model = "claude-opus-4-7"

// MaxOutputTokens caps the generated revision. 64K is enough for very large
// markdown documents while staying well under Opus 4.7's 128K ceiling.
const MaxOutputTokens = 64000

const systemPrompt = `You are revising a markdown document based on resolved comment threads from a Google-Docs-style review workflow.

YOUR GOAL: produce a revised version of the document that incorporates the agreed feedback while changing as little of the original as possible. Preserve the document's structure, voice, headings, formatting, links, images, code blocks, lists, tables, and any unchanged sections exactly as written.

For each resolved comment thread, you will see:
  - QUOTED: the exact portion of the original document the comment refers to
  - COMMENT: the original commenter's note
  - REPLIES: the reply thread (oldest first)
  - RESOLVED BY: who marked the thread done

The conclusion of the thread — typically the final reply, or the original comment if there are no replies — represents the agreed-upon change. Apply that change to the QUOTED portion (and only the minimum surrounding text needed to make it coherent).

RULES:
  1. Output ONLY the revised markdown content. No preamble, no commentary, no explanation, no code-fence wrapper.
  2. Do not add comments, asides, or "[edited]" markers.
  3. If a thread's conclusion is unclear or contradictory, apply the most conservative interpretation (the smallest change that addresses the concern). If still unclear, leave the QUOTED text untouched.
  4. Do not rewrite or "improve" anything the comments didn't ask you to touch.
  5. Do not introduce new headings, sections, or content the comments didn't request.
  6. Preserve trailing newlines and overall whitespace style.`

// ResolvedComment is what the revisor needs to know about each thread it
// should apply.
type ResolvedComment struct {
	Quoted     string
	Author     string
	Body       string
	Replies    []ResolvedReply
	ResolvedBy string
}

type ResolvedReply struct {
	Author string
	Body   string
}

// Result is what comes back from a successful revision.
type Result struct {
	Content   string
	Model     string
	TokensIn  int64
	TokensOut int64
}

// Error categories the API surfaces to the frontend as actionable messages.
type ErrorKind string

const (
	ErrKindInvalidKey    ErrorKind = "invalid_key"
	ErrKindRateLimited   ErrorKind = "rate_limited"
	ErrKindOverloaded    ErrorKind = "overloaded"
	ErrKindContextTooBig ErrorKind = "context_too_big"
	ErrKindTimeout       ErrorKind = "timeout"
	ErrKindRefusal       ErrorKind = "refusal"
	ErrKindEmpty         ErrorKind = "empty"
	ErrKindOther         ErrorKind = "other"
)

type RevisionError struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *RevisionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}
func (e *RevisionError) Unwrap() error { return e.Err }

// ValidateAPIKey makes a tiny no-op request to confirm the key works. Useful
// when accepting a key from the user — fail fast instead of letting the first
// real revision blow up.
func ValidateAPIKey(ctx context.Context, apiKey string) error {
	if !strings.HasPrefix(apiKey, "sk-ant-") {
		return &RevisionError{Kind: ErrKindInvalidKey, Message: "API key should start with sk-ant-"}
	}
	client := sdk.NewClient(option.WithAPIKey(apiKey))
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := client.Messages.New(ctx, sdk.MessageNewParams{
		Model:     Model,
		MaxTokens: 16,
		Messages: []sdk.MessageParam{
			sdk.NewUserMessage(sdk.NewTextBlock("hi")),
		},
	})
	if err != nil {
		return classifyError(err)
	}
	return nil
}

// OnDelta is invoked for each text chunk streamed from Anthropic. Returning
// a non-nil error aborts the stream — useful for surfacing client disconnects.
type OnDelta func(chunk string) error

// Revise generates a new version of the document. onDelta is optional; when
// supplied, each text chunk is forwarded as it arrives so callers can stream
// to the end user.
func Revise(
	ctx context.Context,
	apiKey, title, original string,
	comments []ResolvedComment,
	onDelta OnDelta,
) (*Result, error) {
	if apiKey == "" {
		return nil, &RevisionError{Kind: ErrKindInvalidKey, Message: "API key not configured"}
	}
	if strings.TrimSpace(original) == "" {
		return nil, &RevisionError{Kind: ErrKindEmpty, Message: "document is empty"}
	}
	if len(comments) == 0 {
		return nil, &RevisionError{Kind: ErrKindEmpty, Message: "no resolved comments to apply"}
	}

	client := sdk.NewClient(option.WithAPIKey(apiKey))
	userMessage := buildUserMessage(title, original, comments)

	// 10 minute ceiling. Opus 4.7 on a long doc with many comments can take
	// a while; streaming protects against per-chunk timeouts but we still
	// want a wall clock.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	stream := client.Messages.NewStreaming(ctx, sdk.MessageNewParams{
		Model:     Model,
		MaxTokens: MaxOutputTokens,
		System: []sdk.TextBlockParam{
			{
				Text: systemPrompt,
				// Cache the system prompt so subsequent revisions by the
				// same user are cheaper.
				CacheControl: sdk.CacheControlEphemeralParam{Type: "ephemeral"},
			},
		},
		Messages: []sdk.MessageParam{
			sdk.NewUserMessage(sdk.NewTextBlock(userMessage)),
		},
	})

	var out strings.Builder
	var tokensIn, tokensOut int64
	for stream.Next() {
		event := stream.Current()
		switch ev := event.AsAny().(type) {
		case sdk.ContentBlockDeltaEvent:
			if d, ok := ev.Delta.AsAny().(sdk.TextDelta); ok {
				out.WriteString(d.Text)
				if onDelta != nil {
					if err := onDelta(d.Text); err != nil {
						return nil, &RevisionError{Kind: ErrKindOther, Message: "client disconnected", Err: err}
					}
				}
			}
		case sdk.MessageDeltaEvent:
			if ev.Usage.OutputTokens > 0 {
				tokensOut = ev.Usage.OutputTokens
			}
		case sdk.MessageStartEvent:
			if ev.Message.Usage.InputTokens > 0 {
				tokensIn = ev.Message.Usage.InputTokens
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, classifyError(err)
	}

	revised := stripCodeFence(strings.TrimSpace(out.String()))
	if revised == "" {
		return nil, &RevisionError{Kind: ErrKindEmpty, Message: "Claude returned an empty revision"}
	}

	return &Result{
		Content:   revised,
		Model:     Model,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
	}, nil
}

func buildUserMessage(title, original string, comments []ResolvedComment) string {
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "Document title: %s\n\n", title)
	}
	b.WriteString("=== ORIGINAL MARKDOWN ===\n")
	b.WriteString(original)
	if !strings.HasSuffix(original, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("=== END ORIGINAL MARKDOWN ===\n\n")

	fmt.Fprintf(&b, "%d resolved comment thread(s) to apply:\n\n", len(comments))
	for i, c := range comments {
		fmt.Fprintf(&b, "[Thread %d]\n", i+1)
		fmt.Fprintf(&b, "QUOTED: %q\n", c.Quoted)
		fmt.Fprintf(&b, "COMMENT (by %s): %s\n", c.Author, c.Body)
		if len(c.Replies) > 0 {
			b.WriteString("REPLIES:\n")
			for _, r := range c.Replies {
				fmt.Fprintf(&b, "  - %s: %s\n", r.Author, r.Body)
			}
		}
		if c.ResolvedBy != "" {
			fmt.Fprintf(&b, "RESOLVED BY: %s\n", c.ResolvedBy)
		}
		b.WriteString("\n")
	}
	b.WriteString("Now produce the revised markdown.\n")
	return b.String()
}

// stripCodeFence trims a wrapping ```markdown ... ``` if the model returns one
// despite being told not to.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening line (```lang).
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	} else {
		return s
	}
	// Drop a trailing closing fence.
	s = strings.TrimRight(s, "\n")
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimRight(s, "\n")
	}
	return s
}

// classifyError maps SDK errors into RevisionError kinds the frontend renders
// with appropriate actions.
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &RevisionError{Kind: ErrKindTimeout, Message: "Claude took too long. Try again or split the document into smaller pieces.", Err: err}
	}
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		msg := apiErr.Error()
		switch apiErr.StatusCode {
		case 401:
			return &RevisionError{Kind: ErrKindInvalidKey, Message: "Anthropic rejected your API key. Replace it in your settings.", Err: err}
		case 403:
			return &RevisionError{Kind: ErrKindInvalidKey, Message: "Your API key doesn't have access to this model.", Err: err}
		case 400:
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "max_tokens") || strings.Contains(lower, "context") || strings.Contains(lower, "too long") || strings.Contains(lower, "too large") {
				return &RevisionError{Kind: ErrKindContextTooBig, Message: "The document + comments are too large for this model's context window. Split the doc and revise in pieces.", Err: err}
			}
			return &RevisionError{Kind: ErrKindOther, Message: "Anthropic rejected the request: " + msg, Err: err}
		case 429:
			return &RevisionError{Kind: ErrKindRateLimited, Message: "Your Anthropic account is rate-limited. Wait a minute and try again.", Err: err}
		case 529, 503:
			return &RevisionError{Kind: ErrKindOverloaded, Message: "Anthropic is temporarily overloaded. Retry in a few seconds.", Err: err}
		case 500, 502, 504:
			return &RevisionError{Kind: ErrKindOther, Message: "Anthropic had a server error. Retry shortly.", Err: err}
		}
	}
	return &RevisionError{Kind: ErrKindOther, Message: err.Error(), Err: err}
}
