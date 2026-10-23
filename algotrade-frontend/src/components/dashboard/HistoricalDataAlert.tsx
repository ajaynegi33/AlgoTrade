"use client";

import React from "react";

interface HistoricalDataAlertProps {
  isActive: boolean;
  errorMessage?: string;
}

export function HistoricalDataAlert({ isActive, errorMessage }: HistoricalDataAlertProps) {
  if (isActive) return null;

  return (
    <div className="bg-amber-50/90 border border-amber-200/90 rounded-2xl p-4 shadow-xs flex items-start gap-3.5 text-amber-900 transition-all">
      <div className="w-8 h-8 rounded-xl bg-amber-100 flex items-center justify-center shrink-0 text-amber-600 mt-0.5">
        <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
          <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
      </div>
      <div className="flex-1 text-xs">
        <div className="font-bold text-amber-950 flex items-center gap-2">
          <span>Kite Historical API Subscription Warning</span>
          <span className="px-2 py-0.5 rounded-full bg-amber-200/80 text-[10px] font-mono font-bold uppercase tracking-wider text-amber-800">
            Fallback Mode Active
          </span>
        </div>
        <p className="text-amber-800/90 mt-1 leading-relaxed">
          {errorMessage || "Your ₹500/month Zerodha Kite Historical API subscription is inactive or returned 403. The engine is operating smoothly with algorithmic baseline estimations (1.5% ATR floor & rolling session volume)."}
        </p>
      </div>
    </div>
  );
}
