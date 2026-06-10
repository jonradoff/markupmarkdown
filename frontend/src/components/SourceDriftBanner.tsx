interface Props {
  /** Owner / repo / file path on GitHub, for the "View on GitHub" link. */
  githubURL: string;
  /** When drift was first observed. */
  driftedAt?: string;
  /** Whether the viewer is allowed to merge (admin scope / cookie session). */
  canSync: boolean;
  /** Opens the merge modal. For non-revisions it's a trivial replace;
   * for revisions it runs the 3-way Claude merge with a preview. */
  onMerge: () => void;
  /** Opens the Ignore-this-drift confirmation modal. Dismissed drifts
   * stay suppressed until a *newer* upstream SHA appears. Same gating
   * as onMerge — surfaced only when canSync is true. */
  onIgnore: () => void;
  /** True when this doc is an AI revision (has revision_meta). The
   * banner copy adapts to explain the merge will reconcile both
   * branches' edits. */
  isRevision: boolean;
}

// Banner shown at the top of the doc page when the source file on GitHub
// has a different SHA than the cloned copy. Clicking Sync pulls the
// latest content and re-anchors comments where it can; orphans surface
// in the section below the doc body.
export default function SourceDriftBanner({
  githubURL,
  driftedAt,
  canSync,
  onMerge,
  onIgnore,
  isRevision,
}: Props) {
  const when = driftedAt
    ? new Date(driftedAt).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      })
    : null;

  return (
    <div
      className="mb-6 rounded-lg border p-3 flex items-start gap-3"
      style={{
        backgroundColor: "var(--color-warn-bg)",
        borderColor: "var(--color-warn-border)",
        color: "var(--color-warn-ink)",
      }}
    >
      <div className="shrink-0 mt-0.5" style={{ color: "var(--color-warn-muted)" }}>
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
      </div>
      <div className="flex-1 min-w-0 text-sm">
        <div className="font-medium">
          Source updated on GitHub{when ? ` · noticed ${when}` : ""}
        </div>
        <div className="mt-0.5" style={{ color: "var(--color-warn-muted)" }}>
          {isRevision ? (
            <>
              This document is an AI revision; the original source on GitHub
              has new commits since it was generated. The merge runs a
              Claude-powered 3-way reconciliation so both the upstream edits
              and your AI revision land in the result. You'll get a diff
              preview before anything is saved.
            </>
          ) : (
            <>
              The underlying file has new commits since this doc was cloned.
              Merge to pull in the latest version — comments are re-anchored
              automatically where the original quoted text still appears; the
              rest surface as orphans below the doc with a manual re-anchor
              flow.
            </>
          )}
        </div>
        <div className="mt-2 flex items-center gap-2">
          {canSync && (
            <button
              onClick={onMerge}
              className="text-xs px-3 py-1 rounded font-medium transition-colors"
              style={{
                backgroundColor: "var(--color-warn-action)",
                color: "var(--color-warn-action-fg)",
              }}
              onMouseEnter={(e) =>
                (e.currentTarget.style.backgroundColor =
                  "var(--color-warn-action-hover)")
              }
              onMouseLeave={(e) =>
                (e.currentTarget.style.backgroundColor =
                  "var(--color-warn-action)")
              }
            >
              Merge changes from GitHub
            </button>
          )}
          <a
            href={githubURL}
            target="_blank"
            rel="noreferrer"
            className="text-xs underline hover:no-underline"
          >
            View latest on GitHub
          </a>
          {canSync && (
            <button
              onClick={onIgnore}
              className="text-xs ml-2 underline hover:no-underline"
              style={{ color: "var(--color-warn-muted)" }}
              title="Hide this banner until a newer upstream commit shows up"
            >
              Ignore
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
