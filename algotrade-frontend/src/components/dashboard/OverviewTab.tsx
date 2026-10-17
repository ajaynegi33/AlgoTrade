"use client";

import { useState } from "react";
import { OverviewData } from "../types";
import { TickUpdate } from "@/hooks/useEngineWebSocket";
import { SkeletonOverview } from "../common/Skeleton";
import { apiFetch } from "@/lib/apiClient";
import DecisionFeedTerminal from "./DecisionFeedTerminal";

interface OverviewTabProps {
  overview: OverviewData | null;
  backendUrl?: string;
  latestTick?: TickUpdate;
}

export default function OverviewTab({ overview, backendUrl = "http://localhost:8080", latestTick }: OverviewTabProps) {
  const [isSelling, setIsSelling] = useState<boolean>(false);
  const [sellError, setSellError] = useState<string | null>(null);
  const [isEditingSl, setIsEditingSl] = useState<boolean>(false);
  const [manualSlPrice, setManualSlPrice] = useState<string>("");
  const [isUpdatingSl, setIsUpdatingSl] = useState<boolean>(false);
  const [slError, setSlError] = useState<string | null>(null);

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

  const handleUpdateStopLoss = async () => {
    if (isUpdatingSl || !overview?.activePosition || !manualSlPrice) return;
    const price = parseFloat(manualSlPrice);
    if (isNaN(price) || price <= 0) {
      setSlError("Please enter a valid positive stop price");
      return;
    }

    setIsUpdatingSl(true);
    setSlError(null);

    try {
      const res = await apiFetch("/api/dashboard/update-stop-loss", {
        method: "POST",
        body: JSON.stringify({
          symbol: overview.activePosition.symbol,
          stopPrice: price,
        }),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error(err.error || "Failed to update stop loss");
      }

      setIsEditingSl(false);
    } catch (err: unknown) {
      const errorMessage = err instanceof Error ? err.message : "Failed to update stop loss";
      setSlError(errorMessage);
    } finally {
      setIsUpdatingSl(false);
    }
  };

  const handleInstantSell = async () => {
    if (isSelling || !overview?.activePosition) return;

    setIsSelling(true);
    setSellError(null);

    try {
      const res = await apiFetch("/api/dashboard/manual-exit", {
        method: "POST",
        body: JSON.stringify({
          symbol: overview.activePosition.symbol,
          reason: "MANUAL_OPERATOR_SELL",
        }),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error(err.error || "Failed to execute market exit");
      }
    } catch (err: unknown) {
      const errorMessage = err instanceof Error ? err.message : "Failed to exit position";
      setSellError(errorMessage);
    } finally {
      setIsSelling(false);
    }
  };

  return (
    <div className="flex flex-col gap-6 animate-fadeIn">
      {/* Quick Metrics Grid */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-4">
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4">
          <div className="flex items-center justify-between">
            <p className="text-xs text-slate-400">Current Capital</p>
            {overview && overview.cumulativeNetPnL !== undefined && overview.cumulativeNetPnL !== 0 && (
              <span
                className={`text-[10px] font-mono font-bold px-1.5 py-0.5 rounded ${
                  overview.cumulativeNetPnL > 0
                    ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30"
                    : "bg-rose-500/20 text-rose-400 border border-rose-500/30"
                }`}
              >
                {overview.cumulativeNetPnL > 0 ? "+" : ""}₹{overview.cumulativeNetPnL.toFixed(2)}
              </span>
            )}
          </div>
          <p className="text-lg sm:text-2xl font-bold font-mono text-white mt-1">
            {overview ? `₹${overview.capital.toLocaleString("en-IN", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}` : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">
            Base: ₹{(overview?.baseCapital ?? 10000).toLocaleString("en-IN")} • 2x–5x MIS Leverage (margin-dependent)
          </p>
        </div>
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4">
          <p className="text-xs text-slate-400">Risk Budget (2.0%)</p>
          <p className="text-lg sm:text-2xl font-bold font-mono text-amber-400 mt-1">
            {overview ? `₹${overview.riskBudget.toFixed(2)}` : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">
            {overview ? `2% of dynamic capital (₹${overview.capital.toFixed(2)})` : "Max loss allowed per trade"}
          </p>
        </div>
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4">
          <p className="text-xs text-slate-400">Daily Trade Limit</p>
          <p className="text-lg sm:text-2xl font-bold font-mono text-sky-400 mt-1">
            {overview ? (overview.canTradeToday ? "Active" : "Max Reached") : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">Max 3 trades / 2 losses / ₹400 loss breaker</p>
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
              <div className="flex flex-wrap items-center gap-2">
                <span className="px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-400 text-xs font-mono font-semibold uppercase tracking-wider flex items-center gap-1.5">
                  <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
                  <span>Live Active Position</span>
                </span>
                {overview.activePosition.logicalStage !== undefined && (
                  <span
                    className={`px-2 py-0.5 rounded text-xs font-mono font-bold border ${
                      overview.activePosition.logicalStage === 3
                        ? "bg-purple-500/20 text-purple-300 border-purple-500/40"
                        : overview.activePosition.logicalStage === 2
                        ? "bg-emerald-500/20 text-emerald-300 border-emerald-500/40"
                        : overview.activePosition.logicalStage === 1
                        ? "bg-blue-500/20 text-blue-300 border-blue-500/40"
                        : "bg-amber-500/20 text-amber-300 border-amber-500/40"
                    }`}
                  >
                    {overview.activePosition.logicalStage === 3
                      ? "Stage 3 · Runner Profit Lock"
                      : overview.activePosition.logicalStage === 2
                      ? "Stage 2 · Cost Floor (+₹11 Net)"
                      : overview.activePosition.logicalStage === 1
                      ? "Stage 1 · Soft De-Risk (−0.35R)"
                      : "Stage 0 · Initial Structural SL"}
                  </span>
                )}
                {overview.activePosition.activeExitPolicy && (
                  <span className="px-2 py-0.5 rounded text-xs font-mono font-bold bg-rose-500/20 text-rose-300 border border-rose-500/40 animate-pulse">
                    Exit: {overview.activePosition.activeExitPolicy}
                  </span>
                )}
              </div>
              <h2 className="text-2xl sm:text-3xl font-black text-white mt-1">
                {overview.activePosition.symbol}
              </h2>
              <p className="text-xs text-slate-400 font-mono">
                Quantity: {overview.activePosition.quantity} shares | Entered:{" "}
                {new Date(overview.activePosition.entryTime).toLocaleTimeString()}
                {overview.activePosition.slModificationCount !== undefined && overview.activePosition.slModificationCount > 0 && (
                  <span> | SL Mods: {overview.activePosition.slModificationCount}</span>
                )}
              </p>
            </div>
            {/* Right side cluster: Stop Loss Controls + Immediate Sell Button + Unrealized P&L */}
            <div className="flex flex-wrap items-center gap-2.5 sm:gap-4 w-full sm:w-auto justify-between sm:justify-end">
              {/* Stop Loss Button or Inline Input */}
              {!isEditingSl ? (
                <button
                  type="button"
                  onClick={() => {
                    setIsEditingSl(true);
                    setManualSlPrice(
                      overview.activePosition?.armedTrigger
                        ? String(overview.activePosition.armedTrigger)
                        : overview.activePosition?.activeTrailingStop
                        ? String(overview.activePosition.activeTrailingStop)
                        : ""
                    );
                    setSlError(null);
                  }}
                  disabled={isSelling || isUpdatingSl}
                  title="Set or adjust custom stop-loss price"
                  className="px-3 sm:px-4 py-2 sm:py-2 rounded-md font-bold flex items-center gap-1.5 transition-all duration-150 cursor-pointer shadow-lg select-none bg-slate-900/90 hover:bg-slate-800 text-amber-400 border border-amber-500/30 hover:border-amber-500/60 shadow-slate-950/50 hover:shadow-amber-950/20 active:scale-[0.98]"
                >
                  <svg className="w-4 h-4 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
                  </svg>
                  <span className="text-xs sm:text-sm font-black tracking-wider uppercase">
                    Stop Loss
                  </span>
                </button>
              ) : (
                <div className="flex items-center gap-1.5 bg-slate-900/95 border border-amber-500/60 rounded-md px-2.5 py-1.5 shadow-lg shadow-amber-950/40 animate-fadeIn">
                  <span className="text-xs font-mono text-amber-400 font-bold">₹</span>
                  <input
                    type="number"
                    step="0.05"
                    autoFocus
                    value={manualSlPrice}
                    disabled={isUpdatingSl}
                    onChange={(e) => setManualSlPrice(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.preventDefault();
                        handleUpdateStopLoss();
                      } else if (e.key === "Escape") {
                        setIsEditingSl(false);
                        setSlError(null);
                      }
                    }}
                    placeholder="SL Price"
                    className="w-20 sm:w-24 bg-transparent text-white font-mono text-xs sm:text-sm font-bold focus:outline-none [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
                  />
                  {/* Enter / Save checkmark button */}
                  <button
                    type="button"
                    onClick={handleUpdateStopLoss}
                    disabled={isUpdatingSl || !manualSlPrice}
                    title="Save Stop Loss (Enter)"
                    className="p-1 text-emerald-400 hover:text-emerald-300 disabled:opacity-40 cursor-pointer transition-colors"
                  >
                    {isUpdatingSl ? (
                      <svg className="animate-spin h-3.5 w-3.5 text-amber-300" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z"></path>
                      </svg>
                    ) : (
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" d="M5 13l4 4L19 7" />
                      </svg>
                    )}
                  </button>
                  {/* X-Circle cancel button */}
                  <button
                    type="button"
                    onClick={() => {
                      setIsEditingSl(false);
                      setSlError(null);
                    }}
                    disabled={isUpdatingSl}
                    title="Cancel (Esc)"
                    className="p-1 text-slate-400 hover:text-rose-400 transition-colors cursor-pointer"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                  </button>
                </div>
              )}

              {/* Immediate 1-Click Market Sell Button */}
              <button
                type="button"
                onClick={handleInstantSell}
                disabled={isSelling}
                title="Immediately liquidate active position at market price and book profit/loss"
                className={`group relative px-4 sm:px-5 py-2 sm:py-2 rounded-md font-bold flex items-center gap-2.5 transition-all duration-150 cursor-pointer shadow-lg select-none ${
                  isSelling
                    ? "bg-rose-950/80 border border-rose-800/60 text-rose-300 opacity-80 cursor-wait"
                    : "bg-gradient-to-r from-rose-600 via-rose-600 to-rose-700 hover:from-rose-500 hover:to-rose-600 active:scale-[0.98] text-white border border-rose-400/50 shadow-rose-950/50 hover:shadow-rose-900/60"
                }`}
              >
                {isSelling ? (
                  <>
                    <svg className="animate-spin h-4 w-4 text-rose-200" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z"></path>
                    </svg>
                    <span className="text-xs sm:text-sm tracking-wider font-mono">SELLING...</span>
                  </>
                ) : (
                  <>
                    <span className="flex flex-col items-start text-left leading-tight">
                      <span className="flex items-center gap-1.5 text-xs sm:text-sm font-black tracking-wider uppercase">
                      
                        <span>Sell</span>
                      </span>
      
                    </span>
                  </>
                )}
              </button>

              {/* Unrealized P&L */}
              <div className="text-right">
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
          </div>

          {slError && (
            <div className="mt-3 p-3 rounded-xl bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs flex items-center justify-between animate-fadeIn">
              <span className="flex items-center gap-2">
                <span>⚠️</span>
                <span>{slError}</span>
              </span>
              <button
                type="button"
                onClick={() => setSlError(null)}
                className="text-amber-400 hover:text-white text-xs font-bold px-2 py-0.5 rounded bg-amber-950/40 hover:bg-amber-900/60 cursor-pointer"
              >
                Dismiss
              </button>
            </div>
          )}

          {sellError && (
            <div className="mt-3 p-3 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center justify-between animate-fadeIn">
              <span className="flex items-center gap-2">
                <span>⚠️</span>
                <span>{sellError}</span>
              </span>
              <button
                type="button"
                onClick={() => setSellError(null)}
                className="text-rose-400 hover:text-white text-xs font-bold px-2 py-0.5 rounded bg-rose-950/40 hover:bg-rose-900/60 cursor-pointer"
              >
                Dismiss
              </button>
            </div>
          )}

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
              <p className="text-[11px] text-slate-400">Protective Stop-Limit</p>
              <p className="text-base sm:text-lg font-mono font-bold text-amber-400 mt-0.5">
                ₹{(overview.activePosition.armedTrigger ?? overview.activePosition.activeTrailingStop).toFixed(2)}
              </p>
              <p className="text-[10px] text-slate-500">
                Limit: ₹{(overview.activePosition.armedLimit ?? overview.activePosition.activeTrailingStop).toFixed(2)} | S0: ₹{overview.activePosition.initialStopLoss.toFixed(2)}
              </p>
            </div>
            <div className="bg-slate-950/50 rounded-xl p-3 border border-slate-800/80">
              <p className="text-[11px] text-slate-400">High-Water Mark (Peak)</p>
              <p className="text-base sm:text-lg font-mono font-bold text-sky-400 mt-0.5">
                ₹{highWaterMark.toFixed(2)}
              </p>
              <p className="text-[10px] text-slate-500">
                {overview.activePosition.logicalStage === 3
                  ? "Dynamic Retention Active"
                  : overview.activePosition.logicalStage === 2
                  ? "Locked Above Cost Floor"
                  : overview.activePosition.logicalStage === 1
                  ? "De-risked (−0.35R)"
                  : "Incubation (Initial SL)"}
              </p>
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
            The engine monitors the 210 F&amp;O universe via real-time WebSocket tick stream with 0ms Breakout Radar across market waves (09:20 – 14:45 IST). When a candidate scores ≥ 75 points and breaks out above PDH, an automated trade is executed.
          </p>
        </div>
      )}

      {/* Live Engine Decision Stream & Audit Terminal */}
      <DecisionFeedTerminal backendUrl={backendUrl} />
    </div>
  );
}

