"use client";

import React, {
  createContext,
  useContext,
  useEffect,
  useState,
  useCallback,
  ReactNode,
} from "react";
import { User } from "../lib/types";
import {
  getUserProfile,
  loginUser,
  logoutUser,
  refreshAccessToken,
} from "../lib/api";

interface AuthContextType {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  error: string | null;
  login: (email: string, pass: string) => Promise<void>;
  logout: () => Promise<void>;
  clearError: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider = ({ children }: { children: ReactNode }) => {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Hydrate auth session on initial page mount via silent refresh
  useEffect(() => {
    let isMounted = true;

    async function initializeSession() {
      try {
        const token = await refreshAccessToken();
        if (token && isMounted) {
          const profileData = await getUserProfile();
          if (isMounted) {
            setUser({
              id: profileData.profile.id,
              email: profileData.profile.email,
              name: profileData.profile.name,
              role: profileData.profile.role,
            });
          }
        }
      } catch (err) {
        // Normal state when visitor is unauthenticated
        console.debug("Initial session lookup completed: No active session.", err);
      } finally {
        if (isMounted) {
          setIsLoading(false);
        }
      }
    }

    initializeSession();

    return () => {
      isMounted = false;
    };
  }, []);

  const login = useCallback(async (email: string, pass: string) => {
    setIsLoading(true);
    setError(null);
    try {
      const data = await loginUser(email, pass);
      setUser(data.user);
    } catch (err: unknown) {
      const errorMsg =
        err instanceof Error ? err.message : "Failed to log in. Please try again.";
      setError(errorMsg);
      throw err;
    } finally {
      setIsLoading(false);
    }
  }, []);

  const logout = useCallback(async () => {
    setIsLoading(true);
    try {
      await logoutUser();
    } catch (err) {
      console.error("Logout error:", err);
    } finally {
      setUser(null);
      setError(null);
      setIsLoading(false);
    }
  }, []);

  const clearError = useCallback(() => setError(null), []);

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: !!user,
        error,
        login,
        logout,
        clearError,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
};
