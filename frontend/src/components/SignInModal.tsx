import { useState } from "react";
import Modal from "./Modal";
import { useAuth } from "../auth";
import { setAuthor } from "../utils/author";

interface Props {
  onClose: () => void;
  onContinue: (name: string) => void;
  subtitle?: string;
}

export default function SignInModal({ onClose, onContinue, subtitle }: Props) {
  const { githubEnabled, loginURL } = useAuth();
  const [name, setName] = useState("");

  function save() {
    const next = name.trim();
    if (!next) return;
    setAuthor(next);
    onContinue(next);
  }

  return (
    <Modal title="Who's commenting?" onClose={onClose}>
      <p className="text-sm text-muted mb-4">
        {subtitle ??
          "Set a name we'll show on your comments — or sign in with GitHub to use your GitHub identity (and unlock private repo files)."}
      </p>

      {githubEnabled && (
        <>
          <a
            href={loginURL()}
            className="w-full flex items-center justify-center gap-2 px-4 py-2 rounded bg-tip-bg text-tip-fg font-medium hover:opacity-90"
          >
            <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2c-3.2.7-3.87-1.36-3.87-1.36-.52-1.33-1.27-1.69-1.27-1.69-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.02 1.75 2.68 1.24 3.34.95.1-.74.4-1.24.72-1.53-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.04 0 0 .96-.31 3.15 1.18a10.96 10.96 0 0 1 5.74 0c2.19-1.49 3.15-1.18 3.15-1.18.63 1.58.23 2.75.12 3.04.73.81 1.18 1.84 1.18 3.1 0 4.43-2.69 5.41-5.25 5.69.41.36.78 1.06.78 2.13v3.16c0 .31.21.67.8.56C20.21 21.39 23.5 17.08 23.5 12 23.5 5.65 18.35.5 12 .5z" />
            </svg>
            Sign in with GitHub
          </a>
          <div className="flex items-center gap-3 my-4">
            <div className="flex-1 h-px bg-rule" />
            <div className="text-xs text-faint">or</div>
            <div className="flex-1 h-px bg-rule" />
          </div>
        </>
      )}

      <label className="text-sm font-medium block mb-1">Display name</label>
      <input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") save();
        }}
        placeholder="Jon Radoff"
        className="w-full text-sm border border-rule rounded px-3 py-2 focus:outline-none focus:border-accent bg-card text-ink"
      />
      <div className="flex justify-end gap-2 mt-4">
        <button
          onClick={onClose}
          className="text-sm px-3 py-1 text-muted hover:text-ink"
        >
          Cancel
        </button>
        <button
          onClick={save}
          disabled={!name.trim()}
          className="text-sm px-4 py-2 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
        >
          Continue
        </button>
      </div>
    </Modal>
  );
}
