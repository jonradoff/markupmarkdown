import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, APIError } from "../api";
import type { IndexProgressEvent, MarkdownIndexItem, MarkdownIndexResponse } from "../types";
import ErrorBlock from "../components/ErrorBlock";
import { useToast, toastMessageFor } from "../components/Toast";
import { useDialog } from "../components/Dialogs";
import { useAuth } from "../auth";
import { formatRelative } from "../utils/format";
import { canonicalIndexPath, rewriteToCanonical } from "../utils/canonicalUrl";

// IndexPage renders a single markdown-index (repo / user / org).
// Clicking a row ingests the corresponding .md file via the existing
// document-create flow and redirects to the new doc page so the user
// can comment on it. The index page itself is shareable — anyone with
// the link can open it, subject to the backend's access checks (the
// repo/profile/org being public, or the viewer being signed in and
// having GitHub access).
export default function IndexPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { user } = useAuth();
  const toast = useToast();
  const dialog = useDialog();

  const [index, setIndex] = useState<MarkdownIndexResponse | null>(null);
  const [items, setItems] = useState<MarkdownIndexItem[]>([]);
  const [error, setError] = useState<APIError | null>(null);
  const [openingURL, setOpeningURL] = useState<string | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [titleDraft, setTitleDraft] = useState("");
  const [busy, setBusy] = useState(false);
  // Live progress so the user sees what's happening on a long scan
  // instead of staring at "Loading…" while a 150-repo org spider runs.
  const [progress, setProgress] = useState<{
    status: string;
    current: number;
    total: number;
    scanning: boolean;
  }>({ status: "Connecting…", current: 0, total: 0, scanning: true });
  const [streamErr, setStreamErr] = useState<string | null>(null);
  const [recentRepos, setRecentRepos] = useState<string[]>([]);
  const truncatedRef = useRef(false);

  // Saved filename filters (tabs). Empty string = "All". Tabs are
  // local to the browser/user and persisted to localStorage scoped by
  // index id so each index keeps its own tab set. Max 5 tabs + "All".
  // Last-selected tab is remembered across visits; first-time visitors
  // fall through to the index's owner-pinned defaultFilter (if any).
  const [tabs, setTabs] = useState<string[]>([]);
  const [activeTab, setActiveTab] = useState<string | null>(null);
  const [newTab, setNewTab] = useState<string>("");
  useEffect(() => {
    if (!id) return;
    try {
      const rawTabs = localStorage.getItem(`mumd:idx-tabs:${id}`);
      setTabs(rawTabs ? (JSON.parse(rawTabs) as string[]) : []);
    } catch {
      setTabs([]);
    }
  }, [id]);
  // Resolve the initial activeTab once the index meta arrives. Visitor's
  // saved choice wins; otherwise we honor the owner's pinned default.
  useEffect(() => {
    if (activeTab !== null || !index || !id) return;
    try {
      const stored = localStorage.getItem(`mumd:idx-active:${id}`);
      if (stored !== null) {
        setActiveTab(stored);
        return;
      }
    } catch {
      /* ignore */
    }
    const pinned = (index.defaultFilter || "").trim();
    setActiveTab(pinned);
    // Seed the pinned filter into the user's tab list so it's visible
    // as a chip alongside "All", not just a silent pre-filter.
    if (pinned) {
      setTabs((cur) => (cur.includes(pinned) ? cur : [...cur, pinned].slice(-5)));
    }
  }, [index, id, activeTab]);
  useEffect(() => {
    if (!id) return;
    try {
      localStorage.setItem(`mumd:idx-tabs:${id}`, JSON.stringify(tabs));
    } catch {
      /* quota or privacy mode — ignore */
    }
  }, [id, tabs]);
  useEffect(() => {
    if (!id || activeTab === null) return;
    try {
      localStorage.setItem(`mumd:idx-active:${id}`, activeTab);
    } catch {
      /* ignore */
    }
  }, [id, activeTab]);

  function addTab(filter: string) {
    const f = filter.trim();
    if (!f) return;
    setTabs((cur) => {
      if (cur.includes(f)) return cur;
      const next = [...cur, f];
      // Cap at 5 saved tabs — the user wants quick access, not a
      // sprawling tab strip. Oldest gets bumped.
      return next.slice(Math.max(0, next.length - 5));
    });
    setActiveTab(f);
    setNewTab("");
  }
  function removeTab(filter: string) {
    setTabs((cur) => cur.filter((t) => t !== filter));
    setActiveTab((cur) => (cur === filter ? "" : cur));
  }

  // Owner-only: pin (or unpin) a filter as the default view for
  // share-link visitors. Persists server-side so a freshly-arriving
  // visitor without a personal tab choice lands on the pinned filter.
  async function setPinnedDefault(filter: string) {
    if (!index) return;
    setBusy(true);
    try {
      const updated = await api.patchIndex(index.id, { defaultFilter: filter });
      setIndex((cur) => (cur ? { ...cur, defaultFilter: updated.defaultFilter } : cur));
      toast.success(
        filter
          ? `Pinned "${filter}" as the default for share-link visitors.`
          : "Default filter cleared.",
      );
    } catch (err) {
      toast.error(toastMessageFor(err) || "Couldn't pin the filter.");
    } finally {
      setBusy(false);
    }
  }

  // Open the SSE stream. The first event ("meta") carries the index
  // metadata so the title and chips render immediately; subsequent
  // events carry per-repo progress + items batches. We use fetch +
  // ReadableStream rather than EventSource so we get the same cookie
  // session the rest of the app uses (EventSource ignores credentials
  // in some browsers).
  //
  // `force` opens the stream with ?refresh=1 — Backed by an explicit
  // Refresh button. Without it, the backend serves the cached items
  // for instant subsequent visits and only re-spiders GitHub when
  // the user explicitly asks.
  const reload = useCallback((force = false) => {
    if (!id) return undefined;
    setError(null);
    setItems([]);
    setStreamErr(null);
    setRecentRepos([]);
    truncatedRef.current = false;
    setProgress({
      status: force ? "Refreshing from GitHub…" : "Loading index…",
      current: 0,
      total: 0,
      scanning: true,
    });
    const controller = new AbortController();
    streamIndexItems(id, force, controller.signal, (ev) => {
      switch (ev.kind) {
        case "meta": {
          // Backend sends the full Index meta as the first event.
          const meta = ev as unknown as MarkdownIndexResponse;
          setIndex({ ...meta, items: [] });
          setTitleDraft(meta.title);
          document.title = `${meta.title} · markupmarkdown`;
          // Replace /i/:slug in the address bar with /:owner or
          // /:owner/:repo so the URL reads as the human pasted it.
          // Safe now that the SSE parser kind collision is fixed:
          // replaceState updates only the address bar; React Router
          // doesn't notice (no popstate fires) so IndexPage stays
          // mounted and the rendered content matches the URL the
          // user wants to share.
          const canonical = canonicalIndexPath(meta);
          if (canonical) rewriteToCanonical(canonical);
          break;
        }
        case "ready":
          setProgress((p) => ({ ...p, status: "Starting scan…" }));
          break;
        case "status":
          setProgress((p) => ({ ...p, status: ev.message || p.status }));
          break;
        case "scanning":
          if (ev.items && ev.items.length > 0) {
            setItems((cur) => mergeAndSort([...cur, ...(ev.items || [])]));
          }
          if (ev.repo) {
            // Keep the last 8 repo names so the user sees a small
            // scrolling activity log instead of a single line that
            // updates too fast to read on a 150-repo org.
            setRecentRepos((cur) =>
              [`${ev.repo}${ev.items ? ` · ${ev.items.length} md` : ""}`, ...cur].slice(0, 8),
            );
          }
          setProgress({
            status: ev.message || `Scanning ${ev.current}/${ev.total}…`,
            current: ev.current ?? 0,
            total: ev.total ?? 0,
            scanning: true,
          });
          break;
        case "items":
          if (ev.items && ev.items.length > 0) {
            setItems((cur) => mergeAndSort([...cur, ...(ev.items || [])]));
          }
          if (ev.truncated) truncatedRef.current = true;
          break;
        case "done":
          setProgress((p) => ({ ...p, status: "Scan complete", scanning: false }));
          break;
        case "error":
          setStreamErr(ev.message || ev.error || "Couldn't finish scanning.");
          setProgress((p) => ({ ...p, scanning: false }));
          break;
      }
    })
      .then(() => {
        // Stream closed cleanly. If a "done" event never arrived
        // (proxy mid-flight drop, connection reset), this is the
        // belt-and-suspenders that flips scanning off so the
        // progress banner doesn't spin forever.
        if (controller.signal.aborted) return;
        setProgress((p) => (p.scanning ? { ...p, scanning: false, status: "Scan complete" } : p));
      })
      .catch((err) => {
        if (controller.signal.aborted) return;
        // 401 / 404 from the stream — fall back to the synchronous GET
        // so the user sees the structured APIError page instead of a
        // blank stream-error toast.
        api.getIndex(id)
          .then((res) => {
            setIndex(res);
            setItems(res.items);
            setTitleDraft(res.title);
            setProgress((p) => ({ ...p, scanning: false, status: "Scan complete" }));
          })
          .catch((gErr) => {
            setError(gErr instanceof APIError ? gErr : new APIError(err instanceof Error ? err.message : "Stream interrupted."));
            setProgress((p) => ({ ...p, scanning: false }));
          });
      });
    return () => controller.abort();
  }, [id]);

  useEffect(() => {
    const cleanup = reload();
    return () => {
      cleanup?.();
      document.title = "markupmarkdown";
    };
  }, [reload]);

  async function openFile(item: MarkdownIndexItem) {
    if (openingURL) return;
    setOpeningURL(item.url);
    try {
      const res = await api.createFromURL(item.url);
      if ("kind" in res && res.kind === "self_doc_redirect") {
        navigate(`/d/${res.documentId}`);
      } else {
        navigate(`/d/${(res as { id: string }).id}`);
      }
    } catch (err) {
      toast.error(toastMessageFor(err) || "Couldn't open that file.");
      setOpeningURL(null);
    }
  }

  async function commitRename() {
    if (!index) return;
    const title = titleDraft.trim();
    if (!title || title === index.title) {
      setRenaming(false);
      setTitleDraft(index.title);
      return;
    }
    setBusy(true);
    try {
      const updated = await api.patchIndex(index.id, { title });
      setIndex(updated);
      setRenaming(false);
      toast.success("Index renamed.");
    } catch (err) {
      toast.error(toastMessageFor(err) || "Couldn't rename.");
    } finally {
      setBusy(false);
    }
  }

  async function deleteThisIndex() {
    if (!index) return;
    const ok = await dialog.confirm({
      title: "Delete this index?",
      body:
        "Removes the shared link from your library. We don't delete any of the underlying GitHub files or any documents you've opened from this index.",
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    try {
      await api.deleteIndex(index.id);
      toast.success("Index deleted.");
      navigate("/");
    } catch (err) {
      toast.error(toastMessageFor(err) || "Couldn't delete the index.");
      setBusy(false);
    }
  }

  async function shareLink() {
    const url = `${window.location.origin}/i/${id}`;
    try {
      await navigator.clipboard.writeText(url);
      toast.success("Link copied to clipboard");
    } catch {
      toast.info(url);
    }
  }

  if (error) {
    return (
      <div className="max-w-4xl mx-auto px-6 py-10">
        <ErrorBlock error={error} />
        <div className="mt-4">
          <Link to="/" className="text-sm text-accent hover:underline">
            ← Back home
          </Link>
        </div>
      </div>
    );
  }
  // While the meta event hasn't arrived yet, render the progress
  // banner anyway so the user sees what's happening from the first
  // frame instead of staring at "Loading…". Once meta lands the rest
  // of the page mounts in place.
  if (!index) {
    return (
      <div className="max-w-4xl mx-auto px-6 py-10">
        <div className="text-xs text-muted mb-2">
          <Link to="/" className="hover:text-accent">
            ← All docs
          </Link>
        </div>
        <ProgressBanner
          progress={progress}
          streamErr={streamErr}
          totalShown={items.length}
          percent={
            progress.total > 0
              ? Math.round((progress.current / progress.total) * 100)
              : 0
          }
          recentRepos={recentRepos}
        />
      </div>
    );
  }

  const isOwner = !!user && user.id === index.owner;
  const isMine = !!user; // mine controls (rename/delete) require sign-in; backend additionally checks creator
  const kindLabel =
    index.kind === "repo"
      ? "Repository"
      : index.kind === "org"
        ? "Organization"
        : "User profile";

  // Apply the active filename filter (case-insensitive substring on
  // basename + path). Empty filter = "All" tab = pass through.
  const effectiveTab = activeTab ?? "";
  const filteredItems = effectiveTab
    ? items.filter((it) => matchesFilter(it, effectiveTab))
    : items;
  const pinnedDefault = (index?.defaultFilter || "").trim();
  const isCreator =
    !!user && !!index?.createdById && user.id === index.createdById;
  // Group items by repo for user/org listings so multi-repo views feel
  // organized rather than a flat firehose. We read from `items` (the
  // streamed live list) rather than index.items so updates appear as
  // the scan progresses.
  const grouped =
    index.kind === "repo"
      ? null
      : groupByRepo(filteredItems);
  const totalShown = filteredItems.length;
  const totalAll = items.length;
  const percent =
    progress.total > 0
      ? Math.round((progress.current / progress.total) * 100)
      : 0;

  return (
    <div className="max-w-5xl mx-auto px-6 py-8">
      <div className="text-xs text-muted mb-2">
        <Link to="/" className="hover:text-accent">
          ← All docs
        </Link>
      </div>

      <div className="flex items-start justify-between gap-4 mb-1">
        {renaming ? (
          <input
            value={titleDraft}
            onChange={(e) => setTitleDraft(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitRename();
              if (e.key === "Escape") {
                setRenaming(false);
                setTitleDraft(index.title);
              }
            }}
            autoFocus
            disabled={busy}
            className="text-2xl font-semibold tracking-tight bg-transparent border-b border-rule focus:border-accent outline-none flex-1 min-w-0"
          />
        ) : (
          <button
            onClick={() => isMine && setRenaming(true)}
            className="text-2xl font-semibold tracking-tight text-ink hover:text-accent text-left flex-1 min-w-0 truncate"
            title={isMine ? "Click to rename" : ""}
          >
            {index.title}
          </button>
        )}
        <div className="flex items-center gap-3 text-sm shrink-0">
          <button
            onClick={() => reload(true)}
            className="text-muted hover:text-ink disabled:opacity-50"
            disabled={progress.scanning}
            title="Re-scan GitHub for the latest markdown files (otherwise cached)"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <polyline points="23 4 23 10 17 10" />
              <polyline points="1 20 1 14 7 14" />
              <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
            </svg>
          </button>
          <button onClick={shareLink} className="text-muted hover:text-ink" title="Copy share link">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="18" cy="5" r="3" />
              <circle cx="6" cy="12" r="3" />
              <circle cx="18" cy="19" r="3" />
              <line x1="8.59" y1="13.51" x2="15.42" y2="17.49" />
              <line x1="15.41" y1="6.51" x2="8.59" y2="10.49" />
            </svg>
          </button>
          {isMine && (
            <button onClick={deleteThisIndex} className="text-faint hover:text-danger" disabled={busy}>
              Delete
            </button>
          )}
        </div>
      </div>

      <div className="text-xs text-muted mb-6 flex flex-wrap items-center gap-2">
        <span className="inline-flex items-center gap-1 bg-soft px-2 py-0.5 rounded">
          <span className="text-faint">{kindLabel}:</span>
          <a href={index.sourceUrl} target="_blank" rel="noreferrer" className="text-accent hover:underline">
            {index.sourceUrl.replace(/^https:\/\/github\.com\//, "")}
          </a>
        </span>
        {index.private && (
          <span className="inline-flex items-center gap-1 bg-warn-bg text-warn-ink px-2 py-0.5 rounded">
            Private
          </span>
        )}
        <span>· updated {formatRelative(index.updatedAt)}</span>
        {index.truncated && (
          <span className="text-warn-action">
            · listing truncated by GitHub (very large tree)
          </span>
        )}
      </div>

      {!user && (
        <div className="mb-6 rounded-lg border border-rule bg-card p-3 text-sm text-muted">
          You're viewing this as a guest — only public files are shown. Sign in
          with GitHub to see private repo contents you have access to.
        </div>
      )}

      {/* Filename filter tabs. "All" is always present; the user can
          add up to 5 named filters that case-insensitively match the
          filename or path (e.g. "claude.md", "_PRD"). Tabs persist to
          localStorage scoped by index id; the last-selected tab
          reopens on return. The index creator can pin one as the
          default that share-link visitors see first. */}
      <div className="mb-3 flex items-center gap-1 flex-wrap">
        <span
          className={[
            "inline-flex items-center text-xs rounded border overflow-hidden",
            effectiveTab === ""
              ? "border-accent bg-accent-soft text-accent font-medium"
              : "border-rule text-muted hover:text-ink",
          ].join(" ")}
        >
          <button
            onClick={() => setActiveTab("")}
            className="px-2.5 py-1 hover:bg-soft/50 inline-flex items-center gap-1"
          >
            {pinnedDefault === "" && (
              <PinIcon
                filled
                className="w-3 h-3 text-accent"
                title="Pinned as default for share-link visitors"
              />
            )}
            All
            <span className="ml-1 text-faint tabular-nums">{totalAll}</span>
          </button>
          {isCreator && pinnedDefault !== "" && (
            <button
              onClick={() => setPinnedDefault("")}
              disabled={busy}
              className="px-1.5 py-1 text-faint hover:text-accent border-l border-rule"
              title="Pin 'All' as the default for share-link visitors"
            >
              <PinIcon className="w-3 h-3" />
            </button>
          )}
        </span>
        {tabs.map((t) => {
          const count = items.filter((it) => matchesFilter(it, t)).length;
          const isActive = effectiveTab === t;
          const isPinned = pinnedDefault === t;
          return (
            <span
              key={t}
              className={[
                "inline-flex items-center text-xs rounded border overflow-hidden",
                isActive
                  ? "border-accent bg-accent-soft text-accent font-medium"
                  : "border-rule text-muted hover:text-ink",
              ].join(" ")}
            >
              <button
                onClick={() => setActiveTab(t)}
                className="px-2.5 py-1 hover:bg-soft/50 inline-flex items-center gap-1"
                title={`Filter: filenames containing "${t}"${isPinned ? " (pinned default)" : ""}`}
              >
                {isPinned && (
                  <PinIcon filled className="w-3 h-3 text-accent" title="Pinned default" />
                )}
                {t}
                <span className="ml-1 text-faint tabular-nums">{count}</span>
              </button>
              {isCreator && (
                <button
                  onClick={() => setPinnedDefault(isPinned ? "" : t)}
                  disabled={busy}
                  className="px-1.5 py-1 text-faint hover:text-accent border-l border-rule"
                  title={
                    isPinned
                      ? `Unpin "${t}" as default`
                      : `Pin "${t}" as the default for share-link visitors`
                  }
                >
                  <PinIcon filled={isPinned} className="w-3 h-3" />
                </button>
              )}
              <button
                onClick={() => removeTab(t)}
                className="px-1.5 py-1 text-faint hover:text-danger border-l border-rule"
                title={`Remove the "${t}" tab`}
              >
                ×
              </button>
            </span>
          );
        })}
        {tabs.length < 5 && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              addTab(newTab);
            }}
            className="inline-flex items-center"
          >
            <input
              value={newTab}
              onChange={(e) => setNewTab(e.target.value)}
              placeholder="+ Add filter…"
              className="text-xs px-2 py-1 rounded border border-rule bg-transparent focus:outline-none focus:border-accent w-32"
            />
          </form>
        )}
        {tabs.length >= 5 && (
          <span className="text-xs text-faint">
            (max 5 — remove one to add another)
          </span>
        )}
      </div>

      <ProgressBanner
        progress={progress}
        streamErr={streamErr}
        totalShown={totalShown}
        percent={percent}
        recentRepos={recentRepos}
      />


      {totalShown === 0 ? (
        !progress.scanning ? (
          <div className="rounded-lg border border-rule bg-card p-10 text-center text-muted">
            No markdown files found.
            {index.kind !== "repo" && (
              <div className="text-xs text-faint mt-2">
                We list each repo's <code className="bg-soft px-1 rounded">.md</code> files
                at the root. Subdirectory files aren't included in profile / org
                indexes.
              </div>
            )}
          </div>
        ) : null
      ) : grouped ? (
        <div className="space-y-6">
          {grouped.map((g) => (
            <div key={g.repo}>
              <div className="text-sm font-medium text-ink mb-1 flex items-center gap-2">
                <a
                  href={g.repoUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="hover:text-accent"
                >
                  {g.repo}
                </a>
                {g.private && (
                  <span className="text-[10px] uppercase tracking-wide bg-warn-bg text-warn-ink rounded px-1 py-0.5">
                    private
                  </span>
                )}
              </div>
              {g.description && (
                <div className="text-xs text-muted mb-1">{g.description}</div>
              )}
              <ItemList
                items={g.items}
                openingURL={openingURL}
                onOpen={openFile}
                showRepo={false}
              />
            </div>
          ))}
        </div>
      ) : (
        <ItemList
          items={filteredItems}
          openingURL={openingURL}
          onOpen={openFile}
          showRepo={false}
        />
      )}

      {/* Hidden ownership marker — silences the unused-var lint for
          isOwner without breaking the visible UI. */}
      {isOwner && <span className="sr-only">owner view</span>}
    </div>
  );
}

// ProgressBanner is the "we're scanning, here's what's happening"
// strip rendered above the listing. Visible from the moment the
// component mounts (before the index meta even arrives) so the user
// gets immediate feedback that something IS happening. The recent
// repo log shows the most recent 8 repos as they come in — a 150-repo
// org now reads like a live build log instead of a frozen spinner.
function ProgressBanner({
  progress,
  streamErr,
  totalShown,
  percent,
  recentRepos,
}: {
  progress: { status: string; current: number; total: number; scanning: boolean };
  streamErr: string | null;
  totalShown: number;
  percent: number;
  recentRepos: string[];
}) {
  if (!progress.scanning && !streamErr) return null;
  return (
    <div className="mb-4 rounded-lg border border-rule bg-card p-3">
      <div className="flex items-center gap-2 text-sm flex-wrap">
        {progress.scanning && (
          <span
            aria-hidden
            className="inline-block w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin shrink-0"
          />
        )}
        <span className="text-ink font-medium">
          {streamErr ? "Scan failed" : progress.status}
        </span>
        {progress.total > 0 && (
          <span className="text-muted tabular-nums text-xs">
            {progress.current} / {progress.total} repos
          </span>
        )}
        {totalShown > 0 && (
          <span className="text-faint text-xs">
            · {totalShown} markdown file{totalShown === 1 ? "" : "s"} so far
          </span>
        )}
      </div>
      {progress.total > 0 && (
        <div className="mt-2 h-1.5 bg-soft rounded-full overflow-hidden">
          <div
            className="h-full bg-accent transition-[width] duration-200"
            style={{ width: `${percent}%` }}
          />
        </div>
      )}
      {/* Live activity log — newest at top so the most-recent repo
          stays in the same place (much easier to read than a feed
          that scrolls past too quickly). */}
      {recentRepos.length > 0 && (
        <ul className="mt-2 text-[11px] text-muted font-mono leading-snug max-h-32 overflow-hidden">
          {recentRepos.map((r, i) => (
            <li key={`${r}-${i}`} className="truncate" style={{ opacity: 1 - i * 0.1 }}>
              <span className="text-faint">›</span> {r}
            </li>
          ))}
        </ul>
      )}
      {streamErr && (
        <div className="mt-2 text-xs text-danger">{streamErr}</div>
      )}
    </div>
  );
}

// PinIcon — a simple thumbtack glyph used by the owner-only "pin as
// default" affordance next to filter tabs.
function PinIcon({
  filled = false,
  className = "",
  title,
}: {
  filled?: boolean;
  className?: string;
  title?: string;
}) {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill={filled ? "currentColor" : "none"}
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden={title ? undefined : true}
    >
      {title && <title>{title}</title>}
      <line x1="12" y1="17" x2="12" y2="22" />
      <path d="M5 17h14v-1.76a2 2 0 0 0-1.11-1.79L15 12V7h1a1 1 0 0 0 1-1V4a1 1 0 0 0-1-1H8a1 1 0 0 0-1 1v2a1 1 0 0 0 1 1h1v5l-2.89 1.45A2 2 0 0 0 5 15.24V17z" />
    </svg>
  );
}

