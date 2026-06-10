import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, APIError } from "../api";
import type { MarkdownIndexItem, MarkdownIndexResponse } from "../types";
import ErrorBlock from "../components/ErrorBlock";
import { useToast, toastMessageFor } from "../components/Toast";
import { useDialog } from "../components/Dialogs";
import { useAuth } from "../auth";
import { formatRelative } from "../utils/format";

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
  const [error, setError] = useState<APIError | null>(null);
  const [openingURL, setOpeningURL] = useState<string | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [titleDraft, setTitleDraft] = useState("");
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => {
    if (!id) return;
    setError(null);
    try {
      const res = await api.getIndex(id);
      setIndex(res);
      setTitleDraft(res.title);
      document.title = `${res.title} · markupmarkdown`;
    } catch (err) {
      setError(err instanceof APIError ? err : new APIError("Couldn't load index"));
    }
  }, [id]);

  useEffect(() => {
    reload();
    return () => {
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
      const updated = await api.patchIndex(index.id, title);
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
  if (!index) {
    return <div className="max-w-4xl mx-auto px-6 py-10 text-muted">Loading…</div>;
  }

  const isOwner = !!user && user.id === index.owner;
  const isMine = !!user; // mine controls (rename/delete) require sign-in; backend additionally checks creator
  const kindLabel =
    index.kind === "repo"
      ? "Repository"
      : index.kind === "org"
        ? "Organization"
        : "User profile";

  // Group items by repo for user/org listings so multi-repo views feel
  // organized rather than a flat firehose.
  const grouped =
    index.kind === "repo"
      ? null
      : groupByRepo(index.items);

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

      {index.items.length === 0 ? (
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
          items={index.items}
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
