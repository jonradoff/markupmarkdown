# markupmarkdown

**Comment on any markdown file like it's a Google Doc — and bring agents into the same review loop as your team.** Paste a URL, drag-select text, leave a margin comment. Your teammates see it in real time. Resolve the thread when you're done — or hand the resolved threads to Claude and watch it produce a clean revised version. Agents can join the same review through an MCP server: they read what humans read, leave threads humans approve, and (with explicit human sign-off) apply the resolved feedback as a new revision.

Live: **<https://mumd.metavert.io/>**

---

## The problem

Markdown is where a lot of real thinking lives now — PRDs, design docs, RFCs, release notes, prompts, briefs. But the tools for *reviewing* it are miserable:

- **GitHub PRs** force every discussion through a code-review workflow. Fine for production code, painful for a quick "this paragraph is unclear" on a brainstorming doc you haven't even branched yet.
- **HackMD / Notion / Dropbox Paper** lock your content into their format, pull you out of `.md`, and require everyone to make an account.
- **Pasting into Google Docs** drops all your formatting and now you have two sources of truth.

You just want to drag-select a sentence and leave a comment. Like Google Docs. On a real markdown file. And in 2026 you probably also want an agent on your review team — but you don't want to give that agent your whole identity, and you definitely don't want it shipping changes without you signing off.

## What this is

A small self-contained web app:

- **Backend**: one Go binary (~12 MB Alpine image) talking to MongoDB
- **Frontend**: a single React SPA served from the same binary
- **Storage**: per-doc copy in Mongo, that's it — no S3, no Redis, no queue
- **Deploy**: one Fly machine, scales to zero, ~free for personal use

Everything you'd expect from a Google-Docs-style review experience, in a codebase small enough to read on a Sunday.

## What it does

### For humans

- **Open any markdown file** — paste a URL (raw URL or `github.com/.../blob/.../*.md`, we auto-rewrite) or upload from your computer. Relative image refs (`<img src=".github/logo.svg">`) resolve against the source so READMEs render properly.
- **Drag-select text → comment**. Click the floating "Comment" button, type, submit. Use `@username` to mention someone — they get an in-app notification.
- **Threaded replies**, mark-as-done, reopen, edit, delete. Same model your team already knows.
- **Realtime sync**: every change propagates to every other open tab in <1s via Server-Sent Events.
- **Unread filter pill** — shows you only the threads with new activity since your last visit, with a count badge.
- **Step through comments** with `j` / `k` (or `↑` / `↓`, or the Prev/Next buttons). The position counter respects whichever filter is active.
- **In-app notifications** — bell icon in the header, badge for unread count, dropdown with deep links into the relevant comment.
- **Soft delete with 30-day recovery** — deleted docs sit in Trash and can be restored before the daily purge sweep.
- **Light / dark theme** that respects your system pref.
- **Share dialog** — copies the link with an explicit note about access (private docs warn you about the GitHub-repo requirement before you send the URL).
- **Per-tab title** and **Open Graph link unfurls** so shared URLs look meaningful in Slack/iMessage. Private docs share a generic card so titles don't leak.

### Optional GitHub identity

- **Sign in with GitHub** to use your avatar and display name automatically, and to unlock private repo files (the OAuth app needs `repo` scope).
- **Private docs stay private**: if a doc was cloned from a repo that required GitHub auth to read, every subsequent view re-verifies that the current user has GitHub access. No access? No content shown — they can't even see the title.

### AI revision

- Bring your own Anthropic API key (stored encrypted at rest, deletable any time).
- Click **Revise with AI**, choose which resolved comments to apply (all by default — uncheck any you want to skip), and Claude Opus 4.7 produces a revised version that incorporates the agreed feedback while changing as little of the rest as possible.
- The output **streams as rendered markdown** in real time — you watch headings and paragraphs materialize as Claude writes them.
- Word-level diff preview before you accept. Saving creates a new document with a parent backlink, so revisions form a tree. The original (and its comments) stay untouched.

