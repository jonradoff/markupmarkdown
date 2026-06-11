# CLAUDE.md

Notes for anyone — human or AI — picking up this codebase.

The project is **markupmarkdown**: a Google-Docs-style commenting app for markdown files, with an MCP server so agents can join the same review loop as people. If you're forking it and want to know what's load-bearing, what's idiomatic for this project, and what'll bite you if you change it without thinking — that's what's below.

---

## Mental model

This is one Go binary + one React SPA + MongoDB Atlas. No queue, no S3, no Redis, no separate worker. The intentional design is "small enough to read on a Sunday." Don't add infrastructure unless the feature genuinely demands it.

The product is now best described as **"Google Docs for Markdown."** Originally it shipped as a commenting-only tool; the editor + GitHub round-trip (manual revisions, soft edit lock, 3-way merge, push as PR or direct commit) brought it to a full review-and-ship surface. When you're scoping a new feature, anchor on that mental model — what would Google Docs do here? — but always preserve the design principle: edits happen on the actual markdown text, not a visual mirror. The file in the repo stays the source of truth.

Three audiences share the same data model:

1. **Humans** — cookie session (`mm_session`), full account privileges.
2. **Scripts** — Personal Access Tokens (`mmk_…`), scope-restricted, used via REST.
3. **Agents** — same Personal Access Tokens, used via the MCP server at `/mcp`. Any Bearer-token request is treated as agent-authored (see `actorKindFor` in [backend/internal/api/comments.go](backend/internal/api/comments.go)).

All three paths route through the same access checks, rate limits, and validation. There is no "agent-only" code path that skips guards.

---

## Layout

```
backend/
├── cmd/markupmarkdown/main.go     # entrypoint (config load, store init, register routes)
├── internal/
│   ├── api/                       # HTTP handlers (gorilla/mux)
│   │   ├── api.go                 # API struct + route table — start here when you need to find a handler
│   │   ├── auth.go                # cookie sessions, GitHub OAuth, tokenInfo plumbing
│   │   ├── scope.go               # enforceScope helper (single source of truth for read/write/admin)
│   │   ├── validate.go            # shared comment/reply/anchor validation (REST + MCP share this)
│   │   ├── tokens.go              # personal-access-token CRUD + activity endpoint
│   │   ├── tokenlog.go            # sampled per-token activity logging (~1/min per action)
│   │   ├── comments.go            # comment + reply handlers, agent identity stamping/resolution
│   │   ├── documents.go           # doc CRUD, URL ingest, soft delete
│   │   ├── revisions.go           # AI revision preview (SSE) + accept
│   │   ├── mcpapi.go              # implements mcpserver.API — bridge from MCP into the rest of API
│   │   ├── plaintext_cache.go     # memoized goldmark plain-text extraction for MCP anchoring
│   │   ├── events.go              # SSE hub broadcasts ("comments-updated", "doc-updated", …)
│   │   ├── limits.go              # all rate-limit buckets + concurrency caps live here
│   │   ├── notifications.go       # @-mentions / reply notifications
│   │   ├── access.go              # checkDocAccess / checkCommentAccess (used by every protected handler)
│   │   ├── secrets.go             # Anthropic API key storage (AES-GCM via secrets.Vault)
│   │   └── static.go              # SPA handler with OG meta injection
│   ├── mcpserver/                 # Model Context Protocol server, mounted at /mcp
│   ├── store/                     # MongoDB collection accessors + queries (no business logic)
│   ├── models/                    # Go structs with bson + json tags (the source of truth for shapes)
│   ├── render/                    # goldmark wrappers (HTML + plain text + safe sanitization)
│   ├── ai/                        # Anthropic Messages API client for AI revision
│   ├── auth/                      # GitHub OAuth helpers
│   ├── secrets/                   # AES-GCM Vault for per-user secrets
│   ├── safefetch/                 # SSRF-guarded outbound HTTP (used by URL ingest)
│   ├── limits/                    # token-bucket + counter + per-key semaphore primitives
│   ├── httperr/                   # internal error sanitization (log full error, return {id, generic msg})
│   └── config/                    # YAML + env config loader
│
frontend/
├── src/
│   ├── App.tsx, main.tsx          # router + theme + root layout
│   ├── api.ts                     # typed REST client (every endpoint goes through here)
│   ├── types.ts                   # frontend types — keep in sync with backend models
│   ├── auth.tsx                   # auth context provider
│   ├── pages/                     # Home, Document, Trash
│   └── components/                # SPA components (TokensModal, CommentCard, ReviseModal, etc.)
│
skills/markupmarkdown/SKILL.md     # canonical agent integration guide
                                   # also embedded into the Go binary and served at /SKILL.md
fly.toml + Dockerfile              # single-process production deploy
```

