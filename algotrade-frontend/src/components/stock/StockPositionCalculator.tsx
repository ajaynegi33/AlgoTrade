"use client";

import React, { useState, useMemo, useEffect } from "react";
import { StockDetailData } from "@/lib/types";
import { useTradingStore } from "@/store/useTradingStore";

interface StockPositionCalculatorProps {
  stock: StockDetailData;
}

export function StockPositionCalculator({ stock }: StockPositionCalculatorProps) {
  const { executionState, fetchExecutionState } = useTradingStore();
  const [stopMode, setStopMode] = useState<"VWAP" | "ORL" | "DAY_LOW">("VWAP");

  useEffect(() => {
    if (!executionState) {
      fetchExecutionState();
    }
  }, [executionState, fetchExecutionState]);

  const riskBudget = executionState?.capital?.current_risk_budget || 750;

  // Selected stop reference
  const effectiveStop = useMemo(() => {
    if (stopMode === "ORL" && stock.has_or15m && stock.orl > 0) {
      return stock.orl;
    }
    if (stopMode === "DAY_LOW" && stock.low > 0) {
      return Math.max(0.05, stock.low - (stock.tick_size || 0.05) * 2);
    }
    // Default VWAP buffer
    if (stock.vwap > 0) {
      const buffer = stock.atr14 > 0 ? stock.atr14 * 0.25 : stock.ltp * 0.003;
      return Math.max(0.05, stock.vwap - buffer);
    }
    return stock.stop_proxy > 0 ? stock.stop_proxy : stock.ltp * 0.99;
  }, [stopMode, stock]);

  const entryPrice = stock.ltp;
  const stopDistance = Math.max(stock.tick_size || 0.05, entryPrice - effectiveStop);
  const stopDistancePct = entryPrice > 0 ? (stopDistance / entryPrice) * 100 : 0;

  // Engine Dynamic Sizing: floor(RiskBudget / StopDistance)
  const dynamicQuantity = useMemo(() => {
    if (stopDistance <= 0) return 1;
    return Math.max(1, Math.floor(riskBudget / stopDistance));
  }, [riskBudget, stopDistance]);

  const totalCapitalRequired = dynamicQuantity * entryPrice;
  const mis5xMarginRequired = totalCapitalRequired / 5.0;
  const actualRiskAtStop = dynamicQuantity * stopDistance;

  // Milestones
  const oneR = stopDistance;
  const breakevenTarget = entryPrice + 1.0 * oneR;
  const tranche1Target = entryPrice + 1.5 * oneR;
  const tranche1Shares = Math.floor(dynamicQuantity * 0.5);
  const runnerShares = dynamicQuantity - tranche1Shares;

  // SL-Limit tick buffer (0.5% or 4 ticks below trigger)
  const slTriggerPrice = effectiveStop;
  const slLimitPrice = Math.max(0.05, effectiveStop - Math.max(4 * (stock.tick_size || 0.05), effectiveStop * 0.005));

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs font-mono space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-slate-100 pb-4">
        <div>
          <div className="flex items-center gap-2">
            <h3 className="text-xs font-black text-slate-900 uppercase tracking-wider">
              Autonomous Engine Execution Specification
            </h3>
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-emerald-50 text-emerald-800 border border-emerald-300">
              NSE CASH MIS (5x)
            </span>
          </div>
          <p className="text-[11px] text-slate-500 mt-0.5">
            Dynamic volatility-adjusted position sizing &bull; 1% Fixed Risk &bull; Compliant SL-Limit orders
          </p>
        </div>

        {/* Stop Reference Selector */}
        <div className="flex items-center gap-1.5 bg-slate-100 p-1 rounded-xl border border-slate-200 self-start sm:self-auto text-xs">
          <span className="text-[10px] text-slate-400 font-bold px-2">Stop Anchor:</span>
          <button
            type="button"
            onClick={() => setStopMode("VWAP")}
            className={`px-2.5 py-1 rounded-lg text-[10px] font-bold transition-all cursor-pointer ${
              stopMode === "VWAP"
                ? "bg-white text-slate-900 shadow-2xs"
                : "text-slate-500 hover:text-slate-900"
            }`}
          >
            VWAP Buffer
          </button>
          {stock.has_or15m && stock.orl > 0 && (
            <button
              type="button"
              onClick={() => setStopMode("ORL")}
              className={`px-2.5 py-1 rounded-lg text-[10px] font-bold transition-all cursor-pointer ${
                stopMode === "ORL"
                  ? "bg-white text-slate-900 shadow-2xs"
                  : "text-slate-500 hover:text-slate-900"
              }`}
            >
              15m ORL
            </button>
          )}
          <button
            type="button"
            onClick={() => setStopMode("DAY_LOW")}
            className={`px-2.5 py-1 rounded-lg text-[10px] font-bold transition-all cursor-pointer ${
              stopMode === "DAY_LOW"
                ? "bg-white text-slate-900 shadow-2xs"
                : "text-slate-500 hover:text-slate-900"
            }`}
          >
            Day Low
          </button>
        </div>
      </div>

      {/* Sizing & Margin 4-Box Telemetry Grid */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-xs">
        {/* Box 1: Dynamic Quantity */}
        <div className="p-3 rounded-2xl bg-slate-50 border border-slate-200/80 space-y-1">
          <span className="text-[10px] text-slate-400 block uppercase">Dynamic Sizing</span>
          <div className="text-xl font-black text-slate-900">
            {dynamicQuantity} <span className="text-xs font-semibold text-slate-500">Shares</span>
          </div>
          <span className="text-[10px] text-slate-500 block truncate">
            Formula: ⌊₹{riskBudget} / ₹{stopDistance.toFixed(2)}⌋
          </span>
        </div>

        {/* Box 2: MIS 5x Required Margin */}
        <div className="p-3 rounded-2xl bg-slate-50 border border-slate-200/80 space-y-1">
          <span className="text-[10px] text-slate-400 block uppercase">MIS 5x Margin Required</span>
          <div className="text-xl font-black text-emerald-700">
            ₹{mis5xMarginRequired.toLocaleString("en-IN", { maximumFractionDigits: 0 })}
          </div>
          <span className="text-[10px] text-slate-500 block truncate">
            Total Value: ₹{totalCapitalRequired.toLocaleString("en-IN", { maximumFractionDigits: 0 })}
          </span>
        </div>

        {/* Box 3: Actual Risk at Stop */}
        <div className="p-3 rounded-2xl bg-slate-50 border border-slate-200/80 space-y-1">
          <span className="text-[10px] text-slate-400 block uppercase">Max Loss at Stop</span>
          <div className="text-xl font-black text-rose-700">
            ₹{actualRiskAtStop.toFixed(2)}
          </div>
          <span className="text-[10px] text-slate-500 block truncate">
            Stop Dist: ₹{stopDistance.toFixed(2)} ({stopDistancePct.toFixed(2)}%)
          </span>
        </div>

        {/* Box 4: Tranche 1 1.5R Profit Lock */}
        <div className="p-3 rounded-2xl bg-slate-50 border border-slate-200/80 space-y-1">
          <span className="text-[10px] text-slate-400 block uppercase">1.5R Tranche 1 Lock</span>
          <div className="text-xl font-black text-indigo-700">
            +₹{(tranche1Shares * 1.5 * oneR).toFixed(0)}
          </div>
          <span className="text-[10px] text-slate-500 block truncate">
            {tranche1Shares} shs banked @ ₹{tranche1Target.toFixed(2)}
          </span>
        </div>
      </div>

      {/* Engine Execution Order Specification */}
      <div className="p-4 rounded-2xl bg-slate-50/50 border border-slate-200/80 space-y-2.5 text-xs">
        <span className="text-[10px] font-black text-slate-700 uppercase tracking-wider block">
          Automated Order Dispatch Sequence
        </span>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-2.5 text-[11px]">
          {/* Leg 1: Entry Order */}
          <div className="p-2.5 rounded-xl bg-white border border-slate-200 space-y-1">
            <div className="flex items-center justify-between">
              <span className="font-bold text-slate-900">Leg 1: Entry Order</span>
              <span className="px-1.5 py-0.5 rounded text-[9px] font-bold bg-blue-50 text-blue-800">
                BUY LIMIT
              </span>
            </div>
            <div className="text-slate-600">
              Price: <strong className="text-slate-900">₹{entryPrice.toFixed(2)}</strong> &bull; Qty: <strong>{dynamicQuantity}</strong>
            </div>
            <span className="text-[10px] text-slate-400 block">
              Dispatched upon Stage 0 + Clock + Pattern Trigger
            </span>
          </div>

          {/* Leg 2: Resting Stop Loss Limit */}
          <div className="p-2.5 rounded-xl bg-white border border-slate-200 space-y-1">
            <div className="flex items-center justify-between">
              <span className="font-bold text-slate-900">Leg 2: Risk Stop Order</span>
              <span className="px-1.5 py-0.5 rounded text-[9px] font-bold bg-rose-50 text-rose-800">
                SELL SL-LIMIT
              </span>
            </div>
            <div className="text-slate-600">
              Trigger: <strong className="text-rose-700">₹{slTriggerPrice.toFixed(2)}</strong> &bull; Limit: <strong>₹{slLimitPrice.toFixed(2)}</strong>
            </div>
            <span className="text-[10px] text-slate-400 block">
              Pre-placed on NSE to eliminate slippage &amp; latency
            </span>
          </div>

          {/* Leg 3: Dynamic 4-Gear Trailing */}
          <div className="p-2.5 rounded-xl bg-white border border-slate-200 space-y-1">
            <div className="flex items-center justify-between">
              <span className="font-bold text-slate-900">Leg 3: Autonomous Ratchet</span>
              <span className="px-1.5 py-0.5 rounded text-[9px] font-bold bg-emerald-50 text-emerald-800">
                4-GEAR RATCHET
              </span>
            </div>
            <div className="text-slate-600">
              BE Target: <strong>₹{breakevenTarget.toFixed(2)}</strong> &bull; Runner: <strong>{runnerShares} shs</strong>
            </div>
            <span className="text-[10px] text-slate-400 block">
              BE locked @ +1.0R; runner trails completed 5m candle lows
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