### Agent collaboration (new — see [Agents](#agents) below)

- **Personal access tokens** authenticate scripts and agents to the same REST API humans use.
- **MCP server** at `/mcp` exposes the review primitives as Model Context Protocol tools — any MCP-aware agent (Claude Code, Claude Agent SDK apps, custom tools) can read docs, leave threads, reply to humans, resolve, and trigger AI revisions with human approval.
- **Agent identity badges** — comments and replies created via a token marked "for an agent" get a small bot badge so humans can scan a thread and instantly see who's whom.

## Lightweight by design

This isn't a SaaS. The whole stack:

```
fly machine (512 MB)
└── markupmarkdown (Go binary)
    ├── /api/*           — gorilla/mux router (REST)
    ├── /mcp             — Model Context Protocol server for agents
    ├── /SKILL.md        — canonical agent integration guide (raw markdown)
    ├── /                — SPA from /app/web/dist
    └── MongoDB Atlas    — documents, comments, sessions, notifications, tokens
```

No build-time JavaScript on the server. No background workers besides a daily purge sweep and a bounded view-recording queue. No webhooks. No analytics SDK. No cookie banner because the only cookie is a session ID. Bring your own Anthropic key — your usage, your bill, your data.

## Quick start

```sh
git clone <this-repo>
cd markupmarkdown

# Backend
cd backend
cp .env.example .env       # fill in MONGODB_URI + ENCRYPTION_KEY
go run ./cmd/markupmarkdown

# Frontend (separate terminal)
cd frontend
npm install && npm run dev
```

Open <http://localhost:4720/>.

### Generate the encryption master key

```sh
openssl rand -hex 32
```

Set it as `MARKUPMARKDOWN_ENCRYPTION_KEY` in `backend/.env` (for dev) and as a Fly secret (for prod). This key encrypts per-user Anthropic API keys at rest using AES-256-GCM — without it, the AI-revision feature is disabled but everything else keeps working. Ciphertexts are prefixed with a version (`v1:`) so you can rotate by setting `MARKUPMARKDOWN_ENCRYPTION_KEY_V2=<old>` and updating the primary key.

## Optional: GitHub OAuth

Login + private repos are opt-in. Register an OAuth app at <https://github.com/settings/applications/new>:

| Field | Value |
|---|---|
| Homepage URL | `http://localhost:4720` (dev) / your prod URL |
| Callback URL | `http://localhost:4721/api/auth/github/callback` (dev) / `<your-prod-url>/api/auth/github/callback` |

Add to env:

```
GITHUB_CLIENT_ID=Iv1.xxxxxxxxxxxxxxxx
GITHUB_CLIENT_SECRET=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Scope `repo` enables reading private files; drop to `read:user user:email` if you only want public docs.

## Deploy to Fly.io

```sh
fly launch              # or `fly apps create` if you already have a name
fly secrets set MONGODB_URI="mongodb+srv://..."
fly secrets set MARKUPMARKDOWN_ENCRYPTION_KEY="$(openssl rand -hex 32)"
# optional, after registering the OAuth app:
fly secrets set GITHUB_CLIENT_ID=... GITHUB_CLIENT_SECRET=...
fly deploy
```

The included `Dockerfile` is a 3-stage build (frontend → backend → alpine runtime). Final image is ~12 MB.

## Agents

markupmarkdown is built for **agent + human review on the same doc**. The model is simple: agents see what humans see, contribute the same way (threaded comments anchored to text spans), and the human stays in the loop for anything irreversible — including AI revisions.

### Personal access tokens

Humans grant agents access by minting **Personal access tokens** in their avatar menu → *Personal access tokens*:

- Format: `mmk_<64 hex characters>`
- Stored hashed (SHA-256); plaintext is shown once at creation
- Tag a token as "for an agent" so every comment/reply it creates gets the bot badge
- Revocable any time

The agent passes the token as `Authorization: Bearer mmk_…` on REST and MCP calls.

### MCP server

Streamable HTTP transport at **`/mcp`**, built on [`github.com/mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go). Tools:

