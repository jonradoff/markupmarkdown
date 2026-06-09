import { useEffect, useState } from "react";
import { api, APIError } from "../api";
import type { MdDocument, PushbackInfo, PushbackResult } from "../types";
import ErrorBlock from "./ErrorBlock";
import { useToast } from "./Toast";

interface Props {
  doc: MdDocument;
  onClose: () => void;
  onPushed: (result: PushbackResult) => void;
}

type Mode = "pr" | "direct";

// PushbackModal lets the user push the doc's current content back to
// its source GitHub repo. Two flavors offered side-by-side: open a
// PR from a new branch (safer, the default), or commit directly to
// the target branch (faster, requires push permission + no branch
// protection rules blocking the user). Pros/cons are shown next to
// each radio so the user can pick per-save, not per-doc.
export default function PushbackModal({ doc, onClose, onPushed }: Props) {
  const [loading, setLoading] = useState(true);
  const [info, setInfo] = useState<PushbackInfo | null>(null);
  const [error, setError] = useState<APIError | null>(null);

  const [mode, setMode] = useState<Mode>("pr");
  const [branch, setBranch] = useState("");
  const [commitMessage, setCommitMessage] = useState("");
  const [prTitle, setPRTitle] = useState("");
  const [prBody, setPRBody] = useState("");
  const [targetBranch, setTargetBranch] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const toast = useToast();

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await api.pushbackInfo(doc.id);
        if (cancelled) return;
        setInfo(data);
        setBranch(data.suggestedBranch);
        setCommitMessage(data.suggestedMessage);
        setPRTitle(data.suggestedPRTitle);
        setPRBody(data.suggestedPRBody);
        setTargetBranch(data.defaultBranch);
        // Default mode honors permissions: if direct isn't allowed,
        // force PR.
        if (!data.canPushDirect) setMode("pr");
      } catch (err) {
        if (cancelled) return;
        setError(
          err instanceof APIError
            ? err
            : new APIError((err as Error).message || "Couldn't load repo info")
        );
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [doc.id]);

  async function handleSubmit() {
    if (!info || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const result = await api.pushback(doc.id, {
        mode,
        branch: mode === "pr" ? branch : undefined,
        commitMessage,
        targetBranch,
        prTitle: mode === "pr" ? prTitle : undefined,
        prBody: mode === "pr" ? prBody : undefined,
      });
      onPushed(result);
      onClose();
      if (result.mode === "pr") {
        toast.success(
          `PR #${result.prNumber} opened on ${info.owner}/${info.repo}`
        );
      } else {
        toast.success(`Committed to ${result.branch} on ${info.owner}/${info.repo}`);
      }
    } catch (err) {
      setError(
        err instanceof APIError
          ? err
          : new APIError((err as Error).message || "Couldn't push to GitHub")
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="fixed inset-0 z-40 bg-black/40 flex items-center justify-center p-4">
      <div className="bg-card border border-rule rounded-lg shadow-xl max-w-2xl w-full max-h-[90vh] flex flex-col overflow-hidden">
        <div className="px-5 py-3 border-b border-rule flex items-center justify-between shrink-0">
          <h2 className="text-lg font-semibold">Push to GitHub</h2>
          <button
            onClick={onClose}
            disabled={submitting}
            className="text-muted hover:text-ink text-sm"
          >
            ✕
          </button>
        </div>

        <div className="flex-1 min-h-0 overflow-auto p-5 space-y-4 text-sm">
          {loading && (
            <div className="text-muted">Checking repo permissions…</div>
          )}

          {error && <ErrorBlock error={error} onDismiss={() => setError(null)} />}

          {info && (
            <>
              <div className="text-xs text-muted">
                Pushing{" "}
                <code className="bg-soft px-1 rounded">{info.path}</code> to{" "}
                <a
                  href={info.repoHtmlUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="text-accent hover:underline"
                >
                  {info.owner}/{info.repo}
                </a>
              </div>

              {/* Mode selector — radio cards with pros/cons per option. */}
              <fieldset className="space-y-2">
                <legend className="font-medium mb-1">How do you want to push?</legend>

                <label
                  className={`block border rounded-lg p-3 cursor-pointer transition ${
                    mode === "pr" ? "border-accent ring-2 ring-accent/20" : "border-rule"
                  }`}
                >
                  <div className="flex items-start gap-3">
                    <input
                      type="radio"
                      name="pushback-mode"
                      checked={mode === "pr"}
                      onChange={() => setMode("pr")}
                      className="mt-1"
                    />
                    <div className="flex-1">
                      <div className="font-medium">Open a pull request (recommended)</div>
                      <ul className="text-xs text-muted mt-1 space-y-0.5 list-disc pl-4">
                        <li>Commits to a new branch and opens a PR against <code className="bg-soft px-1 rounded">{targetBranch}</code>.</li>
                        <li>Lets reviewers and CI run before anything ships.</li>
                        <li>Easy to revise — push more commits to the same branch.</li>
                      </ul>
                    </div>
                  </div>
                </label>

                <label
                  className={`block border rounded-lg p-3 transition ${
                    !info.canPushDirect
                      ? "border-rule opacity-50 cursor-not-allowed"
                      : mode === "direct"
                        ? "border-accent ring-2 ring-accent/20 cursor-pointer"
                        : "border-rule cursor-pointer"
                  }`}
                >
                  <div className="flex items-start gap-3">
                    <input
                      type="radio"
                      name="pushback-mode"
                      checked={mode === "direct"}
                      onChange={() => setMode("direct")}
                      disabled={!info.canPushDirect}
                      className="mt-1"
                    />
                    <div className="flex-1">
                      <div className="font-medium">Commit directly to {targetBranch}</div>
                      <ul className="text-xs text-muted mt-1 space-y-0.5 list-disc pl-4">
                        <li>Ships immediately — no review step.</li>
                        <li>Best for small fixes you'd otherwise approve yourself.</li>
                        <li>
                          {info.canPushDirect ? (
                            <>If <code className="bg-soft px-1 rounded">{targetBranch}</code> has branch protection rules, GitHub may still reject the commit — that's a repo-admin decision, not ours.</>
                          ) : (
                            <>Disabled: your GitHub permissions on this repo don't include push. The repo owner can change that.</>
                          )}
                        </li>
                      </ul>
                    </div>
                  </div>
                </label>
              </fieldset>

              {mode === "pr" && (
                <div className="space-y-3">
                  <label className="block">
                    <div className="text-xs text-muted mb-1">Branch name</div>
                    <input
                      type="text"
                      value={branch}
                      onChange={(e) => setBranch(e.target.value)}
                      className="w-full text-sm font-mono px-2 py-1 border border-rule rounded focus:outline-none focus:border-accent"
                    />
                  </label>
                  <label className="block">
                    <div className="text-xs text-muted mb-1">PR title</div>
                    <input
                      type="text"
                      value={prTitle}
                      onChange={(e) => setPRTitle(e.target.value)}
                      className="w-full text-sm px-2 py-1 border border-rule rounded focus:outline-none focus:border-accent"
                    />
                  </label>
                  <label className="block">
                    <div className="text-xs text-muted mb-1">PR description (Markdown ok)</div>
                    <textarea
                      value={prBody}
                      onChange={(e) => setPRBody(e.target.value)}
                      rows={4}
                      className="w-full text-sm font-mono px-2 py-1 border border-rule rounded focus:outline-none focus:border-accent resize-y"
                    />
                  </label>
                </div>
              )}

              <label className="block">
                <div className="text-xs text-muted mb-1">Commit message</div>
                <input
                  type="text"
                  value={commitMessage}
                  onChange={(e) => setCommitMessage(e.target.value)}
                  className="w-full text-sm px-2 py-1 border border-rule rounded focus:outline-none focus:border-accent"
                />
              </label>
            </>
          )}
        </div>

        {info && (
          <div className="px-5 py-3 border-t border-rule flex items-center justify-end gap-2 shrink-0">
            <button
              onClick={onClose}
              disabled={submitting}
              className="text-sm px-3 py-1.5 rounded text-muted hover:text-ink"
            >
              Cancel
            </button>
            <button
              onClick={handleSubmit}
              disabled={submitting || !commitMessage.trim() || (mode === "pr" && !branch.trim())}
              className="text-sm px-4 py-1.5 rounded bg-accent text-accent-fg font-medium hover:opacity-90 disabled:opacity-50"
            >
              {submitting
                ? mode === "pr"
                  ? "Opening PR…"
                  : "Committing…"
                : mode === "pr"
                  ? "Open PR"
                  : `Commit to ${targetBranch}`}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
