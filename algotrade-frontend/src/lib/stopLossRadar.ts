import type { ActivePositionData } from "./types";

export interface StopLossRadarTelemetry {
  currentStop: number;
  currentStopLabel: string;
  currentStopType: "EXCHANGE_RESTING" | "RAM_SHADOW";
  nextStopPrice: number;
  nextStopLabel: string;
  nextStopMilestone: string;
  triggerCondition: string;
  triggerDistanceRupees: number;
  triggerDistancePercent: number;
  triggerProgressPercent: number;
  isTriggerReached: boolean;
}

/**
 * Computes exact real-time Stop Loss Radar metrics across Gears 1 to 4 for Cash Intraday Equity.
 */
export function computeStopLossRadar(pos: ActivePositionData): StopLossRadarTelemetry {
  const isGear4 = pos.current_gear === 4 || pos.mod_count >= 24;
  const currentStop = isGear4 && pos.shadow_stop > 0 ? pos.shadow_stop : pos.current_stop;
  const currentStopType: "EXCHANGE_RESTING" | "RAM_SHADOW" = isGear4 ? "RAM_SHADOW" : "EXCHANGE_RESTING";

  const target1R = pos.entry_price + pos.one_r;
  const target1_5R = pos.entry_price + 1.5 * pos.one_r;

  // State 1: Gear 1 (Sub-1.0R, before Breakeven lock)
  if (pos.mfe_r_multiple < 1.0 && !pos.tranche1_done) {
    const dist = Math.max(0, target1R - pos.ltp);
    const distPct = pos.ltp > 0 ? (dist / pos.ltp) * 100 : 0;
    const progress = Math.min(100, Math.max(0, (pos.r_multiple / 1.0) * 100));

    return {
      currentStop,
      currentStopLabel: "Initial Stop (Swing Low / VWAP)",
      currentStopType,
      nextStopPrice: pos.breakeven_stop,
      nextStopLabel: "Tax-Free Breakeven Floor",
      nextStopMilestone: "Locks 0-Loss Guarantee (Brokerage/STT Covered)",
      triggerCondition: `When LTP reaches ₹${target1R.toFixed(2)} (+1.00R)`,
      triggerDistanceRupees: dist,
      triggerDistancePercent: distPct,
      triggerProgressPercent: progress,
      isTriggerReached: dist <= 0,
    };
  }

  // State 2: Gear 2 (1.0R to 1.5R Pre-Scale)
  if (!pos.tranche1_done) {
    const dist = Math.max(0, target1_5R - pos.ltp);
    const distPct = pos.ltp > 0 ? (dist / pos.ltp) * 100 : 0;
    const progress = Math.min(100, Math.max(0, ((pos.r_multiple - 1.0) / 0.5) * 100));

    return {
      currentStop,
      currentStopLabel: "Breakeven Floor (Risk Eradicated)",
      currentStopType,
      nextStopPrice: target1_5R,
      nextStopLabel: "Tranche 1 50% Scale-Out + Runner Anchor",
      nextStopMilestone: "Banks 50% Profit & Locks Runner to 5m Low",
      triggerCondition: `When LTP reaches ₹${target1_5R.toFixed(2)} (+1.50R)`,
      triggerDistanceRupees: dist,
      triggerDistancePercent: distPct,
      triggerProgressPercent: progress,
      isTriggerReached: dist <= 0,
    };
  }

  // State 3: Gear 3 (Runner Bar-by-Bar Trailing Stop)
  if (!isGear4) {
    const estimatedNextStop = currentStop + Math.max(0.10, 0.25 * pos.base_buffer);
    return {
      currentStop,
      currentStopLabel: "Runner Stop (Previous 5m Low - 2 Ticks)",
      currentStopType,
      nextStopPrice: estimatedNextStop,
      nextStopLabel: "Next 5m Higher Low - 2 Ticks",
      nextStopMilestone: "Trails structural pivot lows on completed 5m closes",
      triggerCondition: "On next completed 5m candle close (:00, :05, :10...)",
      triggerDistanceRupees: Math.max(0, pos.ltp - currentStop),
      triggerDistancePercent: pos.ltp > 0 ? (Math.max(0, pos.ltp - currentStop) / pos.ltp) * 100 : 0,
      triggerProgressPercent: 100,
      isTriggerReached: true,
    };
  }

  // State 4: Gear 4 (Virtual Shadow SL at Mod Cap)
  return {
    currentStop,
    currentStopLabel: "RAM Virtual Shadow SL (Exchange Order Parked)",
    currentStopType: "RAM_SHADOW",
    nextStopPrice: pos.shadow_stop,
    nextStopLabel: "Instant Market Dump (Mod #25)",
    nextStopMilestone: "Mod 24/25 reached; Mod #25 reserved for market dump",
    triggerCondition: `Triggers dump immediately if LTP ≤ ₹${pos.shadow_stop.toFixed(2)}`,
    triggerDistanceRupees: Math.max(0, pos.ltp - pos.shadow_stop),
    triggerDistancePercent: pos.ltp > 0 ? (Math.max(0, pos.ltp - pos.shadow_stop) / pos.ltp) * 100 : 0,
    triggerProgressPercent: 100,
    isTriggerReached: false,
  };
}
