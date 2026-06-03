import { useEffect, useRef, useState } from "react";

interface Props {
  quotedText: string;
  onSubmit: (body: string) => Promise<void> | void;
  onCancel: () => void;
}

export default function NewCommentComposer({
  quotedText,
  onSubmit,
  onCancel,
}: Props) {
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const textRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    textRef.current?.focus();
  }, []);

  async function submit() {
    if (!body.trim() || busy) return;
    setBusy(true);
    try {
      await onSubmit(body.trim());
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="bg-card border-2 border-accent rounded-lg shadow-md p-3">
      <div className="text-xs text-muted mb-2 line-clamp-2 italic">
        “{quotedText}”
      </div>
      <textarea
        ref={textRef}
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            submit();
          }
          if (e.key === "Escape") onCancel();
        }}
        rows={3}
        placeholder="Add a comment…"
        className="w-full text-sm border border-rule rounded p-2 resize-none focus:outline-none focus:border-accent"
      />
      <div className="flex items-center justify-end gap-2 mt-2">
        <button
          onClick={onCancel}
          disabled={busy}
          className="text-sm px-3 py-1 text-muted hover:text-ink"
        >
          Cancel
        </button>
        <button
          onClick={submit}
          disabled={busy || !body.trim()}
          className="text-sm px-3 py-1 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
        >
          Comment
        </button>
      </div>
      <div className="text-[10px] text-faint mt-1 text-right">
        ⌘+Enter to submit
      </div>
    </div>
  );
}
