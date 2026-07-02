import { useState } from "react";
import { api, APIError } from "../api";
import type { MdDocument, Review, ReviewState } from "../types";

interface Props {
  doc: MdDocument;
  onDocRefresh: () => Promise<void>;
  onError: (err: APIError) => void;
}

/** Review-state buttons + agent-proposed banner. Rendered near the top
 * of the doc page so the coordination surface is visible before the
 * reviewer scrolls through the content. Kept intentionally minimal —
 * three buttons, a summary badge, and the accept-revision affordance
 * when applicable. No modal, no separate reviewer list; the aggregate
 * count is enough for the MVP. */
export default function ReviewBar({ doc, onDocRefresh, onError }: Props) {
  const [busy, setBusy] = useState(false);
  const my = doc.myReview;
  const summary = doc.reviews;

  async function setState(state: ReviewState) {
    if (busy) return;
    setBusy(true);
    try {
      // Clicking the button you already have set toggles it off.
      if (my?.state === state) {
        await api.deleteReview(doc.id);
      } else {
        await api.setReview(doc.id, state);
      }
      await onDocRefresh();
    } catch (err) {
      if (err instanceof APIError) onError(err);
    } finally {
      setBusy(false);
    }
  }

  async function acceptRevision() {
    if (busy) return;
    setBusy(true);
    try {
      await api.acceptAgentRevision(doc.id);
      await onDocRefresh();
    } catch (err) {
      if (err instanceof APIError) onError(err);
    } finally {
      setBusy(false);
    }
  }

  const buttons: { state: ReviewState; label: string; tone: string }[] = [
    { state: "commented", label: "Comment", tone: "text-ink" },
    { state: "approved", label: "Approve", tone: "text-success" },
    { state: "changes_requested", label: "Request changes", tone: "text-danger" },
  ];

  return (
    <div className="mb-4 space-y-2">
      {doc.agentProposed && <AgentProposedBanner meta={doc.revisionMeta} onAccept={acceptRevision} busy={busy} />}
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="text-muted">Review:</span>
        {buttons.map((b) => {
          const active = my?.state === b.state;
          return (
            <button
              key={b.state}
              onClick={() => setState(b.state)}
              disabled={busy}
              className={
                "px-2 py-1 rounded border transition " +
                (active
                  ? `border-accent bg-accent-soft ${b.tone} font-medium`
                  : "border-rule text-muted hover:text-ink hover:border-ink") +
                " disabled:opacity-50"
              }
              title={active ? "Click again to clear your review" : undefined}
            >
              {b.label}
            </button>
          );
        })}
        {summary && <SummaryBadge summary={summary} myReview={my} />}
      </div>
    </div>
  );
}

function SummaryBadge({
  summary,
  myReview,
}: {
  summary: NonNullable<MdDocument["reviews"]>;
  myReview?: Review;
}) {
  const parts: string[] = [];
  if (summary.approved > 0) parts.push(`${summary.approved} approved`);
  if (summary.changesRequested > 0)
    parts.push(`${summary.changesRequested} changes requested`);
  if (summary.commented > 0) parts.push(`${summary.commented} commented`);
  if (parts.length === 0) return null;
  return (
    <span className="ml-auto text-xs text-muted">
      {parts.join(" · ")}
      {myReview ? " (incl. you)" : ""}
    </span>
  );
}

function AgentProposedBanner({
  meta,
  onAccept,
  busy,
}: {
  meta: MdDocument["revisionMeta"];
  onAccept: () => Promise<void>;
  busy: boolean;
}) {
  const author = meta?.generatedBy || "an agent";
  return (
    <div className="rounded-md border border-accent bg-accent-soft px-3 py-2 flex items-center justify-between gap-3">
      <div className="text-sm">
        <span className="font-medium">Agent-proposed revision</span>
        <span className="text-muted">
          {" "}
          — written by {author}. Accept it to allow Push to GitHub, or reject by opening a fresh
          revision.
        </span>
      </div>
      <button
        onClick={onAccept}
        disabled={busy}
        className="shrink-0 text-xs px-3 py-1.5 rounded bg-accent text-accent-fg hover:opacity-90 disabled:opacity-50"
      >
        Accept revision
      </button>
    </div>
  );
}
