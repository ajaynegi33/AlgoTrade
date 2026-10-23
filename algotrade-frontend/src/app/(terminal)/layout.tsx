"use client";

import React, { useState, useEffect, useCallback, useMemo, Suspense } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import AuthGuard from "@/components/AuthGuard";
import DashboardSidebar from "@/components/dashboard/DashboardSidebar";
import { GlobalNavbarSearch } from "@/components/dashboard/GlobalNavbarSearch";
import { useAuth } from "@/context/AuthContext";
import { useTradingStore } from "@/store/useTradingStore";
import { toast } from "sonner";

interface MarketCountdownState {
  label: string;
  isLive: boolean;
  isUrgent: boolean;
}

function TerminalHeaderTitle() {
  const pathname = usePathname();

  const title = useMemo(() => {
    if (pathname.startsWith("/search/")) {
      const sym = pathname.replace("/search/", "").toUpperCase();
      return `${sym} Quant Cockpit`;
    }
    if (pathname.startsWith("/search")) {
      return "Search & Market Outliers";
    }
    if (pathname.startsWith("/leaderboard")) {
      return "Quantitative Leaderboard";
    }
    if (pathname.startsWith("/universe")) {
      return "Stock Universe Scanner";
    }
    if (pathname.startsWith("/profile")) {
      return "Kite Profile & Margins";
    }
    return "Trading Command Center";
  }, [pathname]);

  return (
    <h1 className="text-sm sm:text-[22px] font-black text-slate-900 tracking-normal leading-tight truncate">
      {title}
    </h1>
  );
}

