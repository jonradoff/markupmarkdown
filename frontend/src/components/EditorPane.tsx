import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import CodeMirror, {
  type ReactCodeMirrorRef,
  EditorView,
  keymap,
  Prec,
} from "@uiw/react-codemirror";
import { markdown, markdownLanguage } from "@codemirror/lang-markdown";
import { search, searchKeymap, openSearchPanel } from "@codemirror/search";
import { EditorSelection } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
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

// EditorPaneHandle is the imperative surface Document.tsx pokes when
// the anchored comment-card layout needs editor-relative positions —
// in edit mode the rendered Markdown is gone, so getHighlightRect
// from utils/anchor returns nothing. The handle lets the parent ask
// CodeMirror where each comment's quoted text lives instead.
export interface EditorPaneHandle {
  /** Locate `exact` in the editor's current content and return its
   * viewport-relative top/bottom in pixels, or null when no match. */
  coordsForAnchor(exact: string): { top: number; bottom: number } | null;
  /** Scroll the editor so `exact` is visible. */
  scrollAnchorIntoView(exact: string): void;
}

interface Props {
  initialContent: string;
  sourceUrl?: string;
  saving: boolean;
  onSave: (content: string) => Promise<void> | void;
  onCancel: () => void;
  /** When the parent activates a comment while the editor is open,
   * find its quoted text in the raw Markdown and select it so the
   * user can see which span the comment is anchored to. */
  activeAnchorExact?: string;
  /** Fires when the editor scrolls or its size changes — the parent
   * uses this as a layout-tick so the anchored comment cards reflow
   * to match the new editor positions. */
  onLayoutTick?: () => void;
}

