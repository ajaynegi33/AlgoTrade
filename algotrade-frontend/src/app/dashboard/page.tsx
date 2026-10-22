"use client";

import React, { useEffect, useState, useCallback } from "react";
import AuthGuard from "../../components/AuthGuard";
import { useAuth } from "../../context/AuthContext";
import { getUserProfile, setAccessToken } from "../../lib/api";
import { ProfileResponseData } from "../../lib/types";
import DashboardSidebar from "@/components/dashboard/DashboardSidebar";
import { toast } from "sonner";

export default function DashboardPage() {
  return (
    <AuthGuard>
      <DashboardContent />
    </AuthGuard>
  );
}

function DashboardContent() {
  const { user, logout } = useAuth();
  const [profileData, setProfileData] = useState<ProfileResponseData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [testStatus, setTestStatus] = useState<string | null>(null);
  const [isRefreshing, setIsRefreshing] = useState(false);

  // Sidebar responsive & collapsed states
  const [isCollapsed, setIsCollapsed] = useState(false);
  const [isMobileOpen, setIsMobileOpen] = useState(false);
  const [activeSection, setActiveSection] = useState("dashboard");

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

  // Load dashboard data on mount
  useEffect(() => {
    let active = true;

    async function fetchInitialData() {
      try {
        const data = await getUserProfile();
        if (active) {
          setProfileData(data);
          setError(null);
        }
      } catch (err: unknown) {
        if (active) {
          console.error("[Dashboard] Initial fetch error:", err);
          setError(
            err instanceof Error
              ? err.message
              : "Failed to load user profile. Please check the backend connection."
          );
        }
      } finally {
        if (active) {
          setLoading(false);
        }
      }
    }

    fetchInitialData();

    return () => {
      active = false;
    };
  }, []);

  // Manual reload with try/catch exception handling
  const handleReload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getUserProfile();
      setProfileData(data);
      toast.success("Dashboard metrics updated successfully");
    } catch (err: unknown) {
      console.error("[Dashboard] Reload error:", err);
      const msg = err instanceof Error ? err.message : "Failed to load user profile";
      setError(msg);
      toast.error(msg);
    } finally {
      setLoading(false);
    }
  }, []);

  // Demo helper: simulate access token expiry to test silent refresh
  const handleSimulateTokenExpiry = async () => {
    setIsRefreshing(true);
    setTestStatus("Clearing access token in memory to force 401 & trigger silent refresh...");
    try {
      // Intentionally invalidate in-memory access token
      setAccessToken("invalid-expired-mock-token");

      // Trigger protected profile fetch -> backend returns 401 -> fetchWithAuth catches 401
      // and calls POST /api/auth/refresh -> gets new 5-min token -> retries successfully!
      const data = await getUserProfile();
      setProfileData(data);
      setTestStatus("✅ Silent refresh successful! New 5-minute access token acquired seamlessly via RTR.");
      toast.success("Dual-Token RTR Test Passed", {
        description: "5m Access Token seamlessly re-issued via HttpOnly cookie.",
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Silent refresh failed";
      setTestStatus(`❌ Refresh failed: ${msg}`);
      toast.error("RTR Test Failed", { description: msg });
    } finally {
      setIsRefreshing(false);
      setTimeout(() => setTestStatus(null), 6000);
    }
  };

  const handleLogout = async () => {
    try {
      await logout();
      toast.info("Logged out", { description: "Session closed successfully." });
    } catch (err) {
      console.error("Logout error:", err);
    }
  };

  const handleSelectSection = useCallback((section: string) => {
    setActiveSection(section);
    if (section === "positions") {
      document.getElementById("positions-table")?.scrollIntoView({ behavior: "smooth" });
    } else if (section === "security") {
      document.getElementById("token-test-section")?.scrollIntoView({ behavior: "smooth" });
    } else {
      window.scrollTo({ top: 0, behavior: "smooth" });
    }
  }, []);

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 flex selection:bg-emerald-500/20 selection:text-emerald-900">
      {/* =========================================================================
          1. Collapsible Desktop & Mobile Drawer Sidenavbar
          ========================================================================= */}
      <DashboardSidebar
        isCollapsed={isCollapsed}
        onToggleCollapse={handleToggleCollapse}
        isMobileOpen={isMobileOpen}
        onCloseMobile={() => setIsMobileOpen(false)}
        activeSection={activeSection}
        onSelectSection={handleSelectSection}
        user={user}
        onLogout={handleLogout}
      />

      {/* =========================================================================
          2. Main Terminal Content Container
          ========================================================================= */}
      <div className="flex-1 flex flex-col min-w-0 overflow-y-auto">
        {/* Top Header Bar */}
        <header className="sticky top-0 z-30 bg-white/85 backdrop-blur-md border-b border-slate-200/80">
          <div className="px-4 sm:px-6 lg:px-8 h-16 flex items-center justify-between gap-4">
            {/* Left: Mobile hamburger & Breadcrumbs */}
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

              <div className="flex items-center gap-2 text-xs sm:text-sm font-semibold text-slate-800">
                <span className="text-slate-400 font-normal">Terminal</span>
                <span className="text-slate-300">/</span>
                <span className="text-slate-900 font-bold">Overview</span>
              </div>
            </div>

            {/* Right: Live Connection & Token Telemetry */}
            <div className="flex items-center gap-3">
              <div className="hidden sm:flex items-center gap-2 text-xs bg-emerald-50 border border-emerald-200/80 rounded-full px-3 py-1 text-emerald-800 font-mono">
                <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
                <span className="font-semibold">WebSocket: 0ms Active</span>
              </div>

              <div className="hidden lg:flex items-center gap-2 text-xs bg-slate-100 border border-slate-200/80 rounded-full px-3 py-1 text-slate-600 font-mono">
                <span>Dual-Token (5m / 24h RTR)</span>
              </div>
            </div>
          </div>
        </header>

        {/* Main Body */}
        <main className="flex-1 px-4 sm:px-6 lg:px-8 py-6 sm:py-8 space-y-6 max-w-7xl w-full mx-auto">
          {/* Section: Dual-Token Verification & Silent Refresh Test Harness */}
          <div
            id="token-test-section"
            className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs flex flex-col md:flex-row items-start md:items-center justify-between gap-4 scroll-mt-20"
          >
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <span className="px-2.5 py-0.5 rounded-md text-[11px] font-mono font-bold bg-emerald-50 text-emerald-700 border border-emerald-200">
                  JWT Dual-Token Active
                </span>
                <span className="text-xs text-slate-600">
                  Access Token: <strong className="text-slate-900">5 mins (Memory)</strong> | Refresh Cookie: <strong className="text-slate-900">24 hours (HttpOnly)</strong>
                </span>
              </div>
              <p className="text-xs text-slate-500 mt-1.5 leading-relaxed">
                Test Single-Use Refresh Token Rotation (RTR). Simulates access token expiration to verify seamless background token renewal without interrupting the trader.
              </p>
            </div>

            <div className="flex items-center gap-2.5 w-full md:w-auto shrink-0">
              <button
                type="button"
                onClick={handleSimulateTokenExpiry}
                disabled={isRefreshing || loading}
                className="w-full md:w-auto text-xs font-bold px-4 py-2.5 rounded-2xl bg-slate-900 hover:bg-slate-800 text-white transition-all disabled:opacity-50 cursor-pointer shadow-xs active:scale-95 flex items-center justify-center gap-2"
              >
                {isRefreshing ? (
                  <>
                    <div className="w-3.5 h-3.5 border-2 border-white/20 border-t-white rounded-full animate-spin" />
                    <span>Rotating Tokens...</span>
                  </>
                ) : (
                  <span>Test Silent 401 Refresh</span>
                )}
              </button>

              <button
                type="button"
                onClick={handleReload}
                disabled={loading}
                className="text-xs font-bold px-4 py-2.5 rounded-2xl bg-slate-100 hover:bg-slate-200/80 border border-slate-200 text-slate-800 transition-all disabled:opacity-50 cursor-pointer active:scale-95 shrink-0"
              >
                {loading ? "Loading..." : "Reload Data"}
              </button>
            </div>
          </div>

          {/* Test Result Banner */}
          {testStatus && (
            <div className="p-4 rounded-2xl text-xs font-mono font-medium bg-emerald-50 border border-emerald-200 text-emerald-900 flex items-center gap-2 shadow-2xs animate-in fade-in">
              <span>{testStatus}</span>
            </div>
          )}

          {/* Error Handling State */}
          {error && (
            <div className="p-6 rounded-3xl bg-rose-50 border border-rose-200 text-rose-900 space-y-3 shadow-xs">
              <div className="flex items-center gap-3">
                <svg className="w-6 h-6 text-rose-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <div>
                  <h3 className="font-bold text-sm text-rose-950">Failed to communicate with trading backend</h3>
                  <p className="text-xs text-rose-700 mt-0.5">{error}</p>
                </div>
              </div>
              <button
                type="button"
                onClick={handleReload}
                className="px-4 py-2 rounded-xl bg-rose-600 hover:bg-rose-700 text-white text-xs font-bold transition-all cursor-pointer shadow-xs active:scale-95"
              >
                Retry Connection
              </button>
            </div>
          )}

          {/* Loading Skeletons */}
          {loading && !profileData && (
            <div className="grid grid-cols-1 md:grid-cols-4 gap-4 animate-pulse">
              {[1, 2, 3, 4].map((i) => (
                <div key={i} className="h-32 rounded-3xl bg-slate-200/60 border border-slate-200" />
              ))}
            </div>
          )}

          {/* User Data Dashboard Display */}
          {profileData && (
            <div className="space-y-6">
              {/* Top User Profile Header Card */}
              <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs">
                <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
                  <div className="flex items-center gap-4">
                    <div className="w-14 h-14 rounded-2xl bg-gradient-to-br from-emerald-500 to-teal-600 text-white flex items-center justify-center text-xl font-black shadow-md shadow-emerald-500/15 shrink-0">
                      {profileData.profile.name.charAt(0)}
                    </div>
                    <div>
                      <div className="flex items-center gap-2">
                        <h2 className="text-xl font-bold text-slate-900 tracking-tight">
                          {profileData.profile.name}
                        </h2>
                        <span className="text-[10px] uppercase font-bold tracking-wider px-2 py-0.5 rounded-full bg-emerald-50 text-emerald-700 border border-emerald-200">
                          {profileData.profile.tier}
                        </span>
                        {profileData.profile.kyc_verified && (
                          <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-sky-50 text-sky-700 border border-sky-200">
                            KYC Verified
                          </span>
                        )}
                      </div>
                      <div className="text-xs text-slate-500 mt-1 flex flex-wrap gap-x-4 gap-y-1 font-mono">
                        <span>ID: <code className="text-slate-800 font-semibold">{profileData.profile.id}</code></span>
                        <span>Account: <code className="text-slate-800 font-semibold">{profileData.portfolio_summary.account_number}</code></span>
                        <span>Email: <code className="text-slate-800 font-semibold">{profileData.profile.email}</code></span>
                      </div>
                    </div>
                  </div>

                  <div className="text-left md:text-right">
                    <span className="text-xs text-slate-400 font-mono uppercase tracking-wider block">Account Status</span>
                    <div className="text-sm font-bold text-emerald-700 flex items-center md:justify-end gap-1.5 mt-0.5">
                      <span className="w-2 h-2 rounded-full bg-emerald-500" />
                      {profileData.profile.account_status}
                    </div>
                  </div>
                </div>
              </div>

              {/* Financial Summary Bento Cards */}
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
                {/* Total Equity */}
                <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs hover:border-slate-300 transition-colors">
                  <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400 block">Total Equity</span>
                  <div className="text-2xl font-black tracking-tight text-slate-900 mt-1 font-mono">
                    ${profileData.portfolio_summary.total_equity.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                  </div>
                  <div className="text-xs font-mono text-slate-500 mt-1">Currency: {profileData.portfolio_summary.currency}</div>
                </div>

                {/* Available Cash */}
                <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs hover:border-slate-300 transition-colors">
                  <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400 block">Available Cash</span>
                  <div className="text-2xl font-black tracking-tight text-slate-900 mt-1 font-mono">
                    ${profileData.portfolio_summary.cash_balance.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                  </div>
                  <div className="text-xs font-mono font-semibold text-emerald-700 mt-1">Ready for deployment</div>
                </div>

                {/* Unrealized P&L */}
                <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs hover:border-slate-300 transition-colors">
                  <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400 block">Today&apos;s Unrealized P&amp;L</span>
                  <div className="text-2xl font-black tracking-tight text-emerald-700 mt-1 font-mono">
                    +${profileData.portfolio_summary.unrealized_pnl.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                  </div>
                  <div className="text-xs font-mono font-semibold text-emerald-700 mt-1">
                    +{profileData.portfolio_summary.unrealized_pnl_percent}% net alpha
                  </div>
                </div>

                {/* Active Algo Bots */}
                <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs hover:border-slate-300 transition-colors">
                  <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400 block">Active Algo Bots</span>
                  <div className="text-2xl font-black tracking-tight text-sky-700 mt-1 font-mono">
                    {profileData.portfolio_summary.active_bots_count} Bots
                  </div>
                  <div className="text-xs font-mono text-slate-500 mt-1">High-frequency breakout</div>
                </div>
              </div>

              {/* Active Trading Positions Table */}
              <div
                id="positions-table"
                className="bg-white border border-slate-200/90 rounded-3xl overflow-hidden shadow-xs scroll-mt-20"
              >
                <div className="px-6 py-4 border-b border-slate-100 flex items-center justify-between">
                  <div>
                    <h3 className="font-bold text-sm text-slate-900">Active Live Positions</h3>
                    <p className="text-xs text-slate-500 mt-0.5">Real-time mock positions delivered from Go backend</p>
                  </div>
                  <span className="text-xs font-mono font-bold px-2.5 py-1 rounded-full bg-slate-100 text-slate-700">
                    {profileData.active_positions.length} Open Trades
                  </span>
                </div>

                <div className="overflow-x-auto">
                  <table className="w-full text-left border-collapse text-xs">
                    <thead>
                      <tr className="border-b border-slate-100 text-slate-500 uppercase tracking-wider font-mono text-[11px] bg-slate-50/80">
                        <th className="py-3 px-6">Market</th>
                        <th className="py-3 px-6">Side</th>
                        <th className="py-3 px-6">Size</th>
                        <th className="py-3 px-6">Entry Price</th>
                        <th className="py-3 px-6">Mark Price</th>
                        <th className="py-3 px-6 text-right">PnL (USD)</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100">
                      {profileData.active_positions.map((pos) => {
                        const isProfit = pos.pnl >= 0;
                        return (
                          <tr key={pos.symbol} className="hover:bg-slate-50/70 transition-colors">
                            <td className="py-4 px-6 font-bold text-slate-900 font-mono">
                              {pos.symbol}
                            </td>
                            <td className="py-4 px-6">
                              <span
                                className={`px-2.5 py-0.5 rounded-md text-[10px] font-mono font-bold tracking-wider ${
                                  pos.side === "LONG"
                                    ? "bg-emerald-50 text-emerald-700 border border-emerald-200"
                                    : "bg-rose-50 text-rose-700 border border-rose-200"
                                }`}
                              >
                                {pos.side}
                              </span>
                            </td>
                            <td className="py-4 px-6 text-slate-600 font-mono font-medium">{pos.size}</td>
                            <td className="py-4 px-6 text-slate-600 font-mono font-medium">
                              ${pos.entry_price.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                            </td>
                            <td className="py-4 px-6 text-slate-600 font-mono font-medium">
                              ${pos.current_price.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                            </td>
                            <td className={`py-4 px-6 text-right font-mono font-bold ${isProfit ? "text-emerald-700" : "text-rose-600"}`}>
                              {isProfit ? "+" : ""}${pos.pnl.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                              <span className="text-[10px] ml-1.5 opacity-80 font-semibold">
                                ({isProfit ? "+" : ""}{pos.pnl_percent}%)
                              </span>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
