import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, APIError } from "../api";
import ErrorBlock from "../components/ErrorBlock";
import type { AnchorSpec } from "../utils/anchor";
import {
  applyHighlights,
  getHighlightRect,
  getSelectionAnchor,
  unwrapHighlights,
} from "../utils/anchor";
import type { Comment, MdDocument } from "../types";
import MarkdownRender from "../components/MarkdownRender";
import { baseURLForDoc } from "../utils/baseUrl";
import SelectionPopover from "../components/SelectionPopover";
import NewCommentComposer from "../components/NewCommentComposer";
import CommentCard from "../components/CommentCard";
import { getAuthor } from "../utils/author";
import { useAuth } from "../auth";
import SignInModal from "../components/SignInModal";
import APIKeyModal from "../components/APIKeyModal";
import ReviseModal from "../components/ReviseModal";
import { useDialog } from "../components/Dialogs";
import { formatRelative } from "../utils/format";
import { downloadAsMarkdown } from "../utils/download";

type Filter = "open" | "resolved" | "all";

export default function DocumentPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { user } = useAuth();
  const dialog = useDialog();
  const [showSignIn, setShowSignIn] = useState(false);
  const [pendingAction, setPendingAction] = useState<(() => void) | null>(null);
  const [showAPIKey, setShowAPIKey] = useState(false);
  const [showRevise, setShowRevise] = useState(false);
  const [reviseSignInExplain, setReviseSignInExplain] = useState(false);

  const [doc, setDoc] = useState<MdDocument | null>(null);
  const [comments, setComments] = useState<Comment[]>([]);
  const [error, setError] = useState<APIError | null>(null);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>("open");

  const [selection, setSelection] = useState<{
    anchor: AnchorSpec;
    popX: number;
    popY: number;
  } | null>(null);
  const [composer, setComposer] = useState<{ anchor: AnchorSpec; y: number } | null>(null);

  const contentRef = useRef<HTMLDivElement>(null);
  const sidebarRef = useRef<HTMLDivElement>(null);

  // Load doc + comments. If the doc is private and the user can't access it,
  // surface the structured error (sign-in or "no access") and render nothing.
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    (async () => {
      try {
        const d = await api.getDocument(id);
        if (cancelled) return;
        const cs = await api.listComments(id);
        if (cancelled) return;
        setDoc(d);
        setComments(cs);
      } catch (err) {
        if (cancelled) return;
        if (err instanceof APIError) setError(err);
        else setError(new APIError((err as Error).message));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [id]);

  // If the doc has a newer descendant, ask the user once (per session) if
  // they'd like to jump to it. Dismissals are remembered so navigating back
  // to this doc doesn't keep popping the prompt.
  useEffect(() => {
    if (!doc || !doc.latestDescendant) return;
    const latestId = doc.latestDescendant.id;
    if (latestId === doc.id) return;
    const dismissKey = `mm:dismissed-latest:${doc.id}`;
    if (sessionStorage.getItem(dismissKey) === "1") return;
    let cancelled = false;
    (async () => {
      const ok = await dialog.confirm({
        title: "A newer revision exists",
        body: `"${doc.latestDescendant!.title}" is the most recent AI-revised version of this document. Open it instead?`,
        confirmLabel: "Open latest",
        cancelLabel: "Stay here",
      });
      if (cancelled) return;
      if (ok) {
        navigate(`/d/${latestId}`);
      } else {
        sessionStorage.setItem(dismissKey, "1");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [doc, dialog, navigate]);

  // Apply highlights after every render of doc content / comments / active
  useLayoutEffect(() => {
    const el = contentRef.current;
    if (!el || !doc) return;
    const ranges = comments
      .filter((c) => (filter === "all" ? true : filter === "resolved" ? c.resolved : !c.resolved))
      .map((c) => ({
        id: c.id,
        start: c.anchor.start,
        end: c.anchor.end,
        resolved: c.resolved,
        active: c.id === activeId,
      }));
    applyHighlights(el, ranges);
    return () => {
      // Cleanup highlights so React's reconciler can manipulate the DOM
      // cleanly on next render.
      if (contentRef.current) unwrapHighlights(contentRef.current);
    };
  }, [doc, comments, activeId, filter]);

  // Click on a highlighted span -> activate that comment
  useEffect(() => {
    const el = contentRef.current;
    if (!el) return;
    function onClick(e: MouseEvent) {
      const target = (e.target as HTMLElement).closest("span.mm-highlight");
      if (!target) return;
      const cid = (target as HTMLElement).dataset.commentId;
      if (cid) setActiveId(cid);
    }
    el.addEventListener("click", onClick);
    return () => el.removeEventListener("click", onClick);
  }, [doc]);

  // Track text selection inside the content area to show floating popover
  const handleSelectionChange = useCallback(() => {
    const el = contentRef.current;
    if (!el) return;
    const sel = window.getSelection();
    if (!sel || sel.rangeCount === 0 || sel.isCollapsed) {
      setSelection(null);
      return;
    }
    if (!el.contains(sel.anchorNode) || !el.contains(sel.focusNode)) {
      setSelection(null);
      return;
    }
    const anchor = getSelectionAnchor(el);
    if (!anchor) {
      setSelection(null);
      return;
    }
    const rect = sel.getRangeAt(0).getBoundingClientRect();
    setSelection({
      anchor,
      popX: rect.left + rect.width / 2,
      popY: rect.top - 8,
    });
  }, []);

  useEffect(() => {
    document.addEventListener("selectionchange", handleSelectionChange);
    return () =>
      document.removeEventListener("selectionchange", handleSelectionChange);
  }, [handleSelectionChange]);

  const refreshComments = useCallback(async () => {
    if (!id) return;
    try {
      const cs = await api.listComments(id);
      setComments(cs);
    } catch {
      // ignore — most likely the doc was deleted out from under us
    }
  }, [id]);

  // Realtime: subscribe to server-sent events for this doc and refetch
  // the comments list on any change. EventSource auto-reconnects on drop.
  useEffect(() => {
    if (!id) return;
    const es = new EventSource(`/api/documents/${id}/events`, {
      withCredentials: true,
    });
    const onUpdate = () => refreshComments();
    es.addEventListener("comments-updated", onUpdate);
    return () => {
      es.removeEventListener("comments-updated", onUpdate);
      es.close();
    };
  }, [id, refreshComments]);

  function withIdentity(fn: () => void) {
    if (user || getAuthor()) {
      fn();
      return;
    }
    setPendingAction(() => fn);
    setShowSignIn(true);
  }

  async function submitNewComment(body: string) {
    if (!id || !composer) return;
    const author = user?.name || user?.login || getAuthor() || "Anonymous";
    const c = await api.createComment(id, {
      anchor: composer.anchor,
      body,
      author,
    });
    setComposer(null);
    setComments((prev) => [...prev, c]);
    setActiveId(c.id);
    window.getSelection()?.removeAllRanges();
    setSelection(null);
  }

  function openComposerForSelection() {
    if (!selection) return;
    const sel = selection;
    withIdentity(() => {
      setComposer({
        anchor: sel.anchor,
        y: sel.popY + window.scrollY,
      });
      setSelection(null);
    });
  }

  async function handleResolve(c: Comment) {
    const author = user?.name || user?.login || getAuthor() || "Anonymous";
    const updated = await api.resolveComment(c.id, author);
    setComments((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
  }
  async function handleReopen(c: Comment) {
    const updated = await api.reopenComment(c.id);
    setComments((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
  }
  async function handleReply(c: Comment, body: string) {
    const author = user?.name || user?.login || getAuthor() || "Anonymous";
    const updated = await api.createReply(c.id, body, author);
    setComments((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
  }
  async function handleEdit(c: Comment, body: string) {
    const updated = await api.editComment(c.id, body);
    setComments((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
  }
  async function handleDelete(c: Comment) {
    await api.deleteComment(c.id);
    setComments((prev) => prev.filter((x) => x.id !== c.id));
    if (activeId === c.id) setActiveId(null);
  }
  async function handleEditReply(c: Comment, replyId: string, body: string) {
    const updated = await api.editReply(c.id, replyId, body);
    setComments((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
  }
  async function handleDeleteReply(c: Comment, replyId: string) {
    const updated = await api.deleteReply(c.id, replyId);
    setComments((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
  }

  const visibleComments = useMemo(() => {
    return comments
      .filter((c) =>
        filter === "all" ? true : filter === "resolved" ? c.resolved : !c.resolved
      )
      .sort((a, b) => a.anchor.start - b.anchor.start);
  }, [comments, filter]);

  const openCount = comments.filter((c) => !c.resolved).length;
  const resolvedCount = comments.length - openCount;

  async function handleReviseClick() {
    if (!user) {
      setReviseSignInExplain(true);
      setShowSignIn(true);
      return;
    }
    if (resolvedCount === 0) {
      await dialog.alert({
        title: "Nothing to revise yet",
        body: "AI revision only applies comment threads you've marked as Done. Resolve at least one comment first.",
        confirmLabel: "Got it",
      });
      return;
    }
    try {
      const status = await api.getAnthropicKey();
      if (!status.hasKey) {
        setShowAPIKey(true);
        return;
      }
    } catch {
      // proceed to modal which will surface the error
    }
    setShowRevise(true);
  }

  function handleDownload() {
    if (!doc) return;
    downloadAsMarkdown(doc.title, doc.content);
  }

  // Scroll markdown to bring highlight into view when activeId changes
  useEffect(() => {
    if (!activeId || !contentRef.current) return;
    const rect = getHighlightRect(contentRef.current, activeId);
    if (!rect) return;
    const margin = 100;
    if (rect.top < margin || rect.bottom > window.innerHeight - margin) {
      window.scrollTo({
        top: window.scrollY + rect.top - 150,
        behavior: "smooth",
      });
    }
  }, [activeId]);

  // Move active comment by `dir` (-1 prev, +1 next). Wraps at the ends so
  // pressing Next on the last comment goes back to the first.
  const stepComment = useCallback(
    (dir: -1 | 1) => {
      if (visibleComments.length === 0) return;
      const idx = visibleComments.findIndex((c) => c.id === activeId);
      let next: number;
      if (idx === -1) {
        next = dir > 0 ? 0 : visibleComments.length - 1;
      } else {
        next = (idx + dir + visibleComments.length) % visibleComments.length;
      }
      setActiveId(visibleComments[next].id);
    },
    [visibleComments, activeId]
  );

  const activeIndex = useMemo(
    () => visibleComments.findIndex((c) => c.id === activeId),
    [visibleComments, activeId]
  );

  // Keyboard: j/k or ↑/↓ to step through comments when no input is focused.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA") return;
      if (visibleComments.length === 0) return;

      let dir: -1 | 1 | 0 = 0;
      if (e.key === "j" || e.key === "ArrowDown") dir = 1;
      if (e.key === "k" || e.key === "ArrowUp") dir = -1;
      if (!dir) return;
      e.preventDefault();
      stepComment(dir);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [visibleComments, stepComment]);

  async function renameDoc() {
    if (!doc) return;
    const next = await dialog.prompt({
      title: "Rename document",
      defaultValue: doc.title,
      placeholder: "New title",
      confirmLabel: "Rename",
    });
    if (next && next.trim() && next !== doc.title) {
      const updated = await api.renameDocument(doc.id, next.trim());
      setDoc(updated);
    }
  }

  async function deleteDoc() {
    if (!doc) return;
    const ok = await dialog.confirm({
      title: "Delete document?",
      body: `Delete "${doc.title}" and all its comments? This cannot be undone.`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    await api.deleteDocument(doc.id);
    navigate("/");
  }

  // Render NOTHING of the doc when access is denied. Private docs that the
  // viewer can't read fall here with a sign-in or "no access" message.
  if (error) {
    const isAccessGate =
      error.kind === "sign_in_required" || error.kind === "no_github_access";
    return (
      <div className="max-w-2xl mx-auto px-6 py-16">
        {isAccessGate && (
          <div className="text-center mb-6">
            <div className="inline-flex items-center justify-center w-12 h-12 rounded-full bg-soft text-muted mb-3">
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <rect x="3" y="11" width="18" height="11" rx="2" />
                <path d="M7 11V7a5 5 0 0 1 10 0v4" />
              </svg>
            </div>
            <h1 className="text-xl font-semibold tracking-tight">
              {error.kind === "sign_in_required"
                ? "This document is private"
                : "No access"}
            </h1>
          </div>
        )}
        <ErrorBlock error={error} />
        <div className="text-center mt-6">
          <Link to="/" className="text-sm text-muted hover:text-accent">
            ← All docs
          </Link>
        </div>
      </div>
    );
  }
  if (!doc) {
    return (
      <div className="max-w-3xl mx-auto px-6 py-10 text-muted">Loading…</div>
    );
  }

  const me = getAuthor();

  return (
    <div className="flex h-full">
      {/* Main content */}
      <div className="flex-1 min-w-0 overflow-y-auto">
        <div className="max-w-3xl mx-auto px-8 py-8">
          {/* Parent link (if this doc was AI-revised from another) */}
          {doc.parent && (
            <div className="text-xs text-muted mb-1">
              ← Revised from{" "}
              <Link
                to={`/d/${doc.parent.id}`}
                className="text-accent hover:underline"
              >
                {doc.parent.title}
              </Link>
            </div>
          )}

          {/* Title row */}
          <div className="flex items-center justify-between gap-3 mb-1">
            <button
              onClick={renameDoc}
              className="text-2xl font-semibold tracking-tight text-ink hover:text-accent text-left flex-1 min-w-0 truncate"
              title="Click to rename"
            >
              {doc.title}
            </button>
            <div className="flex items-center gap-3 text-sm shrink-0">
              <button
                onClick={handleReviseClick}
                className="inline-flex items-center gap-1 px-3 py-1.5 rounded bg-accent text-accent-fg font-medium hover:opacity-90"
                title="Have Claude apply your resolved comments"
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M12 2 9 9l-7 1 5 5-1 7 6-4 6 4-1-7 5-5-7-1z" />
                </svg>
                Revise with AI
              </button>
              <button
                onClick={handleDownload}
                className="text-muted hover:text-ink"
                title="Download as .md"
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3" />
                </svg>
              </button>
              <button
                onClick={deleteDoc}
                className="text-faint hover:text-danger"
              >
                Delete
              </button>
            </div>
          </div>

          {doc.revisionMeta && (
            <div className="text-xs text-muted mb-1 inline-flex items-center gap-1.5 bg-accent-soft text-accent rounded px-2 py-0.5">
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 2 9 9l-7 1 5 5-1 7 6-4 6 4-1-7 5-5-7-1z" />
              </svg>
              AI-revised by {doc.revisionMeta.generatedBy} —{" "}
              applied {doc.revisionMeta.appliedCommentIds.length} comment
              {doc.revisionMeta.appliedCommentIds.length === 1 ? "" : "s"}
            </div>
          )}
          <div className="text-xs text-muted mb-6">
            {doc.origin === "url" && doc.sourceUrl && (
              <>
                Cloned from{" "}
                <a
                  href={doc.sourceUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="text-accent hover:underline"
                >
                  {doc.sourceUrl}
                </a>
                {" · "}
              </>
            )}
            updated {formatRelative(doc.updatedAt)}
            {!me && !user && (
              <span className="ml-2 text-amber-600">
                · Set your name in the header to comment
              </span>
            )}
          </div>

          {doc.children && doc.children.length > 0 && (
            <div className="text-xs text-muted mb-6 flex items-center gap-2 flex-wrap">
              Revisions:
              {doc.children.map((c, i) => (
                <span key={c.id} className="inline-flex items-center gap-1">
                  <Link
                    to={`/d/${c.id}`}
                    className="text-accent hover:underline"
                  >
                    v{i + 2}
                  </Link>
                  {c.revisionMeta?.generatedBy && (
                    <span className="text-faint">by {c.revisionMeta.generatedBy}</span>
                  )}
                </span>
              ))}
            </div>
          )}

          {/* Rendered markdown */}
          <MarkdownRender
            ref={contentRef}
            content={doc.content}
            baseUrl={baseURLForDoc(doc.sourceUrl)}
          />
        </div>
      </div>

      {/* Comment sidebar */}
      <aside
        ref={sidebarRef}
        className="w-96 shrink-0 border-l border-rule bg-card overflow-y-auto"
      >
        <div className="sticky top-0 z-10 bg-card border-b border-rule">
          <div className="px-4 py-3 flex items-center gap-2">
            <Link to="/" className="text-sm text-muted hover:text-accent">
              ← All docs
            </Link>
            <div className="ml-auto flex items-center gap-1 text-xs">
              <FilterButton
                active={filter === "open"}
                onClick={() => setFilter("open")}
              >
                Open <Count n={openCount} />
              </FilterButton>
              <FilterButton
                active={filter === "resolved"}
                onClick={() => setFilter("resolved")}
              >
                Done <Count n={resolvedCount} />
              </FilterButton>
              <FilterButton
                active={filter === "all"}
                onClick={() => setFilter("all")}
              >
                All <Count n={comments.length} />
              </FilterButton>
            </div>
          </div>
          {visibleComments.length > 0 && (
            <div className="px-4 pb-2 -mt-1 flex items-center gap-2 text-xs">
              <button
                onClick={() => stepComment(-1)}
                title="Previous comment (k or ↑)"
                className="flex items-center gap-1 px-2 py-1 rounded text-muted hover:text-ink hover:bg-soft"
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="15 18 9 12 15 6" />
                </svg>
                Prev
              </button>
              <button
                onClick={() => stepComment(1)}
                title="Next comment (j or ↓)"
                className="flex items-center gap-1 px-2 py-1 rounded text-muted hover:text-ink hover:bg-soft"
              >
                Next
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="9 18 15 12 9 6" />
                </svg>
              </button>
              <div className="ml-auto text-faint tabular-nums">
                {activeIndex >= 0 ? activeIndex + 1 : "—"} of {visibleComments.length}
              </div>
            </div>
          )}
        </div>

        <div className="p-3 space-y-2">
          {visibleComments.length === 0 ? (
            <div className="text-sm text-muted text-center py-8 px-4">
              {filter === "open"
                ? "No open comments. Select any text in the doc to add one."
                : filter === "resolved"
                  ? "No resolved comments yet."
                  : "Nothing here yet."}
            </div>
          ) : (
            visibleComments.map((c) => (
              <CommentCard
                key={c.id}
                comment={c}
                active={activeId === c.id}
                me={me}
                requireIdentity={withIdentity}
                onActivate={() => setActiveId(c.id)}
                onResolve={() =>
                  new Promise<void>((resolve) =>
                    withIdentity(() => handleResolve(c).then(resolve))
                  )
                }
                onReopen={() => handleReopen(c)}
                onReply={(body) => handleReply(c, body)}
                onEdit={(body) => handleEdit(c, body)}
                onDelete={() => handleDelete(c)}
                onEditReply={(rid, body) => handleEditReply(c, rid, body)}
                onDeleteReply={(rid) => handleDeleteReply(c, rid)}
              />
            ))
          )}

          {composer && (
            <div className="mt-2">
              <NewCommentComposer
                quotedText={composer.anchor.exact}
                onSubmit={submitNewComment}
                onCancel={() => setComposer(null)}
              />
            </div>
          )}
        </div>
      </aside>

      {/* Floating selection popover */}
      {selection && !composer && (
        <SelectionPopover
          x={selection.popX}
          y={selection.popY}
          onComment={openComposerForSelection}
        />
      )}

      {showSignIn && (
        <SignInModal
          onClose={() => {
            setShowSignIn(false);
            setPendingAction(null);
            setReviseSignInExplain(false);
          }}
          onContinue={() => {
            setShowSignIn(false);
            const fn = pendingAction;
            setPendingAction(null);
            setReviseSignInExplain(false);
            fn?.();
          }}
          subtitle={
            reviseSignInExplain
              ? "AI revision uses Claude on your behalf. Sign in with GitHub so we can attribute revisions to you — and let private docs verify access."
              : undefined
          }
        />
      )}

      {showAPIKey && (
        <APIKeyModal
          onClose={() => setShowAPIKey(false)}
          onSaved={(status) => {
            if (status.hasKey) {
              setShowAPIKey(false);
              setShowRevise(true);
            }
          }}
        />
      )}

      {showRevise && (
        <ReviseModal
          doc={doc}
          resolvedCount={resolvedCount}
          onClose={() => setShowRevise(false)}
          onAccepted={(newDoc) => {
            setShowRevise(false);
            navigate(`/d/${newDoc.id}`);
          }}
        />
      )}

      {/* Hidden refresh hook for explicit reloads (not strictly needed but useful) */}
      <button onClick={refreshComments} className="hidden" />
    </div>
  );
}

function FilterButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={[
        "px-2 py-1 rounded font-medium",
        active
          ? "bg-accent text-accent-fg"
          : "text-muted hover:bg-soft",
      ].join(" ")}
    >
      {children}
    </button>
  );
}

function Count({ n }: { n: number }) {
  return (
    <span className="ml-1 opacity-70 text-[10px] tabular-nums">{n}</span>
  );
}
