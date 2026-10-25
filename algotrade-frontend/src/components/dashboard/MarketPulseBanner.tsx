"use client";

import React from "react";
import { MarketPulseData } from "@/lib/types";

interface MarketPulseBannerProps {
  pulse?: MarketPulseData;
  isConnected?: boolean;
  lastUpdated?: Date | null;
}

export function MarketPulseBanner({ pulse }: MarketPulseBannerProps) {
  const niftyLTP = pulse?.nifty_ltp ?? 0;
  const niftyChg = pulse?.nifty_change_percent ?? 0;
  const bankLTP = pulse?.banknifty_ltp ?? 0;
  const bankChg = pulse?.banknifty_change_percent ?? 0;
  const breadth = pulse?.breadth_above_vwap ?? 50;
  const regime = pulse?.market_regime ?? "SIDEWAYS_CHOP";

  const isNiftyUp = niftyChg >= 0;
  const isBankUp = bankChg >= 0;

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3.5">
      {/* 1. NIFTY 50 Tile */}
      <div className="bg-white border border-slate-200/90 rounded-2xl p-4 sm:p-4.5 shadow-2xs hover:border-slate-300 transition-colors">
        <span className="text-[11px] font-mono font-bold uppercase tracking-wider text-slate-400 block">
          Nifty 50
        </span>
        <div className="flex items-baseline justify-between gap-2 mt-2">
          <span className="text-base sm:text-lg font-mono font-black text-slate-900 tracking-tight">
            {niftyLTP > 0
              ? `₹${niftyLTP.toLocaleString("en-IN", { minimumFractionDigits: 2 })}`
              : "—"}
          </span>
          <span
            className={`text-xs font-mono font-bold px-2 py-0.5 rounded-lg border ${
              isNiftyUp
                ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                : "bg-rose-50 text-rose-800 border-rose-200"
            }`}
          >
            {isNiftyUp ? "+" : ""}
            {niftyChg.toFixed(2)}%
          </span>
        </div>
      </div>

      {/* 2. BANK NIFTY Tile */}
      <div className="bg-white border border-slate-200/90 rounded-2xl p-4 sm:p-4.5 shadow-2xs hover:border-slate-300 transition-colors">
        <span className="text-[11px] font-mono font-bold uppercase tracking-wider text-slate-400 block">
          Bank Nifty
        </span>
        <div className="flex items-baseline justify-between gap-2 mt-2">
          <span className="text-base sm:text-lg font-mono font-black text-slate-900 tracking-tight">
            {bankLTP > 0
              ? `₹${bankLTP.toLocaleString("en-IN", { minimumFractionDigits: 2 })}`
              : "—"}
          </span>
          <span
            className={`text-xs font-mono font-bold px-2 py-0.5 rounded-lg border ${
              isBankUp
                ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                : "bg-rose-50 text-rose-800 border-rose-200"
            }`}
          >
            {isBankUp ? "+" : ""}
            {bankChg.toFixed(2)}%
          </span>
        </div>
      </div>

      {/* 3. Market Breadth Tile */}
      <div className="bg-white border border-slate-200/90 rounded-2xl p-4 sm:p-4.5 shadow-2xs hover:border-slate-300 transition-colors">
        <span className="text-[11px] font-mono font-bold uppercase tracking-wider text-slate-400 block">
          Market Breadth
        </span>
        <div className="flex items-baseline justify-between gap-2 mt-2">
          <span className="text-base sm:text-lg font-mono font-black text-slate-900 tracking-tight">
            {breadth.toFixed(1)}%
          </span>
          <span className="text-xs font-mono text-slate-500 font-medium">
            &gt; VWAP
          </span>
        </div>
        <div className="w-full h-1.5 bg-slate-100 rounded-full overflow-hidden mt-2">
          <div
            className={`h-full rounded-full transition-all duration-500 ${
              breadth >= 60 ? "bg-emerald-500" : breadth <= 40 ? "bg-rose-500" : "bg-amber-500"
            }`}
            style={{ width: `${Math.min(100, Math.max(0, breadth))}%` }}
          />
        </div>
      </div>

      {/* 4. Market Regime Tile */}
      <div className="bg-white border border-slate-200/90 rounded-2xl p-4 sm:p-4.5 shadow-2xs hover:border-slate-300 transition-colors">
        <span className="text-[11px] font-mono font-bold uppercase tracking-wider text-slate-400 block">
          Market Regime
        </span>
        <div className="mt-2 flex items-center">
          <span
            className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-xl text-xs font-mono font-black border ${
              regime === "BULLISH_EXPANSION" || regime === "V_RECOVERY_BULLISH"
                ? "bg-emerald-50 text-emerald-900 border-emerald-300"
                : regime === "BEARISH_EXPANSION" || regime === "FLASH_SHOCK" || regime === "INTRADAY_DISTRIBUTION"
                ? "bg-rose-50 text-rose-900 border-rose-300"
                : "bg-slate-100 text-slate-800 border-slate-200"
            }`}
          >
            <span className="text-xs">
              {regime === "BULLISH_EXPANSION" || regime === "V_RECOVERY_BULLISH"
                ? "▲"
                : regime === "BEARISH_EXPANSION" || regime === "FLASH_SHOCK" || regime === "INTRADAY_DISTRIBUTION"
                ? "▼"
                : "◆"}
            </span>
            <span>{regime.replace(/_/g, " ")}</span>
          </span>
        </div>
      </div>
    </div>
  );
}
