# Changelog

All notable changes to **markupmarkdown** are recorded here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Index items are cached server-side; explicit Refresh button.** First
  view of an index does a live GitHub spider; subsequent visits load
  from a `index_items` Mongo cache (one row per index, items stored
  as JSON bytes for fast read). A new circular-arrow Refresh button in
  the index header re-spiders on demand and replaces the cache.
  Private items are filtered to the original scanner's audience so a
  cached org listing never leaks private file names to other viewers.
  Stops the "re-index every time I open the page" surprise.
- **Human-readable URL system.** The SPA now accepts three URL shapes
  as first-class addresses for GitHub markdown:
  - `/owner/repo/blob/ref/path` → individual document (auto-clones if
    not yet ingested; otherwise resolves to the existing chain leaf)
  - `/owner/repo` → repo index
  - `/owner` → user or org index
  So `https://mumd.metavert.io/beamable/CrmDesign/blob/main/WINGMAN_PRD.md`
  Just Works as a shareable link. The legacy `/d/:id` and `/i/:id`
  URLs still resolve, but a `replaceState` swaps them to the canonical
  human path on mount so the address bar always reads the way the
  user pasted it. Backed by a new `GET /api/documents/by-source`
  endpoint that deduplicates against existing docs (so two people
  pasting the same blob URL land on the same place — comments
  aggregate instead of fracturing across N parallel clones).
- **Favicon.** The canonical Markdown mark by Dustin Curtis (CC0,
  used by GitHub / VS Code / CommonMark) centered in a 256×256
  rounded square so it reads cleanly at 16×16 / 32×32. Dark-mode
  aware via the `prefers-color-scheme` media query in the SVG.
- **Live progress on index pages.** The "you're staring at Loading…"
  problem during a big org spider is fixed three ways: (a) ProgressBanner
  renders from frame 1 (before the meta event arrives), (b) the home
  submit button reads "Looking up GitHub…" for index targets so the
  POST round-trip isn't dead air, (c) the banner now includes a live
  activity log of the last 8 scanned repos (newest at top, font-mono,
  fading opacity).
- **Live progress + parallel scanning for index materialization.** Org
  and user-profile indexes now stream their results via an SSE channel
  (`GET /api/indexes/:id/stream`) so the user sees "Scanning 47/142
  repos…" with a per-repo progress bar instead of staring at "Loading…"
  for 30 seconds while a 150-repo org spider runs. The per-repo
  fetches fan out across a worker pool of 8 — beamable's ~150 repos
  now complete in under 10 s instead of 60+. POST `/api/indexes`
  returns the index meta immediately (no items) so the home-page form
  navigates straight to the index page, where progress UI takes over.
  Plain `GET /api/indexes/:id` still materializes synchronously for
  API consumers and as a fallback if the stream errors.
- **Filename-filter tabs on index pages.** Save up to 5 case-insensitive
  substring filters (`claude.md`, `_PRD`, etc.) as named chips along
  the top of the listing. An "All" chip is always present. Tabs are
  per-(browser, index) and persisted in localStorage; the last-active
  tab reopens on return. Each tab shows the match count next to its
  label. Hit × on a chip to remove it.
- **Pinned default filter (owner-only).** The index creator can pin one
  of their tabs (or "All") as the default view for share-link
  visitors. First-time visitors land on the pinned filter; once they
  pick their own tab, their localStorage choice takes over. Backed by
  a new `defaultFilter` field on the Index model + a `defaultFilter`
  argument on `PATCH /api/indexes/:id`. Owner sees a pin/unpin
  button on each tab; everyone sees a filled pin icon on the pinned
  tab.
- **"Forget" button on docs + indexes.** Hides an item from MY home
  list without deleting it for everyone. Distinct from Delete (which
  soft-deletes globally). Backed by a new `hidden_items` collection
  keyed on `(user_id, kind, item_id)`. For docs, the marker is keyed
  on the chain root so future revisions of a forgotten chain don't
  re-surface. Endpoints: `POST /api/documents/:id/forget`,
  `POST /api/indexes/:id/forget`. listDocuments + listMyIndexes
  filter against the marker so the action is local to the calling
  user.
- **Owner/repo pill on "Your documents" rows.** Each doc entry now
  carries a `owner/repo` chip next to the title so similarly-named
  files (PRD.md, README.md, …) are distinguishable at a glance
  instead of buried in the fine-print path line. GitHub-sourced docs
  only; uploads stay unchanged.
