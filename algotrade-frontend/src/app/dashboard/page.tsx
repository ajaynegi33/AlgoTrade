"use client";

import React, { useEffect, useState, useCallback } from "react";
import AuthGuard from "../../components/AuthGuard";
import { useAuth } from "../../context/AuthContext";
import { getUserProfile, setAccessToken } from "../../lib/api";
import { ProfileResponseData } from "../../lib/types";

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
    } catch (err: unknown) {
      console.error("[Dashboard] Reload error:", err);
      if (err instanceof Error) {
        setError(err.message);
      } else {
        setError("Failed to load user profile. Please check the backend connection.");
      }
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
      setTestStatus("✅ Silent refresh successful! New 5-minute access token acquired seamlessly.");
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Silent refresh failed";
      setTestStatus(`❌ Refresh failed: ${msg}`);
    } finally {
      setIsRefreshing(false);
      setTimeout(() => setTestStatus(null), 6000);
    }
  };

  const handleLogout = async () => {
    try {
      await logout();
    } catch (err) {
      console.error("Logout error:", err);
    }
  };

  return (
    <div className="min-h-screen bg-zinc-950 text-zinc-100 flex flex-col">
      {/* Top Navigation */}
      <header className="border-b border-zinc-800/80 bg-zinc-900/60 backdrop-blur-md sticky top-0 z-30">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 h-16 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="w-9 h-9 rounded-xl bg-gradient-to-tr from-emerald-500 to-cyan-500 flex items-center justify-center font-bold text-zinc-950 text-base shadow-lg shadow-emerald-500/20">
              AT
            </div>
            <div>
              <div className="font-semibold text-sm leading-none text-zinc-100">AlgoTrade</div>
              <div className="text-[11px] text-zinc-400 mt-1">Terminal Dashboard</div>
            </div>
          </div>

          <div className="flex items-center gap-4">
            <div className="hidden sm:flex items-center gap-2 text-xs bg-zinc-800/60 border border-zinc-700/50 rounded-full px-3 py-1 text-zinc-300">
              <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse" />
              <span>{user?.email}</span>
              <span className="text-zinc-500">•</span>
              <span className="text-emerald-400 font-medium">{user?.role}</span>
            </div>

            <button
              onClick={handleLogout}
              className="text-xs font-medium text-zinc-400 hover:text-red-400 px-3 py-1.5 rounded-lg hover:bg-red-500/10 border border-zinc-800 hover:border-red-500/20 transition-all cursor-pointer"
            >
              Sign Out
            </button>
          </div>
        </div>
      </header>

      {/* Main Content Area */}
      <main className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 lg:px-8 py-8 space-y-6">
        {/* Token Verification & Silent Refresh Test Banner */}
        <div className="bg-zinc-900/80 border border-zinc-800 rounded-2xl p-4 sm:p-5 flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <span className="px-2 py-0.5 rounded text-[11px] font-semibold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                JWT Dual-Token Active
              </span>
              <span className="text-xs text-zinc-400">
                Access Token: <strong>5 mins</strong> | Refresh Cookie: <strong>24 hours</strong>
              </span>
            </div>
            <p className="text-xs text-zinc-500 mt-1.5">
              Verify automatic token refresh when the 5-min access token expires.
            </p>
          </div>

          <div className="flex items-center gap-2 w-full md:w-auto">
            <button
              onClick={handleSimulateTokenExpiry}
              disabled={isRefreshing || loading}
              className="w-full md:w-auto text-xs font-semibold px-4 py-2 rounded-xl bg-zinc-800 hover:bg-zinc-700 border border-zinc-700 text-zinc-200 transition-all disabled:opacity-50 cursor-pointer flex items-center justify-center gap-2"
            >
              {isRefreshing ? (
                <>
                  <div className="w-3.5 h-3.5 border-2 border-zinc-400/20 border-t-zinc-200 rounded-full animate-spin" />
                  <span>Refreshing...</span>
                </>
              ) : (
                <span>Test Silent 401 Refresh</span>
              )}
            </button>

            <button
              onClick={handleReload}
              disabled={loading}
              className="text-xs font-semibold px-4 py-2 rounded-xl bg-emerald-500/10 hover:bg-emerald-500/20 border border-emerald-500/20 text-emerald-400 transition-all disabled:opacity-50 cursor-pointer"
            >
              {loading ? "Loading..." : "Reload Data"}
            </button>
          </div>
        </div>

        {/* Test Result Toast/Banner */}
        {testStatus && (
          <div className="p-3.5 rounded-xl text-xs font-medium bg-zinc-900 border border-zinc-700 text-zinc-200 animate-in fade-in flex items-center gap-2">
            <span>{testStatus}</span>
          </div>
        )}

        {/* Error Handling State */}
        {error && (
          <div className="p-6 rounded-2xl bg-red-500/10 border border-red-500/20 text-red-300 space-y-3">
            <div className="flex items-center gap-3">
              <svg className="w-6 h-6 text-red-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <div>
                <h3 className="font-semibold text-sm">Failed to retrieve dummy user data</h3>
                <p className="text-xs text-red-400/90 mt-0.5">{error}</p>
              </div>
            </div>
            <button
              onClick={handleReload}
              className="px-4 py-2 rounded-xl bg-red-500/20 hover:bg-red-500/30 text-xs font-semibold text-red-200 transition-all cursor-pointer"
            >
              Retry Connection
            </button>
          </div>
        )}

        {/* Loading Skeleton */}
        {loading && !profileData && (
          <div className="grid grid-cols-1 md:grid-cols-4 gap-4 animate-pulse">
            {[1, 2, 3, 4].map((i) => (
              <div key={i} className="h-32 rounded-2xl bg-zinc-900 border border-zinc-800" />
            ))}
          </div>
        )}

        {/* User Data Dashboard Display */}
        {profileData && (
          <div className="space-y-6">
            {/* Top User Profile Header Card */}
            <div className="bg-zinc-900/60 border border-zinc-800 rounded-2xl p-6 backdrop-blur-sm">
              <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
                <div className="flex items-center gap-4">
                  <div className="w-14 h-14 rounded-2xl bg-gradient-to-br from-zinc-800 to-zinc-900 border border-zinc-700/50 flex items-center justify-center text-xl font-bold text-emerald-400">
                    {profileData.profile.name.charAt(0)}
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <h2 className="text-xl font-semibold text-zinc-100">
                        {profileData.profile.name}
                      </h2>
                      <span className="text-[10px] uppercase font-bold tracking-wider px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                        {profileData.profile.tier}
                      </span>
                      {profileData.profile.kyc_verified && (
                        <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-blue-500/10 text-blue-400 border border-blue-500/20">
                          KYC Verified
                        </span>
                      )}
                    </div>
                    <div className="text-xs text-zinc-400 mt-1 flex flex-wrap gap-x-4 gap-y-1">
                      <span>ID: <code className="text-zinc-300 font-mono">{profileData.profile.id}</code></span>
                      <span>Account: <code className="text-zinc-300 font-mono">{profileData.portfolio_summary.account_number}</code></span>
                      <span>Email: <code className="text-zinc-300 font-mono">{profileData.profile.email}</code></span>
                    </div>
                  </div>
                </div>

                <div className="text-right">
                  <span className="text-xs text-zinc-500">Account Status</span>
                  <div className="text-sm font-semibold text-emerald-400 flex items-center md:justify-end gap-1.5 mt-0.5">
                    <span className="w-2 h-2 rounded-full bg-emerald-400" />
                    {profileData.profile.account_status}
                  </div>
                </div>
              </div>
            </div>

            {/* Financial Summary Cards */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
              <div className="bg-zinc-900/60 border border-zinc-800 rounded-2xl p-5">
                <span className="text-xs font-medium text-zinc-400">Total Equity</span>
                <div className="text-2xl font-bold tracking-tight text-zinc-100 mt-1 font-mono">
                  ${profileData.portfolio_summary.total_equity.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                </div>
                <div className="text-xs text-zinc-500 mt-1">Currency: {profileData.portfolio_summary.currency}</div>
              </div>

              <div className="bg-zinc-900/60 border border-zinc-800 rounded-2xl p-5">
                <span className="text-xs font-medium text-zinc-400">Available Cash</span>
                <div className="text-2xl font-bold tracking-tight text-zinc-100 mt-1 font-mono">
                  ${profileData.portfolio_summary.cash_balance.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                </div>
                <div className="text-xs text-emerald-400 mt-1">Ready for deployment</div>
              </div>

              <div className="bg-zinc-900/60 border border-zinc-800 rounded-2xl p-5">
                <span className="text-xs font-medium text-zinc-400">Today&apos;s Unrealized P&amp;L</span>
                <div className="text-2xl font-bold tracking-tight text-emerald-400 mt-1 font-mono">
                  +${profileData.portfolio_summary.unrealized_pnl.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                </div>
                <div className="text-xs text-emerald-400 mt-1">
                  +{profileData.portfolio_summary.unrealized_pnl_percent}% overall gain
                </div>
              </div>

              <div className="bg-zinc-900/60 border border-zinc-800 rounded-2xl p-5">
                <span className="text-xs font-medium text-zinc-400">Active Algo Bots</span>
                <div className="text-2xl font-bold tracking-tight text-cyan-400 mt-1 font-mono">
                  {profileData.portfolio_summary.active_bots_count}
                </div>
                <div className="text-xs text-zinc-500 mt-1">Grid &amp; Momentum strategies</div>
              </div>
            </div>

            {/* Active Trading Positions Table */}
            <div className="bg-zinc-900/60 border border-zinc-800 rounded-2xl overflow-hidden">
              <div className="px-6 py-4 border-b border-zinc-800 flex items-center justify-between">
                <div>
                  <h3 className="font-semibold text-sm text-zinc-100">Active Live Positions</h3>
                  <p className="text-xs text-zinc-500 mt-0.5">Real-time mock positions delivered from Go backend</p>
                </div>
                <span className="text-xs font-mono text-zinc-400">
                  {profileData.active_positions.length} Open Trades
                </span>
              </div>

              <div className="overflow-x-auto">
                <table className="w-full text-left border-collapse text-xs">
                  <thead>
                    <tr className="border-b border-zinc-800 text-zinc-400 uppercase tracking-wider font-medium text-[11px] bg-zinc-950/40">
                      <th className="py-3 px-6">Market</th>
                      <th className="py-3 px-6">Side</th>
                      <th className="py-3 px-6">Size</th>
                      <th className="py-3 px-6">Entry Price</th>
                      <th className="py-3 px-6">Mark Price</th>
                      <th className="py-3 px-6 text-right">PnL (USD)</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-800/60">
                    {profileData.active_positions.map((pos) => {
                      const isProfit = pos.pnl >= 0;
                      return (
                        <tr key={pos.symbol} className="hover:bg-zinc-800/30 transition-colors">
                          <td className="py-4 px-6 font-semibold text-zinc-100 font-mono">
                            {pos.symbol}
                          </td>
                          <td className="py-4 px-6">
                            <span
                              className={`px-2 py-0.5 rounded text-[10px] font-bold tracking-wider ${
                                pos.side === "LONG"
                                  ? "bg-emerald-500/10 text-emerald-400 border border-emerald-500/20"
                                  : "bg-red-500/10 text-red-400 border border-red-500/20"
                              }`}
                            >
                              {pos.side}
                            </span>
                          </td>
                          <td className="py-4 px-6 text-zinc-300 font-mono">{pos.size}</td>
                          <td className="py-4 px-6 text-zinc-300 font-mono">
                            ${pos.entry_price.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                          </td>
                          <td className="py-4 px-6 text-zinc-300 font-mono">
                            ${pos.current_price.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                          </td>
                          <td className={`py-4 px-6 text-right font-mono font-semibold ${isProfit ? "text-emerald-400" : "text-red-400"}`}>
                            {isProfit ? "+" : ""}${pos.pnl.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                            <span className="text-[10px] ml-1.5 opacity-80">
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
  );
}
