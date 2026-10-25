"use client";

import React, { useMemo, useState, memo } from "react";
import { StockRankData, decodeReasonFlags } from "@/lib/types";

interface FullRankedTableProps {
  stocks: StockRankData[];
  selectedSectorId?: number | null;
}

const RankedStockRow = memo(
  function RankedStockRow({ s }: { s: StockRankData }) {
    const badges = decodeReasonFlags(s.reasons);
    const isUp = s.change_percent >= 0;

    return (
      <tr className="hover:bg-slate-50/80 transition-colors group">
        <td className="py-2.5 px-3.5 text-center font-bold text-slate-400 group-hover:text-slate-900">
          #{s.rank}
        </td>
        <td className="py-2.5 px-3.5 font-bold text-slate-900 whitespace-nowrap">
          {s.symbol}
        </td>
        <td className="py-2.5 px-3.5 text-slate-500 text-[11px] whitespace-nowrap">
          {s.sector}
        </td>
        <td className="py-2.5 px-3.5 text-right font-black text-slate-900 whitespace-nowrap">
          ₹{s.ltp.toLocaleString("en-IN", { minimumFractionDigits: 2 })}
        </td>
        <td className="py-2.5 px-3.5 text-right font-bold whitespace-nowrap">
          <span
            className={`px-2 py-0.5 rounded-md ${
              isUp ? "bg-emerald-50 text-emerald-700" : "bg-rose-50 text-rose-700"
            }`}
          >
            {isUp ? "+" : ""}
            {s.change_percent.toFixed(2)}%
          </span>
        </td>
        <td className="py-2.5 px-3.5 text-right text-slate-600 whitespace-nowrap">
          ₹{s.vwap.toFixed(2)}
        </td>
        <td className="py-2.5 px-3.5 text-right font-bold whitespace-nowrap">
          <span className={s.rvol_session >= 1.8 ? "text-emerald-700" : "text-slate-600"}>
            {s.rvol_session.toFixed(1)}x
          </span>
        </td>
        <td className="py-2.5 px-3.5 text-right font-bold whitespace-nowrap">
          <span className={s.composite_rs >= 0 ? "text-emerald-700" : "text-rose-700"}>
            {s.composite_rs >= 0 ? "+" : ""}
            {s.composite_rs.toFixed(2)}%
          </span>
        </td>
        <td className="py-2.5 px-3.5 text-right whitespace-nowrap">
          <span className={`font-bold ${s.buyer_ratio >= 0.58 ? "text-emerald-700" : "text-slate-700"}`}>
            {(s.buyer_ratio * 100).toFixed(0)}%
          </span>
          <span className="text-[10px] text-slate-400 block">
            Agg: {(s.aggressor_buy_ratio * 100).toFixed(0)}%
          </span>
        </td>
        <td className="py-2.5 px-3.5 text-right whitespace-nowrap">
          <span className={`font-bold ${s.ev >= 0.25 ? "text-emerald-700" : "text-slate-600"}`}>
            {s.ev >= 0 ? "+" : ""}
            {s.ev.toFixed(2)}R
          </span>
          <span className="text-[10px] text-slate-400 block">
            {(s.p_win * 100).toFixed(0)}% win
          </span>
        </td>
        <td className="py-2.5 px-3.5">
          <div className="flex flex-wrap gap-1 max-w-xs">
            {badges.map((b) => (
              <span
                key={b.label}
                title={b.description}
                className={`px-1.5 py-0.5 rounded text-[10px] font-bold ${
                  b.type === "catalyst"
                    ? "bg-emerald-50 text-emerald-800 border border-emerald-200/80"
                    : "bg-rose-50 text-rose-800 border border-rose-200/80"
                }`}
              >
                {b.label}
              </span>
            ))}
          </div>
        </td>
      </tr>
    );
  },
  (prev, next) =>
    prev.s.rank === next.s.rank &&
    prev.s.ltp === next.s.ltp &&
    prev.s.change_percent === next.s.change_percent &&
    prev.s.vwap === next.s.vwap &&
    prev.s.rvol_session === next.s.rvol_session &&
    prev.s.composite_rs === next.s.composite_rs &&
    prev.s.buyer_ratio === next.s.buyer_ratio &&
    prev.s.aggressor_buy_ratio === next.s.aggressor_buy_ratio &&
    prev.s.ev === next.s.ev &&
    prev.s.p_win === next.s.p_win &&
    prev.s.reasons === next.s.reasons
);

