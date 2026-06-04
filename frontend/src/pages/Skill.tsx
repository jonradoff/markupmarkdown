import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import MarkdownRender from "../components/MarkdownRender";

// Renders /skill.md (served by the backend) so humans can read the
// agent-facing integration guide in the app's UI. The raw file at
// /skill.md is the canonical source for tooling.

export default function SkillPage() {
  const [body, setBody] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/skill.md");
        if (!res.ok) throw new Error(`http ${res.status}`);
        const text = await res.text();
        if (!cancelled) setBody(text);
      } catch (e) {
        if (!cancelled) setError((e as Error).message);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div className="max-w-3xl mx-auto px-6 py-10">
      <div className="text-xs text-muted mb-4 flex items-center justify-between">
        <Link to="/" className="hover:text-accent">← Home</Link>
        <a
          href="/skill.md"
          target="_blank"
          rel="noreferrer"
          className="hover:text-accent"
        >
          View raw SKILL.md ↗
        </a>
      </div>
      {error ? (
        <div className="text-sm text-danger">Failed to load SKILL.md: {error}</div>
      ) : body == null ? (
        <div className="text-muted text-sm">Loading…</div>
      ) : (
        <MarkdownRender content={stripFrontmatter(body)} />
      )}
    </div>
  );
}

// stripFrontmatter removes the leading `---\n...\n---\n` block if present —
// react-markdown doesn't natively render YAML frontmatter, and the metadata
// inside is for tooling, not humans.
function stripFrontmatter(s: string): string {
  if (!s.startsWith("---\n")) return s;
  const end = s.indexOf("\n---\n", 4);
  if (end < 0) return s;
  return s.slice(end + 5);
}
