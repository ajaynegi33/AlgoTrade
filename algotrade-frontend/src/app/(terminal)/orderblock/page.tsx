"use client";

import React, { useState, useEffect, useRef, useMemo, useCallback } from "react";
import Link from "next/link";
import { getOrderBlockTree } from "@/lib/api";
import {
  OrderBlockTreeResponseData,
  MinimaxEvaluationData,
  TrajectoryData,
  DecisionGateItemData,
  CandleData,
} from "@/lib/types";
import { toast } from "sonner";

function formatINR(val: number, decimals = 2): string {
  const abs = Math.abs(val);
  const formatted = abs.toLocaleString("en-IN", {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  });
  if (val < 0) return `-₹${formatted}`;
  return `₹${formatted}`;
}

export default function OrderBlockPage() {
  const [data, setData] = useState<OrderBlockTreeResponseData | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [activeSide, setActiveSide] = useState<"ALL" | "BULLISH" | "BEARISH">("ALL");
  const [highlightedSymbol, setHighlightedSymbol] = useState<string | null>(null);

  // Popover / Inspection Modal State (matching Screenshots 2, 3, 4)
  const [selectedStock, setSelectedStock] = useState<MinimaxEvaluationData | null>(null);
  const dialogRef = useRef<HTMLDialogElement | null>(null);

  // Interactive Position Sizer State inside the Modal
  const [calcCapital, setCalcCapital] = useState<number>(500000);
  const [calcRiskPct, setCalcRiskPct] = useState<number>(1.0);

  const fetchData = useCallback(async (silent = false) => {
    if (!silent) setIsLoading(true);
    else setIsRefreshing(true);
    try {
      const res = await getOrderBlockTree();
      setData(res);
      // Default highlight first top pick if none selected
      if (!highlightedSymbol && res?.top_bullish_picks?.[0]) {
        setHighlightedSymbol(res.top_bullish_picks[0].symbol);
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to load OrderBlock data";
      toast.error(msg);
    } finally {
      setIsLoading(false);
      setIsRefreshing(false);
    }
  }, [highlightedSymbol]);

  useEffect(() => {
    fetchData();
    const interval = setInterval(() => fetchData(true), 5000);
    return () => clearInterval(interval);
  }, [fetchData]);

  // Modal open/close handling
  const handleOpenModal = useCallback((evalData: MinimaxEvaluationData) => {
    setSelectedStock(evalData);
    if (dialogRef.current) {
      dialogRef.current.showModal();
    }
  }, []);

  const handleCloseModal = useCallback(() => {
    if (dialogRef.current) {
      dialogRef.current.close();
    }
    setSelectedStock(null);
  }, []);

  const handleDialogClick = useCallback(
    (e: React.MouseEvent<HTMLDialogElement>) => {
      if (dialogRef.current && e.target === dialogRef.current) {
        const rect = dialogRef.current.getBoundingClientRect();
        const isInDialog =
          rect.top <= e.clientY &&
          e.clientY <= rect.top + rect.height &&
          rect.left <= e.clientX &&
          e.clientX <= rect.left + rect.width;
        if (!isInDialog) {
          handleCloseModal();
        }
      }
    },
    [handleCloseModal]
  );

  const marketPic = data?.market_picture;
  const bullishCount = marketPic?.bullish_count ?? 0;
  const bearishCount = marketPic?.bearish_count ?? 0;
  const totalCount = marketPic?.total_stocks ?? 213;

  // Filter trajectories based on active side
  const visibleTrajectories = useMemo(() => {
    if (!data?.trajectories) return [];
    if (activeSide === "BULLISH") return data.trajectories.filter((t) => t.direction === "BULLISH");
    if (activeSide === "BEARISH") return data.trajectories.filter((t) => t.direction === "BEARISH");
    return data.trajectories;
  }, [data?.trajectories, activeSide]);

  // Find pick by symbol helper
  const allPicks = useMemo(() => {
    const list: MinimaxEvaluationData[] = [];
    if (data?.top_bullish_picks) list.push(...data.top_bullish_picks);
    if (data?.top_bearish_picks) list.push(...data.top_bearish_picks);
    return list;
  }, [data?.top_bullish_picks, data?.top_bearish_picks]);

  const activePick = useMemo(() => {
    if (!highlightedSymbol) return allPicks[0] || null;
    return allPicks.find((p) => p.symbol === highlightedSymbol) || allPicks[0] || null;
  }, [allPicks, highlightedSymbol]);

  // Position Sizing calculations for modal
  const sizingCalc = useMemo(() => {
    if (!selectedStock) return null;
    const ob = selectedStock.order_block;
    const riskBudget = (calcCapital * calcRiskPct) / 100.0;
    const isBull = selectedStock.direction === "BULLISH";

    const riskPerShareZ = Math.max(
      0.05,
      isBull
        ? ob.zone_level_z - ob.invalidation_s1
        : (ob.invalidation_r1 ?? ob.zone_high) - ob.zone_level_z
    );
    const maxQtyZ = Math.max(1, Math.floor(riskBudget / riskPerShareZ));

    const curLtp = ob.candles_5m?.[ob.candles_5m.length - 1]?.close || ob.zone_level_z;
    const riskPerShareLtp = Math.max(
      0.05,
      isBull
        ? curLtp - ob.invalidation_s1
        : (ob.invalidation_r1 ?? ob.zone_high) - curLtp
    );
    const maxQtyLtp = Math.max(1, Math.floor(riskBudget / riskPerShareLtp));

    return {
      riskBudget,
      riskPerShareZ,
      maxQtyZ,
      riskPerShareLtp,
      maxQtyLtp,
      curLtp,
    };
  }, [selectedStock, calcCapital, calcRiskPct]);

  // Sector callout badges for connecting splines (matching Screenshot 1)
  const sectorBadges = useMemo(() => {
    const leading = marketPic?.leading_sectors?.slice(0, 3) || [
      { name: "AUTO", avg_move_pct: 1.75 },
      { name: "METAL", avg_move_pct: 1.12 },
      { name: "TELECOM", avg_move_pct: 0.65 },
    ];
    const lagging = marketPic?.lagging_sectors?.slice(0, 3) || [
      { name: "CAP GOODS", avg_move_pct: -0.85 },
      { name: "REALTY", avg_move_pct: -1.15 },
      { name: "CONSUMER", avg_move_pct: -1.75 },
    ];
    return { leading, lagging };
  }, [marketPic]);

  if (isLoading && !data) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[60vh] gap-4 font-mono">
        <div className="w-10 h-10 border-3 border-emerald-600 border-t-transparent rounded-full animate-spin" />
        <span className="text-xs text-slate-500 font-semibold tracking-wider uppercase">
          Synthesizing 20-Day 5m Order Blocks &amp; Institutional Quant Flow Map...
        </span>
      </div>
    );
  }

  return (
    <div className="space-y-6 font-mono select-none">
      {/* =========================================================================
          1. The Market Picture Top Institutional Banner
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs flex flex-col lg:flex-row lg:items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-[11px] font-bold uppercase tracking-wider text-slate-400">
              The Market Picture
            </span>
            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-ping" />
            <span
              className={`text-[11px] font-bold px-2.5 py-0.5 rounded-full border ${
                marketPic?.day_bias === "ONE_SIDED_BULLISH"
                  ? "bg-emerald-50 text-emerald-800 border-emerald-300"
                  : marketPic?.day_bias === "ONE_SIDED_BEARISH"
                  ? "bg-rose-50 text-rose-800 border-rose-300"
                  : "bg-slate-100 text-slate-700 border-slate-200"
              }`}
            >
              {marketPic?.day_bias === "ONE_SIDED_BULLISH"
                ? "▲ ONE-SIDED BULLISH SESSION"
                : marketPic?.day_bias === "ONE_SIDED_BEARISH"
                ? "▼ ONE-SIDED BEARISH SESSION"
                : "◆ BALANCED SESSION"}
            </span>
          </div>

          <div className="text-sm text-slate-600 mt-1.5 flex flex-wrap items-center gap-3">
            <span>
              <strong className="text-emerald-700 font-black">▲ {bullishCount} Bullish</strong> vs{" "}
              <strong className="text-rose-700 font-black">▼ {bearishCount} Bearish</strong> ({totalCount} F&amp;O Cash Universe)
            </span>
            <span className="text-slate-300">•</span>
            <span>
              Leader: <strong className="text-emerald-800 font-bold">{sectorBadges.leading[0]?.name} (+{sectorBadges.leading[0]?.avg_move_pct}%)</strong>
            </span>
            <span className="text-slate-300">•</span>
            <span>
              Laggard: <strong className="text-rose-800 font-bold">{sectorBadges.lagging[0]?.name} ({sectorBadges.lagging[0]?.avg_move_pct}%)</strong>
            </span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {/* Side Switcher */}
          <div className="inline-flex rounded-2xl bg-slate-100 p-1 border border-slate-200">
            <button
              type="button"
              onClick={() => setActiveSide("ALL")}
              className={`px-3.5 py-1.5 rounded-xl text-xs font-black transition-all cursor-pointer ${
                activeSide === "ALL" ? "bg-slate-900 text-white shadow-2xs" : "text-slate-600 hover:text-slate-900"
              }`}
            >
              ALL ({totalCount})
            </button>
            <button
              type="button"
              onClick={() => setActiveSide("BULLISH")}
              className={`px-3.5 py-1.5 rounded-xl text-xs font-black transition-all cursor-pointer ${
                activeSide === "BULLISH" ? "bg-emerald-600 text-white shadow-2xs" : "text-slate-600 hover:text-slate-900"
              }`}
            >
              ▲ BULLISH ({bullishCount})
            </button>
            <button
              type="button"
              onClick={() => setActiveSide("BEARISH")}
              className={`px-3.5 py-1.5 rounded-xl text-xs font-black transition-all cursor-pointer ${
                activeSide === "BEARISH" ? "bg-rose-600 text-white shadow-2xs" : "text-slate-600 hover:text-slate-900"
              }`}
            >
              ▼ BEARISH ({bearishCount})
            </button>
          </div>

          <button
            type="button"
            onClick={() => fetchData(false)}
            disabled={isRefreshing}
            className="p-2 rounded-xl border border-slate-200 hover:bg-slate-50 text-slate-500 hover:text-slate-900 transition-colors cursor-pointer"
            title="Refresh OrderBlock map"
          >
            <svg
              className={`w-4 h-4 ${isRefreshing ? "animate-spin" : ""}`}
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          </button>
        </div>
      </div>

      {/* =========================================================================
          2. The 3-Column Institutional Quant Flow Map (Matching Screenshots 1, 2, 3, 4)
          ========================================================================= */}
      <div className="grid grid-cols-1 xl:grid-cols-12 gap-5 items-start">
        {/* =========================================================================
            ZONE 1 (Left 5 Cols on XL): 09:15 Trajectory Fan + Reclaim Funnel + Spline Ribbons
            ========================================================================= */}
        <div className="xl:col-span-5 bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs space-y-4">
          <div className="flex items-center justify-between pb-3 border-b border-slate-100">
            <div>
              <div className="flex items-center gap-2">
                <span className="text-[10px] font-black uppercase tracking-wider text-emerald-800 bg-emerald-50 px-2 py-0.5 rounded border border-emerald-200">
                  ▲ BULLISH FLOW
                </span>
                <span className="text-[10px] font-black uppercase tracking-wider text-rose-800 bg-rose-50 px-2 py-0.5 rounded border border-rose-200">
                  ▼ BEARISH FLOW
                </span>
              </div>
              <h2 className="text-xs font-black uppercase tracking-wider text-slate-800 mt-1">
                09:15 Intraday Trajectory Fan
              </h2>
            </div>
            <span className="text-[10px] font-bold text-slate-400">09:15 &rarr; 15:30 IST</span>
          </div>

          {/* Canvas with SVG Trajectory Fan, Funnel Filter, and Spline Brackets */}
          <div className="relative w-full h-[480px] bg-slate-50/70 border border-slate-200/80 rounded-2xl overflow-hidden flex">
            {/* Background Benchmark Grid Lines */}
            <div className="absolute inset-0 pointer-events-none">
              {/* +2.0% */}
              <div className="absolute left-0 right-0 top-[15%] border-b border-dotted border-slate-300/80">
                <span className="text-[9px] font-bold text-slate-400 px-2">+2.00%</span>
              </div>
              {/* +1.0% */}
              <div className="absolute left-0 right-0 top-[32.5%] border-b border-dotted border-slate-300/60">
                <span className="text-[9px] font-bold text-slate-400 px-2">+1.00%</span>
              </div>
              {/* Zero Axis (0.00%) */}
              <div className="absolute left-0 right-0 top-[50%] border-b border-dashed border-slate-400/90">
                <span className="text-[9px] font-black text-slate-600 px-2 bg-white/90 rounded shadow-2xs">
                  0.00%
                </span>
              </div>
              {/* -1.0% */}
              <div className="absolute left-0 right-0 top-[67.5%] border-b border-dotted border-slate-300/60">
                <span className="text-[9px] font-bold text-slate-400 px-2">-1.00%</span>
              </div>
              {/* -2.0% */}
              <div className="absolute left-0 right-0 top-[85%] border-b border-dotted border-slate-300/80">
                <span className="text-[9px] font-bold text-slate-400 px-2">-2.00%</span>
              </div>

              {/* Vertical Reclaim Funnel Filter Bar (x = 64%) */}
              <div className="absolute top-2 bottom-6 left-[64%] w-px border-r-2 border-indigo-400/60 border-dashed">
                <div className="absolute -top-1 -left-2 text-[8px] font-black text-indigo-700 bg-indigo-50 px-1 rounded border border-indigo-200">
                  [ RECLAIM FUNNEL ]
                </div>
              </div>

              {/* Time Axis Along Bottom */}
              <div className="absolute bottom-1 left-8 right-2 flex justify-between text-[9px] font-bold text-slate-400">
                <span>09:15</span>
                <span>10:30</span>
                <span>12:00</span>
                <span>13:30</span>
                <span>15:30</span>
              </div>
            </div>

            {/* SVG Layer for Curves & Splines */}
            <svg className="w-full h-full" viewBox="0 0 600 480" preserveAspectRatio="none">
              <defs>
                <filter id="emeraldGlow" x="-20%" y="-20%" width="140%" height="140%">
                  <feGaussianBlur stdDeviation="3" result="blur" />
                  <feComposite in="SourceGraphic" in2="blur" operator="over" />
                </filter>
                <filter id="roseGlow" x="-20%" y="-20%" width="140%" height="140%">
                  <feGaussianBlur stdDeviation="3" result="blur" />
                  <feComposite in="SourceGraphic" in2="blur" operator="over" />
                </filter>
              </defs>

              {/* 09:15 Anchor Origin Dot (Blue Ring with Dark Core - Image 1) */}
              <circle cx="28" cy="240" r="10" fill="rgba(59, 130, 246, 0.2)" />
              <circle cx="28" cy="240" r="6" fill="#0f172a" stroke="#3b82f6" strokeWidth="2.5" />

              {/* All Trajectory Ribbons */}
              {visibleTrajectories.map((traj) => {
                if (!traj.points || traj.points.length < 2) return null;
                const isSelected = highlightedSymbol === traj.symbol;
                const isBull = traj.direction === "BULLISH";
                const strokeColor = isBull
                  ? isSelected
                    ? "#059669"
                    : traj.is_pick
                    ? "#10b981"
                    : "rgba(16, 185, 129, 0.25)"
                  : isSelected
                  ? "#e11d48"
                  : traj.is_pick
                  ? "#f43f5e"
                  : "rgba(244, 63, 94, 0.25)";
                const strokeWidth = isSelected ? 4.0 : traj.is_pick ? 2.2 : 1.1;

                const stepX = 360 / Math.max(1, traj.points.length - 1);
                const coords = traj.points.map((pt, idx) => {
                  const x = 32 + idx * stepX;
                  const clamped = Math.max(-2.5, Math.min(2.5, pt));
                  const y = 240 - (clamped / 2.5) * 180;
                  return { x, y };
                });

                const pathD = coords
                  .map((c, idx) => `${idx === 0 ? "M" : "L"} ${c.x.toFixed(1)} ${c.y.toFixed(1)}`)
                  .join(" ");

                return (
                  <path
                    key={traj.symbol}
                    d={pathD}
                    fill="none"
                    stroke={strokeColor}
                    strokeWidth={strokeWidth}
                    strokeLinecap="round"
                    filter={isSelected ? (isBull ? "url(#emeraldGlow)" : "url(#roseGlow)") : undefined}
                    className="transition-all duration-150 cursor-pointer hover:opacity-100"
                    onMouseEnter={() => setHighlightedSymbol(traj.symbol)}
                  />
                );
              })}

              {/* Connecting Spline Connectors (Sankey-style Cubic Bezier Splines - Image 1) */}
              {/* Connecting from Funnel x=392 to Right Edge Sector Endpoints */}
              <path
                d="M 384 150 C 420 150, 430 80, 460 80"
                fill="none"
                stroke="#10b981"
                strokeWidth="2"
                strokeDasharray="3 3"
              />
              <circle cx="460" cy="80" r="5" fill="#10b981" stroke="#ffffff" strokeWidth="2" />

              <path
                d="M 384 190 C 420 190, 430 140, 460 140"
                fill="none"
                stroke="#10b981"
                strokeWidth="1.5"
                strokeDasharray="3 3"
              />
              <circle cx="460" cy="140" r="4" fill="#10b981" stroke="#ffffff" strokeWidth="1.5" />

              <path
                d="M 384 290 C 420 290, 430 340, 460 340"
                fill="none"
                stroke="#f43f5e"
                strokeWidth="1.5"
                strokeDasharray="3 3"
              />
              <circle cx="460" cy="340" r="4" fill="#f43f5e" stroke="#ffffff" strokeWidth="1.5" />

              <path
                d="M 384 330 C 420 330, 430 400, 460 400"
                fill="none"
                stroke="#f43f5e"
                strokeWidth="2"
                strokeDasharray="3 3"
              />
              <circle cx="460" cy="400" r="5" fill="#f43f5e" stroke="#ffffff" strokeWidth="2" />
            </svg>

            {/* Right Edge Sector Capsule Badges (Matching Screenshot 1) */}
            <div className="absolute right-2 top-4 bottom-8 flex flex-col justify-between pointer-events-none">
              {/* Leading Sectors (Green Pills) */}
              <div className="space-y-1.5">
                {sectorBadges.leading.map((sec) => (
                  <div
                    key={sec.name}
                    className="pointer-events-auto bg-emerald-50/90 border border-emerald-300 text-emerald-900 text-[10px] font-black px-2.5 py-1 rounded-xl shadow-2xs flex items-center justify-between gap-2 min-w-[110px]"
                  >
                    <span>| {sec.name}</span>
                    <span className="font-mono text-emerald-700">+{sec.avg_move_pct}%</span>
                  </div>
                ))}
              </div>

              {/* Lagging Sectors (Red Pills) */}
              <div className="space-y-1.5">
                {sectorBadges.lagging.map((sec) => (
                  <div
                    key={sec.name}
                    className="pointer-events-auto bg-rose-50/90 border border-rose-300 text-rose-900 text-[10px] font-black px-2.5 py-1 rounded-xl shadow-2xs flex items-center justify-between gap-2 min-w-[110px]"
                  >
                    <span>| {sec.name}</span>
                    <span className="font-mono text-rose-700">{sec.avg_move_pct}%</span>
                  </div>
                ))}
              </div>
            </div>

            {/* Tracing Badge Indicator */}
            {highlightedSymbol && (
              <div className="absolute top-3 left-12 bg-slate-900/90 text-white text-[10px] font-bold px-3 py-1 rounded-xl shadow-sm backdrop-blur-xs flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
                Active Trajectory: <strong>{highlightedSymbol}</strong>
              </div>
            )}
          </div>
        </div>

        {/* =========================================================================
            ZONE 2 (Middle 3 Cols on XL): Candidate Matrices (Long & Short Build-Up)
            ========================================================================= */}
        <div className="xl:col-span-3 space-y-4">
          {/* Top Panel: BULLISH / LONG BUILD-UP Matrix */}
          <div className="bg-emerald-50/30 border border-emerald-200/80 rounded-3xl p-4 shadow-xs space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-[10px] font-black tracking-wider uppercase text-emerald-800 bg-emerald-100/80 px-2 py-0.5 rounded-md">
                ▲ LONG BUILD-UP &bull;&bull;&bull;&bull;&bull;
              </span>
              <span className="text-[10px] font-bold text-slate-400">
                {data?.bullish_gates?.length || 0} Candidates
              </span>
            </div>

            {/* 3x3 Candidate Grid */}
            <div className="grid grid-cols-2 gap-2">
              {data?.bullish_gates?.slice(0, 8).map((gate) => {
                const isHovered = highlightedSymbol === gate.symbol;
                return (
                  <div
                    key={gate.symbol}
                    onMouseEnter={() => setHighlightedSymbol(gate.symbol)}
                    className={`bg-white border rounded-xl p-2.5 transition-all duration-150 cursor-pointer shadow-2xs hover:scale-[1.02] ${
                      isHovered
                        ? "border-emerald-500 ring-2 ring-emerald-500/20 bg-emerald-50/40"
                        : "border-slate-200/80 hover:border-emerald-400"
                    }`}
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-[11px] font-black text-slate-900">{gate.symbol}</span>
                      <span className="text-[10px] font-bold text-emerald-700">+{gate.change_pct}%</span>
                    </div>
                    <span className="text-[9px] text-slate-400 block truncate">{gate.sector}</span>

                    {/* Mini Depth Volume Bar (Image 2) */}
                    <div className="w-full h-1 bg-slate-100 rounded-full mt-2 overflow-hidden flex">
                      <div
                        className="bg-emerald-500 h-full"
                        style={{ width: `${Math.min(100, Math.max(20, gate.score))}%` }}
                      />
                    </div>
                    <span className="text-[8px] text-slate-400 block text-right mt-0.5">
                      RVOL {gate.score.toFixed(0)}
                    </span>
                  </div>
                );
              })}
            </div>
          </div>

          {/* Bottom Panel: BEARISH / SHORT BUILD-UP Matrix */}
          <div className="bg-rose-50/30 border border-rose-200/80 rounded-3xl p-4 shadow-xs space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-[10px] font-black tracking-wider uppercase text-rose-800 bg-rose-100/80 px-2 py-0.5 rounded-md">
                ▼ SHORT BUILD-UP &bull;&bull;&bull;&bull;&bull;
              </span>
              <span className="text-[10px] font-bold text-slate-400">
                {data?.bearish_gates?.length || 0} Candidates
              </span>
            </div>

            {/* 3x3 Candidate Grid */}
            <div className="grid grid-cols-2 gap-2">
              {data?.bearish_gates?.slice(0, 8).map((gate) => {
                const isHovered = highlightedSymbol === gate.symbol;
                return (
                  <div
                    key={gate.symbol}
                    onMouseEnter={() => setHighlightedSymbol(gate.symbol)}
                    className={`bg-white border rounded-xl p-2.5 transition-all duration-150 cursor-pointer shadow-2xs hover:scale-[1.02] ${
                      isHovered
                        ? "border-rose-500 ring-2 ring-rose-500/20 bg-rose-50/40"
                        : "border-slate-200/80 hover:border-rose-400"
                    }`}
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-[11px] font-black text-slate-900">{gate.symbol}</span>
                      <span className="text-[10px] font-bold text-rose-700">{gate.change_pct}%</span>
                    </div>
                    <span className="text-[9px] text-slate-400 block truncate">{gate.sector}</span>

                    {/* Mini Depth Volume Bar */}
                    <div className="w-full h-1 bg-slate-100 rounded-full mt-2 overflow-hidden flex">
                      <div
                        className="bg-rose-500 h-full"
                        style={{ width: `${Math.min(100, Math.max(20, gate.score))}%` }}
                      />
                    </div>
                    <span className="text-[8px] text-slate-400 block text-right mt-0.5">
                      RVOL {gate.score.toFixed(0)}
                    </span>
                  </div>
                );
              })}
            </div>
          </div>
        </div>

        {/* =========================================================================
            ZONE 3 (Right 4 Cols on XL): Featured Alpha-Beta Showcase Cards (Images 2, 3, 4)
            ========================================================================= */}
        <div className="xl:col-span-4 space-y-4">
          <div className="flex items-center justify-between">
            <h2 className="text-xs font-black uppercase tracking-wider text-slate-800">
              Featured Order Block Showcase
            </h2>
            <span className="text-[10px] font-bold text-slate-400">Click to Deep-Dive</span>
          </div>

          {/* Top Bullish Showcase Cards */}
          {(activeSide === "ALL" || activeSide === "BULLISH") && (
            <div className="space-y-3">
              {data?.top_bullish_picks?.slice(0, 3).map((pick) => {
                const ob = pick.order_block;
                const isHovered = highlightedSymbol === pick.symbol;
                return (
                  <div
                    key={pick.symbol}
                    onMouseEnter={() => setHighlightedSymbol(pick.symbol)}
                    onClick={() => handleOpenModal(pick)}
                    className={`bg-white border rounded-2xl p-4 transition-all duration-150 cursor-pointer shadow-xs hover:border-emerald-500 hover:shadow-md ${
                      isHovered ? "border-emerald-500 ring-2 ring-emerald-500/20 bg-emerald-50/20" : "border-slate-200/90"
                    }`}
                  >
                    {/* Top Row: Symbol, Bull Badge, Long Build-Up */}
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="w-2.5 h-2.5 rounded-full bg-emerald-500 shadow-xs" />
                        <span className="text-xs font-black text-slate-900">{pick.symbol}</span>
                        <span className="text-[9px] font-black px-2 py-0.5 rounded-full bg-emerald-50 text-emerald-800 border border-emerald-300">
                          BULLISH &bull; {ob.zone_age}
                        </span>
                      </div>
                      <span className="text-[10px] font-bold text-emerald-800 bg-emerald-50 px-2 py-0.5 rounded border border-emerald-200">
                        LONG BUILD-UP
                      </span>
                    </div>

                    {/* Price & Zone Z Level */}
                    <div className="mt-2.5 flex items-baseline justify-between">
                      <div>
                        <span className="text-[9px] text-slate-400 block uppercase font-bold">Zone Z Target</span>
                        <strong className="text-base font-black text-slate-900">
                          ₹{ob.zone_level_z.toFixed(2)}
                        </strong>
                      </div>
                      <div className="text-right">
                        <span className="text-[9px] text-slate-400 block font-bold">Zone Band</span>
                        <span className="text-xs font-bold text-slate-700">
                          ₹{ob.zone_low.toFixed(1)} - ₹{ob.zone_high.toFixed(1)}
                        </span>
                      </div>
                    </div>

                    {/* Institutional Key Metrics Grid */}
                    <div className="mt-3 grid grid-cols-4 gap-1.5 text-center text-[10px] bg-slate-50 p-2 rounded-xl border border-slate-100">
                      <div>
                        <span className="text-slate-400 block font-bold">S1 (SL)</span>
                        <span className="font-black text-rose-700">₹{ob.invalidation_s1.toFixed(2)}</span>
                      </div>
                      <div>
                        <span className="text-slate-400 block font-bold">R1 TARGET</span>
                        <span className="font-black text-emerald-700">₹{ob.target_r1.toFixed(2)}</span>
                      </div>
                      <div>
                        <span className="text-slate-400 block font-bold">R2 RUNNER</span>
                        <span className="font-black text-emerald-800">₹{ob.target_r2.toFixed(2)}</span>
                      </div>
                      <div>
                        <span className="text-slate-400 block font-bold">R:R RATIO</span>
                        <span className="font-black text-indigo-700">1 : {ob.risk_reward_ratio.toFixed(1)}</span>
                      </div>
                    </div>

                    {/* Visual Risk-Reward Bar (Matching Screenshot 2 & 4) */}
                    <div className="mt-2.5 space-y-1">
                      <div className="flex justify-between text-[9px] text-slate-400 font-bold">
                        <span>S1 (Risk Floor)</span>
                        <span className="text-indigo-700 font-black">Zone Z Entry</span>
                        <span>R1 / R2 (Targets)</span>
                      </div>
                      <div className="w-full h-1.5 rounded-full bg-slate-200 overflow-hidden flex">
                        <div className="w-[30%] bg-rose-500 h-full" />
                        <div className="w-[10%] bg-amber-400 h-full" />
                        <div className="w-[60%] bg-emerald-500 h-full" />
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}

          {/* Top Bearish Showcase Cards */}
          {(activeSide === "ALL" || activeSide === "BEARISH") && (
            <div className="space-y-3 pt-2">
              {data?.top_bearish_picks?.slice(0, 3).map((pick) => {
                const ob = pick.order_block;
                const isHovered = highlightedSymbol === pick.symbol;
                return (
                  <div
                    key={pick.symbol}
                    onMouseEnter={() => setHighlightedSymbol(pick.symbol)}
                    onClick={() => handleOpenModal(pick)}
                    className={`bg-white border rounded-2xl p-4 transition-all duration-150 cursor-pointer shadow-xs hover:border-rose-500 hover:shadow-md ${
                      isHovered ? "border-rose-500 ring-2 ring-rose-500/20 bg-rose-50/20" : "border-slate-200/90"
                    }`}
                  >
                    {/* Top Row: Symbol, Bear Badge, Short Build-Up */}
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="w-2.5 h-2.5 rounded-full bg-rose-500 shadow-xs" />
                        <span className="text-xs font-black text-slate-900">{pick.symbol}</span>
                        <span className="text-[9px] font-black px-2 py-0.5 rounded-full bg-rose-50 text-rose-800 border border-rose-300">
                          BEARISH &bull; {ob.zone_age}
                        </span>
                      </div>
                      <span className="text-[10px] font-bold text-rose-800 bg-rose-50 px-2 py-0.5 rounded border border-rose-200">
                        SHORT BUILD-UP
                      </span>
                    </div>

                    {/* Price & Zone Z Level */}
                    <div className="mt-2.5 flex items-baseline justify-between">
                      <div>
                        <span className="text-[9px] text-slate-400 block uppercase font-bold">Zone Z Target</span>
                        <strong className="text-base font-black text-slate-900">
                          ₹{ob.zone_level_z.toFixed(2)}
                        </strong>
                      </div>
                      <div className="text-right">
                        <span className="text-[9px] text-slate-400 block font-bold">Zone Band</span>
                        <span className="text-xs font-bold text-slate-700">
                          ₹{ob.zone_low.toFixed(1)} - ₹{ob.zone_high.toFixed(1)}
                        </span>
                      </div>
                    </div>

                    {/* Institutional Key Metrics Grid */}
                    <div className="mt-3 grid grid-cols-4 gap-1.5 text-center text-[10px] bg-slate-50 p-2 rounded-xl border border-slate-100">
                      <div>
                        <span className="text-slate-400 block font-bold">R1 (SL)</span>
                        <span className="font-black text-rose-700">₹{(ob.invalidation_r1 ?? ob.zone_high).toFixed(2)}</span>
                      </div>
                      <div>
                        <span className="text-slate-400 block font-bold">S1 TARGET</span>
                        <span className="font-black text-emerald-700">₹{(ob.target_s1 ?? ob.zone_low).toFixed(2)}</span>
                      </div>
                      <div>
                        <span className="text-slate-400 block font-bold">S2 RUNNER</span>
                        <span className="font-black text-emerald-800">₹{(ob.target_s2 ?? ob.zone_low * 0.98).toFixed(2)}</span>
                      </div>
                      <div>
                        <span className="text-slate-400 block font-bold">R:R RATIO</span>
                        <span className="font-black text-indigo-700">1 : {ob.risk_reward_ratio.toFixed(1)}</span>
                      </div>
                    </div>

                    {/* Visual Risk-Reward Bar */}
                    <div className="mt-2.5 space-y-1">
                      <div className="flex justify-between text-[9px] text-slate-400 font-bold">
                        <span>R1 (Risk Ceiling)</span>
                        <span className="text-indigo-700 font-black">Zone Z Entry</span>
                        <span>S1 / S2 (Targets)</span>
                      </div>
                      <div className="w-full h-1.5 rounded-full bg-slate-200 overflow-hidden flex">
                        <div className="w-[30%] bg-rose-500 h-full" />
                        <div className="w-[10%] bg-amber-400 h-full" />
                        <div className="w-[60%] bg-emerald-500 h-full" />
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* =========================================================================
          3. Interactive High-Resolution Quant Popover Modal (Matching Screenshots 2, 3, 4)
          ========================================================================= */}
      <dialog
        ref={dialogRef}
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        {...({ closedby: "any" } as any)}
        aria-labelledby="dialogTitle"
        onClick={handleDialogClick}
        className="backdrop:bg-slate-900/40 backdrop:backdrop-blur-xs bg-white rounded-3xl p-6 shadow-2xl max-w-2xl w-full border border-slate-200 m-auto text-slate-900"
      >
        {selectedStock && sizingCalc && (
          <div className="space-y-5">
            {/* Popover Header */}
            <div className="flex items-center justify-between pb-3 border-b border-slate-100">
              <div className="flex items-center gap-3">
                <div className="w-9 h-9 rounded-2xl bg-slate-900 text-white flex items-center justify-center font-black text-xs shadow-xs">
                  {selectedStock.symbol.slice(0, 2)}
                </div>
                <div>
                  <div className="flex items-center gap-2">
                    <h3 id="dialogTitle" className="text-base font-black text-slate-900">
                      {selectedStock.symbol}
                    </h3>
                    <span
                      className={`text-[10px] font-bold px-2 py-0.5 rounded-full border ${
                        selectedStock.direction === "BULLISH"
                          ? "bg-emerald-50 text-emerald-800 border-emerald-300"
                          : "bg-rose-50 text-rose-800 border-rose-300"
                      }`}
                    >
                      {selectedStock.direction} &bull; {selectedStock.order_block.zone_age}
                    </span>
                  </div>
                  <span className="text-[11px] text-slate-400">
                    Minimax α-β EV: +{selectedStock.minimax_ev}R &bull; Conviction {selectedStock.conviction_pct}%
                  </span>
                </div>
              </div>

              <button
                type="button"
                onClick={handleCloseModal}
                className="w-8 h-8 rounded-full bg-slate-100 hover:bg-slate-200 text-slate-600 flex items-center justify-center font-bold cursor-pointer"
              >
                &times;
              </button>
            </div>

            {/* 2x4 Metric Matrix (Matching Screenshot 2 & 3 Popover) */}
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs">
              <div className="p-2.5 rounded-xl bg-slate-50 border border-slate-100">
                <span className="text-[9px] text-slate-400 block font-bold uppercase">RANK</span>
                <strong className="text-slate-800 font-black">#1 CANDIDATE</strong>
              </div>
              <div className="p-2.5 rounded-xl bg-slate-50 border border-slate-100">
                <span className="text-[9px] text-slate-400 block font-bold uppercase">CONVICTION</span>
                <strong className="text-emerald-700 font-black">{selectedStock.conviction_pct}%</strong>
              </div>
              <div className="p-2.5 rounded-xl bg-slate-50 border border-slate-100">
                <span className="text-[9px] text-slate-400 block font-bold uppercase">ORDER BLOCK</span>
                <strong className="text-slate-800 font-black">
                  ₹{selectedStock.order_block.zone_level_z.toFixed(2)} (Z)
                </strong>
              </div>
              <div className="p-2.5 rounded-xl bg-slate-50 border border-slate-100">
                <span className="text-[9px] text-slate-400 block font-bold uppercase">R:R RATIO</span>
                <strong className="text-indigo-700 font-black">
                  1 : {selectedStock.order_block.risk_reward_ratio.toFixed(1)}
                </strong>
              </div>
            </div>

            {/* High-Resolution Candlestick Chart with Support/Resistance Zones (Screenshots 2 & 3) */}
            <div className="space-y-1.5">
              <div className="flex justify-between items-center text-[10px] text-slate-500 font-bold">
                <span>5-Minute Completed Candlestick Zone Action</span>
                <span className="text-emerald-700">S1 &rarr; Zone Z &rarr; R1 Target</span>
              </div>

              <div className="relative w-full h-[180px] bg-slate-900 rounded-2xl p-2.5 overflow-hidden border border-slate-800 flex items-center justify-center">
                {/* Visual Candlesticks SVG */}
                <svg className="w-full h-full" viewBox="0 0 500 160" preserveAspectRatio="none">
                  {/* Zone Z Shaded Box */}
                  <rect x="0" y="70" width="500" height="25" fill="rgba(16, 185, 129, 0.15)" />
                  <line x1="0" y1="82" x2="500" y2="82" stroke="#10b981" strokeWidth="1" strokeDasharray="4 4" />
                  <text x="8" y="78" fill="#10b981" fontSize="9" fontWeight="bold">ZONE Z (ENTRY)</text>

                  {/* S1 Invalidation Stop Level */}
                  <line x1="0" y1="125" x2="500" y2="125" stroke="#f43f5e" strokeWidth="1" strokeDasharray="3 3" />
                  <text x="8" y="122" fill="#f43f5e" fontSize="9" fontWeight="bold">S1 (SL ₹{selectedStock.order_block.invalidation_s1.toFixed(1)})</text>

                  {/* R1 Target Level */}
                  <line x1="0" y1="30" x2="500" y2="30" stroke="#10b981" strokeWidth="1" strokeDasharray="3 3" />
                  <text x="8" y="26" fill="#10b981" fontSize="9" fontWeight="bold">R1 (TARGET ₹{selectedStock.order_block.target_r1.toFixed(1)})</text>

                  {/* 5m Candlesticks Rendering */}
                  {selectedStock.order_block.candles_5m?.slice(-18).map((c, i) => {
                    const x = 50 + i * 24;
                    const minP = selectedStock.order_block.invalidation_s1 * 0.99;
                    const maxP = selectedStock.order_block.target_r1 * 1.01;
                    const scaleY = (p: number) => 150 - ((p - minP) / (maxP - minP)) * 130;

                    const openY = scaleY(c.open);
                    const closeY = scaleY(c.close);
                    const highY = scaleY(c.high);
                    const lowY = scaleY(c.low);
                    const isGreen = c.close >= c.open;
                    const color = isGreen ? "#10b981" : "#f43f5e";

                    return (
                      <g key={i}>
                        {/* Wick */}
                        <line x1={x + 5} y1={highY} x2={x + 5} y2={lowY} stroke={color} strokeWidth="1.2" />
                        {/* Body */}
                        <rect
                          x={x}
                          y={Math.min(openY, closeY)}
                          width="10"
                          height={Math.max(2, Math.abs(closeY - openY))}
                          fill={color}
                          rx="1"
                        />
                      </g>
                    );
                  })}
                </svg>
              </div>
            </div>

            {/* Position Sizer Calculator (Prompt Math Implementation) */}
            <div className="p-4 rounded-2xl bg-slate-50 border border-slate-200 space-y-3">
              <span className="text-xs font-black text-slate-800 uppercase block">
                OrderBlock Capital &amp; Position Sizing Calculator
              </span>

              <div className="grid grid-cols-2 gap-3 text-xs">
                <div>
                  <label className="text-[10px] text-slate-500 font-bold block mb-1">
                    Trading Capital (₹)
                  </label>
                  <input
                    type="number"
                    value={calcCapital}
                    onChange={(e) => setCalcCapital(Number(e.target.value) || 0)}
                    className="w-full px-3 py-1.5 rounded-xl border border-slate-200 bg-white font-mono text-xs"
                  />
                </div>
                <div>
                  <label className="text-[10px] text-slate-500 font-bold block mb-1">
                    Risk per Trade (%)
                  </label>
                  <input
                    type="number"
                    step="0.1"
                    value={calcRiskPct}
                    onChange={(e) => setCalcRiskPct(Number(e.target.value) || 0)}
                    className="w-full px-3 py-1.5 rounded-xl border border-slate-200 bg-white font-mono text-xs"
                  />
                </div>
              </div>

              {/* Sizing Formula Output */}
              <div className="p-3 rounded-xl bg-white border border-slate-200/80 text-xs space-y-1 font-mono">
                <div className="flex justify-between">
                  <span className="text-slate-500">Risk Budget ({calcRiskPct}%):</span>
                  <strong className="text-slate-900">{formatINR(sizingCalc.riskBudget)}</strong>
                </div>
                <div className="flex justify-between">
                  <span className="text-slate-500">Risk per share (Z - S1):</span>
                  <strong className="text-slate-900">{formatINR(sizingCalc.riskPerShareZ)}</strong>
                </div>
                <div className="flex justify-between border-t border-slate-100 pt-1 text-emerald-800 font-bold">
                  <span>Optimal Position @ Zone Z:</span>
                  <strong className="text-sm font-black text-emerald-700">
                    {sizingCalc.maxQtyZ.toLocaleString("en-IN")} shares
                  </strong>
                </div>
              </div>
            </div>

            {/* Terminal Action Button */}
            <div className="pt-2 flex justify-end">
              <Link
                href={`/search/${encodeURIComponent(selectedStock.symbol)}`}
                className="px-5 py-2.5 rounded-xl bg-slate-900 hover:bg-slate-800 text-white text-xs font-black transition-colors shadow-xs"
              >
                Inspect {selectedStock.symbol} in Execution Terminal &rarr;
              </Link>
            </div>
          </div>
        )}
      </dialog>
    </div>
  );
}