// matchesFilter returns true if `it` matches the case-insensitive
// substring filter. We test against the basename first (the common
// case — "claude.md", "README") and fall back to the full
// repo/path string so filters like "_PRD" still find files nested in
// subdirectories. For repo indexes the basename test is enough; the
// fallback only kicks in for user/org variants where path-in-repo
// can be more discriminating than the filename alone.
function matchesFilter(it: MarkdownIndexItem, filter: string): boolean {
  const f = filter.toLowerCase();
  if (it.title.toLowerCase().includes(f)) return true;
  if (it.pathInRepo && it.pathInRepo.toLowerCase().includes(f)) return true;
  if (it.repo && it.repo.toLowerCase().includes(f)) return true;
  return false;
}

// streamIndexItems opens an SSE stream and dispatches each event to
// `onEvent`. We use fetch + ReadableStream (not EventSource) because
// EventSource ignores cookies in some browsers, and our SSE endpoint
// is gated by the session cookie. The signal lets the caller abort
// when the component unmounts.
async function streamIndexItems(
  id: string,
  force: boolean,
  signal: AbortSignal,
  onEvent: (ev: IndexProgressEvent & { meta?: MarkdownIndexResponse }) => void
) {
  const qs = force ? "?refresh=1" : "";
  const res = await fetch(`/api/indexes/${id}/stream${qs}`, {
    signal,
    credentials: "same-origin",
    headers: { Accept: "text/event-stream" },
  });
  if (!res.ok) {
    throw new Error(`stream HTTP ${res.status}`);
  }
  if (!res.body) {
    throw new Error("stream response had no body");
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    // SSE messages are separated by a blank line. Each message is one
    // or more `field: value` lines.
    let nl = buf.indexOf("\n\n");
    while (nl >= 0) {
      const raw = buf.slice(0, nl);
      buf = buf.slice(nl + 2);
      const ev = parseSSEFrame(raw);
      if (ev) onEvent(ev);
      nl = buf.indexOf("\n\n");
    }
  }
}

