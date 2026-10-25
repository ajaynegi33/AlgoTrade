"use client";

import React, { useEffect } from "react";
import Link from "next/link";
import { useTradingStore } from "@/store/useTradingStore";
import { useEngineWebSocket } from "@/hooks/useEngineWebSocket";
import { getKiteLoginURL } from "@/lib/api";
import { toast } from "sonner";
import { DashboardOverviewSkeleton } from "@/components/ui/Skeleton";
import { GlobalMarketSessionBar } from "@/components/dashboard/GlobalMarketSessionBar";
import { ExecutionTopBar } from "@/components/dashboard/ExecutionTopBar";
import { LiveActiveTrades } from "@/components/dashboard/LiveActiveTrades";
import { CandidateGateInspector } from "@/components/dashboard/CandidateGateInspector";

export default function DashboardOverviewPage() {
  const {
    kiteStatus,
    universe,
    executionState,
    isKiteLoading,
    isUniverseLoading,
    isExecutionUpdating,
    fetchKiteStatus,
    fetchKiteProfile,
    fetchUniverse,
    fetchExecutionState,
    updateSettings,
    setPaperCapital,
    flattenPosition,
  } = useTradingStore();

  const { snapshot, isConnected } = useEngineWebSocket();

  useEffect(() => {
    fetchKiteStatus();
    fetchUniverse();
    fetchKiteProfile();
    fetchExecutionState();
  }, [fetchKiteStatus, fetchUniverse, fetchKiteProfile, fetchExecutionState]);

  const liveExecution = snapshot?.execution ?? executionState;

  const handleConnectKite = async () => {
    try {
      const loginUrl = await getKiteLoginURL();
      window.location.href = loginUrl;
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Failed to obtain Kite login URL";
      toast.error(message);
    }
  };

  const isInitialLoading = !kiteStatus && !universe && (isKiteLoading || isUniverseLoading);
  if (isInitialLoading) {
    return <DashboardOverviewSkeleton />;
  }

  const isKiteConnected = kiteStatus?.is_connected ?? false;
  const totalStocks = universe?.total_stocks ?? 213;
  const lastScanDate = universe?.scan_date ?? "Today";

  return (
    <div className="space-y-6">
      {/* =========================================================================
          1. Global Market Session Clock & Autonomous Execution Cockpit
          ========================================================================= */}
      <GlobalMarketSessionBar
        sessionClock={liveExecution?.session_clock}
        marketRegime={snapshot?.market_pulse?.market_regime}
        macroShockActive={liveExecution?.macro_shock_active}
      />

      <ExecutionTopBar
        executionState={liveExecution}
        isUpdating={isExecutionUpdating}
        onUpdateSettings={updateSettings}
        onSavePaperCapital={setPaperCapital}
        onFlattenAll={() => flattenPosition(undefined, true)}
      />

      <LiveActiveTrades
        activePositions={liveExecution?.active_positions}
        closedPositions={liveExecution?.closed_positions}
        isUpdating={isExecutionUpdating}
        onFlattenPosition={(positionId) => flattenPosition(positionId, false)}
      />

      <CandidateGateInspector
        candidates={liveExecution?.top_candidate_gates}
      />

      {/* =========================================================================
          2. Compact System Navigation & Infrastructure Health Strip
          ========================================================================= */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-5">
        {/* Card 1: Zerodha Kite Connect */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs flex flex-col justify-between hover:border-slate-300 transition-colors">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Broker &amp; Margin Gateway
              </span>
              {isKiteConnected ? (
                <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-bold bg-emerald-50 text-emerald-800 border border-emerald-300/60 font-mono">
                  <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
                  CONNECTED
                </span>
              ) : (
                <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-bold bg-amber-50 text-amber-800 border border-amber-300/60 font-mono">
                  <span className="w-2 h-2 rounded-full bg-amber-500" />
                  OFFLINE
                </span>
              )}
            </div>

            <div className="text-lg font-black text-slate-900 mt-2.5 tracking-tight">
              {isKiteConnected ? (
                <span className="font-mono">{kiteStatus?.user_id}</span>
              ) : (
                "Session Disconnected"
              )}
            </div>

            <p className="text-xs text-slate-500 mt-1 leading-relaxed">
              {isKiteConnected
                ? `Daily token active for ${kiteStatus?.user_name || "trader"}. Valid until tomorrow 06:00 AM IST.`
                : "Login to Zerodha Kite for live tick streaming and real-money order routing."}
            </p>
          </div>

          <div className="mt-4 pt-3.5 border-t border-slate-100 flex items-center justify-between">
            {isKiteConnected ? (
              <Link
                href="/profile"
                className="text-xs font-bold text-emerald-700 hover:text-emerald-800 flex items-center gap-1 transition-colors"
              >
                <span>Open Broker &amp; Margins</span>
                <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" d="M9 5l7 7-7 7" />
                </svg>
              </Link>
            ) : (
              <button
                type="button"
                onClick={handleConnectKite}
                className="text-xs font-bold text-white bg-emerald-600 hover:bg-emerald-700 px-3.5 py-2 rounded-xl transition-all cursor-pointer shadow-xs active:scale-95 flex items-center gap-1.5"
              >
                <span>Login with Kite</span>
              </button>
            )}
            <span className="text-[10px] font-mono text-slate-400">NSE EQ MIS (5x)</span>
          </div>
        </div>

        {/* Card 2: Trade History, Tax Audit & Analytics */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs flex flex-col justify-between hover:border-slate-300 transition-colors">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Post-Trade &amp; Tax Audit
              </span>
              <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-bold bg-amber-50 text-amber-900 border border-amber-300/60 font-mono">
                CONTRACT NOTE
              </span>
            </div>

            <div className="text-lg font-black text-slate-900 mt-2.5 font-mono tracking-tight">
              Trade &amp; Tax Ledger
            </div>

            <p className="text-xs text-slate-500 mt-1 leading-relaxed">
              Full trade history, itemized 6-part Indian statutory taxes, R-capture analytics, custom column filters, and CSV/JSON export.
            </p>
          </div>

          <div className="mt-4 pt-3.5 border-t border-slate-100 flex items-center justify-between">
            <Link
              href="/trades"
              className="text-xs font-bold text-emerald-700 hover:text-emerald-800 flex items-center gap-1 transition-colors"
            >
              <span>Open Trade &amp; Tax Ledger</span>
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" d="M9 5l7 7-7 7" />
              </svg>
            </Link>
            <span className="text-[10px] font-mono text-slate-400">CSV / JSON Export</span>
          </div>
        </div>

        {/* Card 3: Quant Engine & 213-Stock Universe */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs flex flex-col justify-between hover:border-slate-300 transition-colors">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Quant Engine &amp; Universe
              </span>
              <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-bold bg-emerald-50 text-emerald-800 border border-emerald-300/60 font-mono">
                <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
                {isConnected ? "STREAMING" : "STANDBY"}
              </span>
            </div>

            <div className="text-lg font-black text-slate-900 mt-2.5 font-mono tracking-tight">
              {isUniverseLoading ? "..." : `${totalStocks} F&O Stocks`}
            </div>

            <p className="text-xs text-slate-500 mt-1 leading-relaxed">
              Single-writer lock-free actor ranking all {totalStocks} NSE F&amp;O cash equities with Time-of-Day RVOL and anti-spoof depth.
            </p>
          </div>

          <div className="mt-4 pt-3.5 border-t border-slate-100 flex items-center justify-between">
            <Link
              href="/leaderboard"
              className="text-xs font-bold text-emerald-700 hover:text-emerald-800 flex items-center gap-1 transition-colors"
            >
              <span>Open Live Rank Matrix</span>
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" d="M9 5l7 7-7 7" />
              </svg>
            </Link>
            <span className="text-[10px] font-mono text-slate-400">Synced {lastScanDate}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
