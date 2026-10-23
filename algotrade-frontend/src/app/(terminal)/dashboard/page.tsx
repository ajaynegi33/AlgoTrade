"use client";

import React, { useEffect } from "react";
import Link from "next/link";
import { useTradingStore } from "@/store/useTradingStore";
import { useEngineWebSocket } from "@/hooks/useEngineWebSocket";
import { getKiteLoginURL } from "@/lib/api";
import { toast } from "sonner";
import { DashboardOverviewSkeleton } from "@/components/ui/Skeleton";
import { StockRankData, decodeReasonFlags } from "@/lib/types";

export default function DashboardOverviewPage() {
  const {
    kiteStatus,
    kiteProfile,
    universe,
    isKiteLoading,
    isUniverseLoading,
    fetchKiteStatus,
    fetchKiteProfile,
    fetchUniverse,
  } = useTradingStore();

  // Connect to 1.0s real-time engine stream for telemetry and teaser signals
  const { snapshot, isConnected } = useEngineWebSocket();

  useEffect(() => {
    fetchKiteStatus();
    fetchUniverse();
    fetchKiteProfile();
  }, [fetchKiteStatus, fetchUniverse, fetchKiteProfile]);

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

  // Extract #1 Top Bullish and #1 Top Bearish from engine snapshot for preview
  const topBullish: StockRankData | undefined = snapshot?.top_bullish?.[0];
  const topBearish: StockRankData | undefined = snapshot?.top_bearish?.[0];

  return (
    <div className="space-y-6">

      {/* =========================================================================
          2. Core Platform Telemetry (3 Key Status Cards)
          ========================================================================= */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-5">
        {/* Card 1: Zerodha Kite Connect */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col justify-between hover:border-slate-300 transition-colors">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Broker Connection
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
                <span>View Funds &amp; Margins</span>
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
        <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col justify-between hover:border-slate-300 transition-colors">
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

        {/* Card 3: Quant Engine Actor */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col justify-between hover:border-slate-300 transition-colors">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Quant Engine Status
              </span>
              <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-bold bg-emerald-50 text-emerald-800 border border-emerald-300/60 font-mono">
                <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
                {isConnected ? "STREAMING" : "STANDBY"}
              </span>
            </div>

            <div className="text-xl font-black text-slate-900 mt-3 tracking-tight">
              Single-Writer Actor
            </div>

            <p className="text-xs text-slate-500 mt-1 leading-relaxed">
              Lock-free [512]StockMetrics layout streaming 1.0s unified snapshots with Time-of-Day RVOL and anti-spoof depth.
            </p>
          </div>

          <div className="mt-5 pt-4 border-t border-slate-100 flex items-center justify-between">
            <Link
              href="/leaderboard"
              className="text-xs font-bold text-emerald-700 hover:text-emerald-800 flex items-center gap-1 transition-colors"
            >
              <span>View Leaderboard</span>
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" d="M9 5l7 7-7 7" />
              </svg>
            </Link>
            <span className="text-[10px] font-mono text-slate-400">1.0s Cadence</span>
          </div>
        </div>
      </div>

      {/* =========================================================================
          3. Live Market Pulse & Index Regime Snapshot
          ========================================================================= */}
      {snapshot?.market_pulse && (
        <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
            <div className="flex items-center gap-3">
              {/* <div className="w-10 h-10 rounded-2xl bg-slate-900 text-white flex items-center justify-center font-black font-mono text-sm shrink-0">
                MP
              </div> */}
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider">
                    Market Pulse &amp; Breadth
                  </span>
                  <span
                    className={`text-[10px] font-mono font-bold px-2 py-0.5 rounded-full ${
                      snapshot.market_pulse.market_regime === "BULLISH_EXPANSION"
                        ? "bg-emerald-50 text-emerald-800 border border-emerald-300"
                        : snapshot.market_pulse.market_regime === "BEARISH_EXPANSION"
                        ? "bg-rose-50 text-rose-800 border border-rose-300"
                        : "bg-amber-50 text-amber-800 border border-amber-300"
                    }`}
                  >
                    {snapshot.market_pulse.market_regime.replace("_", " ")}
                  </span>
                </div>
                <p className="text-xs text-slate-500 mt-0.5">
                  Index momentum and breadth ratio across all 213 tracked NSE F&amp;O cash equities.
                </p>
              </div>
            </div>

            <div className="flex items-center gap-4 sm:gap-6 shrink-0 font-mono text-xs">
              <div>
                <span className="text-[10px] text-slate-400 block uppercase">Nifty 50</span>
                <span className="font-bold text-slate-900">
                  {snapshot.market_pulse.nifty_ltp > 0
                    ? `₹${snapshot.market_pulse.nifty_ltp.toLocaleString("en-IN", { minimumFractionDigits: 2 })}`
                    : "—"}
                </span>
                <span
                  className={`ml-1.5 font-semibold text-[11px] ${
                    snapshot.market_pulse.nifty_change_percent >= 0 ? "text-emerald-700" : "text-rose-700"
                  }`}
                >
                  {snapshot.market_pulse.nifty_change_percent >= 0 ? "+" : ""}
                  {snapshot.market_pulse.nifty_change_percent.toFixed(2)}%
                </span>
              </div>

              <div className="h-7 w-px bg-slate-200" />

              <div>
                <span className="text-[10px] text-slate-400 block uppercase">Bank Nifty</span>
                <span className="font-bold text-slate-900">
                  {snapshot.market_pulse.banknifty_ltp > 0
                    ? `₹${snapshot.market_pulse.banknifty_ltp.toLocaleString("en-IN", { minimumFractionDigits: 2 })}`
                    : "—"}
                </span>
                <span
                  className={`ml-1.5 font-semibold text-[11px] ${
                    snapshot.market_pulse.banknifty_change_percent >= 0 ? "text-emerald-700" : "text-rose-700"
                  }`}
                >
                  {snapshot.market_pulse.banknifty_change_percent >= 0 ? "+" : ""}
                  {snapshot.market_pulse.banknifty_change_percent.toFixed(2)}%
                </span>
              </div>

              <div className="h-7 w-px bg-slate-200" />

              <div>
                <span className="text-[10px] text-slate-400 block uppercase">Breadth &gt; VWAP</span>
                <span className="font-bold text-slate-900">
                  {(snapshot.market_pulse.breadth_above_vwap * 100).toFixed(0)}%
                </span>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* =========================================================================
          4. Top Signal Outliers Teaser (Leaderboard Sneak Peek)
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-5">
          <div>
            <div className="flex items-center gap-2">
              <span className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider">
                High-Probability Signal Teaser
              </span>
              <span className="text-[10px] font-mono font-bold bg-slate-100 text-slate-700 px-2 py-0.5 rounded-md">
                EV ≥ +0.25R
              </span>
            </div>
            <p className="text-xs text-slate-500 mt-0.5">
              Top statistical directional candidates ranked by Composite Relative Strength and anti-spoof depth.
            </p>
          </div>

          <Link
            href="/leaderboard"
            className="text-xs font-bold text-emerald-700 hover:text-emerald-800 flex items-center gap-1 transition-colors self-start sm:self-auto"
          >
            <span>View All Ranked Stocks</span>
            <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" d="M9 5l7 7-7 7" />
            </svg>
          </Link>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {/* Bullish Teaser Card */}
          <div className="border border-emerald-200/90 bg-emerald-50/30 rounded-2xl p-4 sm:p-5 flex flex-col justify-between">
            <div>
              <div className="flex items-center justify-between">
                <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[10px] font-mono font-bold bg-emerald-100/80 text-emerald-900 border border-emerald-300">
                  TOP BULLISH (LONG)
                </span>
                <span className="text-[11px] font-mono font-bold text-emerald-800">
                  {topBullish ? `EV: +${topBullish.ev.toFixed(2)}R` : "Scanning..."}
                </span>
              </div>

              {topBullish ? (
                <div className="mt-3">
                  <div className="flex items-baseline justify-between">
                    <div>
                      <h4 className="text-lg font-black text-slate-900 font-mono tracking-tight">
                        {topBullish.symbol}
                      </h4>
                      <span className="text-[11px] font-mono text-slate-500">
                        {topBullish.sector}
                      </span>
                    </div>
                    <div className="text-right font-mono">
                      <div className="text-base font-black text-slate-900">
                        ₹{topBullish.ltp.toLocaleString("en-IN", { minimumFractionDigits: 2 })}
                      </div>
                      <div className="text-xs font-bold text-emerald-700">
                        +{topBullish.change_percent.toFixed(2)}%
                      </div>
                    </div>
                  </div>

                  <div className="mt-3 flex flex-wrap gap-1.5">
                    {decodeReasonFlags(topBullish.reasons).map((b, i) => (
                      <span
                        key={i}
                        className="text-[10px] font-mono font-bold px-2 py-0.5 rounded bg-emerald-100/90 text-emerald-900 border border-emerald-200"
                      >
                        {b.label}
                      </span>
                    ))}
                  </div>
                </div>
              ) : (
                <p className="text-xs text-slate-500 mt-4 leading-relaxed font-mono">
                  Scanning order book tape for institutional bullish push-off confluences...
                </p>
              )}
            </div>

            <div className="mt-4 pt-3 border-t border-emerald-200/60 flex items-center justify-between text-xs font-mono">
              <span className="text-slate-500">Win Rate Target:</span>
              <strong className="text-emerald-800 font-bold">
                {topBullish ? `${(topBullish.p_win * 100).toFixed(0)}%` : "60% - 70%"}
              </strong>
            </div>
          </div>

          {/* Bearish Teaser Card */}
          <div className="border border-rose-200/90 bg-rose-50/30 rounded-2xl p-4 sm:p-5 flex flex-col justify-between">
            <div>
              <div className="flex items-center justify-between">
                <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[10px] font-mono font-bold bg-rose-100/80 text-rose-900 border border-rose-300">
                  TOP BEARISH (SHORT)
                </span>
                <span className="text-[11px] font-mono font-bold text-rose-800">
                  {topBearish ? `EV: +${topBearish.ev.toFixed(2)}R` : "Scanning..."}
                </span>
              </div>

              {topBearish ? (
                <div className="mt-3">
                  <div className="flex items-baseline justify-between">
                    <div>
                      <h4 className="text-lg font-black text-slate-900 font-mono tracking-tight">
                        {topBearish.symbol}
                      </h4>
                      <span className="text-[11px] font-mono text-slate-500">
                        {topBearish.sector}
                      </span>
                    </div>
                    <div className="text-right font-mono">
                      <div className="text-base font-black text-slate-900">
                        ₹{topBearish.ltp.toLocaleString("en-IN", { minimumFractionDigits: 2 })}
                      </div>
                      <div className="text-xs font-bold text-rose-700">
                        {topBearish.change_percent.toFixed(2)}%
                      </div>
                    </div>
                  </div>

                  <div className="mt-3 flex flex-wrap gap-1.5">
                    {decodeReasonFlags(topBearish.reasons).map((b, i) => (
                      <span
                        key={i}
                        className="text-[10px] font-mono font-bold px-2 py-0.5 rounded bg-rose-100/90 text-rose-900 border border-rose-200"
                      >
                        {b.label}
                      </span>
                    ))}
                  </div>
                </div>
              ) : (
                <p className="text-xs text-slate-500 mt-4 leading-relaxed font-mono">
                  Scanning order book tape for institutional bearish breakdown confluences...
                </p>
              )}
            </div>

            <div className="mt-4 pt-3 border-t border-rose-200/60 flex items-center justify-between text-xs font-mono">
              <span className="text-slate-500">Win Rate Target:</span>
              <strong className="text-rose-800 font-bold">
                {topBearish ? `${(topBearish.p_win * 100).toFixed(0)}%` : "60% - 70%"}
              </strong>
            </div>
          </div>
        </div>
      </div>

      {/* =========================================================================
          5. Account Margins & Execution Guardrails
          ========================================================================= */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-5">
        {/* Card A: Account Margins (When connected) */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Capital &amp; Buying Power
              </span>
              <span className="text-[10px] font-mono font-bold bg-slate-100 text-slate-700 px-2 py-0.5 rounded-md">
                EQUITY SEGMENT
              </span>
            </div>

            {kiteProfile?.equity_margin ? (
              <div className="mt-4 space-y-2.5 font-mono text-xs">
                <div className="flex items-center justify-between">
                  <span className="text-slate-500">Available Cash:</span>
                  <strong className="text-slate-900 font-bold text-sm">
                    ₹{kiteProfile.equity_margin.available_cash.toLocaleString("en-IN", { minimumFractionDigits: 2 })}
                  </strong>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-slate-500">Utilized Margin:</span>
                  <strong className="text-slate-900 font-bold">
                    ₹{kiteProfile.equity_margin.utilised_debits.toLocaleString("en-IN", { minimumFractionDigits: 2 })}
                  </strong>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-slate-500">Collateral Liquid:</span>
                  <strong className="text-slate-900 font-bold">
                    ₹{kiteProfile.equity_margin.collateral.toLocaleString("en-IN", { minimumFractionDigits: 2 })}
                  </strong>
                </div>
              </div>
            ) : (
              <div className="mt-4 text-xs text-slate-500 leading-relaxed font-mono">
                {isKiteConnected
                  ? "Loading live margin balances from Zerodha Kite..."
                  : "Login to Zerodha Kite to view live buying power and intraday margin allocation."}
              </div>
            )}
          </div>

          <div className="mt-5 pt-4 border-t border-slate-100 flex items-center justify-between">
            <Link
              href="/profile"
              className="text-xs font-bold text-emerald-700 hover:text-emerald-800 flex items-center gap-1 transition-colors"
            >
              <span>Detailed Account Margins</span>
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" d="M9 5l7 7-7 7" />
              </svg>
            </Link>
            <span className="text-[10px] font-mono text-slate-400">MIS 5x Buying Power</span>
          </div>
        </div>

        {/* Card B: Strategy Execution Guardrails */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400">
                Strategy Guardrails
              </span>
              <span className="text-[10px] font-mono font-bold bg-emerald-50 text-emerald-800 border border-emerald-200 px-2 py-0.5 rounded-md">
                SAFETY LAYER
              </span>
            </div>

            <div className="mt-4 space-y-2 text-xs font-mono text-slate-600">
              <div className="flex items-center justify-between">
                <span>Trading Segment:</span>
                <strong className="text-slate-900 font-bold">NSE EQ (Cash Only)</strong>
              </div>
              <div className="flex items-center justify-between">
                <span>Execution Mode:</span>
                <strong className="text-slate-900 font-bold">MIS Intraday (5x)</strong>
              </div>
              <div className="flex items-center justify-between">
                <span>Execution Window:</span>
                <strong className="text-slate-900 font-bold">09:15 - 15:15 IST</strong>
              </div>
              <div className="flex items-center justify-between">
                <span>Overnight Position Risk:</span>
                <strong className="text-emerald-700 font-bold">0.00% (Strict Square-off)</strong>
              </div>
            </div>
          </div>

          <div className="mt-5 pt-4 border-t border-slate-100 flex items-center justify-between">
            <span className="text-xs font-bold text-slate-600">Phase 2 Ranking Active</span>
            <span className="text-[10px] font-mono text-slate-400">Engine V2.4 Pro</span>
          </div>
        </div>
      </div>
    </div>
  );
}
