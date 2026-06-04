import { useEffect, useState } from "react";
import Modal from "./Modal";
import { api } from "../api";
import type {
  APIToken,
  CreatedTokenResponse,
  TokenEvent,
  TokenScope,
} from "../types";
import { formatRelative } from "../utils/format";
import { useDialog } from "./Dialogs";

interface Props {
  onClose: () => void;
}

// Expiration choices the UI offers. The "never" option uses -1 (server
// convention for "no expiration") so a user who explicitly opts out can.
const expiryChoices: { label: string; days: number }[] = [
  { label: "30 days", days: 30 },
  { label: "90 days", days: 90 },
  { label: "365 days", days: 365 },
  { label: "Never expires", days: -1 },
];

const scopeChoices: { value: TokenScope; label: string; hint: string }[] = [
  { value: "read", label: "Read", hint: "List docs, read comments. No writes." },
  {
    value: "write",
    label: "Write",
    hint:
      "Read + add comments / replies, resolve threads, preview AI revisions. Cannot delete docs or accept revisions.",
  },
  {
    value: "admin",
    label: "Admin",
    hint:
      "Full account access: write + delete docs, accept AI revisions (creates a new doc).",
  },
];

export default function TokensModal({ onClose }: Props) {
  const dialog = useDialog();
  const [tokens, setTokens] = useState<APIToken[] | null>(null);
  const [label, setLabel] = useState("");
  const [scope, setScope] = useState<TokenScope>("write");
  const [expiresInDays, setExpiresInDays] = useState<number>(90);
  const [created, setCreated] = useState<CreatedTokenResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editLabel, setEditLabel] = useState("");
  const [activityFor, setActivityFor] = useState<string | null>(null);
  const [activity, setActivity] = useState<TokenEvent[] | null>(null);

  async function refresh() {
    try {
      setTokens(await api.listTokens());
    } catch (e) {
      setError((e as Error).message);
    }
  }
  useEffect(() => {
    refresh();
  }, []);

  async function generate() {
    if (!label.trim() || busy) return;
    setBusy(true);
    setError(null);
    try {
      const out = await api.createToken({
        label: label.trim(),
        scope,
        expiresInDays,
      });
      setCreated(out);
      setLabel("");
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function saveLabel(id: string) {
    if (!editLabel.trim()) return;
    try {
      await api.updateToken(id, { label: editLabel.trim() });
      setEditingId(null);
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function changeScope(id: string, next: TokenScope) {
    try {
      await api.updateToken(id, { scope: next });
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function revoke(t: APIToken) {
    const ok = await dialog.confirm({
      title: "Revoke token?",
      body: `Revoke "${t.label}"? Any agent using it will immediately lose access.`,
      confirmLabel: "Revoke",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.revokeToken(t.id);
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function viewActivity(id: string) {
    setActivityFor(id);
    setActivity(null);
    try {
      const events = await api.tokenActivity(id);
      setActivity(events);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
    } catch {}
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  }

  return (
    <Modal title="Personal access tokens" onClose={onClose}>
      <p className="text-xs text-muted mb-3 leading-relaxed">
        Tokens authenticate scripts and agents against the markupmarkdown REST
        and MCP APIs. The token's <strong>label</strong> is the identity its
        comments and replies appear under — your underlying account is shown
        on hover so humans can see who's accountable.
      </p>

      {created ? (
        <div className="bg-soft border border-rule rounded p-3 mb-3">
          <div className="text-xs font-medium text-ink mb-1">
            Your new token (shown once)
          </div>
          <div className="flex items-center gap-2">
            <code className="flex-1 font-mono text-xs bg-card border border-rule rounded px-2 py-1 overflow-x-auto whitespace-nowrap">
              {created.token}
            </code>
            <button
              onClick={() => copy(created.token)}
              className="text-xs px-2 py-1 rounded bg-accent text-accent-fg font-medium"
            >
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
          <button
            onClick={() => setCreated(null)}
            className="text-xs text-muted hover:text-ink mt-2"
          >
            I've saved it
          </button>
        </div>
      ) : (
        <div className="flex flex-col gap-3 mb-4">
          <input
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") generate();
            }}
            placeholder="Label (e.g. claude-code, ci-bot, my-script)"
            className="text-sm border border-rule rounded px-3 py-2 focus:outline-none focus:border-accent bg-card text-ink"
          />
          <div>
            <div className="text-[11px] font-semibold uppercase tracking-wide text-muted mb-1">
              Scope
            </div>
            <div className="flex flex-col gap-1">
              {scopeChoices.map((c) => (
                <label
                  key={c.value}
                  className="flex items-start gap-2 text-xs text-ink cursor-pointer"
                >
                  <input
                    type="radio"
                    name="newTokenScope"
                    checked={scope === c.value}
                    onChange={() => setScope(c.value)}
                    className="mt-0.5"
                  />
                  <span>
                    <span className="font-medium">{c.label}</span>{" "}
                    <span className="text-muted">— {c.hint}</span>
                  </span>
                </label>
              ))}
            </div>
          </div>
          <div>
            <div className="text-[11px] font-semibold uppercase tracking-wide text-muted mb-1">
              Expires
            </div>
            <select
              value={expiresInDays}
              onChange={(e) => setExpiresInDays(Number(e.target.value))}
              className="text-sm border border-rule rounded px-2 py-1 bg-card text-ink"
            >
              {expiryChoices.map((c) => (
                <option key={c.days} value={c.days}>
                  {c.label}
                </option>
              ))}
            </select>
          </div>
          <button
            onClick={generate}
            disabled={!label.trim() || busy}
            className="self-end text-sm px-4 py-2 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
          >
            {busy ? "Generating…" : "Generate token"}
          </button>
        </div>
      )}

      <div className="text-xs font-semibold uppercase tracking-wide text-muted mb-2">
        Active tokens ({tokens?.length ?? 0})
      </div>
      {tokens && tokens.length > 0 ? (
        <ul className="border border-rule rounded divide-y divide-rule bg-card max-h-64 overflow-auto">
          {tokens.map((t) => (
            <li
              key={t.id}
              className="flex items-center justify-between px-3 py-2 text-sm gap-2"
            >
              <div className="min-w-0 flex-1">
                {editingId === t.id ? (
                  <div className="flex items-center gap-2">
                    <input
                      autoFocus
                      value={editLabel}
                      onChange={(e) => setEditLabel(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") saveLabel(t.id);
                        if (e.key === "Escape") setEditingId(null);
                      }}
                      className="flex-1 text-sm border border-rule rounded px-2 py-1 focus:outline-none focus:border-accent bg-card text-ink"
                    />
                    <button
                      onClick={() => saveLabel(t.id)}
                      className="text-xs px-2 py-1 rounded bg-accent text-accent-fg"
                    >
                      Save
                    </button>
                    <button
                      onClick={() => setEditingId(null)}
                      className="text-xs text-muted hover:text-ink"
                    >
                      Cancel
                    </button>
                  </div>
                ) : (
                  <>
                    <div className="font-medium text-ink flex items-center gap-2">
                      {t.label}
                      <select
                        value={t.scope}
                        onChange={(e) =>
                          changeScope(t.id, e.target.value as TokenScope)
                        }
                        className="text-[11px] border border-rule rounded px-1 py-0.5 bg-card text-muted"
                        title="Change scope"
                      >
                        {scopeChoices.map((c) => (
                          <option key={c.value} value={c.value}>
                            {c.label.toLowerCase()}
                          </option>
                        ))}
                      </select>
                    </div>
                    <div className="text-[11px] text-muted font-mono">
                      {t.prefix}
                    </div>
                    <div className="text-[11px] text-faint">
                      Created {formatRelative(t.createdAt)}
                      {t.lastUsedAt && ` · last used ${formatRelative(t.lastUsedAt)}`}
                      {t.expiresAt
                        ? ` · expires ${formatRelative(t.expiresAt)}`
                        : " · never expires"}
                    </div>
                  </>
                )}
              </div>
              {editingId !== t.id && (
                <div className="flex items-center gap-2 shrink-0">
                  <button
                    onClick={() => viewActivity(t.id)}
                    className="text-xs text-muted hover:text-ink"
                  >
                    Activity
                  </button>
                  <button
                    onClick={() => {
                      setEditingId(t.id);
                      setEditLabel(t.label);
                    }}
                    className="text-xs text-muted hover:text-ink"
                  >
                    Rename
                  </button>
                  <button
                    onClick={() => revoke(t)}
                    className="text-xs text-danger hover:underline"
                  >
                    Revoke
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <div className="text-xs text-muted">No tokens yet.</div>
      )}

      {activityFor && (
        <div className="border border-rule rounded mt-3 p-3 bg-soft">
          <div className="flex items-center justify-between mb-2">
            <div className="text-xs font-semibold uppercase tracking-wide text-muted">
              Recent activity
            </div>
            <button
              onClick={() => {
                setActivityFor(null);
                setActivity(null);
              }}
              className="text-xs text-muted hover:text-ink"
            >
              Close
            </button>
          </div>
          {activity === null ? (
            <div className="text-xs text-muted">Loading…</div>
          ) : activity.length === 0 ? (
            <div className="text-xs text-muted">
              No activity recorded yet. Events are sampled to about one per
              minute per action.
            </div>
          ) : (
            <ul className="max-h-40 overflow-auto text-xs space-y-1">
              {activity.map((e) => (
                <li key={e.id} className="flex justify-between gap-2">
                  <span className="font-mono text-ink">{e.action}</span>
                  <span className="text-muted shrink-0">
                    {formatRelative(e.at)}
                    {e.documentId && (
                      <>
                        {" "}
                        ·{" "}
                        <a
                          href={`/d/${e.documentId}`}
                          target="_blank"
                          rel="noreferrer"
                          className="text-accent hover:underline"
                        >
                          doc
                        </a>
                      </>
                    )}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {error && <div className="text-xs text-danger mt-3">{error}</div>}

      <div className="text-[11px] text-faint mt-3 leading-relaxed">
        See <a href="/SKILL.md" target="_blank" rel="noreferrer" className="text-accent hover:underline">SKILL.md</a>{" "}
        for how to wire a token into an MCP-aware agent.
      </div>
    </Modal>
  );
}
