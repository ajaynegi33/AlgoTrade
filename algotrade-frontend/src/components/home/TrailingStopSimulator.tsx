"use client";

import { useState, useEffect, useRef } from "react";
import { ResponsiveContainer, AreaChart, Area, Line, XAxis, YAxis, Tooltip, ReferenceLine, CartesianGrid, ReferenceDot } from "recharts";

interface SimDataPoint {
  time: string;
  price: number;
  stopLoss: number | null;
  entryPrice: number;
  volume: number;
  phase: number;
  message: string;
  isExited?: boolean;
  isCrash?: boolean;
}

// Convert 24-hr "HH:MM" string to 12-hr format (with or without AM/PM)
function format12Hour(time24: string, withPeriod = false): string {
  if (!time24 || !time24.includes(":")) return time24;
  const [hStr, mStr] = time24.split(":");
  let h = parseInt(hStr, 10);
  const m = mStr;
  const period = h >= 12 ? "PM" : "AM";
  if (h === 0) h = 12;
  else if (h > 12) h -= 12;
  const hFormatted = h < 10 ? `0${h}` : `${h}`;
  if (withPeriod) {
    return `${hFormatted}:${m} ${period}`;
  }
  return `${hFormatted}:${m}`;
}

const EXIT_INDEX = 32; // Index where exit triggers at 02:35 PM (price: 1,040.0)

