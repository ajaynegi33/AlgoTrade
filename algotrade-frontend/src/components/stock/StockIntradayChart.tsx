"use client";

import React, { useState, useMemo } from "react";
import {
  ResponsiveContainer,
  ComposedChart,
  Bar,
  Line,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ReferenceLine,
  CartesianGrid,
} from "recharts";
import { SessionBarData, ActivePositionData } from "@/lib/types";

interface StockIntradayChartProps {
  bars: SessionBarData[];
  vwap: number;
  orh?: number;
  orl?: number;
  hasOR15m?: boolean;
  dayHigh: number;
  dayLow: number;
  changePercent?: number;
  activePosition?: ActivePositionData | null;
}

interface CustomTooltipProps {
  active?: boolean;
  payload?: Array<{
    dataKey?: string;
    value?: number | [number, number];
    payload?: SessionBarData & {
      vwap?: number;
      ema9?: number;
      candleRange?: [number, number];
    };
  }>;
  label?: string;
}

function CustomChartTooltip({ active, payload, label }: CustomTooltipProps) {
  if (!active || !payload || !payload.length) return null;
  const bar = payload[0]?.payload;
  if (!bar) return null;

  const vwapVal = bar.vwap;
  const ema9Val = bar.ema9;
  const isUp = bar.close >= bar.open;

  return (
    <div className="bg-white/95 backdrop-blur-xs border border-slate-200/90 rounded-2xl p-3 shadow-xl font-mono text-xs space-y-1.5 z-50 pointer-events-none min-w-[210px]">
      <div className="flex items-center justify-between gap-4 border-b border-slate-100 pb-1 text-slate-900 font-bold">
        <span>{label} IST</span>
        <span
          className={`text-[10px] px-1.5 py-0.5 rounded font-bold ${
            isUp
              ? "bg-emerald-50 text-emerald-800 border border-emerald-200"
              : "bg-rose-50 text-rose-800 border border-rose-200"
          }`}
        >
          {isUp ? "BULL 5M" : "BEAR 5M"}
        </span>
      </div>

      <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-[11px]">
        <div className="text-slate-500">
          Open: <strong className="text-slate-900">₹{bar.open ? bar.open.toFixed(2) : "--"}</strong>
        </div>
        <div className="text-slate-500">
          High: <strong className="text-emerald-700">₹{bar.high ? bar.high.toFixed(2) : "--"}</strong>
        </div>
        <div className="text-slate-500">
          Low: <strong className="text-rose-700">₹{bar.low ? bar.low.toFixed(2) : "--"}</strong>
        </div>
        <div className="text-slate-500">
          Close: <strong className={isUp ? "text-emerald-700 font-black" : "text-rose-700 font-black"}>
            ₹{bar.close ? bar.close.toFixed(2) : "--"}
          </strong>
        </div>
      </div>

      <div className="pt-1.5 border-t border-slate-100 space-y-1 text-[10px]">
        <div className="flex items-center justify-between">
          <span className="text-amber-700 font-semibold">
            VWAP: {vwapVal ? `₹${vwapVal.toFixed(2)}` : "--"}
          </span>
          <span className="text-cyan-700 font-semibold">
            9 EMA: {ema9Val ? `₹${ema9Val.toFixed(2)}` : "--"}
          </span>
        </div>
        <div className="flex items-center justify-between text-slate-500">
          <span>Volume: {bar.volume ? bar.volume.toLocaleString("en-IN") : 0}</span>
          <span>Spread: ₹{(bar.high - bar.low).toFixed(2)}</span>
        </div>
      </div>
    </div>
  );
}

/**
 * Custom Candlestick SVG Bar Renderer for Recharts.
 * Calculates wick lines and body rectangle with mathematical precision from [low, high] bounds.
 */
interface CandlestickBarProps {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  payload?: SessionBarData & { candleRange?: [number, number] };
}