| Tool | Purpose |
|---|---|
| `list_documents` | Docs the calling identity has touched |
| `get_document` | Full markdown content + metadata |
| `list_comments` | Threads on a doc — filter by `open` / `resolved` / `all`, optionally pre-render bodies to HTML |
| `add_comment` | Anchor a new thread to a verbatim substring of the doc (with `occurrence` to disambiguate matches) |
| `reply` | Reply to an existing thread |
| `resolve_comment` / `reopen_comment` | Lifecycle |
| `revise_with_ai` | Run Claude Opus 4.7 over the doc + selected resolved threads. Preview-only by default; pass `accept: true` to save as a new child doc |

The full agent guide — including conventions, identity model, rate limits, and out-of-scope actions — lives at [`skills/markupmarkdown/SKILL.md`](skills/markupmarkdown/SKILL.md) and is served live at <https://mumd.metavert.io/SKILL.md>.

### Examples

#### 1. Read a doc and its open threads

```jsonc
// MCP request
{ "name": "list_documents", "arguments": {} }
// → [{ id, title, url, ... }, ...]

{ "name": "get_document", "arguments": { "id": "a3f7c2..." } }
// → { id, title, content, sourceUrl, parentId, ... }

{
  "name": "list_comments",
  "arguments": {
    "document_id": "a3f7c2...",
    "filter": "all",
    "render_html": true
  }
}
// → [{ id, anchor: {exact, ...}, body, bodyHtml, replies, resolved, ... }, ...]
```

#### 2. Leave a margin comment anchored to a specific phrase

```jsonc
{
  "name": "add_comment",
  "arguments": {
    "document_id": "a3f7c2...",
    "quoted_text": "we may scale linearly",
    "body": "This claim is unsupported — the benchmark in §4.2 shows sub-linear scaling above 64 cores. Suggest softening to 'scales well up to medium concurrency'."
  }
}
```

If the quoted text appears multiple times in the doc, the tool returns an error — disambiguate with `"occurrence": 2`.

#### 3. Reply to a human's thread

```jsonc
{
  "name": "reply",
  "arguments": {
    "comment_id": "c1d2e3...",
    "body": "Sources for the original benchmark: [link]. I can produce a revised graph if useful."
  }
}
```

#### 4. Apply resolved comments as a new revision (with human approval)

```jsonc
// Preview only — does NOT save:
{
  "name": "revise_with_ai",
  "arguments": {
    "document_id": "a3f7c2...",
    "accept": false
  }
}
// → { originalContent, revisedContent, model, tokensIn, tokensOut, appliedCommentIds }

// After the human approves the diff, save as a new child doc:
{
  "name": "revise_with_ai",
  "arguments": {
    "document_id": "a3f7c2...",
    "accept": true,
    "comment_ids": ["c1d2e3...", "c4d5e6..."]   // optional subset
  }
}
// → { ..., newDocumentId: "b8c4d1..." }
```

The revision uses the **human user's** stored Anthropic key — the agent never sees it and never gets billed.

### REST fallback

Every MCP tool corresponds to a REST endpoint at `/api/...` — the same Bearer token authenticates both. Use REST when you need an endpoint MCP doesn't expose (notifications, trash, SSE event streams) or your runtime isn't MCP-aware.

### Conventions agents should follow

1. **Always quote verbatim** in `add_comment` — paraphrasing breaks anchoring.
2. **One thread per concern** — easier for humans to review, easier for `revise_with_ai` to apply cleanly.
3. **Use markdown formatting** in bodies — humans see it rendered, other agents can fetch HTML via `render_html: true`.
4. **Mention humans explicitly** with `@github-login` when you want their attention.
5. **Don't resolve your own threads** unless the human told you to.
6. **Treat `revise_with_ai` as privileged** — prefer `accept: false` first, surface the diff, only then call `accept: true`.
7. **Respect rate limits.** Token budgets are per-user; bursts > 30/min get `429`s.

