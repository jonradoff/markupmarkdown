import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
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
import DocumentToolbar from "../components/DocumentToolbar";
import { FilterButton, Count } from "../components/CommentFilterButtons";
import { getAuthor } from "../utils/author";
import { useAuth } from "../auth";
import SignInModal from "../components/SignInModal";
import APIKeyModal from "../components/APIKeyModal";
import ReviseModal from "../components/ReviseModal";
import ShareModal from "../components/ShareModal";
import { useDialog } from "../components/Dialogs";
import { useToast, toastMessageFor } from "../components/Toast";
import { useSessionReadIds } from "../utils/sessionReadIds";
import { relaxAnchors } from "../utils/anchoredLayout";
import { downloadAsMarkdown } from "../utils/download";

type Filter = "open" | "unread" | "resolved" | "all";

export default function DocumentPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { user } = useAuth();
  const dialog = useDialog();
  const toast = useToast();
  const [searchParams, setSearchParams] = useSearchParams();
  const [showSignIn, setShowSignIn] = useState(false);
  const [pendingAction, setPendingAction] = useState<(() => void) | null>(null);
  const [showAPIKey, setShowAPIKey] = useState(false);
  const [showRevise, setShowRevise] = useState(false);
  const [showShare, setShowShare] = useState(false);
  const [reviseSignInExplain, setReviseSignInExplain] = useState(false);

  const [doc, setDoc] = useState<MdDocument | null>(null);
  const [comments, setComments] = useState<Comment[]>([]);
  const [error, setError] = useState<APIError | null>(null);
  const [activeId, setActiveIdRaw] = useState<string | null>(null);
  // Comments the viewer has activated, persisted in sessionStorage so
  // navigating away and back doesn't make recently-read comments show up
  // as unread again.
  const { ids: sessionReadIds, markRead: markSessionRead } =
    useSessionReadIds(id);
  const setActiveId = useCallback(
    (commentId: string | null) => {
      setActiveIdRaw(commentId);
      if (commentId) markSessionRead(commentId);
    },
    [markSessionRead]
  );
  const [filter, setFilter] = useState<Filter>("open");

  const [selection, setSelection] = useState<{
    anchor: AnchorSpec;
    popX: number;
    popY: number;
  } | null>(null);
  const [composer, setComposer] = useState<{ anchor: AnchorSpec; y: number } | null>(null);

  const contentRef = useRef<HTMLDivElement>(null);
  const sidebarRef = useRef<HTMLDivElement>(null);
  const topHeaderRef = useRef<HTMLDivElement>(null);
  const navBarRef = useRef<HTMLDivElement>(null);
  // Refs to each rendered CommentCard wrapper, so we can measure their
  // heights for the anchored-layout solver.
  const cardRefs = useRef<Record<string, HTMLDivElement | null>>({});
  // Map of commentId → top in px, applied as style.top on the wrapper.
  // Recomputed whenever highlights move or comments change.
  const [cardTops, setCardTops] = useState<Record<string, number>>({});
  // Monotonic counter incremented when something not captured by the
  // layout-effect deps (e.g. window resize) should force a re-measure.
  const [layoutTick, setLayoutTick] = useState(0);
  // Measured heights of the two sticky bars at the top of the sidebar,
  // used to (a) offset the Prev/Next bar from the header and (b) push
  // the first anchored card below them so it never starts hidden.
  const [topHeaderH, setTopHeaderH] = useState(0);
  const [navBarH, setNavBarH] = useState(0);

  // Deep-link to a specific comment via ?comment=ID (notifications use this).
  useEffect(() => {
    if (!doc) return;
    const target = searchParams.get("comment");
    if (!target) return;
    if (comments.some((c) => c.id === target)) {
      // Show all so the activate-and-scroll works for resolved threads too.
      setFilter("all");
      setActiveId(target);
      // Clean the query string so a refresh doesn't keep re-activating.
      const next = new URLSearchParams(searchParams);
      next.delete("comment");
      setSearchParams(next, { replace: true });
    }
  }, [doc, comments, searchParams, setSearchParams]);

  // Keep the browser tab title in sync with the doc. Reset on unmount.
  useEffect(() => {
    if (!doc) return;
    const prev = document.title;
    document.title = `${doc.title} · markupmarkdown`;
    return () => {
      document.title = prev;
    };
  }, [doc]);

  // Load doc + comments. If the doc is private and the user can't access it,
  // surface the structured error (sign-in or "no access") and render nothing.
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    // Reset realtime bookkeeping for the new doc.
    refreshSeqRef.current = 0;
    lastAppliedSeqRef.current = 0;
    setError(null);
    setDoc(null);
    setComments([]);
    (async () => {
      try {
        const d = await api.getDocument(id);
        if (cancelled) return;
        const cs = await api.listComments(id);
        if (cancelled) return;
        lastAppliedSeqRef.current = 1;
        refreshSeqRef.current = 1;
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
      .filter((c) => {
        switch (filter) {
          case "all": return true;
          case "resolved": return c.resolved;
          case "unread": return isUnread(c);
          default: return !c.resolved;
        }
      })
      .map((c) => ({
        id: c.id,
        start: c.anchor.start,
        end: c.anchor.end,
        exact: c.anchor.exact,
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

  // Anchor comment cards vertically to their highlighted spans. We
  // measure each highlight's top relative to the sidebar's scroll
  // container, then run a relaxation pass so overlapping anchors push
  // each other down with a constant gap. The cards live in a
  // position: relative container; we set style.top per card.
  useLayoutEffect(() => {
    const content = contentRef.current;
    const sidebar = sidebarRef.current;
    if (!content || !sidebar) return;
    // sidebar.scrollTop accounts for the user's scroll within the
    // sidebar; content.getBoundingClientRect().top minus
    // sidebar.getBoundingClientRect().top normalizes to sidebar-local
    // coordinates.
    const sbBox = sidebar.getBoundingClientRect();
    const items = visibleComments
      .map((c) => {
        const rect = getHighlightRect(content, c.id);
        if (!rect) return null;
        const wrapper = cardRefs.current[c.id];
        const height = wrapper?.offsetHeight ?? 120;
        // Target the top of the highlight, in sidebar-local coords.
        return {
          id: c.id,
          desiredTop: rect.top - sbBox.top + sidebar.scrollTop,
          height,
        };
      })
      .filter((x): x is NonNullable<typeof x> => x !== null);

    if (items.length === 0) {
      setCardTops({});
      return;
    }
    // Enforce a minimum top so no card starts hidden behind the
    // floating Prev/Next bar. The bars are sticky to the SCROLL
    // viewport, but the anchored container's coordinate space is
    // sidebar-local — so we just add enough top padding for the bars
    // to clear the first card.
    const minTop = topHeaderH + navBarH + 8;
    const padded = items.map((it) =>
      it.desiredTop < minTop ? { ...it, desiredTop: minTop } : it
    );
    setCardTops(relaxAnchors(padded, 12));
  }, [doc, comments, activeId, filter, layoutTick, topHeaderH, navBarH]);

  // Trigger a re-measure when the window resizes (column widths change
  // → highlight Y positions change). Bumping layoutTick is the explicit
  // dependency the layout effect watches.
  useEffect(() => {
    const onResize = () => setLayoutTick((n) => n + 1);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  // Measure the two sticky bars at the top of the sidebar with a
  // ResizeObserver so we always know the current header/nav-bar
  // heights — used to (a) offset the Prev/Next bar from the header
  // and (b) push the first anchored card below them so it never
  // starts hidden.
  useEffect(() => {
    const measure = () => {
      const h1 = topHeaderRef.current?.getBoundingClientRect().height ?? 0;
      const h2 = navBarRef.current?.getBoundingClientRect().height ?? 0;
      setTopHeaderH(Math.round(h1));
      setNavBarH(Math.round(h2));
    };
    measure();
    const ro = new ResizeObserver(measure);
    if (topHeaderRef.current) ro.observe(topHeaderRef.current);
    if (navBarRef.current) ro.observe(navBarRef.current);
    return () => ro.disconnect();
  }, [doc]);

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

  // Monotonic counter bumped on every refetch request. Lets us discard a
  // stale list response that resolves AFTER a newer one (or after an
  // optimistic mutation has already updated state). Tracks request order;
  // values don't escape this component.
  const refreshSeqRef = useRef(0);
  const lastAppliedSeqRef = useRef(0);
  const refreshTimerRef = useRef<number | null>(null);

  const refreshComments = useCallback(async () => {
    if (!id) return;
    const seq = ++refreshSeqRef.current;
    try {
      const cs = await api.listComments(id);
      // Drop the response if a newer refresh has already landed or if the
      // doc/id changed mid-flight.
      if (seq < lastAppliedSeqRef.current) return;
      lastAppliedSeqRef.current = seq;
      setComments(cs);
    } catch {
      // Network blip or the doc was deleted out from under us. Don't toast
      // — SSE will fire `comments-updated` again on the next mutation and
      // recover. We only complain when a user-initiated action fails.
    }
  }, [id]);

  // Coalesce a burst of broadcasts into one fetch, so a flurry of agent
  // writes doesn't pummel the server. Trailing-edge debounce — first event
  // schedules the fetch; subsequent events extend the window slightly.
  const scheduleRefresh = useCallback(() => {
    if (refreshTimerRef.current != null) {
      window.clearTimeout(refreshTimerRef.current);
    }
    refreshTimerRef.current = window.setTimeout(() => {
      refreshTimerRef.current = null;
      refreshComments();
    }, 150);
  }, [refreshComments]);

  // Realtime: subscribe to server-sent events for this doc and refetch
  // the comments list on any change. EventSource auto-reconnects on drop;
  // we also force a refresh on every connect so we catch up on anything
  // missed during the gap.
  useEffect(() => {
    if (!id) return;
    const es = new EventSource(`/api/documents/${id}/events`, {
      withCredentials: true,
    });
    const onUpdate = () => scheduleRefresh();
    const onOpen = () => {
      // Fires on initial connect AND on auto-reconnect after a drop.
      // Refetching here recovers any broadcasts we missed during the gap.
      scheduleRefresh();
    };
    const onHello = () => scheduleRefresh(); // belt-and-suspenders
    es.addEventListener("comments-updated", onUpdate);
    es.addEventListener("hello", onHello);
    es.onopen = onOpen;
    return () => {
      es.removeEventListener("comments-updated", onUpdate);
      es.removeEventListener("hello", onHello);
      es.close();
      if (refreshTimerRef.current != null) {
        window.clearTimeout(refreshTimerRef.current);
        refreshTimerRef.current = null;
      }
    };
  }, [id, scheduleRefresh]);

  function withIdentity(fn: () => void) {
    if (user || getAuthor()) {
      fn();
      return;
    }
    setPendingAction(() => fn);
    setShowSignIn(true);
  }

  // applyMutation runs an updater on the comments list AND advances the
  // refresh seq so any list fetch already in flight can't overwrite us
  // with a stale snapshot.
  const applyMutation = useCallback(
    (fn: (prev: Comment[]) => Comment[]) => {
      lastAppliedSeqRef.current = refreshSeqRef.current + 1;
      setComments(fn);
    },
    []
  );

  // Translate an APIError into a user-friendly toast message. Keeps the
  // top-of-page error UI reserved for "can't load the doc at all" cases.
  const toastError = useCallback(
    (err: unknown, fallback: string) => {
      if (err instanceof APIError) {
        toast.error(err.message || fallback);
      } else {
        toast.error(toastMessageFor(err) || fallback);
      }
    },
    [toast]
  );

  async function submitNewComment(body: string) {
    if (!id || !composer) return;
    const author = user?.name || user?.login || getAuthor() || "Anonymous";
    try {
      const c = await api.createComment(id, {
        anchor: composer.anchor,
        body,
        author,
      });
      setComposer(null);
      applyMutation((prev) => [...prev, c]);
      setActiveId(c.id);
      window.getSelection()?.removeAllRanges();
      setSelection(null);
    } catch (err) {
      toastError(err, "Couldn't post that comment.");
      // Keep the composer open with the user's text so they can retry.
      // Re-throw so NewCommentComposer's `busy` state clears via its
      // try/catch and the user can edit + retry.
      throw err;
    }
  }

  function openComposerForSelection() {
    if (!selection) return;
    const sel = selection;
    withIdentity(() => {
      // Translate the popover's window-space Y into sidebar-local
      // coordinates so the composer card lands beside the highlighted
      // span instead of at the top of the list.
      const sb = sidebarRef.current;
      let y = sel.popY + window.scrollY;
      if (sb) {
        const sbBox = sb.getBoundingClientRect();
        y = sel.popY - sbBox.top + sb.scrollTop;
      }
      // Don't let the composer start under the floating sticky bars.
      const minY = topHeaderH + navBarH + 8;
      if (y < minY) y = minY;
      setComposer({
        anchor: sel.anchor,
        y,
      });
      setSelection(null);
      // Scroll the sidebar so the composer is visible (with the
      // floating bars subtracted so it's not jammed against them).
      if (sb) {
        requestAnimationFrame(() => {
          sb.scrollTo({
            top: Math.max(0, y - topHeaderH - navBarH - 16),
            behavior: "smooth",
          });
        });
      }
    });
  }

  async function handleResolve(c: Comment) {
    const author = user?.name || user?.login || getAuthor() || "Anonymous";
    try {
      const updated = await api.resolveComment(c.id, author);
      applyMutation((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
    } catch (err) {
      toastError(err, "Couldn't mark that comment done.");
    }
  }
  async function handleReopen(c: Comment) {
    try {
      const updated = await api.reopenComment(c.id);
      applyMutation((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
    } catch (err) {
      toastError(err, "Couldn't reopen that comment.");
    }
  }
  async function handleReply(c: Comment, body: string) {
    const author = user?.name || user?.login || getAuthor() || "Anonymous";
    try {
      const updated = await api.createReply(c.id, body, author);
      applyMutation((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
    } catch (err) {
      toastError(err, "Couldn't post that reply.");
      throw err; // bubble so CommentCard keeps the draft + clears `busy`
    }
  }
  async function handleEdit(c: Comment, body: string) {
    try {
      const updated = await api.editComment(c.id, body);
      applyMutation((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
    } catch (err) {
      toastError(err, "Couldn't save that edit.");
      throw err;
    }
  }
  async function handleDelete(c: Comment) {
    try {
      await api.deleteComment(c.id);
      applyMutation((prev) => prev.filter((x) => x.id !== c.id));
      if (activeId === c.id) setActiveId(null);
    } catch (err) {
      toastError(err, "Couldn't delete that comment.");
    }
  }
  async function handleEditReply(c: Comment, replyId: string, body: string) {
    try {
      const updated = await api.editReply(c.id, replyId, body);
      applyMutation((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
    } catch (err) {
      toastError(err, "Couldn't save that edit.");
      throw err;
    }
  }
  async function handleDeleteReply(c: Comment, replyId: string) {
    try {
      const updated = await api.deleteReply(c.id, replyId);
      applyMutation((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
    } catch (err) {
      toastError(err, "Couldn't delete that reply.");
    }
  }

  // A comment counts as "unread" when it's newer than the user's previous
  // open of this doc. Anchored on previouslyViewedAt from getDocument —
  // first-ever visit returns no prior, so nothing is unread.
  const isUnread = useCallback(
    (c: Comment) => {
      if (!doc?.previouslyViewedAt) return false;
      if (sessionReadIds.has(c.id)) return false;
      const prev = Date.parse(doc.previouslyViewedAt);
      // Latest activity on a thread = max(comment.updatedAt, last reply).
      let latest = Date.parse(c.updatedAt);
      for (const r of c.replies) {
        const t = Date.parse(r.updatedAt);
        if (t > latest) latest = t;
      }
      return latest > prev;
    },
    [doc?.previouslyViewedAt, sessionReadIds]
  );

  const visibleComments = useMemo(() => {
    return comments
      .filter((c) => {
        switch (filter) {
          case "all": return true;
          case "resolved": return c.resolved;
          case "unread": return isUnread(c);
          default: return !c.resolved; // "open"
        }
      })
      .sort((a, b) => a.anchor.start - b.anchor.start);
  }, [comments, filter, isUnread]);

  const openCount = comments.filter((c) => !c.resolved).length;
  const resolvedCount = comments.length - openCount;
  const unreadCount = comments.filter(isUnread).length;

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
      try {
        const updated = await api.renameDocument(doc.id, next.trim());
        setDoc(updated);
      } catch (err) {
        toastError(err, "Couldn't rename the document.");
      }
    }
  }

  async function deleteDoc() {
    if (!doc) return;
    const ok = await dialog.confirm({
      title: "Delete document?",
      body: `Delete "${doc.title}" and all its comments? You can restore it from Trash for 30 days.`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteDocument(doc.id);
      navigate("/");
    } catch (err) {
      toastError(err, "Couldn't delete the document.");
    }
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
          <DocumentToolbar
            doc={doc}
            me={me}
            signedIn={!!user}
            onRename={renameDoc}
            onRevise={handleReviseClick}
            onShare={() => setShowShare(true)}
            onDownload={handleDownload}
            onDelete={deleteDoc}
          />

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
        {/* Top header: All docs link + filter pills. Sticks at the top
            of the sidebar so it's always reachable. */}
        <div
          ref={topHeaderRef}
          className="sticky top-0 z-20 bg-card border-b border-rule"
        >
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
                active={filter === "unread"}
                highlight={unreadCount > 0}
                onClick={() => setFilter("unread")}
              >
                Unread <Count n={unreadCount} pulse={unreadCount > 0 && filter !== "unread"} />
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
        </div>

        {/* Prev/Next: its own sticky bar that floats above the
            anchored comment cards. Stays visible no matter how far
            down the user has scrolled. Higher z-index than the cards
            (which are absolutely positioned with default z=0) so it
            never gets clipped. */}
        {visibleComments.length > 0 && (
          <div
            ref={navBarRef}
            className="sticky z-30 bg-card/95 backdrop-blur-sm border-b border-rule shadow-sm"
            style={{ top: topHeaderH }}
          >
            <div className="px-4 py-2 flex items-center gap-2 text-xs">
              <button
                onClick={() => stepComment(-1)}
                title="Previous comment (k or ↑)"
                className="flex items-center gap-1 px-2 py-1 rounded text-muted hover:text-ink hover:bg-soft font-medium"
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="15 18 9 12 15 6" />
                </svg>
                Prev
              </button>
              <button
                onClick={() => stepComment(1)}
                title="Next comment (j or ↓)"
                className="flex items-center gap-1 px-2 py-1 rounded text-muted hover:text-ink hover:bg-soft font-medium"
              >
                Next
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="9 18 15 12 9 6" />
                </svg>
              </button>
              <div className="ml-auto text-faint tabular-nums">
                {activeIndex >= 0 ? activeIndex + 1 : "—"} of {visibleComments.length}
              </div>
            </div>
          </div>
        )}

        {visibleComments.length === 0 ? (
          <div className="relative p-3 min-h-[200px]">
            {!composer && (
              <div className="text-sm text-muted text-center py-8 px-4">
                {filter === "open"
                  ? "No open comments. Select any text in the doc to add one."
                  : filter === "resolved"
                    ? "No resolved comments yet."
                    : "Nothing here yet."}
              </div>
            )}
            {composer && (
              <div
                className="absolute left-3 right-3"
                style={{ top: composer.y }}
              >
                <NewCommentComposer
                  documentId={doc.id}
                  quotedText={composer.anchor.exact}
                  onSubmit={submitNewComment}
                  onCancel={() => setComposer(null)}
                />
              </div>
            )}
          </div>
        ) : (
          // Anchored layout: cards are absolutely positioned so they
          // align vertically with their highlight in the doc. We give
          // the container an explicit min-height covering the lowest
          // card so it scrolls naturally.
          <div
            className="relative px-3 pb-6"
            style={{
              minHeight: (() => {
                let h = 0;
                for (const c of visibleComments) {
                  const top = cardTops[c.id] ?? 0;
                  const wrap = cardRefs.current[c.id];
                  const bot = top + (wrap?.offsetHeight ?? 120);
                  if (bot > h) h = bot;
                }
                return h + 24;
              })(),
            }}
          >
            {visibleComments.map((c) => {
              const top = cardTops[c.id];
              return (
                <div
                  key={c.id}
                  ref={(el) => {
                    cardRefs.current[c.id] = el;
                  }}
                  className="absolute left-3 right-3 transition-[top] duration-150"
                  style={{
                    top: top ?? 0,
                    visibility: top == null ? "hidden" : "visible",
                  }}
                >
                  <CommentCard
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
                </div>
              );
            })}
            {composer && (
              <div
                className="absolute left-3 right-3"
                style={{ top: composer.y }}
              >
                <NewCommentComposer
                  documentId={doc.id}
                  quotedText={composer.anchor.exact}
                  onSubmit={submitNewComment}
                  onCancel={() => setComposer(null)}
                />
              </div>
            )}
          </div>
        )}
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
          resolvedComments={comments.filter((c) => c.resolved)}
          onClose={() => setShowRevise(false)}
          onAccepted={(newDoc) => {
            setShowRevise(false);
            navigate(`/d/${newDoc.id}`);
          }}
        />
      )}

      {showShare && (
        <ShareModal doc={doc} onClose={() => setShowShare(false)} />
      )}

      {/* Hidden refresh hook for explicit reloads (not strictly needed but useful) */}
      <button onClick={refreshComments} className="hidden" />
    </div>
  );
}

