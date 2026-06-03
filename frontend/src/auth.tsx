import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { api } from "./api";
import type { AuthUser } from "./types";

interface AuthContextValue {
  user: AuthUser | null;
  githubEnabled: boolean;
  githubClientId?: string;
  loading: boolean;
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
  loginURL: (redirect?: string) => string;
  manageGitHubURL: () => string | null;
}

const Ctx = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [githubEnabled, setGithubEnabled] = useState(false);
  const [githubClientId, setGithubClientId] = useState<string | undefined>();
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const [cfg, me] = await Promise.all([api.authConfig(), api.authMe()]);
      setGithubEnabled(cfg.githubEnabled);
      setGithubClientId(cfg.githubClientId);
      setUser(me.user);
    } catch {
      // best-effort
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const logout = useCallback(async () => {
    await api.authLogout();
    setUser(null);
  }, []);

  const loginURL = useCallback(
    (redirect?: string) => {
      const r = redirect ?? window.location.pathname + window.location.search;
      return `/api/auth/github/login?redirect=${encodeURIComponent(r)}`;
    },
    []
  );

  const manageGitHubURL = useCallback(() => {
    if (!githubClientId) return null;
    return `https://github.com/settings/connections/applications/${githubClientId}`;
  }, [githubClientId]);

  const value = useMemo(
    () => ({
      user,
      githubEnabled,
      githubClientId,
      loading,
      refresh,
      logout,
      loginURL,
      manageGitHubURL,
    }),
    [user, githubEnabled, githubClientId, loading, refresh, logout, loginURL, manageGitHubURL]
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthContextValue {
  const v = useContext(Ctx);
  if (!v) throw new Error("useAuth must be used inside AuthProvider");
  return v;
}
