import { useEffect, useRef, useState } from "react";
import Modal from "./Modal";
import type { MdDocument } from "../types";

interface Props {
  doc: MdDocument;
  onClose: () => void;
}

export default function ShareModal({ doc, onClose }: Props) {
  const url = typeof window !== "undefined" ? window.location.href : "";
  const inputRef = useRef<HTMLInputElement>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    inputRef.current?.select();
  }, []);

  async function copy() {
    try {
      await navigator.clipboard.writeText(url);
    } catch {
      inputRef.current?.select();
      document.execCommand?.("copy");
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1800);
  }

  return (
    <Modal title="Share this document" onClose={onClose}>
      <div className="flex items-center gap-2 mb-3">
        <input
          ref={inputRef}
          readOnly
          value={url}
          onFocus={(e) => e.currentTarget.select()}
          className="flex-1 text-sm font-mono border border-rule rounded px-3 py-2 bg-soft text-ink focus:outline-none"
        />
        <button
          onClick={copy}
          className="text-sm px-3 py-2 rounded bg-accent text-accent-fg font-medium hover:opacity-90 shrink-0"
        >
          {copied ? "Copied!" : "Copy"}
        </button>
      </div>

      {doc.private ? (
        <div className="text-xs text-muted bg-soft border border-rule rounded p-3 leading-relaxed">
          <div className="flex items-start gap-2">
            <svg className="text-muted shrink-0 mt-0.5" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
              <rect x="3" y="11" width="18" height="11" rx="2" />
              <path d="M7 11V7a5 5 0 0 1 10 0v4" />
            </svg>
            <div>
              <div className="text-ink font-medium mb-1">Private document</div>
              The recipient also needs current GitHub read access to{" "}
              <span className="font-mono">
                {doc.githubOwner}/{doc.githubRepo}
              </span>
              . We re-verify their access on every visit — without it they'll
              see a sign-in prompt instead of the content.
            </div>
          </div>
        </div>
      ) : (
        <div className="text-xs text-muted leading-relaxed">
          Anyone with this link can view this document and its comments. To
          comment, the recipient needs to set a display name or sign in.
        </div>
      )}
    </Modal>
  );
}
