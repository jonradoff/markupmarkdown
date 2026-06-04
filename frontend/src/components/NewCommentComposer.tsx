import { useState } from "react";
import MentionInput from "./MentionInput";

interface Props {
  documentId: string;
  quotedText: string;
  onSubmit: (body: string) => Promise<void> | void;
  onCancel: () => void;
}

export default function NewCommentComposer({
  documentId,
  quotedText,
  onSubmit,
  onCancel,
}: Props) {
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit() {
    if (!body.trim() || busy) return;
    setBusy(true);
    try {
      await onSubmit(body.trim());
      // success path: parent unmounts the composer; we don't need to
      // clear the draft because we'll be torn down.
    } catch {
      // onSubmit already showed a toast; keep the draft so the user can
      // retry without retyping. Don't dismiss the composer.
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="bg-card border-2 border-accent rounded-lg shadow-md p-3">
      <div className="text-xs text-muted mb-2 line-clamp-2 italic">
        “{quotedText}”
      </div>
      <MentionInput
        documentId={documentId}
        value={body}
        onChange={setBody}
        placeholder="Add a comment… (use @ to mention)"
        rows={3}
        autoFocus
        onSubmit={submit}
        onEscape={onCancel}
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
