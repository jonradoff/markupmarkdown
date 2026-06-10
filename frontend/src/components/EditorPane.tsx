import { useEffect, useRef, useState } from "react";
import MarkdownRender from "./MarkdownRender";
import { baseURLForDoc } from "../utils/baseUrl";
import {
  applyCodeBlock,
  applyHeading,
  applyHR,
  applyLink,
  applyLinePrefix,
  applyWrap,
  type EditState,
} from "../utils/markdownActions";

interface Props {
  initialContent: string;
  sourceUrl?: string;
  saving: boolean;
  onSave: (content: string) => Promise<void> | void;
  onCancel: () => void;
  /** When the parent activates a comment while the editor is open,
   * find its quoted text in the raw Markdown and select it in the
   * textarea so the user can see which span the comment is anchored
   * to. The native selection is the highlight. */
  activeAnchorExact?: string;
}

// EditorPane is the doc page's Markdown editor: a textarea with an
// optional live preview (off by default — markdown-first). Save
// creates a new revision in the chain (manual edit) via the parent
// handler; cancel drops the draft.
export default function EditorPane({
  initialContent,
  sourceUrl,
  saving,
  onSave,
  onCancel,
  activeAnchorExact,
}: Props) {
  const [content, setContent] = useState(initialContent);
  // Preview is off by default — markdown-only is the canonical edit
  // surface; users can toggle preview on per-session if they want it.
  const [showPreview, setShowPreview] = useState(false);
  const taRef = useRef<HTMLTextAreaElement>(null);

  // Keep cursor anchored when content arrives async (it shouldn't, but
  // belt-and-suspenders).
  useEffect(() => {
    if (taRef.current && content === initialContent) {
      taRef.current.focus();
    }
  }, [content, initialContent]);

  // When the parent activates a comment, locate its quoted text in the
  // raw Markdown and select it — the textarea's native selection
  // serves as the highlight. Tries the exact text first; falls back to
  // a longer-suffix / longer-prefix substring search if the exact
  // doesn't appear verbatim (anchors captured from rendered
  // textContent often span markdown formatting markers — `**bold**`
  // around a phrase, etc — so the raw doesn't always contain the
  // exact string).
  useEffect(() => {
    if (!activeAnchorExact || !taRef.current) return;
    const ta = taRef.current;
    const text = ta.value;
    const idx = findApproxIndex(text, activeAnchorExact);
    if (idx < 0) return;
    const end = idx + matchLength(text, idx, activeAnchorExact);
    ta.focus();
    ta.setSelectionRange(idx, end);
    // Scroll the selection into the visible portion of the textarea.
    // setSelectionRange doesn't scroll on its own; nudge by mutating
    // scrollTop based on a rough line count.
    const before = text.slice(0, idx);
    const lineNum = (before.match(/\n/g) ?? []).length;
    const lineHeightPx = 20; // matches the font-mono text-sm default-ish
    const target = Math.max(0, lineNum * lineHeightPx - ta.clientHeight / 3);
    ta.scrollTop = target;
  }, [activeAnchorExact]);

  const dirty = content !== initialContent;

  // applyAction takes a markdownActions helper, runs it against the
  // textarea's current state, and writes the result back. Centralizes
  // the focus/selection/scroll dance every toolbar button + shortcut
  // needs to perform.
  function applyAction(fn: (s: EditState) => EditState) {
    const ta = taRef.current;
    if (!ta) return;
    const next = fn({
      text: ta.value,
      selectionStart: ta.selectionStart,
      selectionEnd: ta.selectionEnd,
    });
    setContent(next.text);
    // Restore selection after React reconciles. requestAnimationFrame
    // is just-enough delay; setTimeout(0) also works but rAF aligns
    // with the paint cycle.
    requestAnimationFrame(() => {
      if (!taRef.current) return;
      taRef.current.focus();
      taRef.current.setSelectionRange(next.selectionStart, next.selectionEnd);
    });
  }

  // Cmd/Ctrl-S to save, Esc to cancel — plus the standard set of
  // editor shortcuts so muscle memory transfers from other Markdown
  // tools (Bear, iA Writer, Obsidian).
  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    const cmd = e.metaKey || e.ctrlKey;
    if (cmd && e.key === "s") {
      e.preventDefault();
      if (dirty && !saving) onSave(content);
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      if (!saving) onCancel();
      return;
    }
    if (!cmd) return;
    switch (e.key.toLowerCase()) {
      case "b":
        e.preventDefault();
        applyAction((s) => applyWrap(s, "**"));
        return;
      case "i":
        e.preventDefault();
        applyAction((s) => applyWrap(s, "_"));
        return;
      case "k":
        e.preventDefault();
        applyAction(applyLink);
        return;
      case "e":
        e.preventDefault();
        applyAction((s) => applyWrap(s, "`"));
        return;
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 sticky top-0 z-10 bg-card border border-rule rounded-md px-3 py-2 shadow-sm">
        <div className="text-sm font-medium">Editing</div>
        <div className="text-xs text-muted">
          {dirty ? "Unsaved changes" : "No changes yet"} ·{" "}
          <kbd className="text-[10px] bg-soft border border-rule px-1 rounded">
            ⌘S
          </kbd>{" "}
          to save · <kbd className="text-[10px] bg-soft border border-rule px-1 rounded">Esc</kbd> to cancel
        </div>
        <button
          onClick={() => setShowPreview((v) => !v)}
          className="ml-auto text-xs px-2 py-1 rounded text-muted hover:text-ink hover:bg-soft"
          title="Toggle live preview"
        >
          {showPreview ? "Hide preview" : "Show preview"}
        </button>
        <button
          onClick={onCancel}
          disabled={saving}
          className="text-xs px-3 py-1 rounded border border-rule text-muted hover:text-ink"
        >
          Cancel
        </button>
        <button
          onClick={() => onSave(content)}
          disabled={!dirty || saving}
          className="text-xs px-3 py-1 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
        >
          {saving ? "Saving…" : "Save as revision"}
        </button>
      </div>

      {/* Formatting toolbar — operates on the textarea's current
          selection. Cmd/Ctrl shortcuts mirror the buttons (B/I/K/E)
          for muscle memory. */}
      <div className="flex flex-wrap items-center gap-1 text-xs text-muted border border-rule rounded-md px-2 py-1.5 bg-card">
        <ToolbarButton title="Bold (⌘B)" onClick={() => applyAction((s) => applyWrap(s, "**"))}><b>B</b></ToolbarButton>
        <ToolbarButton title="Italic (⌘I)" onClick={() => applyAction((s) => applyWrap(s, "_"))}><i>I</i></ToolbarButton>
        <ToolbarButton title="Strikethrough" onClick={() => applyAction((s) => applyWrap(s, "~~"))}><span style={{ textDecoration: "line-through" }}>S</span></ToolbarButton>
        <ToolbarButton title="Inline code (⌘E)" onClick={() => applyAction((s) => applyWrap(s, "`"))}><code className="text-[11px]">{`<>`}</code></ToolbarButton>
        <span className="w-px h-4 bg-rule mx-1" />
        <ToolbarButton title="Heading 1" onClick={() => applyAction((s) => applyHeading(s, 1))}>H1</ToolbarButton>
        <ToolbarButton title="Heading 2" onClick={() => applyAction((s) => applyHeading(s, 2))}>H2</ToolbarButton>
        <ToolbarButton title="Heading 3" onClick={() => applyAction((s) => applyHeading(s, 3))}>H3</ToolbarButton>
        <span className="w-px h-4 bg-rule mx-1" />
        <ToolbarButton title="Bulleted list" onClick={() => applyAction((s) => applyLinePrefix(s, "- "))}>• List</ToolbarButton>
        <ToolbarButton title="Numbered list" onClick={() => applyAction((s) => applyLinePrefix(s, "1. "))}>1. List</ToolbarButton>
        <ToolbarButton title="Task list" onClick={() => applyAction((s) => applyLinePrefix(s, "- [ ] "))}>☐ Task</ToolbarButton>
        <ToolbarButton title="Quote" onClick={() => applyAction((s) => applyLinePrefix(s, "> "))}>“ Quote</ToolbarButton>
        <span className="w-px h-4 bg-rule mx-1" />
        <ToolbarButton title="Link (⌘K)" onClick={() => applyAction(applyLink)}>Link</ToolbarButton>
        <ToolbarButton title="Code block" onClick={() => applyAction(applyCodeBlock)}>{`{ }`}</ToolbarButton>
        <ToolbarButton title="Horizontal rule" onClick={() => applyAction(applyHR)}>—</ToolbarButton>
      </div>

      <div className={`grid gap-3 ${showPreview ? "md:grid-cols-2" : "grid-cols-1"}`}>
        <textarea
          ref={taRef}
          value={content}
          onChange={(e) => setContent(e.target.value)}
          onKeyDown={handleKeyDown}
          spellCheck={false}
          className="w-full min-h-[60vh] p-3 font-mono text-sm bg-soft border border-rule rounded-md focus:outline-none focus:border-accent resize-y"
        />
        {showPreview && (
          <div className="border border-rule rounded-md p-3 bg-card overflow-auto min-h-[60vh]">
            <MarkdownRender
              content={content}
              baseUrl={baseURLForDoc(sourceUrl)}
            />
          </div>
        )}
      </div>
    </div>
  );
}

