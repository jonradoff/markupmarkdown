import { useEffect, useRef, useState } from "react";
import { api, APIError } from "../api";
import type { Comment, MdDocument, RevisionPreview } from "../types";
import DiffView from "./DiffView";
import ErrorBlock from "./ErrorBlock";
import MarkdownRender from "./MarkdownRender";
import { baseURLForDoc } from "../utils/baseUrl";

interface Props {
  doc: MdDocument;
  resolvedComments: Comment[];
  onClose: () => void;
  onAccepted: (newDoc: MdDocument) => void;
}

type Phase = "intro" | "generating" | "preview" | "accepting" | "error";

const PRIVACY_KEY = "markupmarkdown:ai-privacy-ack";

export default function ReviseModal({
  doc,
  resolvedComments,
  onClose,
  onAccepted,
}: Props) {
  // Selected comment IDs (default: all resolved).
  const [selectedIds, setSelectedIds] = useState<string[]>(() =>
    resolvedComments.map((c) => c.id)
  );
  const [phase, setPhase] = useState<Phase>("intro");
  const [preview, setPreview] = useState<RevisionPreview | null>(null);
  const [error, setError] = useState<APIError | null>(null);
  const [streamed, setStreamed] = useState("");
  const [elapsed, setElapsed] = useState(0);
  const streamedRef = useRef("");

  useEffect(() => {
    if (phase !== "generating") return;
    let cancelled = false;
    streamedRef.current = "";
    setStreamed("");
    setElapsed(0);

    const startedAt = Date.now();
    const timer = window.setInterval(() => {
      if (!cancelled) setElapsed(Math.round((Date.now() - startedAt) / 1000));
    }, 250);

    const ctrl = new AbortController();
    const abortTimer = window.setTimeout(() => ctrl.abort(), 5 * 60 * 1000);

    (async () => {
      try {
        const result = await api.previewRevisionStream(
          doc.id,
          (chunk) => {
            if (cancelled) return;
            streamedRef.current += chunk;
            setStreamed(streamedRef.current);
          },
          ctrl.signal,
          selectedIds
        );
        if (cancelled) return;
        setPreview(result);
        setPhase("preview");
      } catch (err) {
        if (cancelled) return;
        if (err instanceof DOMException && err.name === "AbortError") {
          setError(
            new APIError(
              "The revision request timed out after 5 minutes. The server may have redeployed or the connection was dropped. Try again — the result usually comes back in under a minute."
            )
          );
        } else if (err instanceof APIError) {
          setError(err);
        } else {
          setError(new APIError((err as Error).message));
        }
        setPhase("error");
      }
    })();

    return () => {
      cancelled = true;
      window.clearInterval(timer);
      window.clearTimeout(abortTimer);
      ctrl.abort();
    };
  }, [phase, doc.id, selectedIds]);

  async function accept() {
    if (!preview) return;
    setPhase("accepting");
    try {
      const newDoc = await api.acceptRevision(doc.id, {
        content: preview.revisedContent,
        model: preview.model,
        tokensIn: preview.tokensIn,
        tokensOut: preview.tokensOut,
        appliedCommentIds: preview.appliedCommentIds,
      });
      onAccepted(newDoc);
    } catch (err) {
      if (err instanceof APIError) setError(err);
      else setError(new APIError((err as Error).message));
      setPhase("error");
    }
  }

  function startGeneration() {
    localStorage.setItem(PRIVACY_KEY, "1");
    setPhase("generating");
  }

  const baseUrl = baseURLForDoc(doc.sourceUrl);

  return (
    <div className="fixed inset-0 z-50 flex items-stretch justify-center p-4">
      <div
        className="absolute inset-0 bg-black/50"
        onClick={
          phase === "preview" || phase === "intro" || phase === "error"
            ? onClose
            : undefined
        }
      />
      <div className="relative bg-card border border-rule rounded-lg shadow-xl w-full max-w-6xl my-4 flex flex-col min-h-0 overflow-hidden">
        <header className="flex items-center justify-between px-5 py-3 border-b border-rule shrink-0">
          <div>
            <div className="font-semibold tracking-tight">Revise with AI</div>
            <div className="text-xs text-muted">{doc.title}</div>
          </div>
          <button
            onClick={onClose}
            aria-label="Close"
            className="text-faint hover:text-ink"
            disabled={phase === "generating" || phase === "accepting"}
          >
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </header>

        <div className="flex-1 min-h-0 flex flex-col">
          {phase === "intro" && (
            <IntroPanel
              docTitle={doc.title}
              isPrivate={!!doc.private}
              resolvedComments={resolvedComments}
              selectedIds={selectedIds}
              setSelectedIds={setSelectedIds}
              onStart={startGeneration}
              onCancel={onClose}
            />
          )}
          {phase === "generating" && (
            <StreamingPanel
              streamed={streamed}
              elapsed={elapsed}
              selectedCount={selectedIds.length}
              baseUrl={baseUrl}
            />
          )}
          {phase === "accepting" && (
            <CenteredSpinner hint="Saving the new revision…" />
          )}
          {phase === "preview" && preview && (
            <PreviewPanel
              preview={preview}
              doc={doc}
              onAccept={accept}
              onDiscard={onClose}
            />
          )}
          {phase === "error" && error && (
            <div className="p-8 max-w-xl mx-auto w-full">
              <ErrorBlock error={error} onDismiss={onClose} />
              <div className="text-center mt-4">
                <button
                  onClick={() => {
                    setError(null);
                    setPhase("generating");
                  }}
                  className="text-sm text-accent hover:underline"
                >
                  Retry
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function IntroPanel({
  docTitle,
  isPrivate,
  resolvedComments,
  selectedIds,
  setSelectedIds,
  onStart,
  onCancel,
}: {
  docTitle: string;
  isPrivate: boolean;
  resolvedComments: Comment[];
  selectedIds: string[];
  setSelectedIds: (ids: string[]) => void;
  onStart: () => void;
  onCancel: () => void;
}) {
  const selectedSet = new Set(selectedIds);
  const allSelected = selectedSet.size === resolvedComments.length;

  function toggle(id: string) {
    const next = new Set(selectedSet);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelectedIds([...next]);
  }
  function toggleAll() {
    setSelectedIds(allSelected ? [] : resolvedComments.map((c) => c.id));
  }

  return (
    <div className="p-6 max-w-2xl mx-auto w-full flex flex-col min-h-0">
      <p className="text-sm text-ink mb-4">
        Claude will read <strong>{docTitle}</strong> and apply the selected
        resolved comments. Nothing is saved until you review the diff and click
        Accept.
      </p>

      <div className="flex items-center justify-between mb-2">
        <div className="text-xs font-semibold uppercase tracking-wide text-muted">
          Comments to apply ({selectedIds.length} of {resolvedComments.length})
        </div>
        <button
          onClick={toggleAll}
          className="text-xs text-accent hover:underline"
        >
          {allSelected ? "Deselect all" : "Select all"}
        </button>
      </div>

      <div className="flex-1 min-h-0 overflow-auto border border-rule rounded-lg bg-card divide-y divide-rule mb-4">
        {resolvedComments.map((c) => {
          const checked = selectedSet.has(c.id);
          return (
            <label
              key={c.id}
              className="flex items-start gap-3 p-3 hover:bg-soft cursor-pointer"
            >
              <input
                type="checkbox"
                checked={checked}
                onChange={() => toggle(c.id)}
                className="mt-1 accent-accent"
              />
              <div className="flex-1 min-w-0 text-sm">
                <div className="text-xs italic text-muted line-clamp-1 mb-0.5">
                  “{c.anchor.exact}”
                </div>
                <div className="text-ink line-clamp-2">{c.body}</div>
                <div className="text-[11px] text-faint mt-0.5">
                  by {c.author}
                  {c.replies.length > 0 ? ` · ${c.replies.length} replies` : ""}
                </div>
              </div>
            </label>
          );
        })}
      </div>

      <div className="bg-soft border border-rule rounded p-3 text-xs text-muted mb-4">
        <strong className="text-ink">Heads up:</strong> the document content
        and selected comments will be sent to Anthropic via your own API key.
        {isPrivate && (
          <>
            {" "}This document is marked <strong>private</strong>; only sources
            you (the signed-in user) can read are sent.
          </>
        )}
      </div>

      <div className="flex justify-end gap-2 shrink-0">
        <button
          onClick={onCancel}
          className="text-sm px-3 py-2 text-muted hover:text-ink"
        >
          Cancel
        </button>
        <button
          onClick={onStart}
          disabled={selectedIds.length === 0}
          className="text-sm px-4 py-2 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
        >
          Revise with AI
        </button>
      </div>
    </div>
  );
}

function CenteredSpinner({ hint }: { hint: string }) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center p-10 gap-4 text-muted">
      <div className="w-12 h-12 rounded-full border-2 border-rule border-t-accent animate-spin" />
      <div className="text-sm">{hint}</div>
    </div>
  );
}

function StreamingPanel({
  streamed,
  elapsed,
  selectedCount,
  baseUrl,
}: {
  streamed: string;
  elapsed: number;
  selectedCount: number;
  baseUrl?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  // Auto-scroll the rendered output as new content arrives. Skip if the
  // user has scrolled up — don't fight them.
  const stickToBottom = useRef(true);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (!stickToBottom.current) return;
    el.scrollTop = el.scrollHeight;
  }, [streamed]);

  function onScroll(e: React.UIEvent<HTMLDivElement>) {
    const el = e.currentTarget;
    const atBottom =
      el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    stickToBottom.current = atBottom;
  }

  const approxTokens = Math.round(streamed.length / 4);

  return (
    <div className="flex flex-col flex-1 min-h-0">
      <div className="px-5 py-2 border-b border-rule bg-soft text-xs text-muted flex items-center justify-between shrink-0">
        <div className="flex items-center gap-2">
          <div className="w-3 h-3 rounded-full border-2 border-rule border-t-accent animate-spin" />
          {streamed.length === 0
            ? `Reading the document and ${selectedCount} resolved comment${selectedCount === 1 ? "" : "s"}…`
            : "Claude Opus 4.7 is writing the revision…"}
        </div>
        <div className="tabular-nums">
          ~{approxTokens.toLocaleString()} tokens · {elapsed}s
        </div>
      </div>
      <div
        ref={ref}
        onScroll={onScroll}
        className="flex-1 min-h-0 overflow-auto p-6"
      >
        {streamed.length === 0 ? (
          <div className="text-muted text-sm text-center py-10">
            Waiting for the first token from Claude…
          </div>
        ) : (
          <div className="max-w-3xl mx-auto relative">
            <MarkdownRender content={streamed} baseUrl={baseUrl} />
            <span
              aria-hidden
              className="inline-block w-1.5 h-4 bg-accent align-text-bottom ml-0.5 animate-pulse"
            />
          </div>
        )}
      </div>
    </div>
  );
}

function PreviewPanel({
  preview,
  doc,
  onAccept,
  onDiscard,
}: {
  preview: RevisionPreview;
  doc: MdDocument;
  onAccept: () => void;
  onDiscard: () => void;
}) {
  const cost = preview.costEstimateUsd;
  const costStr =
    cost < 0.005 ? "<$0.01" : `≈ $${cost.toFixed(cost < 1 ? 3 : 2)}`;
  const applied = preview.appliedCommentIds.length;

  return (
    <div className="flex flex-col flex-1 min-h-0">
      <div className="px-5 py-2 border-b border-rule bg-soft text-xs text-muted flex items-center justify-between shrink-0">
        <div>
          Applied <strong className="text-ink">{applied}</strong> resolved
          comment{applied === 1 ? "" : "s"} with{" "}
          <strong className="text-ink">{preview.model}</strong>
        </div>
        <div className="tabular-nums">
          {preview.tokensIn.toLocaleString()} in ·{" "}
          {preview.tokensOut.toLocaleString()} out · {costStr}
        </div>
      </div>

      <div className="flex-1 min-h-0">
        <DiffView
          original={preview.originalContent}
          revised={preview.revisedContent}
          baseUrl={baseURLForDoc(doc.sourceUrl)}
        />
      </div>

      <div className="border-t border-rule px-5 py-3 flex items-center justify-between shrink-0 bg-card">
        <div className="text-xs text-muted">
          Saving creates a new document with a new URL. The original (and its
          comments) stay untouched.
        </div>
        <div className="flex gap-2">
          <button
            onClick={onDiscard}
            className="text-sm px-3 py-2 text-muted hover:text-ink"
          >
            Discard
          </button>
          <button
            onClick={onAccept}
            disabled={preview.identical}
            title={preview.identical ? "Claude returned no changes" : ""}
            className="text-sm px-4 py-2 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
          >
            Save as new revision
          </button>
        </div>
      </div>
    </div>
  );
}