// Official Full-Day NSE Intraday Session Timeline (09:15 AM Market Open to 03:30 PM Market Shutdown)
const SIMULATION_TIMELINE: SimDataPoint[] = [
  // Phase 1: Market Open & Breakout (09:15 AM - 09:45 AM)
  { time: "09:15", price: 1000.0, stopLoss: 985.0, entryPrice: 1000.0, volume: 72000, phase: 1, message: "Market Open (09:15 AM): Bought 100 shares @ ₹1,000. Hard Stop-Loss set at ₹985 to cap maximum risk at ₹1,500." },
  { time: "09:18", price: 1002.5, stopLoss: 985.0, entryPrice: 1000.0, volume: 46000, phase: 1, message: "Market Open (09:15 AM): Bought 100 shares @ ₹1,000. Hard Stop-Loss set at ₹985 to cap maximum risk at ₹1,500." },
  { time: "09:22", price: 997.0, stopLoss: 985.0, entryPrice: 1000.0, volume: 38000, phase: 1, message: "Market Open (09:15 AM): Bought 100 shares @ ₹1,000. Hard Stop-Loss set at ₹985 to cap maximum risk at ₹1,500." },
  { time: "09:25", price: 993.5, stopLoss: 985.0, entryPrice: 1000.0, volume: 41000, phase: 1, message: "Market Open (09:15 AM): Bought 100 shares @ ₹1,000. Hard Stop-Loss set at ₹985 to cap maximum risk at ₹1,500." },
  { time: "09:30", price: 996.8, stopLoss: 985.0, entryPrice: 1000.0, volume: 34000, phase: 1, message: "Market Open (09:15 AM): Bought 100 shares @ ₹1,000. Hard Stop-Loss set at ₹985 to cap maximum risk at ₹1,500." },
  { time: "09:35", price: 1001.0, stopLoss: 985.0, entryPrice: 1000.0, volume: 48000, phase: 1, message: "Market Open (09:15 AM): Bought 100 shares @ ₹1,000. Hard Stop-Loss set at ₹985 to cap maximum risk at ₹1,500." },
  { time: "09:40", price: 1006.5, stopLoss: 985.0, entryPrice: 1000.0, volume: 55000, phase: 1, message: "Market Open (09:15 AM): Bought 100 shares @ ₹1,000. Hard Stop-Loss set at ₹985 to cap maximum risk at ₹1,500." },
  { time: "09:45", price: 1010.0, stopLoss: 985.0, entryPrice: 1000.0, volume: 62000, phase: 1, message: "Market Open (09:15 AM): Bought 100 shares @ ₹1,000. Hard Stop-Loss set at ₹985 to cap maximum risk at ₹1,500." },

  // Phase 2: Morning Rally & Breakeven Ratchet (09:50 AM - 10:45 AM)
  { time: "09:50", price: 1015.0, stopLoss: 1003.5, entryPrice: 1000.0, volume: 105000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "09:55", price: 1013.2, stopLoss: 1003.5, entryPrice: 1000.0, volume: 68000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "10:05", price: 1009.5, stopLoss: 1003.5, entryPrice: 1000.0, volume: 44000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "10:15", price: 1007.8, stopLoss: 1003.5, entryPrice: 1000.0, volume: 36000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "10:25", price: 1011.0, stopLoss: 1003.5, entryPrice: 1000.0, volume: 42000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "10:35", price: 1014.5, stopLoss: 1003.5, entryPrice: 1000.0, volume: 50000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "10:45", price: 1018.0, stopLoss: 1003.5, entryPrice: 1000.0, volume: 54000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },

  // Phase 3: Midday Institutional Accumulation (11:00 AM - 12:45 PM)
  { time: "11:00", price: 1016.5, stopLoss: 1003.5, entryPrice: 1000.0, volume: 32000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "11:15", price: 1021.0, stopLoss: 1003.5, entryPrice: 1000.0, volume: 39000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "11:30", price: 1019.2, stopLoss: 1003.5, entryPrice: 1000.0, volume: 30000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "11:45", price: 1024.0, stopLoss: 1003.5, entryPrice: 1000.0, volume: 43000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "12:00", price: 1027.5, stopLoss: 1003.5, entryPrice: 1000.0, volume: 47000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "12:15", price: 1026.0, stopLoss: 1003.5, entryPrice: 1000.0, volume: 35000, phase: 2, message: "Morning Surge (09:50 AM): Price reached ₹1,015. Stop-Loss raised to ₹1,003.50. Brokerage fees and taxes are covered. Risk is now zero." },
  { time: "12:30", price: 1030.0, stopLoss: 1018.0, entryPrice: 1000.0, volume: 88000, phase: 3, message: "Midday Expansion (12:30 PM): Price reached ₹1,030. Stop-Loss raised to ₹1,018. A net profit of at least ₹1,800 is permanently locked in." },
  { time: "12:45", price: 1028.5, stopLoss: 1018.0, entryPrice: 1000.0, volume: 56000, phase: 3, message: "Midday Expansion (12:30 PM): Price reached ₹1,030. Stop-Loss raised to ₹1,018. A net profit of at least ₹1,800 is permanently locked in." },

  // Phase 4: Afternoon Climax Rally (01:00 PM - 02:20 PM)
  { time: "13:00", price: 1032.0, stopLoss: 1018.0, entryPrice: 1000.0, volume: 52000, phase: 3, message: "Midday Expansion (12:30 PM): Price reached ₹1,030. Stop-Loss raised to ₹1,018. A net profit of at least ₹1,800 is permanently locked in." },
  { time: "13:15", price: 1035.5, stopLoss: 1018.0, entryPrice: 1000.0, volume: 60000, phase: 3, message: "Midday Expansion (12:30 PM): Price reached ₹1,030. Stop-Loss raised to ₹1,018. A net profit of at least ₹1,800 is permanently locked in." },
  { time: "13:30", price: 1033.8, stopLoss: 1018.0, entryPrice: 1000.0, volume: 51000, phase: 3, message: "Midday Expansion (12:30 PM): Price reached ₹1,030. Stop-Loss raised to ₹1,018. A net profit of at least ₹1,800 is permanently locked in." },
  { time: "13:45", price: 1039.0, stopLoss: 1018.0, entryPrice: 1000.0, volume: 68000, phase: 3, message: "Midday Expansion (12:30 PM): Price reached ₹1,030. Stop-Loss raised to ₹1,018. A net profit of at least ₹1,800 is permanently locked in." },
  { time: "14:00", price: 1043.5, stopLoss: 1018.0, entryPrice: 1000.0, volume: 84000, phase: 3, message: "Midday Expansion (12:30 PM): Price reached ₹1,030. Stop-Loss raised to ₹1,018. A net profit of at least ₹1,800 is permanently locked in." },
  { time: "14:10", price: 1046.0, stopLoss: 1018.0, entryPrice: 1000.0, volume: 96000, phase: 3, message: "Midday Expansion (12:30 PM): Price reached ₹1,030. Stop-Loss raised to ₹1,018. A net profit of at least ₹1,800 is permanently locked in." },
  { time: "14:20", price: 1048.5, stopLoss: 1040.0, entryPrice: 1000.0, volume: 125000, phase: 4, message: "Afternoon Peak (02:20 PM): Price hit daily high of ₹1,048.50. Stop-Loss pinned to ₹1,040 to protect maximum +₹4,000 gain." },

  // Phase 5: Reversal, Automated Peak Exit & Panic Crash to 03:30 PM Shutdown
  { time: "14:25", price: 1045.0, stopLoss: 1040.0, entryPrice: 1000.0, volume: 72000, phase: 4, message: "Afternoon Peak (02:20 PM): Price hit daily high of ₹1,048.50. Stop-Loss pinned to ₹1,040 to protect maximum +₹4,000 gain." },
  { time: "14:30", price: 1042.2, stopLoss: 1040.0, entryPrice: 1000.0, volume: 80000, phase: 4, message: "Afternoon Peak (02:20 PM): Price hit daily high of ₹1,048.50. Stop-Loss pinned to ₹1,040 to protect maximum +₹4,000 gain." },
  { time: "14:35", price: 1040.0, stopLoss: 1040.0, entryPrice: 1000.0, volume: 155000, phase: 5, isExited: true, isCrash: true, message: "Flash Reversal (02:35 PM): Stop-Loss triggered! Automatically sold 100 shares at ₹1,040.00. You locked in +₹4,000 profit before the crash." },
  { time: "14:40", price: 1031.0, stopLoss: null, entryPrice: 1000.0, volume: 130000, phase: 5, isExited: true, isCrash: true, message: "Flash Reversal (02:35 PM): Stop-Loss triggered! Automatically sold 100 shares at ₹1,040.00. You locked in +₹4,000 profit before the crash." },
  { time: "14:45", price: 1022.5, stopLoss: null, entryPrice: 1000.0, volume: 142000, phase: 5, isExited: true, isCrash: true, message: "Flash Reversal (02:35 PM): Stop-Loss triggered! Automatically sold 100 shares at ₹1,040.00. You locked in +₹4,000 profit before the crash." },
  { time: "14:50", price: 1012.0, stopLoss: null, entryPrice: 1000.0, volume: 122000, phase: 5, isExited: true, isCrash: true, message: "Market Sell-off (02:50 PM): Price drops towards buy price. You are safe in cash with +₹4,000 profit while manual traders lose their gains." },
  { time: "14:55", price: 1004.0, stopLoss: null, entryPrice: 1000.0, volume: 135000, phase: 5, isExited: true, isCrash: true, message: "Market Sell-off (02:50 PM): Price drops towards buy price. You are safe in cash with +₹4,000 profit while manual traders lose their gains." },
  { time: "15:00", price: 994.5, stopLoss: null, entryPrice: 1000.0, volume: 140000, phase: 5, isExited: true, isCrash: true, message: "Market Sell-off (02:50 PM): Price drops towards buy price. You are safe in cash with +₹4,000 profit while manual traders lose their gains." },
  { time: "15:05", price: 986.0, stopLoss: null, entryPrice: 1000.0, volume: 152000, phase: 5, isExited: true, isCrash: true, message: "Closing Panic (03:05 PM): Price plummets into negative territory. Manual traders face heavy losses while your capital is completely safe." },
  { time: "15:10", price: 978.0, stopLoss: null, entryPrice: 1000.0, volume: 138000, phase: 5, isExited: true, isCrash: true, message: "Closing Panic (03:05 PM): Price plummets into negative territory. Manual traders face heavy losses while your capital is completely safe." },
  { time: "15:15", price: 971.0, stopLoss: null, entryPrice: 1000.0, volume: 146000, phase: 5, isExited: true, message: "Closing Panic (03:05 PM): Price plummets into negative territory. Manual traders face heavy losses while your capital is completely safe.", isCrash: true },
  { time: "15:20", price: 966.5, stopLoss: null, entryPrice: 1000.0, volume: 158000, phase: 5, isExited: true, message: "03:30 PM Market Shutdown: Market dumped to ₹960. You kept +₹4,000 profit. Unprotected manual traders lost -₹4,000.", isCrash: true },
  { time: "15:25", price: 962.0, stopLoss: null, entryPrice: 1000.0, volume: 170000, phase: 5, isExited: true, message: "03:30 PM Market Shutdown: Market dumped to ₹960. You kept +₹4,000 profit. Unprotected manual traders lost -₹4,000.", isCrash: true },
  { time: "15:30", price: 960.0, stopLoss: null, entryPrice: 1000.0, volume: 185000, phase: 5, isExited: true, message: "03:30 PM Market Shutdown: Market dumped to ₹960. You kept +₹4,000 profit. Unprotected manual traders lost -₹4,000.", isCrash: true },
];

