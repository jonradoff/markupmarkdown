import { useEffect, useRef, useState } from "react";
import type { Comment, Reply } from "../types";
import { colorFor, formatRelative, initials } from "../utils/format";
import { getAuthor } from "../utils/author";
import { useAuth } from "../auth";
import { useDialog } from "./Dialogs";
import RichBody from "./RichBody";
import MentionInput from "./MentionInput";

interface Props {
  comment: Comment;
  active: boolean;
  me: string;
  onActivate: () => void;
  onResolve: () => Promise<void>;
  onReopen: () => Promise<void>;
  onReply: (body: string) => Promise<void>;
  onEdit: (body: string) => Promise<void>;
  onDelete: () => Promise<void>;
  onEditReply: (replyId: string, body: string) => Promise<void>;
  onDeleteReply: (replyId: string) => Promise<void>;
  requireIdentity: (next: () => void) => void;
}

function Avatar({
  name,
  url,
  isAgent,
  title,
}: {
  name: string;
  url?: string;
  isAgent?: boolean;
  title?: string;
}) {
  // Agents get a distinct visual — a square-ish rounded badge with the
  // accent color and a robot glyph, not a person photo. This is intentional:
  // bots should not LOOK like humans in a thread.
  if (isAgent) {
    return (
      <span
        className="shrink-0 w-7 h-7 rounded-md bg-accent text-accent-fg flex items-center justify-center"
        title={title ?? `${name} (agent)`}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden
        >
          <rect x="3" y="9" width="18" height="12" rx="3" />
          <circle cx="9" cy="15" r="1.2" fill="currentColor" />
          <circle cx="15" cy="15" r="1.2" fill="currentColor" />
          <path d="M12 9V5" />
          <circle cx="12" cy="4" r="1.2" fill="currentColor" />
          <path d="M7 21v1M17 21v1" />
        </svg>
      </span>
    );
  }
  if (url) {
    return (
      <img
        src={url}
        alt=""
        className="w-7 h-7 shrink-0 rounded-full bg-soft"
        loading="lazy"
        title={title}
      />
    );
  }
  return (
    <span
      className="w-7 h-7 shrink-0 rounded-full text-white text-xs flex items-center justify-center font-medium"
      style={{ background: colorFor(name) }}
      title={title}
    >
      {initials(name)}
    </span>
  );
}

// agentTitle returns the tooltip shown on an agent comment's avatar + name,
// surfacing the accountable human behind the bot.
function agentTitle(name: string, ownerName?: string, ownerLogin?: string): string {
  const owner = ownerName || ownerLogin;
  if (!owner) return `${name} (agent)`;
  if (ownerLogin && ownerName) return `${name} — agent owned by ${ownerName} (@${ownerLogin})`;
  if (ownerLogin) return `${name} — agent owned by @${ownerLogin}`;
  return `${name} — agent owned by ${owner}`;
}

