// Shared URL builders for the human-readable URL system.
//
// Two halves: (1) given a doc or index, compute its canonical
// /owner/repo/blob/ref/path URL so the browser can replaceState to it;
// (2) given a path like /:owner/:repo/blob/:ref/* picked up by the
// resolver routes, reassemble the equivalent github.com URL the
// existing createFromURL flow accepts.

import type { MarkdownIndex, MdDocument } from "../types";

// Frontend-reserved top-level paths that AREN'T owner names. The
// resolver routes return null for any of these so they fall through
// to a 404 rather than firing GitHub API calls.
const RESERVED_TOP = new Set<string>([
  "d",
  "i",
  "gist",
  "api",
  "favicon.svg",
  "robots.txt",
  "sitemap.xml",
  "SKILL.md",
  "skill.md",
  "skill",
]);

/** Computes the canonical "human" path for a document. Returns null
 *  for sources that don't have a meaningful URL shape (uploads,
 *  arbitrary URLs). Switches on sourceKind so gists get their own
 *  /gist/<owner>/<id> form without colliding with github user URLs. */
export function canonicalDocPath(doc: MdDocument): string | null {
  if (doc.sourceKind === "gist") {
    if (!doc.gistOwner || !doc.gistId) return null;
    return `/gist/${encodeURIComponent(doc.gistOwner)}/${encodeURIComponent(doc.gistId)}`;
  }
  // github_blob (or pre-migration docs without sourceKind set).
  if (!doc.githubOwner || !doc.githubRepo || !doc.githubPath) return null;
  if (RESERVED_TOP.has(doc.githubOwner)) return null;
  const ref = doc.githubRef || "main";
  const pathParts = doc.githubPath
    .split("/")
    .map((p) => encodeURIComponent(p))
    .join("/");
  return (
    `/${encodeURIComponent(doc.githubOwner)}` +
    `/${encodeURIComponent(doc.githubRepo)}` +
    `/blob/${encodeURIComponent(ref)}` +
    `/${pathParts}`
  );
}

/** Computes the canonical "human" path for an index. Returns null for
 *  non-github indexes (shouldn't happen today, but keeps callers safe). */
export function canonicalIndexPath(idx: MarkdownIndex): string | null {
  if (!idx.owner) return null;
  if (RESERVED_TOP.has(idx.owner)) return null;
  if (idx.kind === "repo" && idx.repo) {
    return `/${encodeURIComponent(idx.owner)}/${encodeURIComponent(idx.repo)}`;
  }
  return `/${encodeURIComponent(idx.owner)}`;
}

/** Builds the github.com URL from resolver route params. The "*" path
 *  parameter (everything after /blob/:ref/) is already decoded by
 *  React Router; we leave it intact for the createFromURL flow. */
export function githubURLForBlob(
  owner: string,
  repo: string,
  ref: string,
  path: string,
): string {
  return `https://github.com/${owner}/${repo}/blob/${ref}/${path}`;
}

export function githubURLForRepo(owner: string, repo: string): string {
  return `https://github.com/${owner}/${repo}`;
}

export function githubURLForOwner(owner: string): string {
  return `https://github.com/${owner}`;
}

/** Builds the gist landing-page URL from resolver route params. The
 *  backend's normalizeGistURL rewrites to /raw before fetching, so we
 *  hand it the landing form (cleaner to log + display than /raw).  */
export function gistURLFor(owner: string, gistId: string): string {
  return `https://gist.github.com/${owner}/${gistId}`;
}

/** Returns true when `name` is a reserved top-level path the resolver
 *  routes should refuse. Mirrors RESERVED_TOP for external callers. */
export function isReservedTopPath(name: string): boolean {
  return RESERVED_TOP.has(name);
}

/** Replace the browser URL in place (no history entry) when the
 *  caller has computed a canonical path for the same content. Used by
 *  DocumentPage and IndexPage to migrate /d/:id and /i/:id visitors to
 *  the human path without breaking the back button. */
export function rewriteToCanonical(canonical: string) {
  if (typeof window === "undefined") return;
  if (window.location.pathname === canonical) return;
  try {
    window.history.replaceState(window.history.state, "", canonical);
  } catch {
    /* security errors in cross-origin contexts — ignore */
  }
}