function parseSSEFrame(
  raw: string
): (IndexProgressEvent & { meta?: MarkdownIndexResponse }) | null {
  let event = "";
  let data = "";
  for (const line of raw.split("\n")) {
    if (line.startsWith(":")) continue; // heartbeat / comment
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) data += line.slice(5).trim();
  }
  if (!event) return null;
  let payload: Record<string, unknown> = {};
  if (data) {
    try {
      payload = JSON.parse(data);
    } catch {
      // Server-side guarantees JSON. If we get garbage, surface it.
      payload = { message: data };
    }
  }
  // CRITICAL: event-name wins over payload.kind. Without this, the
  // "meta" event clobbers itself — its payload is the Index model
  // which carries its OWN `kind` field ("user" / "org" / "repo"),
  // so `{kind: "meta", ...payload}` would resolve to `kind: "user"`
  // and the dispatcher would silently fall through. Same trap on the
  // "ready" event whose payload is `{kind: idx.Kind}` for legacy
  // reasons.
  return { ...payload, kind: event as IndexProgressEvent["kind"] };
}

// mergeAndSort produces a stable order: by repo (alphabetical), then
// by path within the repo. Stream events arrive in goroutine order so
// without this the listing would jump around as new repos came in.
function mergeAndSort(items: MarkdownIndexItem[]): MarkdownIndexItem[] {
  const seen = new Set<string>();
  const out: MarkdownIndexItem[] = [];
  for (const it of items) {
    const key = `${it.repo ?? ""}::${it.url}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(it);
  }
  out.sort((a, b) => {
    const ar = a.repo ?? "";
    const br = b.repo ?? "";
    if (ar !== br) return ar.localeCompare(br);
    return (a.pathInRepo ?? "").localeCompare(b.pathInRepo ?? "");
  });
  return out;
}

interface ItemListProps {
  items: MarkdownIndexItem[];
  openingURL: string | null;
  onOpen: (item: MarkdownIndexItem) => void;
  showRepo: boolean;
}

function ItemList({ items, openingURL, onOpen, showRepo }: ItemListProps) {
  return (
    <ul className="rounded-lg border border-rule bg-card divide-y divide-rule overflow-hidden">
      {items.map((it, i) => (
        <li key={`${it.url}-${i}`} className="flex items-center justify-between gap-3 px-4 py-2.5">
          <div className="min-w-0 flex-1">
            <button
              onClick={() => onOpen(it)}
              disabled={openingURL !== null}
              className="text-sm text-ink hover:text-accent font-medium text-left disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {openingURL === it.url ? "Opening…" : it.title}
            </button>
            <div className="text-xs text-muted mt-0.5 truncate">
              {showRepo && it.repo && (
                <a href={it.repoUrl} target="_blank" rel="noreferrer" className="hover:text-accent">
                  {it.repo}
                </a>
              )}
              {showRepo && it.repo && it.pathInRepo && " · "}
              <code className="text-faint">{it.pathInRepo}</code>
            </div>
          </div>
          <a
            href={it.url}
            target="_blank"
            rel="noreferrer"
            className="text-xs text-faint hover:text-accent shrink-0"
            title="Open on GitHub"
          >
            ↗
          </a>
        </li>
      ))}
    </ul>
  );
}

interface RepoGroup {
  repo: string;
  repoUrl?: string;
  description?: string;
  private?: boolean;
  items: MarkdownIndexItem[];
}

function groupByRepo(items: MarkdownIndexItem[]): RepoGroup[] {
  const map = new Map<string, RepoGroup>();
  for (const it of items) {
    const key = it.repo || "(unknown)";
    let g = map.get(key);
    if (!g) {
      g = {
        repo: key,
        repoUrl: it.repoUrl,
        description: it.description,
        private: it.private,
        items: [],
      };
      map.set(key, g);
    }
    g.items.push(it);
  }
  return [...map.values()];
}
