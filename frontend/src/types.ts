export interface DocumentSummary {
  id: string;
  title: string;
  sourceUrl?: string;
  origin: "url" | "upload";
  private?: boolean;
  githubOwner?: string;
  githubRepo?: string;
  /** Total nodes in this doc's revision chain (1 = no revisions). */
  revisionCount?: number;
  /** Root doc id when the listed entry is itself a child revision. */
  rootId?: string;
  /** Older, independently-ingested copies of the SAME source file
   *  that got folded into this entry's row. The Recents list shows
   *  only the most-recent copy by default with a "N older copies"
   *  expander — backed by the source-URL dedup in listDocuments. */
  olderVersions?: OlderDocumentVersion[];
  createdAt: string;
  updatedAt: string;
}

export interface OlderDocumentVersion {
  id: string;
  title: string;
  updatedAt: string;
  revisionCount?: number;
}

export interface NotificationItem {
  id: string;
  kind: "mention" | "reply";
  documentId: string;
  documentTitle: string;
  commentId: string;
  actorName: string;
  actorAvatarUrl?: string;
  preview: string;
  createdAt: string;
  readAt?: string;
}

export interface NotificationListResponse {
  unread: number;
  notifications: NotificationItem[];
}

export interface MentionCandidate {
  login: string;
  name: string;
  avatarUrl?: string;
}

export interface TrashItem {
  id: string;
  title: string;
  deletedAt: string;
  daysLeft: number;
}

export interface RevisionMeta {
  model: string;
  appliedCommentIds: string[];
  tokensIn: number;
  tokensOut: number;
  generatedBy: string;
  generatedAt: string;
  /** Set to "agent" when the revision was written through a Bearer
   * token (MCP or REST). Mirrors Comment.actorKind. */
  actorKind?: "human" | "agent";
  ancestorSourceSha?: string;
  /** Stamped when a human accepts an agent-authored revision (P0-3).
   * Pushback refuses to ship an unaccepted agent revision to GitHub. */
  acceptedAt?: string;
  acceptedBy?: string;
}

/** Discrete coordination vocabulary for reviews (P0-1). Mirrors
 * GitHub PR review states. `changes_requested` blocks pushback until
 * the reviewer clears it or the pusher force-overrides. */
export type ReviewState = "approved" | "changes_requested" | "commented";

