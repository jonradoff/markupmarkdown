# markupmarkdown

**Comment on any markdown file like it's a Google Doc.** Paste a URL, drag-select
text, leave a margin comment. Your teammates see it in real time. Resolve the
thread when you're done — or have Claude apply it for you and produce a clean
new version.

Live: **<https://mumd.metavert.io/>**

---

## The problem

Markdown is where a lot of real thinking lives now — PRDs, design docs, RFCs,
release notes, AI prompts, briefs. But the tools for *reviewing* it are
miserable:

- **GitHub PRs** force every discussion through a code-review workflow. Fine
  for production code, painful for a quick "this paragraph is unclear" on a
  brainstorming doc you haven't even branched yet.
- **HackMD / Notion / Dropbox Paper** lock your content into their format,
  pull you out of `.md`, and require everyone to make an account.
- **Pasting into Google Docs** drops all your formatting and now you have two
  sources of truth.

You just want to drag-select a sentence and leave a comment. Like Google Docs.
On a real markdown file.

## What this is

A tiny self-contained web app:

- **Backend**: one Go binary (~12 MB Alpine image) talking to MongoDB
- **Frontend**: a single React SPA served from the same binary
- **Storage**: per-doc copy in Mongo, that's it — no S3, no Redis, no queue
- **Deploy**: one Fly machine, scales to zero, ~free for personal use

Everything you'd expect from a Google-Docs-style review experience, in a
codebase small enough to read on a Sunday.

## What it does

- **Open any markdown file** — paste a URL (raw URL or `github.com/.../blob/...`,
  we auto-rewrite) or upload from your computer.
- **Drag-select text → comment**. Click the floating "Comment" button, type,
  submit.
- **Threaded replies**, mark-as-done, reopen, edit, delete. Same model your
  team already knows.
- **Realtime sync**: every change propagates to every other open tab in <1s
  via Server-Sent Events. Two people reviewing at the same time just works.
- **Light / dark mode** that respects your system pref.
- **Optional GitHub login** — your avatar and display name flow through
  automatically; private repos become readable (after the org admin approves
  the OAuth app). No login required for public docs.
- **Private docs stay private**: if a doc was cloned from a repo that
  required GitHub auth to read, every subsequent view re-verifies that the
  current user has GitHub access. No access? No content shown.
- **Killer feature — Revise with AI**: bring your own Anthropic API key,
  click *Revise with AI*, and Claude Opus 4.7 produces a new version that
  applies your resolved comment threads. Streams the output live, shows a
  word-level diff, lets you accept or discard. Creates a new doc with a
  parent backlink so revisions form a tree.
- **Download the markdown** at any point.

## Lightweight by design

This isn't a SaaS. The whole stack:

```
fly machine (512 MB)
└── markupmarkdown (Go binary)
    ├── /api/*           — gorilla/mux router
    ├── /                — SPA from /app/web/dist
    └── MongoDB Atlas    — documents, comments, sessions
```

No build-time JavaScript on the server. No background workers. No webhooks.
No analytics SDK. No cookie banner because the only cookie is a session ID.
Bring your own Anthropic key — your usage, your bill, your data.

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

Set it as `MARKUPMARKDOWN_ENCRYPTION_KEY` in `backend/.env` (for dev) and as
a Fly secret (for prod). This key encrypts per-user Anthropic API keys at
rest using AES-256-GCM — without it, the AI-revision feature is disabled
but everything else keeps working.

## Optional: GitHub OAuth

Login + private repos are opt-in. Register an OAuth app at
<https://github.com/settings/applications/new>:

| Field | Value |
|---|---|
| Homepage URL | `http://localhost:4720` (dev) / your prod URL |
| Callback URL | `http://localhost:4721/api/auth/github/callback` (dev) / `<your-prod-url>/api/auth/github/callback` |

Add to env:

```
GITHUB_CLIENT_ID=Iv1.xxxxxxxxxxxxxxxx
GITHUB_CLIENT_SECRET=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Scope `repo` enables reading private files; drop to `read:user user:email`
if you only want public docs.

## Deploy to Fly.io

```sh
fly launch              # or `fly apps create` if you already have a name
fly secrets set MONGODB_URI="mongodb+srv://..."
fly secrets set MARKUPMARKDOWN_ENCRYPTION_KEY="$(openssl rand -hex 32)"
# optional, after registering the OAuth app:
fly secrets set GITHUB_CLIENT_ID=... GITHUB_CLIENT_SECRET=...
fly deploy
```

The included `Dockerfile` is a 3-stage build (frontend → backend → alpine
runtime). Final image is ~12 MB.

## API surface

```
POST   /api/documents                                { url, title? } | { content, title }
GET    /api/documents
GET    /api/documents/:id
PATCH  /api/documents/:id                            { title }
DELETE /api/documents/:id

GET    /api/documents/:id/comments
POST   /api/documents/:id/comments                   { anchor:{start,end,exact}, body, author }
GET    /api/documents/:id/events                     SSE stream — comments-updated events

PATCH  /api/comments/:id                             { body }
DELETE /api/comments/:id
POST   /api/comments/:id/resolve                     { author }
POST   /api/comments/:id/reopen
POST   /api/comments/:id/replies                     { body, author }
PATCH  /api/comments/:id/replies/:replyId            { body }
DELETE /api/comments/:id/replies/:replyId

GET    /api/auth/config                              { githubEnabled, githubClientId }
GET    /api/auth/me                                  { user }
GET    /api/auth/github/login?redirect=/d/...
GET    /api/auth/github/callback
POST   /api/auth/logout

GET    /api/me/anthropic-key                         { hasKey, hint }
PUT    /api/me/anthropic-key                         { key }
DELETE /api/me/anthropic-key

POST   /api/documents/:id/revise                     SSE stream — delta / done / error
POST   /api/documents/:id/revisions                  { content, model, tokensIn, tokensOut, appliedCommentIds }
```

## Architecture notes

- **Comment anchoring** uses character offsets into the rendered markdown's
  `textContent`. Cloning the source freezes the offsets so they stay valid
  forever. See [frontend/src/utils/anchor.ts](frontend/src/utils/anchor.ts).
- **Realtime** is a per-document in-memory hub fan-out over SSE. No external
  pub/sub. Disconnections auto-reconnect via the browser's EventSource.
- **AI revision** streams Anthropic's response straight through to the
  browser, so you see Claude writing in real time. Backend-side timeout is
  10 minutes; client-side abort is 5 minutes.
- **API key encryption**: AES-256-GCM with a random nonce per encryption. The
  32-byte master key lives only in `MARKUPMARKDOWN_ENCRYPTION_KEY` (env var
  / Fly secret), never in MongoDB. Encrypted blobs live in a separate
  `user_secrets` collection so the field can't accidentally leak via
  `/api/auth/me`. Hint = first 10 + last 4 characters.

## Build checks

```sh
cd backend && go build ./...
cd frontend && npx tsc --noEmit
```

## License

MIT © 2026 [Metavert LLC](https://metavert.io)