---

## Conventions

### Build + lint + test checks (run before every commit)

```
cd backend  && go build ./... && golangci-lint run ./... && go test ./...
cd frontend && npx tsc --noEmit && npm run test:unit
```

Everything must be clean. Tests + lint are both first-line defenses, not optional. Coverage target on the audited Go surface is 80%; CI fails if a PR drops below.

### Dev servers

- Backend: `cd backend && go run ./cmd/markupmarkdown` (default port 4721).
- Frontend: `cd frontend && npm run dev` (Vite, port 4720, proxies `/api/*` and `/mcp` to backend).

### Deploy

Production runs on Fly: app `markupmarkdown`, primary region `ewr`. Deploy with `fly deploy` from the repo root. The Dockerfile is a two-stage build (Go binary + Vite assets baked into `/app/web/dist`).

### Database

MongoDB Atlas, database `markupmarkdown` (dev defaults; see `backend/internal/config/`). **Never drop or clear the database autonomously.** Live user data. Only the user does destructive Mongo ops.

---

## Rules the codebase enforces

These aren't aspirations — they're enforced in code today, with tests behind them. If you change a handler and skip one, the build catches it. The list below explains *why*, so you can judge edge cases when adding something new.

### 1. Tokens never elevate

A request authenticated via Bearer token can never do more than the token's stored scope allows. Cookie sessions always satisfy any scope check. The hierarchy is `admin > write > read`:

| Scope | Can | Cannot |
|---|---|---|
| `read`  | list docs, read comments, list mention candidates, list notifications | write anything |
| `write` | read + add comments/replies, resolve/reopen threads, preview AI revisions (`revise_with_ai accept=false`) | delete docs, accept AI revisions, rename docs |
| `admin` | write + delete docs, accept AI revisions, rename docs | mint new tokens (always cookie-only) |

All scope checks go through `(a *API).enforceScope(w, r, models.TokenScope…)` in [backend/internal/api/scope.go](backend/internal/api/scope.go). When you add a new write handler, add an `enforceScope` call. Pick the level by asking "if this leaks, what's the worst that happens?":

- destructive or creates new docs → `admin`
- mutates existing comments / replies / resolution state → `write`
- just reads → no enforcement needed (auth alone is enough)

A request that already has a Bearer token must not be able to *mint* a new token; both `createToken` and `updateToken` explicitly reject Bearer-authed callers. Keep that property.

### 2. Cookie-session-only endpoints

Some endpoints must never be reachable via a token, even with admin scope, because the token surface itself could be abused:

- `POST /api/me/tokens` (create token)
- `PATCH /api/me/tokens/:id` (rename / re-scope)
- `DELETE /api/me/tokens/:id` (revoke)
- `GET  /api/me/tokens/:id/activity`
- `PUT  /api/me/anthropic-key` (store secret)

Pattern: `if _, hasToken := tokenInfoFromRequest(r); hasToken { 403 }`. Don't loosen this.

### 3. Bot identity is dynamic, not snapshotted

Agent-authored comments and replies store the `token_id` they were written under; the display name (`Author`), owner display (`OwnerName`, `OwnerLogin`), and avatar are **resolved at read time** by `(a *API).resolveAgentIdentities`. This is what makes "rename a token → all old comments update" work.

When you add a new write path that produces comment-like content:
- Call `stampAgentWrite` / `stampAgentWriteReply` if the caller has a token.
- Call `a.resolveAgentIdentity(ctx, c)` before returning the object to the client.
- Make sure the read path you return through *also* calls `resolveAgentIdentities`.

