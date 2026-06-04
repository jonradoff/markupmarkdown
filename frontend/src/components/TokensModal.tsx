import { useEffect, useState } from "react";
import Modal from "./Modal";
import { api } from "../api";
import type { APIToken, CreatedTokenResponse } from "../types";
import { formatRelative } from "../utils/format";
import { useDialog } from "./Dialogs";

interface Props {
  onClose: () => void;
}

export default function TokensModal({ onClose }: Props) {
  const dialog = useDialog();
  const [tokens, setTokens] = useState<APIToken[] | null>(null);
  const [label, setLabel] = useState("");
  const [isAgent, setIsAgent] = useState(true);
  const [created, setCreated] = useState<CreatedTokenResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

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
      const out = await api.createToken(label.trim(), isAgent);
      setCreated(out);
      setLabel("");
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
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
        Use these to authenticate scripts and agents against the markupmarkdown
        REST and MCP APIs. Mark a token as <strong>for an agent</strong> to
        tag every comment/reply it creates with an agent badge so humans can
        tell who's who in a thread.
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
        <div className="flex flex-col gap-2 mb-4">
          <input
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="Label (e.g. claude-code-laptop)"
            className="text-sm border border-rule rounded px-3 py-2 focus:outline-none focus:border-accent bg-card text-ink"
          />
          <label className="text-xs text-muted flex items-center gap-2">
            <input
              type="checkbox"
              checked={isAgent}
              onChange={(e) => setIsAgent(e.target.checked)}
              className="accent-accent"
            />
            This token is for an agent (tags its contributions with a bot badge)
          </label>
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
        <ul className="border border-rule rounded divide-y divide-rule bg-card max-h-48 overflow-auto">
          {tokens.map((t) => (
            <li
              key={t.id}
              className="flex items-center justify-between px-3 py-2 text-sm"
            >
              <div className="min-w-0">
                <div className="font-medium text-ink flex items-center gap-2">
                  {t.label}
                  {t.isAgent && (
                    <span className="text-[10px] uppercase tracking-wide bg-accent-soft text-accent rounded px-1.5 py-0.5">
                      agent
                    </span>
                  )}
                </div>
                <div className="text-[11px] text-muted font-mono">
                  {t.prefix}
                </div>
                <div className="text-[11px] text-faint">
                  Created {formatRelative(t.createdAt)}
                  {t.lastUsedAt && ` · last used ${formatRelative(t.lastUsedAt)}`}
                </div>
              </div>
              <button
                onClick={() => revoke(t)}
                className="text-xs text-danger hover:underline"
              >
                Revoke
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <div className="text-xs text-muted">No tokens yet.</div>
      )}

      {error && <div className="text-xs text-danger mt-3">{error}</div>}

      <div className="text-[11px] text-faint mt-3 leading-relaxed">
        See <a href="/SKILL.md" target="_blank" rel="noreferrer" className="text-accent hover:underline">SKILL.md</a>{" "}
        for how to wire a token into an MCP-aware agent.
      </div>
    </Modal>
  );
}
