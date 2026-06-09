import { useEffect, useRef, useState } from "react";
import { api, APIError } from "../api";
import type { MdDocument, MergePreview } from "../types";
import DiffView from "./DiffView";
import ErrorBlock from "./ErrorBlock";
import { useToast } from "./Toast";

interface Props {
  doc: MdDocument;
  onClose: () => void;
  onMerged: (summary: { cleanCount: number; orphanCount: number }) => void;
}

type Phase = "intro" | "generating" | "preview" | "accepting" | "error";

// MergeModal walks the user through a 3-way merge of the doc's current
// content with the latest GitHub source. Streaming preview, diff view,
// confirm-to-commit. Mirrors ReviseModal's UX shape so the two AI
// surfaces feel like the same family.
export default function MergeModal({ doc, onClose, onMerged }: Props) {
  const [phase, setPhase] = useState<Phase>("intro");
  const [preview, setPreview] = useState<MergePreview | null>(null);
  const [error, setError] = useState<APIError | null>(null);
  const [streamed, setStreamed] = useState("");
  const [elapsed, setElapsed] = useState(0);
  const [acceptSlow, setAcceptSlow] = useState(false);
  const streamedRef = useRef("");
  const toast = useToast();

  // Streaming ticker — keeps an "elapsed Ns" indicator alive during the
  // long-running Claude call so the user knows we're still working.
  useEffect(() => {
    if (phase !== "generating") return;
    let cancelled = false;
    streamedRef.current = "";
    setStreamed("");
    setElapsed(0);
    const start = Date.now();
    const tick = window.setInterval(() => {
      if (cancelled) return;
      setElapsed(Math.floor((Date.now() - start) / 1000));
    }, 250);
    const controller = new AbortController();
    (async () => {
      try {
        const result = await api.mergePreviewStream(
          doc.id,
          (chunk) => {
            streamedRef.current += chunk;
            setStreamed(streamedRef.current);
          },
          controller.signal
        );
        if (cancelled) return;
        setPreview(result);
        setPhase("preview");
      } catch (err) {
        if (cancelled) return;
        const apiErr =
          err instanceof APIError
            ? err
            : new APIError((err as Error).message || "Merge failed");
        setError(apiErr);
        setPhase("error");
      }
    })();
    return () => {
      cancelled = true;
      controller.abort();
      window.clearInterval(tick);
    };
  }, [phase, doc.id]);

  // Slow-accept hint timer.
  useEffect(() => {
    if (phase !== "accepting") {
      setAcceptSlow(false);
      return;
    }
    const t = window.setTimeout(() => setAcceptSlow(true), 12_000);
    return () => window.clearTimeout(t);
  }, [phase]);

  async function handleAccept() {
    if (!preview) return;
    setPhase("accepting");
    try {
      const res = await api.mergeAcceptSource(doc.id, {
        mergedContent: preview.mergedContent,
        upstreamContent: preview.upstreamContent,
        upstreamSourceSha: preview.upstreamSourceSha,
        model: preview.model,
        tokensIn: preview.tokensIn,
        tokensOut: preview.tokensOut,
      });
      onMerged({
        cleanCount: res?.cleanCount ?? 0,
        orphanCount: res?.orphanCount ?? 0,
      });
      onClose();
      const tail =
        res?.orphanCount > 0
          ? ` — ${res.cleanCount} re-anchored, ${res.orphanCount} orphan${res.orphanCount === 1 ? "" : "s"}`
          : "";
      toast.success(`Merged GitHub source into your revision${tail}.`);
    } catch (err) {
      const apiErr =
        err instanceof APIError
          ? err
          : new APIError((err as Error).message || "Couldn't apply the merge");
      setError(apiErr);
      setPhase("error");
    }
  }

  const isRevision = Boolean(doc.parentId || doc.revisionMeta);

  return (
    <div className="fixed inset-0 z-40 bg-black/40 flex items-center justify-center p-4">
      <div className="bg-card border border-rule rounded-lg shadow-xl max-w-5xl w-full max-h-[90vh] flex flex-col overflow-hidden">
        <div className="px-5 py-3 border-b border-rule flex items-center justify-between shrink-0">
          <h2 className="text-lg font-semibold">Merge changes from GitHub</h2>
          <button
            onClick={onClose}
            className="text-muted hover:text-ink text-sm"
            disabled={phase === "generating" || phase === "accepting"}
          >
            ✕
          </button>
        </div>

        <div className="flex-1 min-h-0 overflow-auto p-5">
          {phase === "intro" && (
            <div className="space-y-4 text-sm">
              {isRevision ? (
                <>
                  <p>
                    This document is an <strong>AI revision</strong>. The
                    original source on GitHub has changed since the revision
                    was generated.
                  </p>
                  <p>
                    Click <em>Run merge</em> below and Claude will produce a
                    new version that incorporates <em>both</em>: the upstream
                    edits (the new commits on GitHub) and your AI revision.
                    You'll get a diff preview before anything is saved.
                  </p>
                </>
              ) : (
                <p>
                  The source on GitHub has new commits since this doc was
                  cloned. Click <em>Run merge</em> to pull the latest content;
                  for a non-revised doc this is a straight replacement (no
                  Claude call, no token cost).
                </p>
              )}
              <p className="text-muted">
                The merge uses your Anthropic API key. You'll see the merged
                Markdown stream as it generates.
              </p>
              <div className="flex justify-end gap-2 pt-2">
                <button
                  onClick={onClose}
                  className="px-3 py-1.5 rounded text-sm text-muted hover:text-ink"
                >
                  Cancel
                </button>
                <button
                  onClick={() => setPhase("generating")}
                  className="px-4 py-1.5 rounded bg-accent text-accent-fg text-sm font-medium hover:opacity-90"
                >
                  Run merge
                </button>
              </div>
            </div>
          )}

          {phase === "generating" && (
            <div className="space-y-3">
              <div className="text-sm text-muted flex items-center gap-3">
                <span className="inline-flex items-center gap-2">
                  <span className="w-2 h-2 rounded-full bg-accent animate-pulse" />
                  Merging — {elapsed}s
                </span>
                <span className="text-faint">
                  Streaming the merged Markdown from Claude…
                </span>
              </div>
              <pre className="text-xs whitespace-pre-wrap bg-soft p-3 rounded border border-rule max-h-[60vh] overflow-auto font-mono">
                {streamed || "…"}
              </pre>
            </div>
          )}

          {phase === "preview" && preview && (
            <div className="space-y-4">
              <div className="flex items-start gap-3 text-sm flex-wrap">
                <div>
                  <div className="text-muted">Model</div>
                  <div className="font-medium">{preview.model}</div>
                </div>
                <div>
                  <div className="text-muted">Tokens</div>
                  <div className="font-medium tabular-nums">
                    {preview.tokensIn.toLocaleString()} in /{" "}
                    {preview.tokensOut.toLocaleString()} out
                  </div>
                </div>
                <div>
                  <div className="text-muted">Estimated cost</div>
                  <div className="font-medium tabular-nums">
                    ${preview.costEstimateUsd.toFixed(4)}
                  </div>
                </div>
                {preview.noMergeNeeded && (
                  <div className="text-xs px-2 py-1 rounded bg-soft text-muted self-center">
                    No Claude call — trivial merge
                  </div>
                )}
              </div>
              {preview.identical ? (
                <div className="text-sm text-muted italic">
                  The merge resolved to your current content. Nothing to apply.
                </div>
              ) : (
                <DiffView
                  original={doc.content}
                  revised={preview.mergedContent}
                />
              )}
              <div className="flex justify-end gap-2 pt-2">
                <button
                  onClick={onClose}
                  className="px-3 py-1.5 rounded text-sm text-muted hover:text-ink"
                >
                  Discard
                </button>
                <button
                  onClick={handleAccept}
                  disabled={preview.identical}
                  className="px-4 py-1.5 rounded bg-accent text-accent-fg text-sm font-medium hover:opacity-90 disabled:opacity-50"
                >
                  Apply merge
                </button>
              </div>
            </div>
          )}

          {phase === "accepting" && (
            <div className="space-y-3 text-sm">
              <div className="flex items-center gap-3">
                <span className="w-2 h-2 rounded-full bg-accent animate-pulse" />
                Applying merge…
              </div>
              {acceptSlow && (
                <div className="text-muted text-xs">
                  Still working — re-anchoring comments against the merged
                  content. This can take a moment on long documents.
                </div>
              )}
            </div>
          )}

          {phase === "error" && error && (
            <div className="space-y-3">
              <ErrorBlock error={error} />
              <div className="flex justify-end gap-2">
                <button
                  onClick={onClose}
                  className="px-3 py-1.5 rounded text-sm text-muted hover:text-ink"
                >
                  Close
                </button>
                <button
                  onClick={() => {
                    setError(null);
                    setPreview(null);
                    setPhase("intro");
                  }}
                  className="px-4 py-1.5 rounded bg-accent text-accent-fg text-sm font-medium hover:opacity-90"
                >
                  Try again
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
