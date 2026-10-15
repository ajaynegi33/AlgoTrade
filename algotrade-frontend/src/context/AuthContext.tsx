"use client";

import React, { createContext, useContext, useState, useEffect, useCallback, useRef } from "react";
import { TOKEN_STORAGE_KEY, apiFetch } from "@/lib/apiClient";

const BACKEND_URL = process.env.NEXT_PUBLIC_BACKEND_URL || "http://localhost:8080";

interface AuthContextType {
  isAuthenticated: boolean;
  isLoading: boolean;
  accessToken: string | null;
  login: (password: string) => Promise<{ success: boolean; error?: string }>;
  logout: () => Promise<void>;
  checkSession: () => Promise<boolean>;
}

const AuthContext = createContext<AuthContextType>({
  isAuthenticated: false,
  isLoading: true,
  accessToken: null,
  login: async () => ({ success: false }),
  logout: async () => {},
  checkSession: async () => false,
});

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState<boolean>(false);
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [accessToken, setAccessToken] = useState<string | null>(null);
  const proactiveRefreshTimerRef = useRef<NodeJS.Timeout | null>(null);

  const scheduleProactiveRefresh = useCallback((expiresInSeconds: number = 900) => {
    if (proactiveRefreshTimerRef.current) {
      clearTimeout(proactiveRefreshTimerRef.current);
    }
    // Refresh proactively at 85% of lifespan (~12.75 minutes for a 15-minute token)
    const refreshDelayMs = Math.max(30000, Math.floor(expiresInSeconds * 0.85) * 1000);
    proactiveRefreshTimerRef.current = setTimeout(async () => {
      try {
        const res = await fetch(`${BACKEND_URL}/api/auth/refresh`, {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
        });
        if (res.ok) {
          const data = await res.json();
          if (data.accessToken) {
            localStorage.setItem(TOKEN_STORAGE_KEY, data.accessToken);
            setAccessToken(data.accessToken);
            scheduleProactiveRefresh(data.expiresInSeconds || 900);
          }
        } else {
          setIsAuthenticated(false);
          setAccessToken(null);
          localStorage.removeItem(TOKEN_STORAGE_KEY);
        }
      } catch (err) {
        console.error("Proactive silent refresh error:", err);
      }
    }, refreshDelayMs);
  }, []);

  const checkSession = useCallback(async (): Promise<boolean> => {
    setIsLoading(true);
    try {
      const storedToken = typeof window !== "undefined" ? localStorage.getItem(TOKEN_STORAGE_KEY) : null;
      if (storedToken) {
        const meRes = await fetch(`${BACKEND_URL}/api/auth/me`, {
          headers: { Authorization: `Bearer ${storedToken}` },
        });
        if (meRes.ok) {
          setAccessToken(storedToken);
          setIsAuthenticated(true);
          setIsLoading(false);
          scheduleProactiveRefresh(900);
          return true;
        }
      }

      // Try cookie-based refresh
      const refreshRes = await fetch(`${BACKEND_URL}/api/auth/refresh`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
      });

      if (refreshRes.ok) {
        const data = await refreshRes.json();
        if (data.accessToken) {
          localStorage.setItem(TOKEN_STORAGE_KEY, data.accessToken);
          setAccessToken(data.accessToken);
          setIsAuthenticated(true);
          setIsLoading(false);
          scheduleProactiveRefresh(data.expiresInSeconds || 900);
          return true;
        }
      }
    } catch (err) {
      console.warn("Session check failed:", err);
    }

    if (typeof window !== "undefined") {
      localStorage.removeItem(TOKEN_STORAGE_KEY);
    }
    setAccessToken(null);
    setIsAuthenticated(false);
    setIsLoading(false);
    return false;
  }, [scheduleProactiveRefresh]);

  useEffect(() => {
    checkSession();

    const handleAuthExpired = () => {
      setIsAuthenticated(false);
      setAccessToken(null);
      if (proactiveRefreshTimerRef.current) {
        clearTimeout(proactiveRefreshTimerRef.current);
      }
    };

    window.addEventListener("algotrade-auth-expired", handleAuthExpired);
    return () => {
      window.removeEventListener("algotrade-auth-expired", handleAuthExpired);
      if (proactiveRefreshTimerRef.current) {
        clearTimeout(proactiveRefreshTimerRef.current);
      }
    };
  }, [checkSession]);

  const login = async (password: string): Promise<{ success: boolean; error?: string }> => {
    try {
      const res = await fetch(`${BACKEND_URL}/api/auth/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ password }),
      });

      const data = await res.json().catch(() => ({}));

      if (!res.ok) {
        return {
          success: false,
          error: data.message || data.error || "Authentication failed. Please verify credentials.",
        };
      }

      if (data.accessToken) {
        localStorage.setItem(TOKEN_STORAGE_KEY, data.accessToken);
        setAccessToken(data.accessToken);
        setIsAuthenticated(true);
        scheduleProactiveRefresh(data.expiresInSeconds || 900);
        return { success: true };
      }

      return { success: false, error: "Invalid response from authentication server." };
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Network error contacting trading engine.";
      return { success: false, error: msg };
    }
  };

  const logout = async (): Promise<void> => {
    if (proactiveRefreshTimerRef.current) {
      clearTimeout(proactiveRefreshTimerRef.current);
    }
    try {
      await apiFetch("/api/auth/logout", { method: "POST" });
    } catch (err) {
      console.warn("Logout notification error:", err);
    } finally {
      localStorage.removeItem(TOKEN_STORAGE_KEY);
      setAccessToken(null);
      setIsAuthenticated(false);
    }
  };

  return (
    <AuthContext.Provider
      value={{
        isAuthenticated,
        isLoading,
        accessToken,
        login,
        logout,
        checkSession,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
