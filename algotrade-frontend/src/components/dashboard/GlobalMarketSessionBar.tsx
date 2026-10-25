"use client";

import React, { memo } from "react";
import { GlobalSessionClockData } from "@/lib/types";

interface GlobalMarketSessionBarProps {
  sessionClock?: GlobalSessionClockData;
  marketRegime?: string;
  macroShockActive?: boolean;
}

const CLOCK_FLAGS: Record<string, string> = {
  NSE: "🇮🇳",
  TOKYO: "🇯🇵",
  SHANGHAI: "🇨🇳",
  FRANKFURT: "🇩🇪",
  LONDON: "🇬🇧",
  NEW_YORK: "🇺🇸",
};

export const GlobalMarketSessionBar = memo(function GlobalMarketSessionBar({
  sessionClock,
  marketRegime = "SIDEWAYS_CHOP",
  macroShockActive = false,
}: GlobalMarketSessionBarProps) {
  if (!sessionClock) {
    return null;
  }

  const openMarkets = sessionClock.clocks.filter((c) => c.is_open);

  const regimeBadgeStyle = (() => {
    if (macroShockActive || marketRegime === "FLASH_SHOCK") {
      return "bg-rose-600 text-white border-rose-700 animate-pulse";
    }
    switch (marketRegime) {
      case "BULLISH_EXPANSION":
        return "bg-emerald-50 text-emerald-800 border-emerald-300";
      case "V_RECOVERY_BULLISH":
        return "bg-teal-50 text-teal-800 border-teal-300";
      case "BEARISH_EXPANSION":
      case "INTRADAY_DISTRIBUTION":
        return "bg-rose-50 text-rose-800 border-rose-300";
      default:
        return "bg-amber-50 text-amber-800 border-amber-300";
    }
  })();

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-4 sm:p-5 shadow-xs space-y-3.5">
      {/* Top Row: Active Playbook & Multi-Horizon Regime Status */}
      <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-2.5 border-b border-slate-100 pb-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[11px] font-mono font-bold uppercase tracking-wider text-slate-900">
            Global Session Desk
          </span>

          <span
            className={`text-[10px] font-mono font-bold px-2.5 py-0.5 rounded-full border ${
              sessionClock.allow_new_entries
                ? "bg-emerald-50 text-emerald-800 border-emerald-300"
                : "bg-slate-100 text-slate-700 border-slate-300"
            }`}
          >
            {sessionClock.phase_label}
          </span>

          {sessionClock.tape_override_active && (
            <span className="text-[10px] font-mono font-bold px-2.5 py-0.5 rounded-full bg-indigo-50 text-indigo-800 border border-indigo-300">
              DOMESTIC TAPE OVERRIDE
            </span>
          )}

          <span className={`text-[10px] font-mono font-bold px-2.5 py-0.5 rounded-full border ${regimeBadgeStyle}`}>
            REGIME: {marketRegime.replace(/_/g, " ")}
          </span>

          <span className="text-[10px] font-mono font-bold px-2.5 py-0.5 rounded-full bg-slate-900 text-white">
            OPEN NOW:{" "}
            {openMarkets.length > 0
              ? openMarkets
                  .map((m) => `${CLOCK_FLAGS[m.id] || ""} ${m.name.split(" ")[0]}`)
                  .join(" • ")
              : "None (Off-Hours)"}
          </span>
        </div>

        <div className="flex items-center gap-3 text-[11px] font-mono text-slate-500">
          <span>
            Min Score: <strong className="text-slate-900">{sessionClock.min_bullish_score.toFixed(0)}</strong>
          </span>
          <span>•</span>
          <span>
            Min RVOL: <strong className="text-slate-900">{sessionClock.min_rvol.toFixed(1)}x</strong>
          </span>
          <span>•</span>
          <span>
            Size Mult: <strong className="text-slate-900">{(sessionClock.size_multiplier * 100).toFixed(0)}%</strong>
          </span>
        </div>
      </div>

      {/* Middle Note */}
      <p className="text-xs text-slate-600 font-mono leading-relaxed">
        {sessionClock.playbook_note}
      </p>

      {/* Bottom Row: 6 DST-Aware Global Exchange Clocks */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-2.5 pt-1">
        {sessionClock.clocks.map((clock) => {
          const dotColor =
            clock.status === "OPEN"
              ? "bg-emerald-500 animate-pulse"
              : clock.status === "PRE_OPEN" || clock.status === "LUNCH"
              ? "bg-amber-500"
              : "bg-slate-300";

          return (
            <div
              key={clock.id}
              className={`rounded-2xl border p-2.5 font-mono transition-colors ${
                clock.is_open
                  ? "bg-emerald-50/40 border-emerald-300/90 shadow-2xs"
                  : "bg-slate-50/70 border-slate-200/70"
              }`}
            >
              <div className="flex items-center justify-between gap-1">
                <span className="text-[10px] font-bold text-slate-800 truncate flex items-center gap-1">
                  <span>{CLOCK_FLAGS[clock.id] || "🌐"}</span>
                  <span className="truncate">{clock.name}</span>
                </span>
                <span className={`w-2 h-2 rounded-full shrink-0 ${dotColor}`} />
              </div>
              <div className="flex items-baseline justify-between mt-1">
                <span className="text-sm font-black text-slate-900">
                  {clock.local_time}
                </span>
                <span
                  className={`text-[9px] font-black px-1.5 py-0.5 rounded ${
                    clock.is_open
                      ? "bg-emerald-600 text-white"
                      : clock.status === "LUNCH" || clock.status === "PRE_OPEN"
                      ? "bg-amber-100 text-amber-800"
                      : "bg-slate-200/70 text-slate-500"
                  }`}
                >
                  {clock.is_open ? "OPEN" : clock.status}
                </span>
              </div>
              <div className="text-[10px] text-slate-500 truncate mt-0.5">
                {clock.status_label}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
});
