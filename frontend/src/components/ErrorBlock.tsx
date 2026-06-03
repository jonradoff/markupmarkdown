import { useState } from "react";
import type { APIError } from "../api";

interface Props {
  error: APIError;
  onDismiss?: () => void;
}

function iconFor(kind?: string) {
  // GitHub-related kinds show a GitHub mark; otherwise a generic warning.
  if (kind?.startsWith("github")) {
    return (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden>
        <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2c-3.2.7-3.87-1.36-3.87-1.36-.52-1.33-1.27-1.69-1.27-1.69-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.02 1.75 2.68 1.24 3.34.95.1-.74.4-1.24.72-1.53-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.04 0 0 .96-.31 3.15 1.18a10.96 10.96 0 0 1 5.74 0c2.19-1.49 3.15-1.18 3.15-1.18.63 1.58.23 2.75.12 3.04.73.81 1.18 1.84 1.18 3.1 0 4.43-2.69 5.41-5.25 5.69.41.36.78 1.06.78 2.13v3.16c0 .31.21.67.8.56C20.21 21.39 23.5 17.08 23.5 12 23.5 5.65 18.35.5 12 .5z" />
      </svg>
    );
  }
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
      <line x1="12" y1="9" x2="12" y2="13" />
      <line x1="12" y1="17" x2="12.01" y2="17" />
    </svg>
  );
}

export default function ErrorBlock({ error, onDismiss }: Props) {
  const [showDetail, setShowDetail] = useState(false);

  const isExternal = (u: string) => u.startsWith("http://") || u.startsWith("https://");

  return (
    <div className="border border-rule rounded-lg p-4 bg-soft text-sm flex gap-3 items-start">
      <div className="text-danger shrink-0 mt-0.5">{iconFor(error.kind)}</div>
      <div className="flex-1 min-w-0">
        <div className="text-ink whitespace-pre-wrap break-words">{error.message}</div>

        {error.actions && error.actions.length > 0 && (
          <div className="mt-3 flex flex-wrap gap-2">
            {error.actions.map((a, i) => (
              <a
                key={i}
                href={a.url}
                target={isExternal(a.url) ? "_blank" : undefined}
                rel={isExternal(a.url) ? "noreferrer" : undefined}
                className="inline-flex items-center gap-1 px-3 py-1.5 rounded bg-accent text-accent-fg text-xs font-medium hover:opacity-90"
              >
                {a.label}
                {isExternal(a.url) && <span aria-hidden>↗</span>}
              </a>
            ))}
          </div>
        )}

        {error.detail && (
          <div className="mt-3">
            <button
              onClick={() => setShowDetail((v) => !v)}
              className="text-xs text-muted hover:text-ink"
            >
              {showDetail ? "Hide" : "Show"} technical details
            </button>
            {showDetail && (
              <pre className="mt-2 text-[11px] text-muted bg-card border border-rule rounded p-2 overflow-x-auto whitespace-pre-wrap break-words">
                {error.detail}
              </pre>
            )}
          </div>
        )}
      </div>
      {onDismiss && (
        <button
          onClick={onDismiss}
          aria-label="Dismiss"
          className="text-faint hover:text-ink shrink-0"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <line x1="18" y1="6" x2="6" y2="18" />
            <line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      )}
    </div>
  );
}