When you add a new path that mutates a token's display (e.g. label rename), it must broadcast `comments-updated` to every doc the token has authored on. Use `store.DistinctDocIDsForToken`. See `updateToken` for the pattern.

### 4. Validation is shared between REST and MCP

`ValidateCommentBody`, `ValidateReplyBody`, `ValidateAnchor` live in [backend/internal/api/validate.go](backend/internal/api/validate.go). REST handlers call them directly. The MCP server reaches them via the `API` interface methods exposed in [backend/internal/api/mcpapi.go](backend/internal/api/mcpapi.go).

If you add a new field with length/format rules, add the helper here and call it from both surfaces. Field-length caps are constants in [backend/internal/api/limits.go](backend/internal/api/limits.go).

### 5. Rate limits cover REST and MCP

The same `rlComment`, `rlRevise`, `reviseSlots` buckets back both surfaces. The MCP tool handlers call `h.api.AllowCommentRate`, `h.api.AllowReviseRate`, `h.api.AcquireReviseSlot` — methods on `*API` that delegate to the same primitives REST uses. **Don't give MCP its own bucket** — a script alternating REST and MCP would otherwise get double the budget.

When you add a new throttled action, wire one bucket in `initLimits` and expose it through the `mcpserver.API` interface if any MCP tool also performs it.

### 6. Per-token activity is sampled

`logTokenAction(ctx, tokenID, action, docID)` writes to the `token_events` collection but is sampled to at most one event per (tokenID, action) per minute. The collection has a 30-day Mongo TTL index. Add a `logTokenAction` call whenever you ship a new agent-callable write tool. Use action strings like `comment.create`, `reply.create`, `comment.resolve`, `revision.accept` — verb-with-namespace.

### 7. Error sanitization at the MCP boundary

User-friendly errors (`"document not found"`, `"quoted_text not found in document"`) pass through verbatim. Mongo / Anthropic internals must not. The pattern is `sanitizeStoreErr(where, err)` in [backend/internal/api/mcpapi.go](backend/internal/api/mcpapi.go): it logs the full error under a generated ID and returns `"…internal error… (id=X)"` to the caller. Use it on every direct store call in `mcpapi.go`.

For REST, the equivalent is `internalError(w, where, err)` from [backend/internal/api/limits.go](backend/internal/api/limits.go).

### 8. Access checks always come first

Every comment / document mutation handler calls `checkDocAccess` or `checkCommentAccess` before doing anything else. These check private-repo GitHub access against `repoAccessCache`. If you add a new handler, follow the pattern:

```go
doc, accErr := a.checkDocAccess(r, docID)
if accErr != nil {
    a.writeAccessError(w, r, accErr)
    return
}
if !a.enforceScope(w, r, models.TokenScopeWrite) { return }
if !a.enforceRate(w, r, a.rlSomething, "…") { return }
capBody(w, r, maxBodySomething)
// …
```