## Full API surface

```
# Documents + comments
POST   /api/documents                           { url, title? } | { content, title }
GET    /api/documents                           your recent docs (requires sign-in)
GET    /api/me/trash                            your soft-deleted docs (within 30-day window)
POST   /api/documents/:id/restore               restore a soft-deleted doc
GET    /api/documents/:id
PATCH  /api/documents/:id                       { title }
DELETE /api/documents/:id                       soft delete

GET    /api/documents/:id/comments              ?render=html to include sanitized HTML bodies
POST   /api/documents/:id/comments              { anchor:{start,end,exact}, body, author }
GET    /api/documents/:id/events                SSE stream — comments-updated events
GET    /api/documents/:id/mention-candidates    people known to this doc, for @-autocomplete

PATCH  /api/comments/:id                        { body }
DELETE /api/comments/:id
POST   /api/comments/:id/resolve                { author }
POST   /api/comments/:id/reopen
POST   /api/comments/:id/replies                { body, author }
PATCH  /api/comments/:id/replies/:replyId       { body }
DELETE /api/comments/:id/replies/:replyId

# Auth + identity
GET    /api/auth/config                         { githubEnabled, githubClientId }
GET    /api/auth/me                             { user }
GET    /api/auth/github/login?redirect=/d/...
GET    /api/auth/github/callback
POST   /api/auth/logout

GET    /api/me/notifications                    { unread, notifications }
POST   /api/me/notifications/read               mark all read
POST   /api/me/notifications/:id/read           mark one read

GET    /api/me/tokens                           your active personal access tokens
POST   /api/me/tokens                           { label, isAgent } → token (shown once)
DELETE /api/me/tokens/:id                       revoke

GET    /api/me/anthropic-key                    { hasKey, hint }
PUT    /api/me/anthropic-key                    { key }
DELETE /api/me/anthropic-key

# AI revision (Server-Sent Events streaming)
POST   /api/documents/:id/revise                stream — delta / done / error
POST   /api/documents/:id/revisions             accept a previewed revision

# Agent guide
GET    /SKILL.md                                canonical SKILL.md (raw markdown; /skill.md and /skill also work)

# MCP
*      /mcp                                     streamable HTTP MCP server (Bearer auth)
```

## Architecture notes

- **Comment anchoring** uses character offsets into the rendered markdown's `textContent`. Cloning the source freezes the offsets so they stay valid forever. Agent comments anchor by text-substring; the frontend resolves them to offsets at render time. See [frontend/src/utils/anchor.ts](frontend/src/utils/anchor.ts).
- **Realtime** is a per-document in-memory hub fan-out over SSE. No external pub/sub. Disconnections auto-reconnect via the browser's EventSource.
- **AI revision** streams Anthropic's response straight through to the browser, so you see Claude writing in real time. Backend-side timeout is 10 minutes; client-side abort is 5 minutes.
- **API key encryption**: AES-256-GCM with a random nonce per encryption. The 32-byte master key lives only in `MARKUPMARKDOWN_ENCRYPTION_KEY` (env var / Fly secret), never in MongoDB. Encrypted blobs live in a separate `user_secrets` collection so the field can't accidentally leak via `/api/auth/me`. Hint = first 10 + last 4 characters. Key versioning supports rotation without re-encryption ceremonies.
- **Personal access tokens** are stored as `sha256(token)`; the plaintext is shown once and never logged.
- **SSRF guard**: the URL fetcher refuses to dial RFC1918, loopback, link-local, metadata, and CGNAT IPs at both initial connection and every redirect.
- **Rate limits + body caps + concurrency semaphores** keep a single Fly machine resilient against burst load and abusive clients. See `backend/internal/limits/`.
- **MCP server**: streamable HTTP at `/mcp`, Bearer-auth, every tool routes through the same access checks as the REST API.

## Build checks

```sh
cd backend && go build ./...
cd frontend && npx tsc --noEmit
```

## License

MIT © 2026 [Metavert LLC](https://metavert.io)
