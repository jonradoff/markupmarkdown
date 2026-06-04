---
name: markupmarkdown
description: Google-Docs-style commenting on markdown documents. Lets agents and human reviewers collaborate on the same docs — agents can read, comment, reply, resolve threads, and trigger AI revisions of a doc based on the comments humans have approved.
---

# markupmarkdown — agent guide

markupmarkdown is a doc-review tool: humans paste markdown URLs, drag-select text, leave inline comments, and resolve threads when settled. Agents do the same things over MCP — they see what humans see, leave comments humans review, and (with human approval) trigger AI revisions that apply the resolved feedback.

## Mental model

The unit of collaboration is a **comment thread anchored to a span of text in the document**. Threads have a state (open / done / unread relative to last view). Resolved threads are the inputs to AI revision: when the model decides to apply them, it produces a *new* document with `parent_id` pointing at the original. Original is never mutated.

```
Document (markdown)
  ├── Anchor (span of text)
  │   └── Thread (comment + replies, resolved or open)
  └── Revisions (child documents, formed by applying resolved threads)
```

Identity sources:

- **Human**: GitHub OAuth, session cookie. Default actor_kind = `human`.
- **Agent**: Personal access token (`mmk_…`) created by a human, sent as Bearer auth. If the token is marked `isAgent: true`, every comment/reply it creates is tagged `actor_kind: "agent"` and rendered with a bot badge so humans can tell who said what.

## Authentication

1. A human user creates a token at <https://mumd.metavert.io/> → avatar menu → **Personal access tokens** → label it (e.g. `claude-code-laptop`) → check "for an agent" → Generate.
2. The token (format `mmk_<64 hex>`) is shown once; the human stores it.
3. The agent sends it as `Authorization: Bearer mmk_…` on every REST and MCP call.

Tokens can be scoped per-agent and revoked any time. Never embed the token in a comment body or commit it to a public repo.

## MCP server

| Property | Value |
|---|---|
| Transport | Streamable HTTP |
| Endpoint | `https://mumd.metavert.io/mcp` (or `https://<your-host>/mcp` for self-hosted) |
| Auth | `Authorization: Bearer mmk_…` |
| Implementation | `github.com/mark3labs/mcp-go` v0.54 |

### Available tools

| Tool | What it does |
|---|---|
| `list_documents` | Lists docs the calling identity has worked on. Set `include_trash: true` to include soft-deleted docs. |
| `get_document` | Full markdown content + metadata. Errors with "access denied" for private docs the token's user can't read. |
| `list_comments` | Comments on a doc. Filter: `open` (default), `resolved`, `all`. Set `render_html: true` to get sanitized HTML rendering of bodies alongside the raw markdown. |
| `add_comment` | Anchors a new comment to a **verbatim substring** of the document. If the substring appears multiple times, pass `occurrence: N` (1-based). |
| `reply` | Reply to an existing thread. |
| `resolve_comment` / `reopen_comment` | Lifecycle. Resolved threads become eligible inputs for `revise_with_ai`. |
| `revise_with_ai` | Runs Claude Opus 4.7 over the doc + selected resolved threads. Default `accept: false` returns a preview; `accept: true` saves the result as a new child document and returns its ID. Uses the **human user's** stored Anthropic key, never the agent's. |

### When agents should do what

- **Reading**: use `get_document` + `list_comments` (filter=`all`, `render_html: true`) for a complete review snapshot.
- **Suggesting changes**: leave a thread per suggestion with `add_comment`. Keep bodies focused on a single concern; the AI revision step works best when each thread says one thing.
- **Discussing**: `reply` to humans' threads to explain reasoning. Don't resolve threads yourself unless the human explicitly delegated that decision.
- **Triggering revisions**: only with explicit human approval. The typical flow is: human approves N resolved threads → asks agent to apply → agent calls `revise_with_ai` with `accept: false` → agent shows the human the diff → on confirmation, agent calls again with `accept: true`.
- **Stopping**: never spam — one substantive comment beats five tiny ones. The frontend collapses long threads and humans will lose track.

## Examples

### Read a doc and its open threads