export interface Review {
  documentId: string;
  state: ReviewState;
  note?: string;
  author?: string;
  authorAvatarUrl?: string;
  actorKind?: "human" | "agent";
  ownerName?: string;
  ownerLogin?: string;
  mine?: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ReviewSummary {
  approved: number;
  changesRequested: number;
  commented: number;
}

/** Structured edit proposal attached to an anchored comment (P0-2).
 * `replacement` is what should replace `comment.anchor.exact`.
 * `appliedAt`/`appliedBy`/`appliedDocId` are stamped when a reviewer
 * clicks Apply — the resulting manual revision id is captured too. */
export interface Suggestion {
  replacement: string;
  appliedAt?: string;
  appliedBy?: string;
  appliedDocId?: string;
}

export interface ParentSummary {
  id: string;
  title: string;
  /** 1-based structural position in the revision chain (root = 1). */
  revisionIndex?: number;
}

export interface RevisionSummary {
  id: string;
  title: string;
  createdAt: string;
  revisionMeta?: RevisionMeta;
  revisionIndex?: number;
}

export interface MdDocument {
  id: string;
  title: string;
  sourceUrl?: string;
  origin: "url" | "upload";
  /** Discriminates which set of source-specific fields are populated.
   * Newer than `origin`; switch on this in new code. */
  sourceKind?: "github_blob" | "gist" | "url" | "upload";
  content: string;
  private?: boolean;
  githubOwner?: string;
  githubRepo?: string;
  githubRef?: string;
  githubPath?: string;
  /** Gist fields — populated when sourceKind === "gist". gistCommit
   * mirrors sourceSha's role for github blobs (both are upstream-
   * content fingerprints driving drift detection). gistFilename is
   * which file inside the gist this doc was cloned from; gistFileCount
   * is stored so the "this gist has N more files" UI affordance
   * doesn't need an extra round-trip. */
  gistOwner?: string;
  gistId?: string;
  gistCommit?: string;
  gistFilename?: string;
  gistFileCount?: number;
  /** Blob SHA of the source file at last sync, if GitHub-sourced. */
  sourceSha?: string;
  /** When the upstream SHA was last checked. */
  sourceCheckedAt?: string;
  /** Set when the latest upstream SHA differs from sourceSha — the
   * frontend renders a "source updated on GitHub" banner. */
  sourceLatestSha?: string;
  /** When the drift was first observed. */
  sourceDriftedAt?: string;
  /** Upstream SHA the user explicitly dismissed via the drift
   * banner's "Ignore" button. The banner stays hidden while this
   * equals sourceLatestSha; if upstream moves to a newer SHA, the
   * backend clears the marker and the banner returns. */
  sourceDriftIgnoredSha?: string;
  parentId?: string;
  revisionMeta?: RevisionMeta;
  parent?: ParentSummary;
  children?: RevisionSummary[];
  latestDescendant?: ParentSummary;
  /** Set on child revisions — points at the original ingest. The
   * source-drift banner uses this for the "Open original" action,
   * because syncing happens on the root (a child revision is AI-
   * diverged from upstream by design). */
  rootDocument?: ParentSummary;
  /** 1-based structural position of this doc in its revision chain
   * (root = 1). Counts soft-deleted ancestors so numbering stays
   * stable as docs come and go. */
  revisionIndex?: number;
  /** Total nodes in the chain — root plus every descendant via the
   * most-recent-child walk. Renders as "v2 of 4" on the toolbar. */
  revisionTotal?: number;
  previouslyViewedAt?: string;
  /** Review-state aggregate for this doc revision (P0-1). Populated
   * on GET /api/documents/:id; empty when no one has reviewed. */
  reviews?: ReviewSummary;
  /** The current viewer's own review (P0-1). Null/undefined when
   * they haven't reviewed. */
  myReview?: Review;
  /** True when the current revision was authored by an agent and
   * hasn't been human-accepted (P0-3). Pushback refuses to ship
   * these until they're accepted via POST /accept-revision. */
  agentProposed?: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface AnthropicKeyStatus {
  hasKey: boolean;
  hint?: string;
  setAt?: string;
  enabled: boolean;
}

// SelfDocRedirect is returned by POST /api/documents when the user
// pastes a markupmarkdown doc URL into the URL field. The frontend
// navigates to `redirect` instead of trying to render a clone.
export interface SelfDocRedirect {
  kind: "self_doc_redirect";
  redirect: string;
  documentId: string;
}

export interface RevisionPreview {
  originalContent: string;
  revisedContent: string;
  model: string;
  tokensIn: number;
  tokensOut: number;
  costEstimateUsd: number;
  appliedCommentIds: string[];
  identical: boolean;
}

/** Pushback metadata — what /pushback/info returns. Lets the modal
 * pre-populate fields and decide whether the direct-commit choice is
 * enabled. */
export interface PushbackInfo {
  owner: string;
  repo: string;
  path: string;
  defaultBranch: string;
  sourceBranch: string;
  canPushDirect: boolean;
  canOpenPR: boolean;
  suggestedBranch: string;
  suggestedMessage: string;
  suggestedPRTitle: string;
  suggestedPRBody: string;
  repoHtmlUrl: string;
  /** At least one reviewer has state=changes_requested (P0-1). The
   * modal renders a warning; sending force=true overrides. */
  changesRequested?: boolean;
  /** Current revision is agent-authored and not yet accepted (P0-3).
   * The modal renders "Accept this revision first" with an Accept
   * button; sending force=true overrides. */
  agentProposed?: boolean;
}

/** A shareable listing of `.md` files anchored to a github resource.
 *  Kind controls what `owner` / `repo` mean:
 *    - "repo": every `.md` file in `owner/repo`
 *    - "user": top-level `.md` files across `owner`'s repos
 *    - "org":  same as user but for an organization
 *  Items are NOT stored — they're computed fresh on every view using
 *  the viewer's GitHub token, so different viewers may see different
 *  listings if their repo access differs. */
export interface MarkdownIndex {
  id: string;
  kind: "repo" | "user" | "org";
  owner: string;
  repo?: string;
  title: string;
  sourceUrl: string;
  private: boolean;
  /** Owner-pinned default filename filter. Share-link visitors get
   * this filter applied on first load (until they pick their own). */
  defaultFilter?: string;
  /** GitHub login of the index creator. Used by the frontend to
   * scope owner-only actions (pin / rename / delete). */
  createdById?: string;
  createdAt: string;
  updatedAt: string;
  deletedAt?: string;
}

export interface MarkdownIndexItem {
  title: string;
  url: string;
  repo?: string;
  repoUrl?: string;
  pathInRepo?: string;
  description?: string;
  updatedAt?: string;
  private?: boolean;
}

export interface MarkdownIndexResponse extends MarkdownIndex {
  items: MarkdownIndexItem[];
  truncated?: boolean;
}

/** A single progress event from the index streaming endpoint. */
export interface IndexProgressEvent {
  kind: "meta" | "ready" | "status" | "scanning" | "items" | "done" | "error";
  message?: string;
  current?: number;
  total?: number;
  repo?: string;
  items?: MarkdownIndexItem[];
  truncated?: boolean;
  error?: string;
}

export interface PushbackResult {
  mode: "pr" | "direct";
  branch: string;
  commitSha: string;
  commitUrl: string;
  prNumber?: number;
  prUrl?: string;
}

/** Result of the streaming POST /api/documents/:id/merge-preview. */
export interface MergePreview {
  mergedContent: string;
  upstreamContent: string;
  upstreamSourceSha: string;
  ancestorSourceSha: string;
  model: string;
  tokensIn: number;
  tokensOut: number;
  costEstimateUsd: number;
  identical: boolean;
  /** True when ancestor==upstream, ours==upstream, or ancestor==ours.
   * No Claude call was made; the merged content is the trivial result. */
  noMergeNeeded?: boolean;
}

export interface Anchor {
  start: number;
  end: number;
  exact: string;
  prefix?: string;
  suffix?: string;
}

export interface AuthUser {
  id: string;
  githubId: number;
  login: string;
  name: string;
  email?: string;
  avatarUrl?: string;
  createdAt: string;
  updatedAt: string;
}

export interface AuthConfig {
  githubEnabled: boolean;
  githubClientId?: string;
}

export interface Reply {
  id: string;
  author: string;
  authorAvatarUrl?: string;
  actorKind?: "human" | "agent";
  ownerName?: string;
  ownerLogin?: string;
  body: string;
  bodyHtml?: string;
  /** Set by the backend when the viewer is the human behind this reply
   * (direct author or owner of the bot/token that wrote it). */
  mine?: boolean;
  createdAt: string;
  updatedAt: string;
}

export type TokenScope = 'read' | 'write' | 'admin';

export interface APIToken {
  id: string;
  prefix: string;
  label: string;
  scope: TokenScope;
  createdAt: string;
  expiresAt?: string;
  lastUsedAt?: string;
}

export interface CreatedTokenResponse {
  token: string;
  metadata: APIToken;
}

export interface TokenEvent {
  id: string;
  action: string;
  documentId?: string;
  at: string;
}

export interface Comment {
  id: string;
  documentId: string;
  anchor: Anchor;
  author: string;
  authorAvatarUrl?: string;
  actorKind?: "human" | "agent";
  ownerName?: string;
  ownerLogin?: string;
  body: string;
  bodyHtml?: string;
  /** Set by the backend when the viewer is the human behind this comment
   * (direct author or owner of the bot/token that wrote it). */
  mine?: boolean;
  resolved: boolean;
  resolvedBy?: string;
  resolvedAt?: string;
  /** True when the source changed and we couldn't unambiguously
   * re-locate the original quoted text. Orphans render in a section
   * below the doc with a manual re-anchor flow. */
  orphan?: boolean;
  /** The quoted text from before the source change — what the
   * comment was originally about. Shown in the orphan card. */
  originalExact?: string;
  /** Structured edit proposal on this anchored comment (P0-2). When
   * present, the comment card renders a one-click Apply button.
   * appliedAt/etc. get stamped when a reviewer applies it. */
  suggestion?: Suggestion;
  replies: Reply[];
  createdAt: string;
  updatedAt: string;
}

export interface SyncSourceResponse {
  id: string;
  sourceSha: string;
  cleanCount: number;
  orphanCount: number;
}

export interface PatchAnchorRequest {
  start?: number;
  end?: number;
  exact?: string;
  prefix?: string;
  suffix?: string;
  /** When true, convert to a document-level comment (no inline anchor). */
  docLevel?: boolean;
}
