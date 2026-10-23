"use client";

import React, { useMemo } from "react";
import { DepthLevelData } from "@/lib/types";

interface StockOrderBook5LevelProps {
  bids: DepthLevelData[];
  asks: DepthLevelData[];
  totalBidQty: number;
  totalAskQty: number;
  antiSpoofRatio: number;
  aggressorBuyRatio: number;
  ltp: number;
}

export function StockOrderBook5Level({
  bids,
  asks,
  totalBidQty,
  totalAskQty,
  antiSpoofRatio,
  aggressorBuyRatio,
  ltp,
}: StockOrderBook5LevelProps) {
  // Safe default 5 rows if book is empty
  const safeBids = useMemo(() => {
    const list = [...(bids || [])];
    while (list.length < 5) {
      list.push({ price: 0, qty: 0, orders: 0 });
    }
    return list.slice(0, 5);
  }, [bids]);

  const safeAsks = useMemo(() => {
    const list = [...(asks || [])];
    while (list.length < 5) {
      list.push({ price: 0, qty: 0, orders: 0 });
    }
    return list.slice(0, 5);
  }, [asks]);

  // Max volume in single tier for background bar scaling
  const maxTierVol = useMemo(() => {
    const allQtys = [...safeBids.map((b) => b.qty), ...safeAsks.map((a) => a.qty)];
    return Math.max(...allQtys, 100);
  }, [safeBids, safeAsks]);

  // Bid-Ask Spread
  const bid1 = safeBids[0]?.price || 0;
  const ask1 = safeAsks[0]?.price || 0;
  const spread = ask1 > 0 && bid1 > 0 ? ask1 - bid1 : 0;
  const spreadPct = ltp > 0 ? (spread / ltp) * 100 : 0;

  // Raw Bid/Ask Ratio
  const totalVolume = totalBidQty + totalAskQty;
  const rawBuyerPct = totalVolume > 0 ? (totalBidQty / totalVolume) * 100 : 50;

  const antiSpoofPct = antiSpoofRatio * 100;
  const aggressorPct = aggressorBuyRatio * 100;

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs flex flex-col justify-between">
      {/* Top Header & Spread */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <div className="flex items-center gap-2">
            <span className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider">
              5-Level Market Depth & Pressure
            </span>
            <span className="text-[10px] font-mono px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 border border-slate-200">
              Anti-Spoof Weighted
            </span>
          </div>

          <div className="text-[11px] font-mono text-slate-500">
            Spread: <strong className="text-slate-800">₹{spread.toFixed(2)}</strong> ({spreadPct.toFixed(2)}%)
          </div>
        </div>

        {/* 5-Level Bids vs Asks Table */}
        <div className="grid grid-cols-2 gap-2 text-xs font-mono">
          {/* Bids Column */}
          <div>
            <div className="flex items-center justify-between text-[10px] text-slate-400 uppercase font-semibold px-2 py-1 bg-slate-50 rounded-lg mb-1">
              <span>Orders</span>
              <span>Volume</span>
              <span className="text-emerald-700">Bid (₹)</span>
            </div>

            <div className="space-y-1">
              {safeBids.map((b, i) => {
                const barWidth = Math.min(100, (b.qty / maxTierVol) * 100);
                return (
                  <div
                    key={`bid-${i}`}
                    className="relative flex items-center justify-between px-2 py-1 rounded-lg overflow-hidden border border-slate-100 hover:border-emerald-200 transition-colors"
                  >
                    {/* Background Volume Bar */}
                    <div
                      className="absolute top-0 bottom-0 right-0 bg-emerald-500/10 pointer-events-none transition-all duration-300"
                      style={{ width: `${barWidth}%` }}
                    />

                    <span className="text-slate-400 text-[10px] relative z-10">{b.orders || "--"}</span>
                    <span className="text-slate-700 font-semibold relative z-10">
                      {b.qty ? b.qty.toLocaleString("en-IN") : "--"}
                    </span>
                    <span className="text-emerald-700 font-bold relative z-10">
                      {b.price ? b.price.toFixed(2) : "--"}
                    </span>
                  </div>
                );
              })}
            </div>
          </div>

          {/* Asks Column */}
          <div>
            <div className="flex items-center justify-between text-[10px] text-slate-400 uppercase font-semibold px-2 py-1 bg-slate-50 rounded-lg mb-1">
              <span className="text-rose-700">Ask (₹)</span>
              <span>Volume</span>
              <span>Orders</span>
            </div>

            <div className="space-y-1">
              {safeAsks.map((a, i) => {
                const barWidth = Math.min(100, (a.qty / maxTierVol) * 100);
                return (
                  <div
                    key={`ask-${i}`}
                    className="relative flex items-center justify-between px-2 py-1 rounded-lg overflow-hidden border border-slate-100 hover:border-rose-200 transition-colors"
                  >
                    {/* Background Volume Bar */}
                    <div
                      className="absolute top-0 bottom-0 left-0 bg-rose-500/10 pointer-events-none transition-all duration-300"
                      style={{ width: `${barWidth}%` }}
                    />

                    <span className="text-rose-700 font-bold relative z-10">
                      {a.price ? a.price.toFixed(2) : "--"}
                    </span>
                    <span className="text-slate-700 font-semibold relative z-10">
                      {a.qty ? a.qty.toLocaleString("en-IN") : "--"}
                    </span>
                    <span className="text-slate-400 text-[10px] relative z-10">{a.orders || "--"}</span>
                  </div>
                );
              })}
            </div>
          </div>
        </div>

        {/* Total Book Quantities */}
        <div className="flex items-center justify-between text-xs font-mono py-2 px-3 mt-3 bg-slate-50 rounded-xl border border-slate-100">
          <span className="text-emerald-700 font-bold">
            Total Bids: {totalBidQty ? totalBidQty.toLocaleString("en-IN") : "--"}
          </span>
          <span className="text-slate-400 text-[10px]">
            Ratio: {rawBuyerPct.toFixed(1)}% / {(100 - rawBuyerPct).toFixed(1)}%
          </span>
          <span className="text-rose-700 font-bold">
            Total Asks: {totalAskQty ? totalAskQty.toLocaleString("en-IN") : "--"}
          </span>
        </div>
      </div>

      {/* Dual Pressure Gauges */}
      <div className="mt-5 pt-4 border-t border-slate-100 space-y-3 font-mono text-xs">
        {/* Anti-Spoof Imbalance Gauge */}
        <div>
          <div className="flex items-center justify-between text-[11px] mb-1">
            <span className="font-semibold text-slate-700 flex items-center gap-1">
              Anti-Spoof Weighted Depth
              <span className="text-[10px] text-slate-400 font-normal">(60% Top / 40% Outer)</span>
            </span>
            <span className={`font-bold ${antiSpoofPct >= 55 ? "text-emerald-700" : antiSpoofPct <= 45 ? "text-rose-700" : "text-slate-700"}`}>
              {antiSpoofPct.toFixed(1)}% {antiSpoofPct >= 55 ? "BULLISH BIAS" : antiSpoofPct <= 45 ? "BEARISH BIAS" : "NEUTRAL"}
            </span>
          </div>
          <div className="relative w-full h-2.5 bg-rose-100 rounded-full overflow-hidden">
            <div
              className="absolute top-0 bottom-0 left-0 bg-emerald-500 rounded-full transition-all duration-300"
              style={{ width: `${Math.min(100, Math.max(0, antiSpoofPct))}%` }}
            />
          </div>
        </div>

        {/* Lee-Ready Aggressor Tick Flow */}
        <div>
          <div className="flex items-center justify-between text-[11px] mb-1">
            <span className="font-semibold text-slate-700 flex items-center gap-1">
              Lee-Ready Aggressor Tick Flow
              <span className="text-[10px] text-slate-400 font-normal">(Ask Lifts vs Bid Hits)</span>
            </span>
            <span className={`font-bold ${aggressorPct >= 55 ? "text-emerald-700" : aggressorPct <= 45 ? "text-rose-700" : "text-slate-700"}`}>
              {aggressorPct.toFixed(1)}% Buying Flow
            </span>
          </div>
          <div className="relative w-full h-2.5 bg-rose-100 rounded-full overflow-hidden">
            <div
              className="absolute top-0 bottom-0 left-0 bg-emerald-500 rounded-full transition-all duration-300"
              style={{ width: `${Math.min(100, Math.max(0, aggressorPct))}%` }}
            />
          </div>
        </div>
      </div>
    </div>
  );
}
