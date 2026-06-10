---
name: markupmarkdown
description: Google-Docs-style commenting on markdown documents. Lets agents and human reviewers collaborate on the same docs — agents can read, comment, reply, resolve threads, and trigger AI revisions of a doc based on the comments humans have approved.
---

# markupmarkdown — agent guide

markupmarkdown is a doc-review tool: humans paste markdown URLs, drag-select text, leave inline comments, edit the markdown directly when needed, and push the resolved revision back to GitHub. Agents do the same things over MCP — they read what humans read, leave comments humans review, edit the doc when asked, and (with human approval) trigger AI revisions, merges, or pull requests.

Humans have a native CodeMirror editor with a formatting toolbar, find & replace, and live preview; agents accomplish the same thing programmatically via `edit_document` (full-content replace) or `revise_with_ai` (resolved-comments-driven). When a human is mid-edit, a soft edit lock prevents other writers (human or agent) from clobbering them — `edit_document` and `revise_with_ai` will surface a 409-style "X is editing" if the doc is locked by someone else.

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

**Reading** (no scope required beyond `read`):

| Tool | What it does |
|---|---|
| `list_documents` | Lists docs the calling identity has worked on. Set `include_trash: true` to include soft-deleted docs. |
| `get_document` | Full markdown content + metadata. Errors with "access denied" for private docs the token's user can't read. |
| `list_comments` | Comments on a doc. Filter: `open` (default), `resolved`, `all`. Set `render_html: true` to get sanitized HTML rendering of bodies alongside the raw markdown. |
| `list_revisions` | Returns the full revision chain (root → leaf) for any doc with each node's `revisionIndex`, `model`, `generatedBy`, `actorKind`, and timestamps. One call replaces walking `parent` / `children` from `get_document`. |

**Commenting** (`write` scope):

| Tool | What it does |
|---|---|
| `add_comment` | Anchors a new comment to a **verbatim substring** of the document. If the substring appears multiple times, pass `occurrence: N` (1-based). |
| `reply` | Reply to an existing thread. |
| `resolve_comment` / `reopen_comment` | Lifecycle. Resolved threads become eligible inputs for `revise_with_ai`. |
| `patch_anchor` | Re-anchor an orphan comment, or convert any comment to a document-level pin (`doc_level: true`). Mine-only — you can only re-anchor comments you (or an agent token you own) wrote. |
| `delete_comment` | Remove a thread you authored. Mine-only — same require-mine guard as the REST surface. |

**Revising the document** (`admin` scope; these create or mutate doc content):

| Tool | What it does |
|---|---|
| `revise_with_ai` | Runs Claude Opus 4.7 over the doc + selected resolved threads. Default `accept: false` returns a preview; `accept: true` saves the result as a new child document, carries unresolved comments forward, and returns the new ID. Uses the **human user's** stored Anthropic key. |
| `edit_document` | Apply a manual edit by sending the full new content. Creates a new child revision in the chain; unresolved comments carry forward. Use when you've decided on a specific change yourself, rather than asking Claude to derive it from resolved threads. |
| `merge_from_github` | Reconcile this doc with its upstream GitHub source via the 3-way Claude merge (ancestor = source content the revision was based on, ours = current doc, theirs = new upstream). Persists the merged content in place and re-anchors comments. Trivial cases (no AI revision, or upstream == ours) bypass Claude. Use when `get_document`'s drift indicators say upstream has changed. |
| `push_to_github` | Opens a pull request from this doc's current content back to its source repo. PR mode only over MCP — direct-commit is intentionally web-UI-only for safety. Only push when a human has explicitly asked. |

### When to edit vs revise vs merge

| Situation | Tool |
|---|---|
| Human resolved comments; agent asked to apply them | `revise_with_ai` (preview, then `accept: true` after the human nods) |
| Agent wants to make a targeted edit it decided on (typo, rewording) | `edit_document` |
| Upstream GitHub source has changed (drift detected); want to incorporate | `merge_from_github` |
| Revision is approved and ready to ship back to the repo | `push_to_github` (PR mode) |
| Comment you wrote has become an orphan after a merge / sync | `patch_anchor` |

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

### Apply a targeted manual edit (no AI revision)

```jsonc
// Read current content + revision index first so you splice into the right base
{ "name": "get_document", "arguments": { "id": "a3f7c2..." } }
// → { content: "...", revisionIndex: 4, ... }

// Save the edited content as a new revision; unresolved comments carry forward.
{
  "name": "edit_document",
  "arguments": {
    "document_id": "a3f7c2...",
    "content": "<the full new markdown>",
    "revision_note": "Fix §4.2 scaling claim per Jon's review"
  }
}
// → { newDocumentId: "d9e3f1...", revisionIndex: 5, carriedForward: 3, orphaned: 0 }
```

Prefer `edit_document` over `revise_with_ai` when **you** decided on a specific change rather than asking Claude to derive it from resolved threads. Always check `orphaned`: a non-zero count means some comments lost their anchor on the new content; either re-anchor with `patch_anchor` or leave them for the human.

### Pull in upstream GitHub edits and push the revision back

