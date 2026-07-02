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
import { canonicalDocPath, rewriteToCanonical } from "../utils/canonicalUrl";
import SelectionPopover from "../components/SelectionPopover";
import NewCommentComposer from "../components/NewCommentComposer";
import CommentCard from "../components/CommentCard";
import DocumentToolbar from "../components/DocumentToolbar";
import { FilterButton, Count } from "../components/CommentFilterButtons";
import CommentStepNav from "../components/CommentStepNav";
import SourceDriftBanner from "../components/SourceDriftBanner";
import ReviewBar from "../components/ReviewBar";
import MergeModal from "../components/MergeModal";
import EditorPane, { type EditorPaneHandle } from "../components/EditorPane";
import PushbackModal from "../components/PushbackModal";
import OrphanCommentCard from "../components/OrphanCommentCard";
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
      if (commentId) {
        markSessionRead(commentId);
        // Mark any pending notifications for this comment as read so
        // the bell badge decrements whether the viewer arrived here
        // via the bell or by scrolling. Fire-and-forget; the bell
        // refreshes on the "mm:notifications-updated" window event.
        void api
          .markNotificationsForComment(commentId)
          .then(({ updated }) => {
            if (updated > 0) {
              window.dispatchEvent(new CustomEvent("mm:notifications-updated"));
            }
          })
          .catch(() => {
            // Network blip; bell's 45s poll will reconcile.
          });
      }
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
  // Re-anchor mode: when the user clicks "Re-anchor to new text" on an
  // orphan card, we enter selection-capture mode. The next text
  // selection inside the content area + click on the popover commits
  // the new anchor against this comment instead of opening a new
  // comment composer.
  const [reanchorTarget, setReanchorTarget] = useState<Comment | null>(null);
  // True when a doc-level comment composer is open at the top of the
  // sidebar. Doc-level comments have an empty anchor (no inline
  // highlight) and are listed in their own section.
  const [docLevelOpen, setDocLevelOpen] = useState(false);
  // showMerge holds the modal state for the 3-way merge flow. Opened
  // when the user clicks the drift banner's merge button.
  const [showMerge, setShowMerge] = useState(false);
  // editing toggles the doc page into Markdown-editor mode. Saving
  // creates a new revision in the chain (manual edit) and navigates
  // to it; the editor pane handles the textarea + live preview.
  const [editing, setEditing] = useState(false);
  const [editSaving, setEditSaving] = useState(false);
  const [showPushback, setShowPushback] = useState(false);
  // editLock holds the current soft-lock state for the doc. When set
  // and !mine, the toolbar's Edit button is hidden and a banner says
  // who's editing. Updated by the SSE "edit-lock-changed" event so
  // every open tab agrees within a round-trip.
  const [editLock, setEditLock] = useState<{
    locked: boolean;
    mine?: boolean;
    holder?: string;
  }>({ locked: false });

  const contentRef = useRef<HTMLDivElement>(null);
  const sidebarRef = useRef<HTMLDivElement>(null);
  // The .relative container that holds the absolutely-positioned
  // comment cards. Used as the coordinate-space origin for cardTops —
  // computing `desiredTop = highlightViewportTop - containerTop` is
  // robust to every layout above it (sticky headers, doc-level
  // section, padding, sidebar scroll) because the container's
  // bounding rect already reflects all of that.
  const anchorsContainerRef = useRef<HTMLDivElement>(null);
  // Imperative handle to the CodeMirror editor when editing. Lets the
  // anchored card layout pull editor-relative anchor coordinates.
  const editorRef = useRef<EditorPaneHandle>(null);
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
        // If the doc is github-anchored, replace /d/:id in the
        // address bar with /:owner/:repo/blob/:ref/path so the URL
        // reads as the human pasted it (or would copy-paste it).
        // /d/:id keeps working as a permalink — we just trade the
        // address bar's display for the human shape on mount.
        const canonical = canonicalDocPath(d);
        if (canonical) rewriteToCanonical(canonical);
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

  // Older revisions no longer trigger a "newer version exists" popup.
  // The doc list dedupes to leaves, so anyone landing on an older
  // revision did so deliberately (via toolbar breadcrumb, history,
  // or a deep link). The toolbar's "Latest revision: v3 →" link gives
  // them a one-click path forward without yanking the page.

  // Apply highlights after every render of doc content / comments / active
  useLayoutEffect(() => {
    const el = contentRef.current;
    if (!el || !doc) return;
    const ranges = comments
      .filter((c) => {
        // Orphan and doc-level comments don't render inline highlights —
        // they live in their own sections (below the doc and at the top
        // of the sidebar, respectively).
        if (c.orphan) return false;
        if (!c.anchor.exact) return false;
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
    const sidebar = sidebarRef.current;
    if (!sidebar) return;
    const content = contentRef.current;
    // In edit mode the rendered Markdown is gone — pull anchor
    // positions from CodeMirror via the imperative ref so cards still
    // line up with the source span of each comment. Outside edit mode
    // fall back to the rendered-DOM highlight rect.
    const usingEditor = editing && editorRef.current;
    if (!usingEditor && !content) return;
    const sbBox = sidebar.getBoundingClientRect();
    const container = anchorsContainerRef.current;
    if (!container) return;
    // We anchor cards in VIEWPORT coordinates. The container itself is
    // inside the sidebar's scroll — but the sidebar is sticky
    // (top-0 h-screen) so its outer box is stable in the viewport and
    // doesn't depend on sidebar.scrollTop. Reading containerRect.top
    // here used to feed sidebar.scrollTop into the formula, which made
    // every Next click amplify the previous scroll: cards marched off
    // into space. By computing card.style.top off sbBox.top (and an
    // offset for header/nav so the math composes with sidebar.scrollTop
    // when there's no scroll yet) the layout stays stable.
    //
    // desiredTop is in container-local coordinates. The container sits
    // inside sidebar.scrollTop; once the layout is committed we DON'T
    // re-scroll the sidebar in response, so containerRect.top stays
    // equal to sbBox.top + (offset of container within sidebar's
    // unscrolled flow). That offset is approximately
    // topHeaderH + navBarH + (doc-level / orphan section heights), and
    // it's stable across Next clicks because those sections don't
    // change height when activeId moves.
    const containerRect = container.getBoundingClientRect();
    const vh = window.innerHeight;
    // Only lay out cards whose anchor is near the current viewport.
    // Cards whose anchors are far above/below the viewport would
    // either pile up at the top edge (pushing the active card down)
    // or render off-screen below — neither is useful, and they make
    // relaxAnchors fight with the active card. The active card is
    // always included so the user has something to look at even when
    // its anchor has scrolled just out of view between clicks.
    const items = visibleComments
      .map((c) => {
        let top: number | null = null;
        if (usingEditor) {
          const exact = c.anchor.exact || c.originalExact || "";
          if (!exact) return null;
          const r = editorRef.current!.coordsForAnchor(exact);
          if (!r) return null;
          top = r.top;
        } else if (content) {
          const rect = getHighlightRect(content, c.id);
          if (!rect) return null;
          top = rect.top;
        }
        if (top == null) return null;
        const inRange = top >= -200 && top <= vh + 200;
        const isActive = c.id === activeId;
        if (!inRange && !isActive) return null;
        const wrapper = cardRefs.current[c.id];
        const height = wrapper?.offsetHeight ?? 120;
        return {
          id: c.id,
          desiredTop: top - containerRect.top,
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
    // Cards must stay below the sticky header + nav bar visually.
    // The stickies' bottom edge in viewport coords is
    //   sbBox.top + topHeaderH + navBarH
    // Translating to container-relative space (so it composes with
    // desiredTop above):
    //   minTop = (stickyBottomViewport - containerRect.top) + buffer
    const stickyBottomY = sbBox.top + topHeaderH + navBarH;
    const minTop = Math.max(0, stickyBottomY - containerRect.top + 8);
    const padded = items.map((it) =>
      it.desiredTop < minTop ? { ...it, desiredTop: minTop } : it
    );
    // Sort items by their editor-anchor position before relaxing.
    // visibleComments is sorted by anchor.start, but MCP-added agent
    // comments all store anchor.start = 0 — they're resolved by quoted
    // text at render time. Without this sort, relaxAnchors would stack
    // them in creation order rather than document order, pushing a card
    // whose anchor is near the top of the doc all the way to the bottom
    // of the sidebar's flow.
    padded.sort((a, b) => a.desiredTop - b.desiredTop);
    setCardTops(relaxAnchors(padded, 12));
  }, [doc, comments, activeId, filter, layoutTick, topHeaderH, navBarH, editing]);

  // Trigger a re-measure when the window resizes OR the page scrolls
  // (the document column scrolls the body now — highlight Y positions
  // change relative to the sticky sidebar each scroll tick). Bumping
  // layoutTick is the explicit dependency the layout effect watches.
  // rAF-throttled so we don't queue a state update per scroll event.
  useEffect(() => {
    let queued = false;
    const tick = () => {
      queued = false;
      setLayoutTick((n) => n + 1);
    };
    const schedule = () => {
      if (queued) return;
      queued = true;
      requestAnimationFrame(tick);
    };
    window.addEventListener("resize", schedule);
    window.addEventListener("scroll", schedule, { passive: true });
    return () => {
      window.removeEventListener("resize", schedule);
      window.removeEventListener("scroll", schedule);
    };
  }, []);

  // Source-drift + access re-verification check. Fires:
  //   • immediately on mount (covers the case Jon raised — open the doc
  //     after a quick GitHub edit, see the banner without a manual reload)
  //   • on window focus / visibilitychange (covers "I tabbed back")
  //   • periodically every 2 min while the tab is visible (covers
  //     "I keep this open all afternoon")
  //
  // The server returns the freshly-computed drift fields synchronously
  // (it also busts the GitHub access caches) so we apply them straight
  // to local state. A 401 / 403 means the user has lost access to the
  // source repo — surface the same access-denied page as a cold load.
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    async function checkSource() {
      if (!id || cancelled) return;
      try {
        const res = await api.checkDocumentSource(id);
        if (cancelled) return;
        setDoc((prev) =>
          prev
            ? {
                ...prev,
                sourceSha: res.sourceSha ?? prev.sourceSha,
                sourceLatestSha: res.sourceLatestSha ?? "",
                sourceDriftedAt: res.sourceDriftedAt ?? undefined,
                sourceDriftIgnoredSha: res.sourceDriftIgnoredSha ?? "",
                rootDocument: res.rootDocument ?? prev.rootDocument,
              }
            : prev
        );
      } catch (err) {
        if (err instanceof APIError) {
          // Lost GitHub access mid-session → render the access-denied
          // page instead of leaving the user on a stale doc.
          if (err.kind === "no_github_access" || err.kind === "sign_in_required") {
            setError(err);
          }
        }
        // Other errors (e.g. network blip, not-github doc) are silent.
      }
    }
    // Immediate check on mount.
    checkSource();
    const onFocus = () => {
      if (document.visibilityState === "visible") checkSource();
    };
    window.addEventListener("focus", onFocus);
    document.addEventListener("visibilitychange", onFocus);
    // Background poll: only ticks while the tab is visible. Two
    // minutes is the upstream-notice-vs-quota sweet spot — frequent
    // enough that a teammate's commit lands within a couple minutes,
    // sparse enough that ten idle tabs don't burn through the
    // anonymous GitHub rate budget (60/hr/IP).
    const pollId = window.setInterval(() => {
      if (document.visibilityState === "visible") checkSource();
    }, 2 * 60 * 1000);
    return () => {
      cancelled = true;
      window.removeEventListener("focus", onFocus);
      document.removeEventListener("visibilitychange", onFocus);
      window.clearInterval(pollId);
    };
  }, [id]);

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
    // doc-updated fires when the source-drift check flips state or
    // after a sync. Re-fetch the doc itself so the banner appears /
    // disappears without a page reload.
    const onDocUpdate = async () => {
      try {
        const d = await api.getDocument(id);
        setDoc(d);
      } catch {
        // SSE will retry; the next pageview also re-fetches.
      }
    };
    const onOpen = () => {
      // Fires on initial connect AND on auto-reconnect after a drop.
      // Refetching here recovers any broadcasts we missed during the gap.
      scheduleRefresh();
    };
    const onHello = () => scheduleRefresh(); // belt-and-suspenders
    // Refresh the edit-lock state when someone claims or releases
    // the lock on this doc. Cheap GET; no need for a separate stream.
    const onEditLockChanged = async () => {
      try {
        const lk = await api.getEditLock(id);
        setEditLock(lk);
      } catch {
        /* network blip — next event reconciles */
      }
    };
    es.addEventListener("comments-updated", onUpdate);
    es.addEventListener("doc-updated", onDocUpdate);
    es.addEventListener("reviews-updated", onDocUpdate);
    es.addEventListener("edit-lock-changed", onEditLockChanged);
    es.addEventListener("hello", onHello);
    es.onopen = onOpen;
    return () => {
      es.removeEventListener("comments-updated", onUpdate);
      es.removeEventListener("doc-updated", onDocUpdate);
      es.removeEventListener("reviews-updated", onDocUpdate);
      es.removeEventListener("edit-lock-changed", onEditLockChanged);
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
      // Mark the new thread as read in this session so the unread filter
      // doesn't immediately surface it. Deliberately NOT setting it as
      // the active comment: doing so triggers a window-scroll-to-
      // highlight and a sidebar scrollIntoView, both of which yank the
      // user off the text they just commented on. The fresh highlight
      // colour is sufficient visual confirmation.
      markSessionRead(c.id);
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

  // Initial edit-lock fetch — gets the state on mount so other-user
  // editing is visible immediately, before any SSE event fires.
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    (async () => {
      try {
        const lk = await api.getEditLock(id);
        if (!cancelled) setEditLock(lk);
      } catch {
        /* no GitHub-flavoured access, or 401 — leave default */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [id]);

  // While in edit mode: refresh the lock every 3.5 min so the
  // server-side 5-min TTL doesn't expire mid-edit. Release on unmount
  // or when leaving edit mode.
  useEffect(() => {
    if (!id || !editing) return;
    const heartbeat = window.setInterval(() => {
      api.claimEditLock(id).catch(() => {
        /* another viewer took over — toast on next SSE round */
      });
    }, 3.5 * 60 * 1000);
    return () => {
      window.clearInterval(heartbeat);
      // Best-effort release. If it fails the 5-min TTL takes over.
      api.releaseEditLock(id).catch(() => {});
    };
  }, [id, editing]);

  // Try to claim the lock when the user clicks Edit. If someone else
  // holds it (409), surface a toast with their name and stay in read
  // mode. Otherwise enter the editor.
  async function startEditing() {
    if (!id) return;
    try {
      await api.claimEditLock(id);
      setEditing(true);
    } catch (err) {
      if (err instanceof APIError && err.kind === "edit_lock_held") {
        toast.error(err.message);
        return;
      }
      toastError(err, "Couldn't start editing.");
    }
  }

  // Save a manual edit — creates a new revision in the chain. We
  // navigate to the new doc afterwards so the user is reading the
  // version they just wrote. The leaf-dedup in the home list means
  // the prior version doesn't clutter the recents.
  async function handleManualSave(content: string) {
    if (!doc || editSaving) return;
    setEditSaving(true);
    try {
      const next = await api.createManualRevision(doc.id, { content });
      setEditing(false);
      toast.success("Saved as a new revision.");
      navigate(`/d/${next.id}`);
    } catch (err) {
      toastError(err, "Couldn't save your edit.");
    } finally {
      setEditSaving(false);
    }
  }

  // Called by MergeModal after a successful merge accept. Refetch
  // doc + comments so the user sees the merged content + re-anchored
  // comments immediately. SSE will broadcast to any other open viewers.
  async function handleMerged() {
    if (!id) return;
    try {
      const next = await api.getDocument(id);
      setDoc(next);
      const cs = await api.listComments(id);
      applyMutation(() => cs);
    } catch (err) {
      toastError(err, "Couldn't refresh after the merge.");
    }
  }

  // Called by the SourceDriftBanner's "Ignore" button. Pops a
  // confirmation modal explaining what's about to happen, then calls
  // the backend to stamp this upstream SHA as ignored. Local doc
  // state is updated optimistically + with the server's authoritative
  // response so the banner disappears immediately.
  async function confirmIgnoreDrift() {
    if (!id || !doc?.sourceLatestSha) return;
    const ok = await dialog.confirm({
      title: "Ignore this upstream change?",
      body: (
        <div className="space-y-2 text-sm">
          <p>
            We'll stop showing the <em>"Source updated on GitHub"</em> banner
            for the current upstream commit. Your stored copy stays as it is
            — we just won't keep nudging you to merge it in.
          </p>
          <p>
            If a <em>newer</em> upstream commit shows up later, the banner
            comes back so you get a chance to act on the latest change.
          </p>
          <p className="text-muted">
            This affects how the doc is presented to every viewer.
          </p>
        </div>
      ),
      confirmLabel: "Ignore for now",
      cancelLabel: "Keep showing",
    });
    if (!ok) return;
    try {
      const res = await api.ignoreSourceDrift(id);
      setDoc((prev) =>
        prev
          ? {
              ...prev,
              sourceSha: res.sourceSha ?? prev.sourceSha,
              sourceLatestSha: res.sourceLatestSha ?? "",
              sourceDriftedAt: res.sourceDriftedAt ?? undefined,
              sourceDriftIgnoredSha:
                res.sourceDriftIgnoredSha ?? doc.sourceLatestSha,
            }
          : prev
      );
      toast.success("Drift banner hidden. We'll let you know if upstream moves again.");
    } catch (err) {
      toastError(err, "Couldn't ignore this drift.");
    }
  }

  // Enter manual re-anchor mode for an orphan comment.
  function startReanchor(c: Comment) {
    setReanchorTarget(c);
    setComposer(null);
    // Scroll the main column to the top so the user can browse the doc
    // and re-locate the right spot without obstruction.
    const main = contentRef.current?.parentElement?.parentElement;
    main?.scrollTo?.({ top: 0, behavior: "smooth" });
    toast.info("Select the new text to re-anchor this comment to.");
  }

  function cancelReanchor() {
    setReanchorTarget(null);
  }

  async function commitReanchor(anchor: AnchorSpec) {
    if (!reanchorTarget) return;
    try {
      const updated = await api.patchCommentAnchor(reanchorTarget.id, {
        start: anchor.start,
        end: anchor.end,
        exact: anchor.exact,
      });
      applyMutation((prev) => prev.map((x) => (x.id === updated.id ? updated : x)));
      setReanchorTarget(null);
      setSelection(null);
      window.getSelection()?.removeAllRanges();
      setActiveId(updated.id);
      toast.success("Re-anchored.");
    } catch (err) {
      toastError(err, "Couldn't re-anchor that comment.");
    }
  }

  async function makeDocLevel(c: Comment) {
    try {
      const updated = await api.patchCommentAnchor(c.id, { docLevel: true });
      applyMutation((prev) => prev.map((x) => (x.id === updated.id ? updated : x)));
    } catch (err) {
      toastError(err, "Couldn't convert to a document-level comment.");
    }
  }

  async function submitDocLevelComment(body: string) {
    if (!id) return;
    const author = user?.name || user?.login || getAuthor() || "Anonymous";
    try {
      const c = await api.createComment(id, {
        anchor: { start: 0, end: 0, exact: "" },
        body,
        author,
      });
      setDocLevelOpen(false);
      applyMutation((prev) => [...prev, c]);
      markSessionRead(c.id);
    } catch (err) {
      toastError(err, "Couldn't post that comment.");
      throw err;
    }
  }

  function openComposerForSelection() {
    if (!selection) return;
    const sel = selection;
    // When re-anchor mode is active, the selection becomes the new
    // anchor for the orphan comment instead of opening a fresh
    // composer.
    if (reanchorTarget) {
      commitReanchor(sel.anchor);
      return;
    }
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
    // Compute the next visible inline comment BEFORE the mutation so
    // the index doesn't shift if the active filter (e.g., "open")
    // would hide c after it's resolved. If there's nowhere to go
    // (single visible comment, or c isn't in the visible list at
    // all), advancing is a no-op.
    let nextId: string | null = null;
    if (visibleComments.length > 1) {
      const idx = visibleComments.findIndex((x) => x.id === c.id);
      if (idx >= 0) {
        const candidate = visibleComments[(idx + 1) % visibleComments.length];
        if (candidate && candidate.id !== c.id) nextId = candidate.id;
      }
    }
    try {
      const updated = await api.resolveComment(c.id, author);
      applyMutation((prev) => prev.map((x) => (x.id === c.id ? updated : x)));
      // Auto-advance to the next comment so a "Mark as Done" click
      // feels like progress through the queue (same gesture as
      // clicking Next).
      if (nextId) setActiveId(nextId);
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
  async function handleApplySuggestion(c: Comment) {
    if (!id || !c.suggestion) return;
    try {
      const newDoc = await api.applySuggestion(c.id);
      // The apply created a new doc — navigate to it so the reviewer
      // sees the change reflected in the source.
      navigate(`/d/${newDoc.id}`);
    } catch (err) {
      toastError(err, "Couldn't apply that suggestion.");
      throw err;
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

  // Partition comments by kind: inline (anchored to a highlight), doc-level
  // (Anchor.exact empty), and orphan (sync couldn't relocate the quote).
  // Each lives in its own section of the UI; only inline drive the
  // anchored sidebar layout.
  const inlineComments = useMemo(
    () =>
      comments.filter(
        (c) => !c.orphan && c.anchor.exact && c.anchor.exact.length > 0
      ),
    [comments]
  );
  const docLevelComments = useMemo(
    () => comments.filter((c) => !c.orphan && !c.anchor.exact),
    [comments]
  );
  const orphanComments = useMemo(
    () => comments.filter((c) => c.orphan),
    [comments]
  );

  const visibleComments = useMemo(() => {
    return inlineComments
      .filter((c) => {
        switch (filter) {
          case "all": return true;
          case "resolved": return c.resolved;
          case "unread": return isUnread(c);
          default: return !c.resolved; // "open"
        }
      })
      .sort((a, b) => a.anchor.start - b.anchor.start);
  }, [inlineComments, filter, isUnread]);

  const openCount = comments.filter((c) => !c.resolved).length;
  const resolvedCount = comments.filter((c) => c.resolved).length;
  const unreadCount = comments.filter(isUnread).length;
  // Drift is present when upstream's latest SHA differs from our
  // baseline AND the user hasn't explicitly dismissed *this* SHA. The
  // backend clears sourceDriftIgnoredSha as soon as upstream moves to
  // a newer SHA — so a fresh upstream commit always re-surfaces the
  // banner, even after a prior Ignore.
  const driftPresent = Boolean(
    doc?.sourceLatestSha &&
      doc.sourceLatestSha !== doc?.sourceSha &&
      doc.sourceLatestSha !== doc?.sourceDriftIgnoredSha
  );

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

  // Scroll markdown to bring highlight into view when activeId changes.
  // In edit mode the rendered DOM is gone — ask CodeMirror to scroll
  // its anchored line into the viewport instead. The page scroll IS
  // the editor's scroll now (cm-scroller overflow: visible) so this
  // moves the body just like the view-mode branch.
  useEffect(() => {
    if (!activeId) return;
    if (editing) {
      const c = comments.find((x) => x.id === activeId);
      const exact = c?.anchor?.exact || c?.originalExact || "";
      if (exact && editorRef.current) {
        editorRef.current.scrollAnchorIntoView(exact);
      }
      return;
    }
    if (!contentRef.current) return;
    const rect = getHighlightRect(contentRef.current, activeId);
    if (!rect) return;
    const margin = 100;
    if (rect.top < margin || rect.bottom > window.innerHeight - margin) {
      window.scrollTo({
        top: window.scrollY + rect.top - 150,
        behavior: "smooth",
      });
    }
  }, [activeId, editing, comments]);

  // The sidebar no longer scrolls internally for anchored cards (cards
  // live in viewport coordinates and follow the page scroll). Scrolling
  // the editor anchor into view via scrollAnchorIntoView is what brings
  // the matching card on-screen. We intentionally don't run a sidebar
  // scrollIntoView here — doing so created a feedback loop where the
  // sidebar's own scroll shifted the cards' containerRect, which
  // shifted desiredTop on the next layout pass, which made the cards
  // march off into space.

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
    <div className="flex min-h-full">
      {/* Main content — uses the page-level (body) scroll, not its own
          inner scroller. The sticky editor toolbar pins to the viewport
          as the user scrolls through a long document. Width fills the
          available space minus the comment sidebar; we cap the inner
          reading column when the browser is very wide so line length
          stays comfortable, but otherwise we want the editor to use
          every pixel the user has given us. */}
      <div className="flex-1 min-w-0">
        <div className={`mx-auto px-8 py-8 ${editing ? "max-w-none" : "max-w-5xl"}`}>
          <DocumentToolbar
            doc={doc}
            me={me}
            signedIn={!!user}
            onRename={renameDoc}
            onRevise={handleReviseClick}
            onEdit={() => withIdentity(startEditing)}
            editLockedBy={editLock.locked && !editLock.mine ? editLock.holder : undefined}
            onPushback={() => withIdentity(() => setShowPushback(true))}
            onShare={() => setShowShare(true)}
            onDownload={handleDownload}
            onDelete={deleteDoc}
          />

          {driftPresent && doc.sourceUrl && (
            <SourceDriftBanner
              githubURL={doc.sourceUrl}
              driftedAt={doc.sourceDriftedAt}
              canSync={!!user}
              onMerge={() => setShowMerge(true)}
              onIgnore={confirmIgnoreDrift}
              isRevision={Boolean(doc.parentId || doc.revisionMeta)}
            />
          )}

          {/* Review-state buttons + agent-proposed banner (P0-1, P0-3).
              Rendered high in the doc surface so the coordination
              state is visible before the reviewer scrolls. Only for
              signed-in humans. Bearer-token users don't see this
              surface — they get the same primitives over MCP. */}
          {user && id && (
            <ReviewBar
              doc={doc}
              onDocRefresh={async () => {
                try {
                  const d = await api.getDocument(id);
                  setDoc(d);
                } catch (err) {
                  if (err instanceof APIError) setError(err);
                }
              }}
              onError={(err) => setError(err)}
            />
          )}

          {/* Multi-file gist affordance — surfaces when the gist has
              more files than the one we ingested. Clicking opens the
              gist landing page so the user can copy a different
              file's URL into mumd. No in-app file picker MVP. */}
          {doc.sourceKind === "gist" &&
            doc.gistOwner &&
            doc.gistId &&
            (doc.gistFileCount ?? 0) > 1 && (
              <div className="mb-4 rounded-md border border-rule bg-soft px-3 py-2 text-sm text-muted">
                This gist has {(doc.gistFileCount ?? 1) - 1} other file
                {(doc.gistFileCount ?? 1) - 1 === 1 ? "" : "s"} —{" "}
                <a
                  href={`https://gist.github.com/${doc.gistOwner}/${doc.gistId}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-accent hover:underline"
                >
                  open the gist on GitHub
                </a>{" "}
                to pick a different one.
              </div>
            )}

          {reanchorTarget && (
            <div className="mb-4 rounded-lg border-2 border-accent bg-accent-soft p-3 flex items-start gap-3">
              <div className="flex-1 text-sm">
                <div className="font-medium">
                  Re-anchoring: “{reanchorTarget.originalExact || reanchorTarget.anchor.exact}”
                </div>
                <div className="text-muted mt-0.5">
                  Select the new text in the doc, then click <em>Re-anchor here</em>{" "}
                  in the popover. The comment will move to point at the new selection.
                </div>
              </div>
              <button
                onClick={cancelReanchor}
                className="text-xs px-2 py-1 rounded border border-rule hover:bg-soft"
              >
                Cancel
              </button>
            </div>
          )}

          {/* Editor mode replaces the rendered markdown with a textarea +
              live preview. Comments + drift banner stay visible above. */}
          {editing ? (
            <EditorPane
              ref={editorRef}
              initialContent={doc.content}
              sourceUrl={doc.sourceUrl}
              saving={editSaving}
              onSave={handleManualSave}
              onCancel={() => setEditing(false)}
              activeAnchorExact={
                activeId
                  ? comments.find((c) => c.id === activeId)?.anchor.exact
                  : undefined
              }
              onLayoutTick={() => setLayoutTick((n) => n + 1)}
            />
          ) : (
            <MarkdownRender
              ref={contentRef}
              content={doc.content}
              baseUrl={baseURLForDoc(doc.sourceUrl)}
              sourceUrl={doc.sourceUrl}
            />
          )}

          {orphanComments.length > 0 && (
            <div className="mt-10 pt-6 border-t border-rule">
              <h2 className="text-sm font-semibold uppercase tracking-wide text-muted mb-3">
                Comments without anchors{" "}
                <span
                  className="ml-1 inline-flex items-center justify-center min-w-[1.5em] px-1.5 py-0.5 rounded-full text-[10px] font-bold"
                  style={{
                    backgroundColor: "var(--color-warn-border)",
                    color: "var(--color-warn-ink)",
                  }}
                >
                  {orphanComments.length}
                </span>
              </h2>
              <p className="text-xs text-muted mb-4">
                These comments referred to text that no longer appears in the document.
                Re-anchor them to new text or pin as document-level comments.
              </p>
              <div className="space-y-3">
                {orphanComments.map((c) => (
                  <OrphanCommentCard
                    key={c.id}
                    comment={c}
                    me={me}
                    onStartReanchor={() => startReanchor(c)}
                    onMakeDocLevel={() => makeDocLevel(c)}
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
                    requireIdentity={withIdentity}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Comment sidebar — sticks to the viewport. Internal scroll is
          allowed for the linear sections (doc-level pins / orphans /
          composer) but the anchored cards live in viewport space and
          follow the page scroll. The card layout (see useLayoutEffect
          above) explicitly avoids reading the cards-container's
          viewport rect so internal sidebar scroll never feeds back into
          card positioning. */}
      <aside
        ref={sidebarRef}
        className="w-96 shrink-0 border-l border-rule bg-card overflow-y-auto sticky top-0 h-screen self-start"
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

        {/* Doc-level comments — flow normally above the anchored
            section. Always visible across all filters (they aren't
            "open" or "resolved" in the inline sense). */}
        {(docLevelComments.length > 0 || docLevelOpen) && (
          <div className="px-3 pt-3 pb-2 border-b border-rule">
            <div className="flex items-center justify-between mb-2 text-xs text-muted">
              <span className="font-medium uppercase tracking-wide">
                Document-level
              </span>
              {!docLevelOpen && (
                <button
                  onClick={() => withIdentity(() => setDocLevelOpen(true))}
                  className="text-accent hover:underline"
                >
                  + Add
                </button>
              )}
            </div>
            {docLevelOpen && (
              <div className="mb-3">
                <NewCommentComposer
                  documentId={doc.id}
                  quotedText=""
                  onSubmit={submitDocLevelComment}
                  onCancel={() => setDocLevelOpen(false)}
                />
              </div>
            )}
            <div className="space-y-2">
              {docLevelComments.map((c) => (
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
                  onApplySuggestion={
                    c.suggestion && !c.suggestion.appliedAt
                      ? () => handleApplySuggestion(c)
                      : undefined
                  }
                />
              ))}
            </div>
          </div>
        )}

        {docLevelComments.length === 0 && !docLevelOpen && (
          <div className="px-3 pt-2 pb-1 border-b border-rule text-right">
            <button
              onClick={() => withIdentity(() => setDocLevelOpen(true))}
              className="text-[11px] text-muted hover:text-accent"
            >
              + Doc-level comment
            </button>
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
            ref={anchorsContainerRef}
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
            {visibleComments.map((c, idx) => {
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
                  <CommentStepNav
                    position={idx + 1}
                    total={visibleComments.length}
                    onPrev={() => stepComment(-1)}
                    onNext={() => stepComment(1)}
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

      {/* Floating selection popover. In re-anchor mode, the action
          relabels to "Re-anchor here" and commits to the orphan
          comment instead of opening a new composer. */}
      {selection && !composer && (
        <SelectionPopover
          x={selection.popX}
          y={selection.popY}
          onComment={openComposerForSelection}
          reanchorMode={!!reanchorTarget}
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

      {showMerge && (
        <MergeModal
          doc={doc}
          onClose={() => setShowMerge(false)}
          onMerged={handleMerged}
        />
      )}

      {showPushback && (
        <PushbackModal
          doc={doc}
          onClose={() => setShowPushback(false)}
          onPushed={async () => {
            // A direct-commit pushback to the doc's tracking branch
            // means our content IS the new upstream. Refetch so the
            // drift banner clears immediately (the backend stamped
            // the new blob SHA as our SourceSHA). PR mode + commits
            // to other branches are harmless to refetch — the doc
            // state just round-trips.
            if (!id) return;
            try {
              const next = await api.getDocument(id);
              setDoc(next);
            } catch {
              /* non-fatal — the SSE doc-updated event will catch us up */
            }
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

