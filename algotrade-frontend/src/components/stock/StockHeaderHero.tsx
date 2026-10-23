"use client";

import React, { useEffect, useState, useRef } from "react";
import { StockDetailData } from "@/lib/types";

interface StockHeaderHeroProps {
  stock: StockDetailData;
}

export function StockHeaderHero({ stock }: StockHeaderHeroProps) {
  const [flash, setFlash] = useState<"up" | "down" | null>(null);
  const prevLtpRef = useRef(stock.ltp);

  useEffect(() => {
    if (prevLtpRef.current > 0 && stock.ltp !== prevLtpRef.current) {
      if (stock.ltp > prevLtpRef.current) {
        setFlash("up");
      } else {
        setFlash("down");
      }
      const timer = setTimeout(() => setFlash(null), 800);
      prevLtpRef.current = stock.ltp;
      return () => clearTimeout(timer);
    }
    prevLtpRef.current = stock.ltp;
  }, [stock.ltp]);

  const isUp = stock.change_percent >= 0;

  // Day range percentage
  const dayRangeSpread = stock.high - stock.low;
  const dayRangePos = dayRangeSpread > 0 ? ((stock.ltp - stock.low) / dayRangeSpread) * 100 : 50;

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs">
      {/* Top breadcrumb & tags */}
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <div className="flex items-center gap-2 text-xs font-mono">
          <span className="font-bold text-slate-800">{stock.symbol}</span>
          <span className="text-slate-300">/</span>
          <span className="px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 font-semibold text-[10px] border border-slate-200">
            {stock.sector}
          </span>
          <span className="px-2 py-0.5 rounded-full bg-slate-50 text-slate-500 text-[10px] border border-slate-200/80">
            NSE Cash Equity
          </span>
        </div>

        <div className="flex items-center gap-2 text-[11px] font-mono text-slate-500">
          <span>Lot Size: <strong className="text-slate-800">{stock.lot_size}</strong></span>
          <span>•</span>
          <span>Tick: <strong className="text-slate-800">₹{stock.tick_size}</strong></span>
          <span>•</span>
          <span className="text-emerald-700 font-semibold bg-emerald-50 px-2 py-0.5 rounded border border-emerald-200/60">
            F&O Active
          </span>
        </div>
      </div>

      {/* Main Quote Telemetry */}
      <div className="flex flex-col lg:flex-row lg:items-end justify-between gap-6">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl sm:text-3xl font-black font-mono text-slate-900 tracking-tight">
              {stock.symbol}
            </h1>
            <span
              className={`text-2xl sm:text-3xl font-black font-mono transition-all duration-300 px-2.5 py-0.5 rounded-xl ${
                flash === "up"
                  ? "bg-emerald-100 text-emerald-800 ring-2 ring-emerald-500/30 scale-105"
                  : flash === "down"
                  ? "bg-rose-100 text-rose-800 ring-2 ring-rose-500/30 scale-105"
                  : "text-slate-900"
              }`}
            >
              ₹{stock.ltp.toFixed(2)}
            </span>
          </div>

          <div className="flex items-center gap-2.5 mt-2 font-mono text-xs font-bold">
            <span
              className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-lg ${
                isUp
                  ? "bg-emerald-50 text-emerald-700 border border-emerald-200/80"
                  : "bg-rose-50 text-rose-700 border border-rose-200/80"
              }`}
            >
              <span>{isUp ? "▲" : "▼"}</span>
              <span>{isUp ? "+" : ""}{stock.change.toFixed(2)}</span>
              <span>({isUp ? "+" : ""}{stock.change_percent.toFixed(2)}%)</span>
            </span>

            <span className="text-slate-400 font-normal">|</span>

            <span className="text-slate-600 font-semibold">
              VWAP: <strong className="text-slate-900">₹{stock.vwap.toFixed(2)}</strong>
            </span>
            <span
              className={`text-[11px] ${
                stock.vwap_distance >= 0 ? "text-emerald-700 font-semibold" : "text-rose-700 font-semibold"
              }`}
            >
              ({stock.vwap_distance >= 0 ? "+" : ""}{stock.vwap_distance_pct.toFixed(2)}%)
            </span>
          </div>
        </div>

        {/* OHLC Mini Matrix */}
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 font-mono text-xs">
          <div className="bg-slate-50 border border-slate-200/70 rounded-xl p-2.5 text-center">
            <span className="text-[10px] text-slate-400 block uppercase font-semibold">Prev Close</span>
            <span className="font-bold text-slate-800">₹{stock.prev_close.toFixed(2)}</span>
          </div>
          <div className="bg-slate-50 border border-slate-200/70 rounded-xl p-2.5 text-center">
            <span className="text-[10px] text-slate-400 block uppercase font-semibold">Open</span>
            <span className="font-bold text-slate-800">₹{stock.open.toFixed(2)}</span>
          </div>
          <div className="bg-slate-50 border border-slate-200/70 rounded-xl p-2.5 text-center">
            <span className="text-[10px] text-slate-400 block uppercase font-semibold">Day High</span>
            <span className="font-bold text-emerald-700">₹{stock.high.toFixed(2)}</span>
          </div>
          <div className="bg-slate-50 border border-slate-200/70 rounded-xl p-2.5 text-center">
            <span className="text-[10px] text-slate-400 block uppercase font-semibold">Day Low</span>
            <span className="font-bold text-rose-700">₹{stock.low.toFixed(2)}</span>
          </div>
        </div>
      </div>

      {/* Visual Day Range Progress Bar */}
      <div className="mt-5 pt-4 border-t border-slate-100 font-mono">
        <div className="flex items-center justify-between text-[11px] text-slate-500 mb-1.5">
          <span className="flex items-center gap-1 font-semibold">
            <span className="text-slate-400">Low:</span> ₹{stock.low.toFixed(2)}
          </span>
          <span className="text-slate-400 text-[10px]">
            Day Range ({dayRangePos.toFixed(0)}% of range)
          </span>
          <span className="flex items-center gap-1 font-semibold">
            <span className="text-slate-400">High:</span> ₹{stock.high.toFixed(2)}
          </span>
        </div>
        <div className="relative w-full h-2 bg-slate-100 rounded-full overflow-hidden">
          <div
            className="absolute top-0 bottom-0 left-0 bg-gradient-to-r from-rose-400 via-amber-400 to-emerald-500 rounded-full"
            style={{ width: `${Math.min(100, Math.max(0, dayRangePos))}%` }}
          />
        </div>
      </div>
    </div>
  );
}
