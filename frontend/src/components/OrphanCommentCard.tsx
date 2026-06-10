import type { Comment } from "../types";
import CommentCard from "./CommentCard";

interface Props {
  comment: Comment;
  me: string;
  /** Called when the user wants to enter manual re-anchor mode for
   * this comment. The parent enters a doc-wide selection mode and
   * commits via the SelectionPopover. */
  onStartReanchor: () => void;
  /** Convert this orphan to a doc-level comment (no inline anchor). */
  onMakeDocLevel: () => Promise<void> | void;
  /** Standard comment actions threaded through to CommentCard. */
  onResolve: () => Promise<void>;
  onReopen: () => Promise<void>;
  onReply: (body: string) => Promise<void>;
  onEdit: (body: string) => Promise<void>;
  onDelete: () => Promise<void>;
  onEditReply: (replyId: string, body: string) => Promise<void>;
  onDeleteReply: (replyId: string) => Promise<void>;
  requireIdentity: (next: () => void) => void;
}

// Card rendered in the orphan section below the doc body. Shows the
// previously-highlighted quote, action buttons for re-anchoring / pinning
// as doc-level, and the full comment thread.
export default function OrphanCommentCard(props: Props) {
  const { comment, onStartReanchor, onMakeDocLevel } = props;
  const original = comment.originalExact || comment.anchor.exact || "";

  return (
    <div
      className="rounded-lg p-3 border"
      style={{
        borderColor: "var(--color-warn-border)",
        backgroundColor: "var(--color-warn-bg-soft)",
        color: "var(--color-warn-ink)",
      }}
    >
      <div
        className="text-[11px] uppercase tracking-wide font-medium mb-1.5"
        style={{ color: "var(--color-warn-muted)" }}
      >
        Couldn't re-anchor this comment
      </div>
      <div
        className="text-xs italic mb-2 border-l-2 pl-2"
        style={{
          color: "var(--color-warn-muted)",
          borderColor: "var(--color-warn-border)",
        }}
      >
        Previously highlighted: “{original}”
      </div>
      <div className="flex flex-wrap items-center gap-2 mb-3 text-xs">
        <button
          onClick={onStartReanchor}
          className="px-2 py-1 rounded font-medium"
          style={{
            backgroundColor: "var(--color-warn-action)",
            color: "var(--color-warn-action-fg)",
          }}
          onMouseEnter={(e) =>
            (e.currentTarget.style.backgroundColor =
              "var(--color-warn-action-hover)")
          }
          onMouseLeave={(e) =>
            (e.currentTarget.style.backgroundColor =
              "var(--color-warn-action)")
          }
        >
          Re-anchor to new text
        </button>
        <button
          onClick={() => onMakeDocLevel()}
          className="px-2 py-1 rounded border"
          style={{
            borderColor: "var(--color-warn-border)",
            color: "var(--color-warn-ink)",
          }}
        >
          Pin as doc-level
        </button>
      </div>
      <CommentCard
        comment={comment}
        active={false}
        me={props.me}
        onActivate={() => {}}
        onResolve={props.onResolve}
        onReopen={props.onReopen}
        onReply={props.onReply}
        onEdit={props.onEdit}
        onDelete={props.onDelete}
        onEditReply={props.onEditReply}
        onDeleteReply={props.onDeleteReply}
        requireIdentity={props.requireIdentity}
        hideQuotedText
      />
    </div>
  );
}