export default function TerminalLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { user, logout } = useAuth();
  const { kiteStatus, fetchKiteStatus } = useTradingStore();

  const [isCollapsed, setIsCollapsed] = useState(false);
  const [isMobileOpen, setIsMobileOpen] = useState(false);
  const [marketCountdown, setMarketCountdown] = useState<MarketCountdownState | null>(null);

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

  // Live countdown to NSE Market Close (03:30 PM IST)
  useEffect(() => {
    function updateCountdown() {
      const now = new Date();
      // Convert to IST (UTC + 5.5 hours)
      const istOffsetMs = 5.5 * 60 * 60 * 1000;
      const istDate = new Date(now.getTime() + now.getTimezoneOffset() * 60 * 1000 + istOffsetMs);
      const day = istDate.getDay(); // 0 is Sunday, 6 is Saturday
      const isWeekday = day >= 1 && day <= 5;

      const openTime = new Date(istDate);
      openTime.setHours(9, 15, 0, 0);

      const closeTime = new Date(istDate);
      closeTime.setHours(15, 30, 0, 0);

      if (!isWeekday) {
        setMarketCountdown({
          label: "Market Closed (Weekend)",
          isLive: false,
          isUrgent: false,
        });
        return;
      }

      const diffMs = closeTime.getTime() - istDate.getTime();
      const openDiffMs = openTime.getTime() - istDate.getTime();

      if (openDiffMs > 0) {
        // Pre-market: before 09:15 IST
        const h = Math.floor(openDiffMs / (1000 * 60 * 60));
        const m = Math.floor((openDiffMs % (1000 * 60 * 60)) / (1000 * 60));
        setMarketCountdown({
          label: h > 0 ? `Opens in ${h}h ${m}m` : `Opens in ${m}m`,
          isLive: false,
          isUrgent: false,
        });
      } else if (diffMs > 0) {
        // Live market: between 09:15 and 15:30 IST
        const h = Math.floor(diffMs / (1000 * 60 * 60));
        const m = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));
        const s = Math.floor((diffMs % (1000 * 60)) / 1000);

        const isUrgent = diffMs <= 15 * 60 * 1000; // < 15 mins to market close
        let label = `Closes in ${h}h ${m}m`;
        if (h === 0) {
          label = `Closes in ${m}m ${s.toString().padStart(2, "0")}s`;
        }

        setMarketCountdown({
          label,
          isLive: true,
          isUrgent,
        });
      } else {
        // Post-market: after 15:30 IST
        setMarketCountdown({
          label: "Market Closed",
          isLive: false,
          isUrgent: false,
        });
      }
    }

    updateCountdown();
    const interval = setInterval(updateCountdown, 1000);
    return () => clearInterval(interval);
  }, []);

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
        <Suspense fallback={<div className="w-64 bg-white border-r border-slate-200/90 h-screen hidden md:block shrink-0" />}>
          <DashboardSidebar
            isCollapsed={isCollapsed}
            onToggleCollapse={handleToggleCollapse}
            isMobileOpen={isMobileOpen}
            onCloseMobile={() => setIsMobileOpen(false)}
            user={user}
            onLogout={handleLogout}
          />
        </Suspense>

        {/* Main Terminal Container */}
        <div className="flex-1 flex flex-col min-w-0 overflow-y-auto">
          {/* Top Header Bar */}
          <header className="sticky top-0 z-30 bg-white/85 backdrop-blur-md border-b border-slate-200/80">
            <div className="px-4 sm:px-6 lg:px-8 h-16 flex items-center justify-between gap-4">
              {/* Left: Mobile hamburger + Page Header Title/Subtitle */}
              <div className="flex items-center gap-3 min-w-0">
                <button
                  type="button"
                  onClick={() => setIsMobileOpen(true)}
                  className="md:hidden p-2 rounded-xl border border-slate-200 text-slate-600 hover:text-slate-900 hover:bg-slate-100 cursor-pointer active:scale-95 transition-all shrink-0"
                  aria-label="Open sidebar"
                >
                  <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 6h16M4 12h16M4 18h16" />
                  </svg>
                </button>

                <div className="min-w-0">
                  <Suspense fallback={<h1 className="text-sm sm:text-[22px] font-black text-slate-900 tracking-normal leading-tight">AlgoTrade</h1>}>
                    <TerminalHeaderTitle />
                  </Suspense>
                </div>
              </div>

              {/* Center: Global Navbar Search */}
              <div className="hidden md:flex flex-1 justify-center px-2 lg:px-4 max-w-lg lg:max-w-xl mx-auto">
                <GlobalNavbarSearch />
              </div>

              {/* Right: Live Connection Telemetry Badges */}
              <div className="flex items-center gap-3">
                {kiteStatus?.is_connected ? (
                  <Link
                    href="/profile"
                    className="flex items-center gap-2 text-xs bg-emerald-50 hover:bg-emerald-100/80 border border-emerald-200/80 rounded-full px-3 py-1 text-emerald-800 font-mono transition-colors shrink-0"
                  >
                    <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
                    <span className="font-semibold">Kite: {kiteStatus.user_id}</span>
                  </Link>
                ) : (
                  <Link
                    href="/profile"
                    className="flex items-center gap-2 text-xs bg-amber-50 hover:bg-amber-100/80 border border-amber-200/80 rounded-full px-3 py-1 text-amber-800 font-mono transition-colors shrink-0"
                  >
                    <span className="w-2 h-2 rounded-full bg-amber-500" />
                    <span className="font-semibold">Kite Disconnected</span>
                  </Link>
                )}

                {marketCountdown && (
                  <div
                    className={`flex items-center gap-1.5 text-xs rounded-full px-3 py-1 font-mono transition-colors shrink-0 border ${
                      marketCountdown.isUrgent
                        ? "bg-rose-50 text-rose-800 border-rose-300 font-bold animate-pulse"
                        : marketCountdown.isLive
                        ? "bg-slate-100/90 text-slate-800 border-slate-200/90 font-semibold"
                        : "bg-slate-50 text-slate-500 border-slate-200/80"
                    }`}
                    title="NSE Trading Session: 09:15 - 15:30 IST"
                  >
                    <span
                      className={`w-1.5 h-1.5 rounded-full ${
                        marketCountdown.isUrgent
                          ? "bg-rose-500 animate-ping"
                          : marketCountdown.isLive
                          ? "bg-emerald-500 animate-pulse"
                          : "bg-slate-400"
                      }`}
                    />
                    <svg
                      className={`w-3.5 h-3.5 ${
                        marketCountdown.isUrgent
                          ? "text-rose-600"
                          : marketCountdown.isLive
                          ? "text-slate-600"
                          : "text-slate-400"
                      }`}
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <circle cx="12" cy="12" r="9" />
                      <path strokeLinecap="round" strokeLinejoin="round" d="M12 7v5l3 2" />
                    </svg>
                    <span>{marketCountdown.label}</span>
                  </div>
                )}
              </div>
            </div>
          </header>

          {/* Page Body */}
          <main className="flex-1 px-2 sm:px-2 lg:px-2 py-6 sm:py-8 max-w-7xl w-full mx-auto">
            <Suspense fallback={null}>
              {children}
            </Suspense>
          </main>
        </div>
      </div>
    </AuthGuard>
  );
}
