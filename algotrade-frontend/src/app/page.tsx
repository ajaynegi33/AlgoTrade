"use client";

import React, { useCallback } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "../context/AuthContext";
import Navbar from "@/components/home/Navbar";
import SmoothGrowthChart from "@/components/home/SmoothGrowthChart";
import EngineStrategyPipeline from "@/components/home/EngineStrategyPipeline";
import TrailingStopSimulator from "@/components/home/TrailingStopSimulator";
import { toast } from "sonner";

export default function Home() {
  const { isAuthenticated } = useAuth();
  const router = useRouter();

  const handleLaunchDashboard = useCallback(() => {
    if (isAuthenticated) {
      router.push("/dashboard");
    } else {
      router.push("/login");
    }
  }, [isAuthenticated, router]);

  const handleSimulateTradeSignal = useCallback(() => {
    toast.success("⚡ Real-Time Breakout Signal Triggered", {
      description: "BUY 150 RELIANCE @ ₹2,980.50 | Hard Trailing Stop at ₹2,935.00 | Queue Depth: 68% Bids",
      duration: 4500,
    });
  }, []);

  return (
    <div className="min-h-screen bg-slate-50/70 text-slate-900 flex flex-col font-sans selection:bg-emerald-500/20 selection:text-emerald-900">
      {/* Top Sticky Navbar with Logo from public/AlgoTrade.png */}
      <Navbar />

      {/* Main Home Content */}
      <main className="flex-1 max-w-7xl mx-auto w-full px-3.5 sm:px-6 lg:px-8 py-8 sm:py-12 flex flex-col gap-10 sm:gap-14">
        {/* Hero Section */}
        <section className="flex flex-col items-center text-center max-w-4xl mx-auto pt-4 sm:pt-8 px-1">
          {/* Autonomous Status Badge */}
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-slate-100 border border-slate-200/90 text-xs font-mono font-semibold text-slate-700 mb-5 shadow-2xs">
            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
            <span>Autonomous High-Frequency Execution</span>
          </div>

          {/* Main Title */}
          <h1 className="text-3xl sm:text-5xl lg:text-6xl font-black text-slate-900 tracking-tight leading-[1.12] font-sans">
            Autonomous Breakout Engine for{" "}
            <span className="text-transparent bg-clip-text bg-gradient-to-r from-emerald-600 via-teal-600 to-cyan-700">
              NSE Equities.
            </span>
          </h1>

          {/* Subtitle */}
          <p className="mt-4 sm:mt-5 text-sm sm:text-lg text-slate-600 max-w-2xl leading-relaxed">
            Eliminate emotional errors and hesitation. AlgoTrade scans high-momentum intraday breakouts at the opening bell, automatically locking in gains with disciplined trailing stop-losses.
          </p>

          {/* Action CTAs */}
          <div className="flex flex-col sm:flex-row items-center gap-3 mt-7 w-full sm:w-auto">
            <button
              type="button"
              onClick={handleLaunchDashboard}
              className="w-full sm:w-auto px-7 py-3.5 rounded-2xl bg-slate-900 hover:bg-slate-800 text-white font-bold text-sm tracking-wide shadow-sm transition-all flex items-center justify-center gap-2 cursor-pointer active:scale-95"
            >
              <span>⚡</span>
              <span>Launch Live Terminal</span>
              <span>→</span>
            </button>

            <button
              type="button"
              onClick={handleSimulateTradeSignal}
              className="w-full sm:w-auto px-6 py-3.5 rounded-2xl bg-white hover:bg-slate-50 text-slate-800 border border-slate-200/90 font-semibold text-sm shadow-2xs transition-all flex items-center justify-center gap-2 cursor-pointer active:scale-95"
            >
              <span>🔔</span>
              <span>Simulate Trade Signal</span>
            </button>
          </div>

          {/* KPI Ribbon Strip */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5 sm:gap-4 mt-10 w-full max-w-3xl">
            <div className="p-3.5 rounded-2xl bg-white border border-slate-200/80 shadow-2xs text-left">
              <span className="text-[10px] font-mono text-slate-500 uppercase tracking-wider block">Strategy Peak</span>
              <span className="text-base sm:text-lg font-black font-mono text-slate-900">₹148,520</span>
              <span className="text-[10px] font-semibold text-emerald-600 block mt-0.5">+48.5% Net Alpha</span>
            </div>

            <div className="p-3.5 rounded-2xl bg-white border border-slate-200/80 shadow-2xs text-left">
              <span className="text-[10px] font-mono text-slate-500 uppercase tracking-wider block">Execution Speed</span>
              <span className="text-base sm:text-lg font-black font-mono text-slate-900">&lt; 15ms</span>
              <span className="text-[10px] font-semibold text-slate-500 block mt-0.5">Sub-millisecond direct</span>
            </div>

            <div className="p-3.5 rounded-2xl bg-white border border-slate-200/80 shadow-2xs text-left">
              <span className="text-[10px] font-mono text-slate-500 uppercase tracking-wider block">Win Ratio</span>
              <span className="text-base sm:text-lg font-black font-mono text-emerald-700">78.4%</span>
              <span className="text-[10px] font-semibold text-slate-500 block mt-0.5">High probability wave</span>
            </div>

            <div className="p-3.5 rounded-2xl bg-white border border-slate-200/80 shadow-2xs text-left">
              <span className="text-[10px] font-mono text-slate-500 uppercase tracking-wider block">Risk Cap</span>
              <span className="text-base sm:text-lg font-black font-mono text-amber-800">1.5% Max</span>
              <span className="text-[10px] font-semibold text-amber-700 block mt-0.5">Hard Stop floor</span>
            </div>
          </div>
        </section>

        {/* 1. Smooth Monotone All-Time Strategy Chart */}
        <SmoothGrowthChart />

        {/* 2. 3-Step Execution Pipeline */}
        <EngineStrategyPipeline />

        {/* 3. Interactive Full-Day Trailing Stop Simulator */}
        <TrailingStopSimulator />

        {/* 4. Institutional Bento Grid Capabilities */}
        <section id="features-section" className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 sm:gap-5 scroll-mt-20">
          {/* Tile 1 */}
          <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs flex flex-col justify-between">
            <div>
              <div className="w-10 h-10 rounded-2xl bg-emerald-50 text-emerald-700 flex items-center justify-center text-lg font-bold mb-3.5">
                ⚡
              </div>
              <h3 className="text-base font-bold text-slate-900 tracking-tight">
                0ms WebSocket Stream
              </h3>
              <p className="text-xs text-slate-600 leading-relaxed mt-1.5">
                Eliminates slow HTTP polling. Ticks and order updates stream directly into client memory with sub-millisecond precision.
              </p>
            </div>
            <div className="mt-5 pt-3 border-t border-slate-100 flex items-center justify-between text-[11px] font-mono text-slate-500">
              <span>Telemetry:</span>
              <strong className="text-emerald-700 font-bold">100% Real-Time</strong>
            </div>
          </div>

          {/* Tile 2 */}
          <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs flex flex-col justify-between">
            <div>
              <div className="w-10 h-10 rounded-2xl bg-sky-50 text-sky-700 flex items-center justify-center text-lg font-bold mb-3.5">
                📊
              </div>
              <h3 className="text-base font-bold text-slate-900 tracking-tight">
                Level-2 Order Book
              </h3>
              <p className="text-xs text-slate-600 leading-relaxed mt-1.5">
                5-level bid-ask depth inspects buy-side wall defenses before routing orders to prevent slippage.
              </p>
            </div>
            <div className="mt-5 pt-3 border-t border-slate-100 flex items-center justify-between text-[11px] font-mono text-slate-500">
              <span>Book Depth:</span>
              <strong className="text-sky-700 font-bold">5 Levels / 0.05 Tick</strong>
            </div>
          </div>

          {/* Tile 3 */}
          <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs flex flex-col justify-between">
            <div>
              <div className="w-10 h-10 rounded-2xl bg-purple-50 text-purple-700 flex items-center justify-center text-lg font-bold mb-3.5">
                🔑
              </div>
              <h3 className="text-base font-bold text-slate-900 tracking-tight">
                Automated 2FA Renewal
              </h3>
              <p className="text-xs text-slate-600 leading-relaxed mt-1.5">
                Headless TOTP integration automatically refreshes Zerodha Kite trading sessions daily before the 9:15 AM opening bell.
              </p>
            </div>
            <div className="mt-5 pt-3 border-t border-slate-100 flex items-center justify-between text-[11px] font-mono text-slate-500">
              <span>Daily Auto-Login:</span>
              <strong className="text-purple-700 font-bold">Active at 8:30 AM</strong>
            </div>
          </div>

          {/* Tile 4 */}
          <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs flex flex-col justify-between">
            <div>
              <div className="w-10 h-10 rounded-2xl bg-amber-50 text-amber-700 flex items-center justify-center text-lg font-bold mb-3.5">
                🛡️
              </div>
              <h3 className="text-base font-bold text-slate-900 tracking-tight">
                Dynamic Trailing Stop
              </h3>
              <p className="text-xs text-slate-600 leading-relaxed mt-1.5">
                High-watermark profit locking ratchets stop losses up during breakouts while strictly capping downside risk at 1.5%.
              </p>
            </div>
            <div className="mt-5 pt-3 border-t border-slate-100 flex items-center justify-between text-[11px] font-mono text-slate-500">
              <span>Capital Risk Cap:</span>
              <strong className="text-amber-800 font-bold">Strict 1.5% Floor</strong>
            </div>
          </div>
        </section>

        {/* Bottom Fast Launch Strip */}
        <section className="bg-slate-900 text-white rounded-3xl p-6 sm:p-8 flex flex-col sm:flex-row items-center justify-between gap-6 shadow-md">
          <div className="space-y-1 text-center sm:text-left">
            <h2 className="text-xl sm:text-2xl font-bold tracking-tight">
              Ready to monitor continuous autonomous signals?
            </h2>
            <p className="text-xs sm:text-sm text-slate-400">
              Authenticate into your secure session and inspect live algorithmic positions.
            </p>
          </div>

          <button
            type="button"
            onClick={handleLaunchDashboard}
            className="w-full sm:w-auto px-7 py-3 rounded-2xl bg-white hover:bg-slate-100 text-slate-900 font-bold text-xs sm:text-sm tracking-wide transition-all shrink-0 cursor-pointer shadow-xs active:scale-95 text-center"
          >
            Open Trading Terminal →
          </button>
        </section>
      </main>

      {/* Footer */}
      <footer className="border-t border-slate-200/80 bg-white py-6 text-center text-xs text-slate-500 font-sans">
        <div className="max-w-7xl mx-auto px-4 flex flex-col sm:flex-row items-center justify-between gap-2">
          <span>AlgoTrade Autonomous High-Frequency Engine. For demonstration &amp; verified trading.</span>
          <span className="font-mono text-[11px] text-slate-400">JWT Dual-Token (5m / 24h RTR) Active</span>
        </div>
      </footer>
    </div>
  );
}
