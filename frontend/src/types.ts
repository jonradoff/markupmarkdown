export interface DocumentSummary {
  id: string;
  title: string;
  sourceUrl?: string;
  origin: "url" | "upload";
  private?: boolean;
  githubOwner?: string;
  githubRepo?: string;
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
}

export interface RevisionSummary {
  id: string;
  title: string;
  createdAt: string;
  revisionMeta?: RevisionMeta;
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
  parentId?: string;
  revisionMeta?: RevisionMeta;
  parent?: ParentSummary;
  children?: RevisionSummary[];
  latestDescendant?: ParentSummary;
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
  replies: Reply[];
  createdAt: string;
  updatedAt: string;
}
