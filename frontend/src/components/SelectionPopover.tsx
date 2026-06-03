interface Props {
  x: number;
  y: number;
  onComment: () => void;
}

export default function SelectionPopover({ x, y, onComment }: Props) {
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
        Comment
      </button>
    </div>
  );
}
