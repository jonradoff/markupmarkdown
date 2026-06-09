import { useEffect, useRef, useState } from "react";
import MarkdownRender from "./MarkdownRender";
import { baseURLForDoc } from "../utils/baseUrl";

interface Props {
  initialContent: string;
  sourceUrl?: string;
  saving: boolean;
  onSave: (content: string) => Promise<void> | void;
  onCancel: () => void;
}

// EditorPane is the doc page's Markdown editor: a textarea split with a
// live preview, auto-saving the draft to sessionStorage so a stray
// refresh doesn't nuke half an hour of edits. Save creates a new
// revision in the chain (manual edit) via the parent handler; cancel
// drops the draft.
export default function EditorPane({
  initialContent,
  sourceUrl,
  saving,
  onSave,
  onCancel,
}: Props) {
  const [content, setContent] = useState(initialContent);
  const [showPreview, setShowPreview] = useState(true);
  const taRef = useRef<HTMLTextAreaElement>(null);

  // Keep cursor anchored when content arrives async (it shouldn't, but
  // belt-and-suspenders).
  useEffect(() => {
    if (taRef.current && content === initialContent) {
      taRef.current.focus();
    }
  }, [content, initialContent]);

  const dirty = content !== initialContent;

  // Cmd/Ctrl-S to save while in the textarea — feels native for anyone
  // who's edited Markdown elsewhere.
  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if ((e.metaKey || e.ctrlKey) && e.key === "s") {
      e.preventDefault();
      if (dirty && !saving) onSave(content);
    } else if (e.key === "Escape") {
      e.preventDefault();
      if (!saving) onCancel();
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