```jsonc
// MCP request
{
  "name": "list_documents",
  "arguments": {}
}

// → returns [{ id, title, url, ... }, ...]

{
  "name": "get_document",
  "arguments": { "id": "a3f7c2..." }
}

// → returns the full markdown content + parent/source metadata

{
  "name": "list_comments",
  "arguments": {
    "document_id": "a3f7c2...",
    "filter": "all",
    "render_html": true
  }
}
```

### Leave a margin comment on a specific phrase

```jsonc
{
  "name": "add_comment",
  "arguments": {
    "document_id": "a3f7c2...",
    "quoted_text": "we may scale linearly",
    "body": "This claim is unsupported — the benchmark on §4.2 shows sub-linear scaling above 64 cores. Suggest softening to 'scales well up to medium concurrency'."
  }
}
```

If `"we may scale linearly"` appears twice in the doc, the tool returns an error. Disambiguate with `"occurrence": 2`.

### Suggest a multi-thread revision and apply it (with human approval)

```jsonc
// 1. Leave several focused threads
{ "name": "add_comment", "arguments": { "document_id": "...", "quoted_text": "...", "body": "..." } }
{ "name": "add_comment", "arguments": { "document_id": "...", "quoted_text": "...", "body": "..." } }

// 2. The human reviews, replies, and resolves the ones they agree with.

// 3. Agent asks Claude to apply the resolved set, preview only:
{
  "name": "revise_with_ai",
  "arguments": {
    "document_id": "a3f7c2...",
    "accept": false
  }
}

// → returns { originalContent, revisedContent, model, tokensIn, tokensOut, appliedCommentIds }

// 4. After the human approves the diff, save it as a new revision:
{
  "name": "revise_with_ai",
  "arguments": {
    "document_id": "a3f7c2...",
    "accept": true
  }
}

// → returns { ..., newDocumentId: "b8c4d1..." }
```

### Reply to a thread

```jsonc
{
  "name": "reply",
  "arguments": {
    "comment_id": "c1d2e3...",
    "body": "Sources for the original benchmark: [link]. I can produce a revised graph if useful."
  }
}
```

## Conventions agents should follow

1. **Always cite verbatim quotes** in `add_comment` — paraphrasing makes anchoring brittle.
2. **One thread per concern** — easier for humans to review, easier for the AI revision step to apply cleanly.
3. **Use markdown formatting** in comment bodies — humans see it rendered (and other agents can fetch HTML via `render_html: true`).
4. **Mention humans** explicitly with `@github-login` when you want their attention; they get an in-app notification.
5. **Don't resolve your own threads** unless the human told you to. Resolution = "the human team agreed this is the answer." Self-resolving short-circuits the review model.
6. **Treat `revise_with_ai` as a privileged action.** Prefer `accept: false` for the first call, surface the diff, then call again with `accept: true` only after explicit confirmation.
7. **Respect rate limits.** The token's per-user budget covers all API endpoints; bursts > 30 in a minute will see `429`s.

## REST fallback

Every MCP tool corresponds to a REST endpoint at `/api/...` (see the project README's API surface). The same Bearer token authenticates both. Use REST when:

- You need an endpoint MCP doesn't expose (e.g. listing trash, fetching notifications)
- You want Server-Sent Events for live updates (`GET /api/documents/:id/events`)
- Your runtime isn't MCP-aware

## Limits

- Comment body: 16 KB. Reply: 16 KB. Quoted-anchor text: 4 KB.
- AI revision: 3 concurrent per user, 30/hour. Anthropic-side limits apply on top.
- Document storage: 5 MB markdown.
- SSE connections: 10 per identity, 200 server-wide.
- Soft-deleted docs are recoverable for 30 days before purge.

## What's out of scope for agents

- **Creating documents from URLs**: human-only for now (private-repo access checks need a real GitHub OAuth session). Agents can revise existing docs, but they can't `POST /api/documents` to clone a new URL.
- **Managing other users' tokens or AI keys**: a token can only create/revoke tokens for itself, and a token can never read or update the Anthropic key.
- **Modifying GitHub OAuth state**: agents inherit the human user's repo access; they can't change it.

For project state, latest changes, and integration ideas, see the project README at <https://github.com/jonradoff/markupmarkdown>.
