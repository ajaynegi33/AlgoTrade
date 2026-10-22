"use client";

import React, { useEffect } from "react";
import { useTradingStore } from "@/store/useTradingStore";
import { getKiteLoginURL } from "@/lib/api";
import { KiteProfileSkeleton } from "@/components/ui/Skeleton";
import { toast } from "sonner";
import UserAvatar from "./UserAvatar";

export default function KiteProfileView() {
  const {
    kiteProfile,
    isProfileLoading,
    fetchKiteProfile,
    fetchKiteStatus,
  } = useTradingStore();

  useEffect(() => {
    fetchKiteProfile();
    fetchKiteStatus();
  }, [fetchKiteProfile, fetchKiteStatus]);

  const handleConnectKite = async () => {
    try {
      const loginUrl = await getKiteLoginURL();
      window.location.href = loginUrl;
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Failed to retrieve Kite login URL");
    }
  };

  if (isProfileLoading && !kiteProfile) {
    return <KiteProfileSkeleton />;
  }

  if (!kiteProfile || !kiteProfile.is_connected) {
    return (
      <div className="bg-white border border-slate-200/90 rounded-3xl p-8 sm:p-12 text-center shadow-xs">
        <div className="w-16 h-16 rounded-2xl bg-amber-50 border border-amber-200 text-amber-600 flex items-center justify-center mx-auto mb-4">
          <svg className="w-8 h-8" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>

        <h2 className="text-xl font-bold text-slate-900">
          Zerodha Kite Session Disconnected
        </h2>
        <p className="text-xs sm:text-sm text-slate-500 max-w-md mx-auto mt-2 leading-relaxed">
          Connect your Zerodha trading account to retrieve live cash ledger balances, collateral margins, and verified trading permissions.
        </p>

        <button
          type="button"
          onClick={handleConnectKite}
          className="mt-6 inline-flex items-center gap-2.5 px-6 py-3 rounded-2xl bg-emerald-600 hover:bg-emerald-700 active:scale-[0.98] text-white font-bold text-sm shadow-sm transition-all cursor-pointer"
        >
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5">
            <path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
          Connect Zerodha Kite
        </button>
      </div>
    );
  }

  const formatINR = (val?: number) => {
    if (val === undefined || isNaN(val)) return "₹ 0.00";
    return new Intl.NumberFormat("en-IN", {
      style: "currency",
      currency: "INR",
      minimumFractionDigits: 2,
    }).format(val);
  };

  return (
    <div className="space-y-6">
      {/* =========================================================================
          1. Zerodha Identity Profile Card
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-6 sm:p-7 shadow-xs">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-5">
          <div className="flex items-center gap-4 sm:gap-5">
            {/* Avatar / Peep / Initial */}
            <UserAvatar
              avatarUrl={kiteProfile.avatar_url}
              name={kiteProfile.user_name}
              size="lg"
              rounded="2xl"
              className=""
            />

            <div>
              <div className="flex flex-wrap items-center gap-2.5">
                <h1 className="text-xl sm:text-2xl font-black text-slate-900 tracking-tight">
                  {kiteProfile.user_name || "Zerodha Trader"}
                </h1>
                <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-emerald-50 text-emerald-800 border border-emerald-300/60 uppercase">
                  {kiteProfile.broker || "ZERODHA"}
                </span>
                <span className="text-[10px] font-mono font-bold px-2 py-0.5 rounded-full bg-slate-100 text-slate-700">
                  {kiteProfile.user_type || "INDIVIDUAL"}
                </span>
              </div>

              <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-xs text-slate-500 mt-1.5 font-mono">
                <div>
                  Client ID: <strong className="text-slate-900">{kiteProfile.user_id}</strong>
                </div>
                {kiteProfile.email && (
                  <div>
                    Email: <span className="text-slate-700">{kiteProfile.email}</span>
                  </div>
                )}
                <div>
                  Status: <strong className="text-emerald-700">ACTIVE TRADER</strong>
                </div>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-semibold bg-emerald-50 text-emerald-800 border border-emerald-300/60">
              <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
              Session Valid (06:00 AM Reset)
            </span>
            <button
              type="button"
              onClick={handleConnectKite}
              className="text-xs font-semibold text-slate-600 hover:text-slate-900 px-3 py-1.5 rounded-xl border border-slate-200 hover:bg-slate-50 transition-all cursor-pointer"
            >
              Re-Auth
            </button>
          </div>
        </div>
      </div>

      {/* =========================================================================
          2. Real-Time Account Funds & Margins
          ========================================================================= */}
      <div>
        <h2 className="text-xs font-bold text-slate-500 uppercase tracking-wider mb-3">
          Equity Segment Funds &amp; Ledger Margins (INR)
        </h2>

        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {/* Net Margin */}
          <div className="bg-white border border-slate-200/90 rounded-2xl p-5 shadow-xs">
            <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400 block">
              Net Available Margin
            </span>
            <div className="text-2xl font-black text-slate-900 mt-1 font-mono">
              {formatINR(kiteProfile.equity_margin?.net)}
            </div>
            <div className="text-[11px] text-emerald-700 font-semibold mt-1">
              Live buying power
            </div>
          </div>

          {/* Cash Balance */}
          <div className="bg-white border border-slate-200/90 rounded-2xl p-5 shadow-xs">
            <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400 block">
              Available Cash
            </span>
            <div className="text-2xl font-black text-emerald-700 mt-1 font-mono">
              {formatINR(kiteProfile.equity_margin?.available_cash)}
            </div>
            <div className="text-[11px] text-slate-500 font-medium mt-1">
              Ready for cash deployment
            </div>
          </div>

          {/* Collateral Margin */}
          <div className="bg-white border border-slate-200/90 rounded-2xl p-5 shadow-xs">
            <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400 block">
              Collateral (Pledged)
            </span>
            <div className="text-2xl font-black text-slate-900 mt-1 font-mono">
              {formatINR(kiteProfile.equity_margin?.collateral)}
            </div>
            <div className="text-[11px] text-slate-500 font-medium mt-1">
              Securities pledge margin
            </div>
          </div>

          {/* Opening Balance */}
          <div className="bg-white border border-slate-200/90 rounded-2xl p-5 shadow-xs">
            <span className="text-[11px] font-mono font-semibold uppercase tracking-wider text-slate-400 block">
              Opening Day Balance
            </span>
            <div className="text-2xl font-black text-slate-700 mt-1 font-mono">
              {formatINR(kiteProfile.equity_margin?.opening_balance)}
            </div>
            <div className="text-[11px] text-slate-500 font-medium mt-1">
              Settled at 09:15 AM
            </div>
          </div>
        </div>
      </div>

      {/* =========================================================================
          3. Trading Permissions & Allowed Exchanges
          ========================================================================= */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Allowed Products */}
        <div className="bg-white border border-slate-200/90 rounded-2xl p-5 shadow-xs">
          <h3 className="text-xs font-bold text-slate-500 uppercase tracking-wider mb-3">
            Permitted Trading Products
          </h3>
          <div className="flex flex-wrap gap-2">
            {(kiteProfile.products || ["CNC", "MIS", "NRML"]).map((p) => (
              <span
                key={p}
                className="inline-flex items-center gap-1.5 px-3 py-1 rounded-xl text-xs font-mono font-bold bg-slate-50 border border-slate-200 text-slate-800"
              >
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
                {p} {p === "MIS" && "(5x Intraday)"}
              </span>
            ))}
          </div>
        </div>

        {/* Allowed Exchanges */}
        <div className="bg-white border border-slate-200/90 rounded-2xl p-5 shadow-xs">
          <h3 className="text-xs font-bold text-slate-500 uppercase tracking-wider mb-3">
            Permitted Exchanges
          </h3>
          <div className="flex flex-wrap gap-2">
            {(kiteProfile.exchanges || ["NSE", "NFO", "BSE"]).map((ex) => (
              <span
                key={ex}
                className="inline-flex items-center gap-1.5 px-3 py-1 rounded-xl text-xs font-mono font-bold bg-slate-50 border border-slate-200 text-slate-800"
              >
                <span className="w-1.5 h-1.5 rounded-full bg-blue-500" />
                {ex}
              </span>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