Order matters: access first (cheapest check + 404 doesn't reveal scope info), then scope, then rate, then body cap.

### 9. Soft delete + 30-day retention

`deleteDocument` only sets `DeletedAt`. A daily `StartPurgeSweep` actually removes rows after `TrashRetention` (30 days). The Trash UI surfaces these. Never hard-delete user docs from a handler — the user's restore flow depends on the soft state.

### 10. SSRF guard on URL ingest

URL fetches go through `(a *API).fetchURL` which uses a custom `DialContext` that rejects private/internal IPs. When adding a new URL-ingest path (Github upload, raw URL, etc.), route it through `fetchURL`. Never use a stock `http.Get`.

### 11. Secrets at rest

User Anthropic API keys are AES-256-GCM encrypted via `secrets.Vault` (master key from `ENCRYPTION_MASTER_KEY`). Plaintext appears only in memory during a request. If you store any new per-user credential, follow the same pattern (`encryptedKeyForUser` / `decryptedKeyForUser`), don't invent something else.

### 12. SKILL.md is the canonical agent guide

[skills/markupmarkdown/SKILL.md](skills/markupmarkdown/SKILL.md) is embedded into the Go binary at compile time (via `//go:embed`) and served raw at `/SKILL.md`, `/skill.md`, and `/skill` (all `text/markdown`). If you change the MCP tool surface or token scopes, update SKILL.md in the same commit. The TokensModal links to it; agents are instructed to read it.

### 13. Comment & reply edit/delete is author-only

Beyond doc-access + scope, [comments.go](backend/internal/api/comments.go) `patchComment` / `deleteComment` / `updateReply` / `deleteReply` call `requireMineComment` or `requireMineReply`. The check is `AuthorID == currentUser.ID`. Agent comments stamp `AuthorID` to the token's owning user, so the same equality covers "I wrote this" and "a bot I own wrote this." Even an admin-scope token cannot edit or delete content authored by a different user. The frontend's `comment.mine` boolean is the server-computed answer; the UI gates the edit/delete buttons on it. Keep both in sync.

### 14. Credential-setting endpoints are cookie-only

`POST/PATCH/DELETE /api/me/tokens*`, `PUT/DELETE /api/me/anthropic-key`, and any other endpoint that stores or rotates a user credential must reject Bearer-token auth with 403. Pattern: `if _, hasToken := tokenInfoFromRequest(r); hasToken { 403 }`. A leaked token must not be able to swap the user's Anthropic key, mint new tokens, or change scopes on existing ones.

## Operational notes

These are properties of the running system that won't show up in code review but are worth knowing before scaling or debugging:

- **`repoAccessCache` is per-process with a 2-minute TTL.** [access.go](backend/internal/api/access.go) — if a user is removed from a GitHub org, they keep seeing private docs for up to 2 min. A multi-machine deploy doubles that staleness per machine because the cache isn't shared.
- **The SSE hub is in-process.** [hub.go](backend/internal/api/hub.go) — `Broadcast` only reaches subscribers connected to *this* machine. **Single-machine is the supported configuration** (Fly `min_machines_running = 1`, no horizontal replicas). If we ever scale out, the chosen fan-out is **MongoDB Atlas change streams**, not Redis — write event records to a capped `events` collection, have each machine watch via `Watch()` change-streams and re-broadcast to its local SSE subscribers. Re-uses infrastructure we already pay for (Atlas) and avoids introducing a separate pubsub dependency. Per-machine in-process caches (repoAccessCache, plaintext, token-event sampler) would need a similar treatment.
- **The PlainText cache is in-process.** [plaintext_cache.go](backend/internal/api/plaintext_cache.go) — same scope as the hub. Cache hit rates drop linearly with replica count; not a correctness issue.
- **The token-event sampler is in-process.** [tokenlog.go](backend/internal/api/tokenlog.go) — on multi-machine deploys, the per-action sampling effectively becomes "1/min per (token, action, machine)" rather than globally 1/min. Acceptable for a small fleet; if you scale up, move the sampler state to Mongo.
- **Hub channel lifetime.** Subscribers receive on `sub.Events()` and *also* watch `sub.Done()` for shutdown. The send channel is intentionally never closed (closing it races with Broadcast's send and would panic).

---

## How to add a new feature safely

When you add a new piece of functionality, walk this checklist:

1. **Model.** Add or extend a struct in [backend/internal/models/models.go](backend/internal/models/models.go). Bson tags drive Mongo, json tags drive REST.
2. **Store.** Add accessor methods in [backend/internal/store/store.go](backend/internal/store/store.go). Keep them dumb — no business logic. Index needs go in `ensureIndexes`.
3. **Handler.** Add to the right `internal/api/*.go` file. Follow the order in rule #8 above.
4. **Wire route.** Register in `(a *API).Register` in [backend/internal/api/api.go](backend/internal/api/api.go).
5. **MCP surface?** If agents should be able to do this, also add a tool to [backend/internal/mcpserver/server.go](backend/internal/mcpserver/server.go) and a method to the `API` interface. Add scope, rate, validation, and `LogTokenAction`.
6. **Frontend types.** Mirror the new model in [frontend/src/types.ts](frontend/src/types.ts).
7. **Frontend client.** Add the call to [frontend/src/api.ts](frontend/src/api.ts).
8. **UI.** Add or extend a component under [frontend/src/components/](frontend/src/components/) or a page.
9. **Realtime?** If the change is user-visible to other open viewers, broadcast on the SSE hub: `a.hub.Broadcast(docID, "comments-updated")` (or a new event name — keep them kebab-case verbs).
10. **SKILL.md update** if you changed the agent surface.
11. **Build check both sides.**

---

## Editor surface (CodeMirror 6)

The editor is a forwardRef'd CodeMirror 6 instance in [frontend/src/components/EditorPane.tsx](frontend/src/components/EditorPane.tsx) with a few properties worth knowing before you touch it:

- **No internal scroll.** The CodeMirror theme extension sets `'&': { height: 'auto', maxHeight: 'none' }` and `'.cm-scroller': { overflow: 'visible' }` so the editor flows in the page-level scroll. Don't re-enable inner scrolling — it breaks the sticky toolbar AND the comment-card alignment (sticky binds to the nearest scroll container, and the cards-layout effect assumes editor coords share the window viewport's frame).
- **Page-level scroll is load-bearing for layout.** The cards-layout effect in `Document.tsx` listens for `window.scroll` (rAF-throttled) to re-position anchored comment cards. If you ever add an inner-scrolling column anywhere on the Document page, also re-layout on its scroll, otherwise comments will drift out of sync with their highlights.
- **Sidebar is `sticky top-0 h-screen self-start`.** It stays pinned while the body scrolls; its own `overflow-y-auto` handles long comment lists. Don't change this unless you have a layout reason to — the sticky toolbar fix relies on the sidebar not capturing scroll.
- **`EditorPaneHandle` exposes two methods.** `coordsForAnchor(exact)` and `scrollAnchorIntoView(exact)`. The `Document.tsx` page calls them on activeId change and during the cards-layout pass. Both fall back via `lineBlockAt + contentTop` when CodeMirror's `coordsAtPos` returns null for off-screen anchors — don't drop that fallback or off-screen comments stop aligning.
- **Theme tracks app theme.** A MutationObserver on `<html>.class` flips the CodeMirror theme between `oneDark` and `"light"`. If you swap the global theme switch, update this observer.
- **`applyWrap` in [utils/markdownActions.ts](frontend/src/utils/markdownActions.ts) handles BOTH overshoot cases.** Case A: markers sit OUTSIDE the selection. Case B: markers are INCLUDED in the selection (user dragged past the `**`). Both strip one layer; selection adjustments are deliberate. Don't "simplify" by removing Case B — selection overshoot is the common user pattern.

## Anchored card layout (Document.tsx)

The card layout has three invariants you'll regret breaking:

1. **The sidebar must not scroll programmatically in response to `activeId` changes.** Doing so feeds `sidebar.scrollTop` back into `containerRect.top`, which the layout effect uses to compute `desiredTop = editorAnchorY - containerTop`. Every Next click would then shift cards further down the container, the container's `minHeight` would grow to fit, the sidebar would become internally scrollable, and the cards would march off into space. Bringing a comment into view is the body-scroll's job (via `scrollAnchorIntoView` on the editor in edit mode, `window.scrollTo` on the rendered highlight rect in view mode). The body scroll naturally brings the anchored card with it.
2. **`visibleComments` is sorted by `anchor.start`, but MCP-added agent comments all store `anchor.start = 0`** — they're resolved against the doc text at render time. The cards-layout effect explicitly re-sorts by computed `desiredTop` before `relaxAnchors` runs. Don't drop this sort or agent-authored comments will stack in creation order rather than document order.
3. **The cards container's `minHeight` is dynamic** — it expands to fit the lowest card so the linear sidebar sections below (orphans, doc-level, composer) don't overlap. That's fine as long as the sidebar doesn't internally scroll in response (see #1). If you ever need cards positioned beyond viewport height, prefer hiding them (via `visibility: hidden` when far off-screen) over making the sidebar scrollable.
4. **Only in-viewport (±200 px) comments lay out.** Cards whose anchor is far above or far below the viewport are filtered out of the layout pass (the active card is exempted). Without this, above-viewport cards clamp to `minTop = 0` and stack — their combined heights then push the active card hundreds of pixels off its highlighted line. If you change this filter, re-test the "I press Next and the active card lands somewhere weird" failure mode.

## Edit-mode scroll-to-anchor

`scrollAnchorIntoView` on the EditorPane handle is the **single** driver for scrolling the page to an active comment in edit mode. Two important nuances:

- **Don't add a second scroller.** The EditorPane's own `useEffect` on `activeAnchorExact` used to also call `window.scrollTo`, and it fought with the Document.tsx caller. One scroll surface, one driver — keep it that way.
- **Two-pass measurement.** CodeMirror uses estimated line heights for content outside its render viewport. A single `coordsAtPos` for a far-off line can be tens to hundreds of pixels wrong, and the error compounds as content scrolls into view and the height map refines. The handle does an instant nudge to the estimated target, then on the second rAF takes an authoritative measurement and smooth-scrolls the rest of the way. Don't simplify this to a single smooth-scroll — the user sees the page land short of the active comment.

## Edit lock + manual revisions

- **The soft edit lock is in-process** ([backend/internal/api/editlock.go](backend/internal/api/editlock.go)). Map keyed by docID with a TTL. On a multi-machine deploy, two users on different machines could both hold the lock until SSE catches up — acceptable for a small fleet, not for higher concurrency. Move it to Mongo with a TTL index if you ever scale out.
- **Manual edits go through `POST /api/documents/:id/manual-revisions`**, which creates a new child doc with `revision_meta.model = "manual"` and stamps `ancestorContent` so a future merge has a 3-way base. Carry-forward of unresolved comments to the new doc is automatic — don't duplicate that logic in client code.
- **Edit mode requires a real signed-in user.** Author display name in localStorage is enough to comment; the edit lock requires `currentUser != nil` (cookie session OR bearer token). Bearer tokens that go through `edit_document` over MCP work fine.

## GitHub round-trip (pushback)

- **The pushback path uses the user's OAuth token.** The same access token already in the session signs the GitHub Contents + Pulls API calls; there is no separate PAT or app installation. If you ever change the OAuth scope, audit pushback at the same time — `repo` is required for write.
- **PR mode and direct-commit mode share `pushback.go`.** PR mode opens a new branch, commits, opens the PR. Direct-commit pushes straight to `TargetBranch`. Branch-protection rejection is surfaced verbatim — do not retry or fall back automatically. The user picks the mode in the modal.
- **MCP only exposes PR mode** ([handlers_extra.go](backend/internal/mcpserver/handlers_extra.go)). Direct commits over MCP are intentionally absent because branch protection is a human-policy concern. Don't add a direct-commit MCP tool without a strong reason.

## Non-obvious gotchas (in the order someone tripped on them)

- **`OwnerName` / `OwnerLogin` on `Comment` and `Reply` are `bson:"-"`.** They're populated at read time, never stored. Adding them to a write path or migration is a bug.
- **Adaptive thinking on Opus 4.7** is configured in the AI client. Don't pass `budget_tokens` (Opus 4.7 will 400). The `ai` package handles this.
- **`GetAPITokenByHash` already filters `revoked_at`.** Don't also filter at handler level; you'll skip the not-found branch.
- **`tokenInfoFromRequest` only returns ok=true if the request came in via Bearer.** That's how `actorKindFor` distinguishes agent vs human. Cookie sessions return `(zero, false)`.
- **`effectiveScope` returns `admin` for cookie sessions**, which is why owners can do everything their tokens can't.
- **The SSE hub is in-process.** If you ever shard the backend, this breaks. Currently one Fly machine = no problem.
- **`PlainText` is goldmark-walked; for big docs it's hot in MCP `add_comment`** — that's why there's a `plainTextFor` cache keyed by `(docID, updatedAt)`. Bypassing it (calling `render.PlainText` directly inside agent paths) wastes CPU.
- **`fetchURL` rewrites `github.com/.../blob/...` to `raw.githubusercontent.com`** before fetching. Tests that depend on the original URL will be confused.
- **The Vite dev proxy** must include `/mcp` (not just `/api/*`) for MCP work from a dev frontend.
- **TokensModal's expiry "Never" option sends `-1`**, not `0`. `0` means "use server default" (90 days).

---

## When in doubt

Read the existing handler in the same file. The patterns are consistent — copying one and adapting it is almost always the right move. Don't introduce a new pattern unless you've found three places where the existing one doesn't fit.

If you find an unexpected directory, file, or branch — investigate before deleting. It may be in-progress work.
