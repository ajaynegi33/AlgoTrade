"use client";

import { OverviewData, ZerodhaSessionData } from "../types";

interface HeaderProps {
  overview: OverviewData | null;
  session: ZerodhaSessionData | null;
  activeTab: "overview" | "leaderboard" | "analytics" | "explorer";
  setActiveTab: (tab: "overview" | "leaderboard" | "analytics" | "explorer") => void;
  onOpenModeModal: () => void;
  onEmergencyExit: () => void;
  onAutoLogin: () => Promise<void>;
  autoLoggingIn: boolean;
}

export default function Header({
  overview,
  session,
  activeTab,
  setActiveTab,
  onOpenModeModal,
  onEmergencyExit,
  onAutoLogin,
  autoLoggingIn,
}: HeaderProps) {
  return (
    <header className="border-b border-slate-800 bg-slate-900/80 backdrop-blur sticky top-0 z-40 px-4 py-3 sm:px-6">
      <div className="max-w-7xl mx-auto flex flex-col sm:flex-row items-center justify-between gap-3">
        <div className="flex items-center gap-3 w-full sm:w-auto justify-between sm:justify-start">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-lg bg-emerald-500/10 border border-emerald-500/30 flex items-center justify-center text-emerald-400 font-bold text-lg">
              ⚡
            </div>
            <div>
              <h1 className="text-base sm:text-lg font-bold tracking-tight text-white flex items-center gap-2">
                AlgoTrade <span className="text-xs px-1.5 py-0.5 rounded bg-slate-800 border border-slate-700 text-slate-400 font-mono">v1.0</span>
              </h1>
              <p className="text-[10px] sm:text-xs text-slate-400">Automated NSE Intraday Engine</p>
            </div>
          </div>

          {/* Status Pill (Mobile) */}
          <div className="sm:hidden flex items-center gap-1.5">
            <span className={`px-2 py-0.5 rounded-full text-[11px] font-semibold uppercase tracking-wider ${
              overview?.status === "IN_TRADE" ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/40 animate-pulse" :
              overview?.status === "OBSERVING" ? "bg-sky-500/20 text-sky-400 border border-sky-500/40" :
              overview?.status === "DAY_COMPLETED" ? "bg-purple-500/20 text-purple-400 border border-purple-500/40" :
              overview?.status === "MARKET_CLOSED" ? "bg-amber-500/20 text-amber-400 border border-amber-500/40" :
              "bg-slate-800 text-slate-400 border border-slate-700"
            }`}>
              ● {overview ? (overview.status === "MARKET_CLOSED" ? "CLOSED" : overview.status) : "DISCONNECTED"}
            </span>
          </div>
        </div>

        <div className="flex items-center gap-2 sm:gap-3 w-full sm:w-auto justify-between sm:justify-end flex-wrap">
          {/* Zerodha Kite Authentication Pill */}
          <div className="flex items-center">
            {session?.active ? (
              <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold font-mono bg-emerald-500/10 text-emerald-400 border border-emerald-500/30">
                <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
                Kite: {session.userName || session.userId || "Connected"} (Auto)
              </span>
            ) : session?.autoLoginConfigured ? (
              <button
                onClick={() => onAutoLogin()}
                disabled={autoLoggingIn}
                className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold font-mono bg-amber-500/10 hover:bg-amber-500/20 text-amber-300 border border-amber-500/30 transition cursor-pointer"
                title="Click to trigger headless TOTP auto-login immediately"
              >
                <span className="w-2 h-2 rounded-full bg-amber-400 animate-ping"></span>
                {autoLoggingIn ? "Auto-Logging in..." : "⚡ Kite: Auto-Login"}
              </button>
            ) : (
              <button
                onClick={() => setActiveTab("explorer")}
                className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold font-mono bg-amber-500/10 hover:bg-amber-500/20 text-amber-400 border border-amber-500/30 transition cursor-pointer"
              >
                <span className="w-2 h-2 rounded-full bg-amber-400"></span>
                Kite: Offline (Auto-Login)
              </button>
            )}
          </div>

          {/* Market Status Pill (Desktop) */}
          <span className="hidden sm:inline-block px-3 py-1 rounded-full text-xs font-semibold uppercase tracking-wider bg-slate-800/80 text-slate-300 border border-slate-700">
            <span className={`inline-block w-2 h-2 rounded-full mr-1.5 ${
              overview?.status === "IN_TRADE" ? "bg-emerald-400 animate-ping" :
              overview?.status === "OBSERVING" ? "bg-sky-400 animate-pulse" :
              overview?.status === "DAY_COMPLETED" ? "bg-purple-400" :
              overview?.status === "MARKET_CLOSED" ? "bg-amber-400" :
              "bg-slate-500"
            }`}></span>
            Status: {overview ? (overview.status === "MARKET_CLOSED" ? `MARKET CLOSED (${overview.nextMarketOpen || "Opens 09:15 AM"})` : overview.status) : "DISCONNECTED"}
          </span>

          {/* Paper vs Live Toggle Button */}
          <button
            onClick={onOpenModeModal}
            disabled={!overview}
            className={`flex items-center gap-2 px-3 py-1.5 rounded-lg border text-xs sm:text-sm font-semibold transition-all cursor-pointer ${
              !overview
                ? "bg-slate-800/50 border-slate-700 text-slate-500 cursor-not-allowed"
                : overview.mode === "LIVE"
                ? "bg-rose-500/10 border-rose-500/40 text-rose-400 hover:bg-rose-500/20 shadow-lg shadow-rose-950/50"
                : "bg-emerald-500/10 border-emerald-500/40 text-emerald-400 hover:bg-emerald-500/20"
            }`}
          >
            <span className="w-2 h-2 rounded-full bg-current"></span>
            {overview ? (overview.mode === "LIVE" ? "REAL MONEY" : "PAPER TRADING") : "NO BACKEND"}
            {overview && <span className="text-[10px] opacity-75 underline ml-1">Switch</span>}
          </button>

          {/* Emergency Kill-Switch */}
          {overview?.status === "IN_TRADE" && (
            <button
              onClick={onEmergencyExit}
              className="bg-rose-600 hover:bg-rose-700 text-white font-bold px-3 py-1.5 rounded-lg text-xs tracking-wider uppercase transition shadow-md shadow-rose-900/50 cursor-pointer"
            >
              Exit Now
            </button>
          )}
        </div>
      </div>
    </header>
  );
}
