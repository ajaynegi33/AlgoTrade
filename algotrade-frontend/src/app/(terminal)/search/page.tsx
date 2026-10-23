"use client";

import React, { useState, useMemo } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useTradingStore } from "@/store/useTradingStore";
import { useEngineWebSocket } from "@/hooks/useEngineWebSocket";
import { StockRankData } from "@/lib/types";

type CategoryFilter = "ALL" | "GAINERS" | "LOSERS" | "VOLUME" | "SWEET_SPOT" | "HIGH_RS";

export default function SearchHubPage() {
  const router = useRouter();
  const { universe } = useTradingStore();
  const { snapshot, isConnected } = useEngineWebSocket();

  const [searchQuery, setSearchQuery] = useState("");
  const [activeCategory, setActiveCategory] = useState<CategoryFilter>("ALL");

  // Get live stocks array from snapshot or universe fallback
  const stocks: StockRankData[] = useMemo(() => {
    if (snapshot?.all_stocks && snapshot.all_stocks.length > 0) {
      return snapshot.all_stocks;
    }
    if (universe?.stocks && universe.stocks.length > 0) {
      return universe.stocks.map((s, idx) => ({
        rank: idx + 1,
        token: s.instrument_token,
        symbol: s.tradingsymbol,
        sector: "NSE Cash",
        sector_id: 0,
        ltp: s.last_price,
        change_percent: 0,
        vwap: s.last_price,
        bullish_score: 50,
        bearish_score: 50,
        rvol_session: 1.0,
        rvol_pace_5m: 1.0,
        composite_rs: 50,
        buyer_ratio: 0.5,
        aggressor_buy_ratio: 0.5,
        ev: 0.25,
        p_win: 0.55,
        stop_proxy: s.last_price * 0.99,
        target_proxy: s.last_price * 1.02,
        reasons: 0,
        pullback_count: 0,
      }));
    }
    return [];
  }, [snapshot, universe]);

  // Derived Outlier Lists
  const topGainers = useMemo(() => {
    return [...stocks]
      .filter((s) => s.change_percent > 0)
      .sort((a, b) => b.change_percent - a.change_percent)
      .slice(0, 5);
  }, [stocks]);

  const topLosers = useMemo(() => {
    return [...stocks]
      .filter((s) => s.change_percent < 0)
      .sort((a, b) => a.change_percent - b.change_percent)
      .slice(0, 5);
  }, [stocks]);

  const volumeOutliers = useMemo(() => {
    return [...stocks]
      .sort((a, b) => b.rvol_session - a.rvol_session)
      .slice(0, 5);
  }, [stocks]);

  const highEvSetups = useMemo(() => {
    return [...stocks]
      .filter((s) => s.ev >= 0.25)
      .sort((a, b) => b.ev - a.ev)
      .slice(0, 5);
  }, [stocks]);

  // Filtered stocks for the table
  const filteredStocks = useMemo(() => {
    let list = stocks;

    // Apply category filter
    if (activeCategory === "GAINERS") {
      list = list.filter((s) => s.change_percent > 0).sort((a, b) => b.change_percent - a.change_percent);
    } else if (activeCategory === "LOSERS") {
      list = list.filter((s) => s.change_percent < 0).sort((a, b) => a.change_percent - b.change_percent);
    } else if (activeCategory === "VOLUME") {
      list = list.filter((s) => s.rvol_session >= 1.5).sort((a, b) => b.rvol_session - a.rvol_session);
    } else if (activeCategory === "SWEET_SPOT") {
      list = list.filter((s) => (s.reasons & 1) !== 0 || s.ev >= 0.30).sort((a, b) => b.ev - a.ev);
    } else if (activeCategory === "HIGH_RS") {
      list = list.filter((s) => s.composite_rs >= 60).sort((a, b) => b.composite_rs - a.composite_rs);
    }

    // Apply text search
    const q = searchQuery.trim().toLowerCase();
    if (q) {
      list = list.filter(
        (s) => s.symbol.toLowerCase().includes(q) || s.sector.toLowerCase().includes(q)
      );
    }

    return list;
  }, [stocks, activeCategory, searchQuery]);

  return (
    <div className="space-y-6 sm:space-y-8">
      {/* 1. Header Banner & Instant Search Input */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <span className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider">
                F&O Equities Search Hub
              </span>
              <span className="text-[11px] font-mono px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 border border-slate-200/80">
                213 NSE Stocks
              </span>
              {isConnected && (
                <span className="flex items-center gap-1.5 text-[10px] font-mono text-emerald-700 font-semibold bg-emerald-50 px-2 py-0.5 rounded-full border border-emerald-200/60">
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
                  Live Tape
                </span>
              )}
            </div>
            <p className="text-xs font-mono text-slate-500 mt-1 max-w-xl">
              Real-time institutional outlier detection, gainers, losers, volume shockers, and deep-dive stock terminals.
            </p>
          </div>

          {/* Quick Search Field */}
          <div className="relative w-full md:w-80">
            <svg
              className="w-4 h-4 text-slate-400 absolute left-3.5 top-1/2 -translate-y-1/2 pointer-events-none"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Filter by symbol or sector..."
              className="w-full pl-9 pr-4 py-2 rounded-2xl border border-slate-200 bg-slate-50/80 focus:bg-white text-xs font-mono text-slate-900 placeholder:text-slate-400 focus:outline-none focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 transition-all"
            />
          </div>
        </div>

        {/* Category Pills */}
        <div className="flex flex-wrap items-center gap-2 mt-4 pt-4 border-t border-slate-100">
          {(
            [
              { id: "ALL", label: "All Equities (213)" },
              { id: "GAINERS", label: "Top Gainers 🚀" },
              { id: "LOSERS", label: "Top Losers 🔻" },
              { id: "VOLUME", label: "Volume Shockers 🔥" },
              { id: "SWEET_SPOT", label: "VWAP Sweet Spots 🎯" },
              { id: "HIGH_RS", label: "High Relative Strength ⚡" },
            ] as const
          ).map((tab) => {
            const isActive = activeCategory === tab.id;
            return (
              <button
                key={tab.id}
                onClick={() => setActiveCategory(tab.id)}
                className={`px-3 py-1.5 rounded-xl text-xs font-mono font-semibold transition-all cursor-pointer active:scale-95 ${
                  isActive
                    ? "bg-slate-900 text-white shadow-xs"
                    : "bg-slate-50 text-slate-600 hover:text-slate-900 hover:bg-slate-100 border border-slate-200/80"
                }`}
              >
                {tab.label}
              </button>
            );
          })}
        </div>
      </div>

      {/* 2. Top 4 Outlier Hero Grids */}
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
        {/* Card 1: Top Gainers */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between mb-3">
              <span className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider flex items-center gap-1.5">
                <span className="w-2 h-2 rounded-full bg-emerald-500" />
                Top Gainers
              </span>
              <span className="text-[10px] font-mono text-slate-400">Intraday High</span>
            </div>

            <div className="space-y-2">
              {topGainers.map((st) => (
                <Link
                  key={st.symbol}
                  href={`/search/${st.symbol.toUpperCase()}`}
                  className="flex items-center justify-between p-2 rounded-xl hover:bg-slate-50 border border-transparent hover:border-slate-200/80 transition-all group"
                >
                  <div className="min-w-0">
                    <span className="text-xs font-mono font-black text-slate-900 group-hover:text-emerald-700 transition-colors">
                      {st.symbol}
                    </span>
                    <span className="block text-[10px] font-mono text-slate-400 truncate">
                      {st.sector}
                    </span>
                  </div>
                  <div className="text-right">
                    <span className="text-xs font-mono font-bold text-slate-900 block">
                      ₹{st.ltp.toFixed(2)}
                    </span>
                    <span className="text-[11px] font-mono font-bold text-emerald-700">
                      +{st.change_percent.toFixed(2)}%
                    </span>
                  </div>
                </Link>
              ))}
            </div>
          </div>
        </div>

        {/* Card 2: Top Losers */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between mb-3">
              <span className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider flex items-center gap-1.5">
                <span className="w-2 h-2 rounded-full bg-rose-500" />
                Top Losers
              </span>
              <span className="text-[10px] font-mono text-slate-400">Intraday Low</span>
            </div>

            <div className="space-y-2">
              {topLosers.map((st) => (
                <Link
                  key={st.symbol}
                  href={`/search/${st.symbol.toUpperCase()}`}
                  className="flex items-center justify-between p-2 rounded-xl hover:bg-slate-50 border border-transparent hover:border-slate-200/80 transition-all group"
                >
                  <div className="min-w-0">
                    <span className="text-xs font-mono font-black text-slate-900 group-hover:text-rose-700 transition-colors">
                      {st.symbol}
                    </span>
                    <span className="block text-[10px] font-mono text-slate-400 truncate">
                      {st.sector}
                    </span>
                  </div>
                  <div className="text-right">
                    <span className="text-xs font-mono font-bold text-slate-900 block">
                      ₹{st.ltp.toFixed(2)}
                    </span>
                    <span className="text-[11px] font-mono font-bold text-rose-700">
                      {st.change_percent.toFixed(2)}%
                    </span>
                  </div>
                </Link>
              ))}
            </div>
          </div>
        </div>

        {/* Card 3: Institutional Volume Shockers */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between mb-3">
              <span className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider flex items-center gap-1.5">
                <span className="w-2 h-2 rounded-full bg-amber-500" />
                Volume Shockers
              </span>
              <span className="text-[10px] font-mono text-slate-400">RVOL Pace</span>
            </div>

            <div className="space-y-2">
              {volumeOutliers.map((st) => (
                <Link
                  key={st.symbol}
                  href={`/search/${st.symbol.toUpperCase()}`}
                  className="flex items-center justify-between p-2 rounded-xl hover:bg-slate-50 border border-transparent hover:border-slate-200/80 transition-all group"
                >
                  <div className="min-w-0">
                    <span className="text-xs font-mono font-black text-slate-900 group-hover:text-amber-700 transition-colors">
                      {st.symbol}
                    </span>
                    <span className="block text-[10px] font-mono text-slate-400 truncate">
                      ₹{st.ltp.toFixed(2)}
                    </span>
                  </div>
                  <div className="text-right">
                    <span className="text-[11px] font-mono font-black text-amber-700 bg-amber-50 px-2 py-0.5 rounded border border-amber-200/80 block">
                      {st.rvol_session.toFixed(2)}x RVOL
                    </span>
                    <span className={`text-[10px] font-mono font-semibold ${st.change_percent >= 0 ? "text-emerald-700" : "text-rose-700"}`}>
                      {st.change_percent >= 0 ? "+" : ""}{st.change_percent.toFixed(2)}%
                    </span>
                  </div>
                </Link>
              ))}
            </div>
          </div>
        </div>

        {/* Card 4: High Expected Value (EV) */}
        <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between mb-3">
              <span className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider flex items-center gap-1.5">
                <span className="w-2 h-2 rounded-full bg-indigo-500" />
                High Expected Value
              </span>
              <span className="text-[10px] font-mono text-slate-400">EV ≥ +0.25R</span>
            </div>

            <div className="space-y-2">
              {highEvSetups.map((st) => (
                <Link
                  key={st.symbol}
                  href={`/search/${st.symbol.toUpperCase()}`}
                  className="flex items-center justify-between p-2 rounded-xl hover:bg-slate-50 border border-transparent hover:border-slate-200/80 transition-all group"
                >
                  <div className="min-w-0">
                    <span className="text-xs font-mono font-black text-slate-900 group-hover:text-indigo-700 transition-colors">
                      {st.symbol}
                    </span>
                    <span className="block text-[10px] font-mono text-slate-400 truncate">
                      P(Win): {(st.p_win * 100).toFixed(0)}%
                    </span>
                  </div>
                  <div className="text-right">
                    <span className="text-[11px] font-mono font-black text-indigo-700 bg-indigo-50 px-2 py-0.5 rounded border border-indigo-200/80 block">
                      +{st.ev.toFixed(2)}R EV
                    </span>
                    <span className="text-[10px] font-mono text-slate-500">
                      ₹{st.ltp.toFixed(2)}
                    </span>
                  </div>
                </Link>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* 3. Searchable Full Stocks Grid */}
      <div className="bg-white border border-slate-200/90 rounded-3xl overflow-hidden shadow-xs">
        <div className="p-4 sm:p-5 border-b border-slate-100 flex items-center justify-between">
          <div>
            <h2 className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider">
              {activeCategory === "ALL" ? "All F&O Equities" : `${activeCategory} Outliers`}
            </h2>
            <span className="text-[11px] font-mono text-slate-400">
              Showing {filteredStocks.length} matching equities. Click any row to open institutional stock terminal.
            </span>
          </div>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse text-xs font-mono">
            <thead>
              <tr className="border-b border-slate-200 bg-slate-50/70 text-slate-500 text-[11px] uppercase tracking-wider">
                <th className="py-3 px-4">#</th>
                <th className="py-3 px-4">Symbol</th>
                <th className="py-3 px-4">Sector</th>
                <th className="py-3 px-4 text-right">LTP (₹)</th>
                <th className="py-3 px-4 text-right">Change %</th>
                <th className="py-3 px-4 text-right">VWAP (₹)</th>
                <th className="py-3 px-4 text-right">RVOL</th>
                <th className="py-3 px-4 text-right">Buyer Ratio</th>
                <th className="py-3 px-4 text-right">Pre-Trade EV</th>
                <th className="py-3 px-4 text-center">Action</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {filteredStocks.map((s, idx) => {
                const isUp = s.change_percent >= 0;
                return (
                  <tr
                    key={s.symbol}
                    onClick={() => router.push(`/search/${s.symbol.toUpperCase()}`)}
                    className="hover:bg-slate-50/80 transition-colors cursor-pointer group"
                  >
                    <td className="py-3 px-4 text-slate-400 text-[11px]">{idx + 1}</td>
                    <td className="py-3 px-4 font-bold text-slate-900 group-hover:text-emerald-700 transition-colors">
                      {s.symbol}
                    </td>
                    <td className="py-3 px-4 text-slate-500">{s.sector}</td>
                    <td className="py-3 px-4 text-right font-semibold text-slate-900">
                      ₹{s.ltp.toFixed(2)}
                    </td>
                    <td className="py-3 px-4 text-right font-bold">
                      <span
                        className={`inline-block px-1.5 py-0.5 rounded ${
                          isUp
                            ? "bg-emerald-50 text-emerald-700 border border-emerald-200/60"
                            : "bg-rose-50 text-rose-700 border border-rose-200/60"
                        }`}
                      >
                        {isUp ? "+" : ""}{s.change_percent.toFixed(2)}%
                      </span>
                    </td>
                    <td className="py-3 px-4 text-right text-slate-600">
                      ₹{s.vwap.toFixed(2)}
                    </td>
                    <td className="py-3 px-4 text-right">
                      <span className={`font-semibold ${s.rvol_session >= 1.8 ? "text-amber-700 font-bold" : "text-slate-600"}`}>
                        {s.rvol_session.toFixed(2)}x
                      </span>
                    </td>
                    <td className="py-3 px-4 text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        <div className="w-12 h-1.5 bg-slate-100 rounded-full overflow-hidden">
                          <div
                            className={`h-full ${s.buyer_ratio >= 0.55 ? "bg-emerald-500" : s.buyer_ratio <= 0.45 ? "bg-rose-500" : "bg-slate-400"}`}
                            style={{ width: `${Math.round(s.buyer_ratio * 100)}%` }}
                          />
                        </div>
                        <span className="text-[10px] text-slate-600 font-mono w-7 text-right">
                          {Math.round(s.buyer_ratio * 100)}%
                        </span>
                      </div>
                    </td>
                    <td className="py-3 px-4 text-right font-semibold">
                      <span className={s.ev >= 0.25 ? "text-indigo-700 font-bold" : "text-slate-500"}>
                        +{s.ev.toFixed(2)}R
                      </span>
                    </td>
                    <td className="py-3 px-4 text-center">
                      <span className="text-xs text-slate-400 group-hover:text-slate-900 font-semibold group-hover:underline">
                        Open Terminal →
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
