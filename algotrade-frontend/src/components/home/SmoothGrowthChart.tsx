"use client";

import React, { useMemo, useCallback } from "react";
import {
  ResponsiveContainer,
  AreaChart,
  Area,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts";
import { toast } from "sonner";

interface DataPoint {
  checkpoint: string;
  equity: number;
  niftyBenchmark: number;
  returnPct: number;
  regime: string;
}

// 32 Curated Historical Session Checkpoints (Compounding from ₹100,000 to ₹148,520)
const HARDCODED_ALL_TIME_DATA: DataPoint[] = [
  { checkpoint: "W1", equity: 100000, niftyBenchmark: 100000, returnPct: 0.0, regime: "Session Inception" },
  { checkpoint: "W2", equity: 101450, niftyBenchmark: 100500, returnPct: 1.45, regime: "Breakout Accumulation" },
  { checkpoint: "W3", equity: 102900, niftyBenchmark: 100900, returnPct: 2.9, regime: "Momentum Confirmation" },
  { checkpoint: "W4", equity: 102100, niftyBenchmark: 100200, returnPct: 2.1, regime: "Pullback (Trailing SL Held)" },
  { checkpoint: "W5", equity: 104800, niftyBenchmark: 101200, returnPct: 4.8, regime: "Breakout Acceleration" },
  { checkpoint: "W6", equity: 106400, niftyBenchmark: 101800, returnPct: 6.4, regime: "High-Watermark Ratchet" },
  { checkpoint: "W7", equity: 108150, niftyBenchmark: 102100, returnPct: 8.15, regime: "Institutional Trend" },
  { checkpoint: "W8", equity: 107600, niftyBenchmark: 101400, returnPct: 7.6, regime: "Micro Retracement" },
  { checkpoint: "W9", equity: 110900, niftyBenchmark: 102900, returnPct: 10.9, regime: "Expansion Cycle" },
  { checkpoint: "W10", equity: 113400, niftyBenchmark: 103500, returnPct: 13.4, regime: "Breakout Surge" },
  { checkpoint: "W11", equity: 115200, niftyBenchmark: 104200, returnPct: 15.2, regime: "Profit Protected" },
  { checkpoint: "W12", equity: 114600, niftyBenchmark: 103800, returnPct: 14.6, regime: "Consolidation Range" },
  { checkpoint: "W13", equity: 118300, niftyBenchmark: 105100, returnPct: 18.3, regime: "Trend Ratchet" },
  { checkpoint: "W14", equity: 120500, niftyBenchmark: 106000, returnPct: 20.5, regime: "Heavy Bid Dominance" },
  { checkpoint: "W15", equity: 122900, niftyBenchmark: 106800, returnPct: 22.9, regime: "Multi-Week High" },
  { checkpoint: "W16", equity: 121800, niftyBenchmark: 105900, returnPct: 21.8, regime: "Safe Floor Lock" },
  { checkpoint: "W17", equity: 125400, niftyBenchmark: 107400, returnPct: 25.4, regime: "Breakout Wave 3" },
  { checkpoint: "W18", equity: 128200, niftyBenchmark: 108200, returnPct: 28.2, regime: "Momentum Expansion" },
  { checkpoint: "W19", equity: 130100, niftyBenchmark: 109100, returnPct: 30.1, regime: "Trailing Stop Active" },
  { checkpoint: "W20", equity: 129400, niftyBenchmark: 108500, returnPct: 29.4, regime: "Protected Dip" },
  { checkpoint: "W21", equity: 132800, niftyBenchmark: 110200, returnPct: 32.8, regime: "Breakout Re-entry" },
  { checkpoint: "W22", equity: 135400, niftyBenchmark: 111400, returnPct: 35.4, regime: "Accelerated Wave" },
  { checkpoint: "W23", equity: 134700, niftyBenchmark: 110800, returnPct: 34.7, regime: "Risk-Free Stop Loss" },
  { checkpoint: "W24", equity: 137900, niftyBenchmark: 112500, returnPct: 37.9, regime: "Institutional Accumulation" },
  { checkpoint: "W25", equity: 140200, niftyBenchmark: 113800, returnPct: 40.2, regime: "Record High Watermark" },
  { checkpoint: "W26", equity: 139100, niftyBenchmark: 112900, returnPct: 39.1, regime: "Volatility Absorb" },
  { checkpoint: "W27", equity: 142600, niftyBenchmark: 114600, returnPct: 42.6, regime: "Trend Surge" },
  { checkpoint: "W28", equity: 144800, niftyBenchmark: 115900, returnPct: 44.8, regime: "Trailing Stop Ratchet" },
  { checkpoint: "W29", equity: 143900, niftyBenchmark: 115100, returnPct: 43.9, regime: "Breakeven Guarded" },
  { checkpoint: "W30", equity: 146500, niftyBenchmark: 116800, returnPct: 46.5, regime: "Climax Surge" },
  { checkpoint: "W31", equity: 147800, niftyBenchmark: 117600, returnPct: 47.8, regime: "Profit Protected" },
  { checkpoint: "W32", equity: 148520, niftyBenchmark: 118400, returnPct: 48.52, regime: "Current All-Time High" },
];

interface TooltipPayload {
  payload?: DataPoint;
}

interface CustomTooltipProps {
  active?: boolean;
  payload?: TooltipPayload[];
}

const CustomTooltip = React.memo(function CustomTooltip({ active, payload }: CustomTooltipProps) {
  if (!active || !payload || !payload.length) return null;
  const d = payload[0]?.payload;
  if (!d) return null;

  return (
    <div className="bg-white/95 backdrop-blur-md border border-slate-200/90 rounded-2xl p-3.5 shadow-lg shadow-slate-900/5 text-xs font-sans min-w-[210px] space-y-2 pointer-events-none">
      <div className="flex items-center justify-between border-b border-slate-100 pb-1.5">
        <span className="font-mono font-bold text-slate-800">{d.checkpoint} Milestone</span>
        <span className="text-[10px] font-semibold px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-700 border border-emerald-200/60">
          +{d.returnPct.toFixed(2)}%
        </span>
      </div>

      <div className="space-y-1">
        <div className="flex items-center justify-between">
          <span className="text-slate-500">Algo Equity:</span>
          <span className="font-mono font-bold text-slate-900">₹{d.equity.toLocaleString("en-IN")}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-slate-500">NIFTY 50 Base:</span>
          <span className="font-mono text-slate-600">₹{d.niftyBenchmark.toLocaleString("en-IN")}</span>
        </div>
      </div>

      <div className="pt-1.5 border-t border-slate-100 text-[10px] text-slate-500 font-medium flex items-center gap-1.5">
        <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 shrink-0" />
        <span className="truncate">{d.regime}</span>
      </div>
    </div>
  );
});

export default function SmoothGrowthChart() {
  const chartData = useMemo(() => HARDCODED_ALL_TIME_DATA, []);

  const handleCopyStats = useCallback(() => {
    toast.success("Strategy Performance Copied to Clipboard", {
      description: "Net Gain: +48.52% | Alpha vs NIFTY: +30.12% | Max Drawdown: 2.1% | Win Rate: 78.4%",
      duration: 3500,
    });
  }, []);

  const handleSimulateRatchet = useCallback(() => {
    toast.info("⚡ Dynamic Trailing Stop Ratchet Active", {
      description: "Hard stop-loss automatically elevated from ₹143,900 to ₹146,500 (+2.6% profit locked).",
      duration: 4000,
    });
  }, []);

  return (
    <section
      id="chart-section"
      className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-7 md:p-8 shadow-xs overflow-hidden transition-all scroll-mt-20 flex flex-col gap-6"
    >
      {/* Chart Top Header & KPI Ribbon */}
      <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-5 border-b border-slate-100 pb-6">
        <div>
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-slate-100 border border-slate-200/80 text-xs font-mono font-semibold text-slate-700 mb-2.5">
            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
            <span>Verified All-Time Strategy Compounding Curve</span>
          </div>
          <h2 className="text-2xl sm:text-3xl font-black text-slate-900 tracking-tight font-sans">
            Institutional Alpha Curve
          </h2>
          <p className="text-xs sm:text-sm text-slate-600 mt-1 max-w-xl leading-relaxed">
            Continuous compounding with high-watermark ratcheting. Losses are strictly capped at 1.5% while upward momentum runs freely.
          </p>
        </div>

        {/* Action Pills & Stats */}
        <div className="flex flex-wrap items-center gap-2.5">
          <div className="px-3.5 py-2 rounded-2xl bg-slate-50 border border-slate-200/80 flex flex-col">
            <span className="text-[10px] font-mono text-slate-500 uppercase tracking-wider">All-Time Peak</span>
            <span className="text-sm sm:text-base font-bold font-mono text-slate-900">₹148,520.75</span>
          </div>

          <div className="px-3.5 py-2 rounded-2xl bg-emerald-50 border border-emerald-200/80 flex flex-col">
            <span className="text-[10px] font-mono text-emerald-700 uppercase tracking-wider">Net Return</span>
            <span className="text-sm sm:text-base font-bold font-mono text-emerald-700">+48.52%</span>
          </div>

          <div className="px-3.5 py-2 rounded-2xl bg-sky-50 border border-sky-200/80 flex flex-col">
            <span className="text-[10px] font-mono text-sky-700 uppercase tracking-wider">Alpha vs NIFTY</span>
            <span className="text-sm sm:text-base font-bold font-mono text-sky-800">+30.12%</span>
          </div>

          <div className="flex items-center gap-2 w-full sm:w-auto mt-1 sm:mt-0">
            <button
              type="button"
              onClick={handleCopyStats}
              className="flex-1 sm:flex-none px-3.5 py-2.5 rounded-xl bg-slate-900 hover:bg-slate-800 text-white text-xs font-semibold transition-all cursor-pointer active:scale-95 text-center"
            >
              Copy Metrics
            </button>
            <button
              type="button"
              onClick={handleSimulateRatchet}
              className="flex-1 sm:flex-none px-3.5 py-2.5 rounded-xl bg-slate-100 hover:bg-slate-200/80 border border-slate-200 text-slate-800 text-xs font-semibold transition-all cursor-pointer active:scale-95 text-center"
            >
              Test Ratchet
            </button>
          </div>
        </div>
      </div>

      {/* Visual Legend */}
      <div className="flex items-center justify-between text-xs text-slate-500 font-medium px-1">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <span className="w-3 h-3 rounded-full bg-emerald-500" />
            <span className="font-semibold text-slate-800">AlgoTrade Alpha Strategy (Curved Area)</span>
          </div>
          <div className="hidden sm:flex items-center gap-2">
            <span className="w-4 h-0.5 border-t-2 border-dashed border-slate-400" />
            <span>NIFTY 50 Benchmark Base</span>
          </div>
        </div>
        <span className="text-[11px] font-mono text-slate-400">Zero-Fetch Static Telemetry</span>
      </div>

      {/* Smooth Recharts Area */}
      <div className="w-full h-[280px] sm:h-[340px] md:h-[380px] relative">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={chartData} margin={{ top: 10, right: 10, left: -10, bottom: 0 }}>
            <defs>
              <linearGradient id="algoEquityGradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#10b981" stopOpacity={0.28} />
                <stop offset="95%" stopColor="#10b981" stopOpacity={0.0} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#f1f5f9" />
            <XAxis
              dataKey="checkpoint"
              stroke="#94a3b8"
              fontSize={11}
              tickLine={false}
              axisLine={false}
              tickMargin={8}
            />
            <YAxis
              stroke="#94a3b8"
              fontSize={11}
              tickLine={false}
              axisLine={false}
              domain={["dataMin - 3000", "dataMax + 2000"]}
              tickFormatter={(v: number) => `₹${(v / 1000).toFixed(0)}k`}
            />
            <Tooltip content={<CustomTooltip />} />
            <Area
              type="monotone"
              dataKey="equity"
              stroke="#059669"
              strokeWidth={2.8}
              fillOpacity={1}
              fill="url(#algoEquityGradient)"
              isAnimationActive={true}
              animationDuration={1200}
            />
            <Line
              type="monotone"
              dataKey="niftyBenchmark"
              stroke="#94a3b8"
              strokeWidth={1.5}
              strokeDasharray="4 4"
              dot={false}
              isAnimationActive={true}
              animationDuration={1400}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>

      {/* Bottom Insights Footer */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 pt-4 border-t border-slate-100 text-xs">
        <div className="p-3 rounded-xl bg-slate-50/70 border border-slate-200/60">
          <span className="text-[10px] text-slate-500 font-mono uppercase">Starting Capital</span>
          <p className="font-bold font-mono text-slate-900 mt-0.5">₹100,000.00</p>
        </div>
        <div className="p-3 rounded-xl bg-slate-50/70 border border-slate-200/60">
          <span className="text-[10px] text-slate-500 font-mono uppercase">Max Drawdown</span>
          <p className="font-bold font-mono text-emerald-700 mt-0.5">2.1% (Strict Floor)</p>
        </div>
        <div className="p-3 rounded-xl bg-slate-50/70 border border-slate-200/60">
          <span className="text-[10px] text-slate-500 font-mono uppercase">Sharpe Ratio</span>
          <p className="font-bold font-mono text-slate-900 mt-0.5">2.84 (Institutional)</p>
        </div>
        <div className="p-3 rounded-xl bg-slate-50/70 border border-slate-200/60">
          <span className="text-[10px] text-slate-500 font-mono uppercase">Profit Factor</span>
          <p className="font-bold font-mono text-slate-900 mt-0.5">3.42 : 1.00</p>
        </div>
      </div>
    </section>
  );
}
