import { useState } from "react";

interface Props {
  /** Owner / repo / file path on GitHub, for the "View on GitHub" link. */
  githubURL: string;
  /** When drift was first observed. */
  driftedAt?: string;
  /** Whether the viewer is allowed to sync (admin scope / cookie session). */
  canSync: boolean;
  onSync: () => Promise<void> | void;
  /** When set, the current doc is a child revision and sync should
   * happen on the root. Renders "Open original" instead of "Sync". */
  rootDoc?: { id: string; title: string };
}

// Banner shown at the top of the doc page when the source file on GitHub
// has a different SHA than the cloned copy. Clicking Sync pulls the
// latest content and re-anchors comments where it can; orphans surface
// in the section below the doc body.
export default function SourceDriftBanner({
  githubURL,
  driftedAt,
  canSync,
  onSync,
  rootDoc,
}: Props) {
  const [busy, setBusy] = useState(false);
  async function handleSync() {
    if (busy) return;
    setBusy(true);
    try {
      await onSync();
    } finally {
      setBusy(false);
    }
  }
  const when = driftedAt
    ? new Date(driftedAt).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      })
    : null;

  return (
    <div className="mb-6 rounded-lg border border-amber-300 bg-amber-50 dark:bg-amber-900/20 dark:border-amber-800 p-3 flex items-start gap-3">
      <div className="shrink-0 text-amber-700 dark:text-amber-300 mt-0.5">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
      </div>
      <div className="flex-1 min-w-0 text-sm">
        <div className="font-medium text-amber-900 dark:text-amber-100">
          Source updated on GitHub{when ? ` · noticed ${when}` : ""}
        </div>
        <div className="text-amber-800 dark:text-amber-200/80 mt-0.5">
          {rootDoc ? (
            <>
              The original source on GitHub has new commits since{" "}
              <em>{rootDoc.title}</em> was cloned. This is an AI revision —
              open the original to sync from GitHub. Comments there are
              re-anchored automatically where the quote still appears; the
              rest become orphans you can manually re-anchor.
            </>
          ) : (
            <>
              The underlying file has new commits since this doc was cloned.
              Sync to pull in the latest version — comments are re-anchored
              automatically where the original quoted text still appears; the
              rest surface as orphans below the doc with a manual re-anchor
              flow.
            </>
          )}
        </div>
        <div className="mt-2 flex items-center gap-2">
          {rootDoc ? (
            <a
              href={`/d/${rootDoc.id}`}
              className="text-xs px-3 py-1 rounded bg-amber-600 text-white font-medium hover:bg-amber-700"
            >
              Open original
            </a>
          ) : (
            canSync && (
              <button
                onClick={handleSync}
                disabled={busy}
                className="text-xs px-3 py-1 rounded bg-amber-600 text-white font-medium hover:bg-amber-700 disabled:opacity-50"
              >
                {busy ? "Syncing…" : "Sync from GitHub"}
              </button>
            )
          )}
          <a
            href={githubURL}
            target="_blank"
            rel="noreferrer"
            className="text-xs text-amber-900 dark:text-amber-100 underline hover:no-underline"
          >
            View latest on GitHub
          </a>
        </div>
      </div>
    </div>
  );
}
