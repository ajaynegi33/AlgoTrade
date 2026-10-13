"use client";

import React, { useState, useMemo, useRef, useEffect } from "react";
import {
  ResponsiveContainer,
  ComposedChart,
  Area,
  Bar,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ReferenceLine,
  CartesianGrid,
  Customized,
} from "recharts";
import { StockChartPoint, ChartTimeframe, ChartInterval, ChartType } from "../types";
import { SkeletonChart } from "../common/Skeleton";

interface StockPriceChartProps {
  symbol: string;
  data: StockChartPoint[];
  prevClose?: number;
  pdh?: number;
  pdl?: number;
  dayHigh?: number;
  dayLow?: number;
  currentPrice: number;
  loading?: boolean;
  selectedTimeframe?: ChartTimeframe;
  onTimeframeChange?: (timeframe: ChartTimeframe) => void;
  selectedInterval?: ChartInterval;
  onIntervalChange?: (interval: ChartInterval) => void;
  chartType?: ChartType;
  onChartTypeChange?: (type: ChartType) => void;
}

const INTRADAY_INTERVALS: { label: string; value: ChartInterval }[] = [
  { label: "1m", value: "1m" },
  { label: "3m", value: "3m" },
  { label: "5m", value: "5m" },
  { label: "15m", value: "15m" },
];

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

/**
 * Beginner-friendly educational popover for chart indicators & terms
 */
interface EducationalInfoBadgeProps {
  id: string;
  title: string;
  description: string;
  tradingTip?: string;
  activeKey: string | null;
  onHover: (key: string | null) => void;
  badgeText?: string;
  badgeColor?: string;
}

