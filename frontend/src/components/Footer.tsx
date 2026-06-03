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
          <Link to="/terms" className="hover:text-accent">
            Terms
          </Link>
          <Link to="/privacy" className="hover:text-accent">
            Privacy
          </Link>
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
