"use client";

import React from "react";
import {
  ResponsiveContainer,
  ComposedChart,
  Area,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  ReferenceLine,
  CartesianGrid,
} from "recharts";
import { StockChartPoint, ChartTimeframe, ChartType } from "../types";
import { SkeletonChart } from "../common/Skeleton";

interface StockPriceChartProps {
  symbol: string;
  data: StockChartPoint[];
  prevClose?: number;
  currentPrice: number;
  loading?: boolean;
  selectedTimeframe?: ChartTimeframe;
  onTimeframeChange?: (timeframe: ChartTimeframe) => void;
  chartType?: ChartType;
  onChartTypeChange?: (type: ChartType) => void;
}

const TIMEFRAMES: { label: string; value: ChartTimeframe }[] = [
  { label: "1D", value: "1D" },
  { label: "1W", value: "1W" },
  { label: "1M", value: "1M" },
  { label: "3M", value: "3M" },
  { label: "6M", value: "6M" },
  { label: "1Y", value: "1Y" },
  { label: "3Y", value: "3Y" },
  { label: "5Y", value: "5Y" },
  { label: "All", value: "ALL" },
];

interface CandlestickBarProps {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  parentViewBox?: { x: number; y: number; width: number; height: number };
  payload?: StockChartPoint;
  minPrice: number;
  maxPrice: number;
}

function CandlestickBar(props: CandlestickBarProps) {
  const { x, width, parentViewBox, payload, minPrice, maxPrice } = props;
  if (!payload || typeof x !== "number" || typeof width !== "number") {
    return null;
  }

  // Extract plot box safely
  const box = parentViewBox || (props as unknown as { viewBox?: { x: number; y: number; width: number; height: number } }).viewBox;
  const plotTop = box?.y ?? 10;
  const plotHeight = box?.height ?? 300;
  const range = maxPrice - minPrice;
  if (range <= 0 || plotHeight <= 0) return null;

  const getY = (val: number) => plotTop + ((maxPrice - val) / range) * plotHeight;

  const open = payload.open ?? payload.price;
  const close = payload.close ?? payload.price;
  const high = payload.high ?? Math.max(open, close);
  const low = payload.low ?? Math.min(open, close);

  const isBullish = close >= open;
  const color = isBullish ? "#10b981" : "#f43f5e";

  const yHigh = getY(high);
  const yLow = getY(low);
  const yOpen = getY(open);
  const yClose = getY(close);

  const centerX = x + width / 2;
  const bodyWidth = Math.max(2, Math.min(width * 0.75, 14));
  const bodyX = centerX - bodyWidth / 2;
  const bodyTop = Math.min(yOpen, yClose);
  const bodyHeight = Math.max(2, Math.abs(yClose - yOpen));

  return (
    <g className="transition-opacity duration-150 hover:opacity-80">
      {/* High-Low Wick / Shadow */}
      <line
        x1={centerX}
        y1={yHigh}
        x2={centerX}
        y2={yLow}
        stroke={color}
        strokeWidth={1.5}
        strokeLinecap="round"
      />
      {/* Open-Close Real Body */}
      <rect
        x={bodyX}
        y={bodyTop}
        width={bodyWidth}
        height={bodyHeight}
        fill={color}
        stroke={color}
        strokeWidth={1}
        rx={1}
      />
    </g>
  );
}

/**
 * Formats timestamps into readable Groww/Kite standard: "DD MMM YYYY • 12HR AM/PM"
 * Handles ISO strings with timezone (+0530), intraday times, and daily candles.
 */
function formatTooltipDateTime(rawDate?: string, timeStr?: string): string {
  let dateObj: Date | null = null;
  let isDaily = false;

  // 1. Try parsing rawDate (e.g., "2026-10-13T09:15:00+0530")
  if (rawDate) {
    // Normalize timezone format for maximum cross-browser compliance (e.g. +0530 -> +05:30)
    const normalized = rawDate.replace(/([+-]\d{2})(\d{2})$/, "$1:$2");
    const parsed = new Date(normalized);
    if (!isNaN(parsed.getTime())) {
      dateObj = parsed;
      if (rawDate.includes("T00:00:00")) isDaily = true;
    }
  }

  // 2. Fallback to timeStr if rawDate was missing or invalid
  if (!dateObj && timeStr) {
    if (/^\d{1,2}:\d{2}$/.test(timeStr.trim())) {
      const now = new Date();
      const [h, m] = timeStr.trim().split(":").map(Number);
      now.setHours(h, m, 0, 0);
      dateObj = now;
    } else {
      const parsed = new Date(timeStr);
      if (!isNaN(parsed.getTime())) {
        dateObj = parsed;
      }
    }
  }

  // Fallback if unable to parse into a date object
  if (!dateObj) {
    return timeStr || rawDate || "";
  }

  const day = String(dateObj.getDate()).padStart(2, "0");
  const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
  const month = months[dateObj.getMonth()];
  const year = dateObj.getFullYear();

  // If daily candle with 00:00 midnight time, show full date without time
  if (isDaily || (dateObj.getHours() === 0 && dateObj.getMinutes() === 0)) {
    return `${day} ${month} ${year}`;
  }

  // Format 12-hour AM/PM time
  let hours = dateObj.getHours();
  const minutes = String(dateObj.getMinutes()).padStart(2, "0");
  const ampm = hours >= 12 ? "PM" : "AM";
  hours = hours % 12 || 12;
  const hoursStr = String(hours).padStart(2, "0");

  return `${day} ${month} ${year} • ${hoursStr}:${minutes} ${ampm}`;
}

