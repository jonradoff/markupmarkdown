import { useEffect, useState } from "react";
import Modal from "./Modal";
import { api } from "../api";
import type { AnthropicKeyStatus } from "../types";
import { formatRelative } from "../utils/format";
import { useDialog } from "./Dialogs";

interface Props {
  onClose: () => void;
  onSaved?: (status: AnthropicKeyStatus) => void;
}

export default function APIKeyModal({ onClose, onSaved }: Props) {
  const dialog = useDialog();
  const [status, setStatus] = useState<AnthropicKeyStatus | null>(null);
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [replacing, setReplacing] = useState(false);

  useEffect(() => {
    api.getAnthropicKey().then(setStatus).catch((e) => setError((e as Error).message));
  }, []);

  async function save() {
    if (!key.trim() || busy) return;
    setBusy(true);
    setError(null);
    try {
      const updated = await api.setAnthropicKey(key.trim());
      setStatus(updated);
      setKey("");
      setReplacing(false);
      onSaved?.(updated);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    const ok = await dialog.confirm({
      title: "Delete API key?",
      body: "Delete your stored API key? You'll need to enter it again to use AI revision.",
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    setError(null);
    try {
      await api.deleteAnthropicKey();
      const updated = await api.getAnthropicKey();
      setStatus(updated);
      onSaved?.(updated);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const showInput = !status?.hasKey || replacing;

  return (
    <Modal title="Anthropic API key" onClose={onClose}>
      {status?.enabled === false ? (
        <p className="text-sm text-danger">
          This server isn't configured to store API keys (missing encryption
          key). Ask the admin to set <code>MARKUPMARKDOWN_ENCRYPTION_KEY</code>.
        </p>
      ) : (
        <>
          <p className="text-sm text-muted mb-3">
            AI revision uses Claude Opus 4.7 and runs on your own Anthropic
            account, billed to your key. Get one at{" "}
            <a
              href="https://console.anthropic.com/account/keys"
              target="_blank"
              rel="noreferrer"
              className="text-accent hover:underline"
            >
              console.anthropic.com
            </a>
            .
          </p>

          {status?.hasKey && !replacing && (
            <div className="bg-soft border border-rule rounded p-3 mb-4 text-sm">
              <div className="flex items-center justify-between">
                <div>
                  <div className="font-medium text-ink">Key on file</div>
                  <div className="font-mono text-muted">{status.hint}</div>
                  {status.setAt && (
                    <div className="text-xs text-faint mt-0.5">
                      Saved {formatRelative(status.setAt)}
                    </div>
                  )}
                </div>
                <div className="flex flex-col gap-1 text-xs">
                  <button
                    onClick={() => setReplacing(true)}
                    className="text-accent hover:underline"
                  >
                    Replace
                  </button>
                  <button
                    onClick={remove}
                    disabled={busy}
                    className="text-danger hover:underline disabled:opacity-50"
                  >
                    Delete
                  </button>
                </div>
              </div>
            </div>
          )}

          {showInput && (
            <>
              <label className="text-sm font-medium block mb-1">
                Paste your key
              </label>
              <input
                autoFocus
                type="password"
                value={key}
                onChange={(e) => setKey(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") save();
                }}
                placeholder="sk-ant-api03-..."
                className="w-full text-sm border border-rule rounded px-3 py-2 focus:outline-none focus:border-accent bg-card text-ink font-mono"
              />
              <div className="flex justify-end gap-2 mt-3">
                {replacing && (
                  <button
                    onClick={() => {
                      setReplacing(false);
                      setKey("");
                    }}
                    className="text-sm px-3 py-1 text-muted hover:text-ink"
                  >
                    Cancel
                  </button>
                )}
                <button
                  onClick={save}
                  disabled={busy || !key.trim()}
                  className="text-sm px-4 py-2 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
                >
                  {busy ? "Validating…" : "Save"}
                </button>
              </div>
            </>
          )}

          <details className="mt-5 text-xs text-muted">
            <summary className="cursor-pointer select-none hover:text-ink">
              How we store this
            </summary>
            <div className="mt-2 space-y-1.5 leading-relaxed">
              <p>
                Your key is encrypted with <strong>AES-256-GCM</strong> using a
                random 12-byte nonce per encryption. The encryption key is held
                only in a server-side env var (
                <code className="font-mono">MARKUPMARKDOWN_ENCRYPTION_KEY</code>)
                set as a Fly secret — never written to MongoDB.
              </p>
              <p>
                The ciphertext lives in a separate{" "}
                <code className="font-mono">user_secrets</code> collection,
                keyed by user id. We store a 14-character hint (
                <code className="font-mono">{status?.hint || "sk-ant-api…XXXX"}</code>
                ) so you can recognize which key is on file without us ever
                exposing the rest.
              </p>
              <p>
                The plaintext key is only decrypted in memory for the duration
                of a revision request. It is never logged, never returned in
                any API response, and you can delete it any time.
              </p>
            </div>
          </details>

          {error && <div className="text-sm text-danger mt-3">{error}</div>}
        </>
      )}
    </Modal>
  );
}
