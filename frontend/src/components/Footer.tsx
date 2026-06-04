import { Link } from "react-router-dom";

export default function Footer() {
  return (
    <footer className="border-t border-rule bg-card">
      <div className="max-w-7xl mx-auto px-6 py-3 flex flex-wrap items-center justify-between gap-2 text-xs text-muted">
        <div>
          © 2026{" "}
          <a
            href="https://metavert.io"
            target="_blank"
            rel="noreferrer"
            className="hover:text-accent"
          >
            Metavert LLC
          </a>
          {" · "}MIT licensed
        </div>
        <nav className="flex items-center gap-4">
          <Link to="/skill" className="hover:text-accent" title="Agent integration guide (SKILL.md)">
            SKILL.md
          </Link>
          <a
            href="https://www.metavert.io/terms-of-service"
            target="_blank"
            rel="noreferrer"
            className="hover:text-accent"
          >
            Terms
          </a>
          <a
            href="https://www.metavert.io/privacy-policy"
            target="_blank"
            rel="noreferrer"
            className="hover:text-accent"
          >
            Privacy
          </a>
          <a
            href="https://metavert.io"
            target="_blank"
            rel="noreferrer"
            className="hover:text-accent"
          >
            metavert.io
          </a>
        </nav>
      </div>
    </footer>
  );
}
