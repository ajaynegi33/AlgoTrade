"use client";

import React from "react";
import {
  ResponsiveContainer,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ReferenceLine,
  CartesianGrid,
} from "recharts";
import { StockChartPoint } from "../types";
import { SkeletonChart } from "../common/Skeleton";

interface StockPriceChartProps {
  symbol: string;
  data: StockChartPoint[];
  prevClose?: number;
  currentPrice: number;
  loading?: boolean;
}

export default function StockPriceChart({
  symbol,
  data,
  prevClose,
  currentPrice,
  loading = false,
}: StockPriceChartProps) {
  // If loading and no previous data exists, show the premium skeleton loader
  if (loading && (!data || data.length === 0)) {
    return <SkeletonChart height="h-[380px] sm:h-[420px]" />;
  }

  // If not loading and no data available
  if (!data || data.length === 0) {
    return (
      <div className="w-full h-[380px] sm:h-[420px] flex flex-col items-center justify-center bg-slate-950/40 rounded-xl border border-slate-800 text-slate-500 text-xs">
        <span className="font-mono">No intraday chart points available for {symbol}</span>
        <span className="text-[10px] text-slate-600 mt-1">Live data resumes during NSE trading hours (09:15 - 15:30)</span>
      </div>
    );
  }

  const isPositive = prevClose ? currentPrice >= prevClose : true;
  const strokeColor = isPositive ? "#10b981" : "#f43f5e"; // Emerald vs Rose
  const gradientId = `colorPrice_${symbol.replace(/[^a-zA-Z0-9]/g, "")}`;

  // Calculate min & max bounds with gentle padding
  const prices = data.map((d) => d.price);
  if (prevClose) prices.push(prevClose);
  const minPrice = Math.min(...prices);
  const maxPrice = Math.max(...prices);
  const padding = (maxPrice - minPrice) * 0.1 || minPrice * 0.005;
  const yDomain: [number, number] = [
    Math.floor(minPrice - padding),
    Math.ceil(maxPrice + padding),
  ];

  return (
    <div className="w-full bg-slate-950/60 rounded-xl border border-slate-800/80 p-5 shadow-inner">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 mb-4">
        <div className="flex items-center gap-2">
          <span className="text-xs font-bold text-slate-300 uppercase tracking-wider font-mono">
            {symbol} Intraday Session Trajectory
          </span>
          <span
            className={`text-[10px] px-2 py-0.5 rounded font-mono font-bold ${
              isPositive ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30" : "bg-rose-500/20 text-rose-400 border border-rose-500/30"
            }`}
          >
            {isPositive ? "▲ BULLISH" : "▼ BEARISH"}
          </span>
        </div>
        <div className="flex items-center gap-4 text-xs font-mono text-slate-400">
          {prevClose && (
            <span className="flex items-center gap-1.5">
              <span className="w-2.5 h-0.5 bg-amber-400 inline-block rounded"></span>
              <span>Prev Close: ₹{prevClose.toFixed(2)}</span>
            </span>
          )}
          <span className="text-white font-bold text-sm">LTP: ₹{currentPrice.toFixed(2)}</span>
        </div>
      </div>

      {/* Increased Chart Height (380px mobile, 420px desktop) */}
      <div className="w-full h-[320px] sm:h-[380px]">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={data} margin={{ top: 10, right: 10, left: -15, bottom: 0 }}>
            <defs>
              <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={strokeColor} stopOpacity={0.4} />
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
                  return (
                    <div className="bg-slate-900/95 border border-slate-700/80 rounded-xl p-3 shadow-2xl backdrop-blur text-xs font-mono">
                      <p className="text-slate-400 text-[10px] mb-1">Time: {pt.time} IST</p>
                      <p className="text-white font-bold text-base">
                        ₹{pt.price.toFixed(2)}
                      </p>
                      {prevClose && (
                        <p className={`text-[11px] font-bold mt-0.5 ${pt.price >= prevClose ? "text-emerald-400" : "text-rose-400"}`}>
                          {pt.price >= prevClose ? "+" : ""}{(pt.price - prevClose).toFixed(2)} (
                          {(((pt.price - prevClose) / prevClose) * 100).toFixed(2)}%)
                        </p>
                      )}
                      {pt.volume && (
                        <p className="text-slate-400 text-[10px] mt-1">Vol: {pt.volume.toLocaleString()}</p>
                      )}
                    </div>
                  );
                }
                return null;
              }}
            />
            {prevClose && (
              <ReferenceLine
                y={prevClose}
                stroke="#fbbf24"
                strokeDasharray="4 4"
                strokeWidth={1.5}
                label={{
                  value: `Prev Close ₹${prevClose.toFixed(2)}`,
                  fill: "#fbbf24",
                  fontSize: 10,
                  position: "insideTopLeft",
                }}
              />
            )}
            <Area
              type="monotone"
              dataKey="price"
              stroke={strokeColor}
              strokeWidth={2.5}
              fillOpacity={1}
              fill={`url(#${gradientId})`}
              dot={false}
              activeDot={{ r: 5, fill: strokeColor, stroke: "#0f172a", strokeWidth: 2 }}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}
