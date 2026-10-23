"use client";

import React, { useState, useMemo } from "react";
import {
  ResponsiveContainer,
  ComposedChart,
  Area,
  Line,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  ReferenceLine,
  CartesianGrid,
} from "recharts";
import { SessionBarData } from "@/lib/types";

interface StockIntradayChartProps {
  bars: SessionBarData[];
  vwap: number;
  orh?: number;
  orl?: number;
  hasOR15m?: boolean;
  dayHigh: number;
  dayLow: number;
  changePercent?: number;
}

interface CustomTooltipProps {
  active?: boolean;
  payload?: Array<{
    dataKey?: string;
    value?: number;
    payload?: SessionBarData & { vwap?: number };
  }>;
  label?: string;
}

function CustomChartTooltip({ active, payload, label }: CustomTooltipProps) {
  if (!active || !payload || !payload.length) return null;
  const bar = payload[0]?.payload;
  if (!bar) return null;

  const vwapVal = payload.find((p) => p.dataKey === "vwap")?.value;

  return (
    <div className="bg-white border border-slate-200/90 rounded-2xl p-3 shadow-xl font-mono text-xs space-y-1.5 z-50 pointer-events-none min-w-[200px]">
      <div className="flex items-center justify-between gap-4 border-b border-slate-100 pb-1 text-slate-900 font-bold">
        <span>{label} IST</span>
        <span className="text-[10px] px-1.5 py-0.5 rounded bg-slate-100 text-slate-600 font-medium">
          5m Bar
        </span>
      </div>

      <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-[11px] text-slate-600">
        <div>
          Open: <strong className="text-slate-800">₹{bar.open ? bar.open.toFixed(2) : "--"}</strong>
        </div>
        <div>
          High: <strong className="text-emerald-700">₹{bar.high ? bar.high.toFixed(2) : "--"}</strong>
        </div>
        <div>
          Low: <strong className="text-rose-700">₹{bar.low ? bar.low.toFixed(2) : "--"}</strong>
        </div>
        <div>
          Close: <strong className="text-slate-950 font-bold">₹{bar.close ? bar.close.toFixed(2) : "--"}</strong>
        </div>
      </div>

      <div className="pt-1.5 border-t border-slate-100 flex items-center justify-between gap-3 text-[10px]">
        <span className="text-amber-600 font-semibold">
          VWAP: {vwapVal ? `₹${vwapVal.toFixed(2)}` : "--"}
        </span>
        <span className="text-slate-500 font-semibold">
          Vol: {bar.volume ? bar.volume.toLocaleString("en-IN") : 0}
        </span>
      </div>
    </div>
  );
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
}: StockIntradayChartProps) {
  const isMounted = React.useSyncExternalStore(
    () => () => {},
    () => true,
    () => false
  );
  const [showVolume, setShowVolume] = useState(true);
  const [showAnchors, setShowAnchors] = useState(true);

  const isUp = changePercent >= 0;

  // Filter valid bars with actual data
  const validBars = useMemo(() => {
    return bars.filter((b) => b.open > 0 || b.close > 0 || b.high > 0 || b.volume > 0);
  }, [bars]);

  // Max volume for dual-axis scaling
  const maxVolume = useMemo(() => {
    if (validBars.length === 0) return 1000;
    return Math.max(...validBars.map((b) => b.volume || 0), 100);
  }, [validBars]);

  // Prepare recharts data
  const chartData = useMemo(() => {
    return validBars.map((b) => ({
      ...b,
      vwap: vwap > 0 ? Number(vwap.toFixed(2)) : undefined,
    }));
  }, [validBars, vwap]);

  // Compute price boundaries
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

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs font-mono w-full">
      {/* Top Header & Chart Controls */}
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <div className="flex items-center gap-3">
          <span className="text-xs font-bold text-slate-900 uppercase tracking-wider">
            5M Intraday Session Chart
          </span>
          <span className="text-[10px] px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 border border-slate-200">
            09:15 - 15:30 IST
          </span>
          {validBars.length > 0 && (
            <span className="text-[10px] text-slate-400">
              {validBars.length} bars recorded
            </span>
          )}
        </div>

        {/* Legend & Toggle Controls */}
        <div className="flex items-center gap-3 text-xs">
          {/* VWAP indicator */}
          <div className="flex items-center gap-1.5 text-[11px] text-amber-700">
            <span className="w-3 h-0.5 bg-amber-500 rounded-full" />
            <span>VWAP: ₹{vwap > 0 ? vwap.toFixed(2) : "--"}</span>
          </div>

          {/* Opening Range Badges */}
          {hasOR15m && orh && orl && (
            <button
              onClick={() => setShowAnchors(!showAnchors)}
              className={`flex items-center gap-1 text-[11px] px-2 py-0.5 rounded-lg border transition-colors cursor-pointer ${
                showAnchors
                  ? "bg-slate-100 border-slate-300 text-slate-800"
                  : "bg-white border-slate-200 text-slate-400"
              }`}
            >
              <span>15m Range</span>
            </button>
          )}

          {/* Volume Toggle */}
          <button
            onClick={() => setShowVolume(!showVolume)}
            className={`flex items-center gap-1 text-[11px] px-2 py-0.5 rounded-lg border transition-colors cursor-pointer ${
              showVolume
                ? "bg-slate-100 border-slate-300 text-slate-800"
                : "bg-white border-slate-200 text-slate-400"
            }`}
          >
            <span>Volume</span>
          </button>
        </div>
      </div>

      {/* Main Recharts Area Container */}
      {!isMounted ? (
        <div className="w-full h-[360px] bg-slate-50 rounded-2xl animate-pulse flex items-center justify-center text-slate-400 text-xs">
          Loading Intraday Chart...
        </div>
      ) : chartData.length === 0 ? (
        <div className="w-full h-[360px] bg-slate-50/60 border border-dashed border-slate-200 rounded-2xl flex flex-col items-center justify-center p-6 text-center">
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
        <div className="w-full h-[360px] sm:h-[380px]">
          <ResponsiveContainer width="100%" height="100%">
            <ComposedChart
              data={chartData}
              margin={{ top: 10, right: 10, left: 0, bottom: 0 }}
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
                <linearGradient id="volumeGradient" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#94a3b8" stopOpacity={0.5} />
                  <stop offset="100%" stopColor="#cbd5e1" stopOpacity={0.15} />
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
                  fill="url(#volumeGradient)"
                  radius={[3, 3, 0, 0]}
                  maxBarSize={16}
                  isAnimationActive={false}
                />
              )}

              {/* 15m Opening Range Anchors */}
              {showAnchors && hasOR15m && orh && orh > 0 && (
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

              {showAnchors && hasOR15m && orl && orl > 0 && (
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

              {/* VWAP Golden Reference Line */}
              {vwap > 0 && (
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

              {/* Close Price Area Curve */}
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
            </ComposedChart>
          </ResponsiveContainer>
        </div>
      )}

      {/* Intraday Price Extremes Summary Footer */}
      <div className="mt-4 pt-3 border-t border-slate-100 flex flex-wrap items-center justify-between gap-3 text-xs text-slate-500">
        <div className="flex items-center gap-4">
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
          <span>Interactive 5m Candlestick / Area Model</span>
        </div>
      </div>
    </div>
  );
}
