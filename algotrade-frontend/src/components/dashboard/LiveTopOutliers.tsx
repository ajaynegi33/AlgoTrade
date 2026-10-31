"use client";

import React, { memo } from "react";
import Link from "next/link";
import { StockRankData, decodeReasonFlags } from "@/lib/types";

interface LiveTopOutliersProps {
  topBullish: StockRankData[];
  topBearish?: StockRankData[];
}

export const LiveTopOutliers = memo(function LiveTopOutliers({ topBullish }: LiveTopOutliersProps) {
  const activeList = topBullish || [];

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h2 className="text-xl font-black text-slate-900 tracking-tight flex items-center gap-2">
            <span>High-Probability Long Outliers</span>
            <span className="text-xs font-mono font-bold px-2.5 py-0.5 rounded-full bg-emerald-50 text-emerald-800 border border-emerald-300/70">
              AT26LONG CASH MIS
            </span>
          </h2>
          <p className="text-xs text-slate-500 mt-0.5">
            Institutional cash momentum filtered by Sector Rotation, Dual RVOL (&ge; 1.5x), Anti-Spoof Depth, and Pre-Trade Expected Value (EV &ge; +0.25R).
          </p>
        </div>

        <div className="inline-flex items-center gap-2 text-xs font-mono text-slate-500 bg-slate-50 px-3 py-1.5 rounded-xl border border-slate-200">
          <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
          <span>Top 5 Long Picks</span>
        </div>
      </div>

      {/* Hero Cards Grid */}
      {!activeList || activeList.length === 0 ? (
        <div className="bg-white border border-slate-200/90 rounded-3xl p-8 text-center text-slate-400 font-mono text-xs shadow-xs">
          Scanning order book tape for institutional long push-off and VWAP reclaim confluences...
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
          {activeList.map((st) => {
            const badges = decodeReasonFlags(st.reasons);
            const catalysts = badges.filter((b) => b.type === "catalyst");
            const penalties = badges.filter((b) => b.type === "penalty");
            const score = st.bullish_score;
            const isUp = st.change_percent >= 0;
            const buyRatio = st.buyer_ratio > 0 ? st.buyer_ratio : 0.5;
            const buyPercent = Math.min(100, Math.max(0, buyRatio * 100));
            const sellPercent = Math.max(0, 100 - buyPercent);

            return (
              <div
                key={st.token}
                className="bg-white border border-slate-200/90 hover:border-emerald-300 rounded-3xl p-4 shadow-xs hover:shadow-md transition-all flex flex-col justify-between group relative overflow-hidden"
              >
                {/* Accent Top Border */}
                <div className="absolute top-0 left-0 right-0 h-full bg-emerald-200/10 pointer-events-none" />

                <div>
                  {/* Card Header: Rank, Symbol & Score */}
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <div className="flex items-center gap-2">
                        <span
                          className={`text-[11px] font-black font-mono px-2 py-0.5 rounded-md ${
                            st.rank === 1
                              ? "bg-amber-100 text-amber-900 border border-amber-300"
                              : "bg-slate-100 text-slate-700"
                          }`}
                        >
                          #{st.rank}
                        </span>
                        <Link
                          href={`/stock/${encodeURIComponent(st.symbol)}`}
                          className="font-mono font-black text-slate-900 text-base tracking-tight hover:text-emerald-700 hover:underline transition-colors cursor-pointer"
                        >
                          {st.symbol}
                        </Link>
                      </div>
                      <span className="text-[11px] font-mono text-slate-400 mt-0.5 block">
                        {st.sector}
                      </span>
                    </div>

                    {/* Quant Score Badge */}
                    <div className="text-right">
                      <div
                        className={`text-sm font-mono font-black px-2 py-0.5 rounded-lg border ${
                          score >= 85
                            ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                            : score >= 70
                            ? "bg-blue-50 text-blue-800 border-blue-200"
                            : "bg-slate-50 text-slate-800 border-slate-200"
                        }`}
                      >
                        {score.toFixed(1)}
                      </div>
                      <span className="text-[9px] font-mono text-slate-400 uppercase">Score</span>
                    </div>
                  </div>

                  {/* Price & VWAP */}
                  <div className="mt-3 pt-3 border-t border-slate-100 flex items-baseline justify-between">
                    <div>
                      <div className="text-lg font-mono font-black text-slate-900">
                        ₹{st.ltp.toLocaleString("en-IN", { minimumFractionDigits: 2 })}
                      </div>
                      <div className="text-[11px] font-mono text-slate-400">
                        VWAP: ₹{st.vwap.toFixed(2)}
                      </div>
                    </div>

                    <div
                      className={`text-xs font-mono font-bold px-2 py-0.5 rounded-lg ${
                        isUp ? "bg-emerald-50 text-emerald-700" : "bg-rose-50 text-rose-700"
                      }`}
                    >
                      {isUp ? "+" : ""}
                      {st.change_percent.toFixed(2)}%
                    </div>
                  </div>

                  {/* Microstructure Metrics Grid */}
                  <div className="mt-3.5 grid grid-cols-2 gap-2 bg-slate-50/80 p-2.5 rounded-2xl border border-slate-100 text-[11px] font-mono">
                    <div>
                      <span className="text-slate-400 block text-[10px]">DUAL RVOL</span>
                      <span
                        className={`font-bold ${
                          st.rvol_session >= 1.8 ? "text-emerald-700" : "text-slate-800"
                        }`}
                      >
                        {st.rvol_session.toFixed(1)}x
                      </span>
                    </div>

                    <div>
                      <span className="text-slate-400 block text-[10px]">BUYER DEPTH</span>
                      <span className="font-bold text-slate-800">
                        {(st.buyer_ratio * 100).toFixed(0)}%
                      </span>
                    </div>

                    <div>
                      <span className="text-slate-400 block text-[10px]">EXP. VALUE (EV)</span>
                      <span
                        className={`font-bold ${
                          st.ev >= 0.25 ? "text-emerald-700" : "text-slate-600"
                        }`}
                      >
                        {st.ev >= 0 ? "+" : ""}
                        {st.ev.toFixed(2)}R
                      </span>
                    </div>

                    <div>
                      <span className="text-slate-400 block text-[10px]">P(WIN) ODDS</span>
                      <span className="font-bold text-slate-800">
                        {(st.p_win * 100).toFixed(0)}%
                      </span>
                    </div>
                  </div>

                  {/* Concrete Stop & Target */}
                  <div className="mt-2.5 flex items-center justify-between text-[10px] font-mono text-slate-500 px-1">
                    <span>
                      Stop: <strong className="text-slate-800">₹{st.stop_proxy.toFixed(1)}</strong>
                    </span>
                    <span>
                      Target: <strong className="text-slate-800">₹{st.target_proxy.toFixed(1)}</strong>
                    </span>
                  </div>

                  {/* Real-Time Order Depth Bar (Buy vs Sell Orders) */}
                  <div className="mt-3 pt-2.5 border-t border-slate-100">
                    <div className="flex items-center justify-between mb-1.5 font-sans">
                      <div>
                        <span className="text-[10px] text-slate-400 font-medium block">
                          Buy orders
                        </span>
                        <span className="text-xs sm:text-[13px] font-mono font-bold text-slate-900">
                          {buyPercent.toFixed(2)}%
                        </span>
                      </div>
                      <div className="text-right">
                        <span className="text-[10px] text-slate-400 font-medium block">
                          Sell orders
                        </span>
                        <span className="text-xs sm:text-[13px] font-mono font-bold text-slate-900">
                          {sellPercent.toFixed(2)}%
                        </span>
                      </div>
                    </div>

                    {/* Dual Segmented Progress Bar */}
                    <div className="w-full h-1.5 bg-slate-100 rounded-full overflow-hidden flex">
                      <div
                        className="bg-emerald-500 h-full transition-all duration-300"
                        style={{ width: `${buyPercent}%` }}
                      />
                      <div
                        className="bg-[#ea5545] h-full transition-all duration-300"
                        style={{ width: `${sellPercent}%` }}
                      />
                    </div>
                  </div>
                </div>

                {/* Reason Catalysts & Penalties */}
                <div className="mt-4 pt-3 border-t border-slate-100 flex flex-wrap gap-1.5">
                  {catalysts.slice(0, 3).map((cat) => (
                    <span
                      key={cat.label}
                      title={cat.description}
                      className="px-2 py-0.5 rounded-md text-[10px] font-mono font-bold bg-emerald-50 text-emerald-800 border border-emerald-200/80"
                    >
                      {cat.label}
                    </span>
                  ))}

                  {penalties.map((pen) => (
                    <span
                      key={pen.label}
                      title={pen.description}
                      className="px-2 py-0.5 rounded-md text-[10px] font-mono font-bold bg-rose-50 text-rose-800 border border-rose-200/80"
                    >
                      {pen.label}
                    </span>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
});
