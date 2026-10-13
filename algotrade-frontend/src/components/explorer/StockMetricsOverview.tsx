"use client";

import { useState } from "react";
import { StockQuoteData } from "../types";

interface StockMetricsOverviewProps {
  quote: StockQuoteData;
}

export default function StockMetricsOverview({ quote }: StockMetricsOverviewProps) {
  // State for hover-activated educational popovers
  const [hoveredInfoKey, setHoveredInfoKey] = useState<string | null>(null);

  // Extract core numbers with safe zero-fallbacks
  const ltp = quote.lastPrice || 0;
  const open = quote.ohlc?.open || 0;
  const high = quote.ohlc?.high || ltp;
  const low = quote.ohlc?.low || ltp;
  const prevClose = quote.ohlc?.close || 0;
  const volume = quote.volume || 0;
  const vwap = quote.averagePrice || 0;
  const lowerCircuit = quote.lowerCircuitLimit || 0;
  const upperCircuit = quote.upperCircuitLimit || 0;
  const ltq = quote.lastTradedQuantity;
  const ltt = quote.lastTradedTime;

  // 1. Day Range Calculations (Thermometer)
  const dayRange = high > low ? high - low : 0;
  const dayRangePct = low > 0 && dayRange > 0 ? (dayRange / low) * 100 : 0;
  const rangePositionPct =
    dayRange > 0 ? Math.min(100, Math.max(0, ((ltp - low) / dayRange) * 100)) : 50;
  // Edge clamping so the floating price badge never clips outside card boundaries
  const clampedLeft = Math.max(3, Math.min(97, rangePositionPct));

  // 2. Turnover Calculation (Volume * VWAP)
  const effectivePriceForTurnover = vwap > 0 ? vwap : ltp;
  const turnoverRupees = volume * effectivePriceForTurnover;
  const turnoverCrores = turnoverRupees / 10000000;

  // 3. VWAP Bias
  const vwapDiff = vwap > 0 ? ltp - vwap : 0;
  const vwapBiasPct = vwap > 0 ? (vwapDiff / vwap) * 100 : 0;

  // 4. Intraday Momentum & Opening Gap
  const changeFromOpen = open > 0 ? ltp - open : 0;
  const changeFromOpenPct = open > 0 ? (changeFromOpen / open) * 100 : 0;

  const gap = prevClose > 0 && open > 0 ? open - prevClose : 0;
  const gapPct = prevClose > 0 ? (gap / prevClose) * 100 : 0;

  // 5. Open-High-Low (OHL) Setup Detection
  // Tolerance: 0.05 paise or 0.05% of open price
  const ohlTolerance = Math.max(0.05, open * 0.0005);
  let ohlStatus: "bullish" | "bearish" | "balanced" = "balanced";
  if (open > 0 && high > 0 && Math.abs(open - high) <= ohlTolerance) {
    ohlStatus = "bearish"; // Open = High (Sellers dominated all day)
  } else if (open > 0 && low > 0 && Math.abs(open - low) <= ohlTolerance) {
    ohlStatus = "bullish"; // Open = Low (Buyers dominated all day)
  }

  // 6. Circuit Headroom Buffers
  const upperBufferPct = upperCircuit > 0 && ltp > 0 ? ((upperCircuit - ltp) / ltp) * 100 : null;
  const lowerBufferPct = lowerCircuit > 0 && ltp > 0 ? ((ltp - lowerCircuit) / ltp) * 100 : null;

  // Educational term guides in crystal-clear simple English
  const infoGuides: Record<
    string,
    { title: string; desc: string; bulletPoints: { term: string; explanation: string }[] }
  > = {
    range: {
      title: "Day's Price Range Thermometer",
      desc: "Think of this like a temperature gauge for today's price action.",
      bulletPoints: [
        {
          term: "Day Low (Left)",
          explanation: "The lowest price anyone paid for this stock today.",
        },
        {
          term: "Day High (Right)",
          explanation: "The highest price anyone paid for this stock today.",
        },
        {
          term: "Glowing Dot (Position)",
          explanation:
            "Shows where the current price is right now. Above 80% means buyers are winning and pushing near highs. Below 20% means sellers are in control near lows. Around 50% means the price is resting in the middle.",
        },
        {
          term: "Day Volatility %",
          explanation:
            "How wide the gap is between high and low. Bigger percentage means lots of action and price movement.",
        },
      ],
    },
    price: {
      title: "Intraday Price & OHL Setup Guide",
      desc: "Explains how the stock started today and who has taken control since the opening bell.",
      bulletPoints: [
        {
          term: "Open",
          explanation: "The very first price matched at 9:15 AM when the market opened today.",
        },
        {
          term: "Previous Close",
          explanation: "The official closing price of the stock yesterday at 3:30 PM.",
        },
        {
          term: "Opening Gap",
          explanation:
            "When the stock opens higher (Gap Up) or lower (Gap Down) than yesterday's close because of overnight news or global markets.",
        },
        {
          term: "Change from Open",
          explanation:
            "Shows whether the price went up or down AFTER the 9:15 AM bell today, ignoring yesterday. A stock can be green vs yesterday but red from open if it started falling after the morning bell.",
        },
        {
          term: "OHL Setup (Open-High-Low)",
          explanation:
            "A popular rule for day traders. If Open = Low, buyers took charge right away and never let price dip (Bullish). If Open = High, heavy sellers dumped immediately and price never went higher (Bearish).",
        },
      ],
    },
    volume: {
      title: "Volume & Institutional Flow Guide",
      desc: "Reveals where the big money (mutual funds, banks, foreign institutions) is putting their cash.",
      bulletPoints: [
        {
          term: "Traded Volume",
          explanation: "The total count of shares bought and sold between all traders today.",
        },
        {
          term: "Total Turnover (₹ Cr)",
          explanation:
            "The total actual cash exchanged today. ₹500+ Crores means huge institutions are actively buying and selling, providing deep liquidity.",
        },
        {
          term: "VWAP (Average Price)",
          explanation:
            "Volume-Weighted Average Price. This is the true average price paid by all buyers today. If current price is ABOVE VWAP, buyers are in profit and momentum is bullish. If BELOW VWAP, sellers dominate.",
        },
        {
          term: "VWAP Bias %",
          explanation:
            "How far price has stretched away from VWAP. When price gets too far above or below, it often bounces back toward VWAP like a rubber band.",
        },
        {
          term: "Last Traded Order",
          explanation: "The exact number of shares matched in the very latest trade execution at the exchange.",
        },
      ],
    },
    circuit: {
      title: "Circuit Limits & Safety Headroom Guide",
      desc: "Protective price barriers set by the National Stock Exchange (NSE).",
      bulletPoints: [
        {
          term: "Circuit Limits (Upper & Lower)",
          explanation:
            "Maximum and minimum prices a stock is allowed to trade on a single day. The exchange freezes trading outside these borders to prevent panics and wild crashes.",
        },
        {
          term: "Safety Buffer %",
          explanation:
            "How much room the stock has before hitting the circuit freeze. If the buffer is under 1.5%, be extra careful—orders can get locked if the circuit hits!",
        },
      ],
    },
  };

  // Reusable hover popover button component
  const renderInfoButton = (key: string, align: "left" | "right" = "left") => {
    const guide = infoGuides[key];
    if (!guide) return null;
    const isHovered = hoveredInfoKey === key;

    return (
      <div
        className="relative inline-flex items-center"
        onMouseEnter={() => setHoveredInfoKey(key)}
        onMouseLeave={() => setHoveredInfoKey(null)}
      >
        <button
          type="button"
          onClick={() => setHoveredInfoKey(isHovered ? null : key)}
          className="w-4 h-4 rounded-full bg-slate-800 hover:bg-slate-700 text-slate-300 hover:text-white text-[10px] font-bold flex items-center justify-center transition cursor-pointer border border-slate-700/60"
          title={`Hover to learn about ${guide.title}`}
          aria-label={`Learn about ${guide.title}`}
        >
          i
        </button>

        {/* Hover Popover */}
        {isHovered && (
          <div
            className={`absolute top-6 z-50 w-72 sm:w-84 bg-slate-950/95 border border-slate-700 rounded-xl p-3.5 shadow-2xl backdrop-blur-md text-xs text-slate-300 space-y-2.5 animate-fadeIn ${
              align === "right" ? "right-0" : "left-0"
            }`}
          >
            <div className="flex items-center justify-between border-b border-slate-800 pb-1.5">
              <p className="font-bold text-white flex items-center gap-1.5 text-[11px]">
                <span>📖</span> {guide.title}
              </p>
            </div>

            <p className="text-[11px] text-slate-400 leading-relaxed">{guide.desc}</p>

            <div className="space-y-2 max-h-64 overflow-y-auto pr-1">
              {guide.bulletPoints.map((pt, idx) => (
                <div key={idx} className="bg-slate-900/80 rounded-lg p-2 border border-slate-800/80">
                  <strong className="text-emerald-400 text-[11px] block">{pt.term}:</strong>
                  <p className="text-[10px] text-slate-300 leading-relaxed mt-0.5">{pt.explanation}</p>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="space-y-4 mt-6">
      {/* ────────────────────────────────────────────────────────────────────── */}
      {/* 1. VISUAL DAY'S PRICE RANGE THERMOMETER                                */}
      {/* ────────────────────────────────────────────────────────────────────── */}
      <div className="bg-slate-900/70 border border-slate-800 rounded-2xl p-4 sm:p-5 shadow-xl relative backdrop-blur-sm">
        <div className="flex items-center justify-between mb-2">
          <div className="flex items-center gap-2">
            <span className="text-base">📍</span>
            <h4 className="text-sm font-bold text-white tracking-wide">Day&apos;s Price Range</h4>
            <span className="text-[11px] font-mono text-slate-400">
              (Intraday Low to High)
            </span>

            {/* Hover-activated Info Popover */}
            {renderInfoButton("range", "left")}
          </div>

          <div className="text-right font-mono text-xs">
            <span className="text-slate-400">Position: </span>
            <strong
              className={`font-bold ${
                rangePositionPct >= 70
                  ? "text-emerald-400"
                  : rangePositionPct <= 30
                  ? "text-rose-400"
                  : "text-amber-400"
              }`}
            >
              {rangePositionPct.toFixed(1)}%
            </strong>
          </div>
        </div>

        {/* Range Bar Graphic with Perfectly Vertically Centered Marker Pin */}
        <div className="relative py-6 my-1">
          {/* Track Line */}
          <div className="h-2.5 w-full bg-slate-950/80 rounded-full border border-slate-800 relative overflow-hidden">
            <div
              className="absolute inset-0 bg-gradient-to-r from-rose-500/70 via-amber-400/70 to-emerald-500/70"
              style={{ width: "100%" }}
            />
          </div>

          {/* Current LTP Pin Indicator - Center of the dot is locked right on the center of the track */}
          <div
            className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2 flex flex-col items-center pointer-events-none transition-all duration-300"
            style={{ left: `${clampedLeft}%` }}
          >
            {/* Floating Price Pill positioned cleanly ABOVE the track line */}
            <div className="absolute bottom-full mb-1.5 flex flex-col items-center">
              <span className="text-[10px] font-mono font-bold text-white bg-slate-950/95 border border-slate-700 px-2 py-0.5 rounded shadow-lg whitespace-nowrap">
                ₹{ltp ? ltp.toFixed(2) : "--"}
              </span>
              <div className="w-0 h-0 border-x-4 border-x-transparent border-t-4 border-t-slate-700" />
            </div>

            {/* Glowing Dot - Perfectly vertically centered on the exact midpoint of the track */}
            <div className="w-4 h-4 rounded-full bg-white border-2 border-slate-950 shadow-[0_0_10px_rgba(255,255,255,0.9)] ring-2 ring-emerald-500/40" />
          </div>
        </div>

        {/* Low, Current, and High labels */}
        <div className="flex items-center justify-between text-xs font-mono pt-1">
          <div>
            <span className="text-slate-400 text-[11px] block">Day Low</span>
            <strong className="text-rose-400 font-bold text-sm">
              ₹{low ? low.toFixed(2) : "--"}
            </strong>
          </div>

          <div className="text-center hidden sm:block">
            <span className="text-slate-400 text-[11px] block">Total Range Spread</span>
            <span className="text-slate-200 font-bold">
              ₹{dayRange.toFixed(2)} ({dayRangePct.toFixed(2)}% Volatility)
            </span>
          </div>

          <div className="text-right">
            <span className="text-slate-400 text-[11px] block">Day High</span>
            <strong className="text-emerald-400 font-bold text-sm">
              ₹{high ? high.toFixed(2) : "--"}
            </strong>
          </div>
        </div>
      </div>

      {/* ────────────────────────────────────────────────────────────────────── */}
      {/* 2. FOUR CLEAN CATEGORIZED CARDS                                        */}
      {/* ────────────────────────────────────────────────────────────────────── */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* CARD 1: INTRADAY PRICE & OHL SETUP */}
        <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-4 sm:p-5 shadow-lg relative">
          <div className="flex items-center justify-between pb-3 mb-3 border-b border-slate-800">
            <h4 className="text-xs font-bold uppercase tracking-wider text-slate-300 flex items-center gap-1.5">
              <span>📊</span> Price Action &amp; OHL Setup
            </h4>
            {renderInfoButton("price", "left")}
          </div>

          <div className="grid grid-cols-2 gap-3 text-xs font-mono">
            <div>
              <span className="text-slate-400 text-[11px] block">Day Open</span>
              <strong className="text-white text-sm">
                ₹{open ? open.toFixed(2) : "--"}
              </strong>
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">Previous Close</span>
              <strong className="text-amber-300 text-sm">
                ₹{prevClose ? prevClose.toFixed(2) : "--"}
              </strong>
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">Opening Gap</span>
              <span
                className={`font-bold ${
                  gapPct > 0.05
                    ? "text-emerald-400"
                    : gapPct < -0.05
                    ? "text-rose-400"
                    : "text-slate-300"
                }`}
              >
                {gapPct > 0 ? "+" : ""}
                {gapPct.toFixed(2)}%
                <span className="text-[10px] text-slate-400 ml-1 font-normal">
                  ({gapPct > 0.05 ? "Gap Up" : gapPct < -0.05 ? "Gap Down" : "Flat"})
                </span>
              </span>
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">Change from Open</span>
              <span
                className={`font-bold ${
                  changeFromOpen >= 0 ? "text-emerald-400" : "text-rose-400"
                }`}
              >
                {changeFromOpen >= 0 ? "+" : ""}
                {changeFromOpen.toFixed(2)} ({changeFromOpenPct >= 0 ? "+" : ""}
                {changeFromOpenPct.toFixed(2)}%)
              </span>
            </div>

            <div className="col-span-2 pt-2 border-t border-slate-800/80 flex items-center justify-between">
              <span className="text-slate-400 text-[11px]">OHL Strategy Status:</span>
              <span
                className={`px-2 py-0.5 rounded text-[11px] font-bold ${
                  ohlStatus === "bullish"
                    ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30"
                    : ohlStatus === "bearish"
                    ? "bg-rose-500/20 text-rose-400 border border-rose-500/30"
                    : "bg-slate-800 text-slate-300"
                }`}
              >
                {ohlStatus === "bullish"
                  ? "🟢 Bullish (Open = Low)"
                  : ohlStatus === "bearish"
                  ? "🔴 Bearish (Open = High)"
                  : "⚪ Balanced Range"}
              </span>
            </div>
          </div>
        </div>

        {/* CARD 2: INSTITUTIONAL VOLUME & FLOW */}
        <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-4 sm:p-5 shadow-lg relative">
          <div className="flex items-center justify-between pb-3 mb-3 border-b border-slate-800">
            <h4 className="text-xs font-bold uppercase tracking-wider text-slate-300 flex items-center gap-1.5">
              <span>⚡</span> Institutional Volume &amp; VWAP
            </h4>
            {renderInfoButton("volume", "right")}
          </div>

          <div className="grid grid-cols-2 gap-3 text-xs font-mono">
            <div>
              <span className="text-slate-400 text-[11px] block">Traded Volume</span>
              <strong className="text-white text-sm">
                {volume ? volume.toLocaleString("en-IN") : "--"}
              </strong>
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">Total Turnover</span>
              <strong className="text-teal-400 text-sm">
                {turnoverCrores >= 1
                  ? `₹${turnoverCrores.toFixed(2)} Cr`
                  : `₹${(turnoverRupees / 100000).toFixed(2)} L`}
              </strong>
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">Exchange VWAP</span>
              <strong className="text-sky-400 text-sm">
                ₹{vwap ? vwap.toFixed(2) : "--"}
              </strong>
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">VWAP Bias</span>
              <span
                className={`font-bold ${
                  vwapDiff >= 0 ? "text-emerald-400" : "text-rose-400"
                }`}
              >
                {vwapDiff >= 0 ? "+" : ""}
                {vwapBiasPct.toFixed(2)}% ({vwapDiff >= 0 ? "+" : ""}₹{vwapDiff.toFixed(2)})
              </span>
            </div>

            <div className="col-span-2 pt-2 border-t border-slate-800/80 flex items-center justify-between">
              <span className="text-slate-400 text-[11px]">Last Match Size &amp; Time:</span>
              <span className="text-slate-300 font-mono text-[11px]">
                {ltq ? `${ltq.toLocaleString("en-IN")} shares` : "--"}
                {ltt ? ` @ ${ltt.includes(" ") ? ltt.split(" ")[1] : ltt}` : ""}
              </span>
            </div>
          </div>
        </div>

        {/* CARD 3: RISK & NSE CIRCUIT LIMITS */}
        <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-4 sm:p-5 shadow-lg relative">
          <div className="flex items-center justify-between pb-3 mb-3 border-b border-slate-800">
            <h4 className="text-xs font-bold uppercase tracking-wider text-slate-300 flex items-center gap-1.5">
              <span>🛡️</span> Risk &amp; NSE Circuit Bands
            </h4>
            {renderInfoButton("circuit", "left")}
          </div>

          <div className="grid grid-cols-2 gap-3 text-xs font-mono">
            <div>
              <span className="text-slate-400 text-[11px] block">Lower Circuit</span>
              <strong className="text-rose-400 text-sm">
                ₹{lowerCircuit ? lowerCircuit.toFixed(2) : "Uncapped"}
              </strong>
              {lowerBufferPct !== null && (
                <span className="text-[10px] text-slate-400 block mt-0.5">
                  Buffer: <strong className="text-emerald-400">+{lowerBufferPct.toFixed(2)}%</strong>
                </span>
              )}
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">Upper Circuit</span>
              <strong className="text-emerald-400 text-sm">
                ₹{upperCircuit ? upperCircuit.toFixed(2) : "Uncapped"}
              </strong>
              {upperBufferPct !== null && (
                <span className="text-[10px] text-slate-400 block mt-0.5">
                  Buffer: <strong className="text-emerald-400">+{upperBufferPct.toFixed(2)}%</strong>
                </span>
              )}
            </div>

            <div className="col-span-2 pt-2 border-t border-slate-800/80 flex items-center justify-between">
              <span className="text-slate-400 text-[11px]">Freeze Protection:</span>
              <span className="px-2 py-0.5 rounded text-[11px] font-bold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                Safe Trading Band
              </span>
            </div>
          </div>
        </div>

        {/* CARD 4: RANGE BOUNDS & EXTREMES */}
        <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-4 sm:p-5 shadow-lg relative">
          <div className="flex items-center justify-between pb-3 mb-3 border-b border-slate-800">
            <h4 className="text-xs font-bold uppercase tracking-wider text-slate-300 flex items-center gap-1.5">
              <span>📈</span> Day Range &amp; Volatility
            </h4>
            {renderInfoButton("range", "right")}
          </div>

          <div className="grid grid-cols-2 gap-3 text-xs font-mono">
            <div>
              <span className="text-slate-400 text-[11px] block">Day High</span>
              <strong className="text-emerald-400 text-sm">
                ₹{high ? high.toFixed(2) : "--"}
              </strong>
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">Day Low</span>
              <strong className="text-rose-400 text-sm">
                ₹{low ? low.toFixed(2) : "--"}
              </strong>
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">Absolute Range</span>
              <span className="text-slate-200 font-bold">
                ₹{dayRange.toFixed(2)}
              </span>
            </div>

            <div>
              <span className="text-slate-400 text-[11px] block">Day Volatility</span>
              <span className="text-amber-400 font-bold">
                {dayRangePct.toFixed(2)}%
              </span>
            </div>

            <div className="col-span-2 pt-2 border-t border-slate-800/80 flex items-center justify-between">
              <span className="text-slate-400 text-[11px]">Session Bias:</span>
              <span className="text-slate-300 font-mono text-[11px]">
                {rangePositionPct >= 60
                  ? "📈 Upper Half (Breakout Bias)"
                  : rangePositionPct <= 40
                  ? "📉 Lower Half (Breakdown Bias)"
                  : "⚖️ Equilibrium Rotation"}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