export default function CommentCard({
  comment,
  active,
  me,
  onActivate,
  onResolve,
  onReopen,
  onReply,
  onEdit,
  onDelete,
  onEditReply,
  onDeleteReply,
  requireIdentity,
}: Props) {
  const { user } = useAuth();
  const dialog = useDialog();
  const [replyOpen, setReplyOpen] = useState(false);
  const [replyBody, setReplyBody] = useState("");
  const [editing, setEditing] = useState(false);
  const [editBody, setEditBody] = useState(comment.body);
  const [busy, setBusy] = useState(false);
  const cardRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (active) {
      cardRef.current?.scrollIntoView({
        behavior: "smooth",
        block: "nearest",
      });
    }
  }, [active]);

  // Keep edit-body in sync with the comment body when realtime updates come in
  useEffect(() => {
    if (!editing) setEditBody(comment.body);
  }, [comment.body, editing]);

  const isMine = user
    ? comment.author === user.name || comment.author === user.login
    : comment.author === getAuthor();

  async function handleReply() {
    if (!replyBody.trim()) return;
    setBusy(true);
    try {
      await onReply(replyBody.trim());
      setReplyBody("");
      setReplyOpen(false);
    } finally {
      setBusy(false);
    }
  }

  function openReplyComposer() {
    requireIdentity(() => setReplyOpen(true));
  }

  async function handleEditSave() {
    if (!editBody.trim()) return;
    setBusy(true);
    try {
      await onEdit(editBody.trim());
      setEditing(false);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      ref={cardRef}
      data-comment-id={comment.id}
      onClick={onActivate}
      className={[
        "bg-card border rounded-lg shadow-sm p-3 cursor-pointer transition",
        active
          ? "border-accent shadow-md ring-2 ring-accent/20"
          : "border-rule hover:border-faint",
        comment.resolved ? "opacity-70" : "",
      ].join(" ")}
    >
      {/* Quoted text */}
      <div className="text-xs text-muted italic mb-2 line-clamp-2 border-l-2 border-rule pl-2">
        “{comment.anchor.exact}”
      </div>

      {/* Author row */}
      <div className="flex items-start gap-2 mb-2">
        <Avatar
          name={comment.author}
          url={comment.authorAvatarUrl}
          isAgent={comment.actorKind === "agent"}
          title={
            comment.actorKind === "agent"
              ? agentTitle(comment.author, comment.ownerName, comment.ownerLogin)
              : undefined
          }
        />
        <div className="flex-1 min-w-0">
          <div className="flex items-center justify-between">
            <div
              className="font-medium text-sm text-ink truncate flex items-center gap-1.5"
              title={
                comment.actorKind === "agent"
                  ? agentTitle(comment.author, comment.ownerName, comment.ownerLogin)
                  : undefined
              }
            >
              <span className="truncate">{comment.author}</span>
              {comment.actorKind === "agent" && (
                <span className="text-[10px] uppercase tracking-wide bg-accent-soft text-accent rounded px-1.5 py-0.5 shrink-0">
                  agent
                </span>
              )}
            </div>
            <div className="text-[11px] text-faint">
              {formatRelative(comment.createdAt)}
            </div>
          </div>
          {editing ? (
            <div onClick={(e) => e.stopPropagation()}>
              <textarea
                value={editBody}
                onChange={(e) => setEditBody(e.target.value)}
                rows={2}
                className="w-full text-sm border border-rule rounded p-1.5 mt-1 focus:outline-none focus:border-accent"
              />
              <div className="flex justify-end gap-2 mt-1">
                <button
                  onClick={() => setEditing(false)}
                  className="text-xs text-muted hover:text-ink"
                >
                  Cancel
                </button>
                <button
                  onClick={handleEditSave}
                  disabled={busy}
                  className="text-xs px-2 py-0.5 rounded bg-accent text-accent-fg disabled:opacity-50"
                >
                  Save
                </button>
              </div>
            </div>
          ) : (
            <div className="text-sm text-ink">
              <RichBody body={comment.body} highlightLogin={user?.login} />
            </div>
          )}
        </div>
      </div>

      {/* Replies */}
      {comment.replies.length > 0 && (
        <div className="mt-2 pl-9 space-y-2">
          {comment.replies.map((r) => (
            <ReplyRow
              key={r.id}
              reply={r}
              mine={user ? r.author === user.name || r.author === user.login : r.author === me}
              onEdit={(body) => onEditReply(r.id, body)}
              onDelete={() => onDeleteReply(r.id)}
            />
          ))}
        </div>
      )}

      {/* Footer actions */}
      <div
        className="flex items-center justify-between mt-3 pt-2 border-t border-rule"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 text-xs">
          {!comment.resolved ? (
            <>
              <button
                onClick={() => (replyOpen ? setReplyOpen(false) : openReplyComposer())}
                className="text-muted hover:text-accent"
              >
                Reply
              </button>
              <span className="text-faint">·</span>
              <button
                onClick={onResolve}
                className="text-muted hover:text-success"
              >
                Mark as done
              </button>
            </>
          ) : (
            <>
              <span className="text-success">
                ✓ Resolved
                {comment.resolvedBy ? ` by ${comment.resolvedBy}` : ""}
              </span>
              <button
                onClick={onReopen}
                className="text-muted hover:text-accent"
              >
                Reopen
              </button>
            </>
          )}
        </div>
        <div className="flex items-center gap-2 text-xs">
          {isMine && !editing && (
            <>
              <button
                onClick={() => setEditing(true)}
                className="text-muted hover:text-ink"
              >
                Edit
              </button>
              <span className="text-faint">·</span>
              <button
                onClick={async () => {
                  const ok = await dialog.confirm({
                    title: "Delete comment?",
                    body: "Delete this comment and all its replies?",
                    confirmLabel: "Delete",
                    danger: true,
                  });
                  if (ok) onDelete();
                }}
                className="text-muted hover:text-danger"
              >
                Delete
              </button>
            </>
          )}
        </div>
      </div>

      {/* Reply composer */}
      {replyOpen && !comment.resolved && (
        <div className="mt-3 pl-9" onClick={(e) => e.stopPropagation()}>
          <MentionInput
            documentId={comment.documentId}
            value={replyBody}
            onChange={setReplyBody}
            placeholder="Reply… (use @ to mention)"
            rows={2}
            autoFocus
            onSubmit={handleReply}
            onEscape={() => setReplyOpen(false)}
            className="w-full text-sm border border-rule rounded p-1.5 resize-none focus:outline-none focus:border-accent"
          />
          <div className="flex justify-end gap-2 mt-1">
            <button
              onClick={() => setReplyOpen(false)}
              className="text-xs text-muted hover:text-ink"
            >
              Cancel
            </button>
            <button
              onClick={handleReply}
              disabled={busy || !replyBody.trim()}
              className="text-xs px-2 py-1 rounded bg-accent text-accent-fg disabled:opacity-50"
            >
              Reply
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function ReplyRow({
  reply,
  mine,
  onEdit,
  onDelete,
}: {
  reply: Reply;
  mine: boolean;
  onEdit: (body: string) => Promise<void>;
  onDelete: () => Promise<void>;
}) {
  const dialog = useDialog();
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState(reply.body);
  const [busy, setBusy] = useState(false);

  async function save() {
    if (!body.trim()) return;
    setBusy(true);
    try {
      await onEdit(body.trim());
      setEditing(false);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex items-start gap-2">
      <Avatar
        name={reply.author}
        url={reply.authorAvatarUrl}
        isAgent={reply.actorKind === "agent"}
        title={
          reply.actorKind === "agent"
            ? agentTitle(reply.author, reply.ownerName, reply.ownerLogin)
            : undefined
        }
      />
      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between">
          <div
            className="text-xs font-medium text-ink flex items-center gap-1.5"
            title={
              reply.actorKind === "agent"
                ? agentTitle(reply.author, reply.ownerName, reply.ownerLogin)
                : undefined
            }
          >
            <span>{reply.author}</span>
            {reply.actorKind === "agent" && (
              <span className="text-[9px] uppercase tracking-wide bg-accent-soft text-accent rounded px-1 py-0.5">
                agent
              </span>
            )}
          </div>
          <div className="text-[10px] text-faint">
            {formatRelative(reply.createdAt)}
          </div>
        </div>
        {editing ? (
          <>
            <textarea
              value={body}
              onChange={(e) => setBody(e.target.value)}
              rows={2}
              className="w-full text-sm border border-rule rounded p-1.5 mt-1 focus:outline-none focus:border-accent"
            />
            <div className="flex justify-end gap-2 mt-1">
              <button
                onClick={() => setEditing(false)}
                className="text-xs text-muted hover:text-ink"
              >
                Cancel
              </button>
              <button
                onClick={save}
                disabled={busy}
                className="text-xs px-2 py-0.5 rounded bg-accent text-accent-fg disabled:opacity-50"
              >
                Save
              </button>
            </div>
          </>
        ) : (
          <div className="text-sm">
            <RichBody body={reply.body} />
          </div>
        )}
        {mine && !editing && (
          <div className="flex gap-2 mt-1 text-[11px]">
            <button
              onClick={() => setEditing(true)}
              className="text-muted hover:text-ink"
            >
              Edit
            </button>
            <button
              onClick={async () => {
                const ok = await dialog.confirm({
                  title: "Delete reply?",
                  body: "Delete this reply?",
                  confirmLabel: "Delete",
                  danger: true,
                });
                if (ok) onDelete();
              }}
              className="text-muted hover:text-danger"
            >
              Delete
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