function CandlestickBarShape(props: CandlestickBarProps) {
  const { x = 0, y = 0, width = 0, height = 0, payload } = props;
  if (!payload || payload.open <= 0 || payload.close <= 0) return null;

  const { open, close, high, low } = payload;
  const isUp = close >= open;
  const color = isUp ? "#10b981" : "#f43f5e";
  const bodyFill = isUp ? "#10b981" : "#f43f5e";

  const totalRange = Math.max(0.001, high - low);
  const ratio = height / totalRange;

  const wickX = x + width / 2;
  const bodyTopPrice = Math.max(open, close);
  const bodyBottomPrice = Math.min(open, close);

  const bodyY = y + (high - bodyTopPrice) * ratio;
  const bodyHeight = Math.max(2, (bodyTopPrice - bodyBottomPrice) * ratio);
  const bodyWidth = Math.max(2, Math.min(14, width * 0.75));
  const bodyX = wickX - bodyWidth / 2;

  return (
    <g className="transition-opacity hover:opacity-80">
      {/* High-Low Wick Line */}
      <line
        x1={wickX}
        y1={y}
        x2={wickX}
        y2={y + height}
        stroke={color}
        strokeWidth={1.5}
      />
      {/* Candle Body */}
      <rect
        x={bodyX}
        y={bodyY}
        width={bodyWidth}
        height={bodyHeight}
        fill={bodyFill}
        stroke={color}
        strokeWidth={1}
        rx={1}
      />
    </g>
  );
}

/**
 * Calculates 9 Exponential Moving Average over completed 5-minute bars.
 * Seeds with first available close. Alpha = 2 / (9 + 1) = 0.20
 */
function computeEMA9(bars: SessionBarData[]): (number | undefined)[] {
  const k = 2 / (9 + 1);
  const result: (number | undefined)[] = [];
  let currentEMA: number | null = null;

  for (let i = 0; i < bars.length; i++) {
    const close = bars[i].close;
    if (close <= 0) {
      result.push(undefined);
      continue;
    }
    if (currentEMA === null) {
      currentEMA = close;
    } else {
      currentEMA = close * k + currentEMA * (1 - k);
    }
    result.push(Number(currentEMA.toFixed(2)));
  }
  return result;
}

