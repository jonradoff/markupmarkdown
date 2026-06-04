// Compact Prev/Next strip rendered beneath each comment card so the
// user can advance to the adjacent thread without scrolling back to
// the sticky nav at the top of the sidebar.

interface Props {
  position: number; // 1-based index of this card in visibleComments
  total: number;
  onPrev: () => void;
  onNext: () => void;
}

export default function CommentStepNav({ position, total, onPrev, onNext }: Props) {
  if (total <= 1) return null;
  return (
    <div
      className="flex items-center justify-between mt-1 px-1 py-1 text-[11px] text-muted opacity-70 hover:opacity-100 transition-opacity"
      onClick={(e) => e.stopPropagation()}
    >
      <button
        onClick={onPrev}
        title="Previous comment (k or ↑)"
        className="flex items-center gap-0.5 px-1.5 py-0.5 rounded hover:bg-soft hover:text-ink"
      >
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="15 18 9 12 15 6" />
        </svg>
        Prev
      </button>
      <span className="tabular-nums text-faint">
        {position} of {total}
      </span>
      <button
        onClick={onNext}
        title="Next comment (j or ↓)"
        className="flex items-center gap-0.5 px-1.5 py-0.5 rounded hover:bg-soft hover:text-ink"
      >
        Next
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="9 18 15 12 9 6" />
        </svg>
      </button>
    </div>
  );
}
