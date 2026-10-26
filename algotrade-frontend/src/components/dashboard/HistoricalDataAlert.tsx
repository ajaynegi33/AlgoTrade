"use client";

import React, { useState } from "react";
import { getKiteLoginURL } from "@/lib/api";
import { useTradingStore } from "@/store/useTradingStore";
import { toast } from "sonner";

interface HistoricalDataAlertProps {
  isActive: boolean;
  errorMessage?: string;
}

export function HistoricalDataAlert({ isActive, errorMessage }: HistoricalDataAlertProps) {
  const kiteStatus = useTradingStore((s) => s.kiteStatus);
  const [isConnecting, setIsConnecting] = useState(false);

  const isNoSessionMsg =
    !errorMessage || errorMessage.toLowerCase().includes("no active kite session");

  // If historical is active, or if the stale error was "No active Kite session" while Kite is already connected, hide banner
  if (isActive || (isNoSessionMsg && kiteStatus?.is_connected)) {
    return null;
  }

  const handleLoginKite = async () => {
    if (isConnecting) return;
    setIsConnecting(true);
    try {
      const loginUrl = await getKiteLoginURL();
      window.location.href = loginUrl;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to obtain Kite login URL";
      toast.error(msg);
      setIsConnecting(false);
    }
  };

  if (isNoSessionMsg) {
    return (
      <div className="bg-amber-50/90 border border-amber-200/90 rounded-2xl p-4 shadow-xs flex flex-col sm:flex-row sm:items-center justify-between gap-3.5 text-amber-900 transition-all">
        <div className="flex items-start gap-3.5">
          <div className="w-8 h-8 rounded-xl bg-amber-100 flex items-center justify-center shrink-0 text-amber-600 mt-0.5">
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
          </div>
          <div className="flex-1 text-xs">
            <div className="font-bold text-amber-950 flex flex-wrap items-center gap-2">
              <span>Zerodha Kite Daily Login Required</span>
              <span className="px-2 py-0.5 rounded-full bg-amber-200/80 text-[10px] font-mono font-bold uppercase tracking-wider text-amber-800">
                Session Disconnected
              </span>
            </div>
            <p className="text-amber-800/90 mt-1 leading-relaxed">
              Zerodha resets access tokens daily at 06:00 AM IST. Log in with Kite to activate live WebSocket tick streaming and 20-day 5m Historical Chart baselines.
            </p>
          </div>
        </div>

        <button
          type="button"
          onClick={handleLoginKite}
          disabled={isConnecting}
          className="px-3.5 py-2 rounded-xl bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 text-white text-xs font-bold font-mono shrink-0 cursor-pointer transition-colors self-start sm:self-center"
        >
          {isConnecting ? "Redirecting..." : "Login with Kite →"}
        </button>
      </div>
    );
  }

  return (
    <div className="bg-amber-50/90 border border-amber-200/90 rounded-2xl p-4 shadow-xs flex items-start gap-3.5 text-amber-900 transition-all">
      <div className="w-8 h-8 rounded-xl bg-amber-100 flex items-center justify-center shrink-0 text-amber-600 mt-0.5">
        <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
          <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
      </div>
      <div className="flex-1 text-xs">
        <div className="font-bold text-amber-950 flex items-center gap-2">
          <span>Kite Historical API Notice</span>
          <span className="px-2 py-0.5 rounded-full bg-amber-200/80 text-[10px] font-mono font-bold uppercase tracking-wider text-amber-800">
            Cached / Fallback Baselines Active
          </span>
        </div>
        <p className="text-amber-800/90 mt-1 leading-relaxed">
          {errorMessage}
        </p>
      </div>
    </div>
  );
}