```jsonc
// 1. Source drift detected (get_document.sourceDriftedAt set). Run the 3-way merge:
{ "name": "merge_from_github", "arguments": { "document_id": "a3f7c2..." } }
// → { merged: true, mergedContent: "...", orphanedComments: 1, trivial: false }

// 2. After the human reviews the merged content + resolves any orphans,
//    open a PR back to the source repo. PR mode is the only mode over MCP.
{
  "name": "push_to_github",
  "arguments": {
    "document_id": "a3f7c2...",
    "branch": "claude/wingman-prd-revisions",
    "commit_message": "PRD: tighten §4.2 scaling claim",
    "pr_title": "PRD revision: scaling claim + resolved review threads",
    "pr_body": "Applies threads resolved in https://mumd.metavert.io/d/a3f7c2..."
  }
}
// → { mode: "pr", branch: "...", commitSha: "...", prNumber: 142, prUrl: "..." }
```

Only call `push_to_github` when a human has explicitly asked.

## The revision chain

Every doc lives in a chain: a root (cloned from a URL or uploaded), plus zero or more child revisions formed by `revise_with_ai`, `edit_document`, or `merge_from_github`. The chain is linear in practice.

- Each child stamps its parent's source content on `revision_meta.ancestorContent` so a future merge knows the common-ancestor for a 3-way reconciliation.
- `list_revisions` returns the whole chain with each node's `revisionIndex` (root = v1).
- `get_document` returns `revisionIndex`, `revisionTotal`, `parent`, `rootDocument`, and `latestDescendant` so a single read tells you where this doc sits.
- The recent-docs list is deduped to the **leaf** of each chain. If you want the current state of a doc the user is working on, that's where they'll be — older versions are reachable via the toolbar's vN breadcrumb.

## Conventions agents should follow

1. **Always cite verbatim quotes** in `add_comment` — paraphrasing makes anchoring brittle.
2. **One thread per concern** — easier for humans to review, easier for `revise_with_ai` to apply cleanly.
3. **Use markdown formatting** in comment bodies — humans see it rendered (and other agents can fetch HTML via `render_html: true`).
4. **Mention humans** explicitly with `@github-login` when you want their attention; they get an in-app notification.
5. **Don't resolve your own threads** unless the human told you to. Resolution = "the human team agreed this is the answer." Self-resolving short-circuits the review model.
6. **Treat `revise_with_ai`, `edit_document`, `merge_from_github`, and `push_to_github` as privileged.** Prefer `accept: false` previews for `revise_with_ai`; for the others, work off an explicit human ask ("apply the changes you suggested", "pull in the upstream edits", "open the PR").
7. **Carry-forward is automatic — don't try to re-anchor on a child you just created.** When `revise_with_ai accept=true` or `edit_document` creates a new revision, the system carries unresolved comments over and re-anchors them against the new content. Orphans that result are surfaced in the new doc's orphan section for the human to handle.
8. **Re-anchor your own orphans on merge.** After `merge_from_github`, your prior agent comment may have become orphan. If you can identify a new span that captures the same intent, call `patch_anchor` with the new `(start, end, quoted_text)`. If you can't, leave it for the human.
9. **Never push to GitHub without explicit human go-ahead.** A human asking "ship this" via a resolved comment or chat reply counts; a periodic schedule does not.
10. **Respect rate limits.** Comments / replies / resolves: 60/min per user. AI revisions: 240/hour, max 3 concurrent. Merges: 240/hour. The Anthropic-side budget is whatever the user set on their account — that's the real ceiling.

## REST fallback

Every MCP tool corresponds to a REST endpoint at `/api/...` (see the project README's API surface). The same Bearer token authenticates both. Use REST when:

- You need an endpoint MCP doesn't expose (e.g. listing trash, fetching notifications)
- You want Server-Sent Events for live updates (`GET /api/documents/:id/events`)
- Your runtime isn't MCP-aware

## Limits

- Comment body: 16 KB. Reply: 16 KB. Quoted-anchor text: 4 KB.
- AI revision: 240/hour per user, max 3 concurrent. Anthropic-side limits apply on top.
- Source merges: 240/hour per user (separate bucket from AI revisions).
- Document storage: 5 MB markdown.
- SSE connections: 10 per identity, 200 server-wide.
- Soft-deleted docs are recoverable for 30 days before purge.

## What's out of scope for agents

- **Creating documents from URLs**: human-only for now (private-repo access checks need a real GitHub OAuth session). Agents can revise / edit / merge existing docs, but they can't `POST /api/documents` to clone a new URL.
- **Managing other users' tokens or AI keys**: a token can only create/revoke tokens for itself, and a token can never read or update the Anthropic key.
- **Modifying GitHub OAuth state**: agents inherit the human user's repo access; they can't change it.
- **Direct-commit pushback**: `push_to_github` is PR-mode only over MCP. Direct commits to a branch are intentionally web-UI-only — branch-protection enforcement is the repo owner's call.
- **Editing comments authored by other users**: same require-mine guard as the REST API. `delete_comment` and `patch_anchor` work only on comments your token wrote.

For project state, latest changes, and integration ideas, see the project README at <https://github.com/jonradoff/markupmarkdown>.
