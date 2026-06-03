import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, APIError } from "../api";
import type { DocumentSummary } from "../types";
import { formatRelative } from "../utils/format";
import ErrorBlock from "../components/ErrorBlock";
import { useDialog } from "../components/Dialogs";

export default function HomePage() {
  const navigate = useNavigate();
  const dialog = useDialog();
  const [docs, setDocs] = useState<DocumentSummary[] | null>(null);
  const [error, setError] = useState<APIError | null>(null);

  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);

  function setErrFrom(err: unknown) {
    if (err instanceof APIError) setError(err);
    else setError(new APIError((err as Error).message));
  }

  async function refresh() {
    try {
      const list = await api.listDocuments();
      setDocs(list);
    } catch (err) {
      setErrFrom(err);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  async function addFromURL(e: React.FormEvent) {
    e.preventDefault();
    if (!url.trim()) return;
    setBusy(true);
    setError(null);
    try {
      const doc = await api.createFromURL(url.trim());
      navigate(`/d/${doc.id}`);
    } catch (err) {
      setErrFrom(err);
    } finally {
      setBusy(false);
    }
  }

  async function handleUpload(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    setBusy(true);
    setError(null);
    try {
      const text = await file.text();
      const title = file.name.replace(/\.md$/i, "");
      const doc = await api.createFromContent(text, title);
      navigate(`/d/${doc.id}`);
    } catch (err) {
      setErrFrom(err);
    } finally {
      setBusy(false);
      e.target.value = "";
    }
  }

  async function handleDelete(id: string, title: string) {
    const ok = await dialog.confirm({
      title: "Delete document?",
      body: `Delete "${title}" and all its comments? This cannot be undone.`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteDocument(id);
      refresh();
    } catch (err) {
      setErrFrom(err);
    }
  }

  return (
    <div className="max-w-4xl mx-auto px-6 py-10">
      <h1 className="text-3xl font-semibold tracking-tight mb-2">
        Comment on any markdown file
      </h1>
      <p className="text-muted mb-8">
        Paste a URL to a <code className="bg-soft px-1 rounded">.md</code> file
        (raw or GitHub blob) or upload one from your computer. Select text to
        leave inline comments — Google Docs style.
      </p>

      <div className="bg-card border border-rule rounded-lg p-5 mb-6">
        <form onSubmit={addFromURL} className="flex gap-2">
          <input
            type="url"
            placeholder="https://github.com/owner/repo/blob/main/README.md"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            className="flex-1 border border-rule rounded px-3 py-2 focus:outline-none focus:border-accent"
            disabled={busy}
          />
          <button
            type="submit"
            disabled={busy || !url.trim()}
            className="px-4 py-2 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
          >
            {busy ? "Loading…" : "Open"}
          </button>
        </form>
        <div className="text-sm text-muted mt-3 flex items-center gap-3">
          <span>or</span>
          <label className="cursor-pointer text-accent hover:underline">
            upload a local .md file
            <input
              type="file"
              accept=".md,text/markdown,text/plain"
              className="hidden"
              onChange={handleUpload}
            />
          </label>
        </div>
        {error && (
          <div className="mt-3">
            <ErrorBlock error={error} onDismiss={() => setError(null)} />
          </div>
        )}
      </div>

      <h2 className="text-lg font-semibold mb-3 mt-8">Recent documents</h2>
      {docs === null ? (
        <div className="text-muted">Loading…</div>
      ) : docs.length === 0 ? (
        <div className="text-muted">No documents yet.</div>
      ) : (
        <ul className="divide-y divide-rule border border-rule rounded-lg bg-card">
          {docs.map((d) => (
            <li
              key={d.id}
              className="flex items-center justify-between px-4 py-3"
            >
              <div className="min-w-0">
                <Link
                  to={`/d/${d.id}`}
                  className="font-medium text-ink hover:text-accent flex items-center gap-2"
                >
                  <span className="truncate">{d.title}</span>
                  {d.private && (
                    <span
                      className="inline-flex items-center gap-1 text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded bg-soft text-muted shrink-0"
                      title={
                        d.githubOwner && d.githubRepo
                          ? `Private — requires GitHub access to ${d.githubOwner}/${d.githubRepo}`
                          : "Private — requires GitHub access"
                      }
                    >
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                        <rect x="3" y="11" width="18" height="11" rx="2" />
                        <path d="M7 11V7a5 5 0 0 1 10 0v4" />
                      </svg>
                      Private
                    </span>
                  )}
                </Link>
                <div className="text-xs text-muted mt-0.5 truncate">
                  {d.origin === "url" && d.sourceUrl ? d.sourceUrl : "Uploaded"}
                  {" · "}updated {formatRelative(d.updatedAt)}
                </div>
              </div>
              <button
                onClick={() => handleDelete(d.id, d.title)}
                className="text-sm text-faint hover:text-danger ml-4"
              >
                Delete
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
