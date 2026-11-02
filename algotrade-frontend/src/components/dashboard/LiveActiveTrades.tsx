"use client";

import React, { memo, useMemo } from "react";
import Link from "next/link";
import { ActivePositionData } from "@/lib/types";
import { computeStopLossRadar } from "@/lib/stopLossRadar";
import { toast } from "sonner";

interface LiveActiveTradesProps {
  activePositions?: ActivePositionData[];
  closedPositions?: ActivePositionData[];
  isUpdating: boolean;
  onFlattenPosition: (positionId: string) => Promise<number>;
}

function getGearBadge(gear: number, gearLabel?: string): { label: string; style: string } {
  switch (gear) {
    case 4:
      return {
        label: gearLabel || "GEAR 4 • VIRTUAL SHADOW SL",
        style: "bg-purple-50 text-purple-800 border-purple-300",
      };
    case 3:
      return {
        label: gearLabel || "GEAR 3 • COARSE RATCHET",
        style: "bg-amber-50 text-amber-800 border-amber-300",
      };
    case 2:
      return {
        label: gearLabel || "GEAR 2 • STRUCTURE LOCK",
        style: "bg-indigo-50 text-indigo-800 border-indigo-300",
      };
    default:
      return {
        label: gearLabel || "GEAR 1 • AGILE SUB-1R",
        style: "bg-emerald-50 text-emerald-800 border-emerald-300",
      };
  }
}

