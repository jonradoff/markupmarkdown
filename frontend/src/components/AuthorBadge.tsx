import { useEffect, useRef, useState } from "react";
import { getAuthor, setAuthor } from "../utils/author";
import { colorFor, initials } from "../utils/format";
import { useAuth } from "../auth";
import SignInModal from "./SignInModal";
import APIKeyModal from "./APIKeyModal";
import TokensModal from "./TokensModal";

export default function AuthorBadge() {
  const { user, githubEnabled, logout, loginURL, manageGitHubURL } = useAuth();
  const [name, setName] = useState(getAuthor());
  const [open, setOpen] = useState(false);
  const [showModal, setShowModal] = useState(false);
  const [showAPIKey, setShowAPIKey] = useState(false);
  const [showTokens, setShowTokens] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function onClick(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, []);

  const displayName = user?.name || user?.login || name || "";
  const hasIdentity = !!user || !!name;

  function handleClear() {
    setAuthor("");
    setName("");
    setOpen(false);
  }

  async function handleLogout() {
    await logout();
    setOpen(false);
  }

  return (
    <div className="relative" ref={menuRef}>
      <button
        onClick={() => {
          if (!hasIdentity) {
            setShowModal(true);
          } else {
            setOpen((o) => !o);
          }
        }}
        className="flex items-center gap-2 text-sm text-ink hover:text-accent"
      >
        {user?.avatarUrl ? (
          <img src={user.avatarUrl} alt="" className="w-7 h-7 rounded-full" />
        ) : displayName ? (
          <span
            className="w-7 h-7 rounded-full text-white text-xs flex items-center justify-center font-medium"
            style={{ background: colorFor(displayName) }}
          >
            {initials(displayName)}
          </span>
        ) : (
          <span className="w-7 h-7 rounded-full bg-soft text-muted text-xs flex items-center justify-center">
            ?
          </span>
        )}
        <span className="max-w-[160px] truncate">
          {displayName || "Sign in"}
        </span>
      </button>

      {open && hasIdentity && (
        <div className="absolute right-0 mt-2 w-56 bg-card border border-rule rounded-lg shadow-lg py-1 z-30">
          {user ? (
            <>
              <div className="px-3 py-2 border-b border-rule">
                <div className="text-sm font-medium truncate">
                  {user.name || user.login}
                </div>
                <div className="text-xs text-muted truncate">@{user.login}</div>
              </div>
              {manageGitHubURL() && (
                <a
                  href={manageGitHubURL()!}
                  target="_blank"
                  rel="noreferrer"
                  className="block px-3 py-2 text-sm hover:bg-soft"
                  title="Review which organizations have granted this app access — request access for any that haven't"
                >
                  Manage GitHub access ↗
                </a>
              )}
              <button
                onClick={() => {
                  setOpen(false);
                  setShowAPIKey(true);
                }}
                className="w-full text-left px-3 py-2 text-sm hover:bg-soft"
                title="For AI revision (Claude Opus 4.7) — stored encrypted, deletable any time"
              >
                Anthropic API key
              </button>
              <button
                onClick={() => {
                  setOpen(false);
                  setShowTokens(true);
                }}
                className="w-full text-left px-3 py-2 text-sm hover:bg-soft"
                title="Personal access tokens for the REST and MCP APIs"
              >
                Personal access tokens
              </button>
              <button
                onClick={handleLogout}
                className="w-full text-left px-3 py-2 text-sm hover:bg-soft"
              >
                Sign out
              </button>
            </>
          ) : (
            <>
              <div className="px-3 py-2 border-b border-rule">
                <div className="text-sm font-medium truncate">{name}</div>
                <div className="text-xs text-muted">Anonymous</div>
              </div>
              <button
                onClick={() => {
                  setOpen(false);
                  setShowModal(true);
                }}
                className="w-full text-left px-3 py-2 text-sm hover:bg-soft"
              >
                Change name
              </button>
              {githubEnabled && (
                <a
                  href={loginURL()}
                  className="block px-3 py-2 text-sm hover:bg-soft"
                >
                  Sign in with GitHub
                </a>
              )}
              <button
                onClick={handleClear}
                className="w-full text-left px-3 py-2 text-sm text-muted hover:bg-soft"
              >
                Go anonymous
              </button>
            </>
          )}
        </div>
      )}

      {showModal && (
        <SignInModal
          onClose={() => setShowModal(false)}
          onContinue={(n) => {
            setName(n);
            setShowModal(false);
          }}
        />
      )}

      {showAPIKey && (
        <APIKeyModal onClose={() => setShowAPIKey(false)} />
      )}

      {showTokens && (
        <TokensModal onClose={() => setShowTokens(false)} />
      )}
    </div>
  );
}
