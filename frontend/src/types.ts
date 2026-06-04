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
  body: string;
  createdAt: string;
  updatedAt: string;
}

export interface Comment {
  id: string;
  documentId: string;
  anchor: Anchor;
  author: string;
  authorAvatarUrl?: string;
  body: string;
  resolved: boolean;
  resolvedBy?: string;
  resolvedAt?: string;
  replies: Reply[];
  createdAt: string;
  updatedAt: string;
}
