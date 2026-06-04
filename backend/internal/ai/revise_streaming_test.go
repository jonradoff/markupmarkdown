package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

type ssertRT struct {
	handler func(*http.Request) *http.Response
}

func (m *ssertRT) RoundTrip(r *http.Request) (*http.Response, error) {
	return m.handler(r), nil
}

func swapDefault(h func(*http.Request) *http.Response) func() {
	prev := http.DefaultTransport
	prevC := http.DefaultClient.Transport
	rt := &ssertRT{handler: h}
	http.DefaultTransport = rt
	http.DefaultClient.Transport = rt
	return func() {
		http.DefaultTransport = prev
		http.DefaultClient.Transport = prevC
	}
}

func makeResp(status int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(status)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

// SSE body the Anthropic streaming SDK expects. We hand-craft a single
// message_start → content_block_delta → message_delta → message_stop
// sequence; the SDK happily consumes that.
const sampleStreamBody = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-opus-4-7","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":42,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"# Hi\n\nrevised body\n"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}

event: message_stop
data: {"type":"message_stop"}

`

func makeSSEResp(body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "text/event-stream")
	rec.WriteHeader(200)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

func TestRevise_Streaming_HappyPath(t *testing.T) {
	restore := swapDefault(func(req *http.Request) *http.Response {
		return makeSSEResp(sampleStreamBody)
	})
	t.Cleanup(restore)

	var chunks []string
	onDelta := func(s string) error {
		chunks = append(chunks, s)
		return nil
	}
	res, err := Revise(context.Background(), "sk-ant-test", "Title",
		"Original\n", []ResolvedComment{{Quoted: "Original", Body: "use Hi"}}, onDelta)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(res.Content, "Hi") {
		t.Errorf("content missing expected text: %q", res.Content)
	}
	if len(chunks) == 0 {
		t.Errorf("onDelta should have been called at least once")
	}
}

func TestRevise_StreamingOnDeltaAbort(t *testing.T) {
	restore := swapDefault(func(req *http.Request) *http.Response {
		return makeSSEResp(sampleStreamBody)
	})
	t.Cleanup(restore)

	onDelta := func(_ string) error {
		return errors.New("client disconnected")
	}
	_, err := Revise(context.Background(), "sk-ant-test", "T", "Hi\n",
		[]ResolvedComment{{Quoted: "Hi", Body: "x"}}, onDelta)
	if err == nil {
		t.Fatal("expected error when onDelta returns one")
	}
}

// classifyError needs an *sdk.Error with all required fields populated to
// call .Error() without panicking. Easiest way to get one is to round-trip
// a real HTTP response through the SDK, which we drive via swapDefault.
func makeSDKError(t *testing.T, status int, body string) error {
	t.Helper()
	restore := swapDefault(func(req *http.Request) *http.Response {
		return makeResp(status, body)
	})
	defer restore()
	err := ValidateAPIKey(context.Background(), "sk-ant-x")
	if err == nil {
		t.Fatalf("expected error for status %d", status)
	}
	// Walk the Unwrap chain to extract the inner *sdk.Error.
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		var sErr *sdk.Error
		if errors.As(cur, &sErr) {
			return sErr
		}
	}
	t.Fatalf("could not extract *sdk.Error from %v", err)
	return nil
}

func TestClassifyError_SDKStatusBranches(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   ErrorKind
	}{
		{401, `{"type":"error","error":{"type":"authentication_error","message":"key bad"}}`, ErrKindInvalidKey},
		{403, `{"type":"error","error":{"type":"permission_error","message":"no model access"}}`, ErrKindInvalidKey},
		{429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, ErrKindRateLimited},
		{503, `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`, ErrKindOverloaded},
		{500, `{"type":"error","error":{"type":"api_error","message":"server"}}`, ErrKindOther},
	}
	for _, c := range cases {
		sdkErr := makeSDKError(t, c.status, c.body)
		out := classifyError(sdkErr)
		rev, ok := out.(*RevisionError)
		if !ok {
			t.Errorf("status %d: got %T", c.status, out)
			continue
		}
		if rev.Kind != c.kind {
			t.Errorf("status %d: kind=%q want %q", c.status, rev.Kind, c.kind)
		}
	}
}

func TestValidateAPIKey_Wireshape(t *testing.T) {
	// We can't easily simulate the full client behaviour; just verify the
	// prefix-check branch (covered already) AND the SDK-side branch by
	// providing a syntactically valid key + mocked 200 response.
	restore := swapDefault(func(req *http.Request) *http.Response {
		return makeResp(200, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"m","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	t.Cleanup(restore)
	if err := ValidateAPIKey(context.Background(), "sk-ant-good"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestValidateAPIKey_InvalidStatusCode(t *testing.T) {
	restore := swapDefault(func(req *http.Request) *http.Response {
		return makeResp(401, `{"type":"error","error":{"type":"authentication_error","message":"invalid"}}`)
	})
	t.Cleanup(restore)
	if err := ValidateAPIKey(context.Background(), "sk-ant-bad"); err == nil {
		t.Error("expected err for 401")
	}
}
