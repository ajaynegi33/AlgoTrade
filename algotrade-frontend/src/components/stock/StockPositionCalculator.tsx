"use client";

import React, { useState, useMemo, useEffect } from "react";
import { StockDetailData } from "@/lib/types";
import { useTradingStore } from "@/store/useTradingStore";
import { toast } from "sonner";

interface StockPositionCalculatorProps {
  stock: StockDetailData;
}

export function StockPositionCalculator({ stock }: StockPositionCalculatorProps) {
  const { executionState, fetchExecutionState } = useTradingStore();
  const [maxRiskBudget, setMaxRiskBudget] = useState<number>(
    executionState?.capital?.current_risk_budget
      ? Math.max(10, Math.round(executionState.capital.current_risk_budget))
      : 750
  );
  const [hasUserEditedRisk, setHasUserEditedRisk] = useState(false);
  const [stopMode, setStopMode] = useState<"ORL" | "VWAP" | "DAY_LOW" | "CUSTOM">("VWAP");
  const [customStop, setCustomStop] = useState<number>(stock.stop_proxy || stock.ltp * 0.99);

  useEffect(() => {
    if (!executionState) {
      fetchExecutionState();
    }
  }, [executionState, fetchExecutionState]);

  useEffect(() => {
    if (!hasUserEditedRisk && executionState?.capital?.current_risk_budget) {
      setMaxRiskBudget(Math.max(10, Math.round(executionState.capital.current_risk_budget)));
    }
  }, [executionState?.capital?.current_risk_budget, hasUserEditedRisk]);

  // Suggested stop loss based on selected mode
  const effectiveStop = useMemo(() => {
    if (stopMode === "ORL" && stock.has_or15m && stock.orl > 0) {
      return stock.orl;
    }
    if (stopMode === "VWAP" && stock.vwap > 0) {
      const buffer = stock.atr14 > 0 ? stock.atr14 * 0.25 : stock.ltp * 0.003;
      return Math.max(0.05, stock.vwap - buffer);
    }
    if (stopMode === "DAY_LOW" && stock.low > 0) {
      return stock.low - (stock.tick_size || 0.05);
    }
    return customStop > 0 ? customStop : stock.ltp * 0.99;
  }, [stopMode, stock, customStop]);

  // Trade Calculations
  const entryPrice = stock.ltp;
  const riskPerShare = Math.max(0.05, entryPrice - effectiveStop);

  const calculatedShares = useMemo(() => {
    if (riskPerShare <= 0) return 0;
    return Math.floor(maxRiskBudget / riskPerShare);
  }, [maxRiskBudget, riskPerShare]);

  const lotSize = stock.lot_size || 1;
  const finalShares = Math.max(1, calculatedShares);
  const actualRisk = finalShares * riskPerShare;

  const target1_5R = entryPrice + 1.5 * riskPerShare;
  const target2_5R = entryPrice + 2.5 * riskPerShare;
  const potentialProfit2R = finalShares * (2.0 * riskPerShare);

  const handleCopyBasket = () => {
    const basket = [
      {
        variety: "regular",
        tradingsymbol: stock.symbol,
        exchange: "NSE",
        transaction_type: "BUY",
        order_type: "LIMIT",
        quantity: finalShares,
        price: Number(entryPrice.toFixed(2)),
        product: "MIS",
        validity: "DAY",
      },
      {
        variety: "regular",
        tradingsymbol: stock.symbol,
        exchange: "NSE",
        transaction_type: "SELL",
        order_type: "SL-M",
        quantity: finalShares,
        trigger_price: Number(effectiveStop.toFixed(2)),
        product: "MIS",
        validity: "DAY",
      },
    ];

    navigator.clipboard.writeText(JSON.stringify(basket, null, 2));
    toast.success("Zerodha Kite Basket JSON copied to clipboard!");
  };

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs font-mono">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-5 border-b border-slate-100 pb-4">
        <div>
          <h2 className="text-xs font-bold text-slate-900 uppercase tracking-wider flex items-center gap-2">
            Intraday Position Sizing &amp; Risk Calculator
            <span className="text-[10px] text-slate-400 font-normal">
              (Friction &amp; Lot-Size Calibrated)
            </span>
          </h2>
          <p className="text-[11px] text-slate-500 mt-0.5">
            Strict risk-budgeting: Never risk more than your predefined rupees on a single trade setup.
          </p>
        </div>

        <button
          onClick={handleCopyBasket}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-slate-900 hover:bg-slate-800 text-white text-xs font-semibold shadow-xs cursor-pointer active:scale-95 transition-all self-start sm:self-auto"
        >
          <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" strokeLinejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
          </svg>
          <span>Copy Kite Order Basket</span>
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {/* Column 1: Trade Inputs */}
        <div className="space-y-4">
          <div>
            <label className="text-[11px] text-slate-500 uppercase font-semibold block mb-1">
              Max Capital at Risk (₹)
            </label>
            <div className="relative">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 font-bold">₹</span>
              <input
                type="number"
                value={maxRiskBudget}
                onChange={(e) => {
                  setHasUserEditedRisk(true);
                  setMaxRiskBudget(Math.max(10, Number(e.target.value)));
                }}
                step="50"
                className="w-full pl-7 pr-3 py-2 bg-slate-50 border border-slate-200 rounded-xl text-xs font-bold text-slate-900 focus:bg-white focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500"
              />
            </div>
          </div>

          <div>
            <label className="text-[11px] text-slate-500 uppercase font-semibold block mb-1">
              Stop Loss Anchor Mode
            </label>
            <div className="grid grid-cols-2 gap-1.5 text-xs">
              <button
                type="button"
                onClick={() => setStopMode("VWAP")}
                className={`p-2 rounded-xl border text-left cursor-pointer transition-all ${
                  stopMode === "VWAP"
                    ? "bg-slate-900 text-white border-slate-900 shadow-2xs font-bold"
                    : "bg-slate-50 text-slate-700 border-slate-200 hover:bg-slate-100"
                }`}
              >
                <span className="block text-[10px] opacity-70">Institutional</span>
                <span>VWAP Buffer</span>
              </button>

              <button
                type="button"
                onClick={() => setStopMode("ORL")}
                className={`p-2 rounded-xl border text-left cursor-pointer transition-all ${
                  stopMode === "ORL"
                    ? "bg-slate-900 text-white border-slate-900 shadow-2xs font-bold"
                    : "bg-slate-50 text-slate-700 border-slate-200 hover:bg-slate-100"
                }`}
              >
                <span className="block text-[10px] opacity-70">15m Drive</span>
                <span>Opening ORL</span>
              </button>

              <button
                type="button"
                onClick={() => setStopMode("DAY_LOW")}
                className={`p-2 rounded-xl border text-left cursor-pointer transition-all ${
                  stopMode === "DAY_LOW"
                    ? "bg-slate-900 text-white border-slate-900 shadow-2xs font-bold"
                    : "bg-slate-50 text-slate-700 border-slate-200 hover:bg-slate-100"
                }`}
              >
                <span className="block text-[10px] opacity-70">Extreme</span>
                <span>Day Low</span>
              </button>

              <button
                type="button"
                onClick={() => setStopMode("CUSTOM")}
                className={`p-2 rounded-xl border text-left cursor-pointer transition-all ${
                  stopMode === "CUSTOM"
                    ? "bg-slate-900 text-white border-slate-900 shadow-2xs font-bold"
                    : "bg-slate-50 text-slate-700 border-slate-200 hover:bg-slate-100"
                }`}
              >
                <span className="block text-[10px] opacity-70">Manual</span>
                <span>Custom Price</span>
              </button>
            </div>
          </div>

          {stopMode === "CUSTOM" && (
            <div>
              <label className="text-[11px] text-slate-500 uppercase font-semibold block mb-1">
                Custom Stop Loss Price (₹)
              </label>
              <input
                type="number"
                value={customStop}
                onChange={(e) => setCustomStop(Number(e.target.value))}
                step="0.05"
                className="w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-xl text-xs font-bold text-slate-900 focus:bg-white focus:outline-none focus:ring-2 focus:ring-emerald-500/20"
              />
            </div>
          )}
        </div>

        {/* Column 2: Computed Position Size */}
        <div className="bg-slate-50 border border-slate-200/80 rounded-2xl p-4 flex flex-col justify-between">
          <div>
            <span className="text-[11px] text-slate-400 uppercase font-semibold block mb-3">
              Computed Position Allocation
            </span>

            <div className="space-y-3 text-xs">
              <div className="flex items-center justify-between pb-2 border-b border-slate-200/60">
                <span className="text-slate-600">Entry Price (LTP)</span>
                <span className="font-bold text-slate-900">₹{entryPrice.toFixed(2)}</span>
              </div>

              <div className="flex items-center justify-between pb-2 border-b border-slate-200/60">
                <span className="text-slate-600">Calculated Stop Loss</span>
                <span className="font-bold text-rose-700">₹{effectiveStop.toFixed(2)}</span>
              </div>

              <div className="flex items-center justify-between pb-2 border-b border-slate-200/60">
                <span className="text-slate-600">Risk Per Share</span>
                <span className="font-bold text-slate-900">₹{riskPerShare.toFixed(2)}</span>
              </div>

              <div className="flex items-center justify-between pb-2 border-b border-slate-200/60">
                <span className="text-slate-600">Cash Lot Granularity</span>
                <span className="font-bold text-slate-900">1 share (F&amp;O Ref: {lotSize})</span>
              </div>
            </div>
          </div>

          <div className="pt-3 border-t border-slate-200 mt-2">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold text-slate-900">Recommended Order:</span>
              <span className="text-sm font-black text-emerald-700 bg-emerald-50 px-2.5 py-1 rounded-lg border border-emerald-200">
                {finalShares.toLocaleString("en-IN")} Shares
              </span>
            </div>
            <span className="text-[10px] text-slate-400 block text-right mt-1">
              Actual Max Risk: ₹{actualRisk.toFixed(0)}
            </span>
          </div>
        </div>

        {/* Column 3: Projections & Risk/Reward */}
        <div className="bg-slate-50 border border-slate-200/80 rounded-2xl p-4 flex flex-col justify-between">
          <div>
            <span className="text-[11px] text-slate-400 uppercase font-semibold block mb-3">
              Target Projections &amp; Expectancy
            </span>

            <div className="space-y-3 text-xs">
              <div className="p-2.5 rounded-xl bg-white border border-slate-200/80 flex items-center justify-between">
                <div>
                  <span className="text-[10px] text-emerald-700 uppercase font-bold block">Target 1 (1.5R)</span>
                  <span className="font-bold text-slate-900 text-sm">₹{target1_5R.toFixed(2)}</span>
                </div>
                <span className="text-xs font-semibold text-emerald-700">
                  +₹{(finalShares * 1.5 * riskPerShare).toFixed(0)}
                </span>
              </div>

              <div className="p-2.5 rounded-xl bg-white border border-slate-200/80 flex items-center justify-between">
                <div>
                  <span className="text-[10px] text-indigo-700 uppercase font-bold block">Target 2 (2.5R Runner)</span>
                  <span className="font-bold text-slate-900 text-sm">₹{target2_5R.toFixed(2)}</span>
                </div>
                <span className="text-xs font-semibold text-indigo-700">
                  +₹{(finalShares * 2.5 * riskPerShare).toFixed(0)}
                </span>
              </div>
            </div>
          </div>

          <div className="pt-3 border-t border-slate-200 mt-2 text-xs">
            <div className="flex items-center justify-between">
              <span className="text-slate-500">2.0R Potential Profit:</span>
              <span className="font-bold text-emerald-700">₹{potentialProfit2R.toFixed(0)}</span>
            </div>
            <div className="flex items-center justify-between mt-1 text-[11px]">
              <span className="text-slate-400">R:R Ratio</span>
              <span className="font-bold text-slate-800">1 : 2.0 (EV +{stock.ev.toFixed(2)}R)</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
