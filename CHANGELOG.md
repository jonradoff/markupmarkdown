# Changelog

All notable changes to **markupmarkdown** are recorded here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **GitHub source-change detection** — for docs cloned from GitHub, every
  open shows a banner the moment the upstream file has new commits.
  Comments still pinned to text that survived the edit are auto re-anchored
  on Sync; the rest move to a *Comments without anchors* section below the
  doc with a manual drag-select re-anchor flow.
- **Doc-level comments** — pin a comment to the whole document instead of
  a text span. Doc-level pins live in their own sidebar section and survive
  any source change.
- **Manual re-anchor flow** — pick an orphan comment, enter re-anchor mode,
  drag-select new text in the doc, click *Re-anchor here* in the popover.
  Or convert the comment to a doc-level pin in one click.
- **Per-card Prev/Next strip** — every comment card now has its own
  Prev/Next pair under the body, so you can step to an adjacent thread
  without scrolling back to the sticky bar at the top of the sidebar.
- **In-document anchor links** — clicking a Markdown link that points at a
  same-doc heading (`[Section](#section-name)`) smooth-scrolls to that
  heading instead of leaving the page. Headings clear the sticky header
  automatically via `scroll-margin-top`.
- **Open original** action — when you're reading an AI-revised child doc
  and the upstream GitHub source has changed, the banner offers to jump to
  the original (where syncing makes sense) instead of clobbering the
  revision with raw upstream content.
- Live changelog (this file) and `CHANGELOG.md` link in the README.

### Changed

- **Bell badge decrements as you read** — viewing a comment now marks any
  pending notifications for that comment as read, regardless of whether
  you arrived via the bell. The badge stays honest as you scroll through
  unread threads.
- **Submitting a comment no longer scrolls the page to the top.** The
  fresh highlight colour is sufficient visual confirmation; the previous
  behaviour yanked you off the text you were just commenting on.
- **Drift checks run on mount, on tab focus, and every 2 min while
  visible**, with the server-side TTL dropped to 60 s. A teammate's
  upstream commit shows up within a couple minutes of your tab focusing
  instead of waiting for a page reload.
- **Access re-verification on every drift check** — busts the cached
  GitHub access state so a user removed from a private repo gets booted
  to the access-denied page within one check cycle.
- Re-anchor comparisons run against the rendered plain text (not the raw
  markdown source), so comments anchored to text spanning a `**bold**`,
  `_italic_`, or `` `code` `` boundary stay clean across upstream edits.

### Fixed

- Comments whose quoted text spanned a Markdown formatting marker
  (`**bold**`, etc.) were incorrectly orphaned on Sync even when the
  underlying text was unchanged. Now matched against the rendered text.
- Source-drift detection ignored child revisions, so a viewer reading an
  AI-revised doc never saw the banner when the original GitHub file
  changed. Drift now evaluates on the revision-chain root; children
  inherit the state and link back to the original for sync.
- Legacy docs (ingested before SHA tracking) were stamped with the
  current upstream SHA as baseline on first check, hiding any drift the
  upstream had already accumulated. Now we compute the git blob SHA of
  the stored content and compare against upstream — the banner appears
  immediately if the upstream has already moved on.
- Coverage badge stuck on *unknown* after the repo went public — CI
  uploads coverage tokenlessly now.

## [0.1.0] — 2026-06-03

Initial public release.

### Added

- **Google-Docs-style commenting** on any Markdown file. Paste a GitHub
  URL or upload a local `.md` — drag-select text in the rendered doc,
  leave a margin comment, get threaded replies, mark-as-done, reopen,
  edit, and delete.
- **Realtime sync** between every open tab on the same doc via
  Server-Sent Events. Sub-second propagation; auto-reconnect.
- **@-mentions** with autocomplete scoped to the people who've actually
  opened this doc (commenters ∪ viewers). Bell icon shows an in-app
  notification with a deep link to the relevant comment.
- **Unread filter** with a count badge — only the threads that have new
  activity since your last visit, anchored on a per-(doc, user) view
  marker.
- **Step through comments** with `j` / `k` (or `↑` / `↓`, or the floating
  Prev / Next bar). The position counter respects the active filter.
- **GitHub OAuth sign-in.** Optional — use your avatar and display name
  automatically, and unlock private repo files. Private docs are gated
  on every read by re-verifying current GitHub access to the source
  repo; if you lose access, you stop seeing content (and the title)
  immediately.
- **AI revision via Claude Opus 4.7.** Bring your own Anthropic API key
  (stored encrypted at rest with AES-256-GCM, deletable any time).
  Pick which resolved comments to apply; watch Claude write the
  revised doc as live-streaming Markdown; word-level diff preview;
  saving creates a new child document so revisions form a tree.
- **MCP server at `/mcp`** for agents — read documents, leave threads
  anchored to text spans, reply to humans, resolve, and trigger AI
  revisions (with explicit human sign-off). The same access checks,
  rate limits, and validation apply as the REST API; no agent-only
  fast path.
- **Personal access tokens** (`mmk_…`) for scripts and agents, with
  scope (`read` / `write` / `admin`), optional expiry, label, and a
  per-token activity log. Stored as SHA-256 hashes; plaintext shown
  once at creation.
- **Agent identity badges** — comments and replies written via a token
  get a visible bot badge; the token's owner shows on hover. Renaming
  a token updates everywhere it has commented (display fields resolve
  at read time).
- **Soft-delete with 30-day recovery.** Deleted docs sit in Trash and
  can be restored before a daily purge sweep removes them for good.
- **Share dialog** with explicit access copy (private docs warn you
  before you send the URL).
- **Open Graph link unfurls** so shared URLs look meaningful in Slack,
  iMessage, Discord, X. Private docs share a generic card so titles
  don't leak.
- **Light / dark theme** that respects your system preference.
- **`/SKILL.md`** — canonical agent integration guide served as raw
  markdown (`/SKILL.md`, `/skill.md`, and `/skill` all work), embedded
  into the Go binary at compile time so the deployed URL is always in
  sync with the binary.

[Unreleased]: https://github.com/jonradoff/markupmarkdown/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/jonradoff/markupmarkdown/releases/tag/v0.1.0