export const LiveActiveTrades = memo(function LiveActiveTrades({
  activePositions = [],
  closedPositions = [],
  isUpdating,
  onFlattenPosition,
}: LiveActiveTradesProps) {
  // Stably sort positions strictly by chronological trade entry order (FIFO).
  // Prevents cards from swapping grid positions when live prices or telemetry update.
  const sortedPositions = useMemo(() => {
    return [...activePositions].sort((a, b) => {
      const timeA = a.opened_at_unix ?? (a.opened_at ? new Date(`1970-01-01T${a.opened_at}Z`).getTime() : 0);
      const timeB = b.opened_at_unix ?? (b.opened_at ? new Date(`1970-01-01T${b.opened_at}Z`).getTime() : 0);
      if (timeA !== timeB) return timeA - timeB;
      return a.position_id.localeCompare(b.position_id);
    });
  }, [activePositions]);

  const handleFlattenSingle = async (pos: ActivePositionData) => {
    if (isUpdating) return;
    try {
      await onFlattenPosition(pos.position_id);
      toast.success(`Flatten order dispatched for ${pos.symbol}`);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : `Failed to flatten ${pos.symbol}`;
      toast.error(msg);
    }
  };

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs font-mono space-y-5">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 border-b border-slate-100 pb-4">
        <div>
          <div className="flex items-center gap-2">
            <h3 className="text-xs font-black text-slate-900 uppercase tracking-wider">
              Live Autonomous Position Book
            </h3>
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-slate-100 text-slate-800">
              {activePositions.length} OPEN
            </span>
          </div>
          <p className="text-[11px] text-slate-500 mt-0.5">
            4-Gear Dynamic Stop Loss Ratchet • 25-Mod Cap • 3-Step Race-Free Scale-Out at +1.0R
          </p>
        </div>

        <div className="flex items-center gap-3 text-[11px] text-slate-500">
          <span>
            Closed Today: <strong className="text-slate-900">{closedPositions.length}</strong>
          </span>
          <Link
            href="/trades"
            className="px-3 py-1 rounded-xl bg-emerald-50 hover:bg-emerald-100 text-emerald-800 border border-emerald-200 font-bold transition-colors"
          >
            Trade &amp; Tax Ledger →
          </Link>
        </div>
      </div>

      {/* Active Positions Grid */}
      {activePositions.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-slate-200 bg-slate-50/50 p-6 text-center">
          <p className="text-xs font-bold text-slate-600">
            No Open Intraday Positions
          </p>
          <p className="text-[11px] text-slate-400 mt-1">
            When Auto-Execution is ARMED and Stage 0 + Session Clock gates are green, VWAP Pullback Reclaim and 15-Minute ORB Breakout entries appear here in real time.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {sortedPositions.map((pos) => {
            const gear = getGearBadge(pos.current_gear, pos.gear_label);
            const modPct = Math.min(
              100,
              (pos.mod_count / Math.max(1, pos.max_mods || 25)) * 100
            );
            const totalNetPnl = pos.net_total_pnl;
            const target1R = pos.entry_price + pos.one_r;
            // Virtual shadow stop is ONLY active in Gear 4 (mod_count >= 24). In Gears 1-3, active SL is strictly current_stop.
            const isGear4 = pos.current_gear === 4 || pos.mod_count >= 24;
            const effectiveStop =
              isGear4 && pos.shadow_stop > 0 ? pos.shadow_stop : pos.current_stop;
            const stopDistRupees = Math.max(0, pos.ltp - effectiveStop);
            const stopDistPct = pos.ltp > 0 ? (stopDistRupees / pos.ltp) * 100 : 0;
            const isTightStop = stopDistPct < 0.35;

            // Map R-Multiple along [-1.0R ... +2.0R] track (0% at -1.0R, 33.3% at 0R Entry, 66.7% at +1.0R, 100% at +2.0R+)
            const rProgressPct = Math.min(
              100,
              Math.max(0, ((pos.r_multiple + 1.0) / 3.0) * 100)
            );
            const stopR =
              pos.one_r > 0 ? (effectiveStop - pos.entry_price) / pos.one_r : -1.0;
            const stopProgressPct = Math.min(
              100,
              Math.max(0, ((stopR + 1.0) / 3.0) * 100)
            );

            const radar = computeStopLossRadar(pos);

            return (
              <div
                key={pos.position_id}
                className="rounded-2xl border border-slate-200/90 bg-slate-50/40 p-4 sm:p-5 space-y-4"
              >
                {/* Card Top: Symbol, Pattern, State, Gear Badge */}
                <div className="flex flex-wrap items-start justify-between gap-2 min-w-0">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 min-w-0">
                      <Link
                        href={`/stock/${encodeURIComponent(pos.symbol)}`}
                        className="text-base font-black text-slate-900 hover:text-emerald-700 hover:underline transition-colors cursor-pointer truncate"
                      >
                        {pos.symbol}
                      </Link>
                      <span className="text-[10px] font-bold px-2 py-0.5 rounded bg-slate-900 text-white shrink-0">
                        {pos.state}
                      </span>
                      <span className="text-[10px] font-bold px-2 py-0.5 rounded bg-slate-200/80 text-slate-700 shrink-0">
                        {pos.pattern_type}
                      </span>
                    </div>
                    <span className="text-[10px] text-slate-400 block mt-0.5 truncate">
                      ID: {pos.position_id} • {pos.mode} • Opened {pos.opened_at || "Live"}
                    </span>
                  </div>

                  <span
                    className={`text-[10px] font-bold px-2.5 py-1 rounded-full border shrink-0 ${gear.style}`}
                  >
                    {gear.label}
                  </span>
                </div>

                {/* Price Telemetry Grid */}
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5 text-xs">
                  <div className="p-2.5 rounded-xl bg-white border border-slate-200/80">
                    <span className="text-[10px] text-slate-400 block uppercase">Entry / BE</span>
                    <strong className="text-slate-900 font-black">
                      ₹{pos.entry_price.toFixed(2)}
                    </strong>
                    <span className="text-[10px] text-slate-400 block">
                      BE: ₹{pos.breakeven_stop.toFixed(2)}
                    </span>
                  </div>

                  <div className="p-2.5 rounded-xl bg-white border border-slate-200/80">
                    <span className="text-[10px] text-slate-400 block uppercase">Live LTP / HWM</span>
                    <strong className="text-slate-900 font-black">
                      ₹{pos.ltp.toFixed(2)}
                    </strong>
                    <span className="text-[10px] text-emerald-700 block font-semibold">
                      HWM: ₹{pos.high_water_mark.toFixed(2)}
                    </span>
                  </div>

                  <div className="p-2.5 rounded-xl bg-white border border-slate-200/80">
                    <span className="text-[10px] text-slate-400 block uppercase">
                      {pos.tranche1_done ? "Runner Buffer (Δ)" : "Target +1.0R"}
                    </span>
                    <strong className="text-indigo-700 font-black">
                      ₹{(pos.tranche1_done ? pos.base_buffer : target1R).toFixed(2)}
                    </strong>
                    <span className="text-[10px] text-slate-400 block">
                      1R = ₹{pos.one_r.toFixed(2)}
                    </span>
                  </div>

                  <div className="p-2.5 rounded-xl bg-white border border-slate-200/80">
                    <span className="text-[10px] text-slate-400 block uppercase">Live R-Multiple</span>
                    <strong
                      className={`font-black ${
                        pos.r_multiple >= 0 ? "text-emerald-700" : "text-rose-700"
                      }`}
                    >
                      {pos.r_multiple >= 0 ? "+" : ""}
                      {pos.r_multiple.toFixed(2)}R
                    </strong>
                    <span className="text-[10px] text-slate-400 block">
                      Peak: +{pos.mfe_r_multiple.toFixed(2)}R
                    </span>
                  </div>
                </div>

                {/* =========================================================================
                    DYNAMIC STOP LOSS & RATCHET RADAR (Current SL -> Next SL -> Activation)
                    ========================================================================= */}
                <div className="rounded-xl border border-slate-200/90 bg-white p-3 space-y-2.5">
                  <div className="flex items-center justify-between border-b border-slate-100 pb-1.5">
                    <div className="flex items-center gap-2">
                      <span className="w-2 h-2 rounded-full bg-rose-500 animate-pulse" />
                      <span className="text-[11px] font-black text-slate-900 uppercase tracking-wider">
                        Stop Loss &amp; Trailing Radar
                      </span>
                    </div>
                    <span className="text-[10px] font-bold text-slate-500">
                      {radar.currentStopType === "RAM_SHADOW" ? (
                        <span className="px-2 py-0.5 rounded bg-purple-100 text-purple-800 border border-purple-200">
                          RAM VIRTUAL SHADOW
                        </span>
                      ) : (
                        <span className="px-2 py-0.5 rounded bg-slate-100 text-slate-700 border border-slate-200">
                          NSE EXCHANGE RESTING
                        </span>
                      )}
                    </span>
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-xs">
                    {/* 1. CURRENT STOP LOSS */}
                    <div className="p-2.5 rounded-lg bg-rose-50/50 border border-rose-100 space-y-1">
                      <span className="text-[10px] font-bold text-rose-700 uppercase tracking-wider block">
                        Current Stop Loss
                      </span>
                      <div className="text-base font-black text-rose-800">
                        ₹{radar.currentStop.toFixed(2)}
                      </div>
                      <div className="text-[10px] text-rose-900 font-medium">
                        {radar.currentStopLabel}
                      </div>
                      <div className="text-[10px] text-slate-500">
                        Dist: <strong className={isTightStop ? "text-amber-700 font-bold" : "text-slate-700"}>
                          ₹{stopDistRupees.toFixed(2)} ({stopDistPct.toFixed(2)}%)
                        </strong>
                      </div>
                    </div>

                    {/* 2. NEXT STOP LOSS PRICE */}
                    <div className="p-2.5 rounded-lg bg-amber-50/50 border border-amber-100 space-y-1">
                      <span className="text-[10px] font-bold text-amber-800 uppercase tracking-wider block">
                        Next Stop Loss Price
                      </span>
                      <div className="text-base font-black text-amber-900">
                        ₹{radar.nextStopPrice.toFixed(2)}
                      </div>
                      <div className="text-[10px] text-amber-900 font-medium truncate" title={radar.nextStopLabel}>
                        {radar.nextStopLabel}
                      </div>
                      <div className="text-[10px] text-slate-500 truncate" title={radar.nextStopMilestone}>
                        {radar.nextStopMilestone}
                      </div>
                    </div>

                    {/* 3. WHEN NEXT SL ACTIVATES */}
                    <div className="p-2.5 rounded-lg bg-emerald-50/50 border border-emerald-100 space-y-1">
                      <span className="text-[10px] font-bold text-emerald-800 uppercase tracking-wider block">
                        When Next SL Activates
                      </span>
                      <div className="text-[11px] font-black text-emerald-950 leading-tight">
                        {radar.triggerCondition}
                      </div>
                      <div className="text-[10px] text-emerald-800 font-semibold">
                        {radar.isTriggerReached ? (
                          <span className="text-emerald-700 font-bold">✓ Milestone Reached / Trailing Active</span>
                        ) : (
                          <span>
                            ₹{radar.triggerDistanceRupees.toFixed(2)} away ({radar.triggerDistancePercent.toFixed(2)}% move)
                          </span>
                        )}
                      </div>
                      {/* Trigger Progress Bar */}
                      <div className="w-full h-1.5 rounded-full bg-emerald-100 overflow-hidden mt-1">
                        <div
                          className="h-full w-full bg-emerald-600 origin-left transition-transform duration-200 ease-out"
                          style={{
                            transform: `scaleX(${Math.min(1, Math.max(0, (radar.triggerProgressPercent || 0) / 100))})`,
                          }}
                        />
                      </div>
                    </div>
                  </div>
                </div>

                {/* Live R-Trajectory & Stop Ratchet Track (-1.0R -> 0R -> +1.0R -> +2.0R+) */}
                <div className="space-y-1 bg-white rounded-xl p-2.5 border border-slate-200/70">
                  <div className="flex items-center justify-between text-[10px]">
                    <span className="text-slate-500 font-bold uppercase">
                      R-Trajectory (Stop @ {stopR >= 0 ? "+" : ""}
                      {stopR.toFixed(2)}R • Peak +{pos.mfe_r_multiple.toFixed(2)}R)
                    </span>
                    <span className="font-bold text-slate-700">
                      Live:{" "}
                      <strong
                        className={
                          pos.r_multiple >= 0 ? "text-emerald-700" : "text-rose-700"
                        }
                      >
                        {pos.r_multiple >= 0 ? "+" : ""}
                        {pos.r_multiple.toFixed(2)}R
                      </strong>
                    </span>
                  </div>
                  <div className="relative w-full h-2.5 rounded-full bg-slate-100 overflow-hidden border border-slate-200/80">
                    {/* Locked Stop Floor Marker */}
                    <div
                      className="absolute top-0 bottom-0 left-0 w-full bg-rose-200/70 border-r-2 border-rose-600 origin-left transition-transform duration-200 ease-out"
                      style={{
                        transform: `scaleX(${Math.min(1, Math.max(0, stopProgressPct / 100))})`,
                      }}
                      title={`Active Stop Floor: ${stopR.toFixed(2)}R`}
                    />
                    {/* Live R Fill */}
                    <div
                      className={`h-full w-full origin-left transition-transform duration-200 ease-out ${
                        pos.r_multiple >= 1.0
                          ? "bg-emerald-600"
                          : pos.r_multiple >= 0
                          ? "bg-teal-500"
                          : "bg-amber-500"
                      }`}
                      style={{
                        transform: `scaleX(${Math.min(1, Math.max(0, rProgressPct / 100))})`,
                      }}
                    />
                  </div>
                  <div className="flex justify-between text-[9px] text-slate-400 font-bold pt-0.5">
                    <span>-1.0R (Init SL)</span>
                    <span>0R (Breakeven)</span>
                    <span>+1.0R (50% Bank)</span>
                    <span>+2.0R+ (Runner)</span>
                  </div>
                </div>

                {/* 25-Modification Budget Progress Bar */}
                <div className="space-y-1">
                  <div className="flex items-center justify-between text-[10px]">
                    <span className="text-slate-500 font-bold uppercase">
                      NSE Order Modification Budget ({pos.mod_count}/{pos.max_mods || 25})
                    </span>
                    <span className="text-slate-500">
                      {pos.tranche1_done ? "Breakeven Locked (₹0 Risk) • 100% Runner Active" : "Pre-1R Initial Risk Active"}
                    </span>
                  </div>
                  <div className="w-full h-1.5 rounded-full bg-slate-200 overflow-hidden">
                    <div
                      className={`h-full w-full origin-left transition-transform duration-200 ease-out ${
                        pos.mod_count >= 24
                          ? "bg-purple-600"
                          : pos.mod_count >= 19
                          ? "bg-amber-500"
                          : "bg-emerald-600"
                      }`}
                      style={{
                        transform: `scaleX(${Math.min(1, Math.max(0, modPct / 100))})`,
                      }}
                    />
                  </div>
                </div>

                {/* Bottom Row: Quantity, Net PnL After Taxes, and 1-Click Flatten */}
                <div className="pt-3 border-t border-slate-200/80 flex flex-wrap items-center justify-between gap-3">
                  <div className="text-xs space-x-3">
                    <span className="text-slate-500">
                      Qty:{" "}
                      <strong className="text-slate-900">
                        {pos.remaining_qty}/{pos.initial_qty}
                      </strong>
                    </span>
                    <span className="text-slate-500">
                      Net PnL (Tax-Adj):{" "}
                      <strong
                        className={`font-black ${
                          totalNetPnl >= 0 ? "text-emerald-700" : "text-rose-700"
                        }`}
                      >
                        {totalNetPnl >= 0 ? "+" : ""}₹{totalNetPnl.toFixed(2)}
                      </strong>
                    </span>
                  </div>

                  <button
                    type="button"
                    disabled={isUpdating}
                    onClick={() => handleFlattenSingle(pos)}
                    className={`px-3.5 py-1.5 rounded-xl text-white text-xs font-black cursor-pointer transition-colors ${
                      pos.state === "EMERGENCY_FLATTENING"
                        ? "bg-amber-600 hover:bg-amber-700 animate-pulse"
                        : "bg-rose-600 hover:bg-rose-700"
                    }`}
                  >
                    {pos.state === "EMERGENCY_FLATTENING" ? "Force Flatten" : "Flatten Position"}
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Closed Positions Summary Link to /trades */}
      {closedPositions.length > 0 && (
        <div className="pt-4 border-t border-slate-100 flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-xs">
          <div className="text-slate-600">
            <strong className="text-slate-900">{closedPositions.length}</strong> intraday position(s) closed today. View itemized Indian contract-note taxes, hold duration, and R-multiple audit in the ledger.
          </div>
          <Link
            href="/trades"
            className="text-xs font-black text-emerald-700 hover:text-emerald-800 hover:underline shrink-0"
          >
            Open Full Trade &amp; Tax Ledger ({closedPositions.length}) →
          </Link>
        </div>
      )}
    </div>
  );
});