- **Markdown indexes — shareable listings of `.md` files anchored to a
  GitHub URL.** Three target shapes are recognized at the home-page
  URL bar:
  - `github.com/owner/repo` → repo index (every `.md` in the repo's
    git tree, one round-trip via the recursive trees API).
  - `github.com/owner` → user *or* org index, disambiguated via
    `/users/{name}` and folded into the right `/users/.../repos` or
    `/orgs/.../repos` listing. Lists each repo's **top-level** `.md`
    files alongside the repo it belongs to (grouped in the UI).
  Indexes live at their own stable URL (`/i/{slug}`), are shareable,
  and items are computed live on every view using the viewer's
  GitHub token — so different viewers may see different listings if
  their repo access differs. Private repo indexes re-verify access
  on every read; private repos in user/org listings are silently
  filtered to what the viewer can see (no leakage). Archived repos
  are excluded from user/org listings by default. The home page
  gains a "Your indexes" section above "Your documents" so a saved
  index is the natural jumping-off point for browsing a team's
  markdown library. Backend: new `indexes` collection + handlers at
  `POST/GET/PATCH/DELETE /api/indexes/:id` and
  `GET /api/me/indexes`; new GitHub helpers `LookupAccount`,
  `ListUserRepos`, `ListOrgRepos`, `ListRepoMarkdownFiles`,
  `ListRepoTopLevelMarkdown`. Indexes are deduped per (creator,
  source) so a second POST returns the existing row instead of
  minting a duplicate. Clicking a file in the listing ingests it via
  the existing `createFromURL` flow and lands the user on the doc
  page so they can comment, edit, or push back.
