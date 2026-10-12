"use client";

import { useState, useEffect } from "react";
import {
  OverviewData,
  ScoredCandidate,
  AnalyticsData,
  ZerodhaSessionData,
} from "@/components/types";
import Header from "@/components/dashboard/Header";
import OverviewTab from "@/components/dashboard/OverviewTab";
import LeaderboardTab from "@/components/dashboard/LeaderboardTab";
import AnalyticsTab from "@/components/dashboard/AnalyticsTab";
import StockExplorerTab from "@/components/explorer/StockExplorerTab";
import DecisionFeedTerminal from "@/components/dashboard/DecisionFeedTerminal";
import BacktestTab from "@/components/backtest/BacktestTab";

export default function Dashboard() {
  const [activeTab, setActiveTab] = useState<"overview" | "leaderboard" | "analytics" | "explorer" | "decisions" | "backtest">("overview");
  const [overview, setOverview] = useState<OverviewData | null>(null);
  const [leaderboard, setLeaderboard] = useState<ScoredCandidate[]>([]);
  const [analytics, setAnalytics] = useState<AnalyticsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [switchingMode, setSwitchingMode] = useState(false);
  const [showConfirmModal, setShowConfirmModal] = useState(false);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);

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

  // Data fetcher connecting to the Java backend
  const fetchData = async () => {
    try {
      const [resOverview, resLeaderboard, resAnalytics] = await Promise.all([
        fetch(`${BACKEND_URL}/api/dashboard/overview`).catch(() => null),
        fetch(`${BACKEND_URL}/api/dashboard/leaderboard`).catch(() => null),
        fetch(`${BACKEND_URL}/api/dashboard/analytics`).catch(() => null),
      ]);

      if (resOverview && resOverview.ok) {
        const data = await resOverview.json();
        setOverview(data);
      }

      if (resLeaderboard && resLeaderboard.ok) {
        const data = await resLeaderboard.json();
        setLeaderboard(data);
      }

      if (resAnalytics && resAnalytics.ok) {
        const data = await resAnalytics.json();
        setAnalytics(data);
      }

      setLastUpdated(new Date());
    } catch (err) {
      console.error("Dashboard fetch error:", err);
    } finally {
      setLoading(false);
    }
  };

  // Zerodha session details fetcher
  const fetchZerodhaStatus = async () => {
    try {
      const res = await fetch(`${BACKEND_URL}/api/dashboard/zerodha/status`);
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
      const res = await fetch(`${BACKEND_URL}/api/dashboard/zerodha/login-url`);
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
      const res = await fetch(`${BACKEND_URL}/api/dashboard/popular-stocks`);
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
      const res = await fetch(`${BACKEND_URL}/api/dashboard/zerodha/auto-login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
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
      const res = await fetch(`${BACKEND_URL}/api/dashboard/zerodha/session?requestToken=${encodeURIComponent(token.trim())}`, {
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

  // Periodic polling for overview, leaderboard, analytics & Zerodha status
  useEffect(() => {
    fetchData();
    fetchZerodhaStatus();
    fetchLoginUrl();
    fetchPopularStocks();
    const interval = setInterval(() => {
      fetchData();
      fetchZerodhaStatus();
    }, 3000);
    return () => clearInterval(interval);
  }, []);

  const handleToggleMode = async () => {
    if (!overview) return;
    const nextMode = overview.mode === "PAPER" ? "LIVE" : "PAPER";
    setSwitchingMode(true);
    try {
      const res = await fetch(`${BACKEND_URL}/api/dashboard/mode?mode=${nextMode}`, {
        method: "POST",
      });
      if (res.ok) {
        await fetchData();
      } else {
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
      await fetch(`${BACKEND_URL}/api/dashboard/emergency-exit`, { method: "POST" });
      fetchData();
    } catch (err) {
      alert("Failed to send emergency exit signal: " + err);
    }
  };

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans selection:bg-emerald-500 selection:text-black">
      {/* Top Header Bar */}
      <Header
        overview={overview}
        session={zerodhaSession}
        activeTab={activeTab}
        setActiveTab={setActiveTab}
        onOpenModeModal={() => setShowConfirmModal(true)}
        onEmergencyExit={handleEmergencyExit}
        onAutoLogin={() => handleAutoLogin()}
        autoLoggingIn={autoLoggingIn}
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
        {!overview && !loading && (
          <div className="bg-amber-500/10 border border-amber-500/30 rounded-xl p-4 flex items-center gap-3 text-amber-400 text-xs sm:text-sm">
            <span className="text-xl">⚠️</span>
            <div>
              <p className="font-semibold text-white">Java Backend Offline ({BACKEND_URL})</p>
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
            <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-slate-800 text-slate-300 font-mono">
              09:30
            </span>
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
            <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
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
            <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-slate-800 text-emerald-400 border border-emerald-500/30 font-mono">
              NEW
            </span>
          </button>
        </div>

        {/* Tab 1: Overview */}
        {activeTab === "overview" && <OverviewTab overview={overview} backendUrl={BACKEND_URL} />}

        {/* Tab 2: Leaderboard */}
        {activeTab === "leaderboard" && (
          <LeaderboardTab
            leaderboard={leaderboard}
            lastUpdated={lastUpdated}
            onSelectSymbol={() => {
              setActiveTab("explorer");
            }}
          />
        )}

        {/* Tab 3: Analytics */}
        {activeTab === "analytics" && <AnalyticsTab analytics={analytics} />}

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
          />
        )}

        {/* Tab 5: Live Engine Decisions Terminal Fullscreen */}
        {activeTab === "decisions" && (
          <div className="flex flex-col gap-6 animate-fadeIn">
            <DecisionFeedTerminal backendUrl={BACKEND_URL} />
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
                Caution: Switching to LIVE connects to Zerodha Kite Connect. Real capital will be committed on the next 09:30 AM execution!
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
