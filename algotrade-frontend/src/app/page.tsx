"use client";

import { useState, useEffect } from "react";
import {
  OverviewData,
  ScoredCandidate,
  AnalyticsData,
  ZerodhaSessionData,
} from "@/components/types";
import { useEngineWebSocket } from "@/hooks/useEngineWebSocket";
import { useAuth } from "@/context/AuthContext";
import { apiFetch } from "@/lib/apiClient";
import LoginOverlay from "@/components/auth/LoginOverlay";
import Header from "@/components/dashboard/Header";
import OverviewTab from "@/components/dashboard/OverviewTab";
import LeaderboardTab from "@/components/dashboard/LeaderboardTab";
import AnalyticsTab from "@/components/dashboard/AnalyticsTab";
import StockExplorerTab from "@/components/explorer/StockExplorerTab";
import DecisionFeedTerminal from "@/components/dashboard/DecisionFeedTerminal";
import BacktestTab from "@/components/backtest/BacktestTab";
import PreMarketScannerTab from "@/components/dashboard/PreMarketScannerTab";

export default function Dashboard() {
  const [activeTab, setActiveTab] = useState<"overview" | "leaderboard" | "scanner" | "analytics" | "explorer" | "decisions" | "backtest">("overview");
  const { isAuthenticated, isLoading, accessToken, logout } = useAuth();
  
  // Real-time bidirectional WebSocket stream (0ms lag, zero HTTP polling)
  const {
    overview,
    setOverview,
    leaderboard,
    setLeaderboard,
    recentDecisions,
    connectionStatus,
    lastUpdated,
    latestTicks,
    subscribeSymbol,
    marketPhase,
  } = useEngineWebSocket(accessToken);

  const [analytics, setAnalytics] = useState<AnalyticsData | null>(null);
  const [switchingMode, setSwitchingMode] = useState(false);
  const [showConfirmModal, setShowConfirmModal] = useState(false);

  // Zerodha Kite session state
  const [zerodhaSession, setZerodhaSession] = useState<ZerodhaSessionData | null>(null);
  const [loginUrl, setLoginUrl] = useState<string | null>(null);
  const [activatingToken, setActivatingToken] = useState(false);
  const [autoLoggingIn, setAutoLoggingIn] = useState(false);
  const [sessionAlert, setSessionAlert] = useState<{ type: "success" | "error"; message: string } | null>(null);

  // Popular stocks
  const [popularStocks, setPopularStocks] = useState<string[]>([
    "RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK",
    "SBIN", "BHARTIARTL", "LT", "AXISBANK", "NTPC",
    "WIPRO", "MARUTI", "SUNPHARMA", "BAJFINANCE"
  ]);

  const BACKEND_URL = process.env.NEXT_PUBLIC_BACKEND_URL || "http://localhost:8080";

  // Historical analytics fetcher (only called on mount or when switching to analytics tab)
  const fetchAnalytics = async () => {
    try {
      const res = await apiFetch("/api/dashboard/analytics");
      if (res.ok) {
        const data = await res.json();
        setAnalytics(data);
      }
    } catch (err) {
      console.error("Analytics fetch error:", err);
    }
  };

  // Zerodha session details fetcher
  const fetchZerodhaStatus = async () => {
    try {
      const res = await apiFetch("/api/dashboard/zerodha/status");
      if (res.ok) {
        const data: ZerodhaSessionData = await res.json();
        setZerodhaSession(data);
      }
    } catch (err) {
      console.error("Zerodha status fetch error:", err);
    }
  };

  // Zerodha login URL fetcher
  const fetchLoginUrl = async () => {
    try {
      const res = await apiFetch("/api/dashboard/zerodha/login-url");
      if (res.ok) {
        const data = await res.json();
        if (data.loginUrl) {
          setLoginUrl(data.loginUrl);
        }
      }
    } catch (err) {
      console.error("Zerodha login URL fetch error:", err);
    }
  };

  // Popular stocks list fetcher
  const fetchPopularStocks = async () => {
    try {
      const res = await apiFetch("/api/dashboard/popular-stocks");
      if (res.ok) {
        const list = await res.json();
        if (Array.isArray(list) && list.length > 0) {
          setPopularStocks(list);
        }
      }
    } catch {
      // Default list persists safely
    }
  };

  // Automated headless login using TOTP
  const handleAutoLogin = async (password?: string, totpSecret?: string) => {
    setAutoLoggingIn(true);
    setSessionAlert(null);
    try {
      const res = await apiFetch("/api/dashboard/zerodha/auto-login", {
        method: "POST",
        body: JSON.stringify({
          password: password && password.trim() ? password.trim() : undefined,
          totpSecret: totpSecret && totpSecret.trim() ? totpSecret.trim() : undefined,
        }),
      });
      const data = await res.json();
      if (res.ok && data.success) {
        setSessionAlert({
          type: "success",
          message: `⚡ Automated 2FA Login Succeeded for ${data.userName || data.userId || "trader"}! Scheduled daily auto-renewal is active.`
        });
        await fetchZerodhaStatus();
      } else {
        setSessionAlert({
          type: "error",
          message: data.error || "Automated login failed. Please verify your credentials or TOTP secret."
        });
      }
    } catch (err: unknown) {
      const errorMessage = err instanceof Error ? err.message : String(err);
      setSessionAlert({
        type: "error",
        message: "Failed to connect to backend engine: " + errorMessage
      });
    } finally {
      setAutoLoggingIn(false);
    }
  };

  // Manual request_token exchange handler
  const handleManualTokenSubmit = async (token: string) => {
    if (!token.trim()) return;
    setActivatingToken(true);
    setSessionAlert(null);
    try {
      const res = await apiFetch(`/api/dashboard/zerodha/session?requestToken=${encodeURIComponent(token.trim())}`, {
        method: "POST"
      });
      const data = await res.json();
      if (res.ok && data.success) {
        setSessionAlert({
          type: "success",
          message: `Zerodha session successfully activated for ${data.userName || data.userId || "trader"}!`
        });
        await fetchZerodhaStatus();
      } else {
        setSessionAlert({
          type: "error",
          message: data.error || "Failed to exchange request_token. Please ensure you copied the fresh token."
        });
      }
    } catch (err: unknown) {
      const errorMessage = err instanceof Error ? err.message : String(err);
      setSessionAlert({
        type: "error",
        message: "Failed to connect to backend engine: " + errorMessage
      });
    } finally {
      setActivatingToken(false);
    }
  };

  // Handle URL redirect query parameters from Zerodha OAuth callback
  useEffect(() => {
    if (typeof window !== "undefined") {
      const params = new URLSearchParams(window.location.search);
      const sessionParam = params.get("session");
      if (sessionParam === "activated") {
        setSessionAlert({
          type: "success",
          message: "Zerodha Kite morning session activated successfully!"
        });
        window.history.replaceState({}, document.title, window.location.pathname);
      } else if (sessionParam === "error") {
        const msg = params.get("msg") || "Authentication failed.";
        setSessionAlert({
          type: "error",
          message: `Kite login error: ${msg}`
        });
        window.history.replaceState({}, document.title, window.location.pathname);
      }
    }
  }, []);

  // Initial load for session and reference data (no periodic polling!)
  useEffect(() => {
    fetchAnalytics();
    fetchZerodhaStatus();
    fetchLoginUrl();
    fetchPopularStocks();
  }, []);

  // Refresh analytics metrics when user navigates to the analytics tab
  useEffect(() => {
    if (activeTab === "analytics") {
      fetchAnalytics();
    }
  }, [activeTab]);

  // Fallback fetch if leaderboard is empty when user navigates to the leaderboard tab
  useEffect(() => {
    if (activeTab === "leaderboard" && leaderboard.length === 0) {
      apiFetch("/api/dashboard/leaderboard")
        .then((res) => (res.ok ? res.json() : []))
        .then((data: any) => {
          if (Array.isArray(data) && data.length > 0) {
            setLeaderboard(data);
            data.slice(0, 10).forEach((c: any) => {
              if (c && c.symbol) subscribeSymbol(c.symbol);
            });
          }
        })
        .catch(() => {});
    }
  }, [activeTab, leaderboard.length, setLeaderboard, subscribeSymbol]);

  // Automatically subscribe to active position symbol over WebSocket as soon as a trade is entered
  useEffect(() => {
    if (overview?.activePosition?.symbol) {
      subscribeSymbol(overview.activePosition.symbol);
    }
  }, [overview?.activePosition?.symbol, subscribeSymbol]);

  // Dynamic fallback polling exclusively while an active position is open (every 3 seconds)
  useEffect(() => {
    if (!overview?.activePosition) return;
    const interval = setInterval(async () => {
      try {
        const res = await apiFetch("/api/dashboard/overview");
        if (res.ok) {
          const fresh = await res.json();
          setOverview((prev) => {
            if (JSON.stringify(prev) === JSON.stringify(fresh)) return prev;
            return fresh;
          });
        }
      } catch {}
    }, 3000);
    return () => clearInterval(interval);
  }, [overview?.activePosition?.symbol, setOverview]);

  const handleToggleMode = async () => {
    if (!overview) return;
    const nextMode = overview.mode === "PAPER" ? "LIVE" : "PAPER";
    setSwitchingMode(true);
    try {
      const res = await apiFetch(`/api/dashboard/mode?mode=${nextMode}`, {
        method: "POST",
      });
      if (!res.ok) {
        const err = await res.json();
        alert(err.error || "Cannot switch mode");
      }
    } catch {
      alert("Failed to reach backend engine. Please check if the Java backend is running.");
    } finally {
      setSwitchingMode(false);
      setShowConfirmModal(false);
    }
  };

  const handleEmergencyExit = async () => {
    if (!confirm("EMERGENCY KILL-SWITCH: Are you sure you want to force exit all open positions immediately?")) {
      return;
    }
    try {
      await apiFetch("/api/dashboard/emergency-exit", { method: "POST" });
    } catch (err) {
      alert("Failed to send emergency exit signal: " + err);
    }
  };

  if (isLoading) {
    return (
      <div className="min-h-screen bg-slate-950 flex flex-col items-center justify-center gap-3">
        <div className="w-8 h-8 border-2 border-emerald-500 border-t-transparent rounded-full animate-spin" />
        <p className="text-xs font-mono text-slate-400">Verifying Operator Session...</p>
      </div>
    );
  }

  if (!isAuthenticated) {
    return <LoginOverlay />;
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans selection:bg-emerald-500 selection:text-black">
      {/* Top Header Bar with Live WebSocket Stream Badge */}
      <Header
        overview={overview}
        session={zerodhaSession}
        wsStatus={connectionStatus}
        activeTab={activeTab}
        setActiveTab={setActiveTab}
        onOpenModeModal={() => setShowConfirmModal(true)}
        onEmergencyExit={handleEmergencyExit}
        onAutoLogin={() => handleAutoLogin()}
        autoLoggingIn={autoLoggingIn}
        onLogout={logout}
      />

      {/* Main Container */}
      <main className="max-w-7xl mx-auto w-full p-4 sm:p-6 flex-1 flex flex-col gap-6">
        {/* Global Session Notification Alert */}
        {sessionAlert && (
          <div
            className={`p-4 rounded-xl flex items-center justify-between gap-3 text-sm border backdrop-blur-sm animate-fadeIn ${
              sessionAlert.type === "success"
                ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-300"
                : "bg-rose-500/10 border-rose-500/30 text-rose-300"
            }`}
          >
            <div className="flex items-center gap-2">
              <span className="text-base">{sessionAlert.type === "success" ? "✅" : "⚠️"}</span>
              <span>{sessionAlert.message}</span>
            </div>
            <button
              onClick={() => setSessionAlert(null)}
              className="text-xs text-slate-400 hover:text-white underline cursor-pointer"
            >
              Dismiss
            </button>
          </div>
        )}

        {/* Backend Disconnected Warning Banner */}
        {connectionStatus === "error" && !overview && (
          <div className="bg-amber-500/10 border border-amber-500/30 rounded-xl p-4 flex items-center gap-3 text-amber-400 text-xs sm:text-sm">
            <span className="text-xl">⚠️</span>
            <div>
              <p className="font-semibold text-white">Java Backend WebSocket Disconnected ({BACKEND_URL})</p>
              <p className="text-slate-400 mt-0.5">
                Waiting for the Java trading engine to connect. All metrics, candidate scores, and trade logs reflect live backend data only.
              </p>
            </div>
          </div>
        )}

        {/* Tab Navigation with cursor-pointer and hover styling */}
        <div className="flex items-center gap-2 border-b border-slate-800 pb-2 overflow-x-auto text-sm">
          <button
            onClick={() => setActiveTab("overview")}
            className={`px-4 py-2 rounded-lg font-medium whitespace-nowrap transition-all cursor-pointer ${
              activeTab === "overview"
                ? "bg-emerald-500 text-slate-950 font-bold shadow-md shadow-emerald-950"
                : "text-slate-400 hover:text-white hover:bg-slate-900"
            }`}
          >
            📊 Active Position
          </button>
          <button
            onClick={() => setActiveTab("leaderboard")}
            className={`px-4 py-2 rounded-lg font-medium whitespace-nowrap transition-all flex items-center gap-2 cursor-pointer ${
              activeTab === "leaderboard"
                ? "bg-emerald-500 text-slate-950 font-bold shadow-md shadow-emerald-950"
                : "text-slate-400 hover:text-white hover:bg-slate-900"
            }`}
          >
            🏆 Top 10 Performers
          </button>
          <button
            onClick={() => setActiveTab("scanner")}
            className={`px-4 py-2 rounded-lg font-medium whitespace-nowrap transition-all flex items-center gap-2 cursor-pointer ${
              activeTab === "scanner"
                ? "bg-amber-500 text-slate-950 font-bold shadow-md shadow-amber-950"
                : "text-slate-400 hover:text-white hover:bg-slate-900"
            }`}
          >
            🌅 Pre-Market
          </button>
          <button
            onClick={() => setActiveTab("analytics")}
            className={`px-4 py-2 rounded-lg font-medium whitespace-nowrap transition-all flex items-center gap-2 cursor-pointer ${
              activeTab === "analytics"
                ? "bg-emerald-500 text-slate-950 font-bold shadow-md shadow-emerald-950"
                : "text-slate-400 hover:text-white hover:bg-slate-900"
            }`}
          >
            💰 Brokerage
          </button>
          <button
            onClick={() => setActiveTab("explorer")}
            className={`px-4 py-2 rounded-lg font-medium whitespace-nowrap transition-all flex items-center gap-2 cursor-pointer ${
              activeTab === "explorer"
                ? "bg-emerald-500 text-slate-950 font-bold shadow-md shadow-emerald-950"
                : "text-slate-400 hover:text-white hover:bg-slate-900"
            }`}
          >
            🔍 Stock Explorer
          </button>
          <button
            onClick={() => setActiveTab("decisions")}
            className={`px-4 py-2 rounded-lg font-medium whitespace-nowrap transition-all flex items-center gap-2 cursor-pointer ${
              activeTab === "decisions"
                ? "bg-emerald-500 text-slate-950 font-bold shadow-md shadow-emerald-950"
                : "text-slate-400 hover:text-white hover:bg-slate-900"
            }`}
          >
            ⚡ Live Engine Decisions
          </button>
          <button
            onClick={() => setActiveTab("backtest")}
            className={`px-4 py-2 rounded-lg font-medium whitespace-nowrap transition-all flex items-center gap-2 cursor-pointer ${
              activeTab === "backtest"
                ? "bg-emerald-500 text-slate-950 font-bold shadow-md shadow-emerald-950"
                : "text-slate-400 hover:text-white hover:bg-slate-900"
            }`}
          >
            🧪 Strategy Backtest
          
          </button>
        </div>

        {/* Tab 1: Overview */}
        {activeTab === "overview" && (
          <OverviewTab
            overview={overview}
            backendUrl={BACKEND_URL}
            latestTick={overview?.activePosition ? latestTicks[overview.activePosition.symbol] : undefined}
          />
        )}

        {/* Tab 2: Leaderboard */}
        {activeTab === "leaderboard" && (
          <LeaderboardTab
            leaderboard={leaderboard}
            lastUpdated={lastUpdated}
            marketPhase={marketPhase}
            backendUrl={BACKEND_URL}
            latestTicks={latestTicks}
            onSelectSymbol={() => {
              setActiveTab("explorer");
            }}
          />
        )}

        {/* Tab: Pre-Market Auction Scanner */}
        {activeTab === "scanner" && <PreMarketScannerTab />}

        {/* Tab 3: Analytics */}
        {activeTab === "analytics" && (
          <AnalyticsTab analytics={analytics} backendUrl={BACKEND_URL} />
        )}

        {/* Tab 4: Stock Explorer */}
        {activeTab === "explorer" && (
          <StockExplorerTab
            backendUrl={BACKEND_URL}
            session={zerodhaSession}
            loginUrl={loginUrl}
            onAutoLogin={handleAutoLogin}
            onManualTokenSubmit={handleManualTokenSubmit}
            activatingToken={activatingToken}
            autoLoggingIn={autoLoggingIn}
            popularStocks={popularStocks}
            latestTicks={latestTicks}
            subscribeSymbol={subscribeSymbol}
          />
        )}

        {/* Tab 5: Live Engine Decisions Terminal Fullscreen (Real-time WebSocket Stream) */}
        {activeTab === "decisions" && (
          <div className="flex flex-col gap-6 animate-fadeIn">
            <DecisionFeedTerminal
              backendUrl={BACKEND_URL}
              streamEvents={recentDecisions}
              wsStatus={connectionStatus}
            />
          </div>
        )}

        {/* Tab 6: Strategy Backtest Lab */}
        {activeTab === "backtest" && <BacktestTab backendUrl={BACKEND_URL} />}
      </main>

      {/* Safety Modal for Paper <-> Live Mode Switching */}
      {showConfirmModal && (
        <div className="fixed inset-0 z-50 bg-black/80 backdrop-blur-sm flex items-center justify-center p-4 animate-fadeIn">
          <div className="bg-slate-900 border border-slate-700 rounded-2xl max-w-md w-full p-6 shadow-2xl">
            <h3 className="text-lg font-bold text-white flex items-center gap-2">
              ⚠️ Switch Execution Venue
            </h3>
            <p className="text-sm text-slate-300 mt-3 leading-relaxed">
              Are you sure you want to switch from{" "}
              <strong className="text-white font-mono">{overview?.mode}</strong> to{" "}
              <strong
                className={
                  overview?.mode === "PAPER"
                    ? "text-rose-400 font-mono"
                    : "text-emerald-400 font-mono"
                }
              >
                {overview?.mode === "PAPER"
                  ? "LIVE REAL MONEY (Zerodha)"
                  : "PAPER TRADING (Simulation)"}
              </strong>
              ?
            </p>
            {overview?.mode === "PAPER" && (
              <p className="text-xs text-rose-300/90 bg-rose-500/10 border border-rose-500/30 rounded-lg p-2.5 mt-3">
                Caution: Switching to LIVE connects to Zerodha Kite Connect. Real capital will be committed on the next qualifying continuous wave breakout signal (09:20 – 14:45 IST)!
              </p>
            )}
            <div className="flex items-center justify-end gap-3 mt-6">
              <button
                onClick={() => setShowConfirmModal(false)}
                className="px-4 py-2 rounded-lg text-sm font-medium text-slate-400 hover:text-white transition cursor-pointer"
              >
                Cancel
              </button>
              <button
                onClick={handleToggleMode}
                disabled={switchingMode}
                className="px-4 py-2 rounded-lg text-sm font-bold bg-emerald-500 hover:bg-emerald-400 text-slate-950 transition cursor-pointer disabled:opacity-50"
              >
                {switchingMode ? "Switching..." : "Confirm Switch"}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Footer */}
      <footer className="border-t border-slate-900 bg-slate-950 px-4 py-3 text-center text-xs text-slate-600">
        AlgoTrade Automated Engine • Ajay Negi
      </footer>
    </div>
  );
}