function EducationalInfoBadge({
  id,
  title,
  description,
  tradingTip,
  activeKey,
  onHover,
  badgeText,
  badgeColor = "text-cyan-400",
}: EducationalInfoBadgeProps) {
  const isOpen = activeKey === id;

  return (
    <div
      className="relative inline-flex items-center"
      onMouseEnter={() => onHover(id)}
      onMouseLeave={() => onHover(null)}
    >
      <button
        type="button"
        className="w-4 h-4 rounded-full bg-slate-800/90 hover:bg-slate-700 text-slate-400 hover:text-white flex items-center justify-center text-[10px] font-serif transition-colors ml-1 cursor-help"
        aria-label={`Info about ${title}`}
      >
        i
      </button>

      {isOpen && (
        <div className="absolute z-50 bottom-full left-1/2 -translate-x-1/2 mb-2 w-64 p-3 bg-slate-900/98 border border-slate-700/90 rounded-xl shadow-2xl backdrop-blur-md text-xs font-sans text-slate-200 pointer-events-none animate-in fade-in zoom-in-95 duration-150">
          <div className="flex items-center justify-between border-b border-slate-800 pb-1.5 mb-2">
            <span className="font-bold text-white text-[12px]">{title}</span>
            {badgeText && (
              <span className={`text-[10px] font-mono font-bold px-1.5 py-0.5 rounded bg-slate-800 ${badgeColor}`}>
                {badgeText}
              </span>
            )}
          </div>
          <p className="text-[11px] leading-relaxed text-slate-300 mb-2">
            {description}
          </p>
          {tradingTip && (
            <div className="pt-1.5 border-t border-slate-800/80 flex items-start gap-1.5 text-[10px] text-amber-300">
              <span className="shrink-0 font-bold">💡 Tip:</span>
              <span className="leading-tight">{tradingTip}</span>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

interface CandlestickBarProps {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  background?: { x: number; y: number; width: number; height: number };
  parentViewBox?: { x: number; y: number; width: number; height: number };
  payload?: StockChartPoint;
  minPrice: number;
  maxPrice: number;
  showVolume?: boolean;
  maxVolume?: number;
  isLive?: boolean;
}

function CandlestickBar(props: CandlestickBarProps) {
  const { x, width, payload, minPrice, maxPrice, showVolume, maxVolume, isLive } = props;
  if (!payload || typeof x !== "number" || typeof width !== "number") {
    return null;
  }

  // Extract plot box safely: Recharts props.background provides { x, y: offset.top, width, height: offset.height }
  // This accurately represents the chart area above the X-axis baseline (excluding tick labels).
  const bg = props.background;
  const plotTop = bg?.y ?? 10;
  const plotBaseline = (bg && typeof bg.y === "number" && typeof bg.height === "number")
    ? bg.y + bg.height
    : (typeof props.y === "number" && typeof props.height === "number" && (props.y + props.height) > plotTop)
      ? props.y + props.height
      : (props.parentViewBox ? props.parentViewBox.height - 30 : 350);
  const plotHeight = Math.max(10, plotBaseline - plotTop);
  const range = maxPrice - minPrice;
  if (range <= 0 || plotHeight <= 0) return null;

  const getY = (val: number) => plotTop + ((maxPrice - val) / range) * plotHeight;

  const open = payload.open ?? payload.price;
  const close = payload.close ?? payload.price;
  const high = payload.high ?? Math.max(open, close);
  const low = payload.low ?? Math.min(open, close);

  const isBullish = close >= open;
  const color = isBullish ? "#10b981" : "#f43f5e";
  const glowFilter = isLive ? (isBullish ? "url(#liveGlowBullish)" : "url(#liveGlowBearish)") : undefined;

  const yHigh = getY(high);
  const yLow = getY(low);
  const yOpen = getY(open);
  const yClose = getY(close);

  // Exact centerline of the slot - shared by candle wick, body, volume bar, and tooltip crosshair
  const centerX = x + width / 2;
  const bodyWidth = Math.max(2, Math.min(width * 0.75, 14));
  const bodyX = centerX - bodyWidth / 2;
  const bodyTop = Math.min(yOpen, yClose);
  const bodyHeight = Math.max(2, Math.abs(yClose - yOpen));

  // Volume Sub-Bar rendered along bottom 22% of chart, perfectly aligned with candle center and anchored at plotBaseline
  let volumeBarElement = null;
  if (showVolume && payload.volume && payload.volume > 0 && maxVolume && maxVolume > 0) {
    const maxVolScaled = maxVolume * 4.5;
    const volHeight = Math.max(1, Math.min(plotHeight * 0.22, (payload.volume / maxVolScaled) * plotHeight));
    const volTop = plotBaseline - volHeight;
    const volWidth = Math.max(2, Math.min(width * 0.7, 12));
    const volX = centerX - volWidth / 2;

    volumeBarElement = (
      <rect
        x={volX}
        y={volTop}
        width={volWidth}
        height={volHeight}
        fill={color}
        opacity={0.35}
        rx={1}
      />
    );
  }

  return (
    <g className="transition-opacity duration-150 hover:opacity-85">
      {/* Volume Sub-Bar at Bottom (100% aligned on centerX) */}
      {volumeBarElement}

      {/* High-Low Wick / Shadow (100% aligned on centerX) */}
      <line
        x1={centerX}
        y1={yHigh}
        x2={centerX}
        y2={yLow}
        stroke={color}
        strokeWidth={isLive ? 2 : 1.5}
        strokeLinecap="round"
        filter={glowFilter}
      />
      {/* Open-Close Real Body (100% aligned on centerX) */}
      <rect
        x={bodyX}
        y={bodyTop}
        width={bodyWidth}
        height={bodyHeight}
        fill={color}
        stroke={color}
        strokeWidth={1}
        rx={1}
        filter={glowFilter}
      />
    </g>
  );
}

/**
 * Volume Sub-Chart Bar rendering along bottom 20% of canvas
 */
interface VolumeBarProps {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  payload?: StockChartPoint;
}

function VolumeBarShape(props: VolumeBarProps) {
  const { x, y, width, height, payload } = props;
  if (!payload || typeof x !== "number" || typeof width !== "number" || typeof y !== "number" || typeof height !== "number") {
    return null;
  }
  if (!payload.volume || payload.volume <= 0 || height <= 0) return null;

  const open = payload.open ?? payload.price;
  const close = payload.close ?? payload.price;
  const isBullish = close >= open;
  const color = isBullish ? "#10b981" : "#f43f5e";

  const centerX = x + width / 2;
  const barWidth = Math.max(1.5, Math.min(width * 0.7, 12));
  const barX = centerX - barWidth / 2;

  return (
    <rect
      x={barX}
      y={y}
      width={barWidth}
      height={Math.max(1, height)}
      fill={color}
      opacity={0.35}
      rx={1}
      className="transition-opacity duration-150 hover:opacity-80"
    />
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
  pdh,
  pdl,
  dayHigh: _dayHigh,
  dayLow: _dayLow,
  currentPrice,
  loading = false,
  selectedTimeframe = "1D",
  onTimeframeChange,
  selectedInterval = "5m",
  onIntervalChange,
  chartType = "area",
  onChartTypeChange,
}: StockPriceChartProps) {
  // Overlay Toggles (VWAP, Volume, Levels, Crosshair)
  const [showVwap, setShowVwap] = useState(true);
  const [showVolume, setShowVolume] = useState(true);
  const [showLevels, setShowLevels] = useState(true);
  const [showCrosshair, setShowCrosshair] = useState(true);
  const [crosshairPos, setCrosshairPos] = useState<{
    x: number;
    y: number;
    price: number;
    time: string;
  } | null>(null);
  const [hoveredInfoKey, setHoveredInfoKey] = useState<string | null>(null);

  // Real-time tick direction tracker for instant up/down color calibration
  const prevPriceRef = useRef(currentPrice);
  const [lastTickDir, setLastTickDir] = useState<"up" | "down" | null>(null);

  useEffect(() => {
    if (currentPrice > 0 && prevPriceRef.current > 0 && currentPrice !== prevPriceRef.current) {
      setLastTickDir(currentPrice > prevPriceRef.current ? "up" : "down");
      prevPriceRef.current = currentPrice;
    }
  }, [currentPrice]);

  // 1. Calculate Cumulative VWAP across intraday session (only for 1D)
  // MUST be called before any early returns to strictly satisfy React Rules of Hooks
  const enrichedData = useMemo(() => {
    if (!data || data.length === 0) return [];

    let cumVal = 0;
    let cumVol = 0;

    return data.map((d, index) => {
      const isLast = index === data.length - 1;
      const isLiveActive = selectedTimeframe === "1D" && currentPrice > 0 && isLast;

      const open = d.open ?? d.price;
      const close = isLiveActive ? currentPrice : (d.close ?? d.price);
      const price = isLiveActive ? currentPrice : d.price;
      const rawHigh = d.high ?? Math.max(open, close, price);
      const rawLow = d.low ?? Math.min(open, close, price);
      const high = isLiveActive ? Math.max(rawHigh, currentPrice) : rawHigh;
      const low = isLiveActive ? Math.min(rawLow, currentPrice) : rawLow;
      const typicalPrice = (high + low + close) / 3;
      const vol = d.volume && d.volume > 0 ? d.volume : 0;

      cumVal += typicalPrice * vol;
      cumVol += vol;

      // Double-checked zero division guard
      const vwap = cumVol > 0 ? Math.round((cumVal / cumVol) * 100) / 100 : Math.round(close * 100) / 100;

      return {
        ...d,
        price,
        open,
        close,
        high,
        low,
        vwap: selectedTimeframe === "1D" ? vwap : undefined,
      };
    });
  }, [data, selectedTimeframe, currentPrice]);

  // 2. Max Volume for bottom 20% secondary Y-axis scaling
  const maxVolume = useMemo(() => {
    if (enrichedData.length === 0) return 0;
    const vols = enrichedData.map((d) => d.volume || 0);
    return Math.max(...vols, 0);
  }, [enrichedData]);

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

  // 3. Y-Domain calculation with room for wicks and key levels
  const allHighs = enrichedData.map((d) => d.high ?? d.price);
  const allLows = enrichedData.map((d) => d.low ?? d.price);
  if (selectedTimeframe === "1D") {
    if (prevClose) {
      allHighs.push(prevClose);
      allLows.push(prevClose);
    }
    if (showLevels) {
      // Include PDH / PDL in domain if within 8% of current price to keep lines visible
      if (pdh && pdh > 0 && pdh < currentPrice * 1.08 && pdh > currentPrice * 0.92) {
        allHighs.push(pdh);
      }
      if (pdl && pdl > 0 && pdl > currentPrice * 0.92 && pdl < currentPrice * 1.08) {
        allLows.push(pdl);
      }
    }
  }
  if (currentPrice > 0) {
    allHighs.push(currentPrice);
    allLows.push(currentPrice);
  }
  const periodHigh = Math.max(...allHighs);
  const periodLow = Math.min(...allLows);

  const padding = (periodHigh - periodLow) * 0.08 || periodLow * 0.005;
  const yDomain: [number, number] = [
    Math.floor(periodLow - padding),
    Math.ceil(periodHigh + padding),
  ];

  // Latest calculated VWAP value
  const latestVwap = enrichedData.length > 0 ? enrichedData[enrichedData.length - 1].vwap : undefined;

  const handleMouseMove = (state: {
    isTooltipActive?: boolean;
    activeCoordinate?: { x: number; y: number };
    chartY?: number;
    activePayload?: Array<{ payload: StockChartPoint }>;
  } | null) => {
    if (
      !showCrosshair ||
      !state ||
      !state.isTooltipActive ||
      !state.activeCoordinate ||
      typeof state.chartY !== "number"
    ) {
      if (crosshairPos) setCrosshairPos(null);
      return;
    }
    const x = state.activeCoordinate.x;
    const y = state.chartY;
    const pt = state.activePayload?.[0]?.payload;
    const time = pt?.time ?? "";

    const range = yDomain[1] - yDomain[0];
    const estPrice = range > 0 ? yDomain[1] - ((y - 10) / 350) * range : currentPrice;

    setCrosshairPos({
      x,
      y,
      price: Math.max(yDomain[0], Math.min(yDomain[1], estPrice)),
      time,
    });
  };

  return (
    <div className="w-full bg-slate-950/60 rounded-xl border border-slate-800/80 p-5 shadow-inner">
      {/* Header: Title, Return Badge, Indicators & Beginner Info Badges */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-4">
        <div>
          <div className="flex items-center gap-2 flex-wrap">
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

            {/* Beginner Educational Info Badge for Candlesticks */}
            <EducationalInfoBadge
              id="candlesticks"
              title="Japanese Candlesticks"
              description="Green candles show buyers won (price moved UP). Red candles show sellers won (price moved DOWN). The thin sticks (wicks) mark the highest and lowest prices hit during that 5-minute interval."
              tradingTip="Long bottom wicks mean buyers aggressively defended lower prices!"
              activeKey={hoveredInfoKey}
              onHover={setHoveredInfoKey}
              badgeText="OHLC"
              badgeColor="text-emerald-400"
            />

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

        {/* Intraday Technical Overlays (VWAP, Volume, PDH/PDL) & Toggles */}
        <div className="flex flex-col sm:items-end gap-2">
          {/* Extremes & Reference Levels Row */}
          <div className="flex flex-wrap items-center gap-3 text-xs font-mono text-slate-400">
            <div className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full bg-rose-500/80"></span>
              <span className="text-slate-400">Low:</span>
              <span className="text-slate-200 font-semibold">₹{periodLow.toFixed(2)}</span>
            </div>
            <div className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full bg-emerald-500/80"></span>
              <span className="text-slate-400">High:</span>
              <span className="text-slate-200 font-semibold">₹{periodHigh.toFixed(2)}</span>
            </div>
            {latestVwap && selectedTimeframe === "1D" && (
              <div className="flex items-center gap-1">
                <span className="w-2 h-2 rounded-full bg-cyan-400"></span>
                <span className="text-cyan-400 font-semibold">VWAP:</span>
                <span className="text-white font-bold">₹{latestVwap.toFixed(2)}</span>
              </div>
            )}
          </div>

          {/* Interactive Indicator Toggles for 1D Intraday */}
          {selectedTimeframe === "1D" && (
            <div className="flex items-center gap-1.5 flex-wrap">
              {/* VWAP Toggle & Educational Info */}
              <div className="flex items-center">
                <button
                  type="button"
                  onClick={() => setShowVwap(!showVwap)}
                  className={`px-2 py-0.5 text-[11px] font-mono rounded flex items-center gap-1 transition cursor-pointer border ${
                    showVwap
                      ? "bg-cyan-500/15 text-cyan-300 border-cyan-500/40 font-semibold"
                      : "bg-slate-900/60 text-slate-500 border-slate-800 hover:text-slate-300"
                  }`}
                >
                  <span className="w-1.5 h-1.5 rounded-full bg-cyan-400"></span>
                  <span>VWAP</span>
                  <span className="text-[10px] opacity-75">{showVwap ? "✓" : ""}</span>
                </button>
                <EducationalInfoBadge
                  id="vwap"
                  title="VWAP (Volume Weighted Average Price)"
                  description="The true average price weighted by volume. Big institutions and mutual funds buy when price is ABOVE VWAP. Price trading above the cyan line indicates strong bullish intraday support."
                  tradingTip="Never buy long when price is consistently below VWAP!"
                  activeKey={hoveredInfoKey}
                  onHover={setHoveredInfoKey}
                  badgeText="Institutional Line"
                  badgeColor="text-cyan-400"
                />
              </div>

              {/* Volume Toggle & Educational Info */}
              <div className="flex items-center">
                <button
                  type="button"
                  onClick={() => setShowVolume(!showVolume)}
                  className={`px-2 py-0.5 text-[11px] font-mono rounded flex items-center gap-1 transition cursor-pointer border ${
                    showVolume
                      ? "bg-emerald-500/15 text-emerald-300 border-emerald-500/40 font-semibold"
                      : "bg-slate-900/60 text-slate-500 border-slate-800 hover:text-slate-300"
                  }`}
                >
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-400"></span>
                  <span>Volume</span>
                  <span className="text-[10px] opacity-75">{showVolume ? "✓" : ""}</span>
                </button>
                <EducationalInfoBadge
                  id="volume"
                  title="Volume Sub-Bars"
                  description="Displays the total shares traded in each 5-minute candle. Tall bars show heavy institutional orders. Short bars show calm retail trading."
                  tradingTip="A breakout above resistance is 3x more reliable if backed by a tall green volume bar!"
                  activeKey={hoveredInfoKey}
                  onHover={setHoveredInfoKey}
                  badgeText="Bottom 20%"
                  badgeColor="text-emerald-400"
                />
              </div>

              {/* PDH / PDL Levels Toggle & Educational Info */}
              <div className="flex items-center">
                <button
                  type="button"
                  onClick={() => setShowLevels(!showLevels)}
                  className={`px-2 py-0.5 text-[11px] font-mono rounded flex items-center gap-1 transition cursor-pointer border ${
                    showLevels
                      ? "bg-amber-500/15 text-amber-300 border-amber-500/40 font-semibold"
                      : "bg-slate-900/60 text-slate-500 border-slate-800 hover:text-slate-300"
                  }`}
                >
                  <span className="w-1.5 h-1.5 rounded-full bg-amber-400"></span>
                  <span>PDH / PDL</span>
                  <span className="text-[10px] opacity-75">{showLevels ? "✓" : ""}</span>
                </button>
                <EducationalInfoBadge
                  id="pdh_pdl"
                  title="PDH & PDL (Prev Day High/Low)"
                  description="PDH is yesterday's highest price (green dashed line). PDL is yesterday's lowest price (red dashed line). When price crosses above PDH, our momentum algo detects an explosive breakout!"
                  tradingTip="Stocks breaking PDH on heavy volume often trend for the entire session."
                  activeKey={hoveredInfoKey}
                  onHover={setHoveredInfoKey}
                  badgeText="Breakout Levels"
                  badgeColor="text-amber-400"
                />
              </div>

              {/* 2-Axis Reticle Crosshair Toggle */}
              <button
                type="button"
                onClick={() => setShowCrosshair(!showCrosshair)}
                title="Toggle 2-Axis Reticle Crosshair (Price & Time Badges)"
                className={`px-2 py-0.5 text-[11px] font-mono rounded flex items-center gap-1 transition cursor-pointer border ${
                  showCrosshair
                    ? "bg-sky-500/15 text-sky-300 border-sky-500/40 font-semibold"
                    : "bg-slate-900/60 text-slate-500 border-slate-800 hover:text-slate-300"
                }`}
              >
                <span>✛</span>
                <span>Crosshair</span>
                <span className="text-[10px] opacity-75">{showCrosshair ? "✓" : ""}</span>
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Dual-Axis Chart Canvas: Price Axis + Volume Axis */}
      <div className="w-full h-[330px] sm:h-[390px] select-none outline-none focus:outline-none focus:ring-0 [&_*]:outline-none [&_*]:focus:outline-none">
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart
            data={enrichedData}
            margin={{ top: 10, right: 14, left: -15, bottom: 0 }}
            style={{ outline: "none", userSelect: "none" }}
            onMouseMove={handleMouseMove}
            onMouseLeave={() => setCrosshairPos(null)}
          >
            <defs>
              <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={strokeColor} stopOpacity={0.35} />
                <stop offset="95%" stopColor={strokeColor} stopOpacity={0.0} />
              </linearGradient>

              {/* Real-time Neon Glow Filters for Bullish and Bearish Forming Candles */}
              <filter id="liveGlowBullish" x="-40%" y="-40%" width="180%" height="180%">
                <feDropShadow dx="0" dy="0" stdDeviation="3.5" floodColor="#10b981" floodOpacity="0.85" />
              </filter>
              <filter id="liveGlowBearish" x="-40%" y="-40%" width="180%" height="180%">
                <feDropShadow dx="0" dy="0" stdDeviation="3.5" floodColor="#f43f5e" floodOpacity="0.85" />
              </filter>
            </defs>

            <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" opacity={0.6} vertical={false} />

            {/* X-Axis */}
            <XAxis
              dataKey="time"
              stroke="#64748b"
              fontSize={11}
              tickLine={false}
              axisLine={{ stroke: "#334155" }}
              minTickGap={28}
            />

            {/* Primary Y-Axis (Prices) */}
            <YAxis
              yAxisId="priceAxis"
              domain={yDomain}
              stroke="#64748b"
              fontSize={11}
              tickLine={false}
              axisLine={{ stroke: "#334155" }}
              tickFormatter={(v) => `₹${v}`}
            />

            {/* Secondary Y-Axis (Volume - Scaled to Bottom 22% of Canvas) */}
            <YAxis
              yAxisId="volumeAxis"
              domain={[0, maxVolume > 0 ? maxVolume * 4.5 : 100]}
              orientation="right"
              hide={true}
            />

            {/* Interactive Tooltip with OHLC, VWAP, Volume, and Aligned Dashed Crosshair */}
            <Tooltip
              cursor={{ stroke: "#64748b", strokeWidth: 1.2, strokeDasharray: "3 3" }}
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
                    <div className="bg-slate-900/98 border border-slate-700/80 rounded-xl p-3.5 shadow-2xl backdrop-blur text-xs font-mono min-w-[215px]">
                      <div className="flex items-center justify-between border-b border-slate-800 pb-2 mb-2 gap-2">
                        <span className="text-slate-300 text-[11px] font-semibold tracking-wide">
                          {formatTooltipDateTime(pt.rawDate, pt.time)}
                        </span>
                        <span
                          className={`text-[10px] px-1.5 py-0.5 rounded font-bold ${
                            isUp ? "bg-emerald-500/20 text-emerald-400" : "bg-rose-500/20 text-rose-400"
                          }`}
                        >
                          {isUp ? "▲ BULLISH" : "▼ BEARISH"}
                        </span>
                      </div>

                      {/* OHLC Breakdown */}
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

                      {/* Price Change & VWAP */}
                      <div className="pt-1.5 border-t border-slate-800/80 space-y-1 text-[11px]">
                        <div className="flex items-center justify-between">
                          <span className="text-slate-400">Change:</span>
                          <span className={`font-bold ${candleChange >= 0 ? "text-emerald-400" : "text-rose-400"}`}>
                            {candleChange >= 0 ? "+" : ""}₹{candleChange.toFixed(2)} ({candlePct >= 0 ? "+" : ""}{candlePct.toFixed(2)}%)
                          </span>
                        </div>

                        {pt.vwap && (
                          <div className="flex items-center justify-between text-cyan-300">
                            <span className="text-slate-400">VWAP:</span>
                            <span className="font-bold">₹{pt.vwap.toFixed(2)}</span>
                          </div>
                        )}

                        {pt.volume ? (
                          <div className="flex items-center justify-between text-slate-400 text-[10px]">
                            <span>Volume:</span>
                            <span className="text-slate-200 font-semibold">{pt.volume.toLocaleString()} shares</span>
                          </div>
                        ) : null}
                      </div>
                    </div>
                  );
                }
                return null;
              }}
            />

            {/* Reference Line: Previous Day High (PDH) */}
            {selectedTimeframe === "1D" && showLevels && pdh && pdh > 0 && (
              <ReferenceLine
                yAxisId="priceAxis"
                y={pdh}
                stroke="#10b981"
                strokeDasharray="3 3"
                strokeWidth={1.2}
                label={{
                  value: `PDH ₹${pdh.toFixed(2)}`,
                  fill: "#10b981",
                  fontSize: 10,
                  position: "insideTopLeft",
                }}
              />
            )}

            {/* Reference Line: Previous Day Low (PDL) */}
            {selectedTimeframe === "1D" && showLevels && pdl && pdl > 0 && (
              <ReferenceLine
                yAxisId="priceAxis"
                y={pdl}
                stroke="#f43f5e"
                strokeDasharray="3 3"
                strokeWidth={1.2}
                label={{
                  value: `PDL ₹${pdl.toFixed(2)}`,
                  fill: "#f43f5e",
                  fontSize: 10,
                  position: "insideBottomLeft",
                }}
              />
            )}

            {/* Reference Line: Previous Close (1D) */}
            {selectedTimeframe === "1D" && prevClose && prevClose > 0 && (
              <ReferenceLine
                yAxisId="priceAxis"
                y={prevClose}
                stroke="#fbbf24"
                strokeDasharray="4 4"
                strokeWidth={1.2}
                label={{
                  value: `Prev Close ₹${prevClose.toFixed(2)}`,
                  fill: "#fbbf24",
                  fontSize: 10,
                  position: "insideTopRight",
                }}
              />
            )}

            {/* Reference Line: Period Baseline for Multi-Day (1W, 1M, etc.) */}
            {selectedTimeframe !== "1D" && baselinePrice > 0 && (
              <ReferenceLine
                yAxisId="priceAxis"
                y={baselinePrice}
                stroke="#fbbf24"
                strokeDasharray="4 4"
                strokeWidth={1.5}
                label={{
                  value: `Baseline ₹${baselinePrice.toFixed(2)}`,
                  fill: "#fbbf24",
                  fontSize: 10,
                  position: "insideTopLeft",
                }}
              />
            )}

            {/* Volume Sub-Chart Bars for Area View ONLY (In candle view, volume is drawn inside CandlestickBar to prevent Recharts bar clustering) */}
            {chartType === "area" && showVolume && (
              <Bar
                yAxisId="volumeAxis"
                dataKey="volume"
                shape={<VolumeBarShape />}
                isAnimationActive={false}
              />
            )}

            {/* Candlestick or Area Chart on Primary Price Axis */}
            {chartType === "candle" ? (
              <Bar
                yAxisId="priceAxis"
                dataKey="price"
                shape={(barProps: unknown) => (
                  <CandlestickBar
                    {...(barProps as CandlestickBarProps)}
                    minPrice={yDomain[0]}
                    maxPrice={yDomain[1]}
                    showVolume={showVolume}
                    maxVolume={maxVolume}
                    isLive={selectedTimeframe === "1D" && (barProps as { index?: number }).index === enrichedData.length - 1}
                  />
                )}
                isAnimationActive={false}
              />
            ) : (
              <Area
                yAxisId="priceAxis"
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

            {/* Dynamic Cumulative VWAP Curve (Cyan Line) */}
            {showVwap && selectedTimeframe === "1D" && (
              <Line
                yAxisId="priceAxis"
                type="monotone"
                dataKey="vwap"
                stroke="#06b6d4"
                strokeWidth={1.8}
                dot={false}
                activeDot={{ r: 4, fill: "#06b6d4", stroke: "#0f172a", strokeWidth: 1.5 }}
                isAnimationActive={false}
              />
            )}

            {/* Live Current Price Reference Line with Flush End-Aligned Badge */}
            {currentPrice > 0 && selectedTimeframe === "1D" && (
              <ReferenceLine
                yAxisId="priceAxis"
                y={currentPrice}
                shape={(lineProps: unknown) => {
                  const props = lineProps as { x1?: number; x2?: number; y1?: number };
                  const { x1, x2, y1 } = props;
                  if (typeof x1 !== "number" || typeof x2 !== "number" || typeof y1 !== "number") {
                    return <g />;
                  }
                  const lastPoint = enrichedData.length > 0 ? enrichedData[enrichedData.length - 1] : null;
                  const prevPoint = enrichedData.length > 1 ? enrichedData[enrichedData.length - 2] : null;
                  const candleOpen = lastPoint?.open ?? prevPoint?.close ?? prevPoint?.price ?? baselinePrice;

                  // Active interval direction: green when bullish/up, red when bearish/down
                  const isFormingUp = currentPrice !== candleOpen
                    ? currentPrice > candleOpen
                    : lastTickDir === "up" || (lastTickDir === null && currentPrice >= baselinePrice);

                  const liveColor = isFormingUp ? "#10b981" : "#f43f5e";

                  return (
                    <g className="pointer-events-none">
                      {/* Dotted horizontal tracking line spanning completely from left to right */}
                      <line
                        x1={x1}
                        y1={y1}
                        x2={x2}
                        y2={y1}
                        stroke={liveColor}
                        strokeDasharray="2 3"
                        strokeWidth={1.2}
                        opacity={0.85}
                      />
                      {/* Live beacon dot with smooth heartbeat pulsing animation at the tip */}
                      <g transform={`translate(${x2}, ${y1})`}>
                        {/* Solid center dot */}
                        <circle cx={0} cy={0} r={3.5} fill={liveColor} />
                        {/* Outer pulsing ring with heartbeat rhythm */}
                        <circle cx={0} cy={0} r={6.5} fill="none" stroke={liveColor} strokeWidth={1.5} opacity={0.85}>
                          <animate attributeName="r" values="3.5;8.5;3.5" dur="1.8s" repeatCount="indefinite" />
                          <animate attributeName="opacity" values="0.9;0.05;0.9" dur="1.8s" repeatCount="indefinite" />
                        </circle>
                        {/* Soft ambient glow halo */}
                        <circle cx={0} cy={0} r={12} fill={liveColor} opacity={0.15}>
                          <animate attributeName="r" values="4;12;4" dur="1.8s" repeatCount="indefinite" />
                          <animate attributeName="opacity" values="0.25;0.0;0.25" dur="1.8s" repeatCount="indefinite" />
                        </circle>
                      </g>
                    </g>
                  );
                }}
              />
            )}

            {/* 2-Axis Reticle Crosshair with Floating Y-Price & X-Time Badges */}
            {showCrosshair && crosshairPos && (
              <Customized
                component={(chartProps: { offset?: { top: number; left: number; width: number; height: number }; width?: number }) => {
                  const offset = chartProps.offset || { top: 10, left: 0, width: chartProps.width || 800, height: 350 };
                  const range = yDomain[1] - yDomain[0];
                  const exactPrice = range > 0
                    ? yDomain[1] - ((crosshairPos.y - offset.top) / offset.height) * range
                    : crosshairPos.price;
                  const clampedPrice = Math.max(yDomain[0], Math.min(yDomain[1], exactPrice));
                  const plotLeft = offset.left;
                  const plotRight = offset.left + offset.width;
                  const plotBottom = offset.top + offset.height;

                  return (
                    <g className="pointer-events-none">
                      {/* Horizontal Reticle Dashed Line */}
                      <line
                        x1={plotLeft}
                        y1={crosshairPos.y}
                        x2={plotRight}
                        y2={crosshairPos.y}
                        stroke="#64748b"
                        strokeWidth={1}
                        strokeDasharray="3 3"
                        opacity={0.8}
                      />
                      {/* Floating Y-Axis Price Badge Pill */}
                      <g transform={`translate(${plotRight - 4}, ${crosshairPos.y})`}>
                        <rect
                          x={-60}
                          y={-10}
                          width={60}
                          height={20}
                          rx={4}
                          fill="#090d16"
                          stroke="#06b6d4"
                          strokeWidth={1}
                          opacity={0.95}
                        />
                        <text
                          x={-30}
                          y={4}
                          textAnchor="middle"
                          fill="#38bdf8"
                          fontSize={10}
                          fontFamily="monospace"
                          fontWeight="bold"
                        >
                          ₹{clampedPrice.toFixed(2)}
                        </text>
                      </g>
                      {/* Floating X-Axis Time Badge Pill */}
                      {crosshairPos.time && (
                        <g transform={`translate(${crosshairPos.x}, ${plotBottom})`}>
                          <rect
                            x={-28}
                            y={2}
                            width={56}
                            height={18}
                            rx={4}
                            fill="#090d16"
                            stroke="#64748b"
                            strokeWidth={1}
                            opacity={0.95}
                          />
                          <text
                            x={0}
                            y={14}
                            textAnchor="middle"
                            fill="#e2e8f0"
                            fontSize={10}
                            fontFamily="monospace"
                            fontWeight="bold"
                          >
                            {crosshairPos.time}
                          </text>
                        </g>
                      )}
                    </g>
                  );
                }}
              />
            )}
          </ComposedChart>
        </ResponsiveContainer>
      </div>

      {/* Groww-style Timeframe Selector Bar, Intraday Interval Pills & Candlestick Toggle */}
      <div className="flex flex-wrap items-center justify-between gap-3 pt-3 mt-2 border-t border-slate-800/80">
        <div className="flex flex-wrap items-center gap-2">
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

          {/* Intraday Resolution Pills (1m / 3m / 5m / 15m) when in 1D view */}
          {selectedTimeframe === "1D" && (
            <div className="flex items-center gap-1 bg-slate-900/90 p-0.5 rounded-lg border border-slate-800">
              <span className="text-[10px] font-mono text-slate-500 px-1.5 hidden sm:inline">Interval:</span>
              {INTRADAY_INTERVALS.map((intvl) => {
                const isSelected = (selectedInterval || "5m") === intvl.value;
                return (
                  <button
                    key={intvl.value}
                    onClick={() => onIntervalChange?.(intvl.value)}
                    className={`px-2 py-0.5 text-xs font-mono rounded transition-all cursor-pointer ${
                      isSelected
                        ? "bg-emerald-500/25 text-emerald-300 font-bold border border-emerald-500/40 shadow-xs"
                        : "text-slate-400 hover:text-slate-200"
                    }`}
                  >
                    {intvl.label}
                  </button>
                );
              })}
            </div>
          )}
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
