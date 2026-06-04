import type {
  Anchor,
  AnthropicKeyStatus,
  APIToken,
  AuthConfig,
  AuthUser,
  Comment,
  CreatedTokenResponse,
  DocumentSummary,
  MdDocument,
  MentionCandidate,
  NotificationListResponse,
  PatchAnchorRequest,
  RevisionPreview,
  SelfDocRedirect,
  SyncSourceResponse,
  TokenEvent,
  TokenScope,
  TrashItem,
} from "./types";

export interface APIErrorAction {
  label: string;
  url: string;
}

export class APIError extends Error {
  kind?: string;
  detail?: string;
  actions?: APIErrorAction[];

  constructor(message: string, opts?: { kind?: string; detail?: string; actions?: APIErrorAction[] }) {
    super(message);
    this.name = "APIError";
    this.kind = opts?.kind;
    this.detail = opts?.detail;
    this.actions = opts?.actions;
  }
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
  });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    let kind: string | undefined;
    let detail: string | undefined;
    let actions: APIErrorAction[] | undefined;
    try {
      const body = await res.json();
      if (body?.error) msg = body.error;
      if (body?.kind) kind = body.kind;
      if (body?.detail) detail = body.detail;
      if (Array.isArray(body?.actions)) actions = body.actions;
    } catch {}
    throw new APIError(msg, { kind, detail, actions });
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  authConfig: () => req<AuthConfig>("/api/auth/config"),
  authMe: () => req<{ user: AuthUser | null }>("/api/auth/me"),
  authLogout: () =>
    req<void>("/api/auth/logout", { method: "POST" }),

  listDocuments: () => req<DocumentSummary[]>("/api/documents"),
  getDocument: (id: string) => req<MdDocument>(`/api/documents/${id}`),
  // createFromURL can either return a Document (cloned) or a redirect
  // instruction if the user pasted a markupmarkdown doc URL. The caller
  // checks `kind === "self_doc_redirect"` to decide which branch to take.
  createFromURL: (url: string, title?: string) =>
    req<MdDocument | SelfDocRedirect>("/api/documents", {
      method: "POST",
      body: JSON.stringify({ url, title }),
    }),
  createFromContent: (content: string, title: string) =>
    req<MdDocument>("/api/documents", {
      method: "POST",
      body: JSON.stringify({ content, title }),
    }),
  renameDocument: (id: string, title: string) =>
    req<MdDocument>(`/api/documents/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ title }),
    }),
  deleteDocument: (id: string) =>
    req<void>(`/api/documents/${id}`, { method: "DELETE" }),
  /** Pulls the latest source from GitHub, re-anchors comments where
   * possible, and flips the rest to orphan. */
  syncDocumentSource: (id: string) =>
    req<SyncSourceResponse>(`/api/documents/${id}/sync`, { method: "POST" }),
  /** Forces an immediate upstream SHA check, bypassing the server-side
   * TTL. Any drift fires a doc-updated broadcast over SSE. */
  checkDocumentSource: (id: string) =>
    req<{ status: string }>(`/api/documents/${id}/check-source`, { method: "POST" }),

  listTrash: () => req<TrashItem[]>("/api/me/trash"),
  restoreDocument: (id: string) =>
    req<{ id: string }>(`/api/documents/${id}/restore`, { method: "POST" }),

  listNotifications: () =>
    req<NotificationListResponse>("/api/me/notifications"),
  markAllNotificationsRead: () =>
    req<void>("/api/me/notifications/read", { method: "POST" }),
  markNotificationRead: (id: string) =>
    req<void>(`/api/me/notifications/${id}/read`, { method: "POST" }),
  /** Mark every pending notification for this comment as read — fires
   * whenever the viewer activates the comment, regardless of how they
   * got there. Returns {updated} so callers can tell whether the bell
   * needs refreshing. */
  markNotificationsForComment: (commentId: string) =>
    req<{ updated: number }>(
      `/api/me/notifications/comment/${commentId}/read`,
      { method: "POST" }
    ),
  listMentionCandidates: (docId: string) =>
    req<MentionCandidate[]>(`/api/documents/${docId}/mention-candidates`),

  listComments: (documentId: string) =>
    req<Comment[]>(`/api/documents/${documentId}/comments`),
  createComment: (
    documentId: string,
    payload: { anchor: Anchor; body: string; author: string }
  ) =>
    req<Comment>(`/api/documents/${documentId}/comments`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  editComment: (id: string, body: string) =>
    req<Comment>(`/api/comments/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ body }),
    }),
  deleteComment: (id: string) =>
    req<void>(`/api/comments/${id}`, { method: "DELETE" }),
  resolveComment: (id: string, author: string) =>
    req<Comment>(`/api/comments/${id}/resolve`, {
      method: "POST",
      body: JSON.stringify({ author }),
    }),
  reopenComment: (id: string) =>
    req<Comment>(`/api/comments/${id}/reopen`, { method: "POST" }),
  /** Manually re-anchor an orphan comment, or convert any comment to a
   * doc-level pin via {docLevel: true}. */
  patchCommentAnchor: (id: string, payload: PatchAnchorRequest) =>
    req<Comment>(`/api/comments/${id}/anchor`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),

  createReply: (commentId: string, body: string, author: string) =>
    req<Comment>(`/api/comments/${commentId}/replies`, {
      method: "POST",
      body: JSON.stringify({ body, author }),
    }),
  editReply: (commentId: string, replyId: string, body: string) =>
    req<Comment>(`/api/comments/${commentId}/replies/${replyId}`, {
      method: "PATCH",
      body: JSON.stringify({ body }),
    }),
  deleteReply: (commentId: string, replyId: string) =>
    req<Comment>(`/api/comments/${commentId}/replies/${replyId}`, {
      method: "DELETE",
    }),

  getAnthropicKey: () => req<AnthropicKeyStatus>("/api/me/anthropic-key"),
  setAnthropicKey: (key: string) =>
    req<AnthropicKeyStatus>("/api/me/anthropic-key", {
      method: "PUT",
      body: JSON.stringify({ key }),
    }),
  deleteAnthropicKey: () =>
    req<void>("/api/me/anthropic-key", { method: "DELETE" }),

  // Streams the revision back as Server-Sent Events. Calls onDelta with each
  // text chunk; resolves with the final preview metadata when "done" arrives.
  previewRevisionStream: async (
    documentId: string,
    onDelta: (text: string) => void,
    signal?: AbortSignal,
    commentIds?: string[]
  ): Promise<RevisionPreview> => {
    const res = await fetch(`/api/documents/${documentId}/revise`, {
      method: "POST",
      credentials: "include",
      headers: {
        Accept: "text/event-stream",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ commentIds: commentIds ?? [] }),
      signal,
    });
    if (!res.ok || !res.body) {
      let msg = `${res.status} ${res.statusText}`;
      let kind: string | undefined;
      let actions: { label: string; url: string }[] | undefined;
      try {
        const body = await res.json();
        if (body?.error) msg = body.error;
        if (body?.kind) kind = body.kind;
        if (Array.isArray(body?.actions)) actions = body.actions;
      } catch {}
      throw new APIError(msg, { kind, actions });
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let done: RevisionPreview | null = null;
    let streamErr: APIError | null = null;

    function processBlock(block: string) {
      let event = "message";
      const dataLines: string[] = [];
      for (const line of block.split("\n")) {
        if (line.startsWith("event: ")) event = line.slice(7);
        else if (line.startsWith("data: ")) dataLines.push(line.slice(6));
        else if (line.startsWith("data:")) dataLines.push(line.slice(5));
      }
      if (dataLines.length === 0) return;
      let payload: unknown;
      try {
        payload = JSON.parse(dataLines.join("\n"));
      } catch {
        return;
      }
      if (event === "delta") {
        const text = (payload as { text?: string }).text;
        if (typeof text === "string") onDelta(text);
      } else if (event === "done") {
        done = payload as RevisionPreview;
      } else if (event === "error") {
        const p = payload as { error?: string; kind?: string; actions?: { label: string; url: string }[] };
        streamErr = new APIError(p.error ?? "AI revision failed", {
          kind: p.kind,
          actions: p.actions,
        });
      }
    }

    while (true) {
      const { done: streamDone, value } = await reader.read();
      if (streamDone) break;
      buffer += decoder.decode(value, { stream: true });
      let sep;
      while ((sep = buffer.indexOf("\n\n")) >= 0) {
        const block = buffer.slice(0, sep);
        buffer = buffer.slice(sep + 2);
        processBlock(block);
      }
    }
    if (buffer.trim()) processBlock(buffer);

    if (streamErr) throw streamErr;
    if (!done) throw new APIError("Stream ended before the revision completed.");
    return done;
  },
  acceptRevision: (
    documentId: string,
    payload: {
      content: string;
      model: string;
      tokensIn: number;
      tokensOut: number;
      appliedCommentIds: string[];
    }
  ) =>
    req<MdDocument>(`/api/documents/${documentId}/revisions`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),

  listTokens: () => req<APIToken[]>("/api/me/tokens"),
  createToken: (input: {
    label: string;
    scope: TokenScope;
    // -1 = never expires; 0 = server default; positive = days
    expiresInDays: number;
  }) =>
    req<CreatedTokenResponse>("/api/me/tokens", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateToken: (id: string, patch: { label?: string; scope?: TokenScope }) =>
    req<void>(`/api/me/tokens/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),
  revokeToken: (id: string) =>
    req<void>(`/api/me/tokens/${id}`, { method: "DELETE" }),
  tokenActivity: (id: string) =>
    req<TokenEvent[]>(`/api/me/tokens/${id}/activity`),
};
