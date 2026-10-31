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
          2. High-Signal Infrastructure & Session Telemetry Footer Strip
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-2xl p-3.5 px-5 shadow-2xs font-mono text-xs flex flex-wrap items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-4 text-slate-600">
          <div className="flex items-center gap-2">
            <span className={`w-2 h-2 rounded-full ${isKiteConnected ? "bg-emerald-500 animate-pulse" : "bg-amber-500"}`} />
            <span className="font-bold text-slate-900">
              Kite: {isKiteConnected ? kiteStatus?.user_id : "DISCONNECTED"}
            </span>
            {isKiteConnected ? (
              <span className="text-[10px] text-slate-400">
                (Valid till 06:00 AM IST)
              </span>
            ) : (
              <button
                type="button"
                onClick={handleConnectKite}
                className="text-[10px] font-bold text-emerald-700 hover:underline cursor-pointer"
              >
                [Login Now]
              </button>
            )}
          </div>

          <span className="text-slate-300">•</span>

          <div className="flex items-center gap-1.5">
            <span className={`w-2 h-2 rounded-full ${isConnected ? "bg-emerald-500" : "bg-slate-400"}`} />
            <span>Engine: <strong>{isConnected ? "STREAMING 1Hz" : "OFFLINE"}</strong></span>
            <span className="text-slate-400">({totalStocks} Cash Equities)</span>
          </div>

          <span className="text-slate-300">•</span>

          <span className="text-slate-500">
            Regime: <strong className="text-slate-900">Cash MIS 5x Long-Only</strong> ({lastScanDate})
          </span>
        </div>

        <div className="flex items-center gap-4 text-[11px] font-bold">
          <Link
            href="/trades"
            className="text-emerald-700 hover:text-emerald-800 hover:underline transition-colors"
          >
            Trade &amp; Tax Ledger →
          </Link>
          <Link
            href="/profile"
            className="text-slate-500 hover:text-slate-800 hover:underline transition-colors"
          >
            Broker Settings →
          </Link>
        </div>
      </div>
    </div>
  );
}