// EditorPane wraps CodeMirror 6 with the GFM markdown language
// extension — same syntax highlighting users see in VS Code, Obsidian,
// HackMD, and GitHub's web editor. Toolbar buttons + ⌘B/I/K/E/F/S
// stay wired through the same markdownActions helpers.
//
// forwardRef so Document.tsx can pull editor-relative anchor
// coordinates for the anchored comment-card layout: in edit mode the
// rendered Markdown is gone, so the parent's getHighlightRect-based
// layout has nothing to anchor cards against. The ref handle lets it
// ask CodeMirror where each anchor lives instead.
const EditorPane = forwardRef<EditorPaneHandle, Props>(function EditorPane({
  initialContent,
  sourceUrl,
  saving,
  onSave,
  onCancel,
  activeAnchorExact,
  onLayoutTick,
}, ref) {
  const [content, setContent] = useState(initialContent);
  // Preview is off by default — markdown-only is the canonical edit
  // surface; users can toggle preview on per-session if they want.
  const [showPreview, setShowPreview] = useState(false);
  const cmRef = useRef<ReactCodeMirrorRef>(null);

  // Track the live dark/light theme so CodeMirror's syntax-highlight
  // palette flips with the rest of the app. The app marks dark mode
  // by toggling a `dark` class on <html>; observe that and re-render
  // when it changes (theme switcher in the header, or system
  // preference change while the editor is open).
  const [isDark, setIsDark] = useState(() =>
    document.documentElement.classList.contains("dark")
  );
  useEffect(() => {
    const update = () =>
      setIsDark(document.documentElement.classList.contains("dark"));
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    return () => observer.disconnect();
  }, []);

  const dirty = content !== initialContent;

  // Refs to the latest save / cancel callbacks so the static keymap
  // we register with CodeMirror always reaches today's handlers
  // without forcing a full extension rebuild on every keystroke.
  const onSaveRef = useRef(onSave);
  const onCancelRef = useRef(onCancel);
  const contentRef = useRef(content);
  const dirtyRef = useRef(dirty);
  const savingRef = useRef(saving);
  onSaveRef.current = onSave;
  onCancelRef.current = onCancel;
  contentRef.current = content;
  dirtyRef.current = dirty;
  savingRef.current = saving;

  // applyAction is the central dispatch for every formatting button +
  // shortcut. Pulls the current state out of CodeMirror, runs the
  // pure markdownActions helper, and dispatches the result back as a
  // single transaction so undo/redo treat it as one step.
  const applyAction = useCallback((fn: (s: EditState) => EditState) => {
    const view = cmRef.current?.view;
    if (!view) return;
    const sel = view.state.selection.main;
    const next = fn({
      text: view.state.doc.toString(),
      selectionStart: sel.from,
      selectionEnd: sel.to,
    });
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: next.text },
      selection: EditorSelection.range(next.selectionStart, next.selectionEnd),
    });
    view.focus();
  }, []);

  // Stable ref to the layout-tick so the extension array doesn't
  // rebuild on every parent re-render (which would re-instantiate
  // CodeMirror).
  const layoutTickRef = useRef(onLayoutTick);
  layoutTickRef.current = onLayoutTick;

  // CodeMirror extensions — built once, only rebuilt if the toggleable
  // pieces (none right now) change. Includes the markdown language,
  // search panel (default UI; opens on ⌘F), and our custom keymap.
  const extensions = useMemo(
    () => [
      markdown({ base: markdownLanguage, codeLanguages: [] }),
      search({ top: true }),
      EditorView.lineWrapping,
      // Disable CodeMirror's internal scroll viewport — the parent
      // column scrolls the page instead, so the user sees one
      // scrollbar instead of a scroll-in-scroll. As a bonus, every
      // line is "in view" from CodeMirror's perspective, so
      // coordsAtPos always returns a real rect and the comment-
      // anchored sidebar cards line up exactly with the highlighted
      // source row.
      EditorView.theme({
        "&": { height: "auto", maxHeight: "none" },
        ".cm-scroller": { overflow: "visible" },
        ".cm-content": { padding: "12px 0" },
      }),
      // Notify the parent on scroll / geometry change so the
      // anchored comment-card layout in the sidebar reflows.
      EditorView.updateListener.of((u) => {
        if (u.geometryChanged || u.viewportChanged) {
          layoutTickRef.current?.();
        }
      }),
      // Prec.highest so our save/format shortcuts shadow CodeMirror's
      // defaults that share the same chord (e.g. ⌘B is bold here,
      // not "select-by-character" extend).
      Prec.highest(
        keymap.of([
          {
            key: "Mod-s",
            run: () => {
              if (dirtyRef.current && !savingRef.current) {
                onSaveRef.current(contentRef.current);
              }
              return true;
            },
          },
          {
            key: "Escape",
            run: () => {
              if (!savingRef.current) onCancelRef.current();
              return true;
            },
          },
          { key: "Mod-b", run: () => { applyAction((s) => applyWrap(s, "**")); return true; } },
          { key: "Mod-i", run: () => { applyAction((s) => applyWrap(s, "_")); return true; } },
          { key: "Mod-e", run: () => { applyAction((s) => applyWrap(s, "`")); return true; } },
          { key: "Mod-k", run: () => { applyAction(applyLink); return true; } },
        ])
      ),
      keymap.of(searchKeymap), // ⌘F open, ⌘G next, ⇧⌘G prev, etc.
    ],
    [applyAction]
  );

  // Locate the active comment's quoted text in the editor's content
  // and select it, the same gesture the old textarea version used.
  // findApproxIndex falls back to the longest contiguous substring
  // when the rendered anchor spans markdown formatting markers.
  useEffect(() => {
    if (!activeAnchorExact) return;
    const view = cmRef.current?.view;
    if (!view) return;
    const text = view.state.doc.toString();
    const idx = findApproxIndex(text, activeAnchorExact);
    if (idx < 0) return;
    const end = idx + matchLength(text, idx, activeAnchorExact);
    view.dispatch({
      selection: EditorSelection.range(idx, end),
      effects: EditorView.scrollIntoView(idx, { y: "center" }),
    });
    view.focus();
  }, [activeAnchorExact]);

  function openSearch() {
    const view = cmRef.current?.view;
    if (!view) return;
    view.focus();
    openSearchPanel(view);
  }

  // Expose CodeMirror-relative anchor coordinates so the doc page's
  // anchored card layout still works in edit mode. coordsAtPos returns
  // null for off-screen positions; lineBlockAt + contentDOM offset is
  // the fallback so cards still get a sensible Y even when the
  // corresponding line isn't currently in the viewport.
  useImperativeHandle(ref, () => ({
    coordsForAnchor(exact) {
      const view = cmRef.current?.view;
      if (!view || !exact) return null;
      const text = view.state.doc.toString();
      const idx = findApproxIndex(text, exact);
      if (idx < 0) return null;
      const end = idx + matchLength(text, idx, exact);
      const start = view.coordsAtPos(idx);
      const fin = view.coordsAtPos(end);
      if (start && fin) {
        return { top: start.top, bottom: fin.bottom };
      }
      // Off-screen — derive from the document layout instead.
      try {
        const block = view.lineBlockAt(idx);
        const contentTop = view.contentDOM.getBoundingClientRect().top;
        // block.top is editor-document-relative; subtract scrollTop to
        // get viewport-relative.
        const scrollTop = view.scrollDOM.scrollTop;
        const viewportTop = contentTop + (block.top - scrollTop);
        return { top: viewportTop, bottom: viewportTop + block.height };
      } catch {
        return null;
      }
    },
    scrollAnchorIntoView(exact) {
      const view = cmRef.current?.view;
      if (!view || !exact) return;
      const idx = findApproxIndex(view.state.doc.toString(), exact);
      if (idx < 0) return;
      view.dispatch({
        effects: EditorView.scrollIntoView(idx, { y: "center" }),
      });
    },
  }), []);

  return (
    <div className="space-y-3">
      {/* Action bar + formatting toolbar share one sticky frame so
          the formatting controls stay visible as the page scrolls
          through a long document. */}
      <div className="sticky top-0 z-10 bg-card border border-rule rounded-md shadow-sm">
        <div className="flex items-center gap-2 px-3 py-2">
          <div className="text-sm font-medium">Editing</div>
          <div className="text-xs text-muted">
            {dirty ? "Unsaved changes" : "No changes yet"} ·{" "}
            <kbd className="text-[10px] bg-soft border border-rule px-1 rounded">⌘S</kbd>{" "}
            to save ·{" "}
            <kbd className="text-[10px] bg-soft border border-rule px-1 rounded">⌘F</kbd>{" "}
            to find ·{" "}
            <kbd className="text-[10px] bg-soft border border-rule px-1 rounded">Esc</kbd> to cancel
          </div>
          <button
            onClick={openSearch}
            className="ml-auto text-xs px-2 py-1 rounded text-muted hover:text-ink hover:bg-soft"
            title="Find & replace (⌘F)"
          >
            Find
          </button>
          <button
            onClick={() => setShowPreview((v) => !v)}
            className="text-xs px-2 py-1 rounded text-muted hover:text-ink hover:bg-soft"
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

        {/* Formatting toolbar. */}
        <div className="flex flex-wrap items-center gap-1 text-xs text-muted border-t border-rule px-2 py-1.5">
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
      </div>

      <div className={`grid gap-3 ${showPreview ? "md:grid-cols-2" : "grid-cols-1"}`}>
        <div className="border border-rule rounded-md bg-card">
          <CodeMirror
            ref={cmRef}
            value={content}
            onChange={(v: string) => setContent(v)}
            extensions={extensions}
            basicSetup={{
              lineNumbers: false,
              foldGutter: false,
              highlightActiveLine: true,
              indentOnInput: false,
              dropCursor: true,
              autocompletion: false,
              searchKeymap: false, // we install searchKeymap above
              defaultKeymap: true,
              history: true,
              bracketMatching: true,
              closeBrackets: false,
              rectangularSelection: false,
              crosshairCursor: false,
            }}
            theme={isDark ? oneDark : "light"}
            placeholder="Edit Markdown…"
            style={{ fontSize: 14 }}
          />
        </div>
        {showPreview && (
          <div className="border border-rule rounded-md p-3 bg-card">
            <MarkdownRender
              content={content}
              baseUrl={baseURLForDoc(sourceUrl)}
            />
          </div>
        )}
      </div>
    </div>
  );
});