const NORMAL_STEP_MS = 1800; // 1.8 seconds on every single price tick (stops 1-2s minimum)
const MILESTONE_STEP_MS = 4000; // 4.0 seconds whenever a milestone or message updates (sticks 3-4s so user can read)
const HOLD_END_MS = 10000; // 10.0 seconds to review the final crash comparison before smooth loop restart

function getStepDelay(index: number): number {
  if (index === 0) return MILESTONE_STEP_MS;
  const current = SIMULATION_TIMELINE[index];
  const prev = SIMULATION_TIMELINE[index - 1];

  // Pause for 4.0s when message changes, phase changes, or exit occurs so user can comfortably read
  if (
    current.message !== prev.message ||
    current.phase !== prev.phase ||
    (current.isExited && !prev.isExited)
  ) {
    return MILESTONE_STEP_MS;
  }
  return NORMAL_STEP_MS;
}

export default function TrailingStopSimulator() {
  const [stepIndex, setStepIndex] = useState(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Background tab defense
  const isVisibleRef = useRef(true);

  useEffect(() => {
    const handleVisibility = () => {
      isVisibleRef.current = document.visibilityState === "visible";
    };
    document.addEventListener("visibilitychange", handleVisibility);
    return () => document.removeEventListener("visibilitychange", handleVisibility);
  }, []);

  // Continuous animation loop with milestone-aware pacing
  useEffect(() => {
    let active = true;

    const scheduleStep = (idx: number) => {
      if (!active) return;
      const delay = getStepDelay(idx);

      timerRef.current = setTimeout(() => {
        if (!active) return;

        if (!isVisibleRef.current) {
          // If tab is hidden in background, wait and retry
          timerRef.current = setTimeout(() => scheduleStep(idx), 1000);
          return;
        }

        if (idx < SIMULATION_TIMELINE.length - 1) {
          const nextIdx = idx + 1;
          setStepIndex(nextIdx);
          scheduleStep(nextIdx);
        } else {
          // Reached 03:30 PM market shutdown: hold for 10 seconds to review final comparison, then restart
          timerRef.current = setTimeout(() => {
            if (!active) return;
            setStepIndex(0);
            scheduleStep(0);
          }, HOLD_END_MS);
        }
      }, delay);
    };

    scheduleStep(0);

    return () => {
      active = false;
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const currentPoint = SIMULATION_TIMELINE[stepIndex];

  // Fixed full-day session dataset (09:15 to 15:30) with null future values
  // This guarantees the X-Axis domain is static from frame 0 and moves left -> right
  const chartData = SIMULATION_TIMELINE.map((pt, idx) => {
    if (idx <= stepIndex) {
      const isPastExit = idx > EXIT_INDEX;
      return {
        time: pt.time,
        // In-trade green curve up to and including exit index
        gainPrice: !isPastExit ? pt.price : null,
        // Crash red curve from exit index onwards (connecting at exit index for seamless line)
        crashPrice: idx >= EXIT_INDEX ? pt.price : null,
        stopLoss: pt.stopLoss,
        rawPrice: pt.price,
      };
    }
    // Future unreached points are null, preserving fixed 09:15-15:30 X-Axis scale
    return {
      time: pt.time,
      gainPrice: null,
      crashPrice: null,
      stopLoss: null,
      rawPrice: null,
    };
  });

  // Financial metrics
  const shares = 100;
  const currentPrice = currentPoint.price;
  const entryPrice = 1000.0;
  const currentStopLoss = currentPoint.stopLoss;
  const isExited = !!currentPoint.isExited;

  // Realized or unrealized P&L
  const activeProfitPrice = isExited ? 1040.0 : currentPrice;
  const pnlAmount = (activeProfitPrice - entryPrice) * shares;
  const pnlPct = ((activeProfitPrice - entryPrice) / entryPrice) * 100;
  const isPositive = pnlAmount >= 0;

  // Active Safety Net Status text
  let safetyStatus = "Initial Safety Floor at ₹985.00";
  if (currentPoint.phase === 2) safetyStatus = "Brokerage & Fees Covered (Risk-Free)";
  if (currentPoint.phase === 3) safetyStatus = "+₹1,800 Minimum Profit Locked";
  if (currentPoint.phase === 4) safetyStatus = "+₹4,000 Peak Profit Pinned";
  if (isExited) safetyStatus = "Safely Sold @ ₹1,040.00";

  // Adaptive Toast / Banner Styling based on market phase (clean neutral border, no green outline)
  let bannerClass = "bg-slate-50 border-slate-200/90 text-slate-800";
  let timeClass = "text-slate-600";

  if (currentPoint.phase === 1) {
    bannerClass = "bg-sky-50/70 border-slate-200/90 text-slate-900";
    timeClass = "text-sky-700";
  } else if (currentPoint.phase === 2 || currentPoint.phase === 3) {
    bannerClass = "bg-emerald-50/60 border-slate-200/90 text-slate-900";
    timeClass = "text-emerald-700";
  } else if (currentPoint.phase === 4) {
    bannerClass = "bg-amber-50/60 border-slate-200/90 text-slate-900";
    timeClass = "text-amber-800";
  } else if (currentPoint.phase === 5) {
    bannerClass = "bg-rose-50/60 border-slate-200/90 text-slate-900";
    timeClass = "text-rose-700";
  }

  return (
    <section
      id="simulator-section"
      className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-8 shadow-xs flex flex-col gap-6 scroll-mt-20"
    >
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-slate-100 pb-5">
        <div>
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-slate-100 border border-slate-200 text-slate-700 text-xs font-mono font-semibold mb-2">
            <span>Automated Risk Protection</span>
          </div>
          <h2 className="text-2xl sm:text-3xl font-black text-slate-900 tracking-tight">
            How the Trailing Stop-Loss Locks Profit
          </h2>
          <p className="text-sm text-slate-600 mt-1 max-w-2xl leading-relaxed">
            Watch how our algorithm protects you throughout the full trading day (09:15 AM to 03:30 PM). As the share price climbs, the safety limit automatically follows it upward. When the market suddenly crashes, you exit with guaranteed profit while manual traders lose money.
          </p>
        </div>

        {/* Live Simulation Indicator */}
        <div className="flex items-center gap-2 px-3.5 py-2 rounded-2xl bg-emerald-50 border border-emerald-200/80 text-emerald-800 text-xs font-mono font-semibold shrink-0">
          <span>Market Simulation (09:15 AM - 03:30 PM)</span>
        </div>
      </div>

      {/* 4-Metric Real-Time HUD */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3.5">
        {/* Metric 1: Current Share Price with 12hr AM/PM */}
        <div className="p-4 rounded-2xl bg-slate-50 border border-slate-200/80">
          <span className="block text-[11px] font-mono text-slate-500 uppercase tracking-wider font-semibold">
            Current Price
          </span>
          <div className="flex items-baseline gap-2 mt-1">
            <span className="text-xl sm:text-2xl font-black font-mono text-slate-900">
              ₹{currentPrice.toFixed(2)}
            </span>
            <span className="text-xs font-mono text-slate-500">
              {format12Hour(currentPoint.time, true)}
            </span>
          </div>
        </div>

        {/* Metric 2: Position */}
        <div className="p-4 rounded-2xl bg-slate-50 border border-slate-200/80">
          <span className="block text-[11px] font-mono text-slate-500 uppercase tracking-wider font-semibold">
            Active Position
          </span>
          <div className="flex items-baseline gap-2 mt-1">
            <span className="text-xl sm:text-2xl font-black font-mono text-slate-900">
              100 Shares
            </span>
            <span className="text-xs font-mono text-slate-500">@ ₹1,000</span>
          </div>
        </div>

        {/* Metric 3: P&L with Taxes & Net In-Hand Subtext */}
        <div className="p-4 rounded-2xl bg-slate-50 border border-slate-200/80">
          <span className="block text-[11px] font-mono text-slate-500 uppercase tracking-wider font-semibold">
            {isExited ? "Locked Net Profit" : "Current Profit / Loss"}
          </span>
          <div className="flex items-baseline gap-2 mt-1">
            <span
              className={`text-xl sm:text-2xl font-black font-mono ${
                isPositive ? "text-emerald-700" : "text-rose-700"
              }`}
            >
              {isPositive ? "+" : ""}₹{pnlAmount.toFixed(2)}
            </span>
            <span
              className={`text-xs font-mono font-bold ${
                isPositive ? "text-emerald-600" : "text-rose-600"
              }`}
            >
              ({isPositive ? "+" : ""}{pnlPct.toFixed(2)}%)
            </span>
          </div>
          <span className="block text-[10px] text-slate-500 font-mono font-medium truncate mt-0.5">
            {isPositive
              ? `Est.Taxes: ₹84 • In-Hand: +₹${Math.max(0, pnlAmount - 84).toFixed(0)}`
              : `Tax: ₹0 (Cap Loss) • Net: -₹${Math.abs(pnlAmount).toFixed(0)}`}
          </span>
        </div>

        {/* Metric 4: Safety Net */}
        <div className="p-4 rounded-2xl bg-amber-50/60 border border-amber-200/80">
          <span className="block text-[11px] font-mono text-amber-900 uppercase tracking-wider font-semibold">
            Stop Loss 
          </span>
          <div className="flex items-baseline gap-2 mt-1">
            <span className="text-xl sm:text-2xl font-black font-mono text-amber-900">
              {currentStopLoss ? `₹${currentStopLoss.toFixed(2)}` : "Protected (Sold)"}
            </span>
          </div>
          <span className="block text-[10px] text-amber-800 font-medium truncate mt-0.5">
            {safetyStatus}
          </span>
        </div>
      </div>

      {/* Chart Canvas with Double-Click Blue Outline Prevention & Overflow Hidden */}
      <div
        className="w-full h-[320px] sm:h-[400px] select-none outline-none focus:outline-none focus:ring-0 focus-visible:outline-none [&_*]:outline-none [&_*]:focus:outline-none [&_svg]:outline-none overflow-hidden"
        style={{ outline: "none", WebkitTapHighlightColor: "transparent", userSelect: "none", overflow: "hidden" }}
      >
        <ResponsiveContainer width="100%" height="100%" className="outline-none focus:outline-none [&_*]:outline-none overflow-hidden">
          <AreaChart
            data={chartData}
            margin={{ top: 15, right: 15, left: -10, bottom: 0 }}
            className="outline-none focus:outline-none [&_*]:outline-none overflow-hidden [&_svg]:overflow-hidden"
            style={{ outline: "none", userSelect: "none", overflow: "hidden" }}
          >
            <defs>
              {/* Green Gradient for in-trade rally */}
              <linearGradient id="simGainGradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#10b981" stopOpacity={0.25} />
                <stop offset="95%" stopColor="#10b981" stopOpacity={0.0} />
              </linearGradient>

              {/* Red Gradient for post-exit market crash */}
              <linearGradient id="simCrashGradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#ef4444" stopOpacity={0.25} />
                <stop offset="95%" stopColor="#ef4444" stopOpacity={0.0} />
              </linearGradient>
            </defs>

            <CartesianGrid strokeDasharray="3 3" stroke="#f1f5f9" vertical={false} />

            {/* Stable Full-Day X-Axis with 12-Hour format (NO AM/PM) - Anchored Flush */}
            <XAxis
              dataKey="time"
              stroke="#94a3b8"
              fontSize={11}
              tickLine={false}
              axisLine={{ stroke: "#e2e8f0" }}
              interval="preserveStartEnd"
              minTickGap={32}
              tickFormatter={(v) => format12Hour(v, false)}
            />

            {/* Stable Fixed Y-Axis for Price */}
            <YAxis
              domain={[950, 1060]}
              stroke="#94a3b8"
              fontSize={11}
              tickLine={false}
              axisLine={{ stroke: "#e2e8f0" }}
              tickFormatter={(v) => `₹${v}`}
            />

            <Tooltip
              content={({ active, payload }) => {
                if (active && payload && payload.length) {
                  const pt = payload[0].payload;
                  if (pt.rawPrice === null || pt.rawPrice === undefined) return null;
                  return (
                    <div className="bg-white/95 border border-slate-200 rounded-xl p-2.5 shadow-lg backdrop-blur text-xs font-mono">
                      <div className="text-slate-500 font-semibold mb-1">
                        {format12Hour(pt.time, true)}
                      </div>
                      <div className="text-slate-900 font-bold">Price: ₹{pt.rawPrice.toFixed(2)}</div>
                      {pt.stopLoss && (
                        <div className="text-amber-700 font-bold">
                          Stop Loss: ₹{pt.stopLoss.toFixed(2)}
                        </div>
                      )}
                    </div>
                  );
                }
                return null;
              }}
            />

            {/* Entry Line at 1,000 */}
            <ReferenceLine
              y={1000.0}
              stroke="#64748b"
              strokeDasharray="4 4"
              strokeWidth={1.2}
              label={{
                value: "Entry: ₹1,000.00",
                fill: "#64748b",
                fontSize: 10,
                position: "insideTopLeft",
              }}
            />

            {/* Exit Trigger Reference Line with comfortable margin from edge and line */}
            {isExited && (
              <ReferenceLine
                y={1040.0}
                stroke="#10b981"
                strokeWidth={2}
                strokeDasharray="3 3"
                label={{
                  value: "Automated Exit Sold: ₹1,040.00 (+₹4,000)",
                  fill: "#059669",
                  fontSize: 10,
                  fontWeight: 700,
                  position: "insideTopRight",
                  dy: 15,
                  dx: -16,
                }}
              />
            )}

            {/* 1. Green Gain Area (09:15 AM to 02:35 PM Exit) */}
            <Area
              type="monotone"
              dataKey="gainPrice"
              stroke="#10b981"
              strokeWidth={2.5}
              fill="url(#simGainGradient)"
              isAnimationActive={false}
              connectNulls={false}
            />

            {/* 2. Red Crash Area (02:35 PM Exit down to 03:30 PM Shutdown @ ₹960) */}
            <Area
              type="monotone"
              dataKey="crashPrice"
              stroke="#ef4444"
              strokeWidth={2.5}
              fill="url(#simCrashGradient)"
              isAnimationActive={false}
              connectNulls={false}
            />

            {/* 3. Stepping Trailing Stop-Loss Line */}
            <Line
              type="stepAfter"
              dataKey="stopLoss"
              stroke="#f59e0b"
              strokeWidth={2.2}
              strokeDasharray="4 4"
              dot={false}
              isAnimationActive={false}
              connectNulls={false}
            />

            {/* Exit Marker Anchor Dot at ₹1,040 */}
            {isExited && (
              <ReferenceDot
                x={SIMULATION_TIMELINE[EXIT_INDEX].time}
                y={1040.0}
                r={6}
                fill="#10b981"
                stroke="#ffffff"
                strokeWidth={2}
              />
            )}

            {/* Live Current Price Dot (Green in trade, Red during crash) */}
            <ReferenceDot
              x={currentPoint.time}
              y={currentPoint.price}
              r={5}
              fill={isExited ? "#ef4444" : "#10b981"}
              stroke="#ffffff"
              strokeWidth={2}
            />

            {/* Live Trailing Stop-Loss Dot */}
            {currentStopLoss !== null && (
              <ReferenceDot
                x={currentPoint.time}
                y={currentStopLoss}
                r={4}
                fill="#f59e0b"
                stroke="#ffffff"
                strokeWidth={1.5}
              />
            )}
          </AreaChart>
        </ResponsiveContainer>
      </div>

      {/* Adaptive Real-Time Plain-English Explanation Banner with 12hr AM/PM */}
      <div
        className={`p-4 sm:p-5 rounded-2xl border transition-all duration-300 ${bannerClass}`}
      >
        <div className="w-full">
          <div className="flex items-center justify-between gap-2 mb-1.5 flex-wrap">
            <span className={`block text-[10px] font-mono uppercase tracking-wider font-bold ${timeClass}`}>
              Algorithm Live Log • {format12Hour(currentPoint.time, true)}
            </span>
            {/* {isMilestoneStep ? (
              <span className={`px-2 py-0.5 rounded-full text-[9px] font-mono font-bold uppercase border animate-pulse ${badgeClass}`}>
                Milestone Action (Paused 4s)
              </span>
            ) : (
              <span className={`px-2 py-0.5 rounded-full text-[9px] font-mono border ${badgeClass}`}>
                Tick Progress (1.8s)
              </span>
            )} */}
          </div>
          <p className="text-xs sm:text-sm font-medium leading-relaxed text-slate-800">
            {currentPoint.message}
          </p>
        </div>
      </div>

      {/* Permanently Visible Outcome Comparison Card (Never Hidden, Always Readable) */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* AlgoTrade Protected Outcome */}
        <div className="p-5 rounded-2xl bg-emerald-50 border border-emerald-200 flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between gap-2 mb-2">
              <span className="text-xs font-mono font-bold uppercase text-emerald-800 tracking-wider">
                With AlgoTrade Trailing Stop
              </span>
              <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-bold uppercase bg-emerald-100 text-emerald-800 border border-emerald-300">
                {isExited ? "Exit Executed @ 02:35 PM" : "Automated Protection"}
              </span>
            </div>
            <h4 className="text-xl font-black text-slate-900 mt-1">
              +₹4,000.00 Net Profit Protected
            </h4>
            <p className="text-xs text-emerald-800 mt-2 leading-relaxed">
              The algorithm automatically sold all 100 shares at ₹1,040 at 02:35 PM right as the reversal began. You locked in +4.0% profit without emotional hesitations.
            </p>
          </div>
          <div className="mt-4 pt-3 border-t border-emerald-200/80 flex items-center justify-between text-xs font-mono">
            <span className="text-emerald-800 font-semibold">Exit Price:</span>
            <strong className="text-emerald-900 font-bold">₹1,040.00 per share (Safe in Cash)</strong>
          </div>
        </div>

        {/* Manual Trader Outcome */}
        <div className="p-5 rounded-2xl bg-rose-50 border border-rose-200 flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between gap-2 mb-2">
              <span className="text-xs font-mono font-bold uppercase text-rose-800 tracking-wider">
                Manual Trader (Without Trailing Stop)
              </span>
              <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-bold uppercase bg-rose-100 text-rose-800 border border-rose-300">
                {isExited ? "Active Loss @ ₹960" : "Unprotected Downside"}
              </span>
            </div>
            <h4 className="text-xl font-black text-slate-900 mt-1">
              -₹4,000.00 Net Capital Loss
            </h4>
            <p className="text-xs text-rose-800 mt-2 leading-relaxed">
              A manual trader hoped the price would bounce back and did not sell. They rode the full drop all the way down to ₹960 at 03:30 PM Market Shutdown, turning a winning trade into a loss.
            </p>
          </div>
          <div className="mt-4 pt-3 border-t border-rose-200/80 flex items-center justify-between text-xs font-mono">
            <span className="text-rose-800 font-semibold">03:30 PM Shutdown Value:</span>
            <strong className="text-rose-900 font-bold">₹960.00 per share</strong>
          </div>
        </div>
      </div>
    </section>
  );
}
