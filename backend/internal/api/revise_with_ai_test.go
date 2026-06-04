package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"markupmarkdown/internal/ai"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

// SSE stream the Anthropic SDK expects from a streaming Messages call.
const sseHappy = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-opus-4-7","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":50,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"# Revised\n\nUpdated body.\n"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}

event: message_stop
data: {"type":"message_stop"}

`

func sseHandler(req *http.Request) *http.Response {
	if strings.Contains(req.URL.Path, "/messages") {
		// Streaming request → SSE body.
		rec := newSSE(sseHappy)
		return rec
	}
	return makeResp(200, "{}")
}

func newSSE(body string) *http.Response {
	res := makeResp(200, body)
	res.Header.Set("Content-Type", "text/event-stream")
	return res
}

func TestReviseWithAI_PreviewSucceedsWithMockedAnthropic(t *testing.T) {
	restore := ghMock(t, sseHandler)
	defer restore()

	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)

	// Set up an Anthropic key (via the store directly to skip key-validation).
	_ = st.UpsertAnthropicKey(context.Background(), user.ID,
		mustEncryptForTest(t, a, "sk-ant-fake"), "fake…ake")

	// Create a doc with a resolved comment so revise has something to do.
	doc := testutil.NewTestDocument(t, st, user.ID, "Hello world")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "Hello", "use 'Hi'")
	// Mark it resolved.
	if _, err := a.ResolveComment(context.Background(), user.ID, c.ID, false); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	out, err := a.ReviseWithAI(context.Background(), user.ID, doc.ID, nil, false, "t")
	if err != nil {
		t.Fatalf("revise: %v", err)
	}
	if out == nil || out.RevisedContent == "" {
		t.Fatalf("empty result: %+v", out)
	}
	if out.NewDocID != "" {
		t.Error("preview should NOT create a new doc")
	}
}

func TestReviseWithAI_AcceptCreatesNewDoc(t *testing.T) {
	restore := ghMock(t, sseHandler)
	defer restore()

	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	_ = st.UpsertAnthropicKey(context.Background(), user.ID,
		mustEncryptForTest(t, a, "sk-ant-fake"), "fake…ake")

	doc := testutil.NewTestDocument(t, st, user.ID, "Hi")
	c := testutil.NewTestComment(t, st, doc.ID, user.ID, "Hi", "x")
	_, _ = a.ResolveComment(context.Background(), user.ID, c.ID, false)

	out, err := a.ReviseWithAI(context.Background(), user.ID, doc.ID, nil, true, "t")
	if err != nil {
		t.Fatalf("revise: %v", err)
	}
	if out.NewDocID == "" {
		t.Error("accept=true should set NewDocID")
	}
	got, _ := st.GetDocument(context.Background(), out.NewDocID)
	if got == nil {
		t.Error("new doc not found")
	}
}

func TestReviseWithAI_NoKey(t *testing.T) {
	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "x")
	if _, err := a.ReviseWithAI(context.Background(), user.ID, doc.ID, nil, false, "t"); err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestReviseWithAI_NoResolved(t *testing.T) {
	restore := ghMock(t, sseHandler)
	defer restore()

	_, st, a := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	_ = st.UpsertAnthropicKey(context.Background(), user.ID,
		mustEncryptForTest(t, a, "sk-ant-fake"), "fake…ake")
	doc := testutil.NewTestDocument(t, st, user.ID, "x")
	// No resolved comments.
	if _, err := a.ReviseWithAI(context.Background(), user.ID, doc.ID, nil, false, "t"); err == nil {
		t.Fatal("expected error for no resolved")
	}
}

// mustEncryptForTest reaches into the API's vault to encrypt the given
// plaintext. The vault is created from the test config's master key.
func mustEncryptForTest(t *testing.T, a interface {
	// Encrypter exposed via a thin assertion below.
	// We hold the vault privately on *api.API and don't export it, but we
	// can simulate by calling the secrets package directly via testutil.
}, key string) string {
	t.Helper()
	v := mustVault(t)
	ct, err := v.Encrypt(key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return ct
}

var _ = ai.Model // keep import even if all uses are conditional

// mustVault builds a vault from the test config so the test can store a
// pre-encrypted key.
func mustVault(t *testing.T) interface {
	Encrypt(string) (string, error)
} {
	t.Helper()
	cfg := testutil.LoadTestConfig(t)
	v, err := mkVault(cfg.Encryption.MasterKey)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	return v
}

// mkVault delegates to the secrets package via a small shim to keep this
// test file free of internal-package imports the linter might object to.
func mkVault(key string) (interface{ Encrypt(string) (string, error) }, error) {
	return testutil.NewTestVault(key)
}

// silence unused-model lint
var _ models.Document