export function StockIntradayChart({
  bars,
  vwap,
  orh,
  orl,
  hasOR15m,
  dayHigh,
  dayLow,
  changePercent = 0,
  activePosition,
}: StockIntradayChartProps) {
  const isMounted = React.useSyncExternalStore(
    () => () => {},
    () => true,
    () => false
  );

  const [chartMode, setChartMode] = useState<"CANDLE" | "AREA">("CANDLE");
  const [showVolume, setShowVolume] = useState(true);
  const [showIndicators, setShowIndicators] = useState(true);

  const isUp = changePercent >= 0;

  // Filter valid bars with actual data
  const validBars = useMemo(() => {
    return bars.filter((b) => b.open > 0 || b.close > 0 || b.high > 0 || b.volume > 0);
  }, [bars]);

  // Compute 9 EMA series
  const ema9Series = useMemo(() => computeEMA9(validBars), [validBars]);

  // Max volume for dual-axis scaling
  const maxVolume = useMemo(() => {
    if (validBars.length === 0) return 1000;
    return Math.max(...validBars.map((b) => b.volume || 0), 100);
  }, [validBars]);

  // Prepare chart data items with candle ranges, vwap, ema9, and volume color
  const chartData = useMemo(() => {
    return validBars.map((b, idx) => ({
      ...b,
      vwap: vwap > 0 ? Number(vwap.toFixed(2)) : undefined,
      ema9: ema9Series[idx],
      candleRange: [b.low, b.high] as [number, number],
      volumeColor: b.close >= b.open ? "#10b981" : "#f43f5e",
    }));
  }, [validBars, vwap, ema9Series]);

  // Compute price boundaries with padding
  const priceStats = useMemo(() => {
    if (validBars.length === 0) {
      return {
        firstClose: dayLow || 100,
        lastClose: dayHigh || 100,
        high: dayHigh || 105,
        low: dayLow || 95,
      };
    }
    const closes = validBars.map((b) => b.close);
    const highs = validBars.map((b) => b.high).filter((h) => h > 0);
    const lows = validBars.map((b) => b.low).filter((l) => l > 0);
    return {
      firstClose: closes[0] || dayLow,
      lastClose: closes[closes.length - 1] || dayHigh,
      high: highs.length > 0 ? Math.max(...highs, dayHigh || 0) : dayHigh,
      low: lows.length > 0 ? Math.min(...lows, dayLow || 999999) : dayLow,
    };
  }, [validBars, dayHigh, dayLow]);

  // In-Trade Active Levels
  const tradeLevels = useMemo(() => {
    if (!activePosition) return null;
    const isGear4 = activePosition.current_gear === 4 || activePosition.mod_count >= 24;
    const effectiveStop = isGear4 && activePosition.shadow_stop > 0 ? activePosition.shadow_stop : activePosition.current_stop;
    const target1_5R = activePosition.entry_price + 1.5 * activePosition.one_r;

    return {
      entry: activePosition.entry_price,
      currentStop: effectiveStop,
      breakeven: activePosition.breakeven_stop,
      target1_5R,
      isGear4,
    };
  }, [activePosition]);

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs font-mono w-full space-y-4">
      {/* Top Header & Chart Controls */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2 sm:gap-3">
          <span className="text-xs font-black text-slate-900 uppercase tracking-wider">
            5M Intraday Equity Terminal
          </span>
          <span className="text-[10px] px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 border border-slate-200">
            09:15 - 15:30 IST
          </span>
          {tradeLevels && (
            <span className="text-[10px] font-bold px-2.5 py-0.5 rounded-full bg-emerald-50 text-emerald-800 border border-emerald-300 animate-pulse">
              IN ACTIVE TRADE
            </span>
          )}
          {validBars.length > 0 && (
            <span className="text-[10px] text-slate-400">
              {validBars.length} bars
            </span>
          )}
        </div>

        {/* Legend & Toggle Controls */}
        <div className="flex flex-wrap items-center gap-2 text-xs">
          {/* Chart Mode Toggle */}
          <div className="inline-flex rounded-xl bg-slate-100 p-0.5 border border-slate-200">
            <button
              type="button"
              onClick={() => setChartMode("CANDLE")}
              className={`px-2.5 py-1 rounded-lg text-[10px] font-bold transition-all cursor-pointer ${
                chartMode === "CANDLE"
                  ? "bg-white text-slate-900 shadow-2xs"
                  : "text-slate-500 hover:text-slate-800"
              }`}
            >
              Candles
            </button>
            <button
              type="button"
              onClick={() => setChartMode("AREA")}
              className={`px-2.5 py-1 rounded-lg text-[10px] font-bold transition-all cursor-pointer ${
                chartMode === "AREA"
                  ? "bg-white text-slate-900 shadow-2xs"
                  : "text-slate-500 hover:text-slate-800"
              }`}
            >
              Area
            </button>
          </div>

          {/* Indicator Pills */}
          <div className="flex items-center gap-2 bg-slate-50 px-2.5 py-1 rounded-xl border border-slate-200 text-[10px]">
            {/* VWAP */}
            <span className="flex items-center gap-1 text-amber-700 font-bold">
              <span className="w-2.5 h-0.5 bg-amber-500 rounded-full" />
              VWAP: ₹{vwap > 0 ? vwap.toFixed(2) : "--"}
            </span>

            {/* 9 EMA */}
            <span className="flex items-center gap-1 text-cyan-700 font-bold">
              <span className="w-2.5 h-0.5 bg-cyan-500 rounded-full" />
              9 EMA: ₹{ema9Series.length > 0 && ema9Series[ema9Series.length - 1] ? ema9Series[ema9Series.length - 1]?.toFixed(2) : "--"}
            </span>
          </div>

          {/* Indicators Toggle */}
          <button
            type="button"
            onClick={() => setShowIndicators(!showIndicators)}
            className={`text-[10px] font-bold px-2 py-1 rounded-xl border transition-colors cursor-pointer ${
              showIndicators
                ? "bg-slate-100 border-slate-300 text-slate-800"
                : "bg-white border-slate-200 text-slate-400"
            }`}
          >
            Indicators
          </button>

          {/* Volume Toggle */}
          <button
            type="button"
            onClick={() => setShowVolume(!showVolume)}
            className={`text-[10px] font-bold px-2 py-1 rounded-xl border transition-colors cursor-pointer ${
              showVolume
                ? "bg-slate-100 border-slate-300 text-slate-800"
                : "bg-white border-slate-200 text-slate-400"
            }`}
          >
            Volume
          </button>
        </div>
      </div>

      {/* Main Chart Container */}
      {!isMounted ? (
        <div className="w-full h-[380px] bg-slate-50 rounded-2xl animate-pulse flex items-center justify-center text-slate-400 text-xs">
          Loading Intraday Chart...
        </div>
      ) : chartData.length === 0 ? (
        <div className="w-full h-[380px] bg-slate-50/60 border border-dashed border-slate-200 rounded-2xl flex flex-col items-center justify-center p-6 text-center">
          <div className="w-10 h-10 rounded-xl bg-slate-100 text-slate-400 flex items-center justify-center text-lg mb-2">
            📈
          </div>
          <p className="text-xs font-bold text-slate-700 mb-1">
            Waiting for Live 5m Bar Formation
          </p>
          <p className="text-[11px] text-slate-400 max-w-sm">
            Intraday 5-minute OHLCV candles will plot automatically between 09:15 AM and 03:30 PM IST.
          </p>
        </div>
      ) : (
        <div className="w-full h-[380px] sm:h-[400px]">
          <ResponsiveContainer width="100%" height="100%">
            <ComposedChart
              data={chartData}
              margin={{ top: 12, right: 12, left: 0, bottom: 0 }}
            >
              <defs>
                <linearGradient id="priceGradientUp" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#10b981" stopOpacity={0.25} />
                  <stop offset="95%" stopColor="#10b981" stopOpacity={0.0} />
                </linearGradient>
                <linearGradient id="priceGradientDown" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#f43f5e" stopOpacity={0.25} />
                  <stop offset="95%" stopColor="#f43f5e" stopOpacity={0.0} />
                </linearGradient>
              </defs>

              <CartesianGrid strokeDasharray="3 3" stroke="#f1f5f9" vertical={false} />

              <XAxis
                dataKey="time"
                stroke="#94a3b8"
                tick={{ fontSize: 11, fontFamily: "monospace" }}
                tickLine={false}
                axisLine={{ stroke: "#e2e8f0" }}
                minTickGap={25}
              />

              {/* Price Axis (Right) */}
              <YAxis
                yAxisId="price"
                orientation="right"
                domain={["auto", "auto"]}
                stroke="#94a3b8"
                tick={{ fontSize: 11, fontFamily: "monospace" }}
                tickLine={false}
                axisLine={{ stroke: "#e2e8f0" }}
                tickFormatter={(val: number) => `₹${val.toFixed(1)}`}
              />

              {/* Volume Axis (Left, hidden, scaled 4x so volume sits in bottom quadrant) */}
              <YAxis
                yAxisId="volume"
                orientation="left"
                domain={[0, maxVolume * 4]}
                hide
              />

              <Tooltip content={<CustomChartTooltip />} />

              {/* Volume Bars */}
              {showVolume && (
                <Bar
                  yAxisId="volume"
                  dataKey="volume"
                  fill="#cbd5e1"
                  radius={[2, 2, 0, 0]}
                  maxBarSize={14}
                  isAnimationActive={false}
                  opacity={0.45}
                />
              )}

              {/* Opening Range Reference Lines (ORH & ORL) */}
              {showIndicators && hasOR15m && orh && orh > 0 && (
                <ReferenceLine
                  yAxisId="price"
                  y={orh}
                  stroke="#10b981"
                  strokeDasharray="4 4"
                  strokeWidth={1.5}
                  label={{
                    value: `ORH: ₹${orh.toFixed(2)}`,
                    position: "insideTopLeft",
                    fill: "#059669",
                    fontSize: 10,
                    fontWeight: 700,
                  }}
                />
              )}

              {showIndicators && hasOR15m && orl && orl > 0 && (
                <ReferenceLine
                  yAxisId="price"
                  y={orl}
                  stroke="#f43f5e"
                  strokeDasharray="4 4"
                  strokeWidth={1.5}
                  label={{
                    value: `ORL: ₹${orl.toFixed(2)}`,
                    position: "insideBottomLeft",
                    fill: "#e11d48",
                    fontSize: 10,
                    fontWeight: 700,
                  }}
                />
              )}

              {/* =====================================================================
                  ACTIVE IN-TRADE EXECUTION LEVEL OVERLAYS
                  ===================================================================== */}
              {tradeLevels && (
                <>
                  {/* Entry Price */}
                  <ReferenceLine
                    yAxisId="price"
                    y={tradeLevels.entry}
                    stroke="#0284c7"
                    strokeWidth={1.8}
                    label={{
                      value: `ENTRY: ₹${tradeLevels.entry.toFixed(2)}`,
                      position: "insideTopRight",
                      fill: "#0284c7",
                      fontSize: 10,
                      fontWeight: 800,
                    }}
                  />

                  {/* Active Stop Loss */}
                  <ReferenceLine
                    yAxisId="price"
                    y={tradeLevels.currentStop}
                    stroke="#e11d48"
                    strokeWidth={1.8}
                    label={{
                      value: `${tradeLevels.isGear4 ? "RAM SHADOW SL" : "ACTIVE SL"}: ₹${tradeLevels.currentStop.toFixed(2)}`,
                      position: "insideBottomRight",
                      fill: "#e11d48",
                      fontSize: 10,
                      fontWeight: 800,
                    }}
                  />

                  {/* Breakeven Floor */}
                  {tradeLevels.breakeven > 0 && (
                    <ReferenceLine
                      yAxisId="price"
                      y={tradeLevels.breakeven}
                      stroke="#059669"
                      strokeDasharray="3 3"
                      strokeWidth={1.4}
                      label={{
                        value: `BE: ₹${tradeLevels.breakeven.toFixed(2)}`,
                        position: "insideLeft",
                        fill: "#059669",
                        fontSize: 10,
                        fontWeight: 700,
                      }}
                    />
                  )}

                  {/* 1.5R Target Scale-Out */}
                  {tradeLevels.target1_5R > 0 && (
                    <ReferenceLine
                      yAxisId="price"
                      y={tradeLevels.target1_5R}
                      stroke="#6366f1"
                      strokeDasharray="4 3"
                      strokeWidth={1.5}
                      label={{
                        value: `1.5R TARGET: ₹${tradeLevels.target1_5R.toFixed(2)}`,
                        position: "insideTopLeft",
                        fill: "#4f46e5",
                        fontSize: 10,
                        fontWeight: 700,
                      }}
                    />
                  )}
                </>
              )}

              {/* 9 Exponential Moving Average (Cyan Line) */}
              {showIndicators && (
                <Line
                  yAxisId="price"
                  type="monotone"
                  dataKey="ema9"
                  stroke="#06b6d4"
                  strokeWidth={1.8}
                  dot={false}
                  isAnimationActive={false}
                  name="9 EMA"
                />
              )}

              {/* Session VWAP (Amber Line) */}
              {showIndicators && vwap > 0 && (
                <Line
                  yAxisId="price"
                  type="monotone"
                  dataKey="vwap"
                  stroke="#f59e0b"
                  strokeWidth={1.8}
                  strokeDasharray="4 3"
                  dot={false}
                  isAnimationActive={false}
                  name="VWAP"
                />
              )}

              {/* Price Render Mode: Candlesticks vs Area Curve */}
              {chartMode === "CANDLE" ? (
                <Bar
                  yAxisId="price"
                  dataKey="candleRange"
                  shape={<CandlestickBarShape />}
                  isAnimationActive={false}
                />
              ) : (
                <Area
                  yAxisId="price"
                  type="monotone"
                  dataKey="close"
                  stroke={isUp ? "#10b981" : "#f43f5e"}
                  strokeWidth={2}
                  fill={isUp ? "url(#priceGradientUp)" : "url(#priceGradientDown)"}
                  dot={false}
                  activeDot={{
                    r: 5,
                    stroke: isUp ? "#10b981" : "#f43f5e",
                    strokeWidth: 2,
                    fill: "#ffffff",
                  }}
                  name="Close"
                />
              )}
            </ComposedChart>
          </ResponsiveContainer>
        </div>
      )}

      {/* Footer Summary */}
      <div className="pt-3 border-t border-slate-100 flex flex-wrap items-center justify-between gap-3 text-xs text-slate-500">
        <div className="flex items-center gap-3">
          <span>
            Day Low: <strong className="text-slate-800">₹{priceStats.low.toFixed(2)}</strong>
          </span>
          <span>•</span>
          <span>
            Day High: <strong className="text-slate-800">₹{priceStats.high.toFixed(2)}</strong>
          </span>
          {hasOR15m && orh && orl && (
            <>
              <span>•</span>
              <span className="text-emerald-700 font-semibold">
                ORH: ₹{orh.toFixed(2)}
              </span>
              <span>•</span>
              <span className="text-rose-700 font-semibold">
                ORL: ₹{orl.toFixed(2)}
              </span>
            </>
          )}
        </div>

        <div className="flex items-center gap-2 text-[11px] text-slate-400">
          <span>5m High-Precision OHLCV &bull; 9 EMA Confluence &bull; Institutional VWAP</span>
        </div>
      </div>
    </div>
  );
}
