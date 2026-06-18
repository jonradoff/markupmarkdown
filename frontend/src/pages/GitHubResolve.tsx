import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, APIError } from "../api";
import {
  gistURLFor,
  githubURLForBlob,
  githubURLForOwner,
  githubURLForRepo,
  isReservedTopPath,
} from "../utils/canonicalUrl";
import ErrorBlock from "../components/ErrorBlock";

// GitHubResolve handles the three human-readable URL shapes the SPA
// now accepts as a first-class language for accessing GitHub markdown:
//
//   /:owner                              → user / org index
//   /:owner/:repo                        → repo index
//   /:owner/:repo/blob/:ref/<path>       → individual markdown document
//
// For docs it tries to resolve to an existing chain leaf via the
// /api/documents/by-source endpoint first; on 404 it falls back to
// the standard createFromURL flow (which ingests + clones). Either
// way it ends up with a doc id which it navigates to via /d/:id; the
// DocumentPage's canonicalizer will then replaceState back to the
// human URL so the user's address bar reads as they typed it.
//
// For indexes it calls createIndex (which dedupes per-creator
// server-side) and navigates to /i/:id; IndexPage canonicalizes
// the same way.
//
// We're intentionally NOT inlining DocumentPage / IndexPage here —
// having two routes for the same component would make every page-level
// concern (open-graph injection, browser title, scroll restoration)
// branchy. Better to centralize at the /d/:id and /i/:id render and
// let the canonicalizer maintain the human URL.
type Mode = "owner" | "repo" | "doc" | "gist";

interface Props {
  mode: Mode;
}

export default function GitHubResolve({ mode }: Props) {
  const navigate = useNavigate();
  const params = useParams();
  const [error, setError] = useState<APIError | null>(null);
  const [status, setStatus] = useState<string>("Resolving…");

  useEffect(() => {
    let cancelled = false;
    async function go() {
      const owner = params.owner ?? "";
      const repo = params.repo ?? "";
      const ref = params.ref ?? "main";
      // React Router exposes the catch-all match as params["*"]; we
      // typed it loosely because the resolver pages are shared.
      const path = (params as Record<string, string | undefined>)["*"] ?? "";
      if (!owner || isReservedTopPath(owner)) {
        setError(new APIError("Not found", { kind: "not_found" }));
        return;
      }

      try {
        if (mode === "doc") {
          if (!repo || !path) {
            setError(new APIError("Bad GitHub URL — repo and file path are required", { kind: "bad_request" }));
            return;
          }
          setStatus(`Looking for ${owner}/${repo}/${path}…`);
          // Try to resolve to an existing doc first so two viewers
          // pasting the same URL land on the same place (comments
          // aggregate instead of fracturing).
          try {
            const existing = await api.findDocBySource({ owner, repo, ref, path });
            if (cancelled) return;
            navigate(`/d/${existing.id}`, { replace: true });
            return;
          } catch (err) {
            if (err instanceof APIError && err.kind !== "not_found") {
              throw err;
            }
            // 404 — no existing doc. Fall through to ingest.
          }
          setStatus(`Cloning ${path} from GitHub…`);
          const ghURL = githubURLForBlob(owner, repo, ref, path);
          const res = await api.createFromURL(ghURL);
          if (cancelled) return;
          if ("kind" in res && res.kind === "self_doc_redirect") {
            navigate(res.redirect, { replace: true });
          } else {
            navigate(`/d/${(res as { id: string }).id}`, { replace: true });
          }
        } else if (mode === "gist") {
          // Canonical gist URL — explicit. The repo branch below
          // also reaches this codepath as a fallback when GitHub
          // answers 404, but only when the user typed the github-
          // mirror URL (no `gist/` prefix).
          const gistId = params.gistId ?? "";
          if (!gistId) {
            setError(new APIError("Bad gist URL — owner and id are required", { kind: "bad_request" }));
            return;
          }
          setStatus(`Loading gist ${owner}/${gistId}…`);
          const res = await api.createFromURL(gistURLFor(owner, gistId));
          if (cancelled) return;
          if ("kind" in res && res.kind === "self_doc_redirect") {
            navigate(res.redirect, { replace: true });
          } else {
            navigate(`/d/${(res as { id: string }).id}`, { replace: true });
          }
        } else if (mode === "repo") {
          // Try as repo first; fall back to gist on 404. Preserves
          // the URL-swap trick (mumd.metavert.io/<owner>/<seg>
          // works for both github.com/<owner>/<repo> and
          // gist.github.com/<owner>/<id>) without depending on a
          // fragile hex regex. Real repos pay the same one round-
          // trip as today; gists pay two.
          setStatus(`Indexing ${owner}/${repo}…`);
          try {
            const idx = await api.createIndex(githubURLForRepo(owner, repo));
            if (cancelled) return;
            navigate(`/i/${idx.id}`, { replace: true });
            return;
          } catch (err) {
            // Only fall back on a definite "no such thing" — other
            // errors (auth, rate limit) should bubble.
            const isMissing =
              err instanceof APIError &&
              (err.kind === "not_found" ||
                /not found/i.test(err.message));
            if (!isMissing) throw err;
          }
          setStatus(`Not a repo — trying as a gist…`);
          try {
            const res = await api.createFromURL(gistURLFor(owner, repo));
            if (cancelled) return;
            if ("kind" in res && res.kind === "self_doc_redirect") {
              navigate(res.redirect, { replace: true });
            } else {
              navigate(`/d/${(res as { id: string }).id}`, { replace: true });
            }
          } catch (gistErr) {
            const isMissing =
              gistErr instanceof APIError &&
              (gistErr.kind === "not_found" ||
                /not found/i.test(gistErr.message));
            if (isMissing) {
              setError(new APIError(
                `No repo or gist at ${owner}/${repo} on GitHub.`,
                { kind: "not_found" },
              ));
              return;
            }
            throw gistErr;
          }
        } else {
          setStatus(`Looking up ${owner} on GitHub…`);
          const idx = await api.createIndex(githubURLForOwner(owner));
          if (cancelled) return;
          navigate(`/i/${idx.id}`, { replace: true });
        }
      } catch (err) {
        if (cancelled) return;
        if (err instanceof APIError) {
          setError(err);
        } else {
          setError(new APIError((err as Error).message || "Couldn't resolve URL"));
        }
      }
    }
    void go();
    return () => {
      cancelled = true;
    };
  }, [mode, params, navigate]);

  if (error) {
    return (
      <div className="max-w-4xl mx-auto px-6 py-10">
        <ErrorBlock error={error} />
        <div className="mt-4 text-sm">
          <Link to="/" className="text-accent hover:underline">
            ← Back home
          </Link>
        </div>
      </div>
    );
  }
  return (
    <div className="max-w-4xl mx-auto px-6 py-10">
      <div className="rounded-lg border border-rule bg-card p-4 flex items-center gap-3">
        <span
          aria-hidden
          className="inline-block w-3 h-3 border-2 border-accent border-t-transparent rounded-full animate-spin shrink-0"
        />
        <span className="text-sm text-ink">{status}</span>
      </div>
    </div>
  );
}