- **Prev/Next hunk navigation in the diff viewer.** Both the
  AI-revise preview and the 3-way merge diff get a `‹ Prev / Next ›`
  pair plus a `N / total` counter in the diff toolbar. Each press
  smooth-scrolls the next changed section's sticky header to the top
  of the scroller (cleared for the diff toolbar's height), and the
  current hunk gets an accent-tinted header so it's obvious where you
  are. The "Rendered" tab hides the controls — they're meaningful
  only on the unified diff. Scratches the "I have to manually scroll
  to find each change" itch on long docs.
- **`Ignore` button on the source-drift banner.** Dismisses the
  banner for the *current* upstream SHA only — if a newer upstream
  commit shows up later, the banner returns. Pops a confirmation
  modal that spells out the implication ("we'll stop nudging you to
  merge, but a newer commit re-surfaces the banner") so it's not a
  one-click footgun. Backed by a new
  `POST /api/documents/:id/drift/ignore` endpoint that stamps a
  `sourceDriftIgnoredSha` on the chain root; the existing
  `SetDocumentSourceCheck` clears the marker as soon as upstream
  moves past the ignored SHA.

### Changed

- **Direct-commit pushback clears the drift banner.** After a
  successful direct commit to the doc's tracking branch (the same
  ref the doc was cloned from), the pushback handler stamps the new
  blob SHA as the doc's `SourceSHA` baseline and broadcasts a
  `doc-updated` event. The next drift check sees us in sync and the
  banner disappears. PR mode + commits to non-tracking branches
  intentionally don't clear drift (the PR isn't merged; a sibling
  branch doesn't affect the tracked ref).

### Fixed

- **Native markdown editor.** Click *Edit* on any doc for a CodeMirror 6
  editor with markdown syntax highlighting, light/dark theme (tracks the
  app theme automatically), and `⌘S` to save your changes as a new
  revision. The editor runs in the same page scroll as view mode — no
  scroll-in-scroll. Comments stay anchored to their text spans as you
  edit; the sidebar tracks the same lines in real time.
- **Sticky formatting toolbar.** Bold, italic, code, H1/H2/H3, bulleted
  list, numbered list, task list, blockquote, link, code block, HR. Both
  the action bar (Editing / Save / Cancel / Find / Show preview) and the
  formatting controls share one sticky frame that pins to the top of the
  editor column as you scroll through long documents.
- **Find & replace** inside the editor (`⌘F`) with regex, case sensitivity,
  whole-word, and replace-all — the standard CodeMirror search panel.
- **Live side-by-side preview.** Toggle *Show preview* to render the
  document on the right as you edit on the left.
- **Smart wrap-toggle.** Selecting `**bold**` (with the markers included)
  and clicking *B* strips one layer instead of doubling up — for bold,
  italic, and code. Catches the common selection-overshoot pattern most
  markdown editors get wrong.
- **Push to GitHub.** For docs cloned from a GitHub blob URL, click *Push
  to GitHub* on any revision. Two modes: **open a pull request** from a
  new branch (with prefilled title + body that reference the doc), or
  **commit directly** to a branch you pick (typically `main`). The OAuth
  token from your sign-in does the work — no separate PAT needed. Branch
  protection rules are enforced on GitHub's side and surfaced verbatim if
  they reject the push.
- **Manual revision API.** New endpoint
  `POST /api/documents/:id/manual-revisions` writes editor saves as a new
  child doc, parented to the source doc — manual edits and AI revisions
  share the same versioning model.
- **Soft edit lock.** Whoever clicks *Edit* first holds the lock; other
  viewers see a banner with the holder's display name and the Edit button
  disabled. Lock auto-releases on Save / Cancel / disconnect, broadcasts
  over the existing SSE hub.
- **3-way merge engine.** When a doc has unsynced upstream changes AND
  local edits, the merge UI runs a 3-way diff against the original, the
  upstream version, and the local version — surfacing clean merges,
  manual conflicts, and a per-region pick-a-side UI.
- **Comments carry through AI revision.** Resolved comments applied via
  *Revise with AI* keep their threads on the new child doc, re-anchored
  against the revised text where the quoted span still appears.
- **MCP Tier 1 + Tier 2 tools.** Agents can now `edit_document`,
  `patch_comment_anchor`, `delete_comment`, `list_revisions`,
  `merge_document`, and `push_to_github` — the same actions the human UI
  exposes, with the same access checks, scope enforcement, and rate
  limits.
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

- **Active comment text is now highlighted in the editor.** Pressing Next
  in edit mode paints the matched span with the same yellow background
  the view-mode rendered markdown uses — implemented as a CodeMirror
  StateField + Decoration that's driven by a `setActiveHighlight` state
  effect. The old "set a text selection" gesture wasn't visible enough
  (especially in dark mode and on an unfocused editor) and could be lost
  to keyboard navigation.
- **`Next` reliably scrolls to the next comment.** Two compounding causes
  fixed: (a) both the Document.tsx `activeId` effect and EditorPane's
  internal effect were calling `window.scrollTo`, fighting over the
  smooth-scroll target; (b) CodeMirror uses estimated line heights for
  content outside its render viewport, so a single `coordsAtPos` read
  could be tens to hundreds of pixels off. Now there's exactly one
  scroll driver (`scrollAnchorIntoView` on the editor handle), and it
  takes a two-pass measurement: an instant nudge to the estimated
  target, then a smooth-scroll to the authoritative position once CM
  has had a frame to refine its height map. The page lands on the
  active comment's line every click.
- **Stacked off-viewport cards no longer push the active card off its
  anchor.** The cards-layout pass now filters to comments whose anchor
  is within ±200 px of the viewport (plus the active card unconditionally).
  Previously, a comment anchored above the viewport would clamp to
  `minTop = 0` and stack with every other above-viewport card; their
  combined height pushed the active comment's card hundreds of pixels
  below its highlighted span.
- **Card runaway on `Next` in edit mode.** Pressing Next would scroll the
  body so the editor anchor landed at the viewport, but the comment cards
  would march off into ever-larger `style.top` values, landing in empty
  space tens of thousands of pixels below the doc. Three compounding
  causes: (1) the sidebar's scroll-into-view fed `sidebar.scrollTop` back
  into `containerRect.top`, which amplified `desiredTop` on the next
  layout; (2) the anchored-cards container's `minHeight` grew with each
  pass, making the sidebar internally scrollable so the loop could
  continue; and (3) MCP-added agent comments all store `anchor.start = 0`
  (text-substring anchoring resolved at render time), so the layout's
  sort fell through to insertion order — a card anchored near the top of
  the doc was stacked below cards anchored further down because it was
  inserted later. Fixes: dropped the sidebar scroll-into-view (the
  body-scroll already brings the right card into view), and the
  cards-layout pass now sorts by editor-anchor Y before relaxing.
- **Content column expands with the browser.** Removed the `max-w-3xl`
  cap (1024px effective) on the document column — view mode now uses
  `max-w-5xl`, edit mode uses `max-w-none`. The editor fills whatever
  space you give it, instead of wrapping early in a narrow strip and
  leaving the rest of the column blank.
- **Sticky toolbar pins correctly in long docs.** The action + formatting
  bar wasn't actually sticking in edit mode because the editor lived
  inside an `overflow-y-auto` column — CSS sticky bound to the column,
  not the viewport, so scrolling the page slid the bar off-screen.
  Removed the inner column scroll, made the comment sidebar
  viewport-sticky instead, and re-laid out anchored cards on body scroll
  via an rAF-throttled scroll listener.
- **Comment cards align with their highlighted line in edit mode.** The
  layout formula now computes `desiredTop` against the anchored container's
  bounding rect, so the math is identical for view mode and edit mode.
- **Edit-mode active-comment scroll.** Clicking *Next* on a comment now
  asks CodeMirror to scroll the anchored line into view in edit mode
  (instead of silently no-oping because the rendered DOM is gone).
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
