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
  createdAt: string;
  updatedAt: string;
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
  content: string;
  private?: boolean;
  githubOwner?: string;
  githubRepo?: string;
  githubRef?: string;
  githubPath?: string;
  /** Blob SHA of the source file at last sync, if GitHub-sourced. */
  sourceSha?: string;
  /** When the upstream SHA was last checked. */
  sourceCheckedAt?: string;
  /** Set when the latest upstream SHA differs from sourceSha — the
   * frontend renders a "source updated on GitHub" banner. */
  sourceLatestSha?: string;
  /** When the drift was first observed. */
  sourceDriftedAt?: string;
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
