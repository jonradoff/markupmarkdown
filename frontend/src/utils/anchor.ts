// Anchor utilities: map between DOM selections and character offsets in the
// container's textContent, and apply highlight spans for stored ranges.

export interface AnchorSpec {
  start: number;
  end: number;
  exact: string;
}

export interface HighlightRange {
  id: string;
  start: number;
  end: number;
  resolved: boolean;
  active: boolean;
  // For agent-created comments anchored by text-substring rather than by
  // character offsets. When start == end == 0 and exact is set, the
  // renderer resolves it against the live textContent.
  exact?: string;
}

function getTextOffset(
  container: HTMLElement,
  node: Node,
  offsetInNode: number
): number {
  if (!container.contains(node) && node !== container) return -1;

  if (node.nodeType !== Node.TEXT_NODE) {
    let acc = 0;
    for (let i = 0; i < offsetInNode && i < node.childNodes.length; i++) {
      acc += node.childNodes[i].textContent?.length ?? 0;
    }
    if (node === container) return acc;
    return offsetOfNodeStart(container, node) + acc;
  }

  let offset = 0;
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  let cur: Node | null;
  while ((cur = walker.nextNode())) {
    if (cur === node) return offset + offsetInNode;
    offset += cur.textContent?.length ?? 0;
  }
  return -1;
}

function offsetOfNodeStart(container: HTMLElement, node: Node): number {
  let offset = 0;
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  let cur: Node | null;
  while ((cur = walker.nextNode())) {
    if (node === cur || node.contains(cur)) return offset;
    offset += cur.textContent?.length ?? 0;
  }
  return -1;
}

export function getSelectionAnchor(container: HTMLElement): AnchorSpec | null {
  const sel = window.getSelection();
  if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return null;

  const range = sel.getRangeAt(0);
  if (!container.contains(range.commonAncestorContainer)) return null;

  const start = getTextOffset(container, range.startContainer, range.startOffset);
  const end = getTextOffset(container, range.endContainer, range.endOffset);
  if (start < 0 || end < 0 || end <= start) return null;

  const exact = sel.toString();
  if (!exact || exact.trim() === "") return null;

  return { start, end, exact };
}

export function unwrapHighlights(container: HTMLElement) {
  const spans = Array.from(container.querySelectorAll("span.mm-highlight"));
  for (const span of spans) {
    const parent = span.parentNode;
    if (!parent) continue;
    while (span.firstChild) parent.insertBefore(span.firstChild, span);
    parent.removeChild(span);
  }
  container.normalize();
}

export function applyHighlights(
  container: HTMLElement,
  ranges: HighlightRange[]
) {
  unwrapHighlights(container);

  // Resolve agent-style anchors (start == end == 0 with non-empty exact) by
  // finding the substring in the current textContent.
  const resolved = ranges.map((r) =>
    r.start === 0 && r.end === 0 && r.exact
      ? resolveTextAnchor(container, r)
      : r
  );

  const sorted = resolved
    .filter((r) => r.end > r.start)
    .sort((a, b) => a.start - b.start || a.end - b.end);
  for (const r of sorted) {
    wrapRange(container, r);
  }
}

// resolveTextAnchor walks the container's textContent and returns the first
// occurrence of r.exact, mapping it back to a [start, end] character range.
// Used for comments created by agents via MCP (text-substring anchoring).
function resolveTextAnchor(
  container: HTMLElement,
  r: HighlightRange
): HighlightRange {
  const needle = r.exact ?? "";
  if (!needle) return r;
  let combined = "";
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  let cur: Node | null;
  while ((cur = walker.nextNode())) {
    combined += cur.textContent ?? "";
  }
  const idx = combined.indexOf(needle);
  if (idx < 0) return { ...r, start: 0, end: 0 };
  return { ...r, start: idx, end: idx + needle.length };
}

function wrapRange(container: HTMLElement, range: HighlightRange) {
  let offset = 0;
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  const targets: Array<{
    node: Text;
    startInNode: number;
    endInNode: number;
  }> = [];

  let cur: Node | null;
  while ((cur = walker.nextNode())) {
    const text = cur as Text;
    const len = text.length;
    const nodeStart = offset;
    const nodeEnd = offset + len;
    offset = nodeEnd;

    if (nodeEnd <= range.start || nodeStart >= range.end) continue;
    if (text.parentElement?.closest("span.mm-highlight")) continue;

    const startInNode = Math.max(0, range.start - nodeStart);
    const endInNode = Math.min(len, range.end - nodeStart);
    if (endInNode > startInNode) {
      targets.push({ node: text, startInNode, endInNode });
    }
  }

  for (let i = targets.length - 1; i >= 0; i--) {
    const t = targets[i];
    let target: Text = t.node;
    if (t.startInNode > 0) {
      target = target.splitText(t.startInNode);
    }
    const segLen = t.endInNode - t.startInNode;
    if (segLen < target.length) {
      target.splitText(segLen);
    }
    const span = document.createElement("span");
    span.className = "mm-highlight";
    span.dataset.commentId = range.id;
    if (range.resolved) span.dataset.resolved = "true";
    if (range.active) span.dataset.active = "true";
    target.parentNode!.insertBefore(span, target);
    span.appendChild(target);
  }
}

export function getHighlightRect(
  container: HTMLElement,
  commentId: string
): DOMRect | null {
  const el = container.querySelector(
    `span.mm-highlight[data-comment-id="${commentId}"]`
  );
  if (!el) return null;
  return (el as HTMLElement).getBoundingClientRect();
}
