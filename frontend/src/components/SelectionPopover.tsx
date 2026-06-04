interface Props {
  x: number;
  y: number;
  onComment: () => void;
  /** When true, label the action "Re-anchor here" instead of "Comment".
   * Used during the orphan-comment manual re-anchor flow. */
  reanchorMode?: boolean;
}

export default function SelectionPopover({ x, y, onComment, reanchorMode }: Props) {
  return (
    <div
      className="fixed z-30 bg-tip-bg text-tip-fg text-sm rounded-md shadow-lg px-2 py-1 flex items-center gap-1"
      style={{ left: x, top: y, transform: "translate(-50%, -100%)" }}
      onMouseDown={(e) => e.preventDefault()}
    >
      <button
        onClick={onComment}
        className="px-2 py-1 hover:bg-tip-fg/10 rounded flex items-center gap-1"
      >
        {reanchorMode ? (
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="12" cy="10" r="3" />
            <path d="M12 2a8 8 0 0 0-8 8c0 4.5 8 12 8 12s8-7.5 8-12a8 8 0 0 0-8-8z" />
          </svg>
        ) : (
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
          </svg>
        )}
        {reanchorMode ? "Re-anchor here" : "Comment"}
      </button>
    </div>
  );
}
