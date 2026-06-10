import { Link } from "react-router-dom";
import type { MdDocument } from "../types";
import { formatRelative } from "../utils/format";

// Toolbar that lives at the top of the document content column: parent
// link (for AI revisions), title-as-rename-button, action buttons,
// revision-meta chip, "cloned from" + "updated" meta line, revisions
// list. Pulled out of DocumentPage so the page file stays focused on
// data flow + layout.

interface Props {
  doc: MdDocument;
  me: string;
  signedIn: boolean;
  onRename: () => void;
  onRevise: () => void;
  onEdit: () => void;
  /** Display name of another user currently holding the soft edit
   * lock; when set, the Edit button is disabled with a tooltip. */
  editLockedBy?: string;
  onPushback: () => void;
  onShare: () => void;
  onDownload: () => void;
  onDelete: () => void;
}

export default function DocumentToolbar({
  doc,
  me,
  signedIn,
  onRename,
  onRevise,
  onEdit,
  editLockedBy,
  onPushback,
  onShare,
  onDownload,
  onDelete,
}: Props) {
  const isGitHubDoc = Boolean(
    doc.sourceUrl && /^https:\/\/github\.com\//.test(doc.sourceUrl)
  );
  return (
    <>
      {/* Revision-chain breadcrumb. The vN labels make it obvious
          which version you're backing up to, even when every node
          shares the same title (the common case for AI revisions of
          a single source file). */}
      {doc.parent && (
        <div className="text-xs text-muted mb-1 flex items-center gap-2 flex-wrap">
          <Link to={`/d/${doc.parent.id}`} className="text-accent hover:underline">
            ← {doc.parent.revisionIndex ? `v${doc.parent.revisionIndex}` : "Previous version"}
          </Link>
          {doc.revisionIndex && (
            <span className="text-faint">
              · Viewing v{doc.revisionIndex}
              {doc.revisionTotal && doc.revisionTotal > doc.revisionIndex
                ? ` of ${doc.revisionTotal}`
                : ""}
            </span>
          )}
          {doc.latestDescendant && doc.latestDescendant.id !== doc.id && (
            <Link
              to={`/d/${doc.latestDescendant.id}`}
              className="text-accent hover:underline"
            >
              · Latest: v{doc.latestDescendant.revisionIndex ?? "?"} →
            </Link>
          )}
        </div>
      )}
      {!doc.parent && doc.latestDescendant && doc.latestDescendant.id !== doc.id && (
        <div className="text-xs text-muted mb-1 flex items-center gap-2">
          <span className="text-faint">Viewing v1 (original)</span>
          <Link
            to={`/d/${doc.latestDescendant.id}`}
            className="text-accent hover:underline"
          >
            · Latest: v{doc.latestDescendant.revisionIndex ?? "?"} →
          </Link>
        </div>
      )}

      {/* Title row */}
      <div className="flex items-center justify-between gap-3 mb-1">
        <button
          onClick={onRename}
          className="text-2xl font-semibold tracking-tight text-ink hover:text-accent text-left flex-1 min-w-0 truncate"
          title="Click to rename"
        >
          {doc.title}
        </button>
        <div className="flex items-center gap-3 text-sm shrink-0">
          <button
            onClick={onEdit}
            disabled={!!editLockedBy}
            className="inline-flex items-center gap-1 px-3 py-1.5 rounded border border-rule text-ink font-medium hover:bg-soft disabled:opacity-50 disabled:cursor-not-allowed"
            title={
              editLockedBy
                ? `${editLockedBy} is editing this document. Try again in a few minutes.`
                : "Edit the Markdown directly. Saving creates a new revision."
            }
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
              <path d="m18.5 2.5 3 3L12 15l-4 1 1-4 9.5-9.5z" />
            </svg>
            {editLockedBy ? `${editLockedBy} editing…` : "Edit"}
          </button>
          <button
            onClick={onRevise}
            className="inline-flex items-center gap-1 px-3 py-1.5 rounded bg-accent text-accent-fg font-medium hover:opacity-90"
            title="Have Claude apply your resolved comments"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 2 9 9l-7 1 5 5-1 7 6-4 6 4-1-7 5-5-7-1z" />
            </svg>
            Revise with AI
          </button>
          {isGitHubDoc && signedIn && (
            <button
              onClick={onPushback}
              className="text-muted hover:text-ink"
              title="Push this revision back to GitHub (PR or direct commit)"
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 19V5M5 12l7-7 7 7" />
              </svg>
            </button>
          )}
          <button onClick={onShare} className="text-muted hover:text-ink" title="Share this document">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="18" cy="5" r="3" />
              <circle cx="6" cy="12" r="3" />
              <circle cx="18" cy="19" r="3" />
              <line x1="8.59" y1="13.51" x2="15.42" y2="17.49" />
              <line x1="15.41" y1="6.51" x2="8.59" y2="10.49" />
            </svg>
          </button>
          <button onClick={onDownload} className="text-muted hover:text-ink" title="Download as .md">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3" />
            </svg>
          </button>
          <button onClick={onDelete} className="text-faint hover:text-danger">
            Delete
          </button>
        </div>
      </div>

      {doc.revisionMeta && (
        <div className="text-xs text-muted mb-1 inline-flex items-center gap-1.5 bg-accent-soft text-accent rounded px-2 py-0.5">
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 2 9 9l-7 1 5 5-1 7 6-4 6 4-1-7 5-5-7-1z" />
          </svg>
          {doc.revisionMeta.model === "manual" ? (
            <>Manual edit by {doc.revisionMeta.generatedBy}</>
          ) : (
            <>
              AI-revised by {doc.revisionMeta.generatedBy} — applied{" "}
              {doc.revisionMeta.appliedCommentIds.length} comment
              {doc.revisionMeta.appliedCommentIds.length === 1 ? "" : "s"}
            </>
          )}
        </div>
      )}

      <div className="text-xs text-muted mb-6">
        {doc.origin === "url" && doc.sourceUrl && (
          <>
            Cloned from{" "}
            <a href={doc.sourceUrl} target="_blank" rel="noreferrer" className="text-accent hover:underline">
              {doc.sourceUrl}
            </a>
            {" · "}
          </>
        )}
        updated {formatRelative(doc.updatedAt)}
        {!me && !signedIn && (
          <span className="ml-2 text-amber-600">· Set your name in the header to comment</span>
        )}
      </div>

      {doc.children && doc.children.length > 0 && (
        <div className="text-xs text-muted mb-6 flex items-center gap-2 flex-wrap">
          Direct revisions:
          {doc.children.map((c, i) => (
            <span key={c.id} className="inline-flex items-center gap-1">
              <Link to={`/d/${c.id}`} className="text-accent hover:underline">
                v{c.revisionIndex ?? (doc.revisionIndex ?? 1) + i + 1}
              </Link>
              {c.revisionMeta?.generatedBy && (
                <span className="text-faint">by {c.revisionMeta.generatedBy}</span>
              )}
            </span>
          ))}
        </div>
      )}
    </>
  );
}
