// Layout solver for Google-Docs-style margin comments: each card wants
// to live at the Y position of its anchored span; if two cards would
// overlap, push the later one down so it sits flush below the earlier.
//
// `desiredTops` is ordered by visual sequence (top → bottom). Each
// value is the preferred top in CSS px relative to the sidebar's
// scroll container. Returned array is the same length with collisions
// resolved.

export interface AnchoredItem {
  id: string;
  desiredTop: number;
  height: number;
}

/**
 * relaxAnchors pushes overlapping items down by min-gap so each card's
 * top >= previous card's bottom + gap. Doesn't pull items up if there's
 * unused space above — that would tear them away from their anchors.
 */
export function relaxAnchors(items: AnchoredItem[], gap = 12): Record<string, number> {
  const out: Record<string, number> = {};
  let cursor = -Infinity;
  for (const it of items) {
    const top = Math.max(it.desiredTop, cursor + gap);
    out[it.id] = top;
    cursor = top + it.height;
  }
  return out;
}