export default function StockPriceChart({
  symbol,
  data,
  prevClose,
  currentPrice,
  loading = false,
  selectedTimeframe = "1D",
  onTimeframeChange,
  chartType = "area",
  onChartTypeChange,
}: StockPriceChartProps) {
  // If initial load with zero data, render the skeleton loader
  if (loading && (!data || data.length === 0)) {
    return <SkeletonChart height="h-[380px] sm:h-[430px]" />;
  }

  // If data is unavailable
  if (!data || data.length === 0) {
    return (
      <div className="w-full bg-slate-950/60 rounded-xl border border-slate-800/80 p-5 shadow-inner">
        <div className="w-full h-[320px] flex flex-col items-center justify-center text-slate-500 text-xs">
          <span className="font-mono">No chart points available for {symbol} ({selectedTimeframe})</span>
          <span className="text-[10px] text-slate-600 mt-1">
            Historical candles resume or refresh during live market hours
          </span>
        </div>
        {/* Timeframe pill bar so user can still switch back */}
        <div className="flex items-center justify-between pt-3 border-t border-slate-800">
          <div className="flex items-center gap-1.5 overflow-x-auto py-1">
            {TIMEFRAMES.map((tf) => (
              <button
                key={tf.value}
                onClick={() => onTimeframeChange?.(tf.value)}
                className={`px-3 py-1 text-xs font-mono rounded-lg transition-all ${
                  selectedTimeframe === tf.value
                    ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/40 font-bold"
                    : "text-slate-400 hover:text-slate-200 hover:bg-slate-800/60"
                }`}
              >
                {tf.label}
              </button>
            ))}
          </div>
        </div>
      </div>
    );
  }

  // Baseline calculation: 1D uses prevClose if available, multi-timeframe uses start of period
  const baselinePrice =
    selectedTimeframe === "1D" && prevClose && prevClose > 0
      ? prevClose
      : data[0].open ?? data[0].price;

  const periodReturn = currentPrice - baselinePrice;
  const periodReturnPct = baselinePrice > 0 ? (periodReturn / baselinePrice) * 100 : 0;
  const isPositive = periodReturn >= 0;
  const strokeColor = isPositive ? "#10b981" : "#f43f5e";
  const gradientId = `colorPrice_${symbol.replace(/[^a-zA-Z0-9]/g, "")}`;

  // Calculate Period High and Low
  const allHighs = data.map((d) => d.high ?? d.price);
  const allLows = data.map((d) => d.low ?? d.price);
  if (selectedTimeframe === "1D" && prevClose) {
    allHighs.push(prevClose);
    allLows.push(prevClose);
  }
  const periodHigh = Math.max(...allHighs);
  const periodLow = Math.min(...allLows);

  // Y-Domain with padding for candle wicks and breathing room
  const padding = (periodHigh - periodLow) * 0.08 || periodLow * 0.005;
  const yDomain: [number, number] = [
    Math.floor(periodLow - padding),
    Math.ceil(periodHigh + padding),
  ];

  return (
    <div className="w-full bg-slate-950/60 rounded-xl border border-slate-800/80 p-5 shadow-inner">
      {/* Header with Symbol, Timeframe, Period Return Badge & Extremes */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-4">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-xs font-bold text-slate-300 uppercase tracking-wider font-mono">
              {symbol} • {selectedTimeframe === "1D" ? "Intraday Session" : `${selectedTimeframe} Performance`}
            </span>
            <span
              className={`text-[10px] px-2 py-0.5 rounded font-mono font-bold ${
                isPositive
                  ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30"
                  : "bg-rose-500/20 text-rose-400 border border-rose-500/30"
              }`}
            >
              {isPositive ? "▲ BULLISH" : "▼ BEARISH"}
            </span>
            {loading && (
              <span className="text-[10px] text-amber-400 font-mono flex items-center gap-1 animate-pulse">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-400"></span>
                Loading...
              </span>
            )}
          </div>

          {/* Period Return Badge */}
          <div className="flex items-center gap-2 mt-1">
            <span className="text-lg sm:text-xl font-bold font-mono text-white">
              ₹{currentPrice.toFixed(2)}
            </span>
            <span
              className={`text-xs sm:text-sm font-mono font-bold px-2 py-0.5 rounded ${
                periodReturn >= 0
                  ? "bg-emerald-500/15 text-emerald-400 border border-emerald-500/25"
                  : "bg-rose-500/15 text-rose-400 border border-rose-500/25"
              }`}
            >
              {periodReturn >= 0 ? "+" : ""}₹{periodReturn.toFixed(2)} ({periodReturnPct >= 0 ? "+" : ""}{periodReturnPct.toFixed(2)}%)
              <span className="text-[10px] opacity-75 ml-1 font-normal">({selectedTimeframe})</span>
            </span>
          </div>
        </div>

        {/* Extremes & Baseline Info */}
        <div className="flex flex-wrap items-center gap-3 sm:gap-4 text-xs font-mono text-slate-400">
          <div className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-rose-500/80"></span>
            <span className="text-slate-400">Low:</span>
            <span className="text-slate-200 font-semibold">₹{periodLow.toFixed(2)}</span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-emerald-500/80"></span>
            <span className="text-slate-400">High:</span>
            <span className="text-slate-200 font-semibold">₹{periodHigh.toFixed(2)}</span>
          </div>
          {baselinePrice > 0 && (
            <div className="flex items-center gap-1.5">
              <span className="w-2.5 h-0.5 bg-amber-400 inline-block rounded"></span>
              <span className="text-slate-400">
                {selectedTimeframe === "1D" && prevClose ? "Prev Close" : "Start"}:
              </span>
              <span className="text-amber-300 font-semibold">₹{baselinePrice.toFixed(2)}</span>
            </div>
          )}
        </div>
      </div>

      {/* Chart Canvas (Responsive ComposedChart) */}
      <div className="w-full h-[320px] sm:h-[370px]">
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart data={data} margin={{ top: 10, right: 10, left: -15, bottom: 0 }}>
            <defs>
              <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={strokeColor} stopOpacity={0.35} />
                <stop offset="95%" stopColor={strokeColor} stopOpacity={0.0} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" opacity={0.6} vertical={false} />
            <XAxis
              dataKey="time"
              stroke="#64748b"
              fontSize={11}
              tickLine={false}
              axisLine={{ stroke: "#334155" }}
              minTickGap={28}
            />
            <YAxis
              domain={yDomain}
              stroke="#64748b"
              fontSize={11}
              tickLine={false}
              axisLine={{ stroke: "#334155" }}
              tickFormatter={(v) => `₹${v}`}
            />
            <Tooltip
              content={({ active, payload }) => {
                if (active && payload && payload.length) {
                  const pt = payload[0].payload as StockChartPoint;
                  const open = pt.open ?? pt.price;
                  const close = pt.close ?? pt.price;
                  const high = pt.high ?? Math.max(open, close);
                  const low = pt.low ?? Math.min(open, close);
                  const isUp = close >= open;
                  const candleChange = close - open;
                  const candlePct = open > 0 ? (candleChange / open) * 100 : 0;

                  return (
                    <div className="bg-slate-900/95 border border-slate-700/80 rounded-xl p-3.5 shadow-2xl backdrop-blur text-xs font-mono min-w-[200px]">
                      <div className="flex items-center justify-between border-b border-slate-800 pb-2 mb-2 gap-2">
                        <span className="text-slate-300 text-[11px] font-semibold tracking-wide">
                          {formatTooltipDateTime(pt.rawDate, pt.time)}
                        </span>
                        <span
                          className={`text-[10px] px-1 py-0.5 rounded font-bold ${
                            isUp ? "bg-emerald-500/20 text-emerald-400" : "bg-rose-500/20 text-rose-400"
                          }`}
                        >
                          {isUp ? "▲ BULLISH" : "▼ BEARISH"}
                        </span>
                      </div>

                      {chartType === "candle" || pt.open !== undefined ? (
                        <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-[11px] mb-2">
                          <div className="flex justify-between">
                            <span className="text-slate-500">Open:</span>
                            <span className="text-white font-semibold">₹{open.toFixed(2)}</span>
                          </div>
                          <div className="flex justify-between">
                            <span className="text-slate-500">High:</span>
                            <span className="text-emerald-400 font-semibold">₹{high.toFixed(2)}</span>
                          </div>
                          <div className="flex justify-between">
                            <span className="text-slate-500">Low:</span>
                            <span className="text-rose-400 font-semibold">₹{low.toFixed(2)}</span>
                          </div>
                          <div className="flex justify-between">
                            <span className="text-slate-500">Close:</span>
                            <span className="text-white font-semibold">₹{close.toFixed(2)}</span>
                          </div>
                        </div>
                      ) : (
                        <div className="text-white font-bold text-base mb-1">
                          ₹{pt.price.toFixed(2)}
                        </div>
                      )}

                      <div className="flex items-center justify-between pt-1 border-t border-slate-800/80 text-[11px]">
                        <span className="text-slate-400">Change:</span>
                        <span className={`font-bold ${candleChange >= 0 ? "text-emerald-400" : "text-rose-400"}`}>
                          {candleChange >= 0 ? "+" : ""}₹{candleChange.toFixed(2)} ({candlePct >= 0 ? "+" : ""}{candlePct.toFixed(2)}%)
                        </span>
                      </div>

                      {pt.volume ? (
                        <div className="flex items-center justify-between pt-1 text-[10px] text-slate-500">
                          <span>Volume:</span>
                          <span className="text-slate-300 font-semibold">{pt.volume.toLocaleString()}</span>
                        </div>
                      ) : null}
                    </div>
                  );
                }
                return null;
              }}
            />

            {/* Baseline Reference Line */}
            {baselinePrice > 0 && (
              <ReferenceLine
                y={baselinePrice}
                stroke="#fbbf24"
                strokeDasharray="4 4"
                strokeWidth={1.5}
                label={{
                  value: `${selectedTimeframe === "1D" && prevClose ? "Prev Close" : "Baseline"} ₹${baselinePrice.toFixed(2)}`,
                  fill: "#fbbf24",
                  fontSize: 10,
                  position: "insideTopLeft",
                }}
              />
            )}

            {/* Render Candlesticks or Area Chart according to user toggle */}
            {chartType === "candle" ? (
              <Bar
                dataKey="price"
                shape={(barProps: unknown) => (
                  <CandlestickBar
                    {...(barProps as CandlestickBarProps)}
                    minPrice={yDomain[0]}
                    maxPrice={yDomain[1]}
                  />
                )}
                isAnimationActive={false}
              />
            ) : (
              <Area
                type="monotone"
                dataKey="price"
                stroke={strokeColor}
                strokeWidth={2.5}
                fillOpacity={1}
                fill={`url(#${gradientId})`}
                dot={false}
                activeDot={{ r: 5, fill: strokeColor, stroke: "#0f172a", strokeWidth: 2 }}
                isAnimationActive={false}
              />
            )}
          </ComposedChart>
        </ResponsiveContainer>
      </div>

      {/* Groww-style Timeframe Selector Bar & Candlestick Toggle */}
      <div className="flex flex-wrap items-center justify-between gap-3 pt-3 mt-2 border-t border-slate-800/80">
        {/* Timeframe Pills */}
        <div className="flex items-center gap-1 sm:gap-1.5 overflow-x-auto py-1 scrollbar-none">
          {TIMEFRAMES.map((tf) => {
            const isSelected = selectedTimeframe === tf.value;
            return (
              <button
                key={tf.value}
                onClick={() => onTimeframeChange?.(tf.value)}
                className={`px-3 py-1 text-xs font-mono font-medium rounded-lg transition-all duration-150 cursor-pointer ${
                  isSelected
                    ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/50 shadow-sm shadow-emerald-950 font-bold"
                    : "text-slate-400 hover:text-slate-200 hover:bg-slate-800/60 border border-transparent"
                }`}
              >
                {tf.label}
              </button>
            );
          })}
        </div>

        {/* Chart Style Toggle (Area vs Candlestick) */}
        <div className="flex items-center gap-1 bg-slate-900/80 p-0.5 rounded-lg border border-slate-800">
          <button
            onClick={() => onChartTypeChange?.("area")}
            title="Area / Line View"
            className={`flex items-center gap-1.5 px-2.5 py-1 text-xs font-mono rounded-md transition-all cursor-pointer ${
              chartType === "area"
                ? "bg-slate-800 text-white font-bold shadow-sm"
                : "text-slate-400 hover:text-slate-200"
            }`}
          >
            <span>📊</span>
            <span className="hidden sm:inline">Line</span>
          </button>
          <button
            onClick={() => onChartTypeChange?.("candle")}
            title="Japanese Candlestick View"
            className={`flex items-center gap-1.5 px-2.5 py-1 text-xs font-mono rounded-md transition-all cursor-pointer ${
              chartType === "candle"
                ? "bg-slate-800 text-emerald-400 font-bold shadow-sm"
                : "text-slate-400 hover:text-slate-200"
            }`}
          >
            <span>🕯️</span>
            <span className="hidden sm:inline">Candles</span>
          </button>
        </div>
      </div>
    </div>
  );
}
