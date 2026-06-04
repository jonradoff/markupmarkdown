import type {
  Anchor,
  AnthropicKeyStatus,
  AuthConfig,
  AuthUser,
  Comment,
  DocumentSummary,
  MdDocument,
  RevisionPreview,
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
  createFromURL: (url: string, title?: string) =>
    req<MdDocument>("/api/documents", {
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
};
