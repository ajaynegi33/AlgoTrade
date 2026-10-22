"use client";

import React, { useEffect } from "react";
import Link from "next/link";
import { useTradingStore } from "@/store/useTradingStore";
import { getKiteLoginURL } from "@/lib/api";
import { DashboardOverviewSkeleton } from "@/components/ui/Skeleton";
import { toast } from "sonner";

export default function DashboardOverviewPage() {
  const {
    kiteStatus,
    universe,
    isKiteLoading,
    isUniverseLoading,
    fetchKiteStatus,
    fetchUniverse,
  } = useTradingStore();

  useEffect(() => {
    fetchKiteStatus();
    fetchUniverse();
  }, [fetchKiteStatus, fetchUniverse]);

  const handleConnectKite = async () => {
    try {
      const loginUrl = await getKiteLoginURL();
      window.location.href = loginUrl;
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Failed to retrieve Kite login URL");
    }
  };

  const isInitialLoading = !kiteStatus && !universe && (isKiteLoading || isUniverseLoading);
  if (isInitialLoading) {
    return <DashboardOverviewSkeleton />;
  }

  const isKiteConnected = kiteStatus?.is_connected;
  const totalStocks = universe?.total_stocks ?? 0;
  const lastScanDate = universe?.scan_date ? new Date(universe.scan_date).toLocaleTimeString("en-IN", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: true,
  }) : "08:30 AM IST";

  return (
    <div className="space-y-6">
      {/* =========================================================================
          1. Hero / Engine Readiness Banner
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-6 sm:p-8 shadow-xs relative overflow-hidden">
        <div className="absolute top-0 right-0 w-80 h-80 bg-gradient-to-bl from-emerald-50 via-teal-50/40 to-transparent rounded-full blur-2xl pointer-events-none -mr-20 -mt-20" />

        <div className="relative z-10 flex flex-col md:flex-row md:items-center justify-between gap-6">
          <div>
            <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs font-mono font-bold bg-emerald-50 text-emerald-800 border border-emerald-200/80 mb-3">
              <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
              <span>Phase 1 • Scanner & Kite Auth Active</span>
            </div>

            <h1 className="text-2xl sm:text-3xl font-black text-slate-900 tracking-tight">
              Trading Engine Overview
            </h1>
            <p className="text-xs sm:text-sm text-slate-600 max-w-2xl mt-1.5 leading-relaxed">
              Automated high-liquidity National Stock Exchange (NSE) cash equities scanner. Filtered strictly from active F&O underlying contracts for intraday execution.
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-3 shrink-0">
            <Link
              href="/universe"
              className="text-xs font-bold px-4 py-2.5 rounded-2xl bg-slate-900 hover:bg-slate-800 text-white transition-all shadow-xs active:scale-[0.98] flex items-center gap-2"
            >
              <span>View Stock Universe</span>
              <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M14 5l7 7m0 0l-7 7m7-7H3" />
              </svg>
            </Link>

            <Link
              href="/profile"
              className="text-xs font-bold px-4 py-2.5 rounded-2xl bg-slate-100 hover:bg-slate-200/80 border border-slate-200 text-slate-800 transition-all active:scale-[0.98]"
            >
              Zerodha Account
            </Link>
          </div>
        </div>
      </div>

      {/* =========================================================================
          2. Core System Readiness Bento Cards
          ========================================================================= */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-5">
        {/* Card 1: Zerodha Kite Connect State */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Broker Session
              </span>
              {isKiteLoading ? (
                <span className="text-[10px] font-mono bg-slate-100 text-slate-600 px-2 py-0.5 rounded-full">
                  Checking...
                </span>
              ) : isKiteConnected ? (
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

            <div className="text-xl font-black text-slate-900 mt-3 tracking-tight">
              {isKiteConnected ? (
                <span className="font-mono">{kiteStatus?.user_id}</span>
              ) : (
                "Session Disconnected"
              )}
            </div>

            <p className="text-xs text-slate-500 mt-1 leading-relaxed">
              {isKiteConnected
                ? `Daily token active for ${kiteStatus?.user_name || "trader"}. Valid until tomorrow 06:00 AM IST.`
                : "Daily Zerodha login required to access live quotes and execute trades."}
            </p>
          </div>

          <div className="mt-5 pt-4 border-t border-slate-100 flex items-center justify-between">
            {isKiteConnected ? (
              <Link
                href="/profile"
                className="text-xs font-bold text-emerald-700 hover:text-emerald-800 flex items-center gap-1 transition-colors"
              >
                <span>View Funds & Margins</span>
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
                <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
                </svg>
                <span>Login with Kite</span>
              </button>
            )}
            <span className="text-[10px] font-mono text-slate-400">Zerodha Connect</span>
          </div>
        </div>

        {/* Card 2: 08:30 AM Stock Universe */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Universe Status
              </span>
              <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-bold bg-emerald-50 text-emerald-800 border border-emerald-300/60 font-mono">
                {totalStocks > 0 ? "POPULATED" : "PENDING"}
              </span>
            </div>

            <div className="text-3xl font-black text-slate-900 mt-3 font-mono tracking-tight">
              {isUniverseLoading ? "..." : `${totalStocks} Stocks`}
            </div>

            <p className="text-xs text-slate-500 mt-1 leading-relaxed">
              High-liquidity NSE Cash Equities filtered from NFO universe contracts with instrument token mappings.
            </p>
          </div>

          <div className="mt-5 pt-4 border-t border-slate-100 flex items-center justify-between">
            <Link
              href="/universe"
              className="text-xs font-bold text-emerald-700 hover:text-emerald-800 flex items-center gap-1 transition-colors"
            >
              <span>Explore 213 Stocks</span>
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" d="M9 5l7 7-7 7" />
              </svg>
            </Link>
            <span className="text-[10px] font-mono text-slate-400">Synced {lastScanDate}</span>
          </div>
        </div>

        {/* Card 3: Execution Engine Specifications */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Strategy Config
              </span>
              <span className="text-[10px] font-mono font-bold bg-slate-100 text-slate-700 px-2 py-0.5 rounded-md">
                INTRADAY ONLY
              </span>
            </div>

            <div className="text-xl font-black text-slate-900 mt-3 tracking-tight">
              NSE Cash Equities
            </div>

            <div className="mt-2 space-y-1.5 text-xs text-slate-500 font-mono">
              <div className="flex items-center justify-between">
                <span>Trading Segment:</span>
                <strong className="text-slate-800">NSE EQ</strong>
              </div>
              <div className="flex items-center justify-between">
                <span>Product Mode:</span>
                <strong className="text-slate-800">MIS (5x Leverage)</strong>
              </div>
              <div className="flex items-center justify-between">
                <span>Execution Window:</span>
                <strong className="text-slate-800">09:15 - 15:15 IST</strong>
              </div>
            </div>
          </div>

          <div className="mt-5 pt-4 border-t border-slate-100 flex items-center justify-between">
            <span className="text-xs font-bold text-slate-600">Zero Overnight Risk</span>
            <span className="text-[10px] font-mono text-slate-400">Engine V1</span>
          </div>
        </div>
      </div>

      {/* =========================================================================
          3. Quick Action & Pipeline Roadmap Banner
          ========================================================================= */}
      <div className="bg-gradient-to-r from-slate-900 via-slate-800 to-slate-900 text-white rounded-3xl p-6 sm:p-7 shadow-sm">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <h3 className="text-base sm:text-lg font-bold">
              Algo Trading Execution Pipeline
            </h3>
            <p className="text-xs text-slate-400 mt-1 max-w-xl leading-relaxed">
              <strong>Phase 1 Active:</strong> Daily Kite Authentication &amp; 08:30 AM Universal Instrument Scanner. Strategy signals, order placement, and live position risk management will activate in upcoming phases.
            </p>
          </div>

          <div className="flex items-center gap-3 shrink-0">
            <Link
              href="/universe"
              className="text-xs font-bold px-4 py-2.5 rounded-xl bg-white text-slate-900 hover:bg-slate-100 transition-all active:scale-95"
            >
              Open Scanner
            </Link>
            <Link
              href="/profile"
              className="text-xs font-bold px-4 py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 transition-all active:scale-95"
            >
              Kite Profile
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}
