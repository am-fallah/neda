import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { authApi } from "@/api/endpoints";
import {
  isApiError,
  setCsrfToken,
  setUnauthorizedHandler,
  type ApiError,
} from "@/api/client";

type AuthStatus = "loading" | "authenticated" | "anonymous" | "error";

interface AuthContextValue {
  status: AuthStatus;
  setupRequired: boolean;
  expiresAt: string | null;
  error: ApiError | null;
  login: (password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [setupRequired, setSetupRequired] = useState(false);
  const [expiresAt, setExpiresAt] = useState<string | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const refresh = useCallback(async () => {
    try {
      const session = await authApi.session();
      if (!mounted.current) return;
      setError(null);
      setSetupRequired(Boolean(session.setupRequired));
      setExpiresAt(session.expiresAt ?? null);
      if (session.authenticated && session.csrfToken) {
        setCsrfToken(session.csrfToken);
        setStatus("authenticated");
      } else {
        setCsrfToken("");
        setStatus("anonymous");
      }
    } catch (caught) {
      if (!mounted.current) return;
      setCsrfToken("");
      setError(
        isApiError(caught)
          ? caught
          : null,
      );
      setStatus("error");
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    setUnauthorizedHandler(() => {
      setCsrfToken("");
      setExpiresAt(null);
      setStatus("anonymous");
    });
    return () => setUnauthorizedHandler(null);
  }, []);

  const login = useCallback(async (password: string) => {
    const result = await authApi.login(password);
    setCsrfToken(result.csrfToken);
    setExpiresAt(result.expiresAt);
    setSetupRequired(false);
    setError(null);
    setStatus("authenticated");
  }, []);

  const logout = useCallback(async () => {
    try {
      await authApi.logout();
    } catch {
      // A missing or expired session is already logged out. Clear local state
      // regardless; never surface raw server details here.
    } finally {
      setCsrfToken("");
      setExpiresAt(null);
      setStatus("anonymous");
    }
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      status,
      setupRequired,
      expiresAt,
      error,
      login,
      logout,
      refresh,
    }),
    [status, setupRequired, expiresAt, error, login, logout, refresh],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used inside AuthProvider");
  }
  return context;
}