export default EditorPane;

// ToolbarButton is the tiny styled wrapper for every formatting
// button. preventDefault on mouseDown keeps the editor focused so
// clicking the button doesn't blur the selection.
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
      onMouseDown={(e) => e.preventDefault()}
      onClick={onClick}
      className="px-2 py-1 rounded hover:bg-soft text-muted hover:text-ink min-w-[1.75rem] flex items-center justify-center"
    >
      {children}
    </button>
  );
}

// findApproxIndex tries to locate `needle` in `text`. The needle was
// captured from rendered textContent, so it might span Markdown
// formatting that doesn't appear verbatim in source (e.g. " first
// draft " between **…** asterisks). Strategy: exact match → trim
// match → longest contiguous internal substring. Returns -1 when
// nothing reasonable matches.
function findApproxIndex(text: string, needle: string): number {
  if (!needle) return -1;
  const idx = text.indexOf(needle);
  if (idx >= 0) return idx;
  const trimmed = needle.trim();
  if (trimmed && trimmed !== needle) {
    const j = text.indexOf(trimmed);
    if (j >= 0) return j;
  }
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
// index — full needle length when the match was exact, less when we
// fell back to an approximate substring.
function matchLength(text: string, idx: number, needle: string): number {
  if (text.slice(idx, idx + needle.length) === needle) return needle.length;
  const trimmed = needle.trim();
  if (text.slice(idx, idx + trimmed.length) === trimmed) return trimmed.length;
  let len = Math.min(trimmed.length, text.length - idx);
  while (len > 0) {
    const sub = trimmed.slice(0, len);
    if (text.slice(idx, idx + len) === sub) return len;
    len--;
  }
  return 0;
}
