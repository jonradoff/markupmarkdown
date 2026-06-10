import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, APIError } from "../api";
import type { DocumentSummary, TrashItem } from "../types";
import { formatRelative } from "../utils/format";
import ErrorBlock from "../components/ErrorBlock";
import { useDialog } from "../components/Dialogs";
import { useToast, toastMessageFor } from "../components/Toast";
import { useAuth } from "../auth";

export default function HomePage() {
  const navigate = useNavigate();
  const dialog = useDialog();
  const toast = useToast();
  const { user, githubEnabled, loginURL, loading: authLoading } = useAuth();
  const [docs, setDocs] = useState<DocumentSummary[] | null>(null);
  const [trash, setTrash] = useState<TrashItem[] | null>(null);
  const [showTrash, setShowTrash] = useState(false);
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
      // 401 (sign-in required) is expected when not logged in — don't
      // surface it as a top-of-page error. The empty-state UI handles it.
      if (err instanceof APIError && err.kind === "sign_in_required") {
        setDocs([]);
        return;
      }
      setErrFrom(err);
    }
    // Fetch trash lazily — only signed-in users have one.
    try {
      const t = await api.listTrash();
      setTrash(t);
    } catch {
      setTrash([]);
    }
  }

  async function restoreFromTrash(id: string) {
    try {
      await api.restoreDocument(id);
      await refresh();
      toast.success("Restored");
    } catch (err) {
      toast.error(toastMessageFor(err) || "Couldn't restore that document.");
    }
  }

  useEffect(() => {
    if (authLoading) return;
    if (!user) {
      setDocs([]);
      return;
    }
    refresh();
  }, [user, authLoading]);

  async function addFromURL(e: React.FormEvent) {
    e.preventDefault();
    if (!url.trim()) return;
    setBusy(true);
    setError(null);
    try {
      const result = await api.createFromURL(url.trim());
      // Self-doc redirect: the user pasted one of our own doc URLs;
      // navigate to it instead of cloning the SPA HTML.
      if ("kind" in result && result.kind === "self_doc_redirect") {
        navigate(result.redirect);
        return;
      }
      navigate(`/d/${(result as { id: string }).id}`);
    } catch (err) {
      // not_markdown errors get a dedicated, more explanatory dialog so
      // users understand what went wrong (especially after pasting
      // google.com or similar).
      if (err instanceof APIError && err.kind === "not_markdown") {
        await dialog.alert({
          title: "That doesn't look like a markdown file",
          body:
            "markupmarkdown is for commenting on `.md` documents — not for editing arbitrary web pages.\n\n" +
            "Try a raw .md URL: e.g. a GitHub raw file, a docs site that serves Markdown, or upload a local file below.",
          confirmLabel: "Got it",
        });
      } else {
        setErrFrom(err);
      }
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
      body: `Delete "${title}" and all its comments? You can restore it from Trash for 30 days.`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteDocument(id);
      refresh();
      toast.success("Moved to Trash");
    } catch (err) {
      toast.error(toastMessageFor(err) || "Couldn't delete the document.");
    }
  }

  return (
    <div className="max-w-4xl mx-auto px-6 py-10">
      <h1 className="text-3xl font-semibold tracking-tight mb-2">
        Google Docs for Markdown — edit, comment, and ship <code className="bg-soft px-1 rounded">.md</code> files
      </h1>
      <p className="text-muted mb-8">
        Paste a GitHub URL or upload a{" "}
        <code className="bg-soft px-1 rounded">.md</code> file. Drag-select
        text for margin comments, or click <em>Edit</em> for a native
        markdown editor with formatting toolbar, find &amp; replace, and
        live preview. Push your changes back to GitHub as a pull request
        or direct commit. Threaded replies, @-mentions, realtime sync, AI
        revision via Claude, and an MCP server so agents review alongside
        humans.
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

      {authLoading ? (
        <div className="mt-8 text-muted text-sm">Loading…</div>
      ) : !user ? (
        <div className="mt-8 border border-rule rounded-lg bg-card p-6 text-center">
          <h2 className="text-lg font-semibold mb-1">
            Sign in to see your documents
          </h2>
          <p className="text-sm text-muted mb-4">
            Once you sign in, this is where you'll find the docs you've
            created or commented on. We only show files you've worked on
            — and we automatically hide private docs you've lost GitHub
            access to.
          </p>
          {githubEnabled ? (
            <a
              href={loginURL("/")}
              className="inline-flex items-center gap-2 px-4 py-2 rounded bg-tip-bg text-tip-fg text-sm font-medium hover:opacity-90"
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
                <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2c-3.2.7-3.87-1.36-3.87-1.36-.52-1.33-1.27-1.69-1.27-1.69-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.02 1.75 2.68 1.24 3.34.95.1-.74.4-1.24.72-1.53-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.04 0 0 .96-.31 3.15 1.18a10.96 10.96 0 0 1 5.74 0c2.19-1.49 3.15-1.18 3.15-1.18.63 1.58.23 2.75.12 3.04.73.81 1.18 1.84 1.18 3.1 0 4.43-2.69 5.41-5.25 5.69.41.36.78 1.06.78 2.13v3.16c0 .31.21.67.8.56C20.21 21.39 23.5 17.08 23.5 12 23.5 5.65 18.35.5 12 .5z" />
              </svg>
              Sign in with GitHub
            </a>
          ) : (
            <p className="text-xs text-faint">
              GitHub sign-in isn't configured on this server.
            </p>
          )}
        </div>
      ) : null}

      {/* Marketing prose for unauthenticated visitors — also doubles as
          the visible content most SEO signals reward. Authenticated
          users see this below their own docs (it's short). */}
      {!authLoading && !user && <MarketingSections />}

      {user && (
        <>
          <h2 className="text-lg font-semibold mb-3 mt-8">Your documents</h2>
          {docs === null ? (
            <div className="text-muted">Loading…</div>
          ) : docs.length === 0 ? (
            <div className="text-muted text-sm border border-rule rounded-lg bg-card p-6">
              You haven't worked on any documents yet. Open one above to get
              started, or paste a URL and leave a comment.
            </div>
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
                  {d.revisionCount && d.revisionCount > 1 && (
                    <span
                      className="inline-flex items-center gap-1 text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded bg-accent-soft text-accent shrink-0"
                      title={`AI-revised · ${d.revisionCount} versions in this chain (the link opens the most recent)`}
                    >
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                        <path d="M3 12a9 9 0 0 1 9-9 9 9 0 0 1 6.7 3" />
                        <polyline points="21 3 21 9 15 9" />
                      </svg>
                      v{d.revisionCount}
                    </span>
                  )}
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
          {trash && trash.length > 0 && (
            <div className="mt-8">
              <button
                onClick={() => setShowTrash((v) => !v)}
                className="text-xs text-muted hover:text-ink flex items-center gap-1"
              >
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ transform: showTrash ? "rotate(90deg)" : "" }}>
                  <polyline points="9 18 15 12 9 6" />
                </svg>
                Trash ({trash.length})
              </button>
              {showTrash && (
                <ul className="mt-2 divide-y divide-rule border border-rule rounded-lg bg-card">
                  {trash.map((t) => (
                    <li
                      key={t.id}
                      className="flex items-center justify-between px-4 py-2.5"
                    >
                      <div className="min-w-0 text-sm">
                        <div className="text-ink truncate">{t.title}</div>
                        <div className="text-[11px] text-muted">
                          Deleted {formatRelative(t.deletedAt)} ·{" "}
                          {t.daysLeft > 0
                            ? `purged in ${t.daysLeft} day${t.daysLeft === 1 ? "" : "s"}`
                            : "scheduled for purge"}
                        </div>
                      </div>
                      <button
                        onClick={() => restoreFromTrash(t.id)}
                        className="text-xs text-accent hover:underline ml-4"
                      >
                        Restore
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}

// MarketingSections renders the SEO-resonant prose shown to visitors
// who haven't signed in yet (and serves double duty as the visible
// content crawlers reward). Section order mirrors the FAQPage schema
// the backend injects so the on-page Q&As reinforce the structured
// data.
function MarketingSections() {
  return (
    <section className="mt-12 space-y-12">
      <div>
        <h2 className="text-xl font-semibold mb-2">
          Built for PRDs, RFCs, release notes, and prompt libraries
        </h2>
        <p className="text-muted">
          Markdown is where a lot of real product thinking lives — but the
          tools for reviewing <em>and editing</em> it are miserable. GitHub
          PRs force every discussion through a code-review workflow. Pasting
          into Google Docs drops your formatting and creates a second source
          of truth. Markupmarkdown brings Google-Docs-style margin comments
          and a native markdown editor directly to your{" "}
          <code className="bg-soft px-1 rounded">.md</code> files — and
          because edits happen on the actual markdown (not a visual mirror),
          the file in your repo stays the source of truth. One click pushes
          your revision back to GitHub as a pull request or a direct commit.
        </p>
      </div>

      <div>
        <h2 className="text-xl font-semibold mb-2">
          A real markdown editor, with GitHub round-trip built in
        </h2>
        <p className="text-muted">
          Click <em>Edit</em> for a CodeMirror 6 editor with syntax
          highlighting, a formatting toolbar (bold, italic, code, headings,
          lists, links, code blocks, blockquote, HR), find &amp; replace with
          regex, light/dark theme, and a live side-by-side preview.
          <code className="bg-soft px-1 rounded">⌘S</code> saves your changes
          as a new revision so the version history forms a tree. Comments
          stay anchored to their text spans as you edit. When you're done,
          click <em>Push to GitHub</em> to open a pull request from a new
          branch (prefilled title and body) or commit directly to a branch
          you pick — branch-protection rules are enforced on GitHub's side
          and surfaced verbatim if they reject the push.
        </p>
      </div>

      <div>
        <h2 className="text-xl font-semibold mb-2">
          Humans and AI agents review the same documents
        </h2>
        <p className="text-muted">
          Markupmarkdown ships an open{" "}
          <a
            href="/SKILL.md"
            className="text-accent hover:underline"
          >
            Model Context Protocol server
          </a>{" "}
          so AI agents read what humans read, leave threads humans can
          approve, and apply resolved feedback as new revisions — with
          explicit human sign-off. Agent comments carry a visible bot badge.
          The same access checks, rate limits, and validation apply to MCP
          and REST — no agent-only fast path.
        </p>
      </div>

      <div>
        <h2 className="text-xl font-semibold mb-2">
          Open source, self-hosted, bring your own AI key
        </h2>
        <p className="text-muted">
          Everything is{" "}
          <a
            href="https://github.com/jonradoff/markupmarkdown"
            className="text-accent hover:underline"
          >
            MIT-licensed on GitHub
          </a>
          . One Go binary, a React SPA, MongoDB — designed to deploy on a
          single Fly.io machine. AI revision uses your own Anthropic API
          key, stored AES-256-GCM encrypted at rest and deletable any time.
          Your usage, your bill, your data.
        </p>
      </div>

      <div>
        <h2 className="text-xl font-semibold mb-4">Frequently asked</h2>
        <dl className="space-y-5">
          <div>
            <dt className="font-medium text-ink">How do I comment on a Markdown file?</dt>
            <dd className="text-muted mt-1">
              Paste the URL of any <code className="bg-soft px-1 rounded">.md</code>{" "}
              file (raw or a <code className="bg-soft px-1 rounded">github.com/.../blob/.../*.md</code>{" "}
              link) or upload a local file. Drag-select text in the rendered
              document and click the Comment button that floats next to your
              selection. Threaded replies, @-mentions, mark-as-done, and
              reopen are one click each.
            </dd>
          </div>
          <div>
            <dt className="font-medium text-ink">Can I edit the markdown directly?</dt>
            <dd className="text-muted mt-1">
              Yes. Click <em>Edit</em> and you get a CodeMirror 6 editor with
              syntax highlighting, a sticky formatting toolbar (bold, italic,
              code, headings, lists, task lists, blockquote, link, code
              block, HR), find &amp; replace with regex, light/dark theme,
              and a live preview.{" "}
              <code className="bg-soft px-1 rounded">⌘S</code> saves a new
              revision; comments stay anchored as you edit.
            </dd>
          </div>
          <div>
            <dt className="font-medium text-ink">Can I push my edits back to GitHub?</dt>
            <dd className="text-muted mt-1">
              Yes. For docs cloned from a GitHub blob URL, click{" "}
              <em>Push to GitHub</em>. Choose between opening a pull request
              from a new branch (with prefilled title and body) or committing
              directly to a branch like <code className="bg-soft px-1 rounded">main</code>.
              The OAuth token from your sign-in does the work — no separate
              GitHub PAT needed.
            </dd>
          </div>
          <div>
            <dt className="font-medium text-ink">
              Can I review Markdown files from private GitHub repos?
            </dt>
            <dd className="text-muted mt-1">
              Yes. Sign in with GitHub and you can open files from any repo
              you have read access to. Private docs are gated on every read
              by re-verifying your GitHub access to the source repo — losing
              access means you stop seeing content (and the title)
              immediately.
            </dd>
          </div>
          <div>
            <dt className="font-medium text-ink">How does AI revision work?</dt>
            <dd className="text-muted mt-1">
              Resolve the comments you want applied, click <em>Revise with AI</em>,
              and Claude Opus 4.7 produces a new revision that incorporates
              the resolved feedback while changing as little of the rest as
              possible. The output streams as rendered Markdown; you get a
              word-level diff before accepting. Saving creates a new child
              document so revisions form a tree.
            </dd>
          </div>
          <div>
            <dt className="font-medium text-ink">What is the MCP server for?</dt>
            <dd className="text-muted mt-1">
              The Model Context Protocol server at{" "}
              <code className="bg-soft px-1 rounded">/mcp</code> lets AI
              agents (Claude Desktop, Claude Code, custom MCP clients) read
              documents, leave threads anchored to text spans, reply to
              humans, resolve threads, and trigger AI revisions. Agents
              authenticate via per-user personal access tokens; the same
              access checks and rate limits apply as the REST API.
            </dd>
          </div>
          <div>
            <dt className="font-medium text-ink">Is markupmarkdown free?</dt>
            <dd className="text-muted mt-1">
              Yes. MIT-licensed open source. Self-host it on Fly.io with one
              command, or use the hosted demo at{" "}
              <a href="https://mumd.metavert.io/" className="text-accent hover:underline">
                mumd.metavert.io
              </a>
              . Bring your own Anthropic API key for AI revision.
            </dd>
          </div>
        </dl>
      </div>
    </section>
  );
}
