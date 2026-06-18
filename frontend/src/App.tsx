import { Link, Route, Routes } from "react-router-dom";
import HomePage from "./pages/Home";
import DocumentPage from "./pages/Document";
import IndexPage from "./pages/Index";
import GitHubResolve from "./pages/GitHubResolve";
import AuthorBadge from "./components/AuthorBadge";
import ThemeToggle from "./components/ThemeToggle";
import NotificationBell from "./components/NotificationBell";
import Footer from "./components/Footer";

export default function App() {
  return (
    <div className="min-h-full flex flex-col">
      <header className="border-b border-rule bg-card">
        <div className="max-w-7xl mx-auto px-6 h-14 flex items-center justify-between">
          <Link
            to="/"
            className="font-semibold tracking-tight text-ink hover:text-accent"
          >
            markupmarkdown
          </Link>
          <div className="flex items-center gap-3">
            <NotificationBell />
            <ThemeToggle />
            <AuthorBadge />
          </div>
        </div>
      </header>
      <main className="flex-1 min-h-0">
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="/d/:id" element={<DocumentPage />} />
          <Route path="/i/:id" element={<IndexPage />} />
          {/* Human-readable GitHub URL routes. Ordered most-specific
              first so /:owner/:repo/blob/:ref/* matches before the
              shorter prefixes. The leading literals `d`, `i`, and
              `gist` are reserved at the resolver level so a github
              user literally named "d" wouldn't collide with /d/:id. */}
          <Route path="/gist/:owner/:gistId" element={<GitHubResolve mode="gist" />} />
          <Route path="/:owner/:repo/blob/:ref/*" element={<GitHubResolve mode="doc" />} />
          <Route path="/:owner/:repo" element={<GitHubResolve mode="repo" />} />
          <Route path="/:owner" element={<GitHubResolve mode="owner" />} />
        </Routes>
      </main>
      <Footer />
    </div>
  );
}