export function FullRankedTable({ stocks, selectedSectorId }: FullRankedTableProps) {
  const [searchTerm, setSearchTerm] = useState("");
  const [sectorFilter, setSectorFilter] = useState<string>("ALL");
  const [sweetSpotOnly, setSweetSpotOnly] = useState(false);
  const [highRvolOnly, setHighRvolOnly] = useState(false);
  const [highEvOnly, setHighEvOnly] = useState(false);

  // Available unique sectors for dropdown
  const uniqueSectors = useMemo(() => {
    const set = new Set<string>();
    stocks.forEach((s) => {
      if (s.sector) set.add(s.sector);
    });
    return Array.from(set).sort();
  }, [stocks]);

  // Filtered dataset
  const filteredStocks = useMemo(() => {
    return stocks.filter((s) => {
      // 1. External sector selection from SectorHeatmapBar
      if (selectedSectorId && s.sector_id !== selectedSectorId) {
        return false;
      }

      // 2. Search query filter
      if (searchTerm.trim()) {
        const query = searchTerm.toLowerCase();
        if (!s.symbol.toLowerCase().includes(query) && !s.sector.toLowerCase().includes(query)) {
          return false;
        }
      }

      // 3. Sector dropdown filter
      if (sectorFilter !== "ALL" && s.sector !== sectorFilter) {
        return false;
      }

      // 4. Sweet Spot Checkbox (ReasonVWAPSweetSpot = 1 << 0)
      if (sweetSpotOnly && (s.reasons & (1 << 0)) === 0) {
        return false;
      }

      // 5. High RVOL Checkbox (>= 1.5x)
      if (highRvolOnly && s.rvol_session < 1.5 && s.rvol_pace_5m < 1.5) {
        return false;
      }

      // 6. High EV Checkbox (>= +0.25R)
      if (highEvOnly && s.ev < 0.25) {
        return false;
      }

      return true;
    });
  }, [stocks, selectedSectorId, searchTerm, sectorFilter, sweetSpotOnly, highRvolOnly, highEvOnly]);

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs space-y-4">
      {/* Table Controls / Filter Toolbar */}
      <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4">
        <div>
          <h3 className="text-base font-black text-slate-900 tracking-tight flex items-center gap-2">
            <span>Full NSE F&amp;O Watchlist</span>
            <span className="text-xs font-mono font-bold px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 border border-slate-200">
              {filteredStocks.length} of {stocks.length} Stocks
            </span>
          </h3>
          {/* <p className="text-xs text-slate-500 mt-0.5">
            Streaming unified 1.0s quantitative metrics with anti-spoof depth and risk haircuts.
          </p> */}
        </div>

        {/* Filter Controls */}
        <div className="flex flex-wrap items-center gap-3">
          {/* Search Input */}
          <div className="relative">
            <input
              type="text"
              placeholder="Search ticker..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="pl-8 pr-3 py-1.5 rounded-xl border border-slate-200 bg-slate-50/80 text-xs font-mono text-slate-800 placeholder-slate-400 focus:outline-none focus:ring-2 focus:ring-slate-900/10 focus:border-slate-400 transition-all w-36 sm:w-44"
            />
            <svg className="w-3.5 h-3.5 text-slate-400 absolute left-2.5 top-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
          </div>

          {/* Sector Dropdown */}
          <div className="relative">
            <select
              value={sectorFilter}
              onChange={(e) => setSectorFilter(e.target.value)}
              className="appearance-none pl-3 pr-8 py-1.5 rounded-xl border border-slate-200 bg-slate-50/80 text-xs font-mono text-slate-800 focus:outline-none focus:ring-2 focus:ring-slate-900/10 focus:border-slate-400 transition-all cursor-pointer"
            >
              <option value="ALL">All Sectors</option>
              {uniqueSectors.map((sec) => (
                <option key={sec} value={sec}>{sec}</option>
              ))}
            </select>
            <svg
              className="w-3.5 h-3.5 text-slate-400 absolute right-2.5 top-1/2 -translate-y-1/2 pointer-events-none"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
          </div>

          {/* Quick Toggle Checkboxes */}
          <div className="flex items-center gap-3 text-xs font-mono">
            <label className="flex items-center gap-1.5 cursor-pointer text-slate-700 hover:text-slate-900">
              <input
                type="checkbox"
                checked={sweetSpotOnly}
                onChange={(e) => setSweetSpotOnly(e.target.checked)}
                className="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
              />
              <span>Sweet Spot</span>
            </label>

            <label className="flex items-center gap-1.5 cursor-pointer text-slate-700 hover:text-slate-900">
              <input
                type="checkbox"
                checked={highRvolOnly}
                onChange={(e) => setHighRvolOnly(e.target.checked)}
                className="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
              />
              <span>RVOL &gt; 1.5x</span>
            </label>

            <label className="flex items-center gap-1.5 cursor-pointer text-slate-700 hover:text-slate-900">
              <input
                type="checkbox"
                checked={highEvOnly}
                onChange={(e) => setHighEvOnly(e.target.checked)}
                className="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
              />
              <span>EV &ge; +0.25R</span>
            </label>
          </div>
        </div>
      </div>

      {/* Table with CSS Content-Visibility Containment to isolate 1Hz layout recalculations */}
      <div
        className="overflow-x-auto rounded-2xl border border-slate-200/90 scrollbar-thin scrollbar-thumb-slate-200"
        style={{
          contentVisibility: "auto",
          containIntrinsicSize: "auto 900px auto 1200px",
          contain: "layout style paint",
        }}
      >
        <table className="w-full text-left border-collapse text-xs">
          <thead>
            <tr className="bg-slate-50/90 border-b border-slate-200 font-mono text-[11px] text-slate-500 font-bold uppercase tracking-wider">
              <th className="py-3 px-3.5 text-center w-12">Rk</th>
              <th className="py-3 px-3.5">Symbol</th>
              <th className="py-3 px-3.5">Sector</th>
              <th className="py-3 px-3.5 text-right">LTP (₹)</th>
              <th className="py-3 px-3.5 text-right">Chg %</th>
              <th className="py-3 px-3.5 text-right">VWAP (₹)</th>
              <th className="py-3 px-3.5 text-right">RVOL</th>
              <th className="py-3 px-3.5 text-right">RS vs Nifty</th>
              <th className="py-3 px-3.5 text-right">Buyer Flow</th>
              <th className="py-3 px-3.5 text-right">EV / P(Win)</th>
              <th className="py-3 px-3.5">Reason Badges</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 font-mono text-slate-700">
            {filteredStocks.length === 0 ? (
              <tr>
                <td colSpan={11} className="py-8 text-center text-slate-400">
                  No stocks match the selected filter criteria.
                </td>
              </tr>
            ) : (
              filteredStocks.map((s) => <RankedStockRow key={s.token} s={s} />)
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
