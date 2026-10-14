"use client";

import { OverviewData } from "../types";
import { TickUpdate } from "@/hooks/useEngineWebSocket";
import { SkeletonOverview } from "../common/Skeleton";
import DecisionFeedTerminal from "./DecisionFeedTerminal";

interface OverviewTabProps {
  overview: OverviewData | null;
  backendUrl?: string;
  latestTick?: TickUpdate;
}

export default function OverviewTab({ overview, backendUrl = "http://localhost:8080", latestTick }: OverviewTabProps) {
  if (!overview) {
    return <SkeletonOverview />;
  }

  // Dynamic real-time calculation: merges authoritative backend state with sub-second WebSocket ticks
  const pos = overview?.activePosition;
  const currentLtp = latestTick && latestTick.ltp > 0 ? latestTick.ltp : pos?.currentPrice ?? 0;
  const entryPrice = pos?.entryPrice ?? 0;
  const quantity = pos?.quantity ?? 0;
  const rawPnL = pos ? (currentLtp - entryPrice) * quantity : 0;
  const unrealizedPnL = Math.round(rawPnL * 100) / 100;
  const rawPnLPct = pos && entryPrice > 0 ? ((currentLtp - entryPrice) / entryPrice) * 100 : 0;
  const unrealizedPnLPct = Math.round(rawPnLPct * 100) / 100;
  const highWaterMark = pos ? Math.max(pos.highWaterMark, currentLtp) : 0;

  return (
    <div className="flex flex-col gap-6 animate-fadeIn">
      {/* Quick Metrics Grid */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-4">
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4">
          <p className="text-xs text-slate-400">Capital Base</p>
          <p className="text-lg sm:text-2xl font-bold font-mono text-white mt-1">
            {overview ? `₹${overview.capital.toLocaleString("en-IN")}` : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">Dedicated portfolio (5x MIS Leverage)</p>
        </div>
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4">
          <p className="text-xs text-slate-400">Risk Budget (2.0%)</p>
          <p className="text-lg sm:text-2xl font-bold font-mono text-amber-400 mt-1">
            {overview ? `₹${overview.riskBudget.toFixed(2)}` : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">Max loss allowed per trade</p>
        </div>
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4">
          <p className="text-xs text-slate-400">Daily Trade Limit</p>
          <p className="text-lg sm:text-2xl font-bold font-mono text-sky-400 mt-1">
            {overview ? (overview.canTradeToday ? "Active" : "Max Reached") : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">Max 3 trades / 2 losses lock</p>
        </div>
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4">
          <p className="text-xs text-slate-400">Active Strategy</p>
          <p className="text-sm sm:text-base font-semibold text-emerald-400 truncate mt-1">
            {overview ? overview.strategyName : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">All-Day Continuous Wave Breakout</p>
        </div>
      </div>

      {/* Active Position Card */}
      {overview?.activePosition ? (
        <div className="bg-gradient-to-br from-slate-900 to-slate-900/90 border-2 border-emerald-500/50 rounded-2xl p-5 sm:p-6 shadow-xl shadow-emerald-950/20">
          <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between pb-4 border-b border-slate-800 gap-3">
            <div>
              <div className="flex items-center gap-2">
                <span className="px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-400 text-xs font-mono font-semibold uppercase tracking-wider flex items-center gap-1.5">
                  <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
                  <span>Live Active Position</span>
                </span>
                <span className="px-2 py-0.5 rounded bg-slate-800 text-slate-400 text-[10px] font-mono">
                  ⚡ 0ms Live Ticker Stream
                </span>
              </div>
              <h2 className="text-2xl sm:text-3xl font-black text-white mt-1">
                {overview.activePosition.symbol}
              </h2>
              <p className="text-xs text-slate-400 font-mono">
                Quantity: {overview.activePosition.quantity} shares | Entered:{" "}
                {new Date(overview.activePosition.entryTime).toLocaleTimeString()}
              </p>
            </div>
            <div className="text-left sm:text-right">
              <p className="text-xs text-slate-400 uppercase tracking-wider">Unrealized P&amp;L</p>
              <p
                className={`text-2xl sm:text-4xl font-mono font-black ${
                  unrealizedPnL >= 0 ? "text-emerald-400" : "text-rose-400"
                }`}
              >
                {unrealizedPnL >= 0 ? "+" : ""}₹
                {unrealizedPnL.toFixed(2)}
                <span className="text-sm sm:text-lg font-normal ml-2 opacity-80">
                  ({unrealizedPnLPct >= 0 ? "+" : ""}
                  {unrealizedPnLPct.toFixed(2)}%)
                </span>
              </p>
            </div>
          </div>

          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mt-5">
            <div className="bg-slate-950/50 rounded-xl p-3 border border-slate-800/80">
              <p className="text-[11px] text-slate-400">Entry Price</p>
              <p className="text-base sm:text-lg font-mono font-bold text-white mt-0.5">
                ₹{overview.activePosition.entryPrice.toFixed(2)}
              </p>
            </div>
            <div className="bg-slate-950/50 rounded-xl p-3 border border-slate-800/80">
              <p className="text-[11px] text-slate-400">Current LTP</p>
              <p className="text-base sm:text-lg font-mono font-bold text-white mt-0.5 flex items-center gap-1.5">
                <span>₹{currentLtp.toFixed(2)}</span>
                {currentLtp >= overview.activePosition.entryPrice ? (
                  <span className="text-emerald-400 text-xs">▲</span>
                ) : (
                  <span className="text-rose-400 text-xs">▼</span>
                )}
              </p>
            </div>
            <div className="bg-slate-950/50 rounded-xl p-3 border border-slate-800/80">
              <p className="text-[11px] text-slate-400">Active Trailing Stop</p>
              <p className="text-base sm:text-lg font-mono font-bold text-amber-400 mt-0.5">
                ₹{overview.activePosition.activeTrailingStop.toFixed(2)}
              </p>
              <p className="text-[10px] text-slate-500">
                Initial: ₹{overview.activePosition.initialStopLoss.toFixed(2)}
              </p>
            </div>
            <div className="bg-slate-950/50 rounded-xl p-3 border border-slate-800/80">
              <p className="text-[11px] text-slate-400">High-Water Mark (Peak)</p>
              <p className="text-base sm:text-lg font-mono font-bold text-sky-400 mt-0.5">
                ₹{highWaterMark.toFixed(2)}
              </p>
              <p className="text-[10px] text-slate-500">Trail buffer: 2.0%</p>
            </div>
          </div>
        </div>
      ) : overview?.status === "MARKET_CLOSED" ? (
        <div className="bg-slate-900/60 border border-amber-500/30 rounded-2xl p-8 sm:p-12 text-center flex flex-col items-center justify-center">
          <div className="w-12 h-12 rounded-full bg-amber-500/10 border border-amber-500/30 flex items-center justify-center text-amber-400 text-xl mb-3">
            🌙
          </div>
          <h3 className="text-lg font-semibold text-white">NSE Equity Market is Currently Closed</h3>
          <p className="text-sm text-slate-400 max-w-lg mt-2 leading-relaxed">
            Observation of candidate ticks and intraday trade entries are automatically paused outside regular market hours (09:15 AM – 03:30 PM IST, Monday through Friday).
          </p>
          <div className="mt-5 inline-flex items-center gap-2 px-4 py-2 rounded-xl bg-amber-500/10 border border-amber-500/20 text-amber-300 text-xs sm:text-sm font-mono">
            <span>Next Trading Session Opens:</span>
            <strong className="text-white font-bold">{overview.nextMarketOpen || "09:15 AM IST"}</strong>
          </div>
        </div>
      ) : (
        <div className="bg-slate-900/40 border border-dashed border-slate-800 rounded-2xl p-8 sm:p-12 text-center flex flex-col items-center justify-center">
          <div className="w-12 h-12 rounded-full bg-slate-800/60 flex items-center justify-center text-slate-500 text-xl mb-3">
            ⏳
          </div>
          <h3 className="text-lg font-semibold text-white">No Open Position Currently</h3>
          <p className="text-sm text-slate-400 max-w-md mt-1">
            The engine continuously scans the 210 F&amp;O universe every 60 seconds across market waves (09:20 – 14:45 IST). When a candidate scores ≥ 75 points and breaks out above PDH, an automated trade is executed.
          </p>
        </div>
      )}

      {/* Live Engine Decision Stream & Audit Terminal */}
      <DecisionFeedTerminal backendUrl={backendUrl} />
    </div>
  );
}

