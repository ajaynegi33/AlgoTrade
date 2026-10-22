"use client";

import React, { useState, useEffect, useCallback } from "react";
import Link from "next/link";
import AuthGuard from "@/components/AuthGuard";
import DashboardSidebar from "@/components/dashboard/DashboardSidebar";
import { useAuth } from "@/context/AuthContext";
import { useTradingStore } from "@/store/useTradingStore";
import { toast } from "sonner";

export default function TerminalLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { user, logout } = useAuth();
  const { kiteStatus, fetchKiteStatus } = useTradingStore();

  const [isCollapsed, setIsCollapsed] = useState(false);
  const [isMobileOpen, setIsMobileOpen] = useState(false);

  // Load saved sidebar collapsed preference from localStorage
  useEffect(() => {
    try {
      const saved = localStorage.getItem("algotrade_sidebar_collapsed");
      if (saved !== null) {
        queueMicrotask(() => {
          setIsCollapsed(saved === "true");
        });
      }
    } catch {
      // Ignore storage errors
    }
  }, []);

  // Poll Kite connection status once on layout mount
  useEffect(() => {
    fetchKiteStatus();
  }, [fetchKiteStatus]);

  // Handle Kite OAuth callback query params from browser 302 redirect
  useEffect(() => {
    if (typeof window === "undefined") return;

    const urlParams = new URLSearchParams(window.location.search);
    const kiteStatusParam = urlParams.get("kite_status");
    const errorMsg = urlParams.get("message");

    if (kiteStatusParam === "success") {
      toast.success("Zerodha Kite connected successfully! Session is now active.");
      window.history.replaceState({}, document.title, window.location.pathname);
      fetchKiteStatus();
    } else if (kiteStatusParam === "error") {
      toast.error(errorMsg || "Zerodha authentication failed");
      window.history.replaceState({}, document.title, window.location.pathname);
    }
  }, [fetchKiteStatus]);

  const handleToggleCollapse = useCallback(() => {
    setIsCollapsed((prev) => {
      const next = !prev;
      try {
        localStorage.setItem("algotrade_sidebar_collapsed", String(next));
      } catch {
        // Ignore storage errors
      }
      return next;
    });
  }, []);

  const handleLogout = async () => {
    try {
      await logout();
      toast.info("Logged out", { description: "Session closed successfully." });
    } catch (err) {
      console.error("Logout error:", err);
    }
  };

  return (
    <AuthGuard>
      <div className="min-h-screen bg-slate-50 text-slate-900 flex selection:bg-emerald-500/20 selection:text-emerald-900">
        {/* Collapsible Desktop & Mobile Drawer Sidenavbar */}
        <DashboardSidebar
          isCollapsed={isCollapsed}
          onToggleCollapse={handleToggleCollapse}
          isMobileOpen={isMobileOpen}
          onCloseMobile={() => setIsMobileOpen(false)}
          user={user}
          onLogout={handleLogout}
        />

        {/* Main Terminal Container */}
        <div className="flex-1 flex flex-col min-w-0 overflow-y-auto">
          {/* Top Header Bar */}
          <header className="sticky top-0 z-30 bg-white/85 backdrop-blur-md border-b border-slate-200/80">
            <div className="px-4 sm:px-6 lg:px-8 h-16 flex items-center justify-between gap-4">
              {/* Left: Mobile hamburger (no breadcrumbs) */}
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  onClick={() => setIsMobileOpen(true)}
                  className="md:hidden p-2 rounded-xl border border-slate-200 text-slate-600 hover:text-slate-900 hover:bg-slate-100 cursor-pointer active:scale-95 transition-all"
                  aria-label="Open sidebar"
                >
                  <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 6h16M4 12h16M4 18h16" />
                  </svg>
                </button>
              </div>

              {/* Right: Live Connection Telemetry Badges */}
              <div className="flex items-center gap-3">
                {kiteStatus?.is_connected ? (
                  <Link
                    href="/profile"
                    className="flex items-center gap-2 text-xs bg-emerald-50 hover:bg-emerald-100/80 border border-emerald-200/80 rounded-full px-3 py-1 text-emerald-800 font-mono transition-colors"
                  >
                    <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
                    <span className="font-semibold">Kite: {kiteStatus.user_id}</span>
                  </Link>
                ) : (
                  <Link
                    href="/profile"
                    className="flex items-center gap-2 text-xs bg-amber-50 hover:bg-amber-100/80 border border-amber-200/80 rounded-full px-3 py-1 text-amber-800 font-mono transition-colors"
                  >
                    <span className="w-2 h-2 rounded-full bg-amber-500" />
                    <span className="font-semibold">Kite Disconnected</span>
                  </Link>
                )}

                <div className="hidden sm:flex items-center gap-2 text-xs bg-slate-100 border border-slate-200/80 rounded-full px-3 py-1 text-slate-600 font-mono">
                  <span>NSE Cash 08:30 AM</span>
                </div>
              </div>
            </div>
          </header>

          {/* Page Body */}
          <main className="flex-1 px-4 sm:px-6 lg:px-8 py-6 sm:py-8 max-w-7xl w-full mx-auto">
            {children}
          </main>
        </div>
      </div>
    </AuthGuard>
  );
}