// ToolbarButton is the tiny styled wrapper used by every formatting
// button in the editor toolbar. Pulled out so the JSX above stays
// readable and the styling stays consistent.
function ToolbarButton({
  title,
  onClick,
  children,
}: {
  title: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={title}
      onMouseDown={(e) => e.preventDefault() /* keep textarea focus */}
      onClick={onClick}
      className="px-2 py-1 rounded hover:bg-soft text-muted hover:text-ink min-w-[1.75rem] flex items-center justify-center"
    >
      {children}
    </button>
  );
}

// findApproxIndex tries to locate `needle` in `text`. The needle was
// captured from rendered textContent, so it might span Markdown
// formatting that doesn't appear verbatim in the raw source (e.g.
// " first draft " between **…** asterisks). Strategy:
//   1. Exact match — covers most cases instantly.
//   2. Strip leading/trailing spaces from the needle and retry.
//   3. Walk down progressively shorter substrings, looking for the
//      longest one we can locate exactly. Picks up the spot even
//      when only part of the rendered text survives verbatim.
// Returns -1 when nothing reasonable matches.
function findApproxIndex(text: string, needle: string): number {
  if (!needle) return -1;
  const idx = text.indexOf(needle);
  if (idx >= 0) return idx;
  const trimmed = needle.trim();
  if (trimmed && trimmed !== needle) {
    const j = text.indexOf(trimmed);
    if (j >= 0) return j;
  }
  // Fall back to the longest contiguous internal substring. Try the
  // middle ~half of the needle first (most informative span).
  const minLen = Math.max(12, Math.floor(trimmed.length * 0.4));
  for (let len = trimmed.length - 1; len >= minLen; len--) {
    for (let start = 0; start + len <= trimmed.length; start++) {
      const sub = trimmed.slice(start, start + len);
      const k = text.indexOf(sub);
      if (k >= 0) return k;
    }
  }
  return -1;
}

// matchLength returns the actual selection length at the matched
// index. For an exact or trim-stripped match we use the needle length;
// for an approximate substring match we still highlight whatever did
// match — visually that's enough for the user to spot the spot.
function matchLength(text: string, idx: number, needle: string): number {
  // If `needle` appears verbatim at idx, use its full length.
  if (text.slice(idx, idx + needle.length) === needle) return needle.length;
  const trimmed = needle.trim();
  if (text.slice(idx, idx + trimmed.length) === trimmed) return trimmed.length;
  // Approximate match — highlight whatever contiguous span at idx
  // matches the longest internal slice we found.
  let len = Math.min(trimmed.length, text.length - idx);
  while (len > 0) {
    const sub = trimmed.slice(0, len);
    if (text.slice(idx, idx + len) === sub) return len;
    len--;
  }
  return 0;
}
