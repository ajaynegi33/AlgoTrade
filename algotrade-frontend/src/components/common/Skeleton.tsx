"use client";

import React from "react";

export function Skeleton({ className = "" }: { className?: string }) {
  return (
    <div
      className={`animate-pulse bg-gradient-to-r from-slate-800/70 via-slate-700/50 to-slate-800/70 rounded-lg ${className}`}
    />
  );
}

export function SkeletonCard({ className = "" }: { className?: string }) {
  return (
    <div
      className={`bg-slate-900/60 border border-slate-800/80 rounded-xl p-4 backdrop-blur-sm ${className}`}
    >
      <Skeleton className="h-3 w-24 mb-2.5" />
      <Skeleton className="h-7 w-36 mb-2" />
      <Skeleton className="h-2.5 w-20" />
    </div>
  );
}

export function SkeletonChart({ height = "h-[400px]" }: { height?: string }) {
  return (
    <div
      className={`w-full bg-slate-950/60 rounded-xl border border-slate-800/80 p-5 shadow-inner flex flex-col justify-between ${height}`}
    >
      {/* Top Header */}
      <div className="flex items-center justify-between pb-3 border-b border-slate-800/60">
        <div className="flex items-center gap-3">
          <Skeleton className="h-4 w-44" />
          <Skeleton className="h-4 w-16 rounded-full" />
        </div>
        <div className="flex items-center gap-3">
          <Skeleton className="h-3 w-28" />
          <Skeleton className="h-4 w-24" />
        </div>
      </div>

      {/* Center Simulated Chart Waveform */}
      <div className="flex-1 my-4 relative flex items-center justify-center overflow-hidden">
        <svg
          className="w-full h-full opacity-20 text-emerald-500 animate-pulse"
          preserveAspectRatio="none"
          viewBox="0 0 500 150"
        >
          <path
            d="M0,120 Q50,60 100,90 T200,40 T300,100 T400,30 T500,70 L500,150 L0,150 Z"
            fill="currentColor"
            opacity="0.3"
          />
          <path
            d="M0,120 Q50,60 100,90 T200,40 T300,100 T400,30 T500,70"
            fill="none"
            stroke="currentColor"
            strokeWidth="3"
          />
        </svg>

        {/* Ambient shimmer overlay */}
        <div className="absolute inset-0 bg-gradient-to-t from-slate-950/80 via-transparent to-transparent pointer-events-none" />
        <div className="absolute flex flex-col items-center justify-center">
          <div className="px-3.5 py-1.5 rounded-full bg-slate-900/90 border border-slate-700/80 text-[11px] font-mono text-slate-300 shadow-xl flex items-center gap-2 backdrop-blur">
            <span className="w-2 h-2 rounded-full bg-emerald-400 animate-ping" />
            Loading real-time NSE trajectory...
          </div>
        </div>
      </div>

      {/* Bottom X-axis markers */}
      <div className="flex items-center justify-between pt-2 border-t border-slate-800/60">
        <Skeleton className="h-2.5 w-12" />
        <Skeleton className="h-2.5 w-12" />
        <Skeleton className="h-2.5 w-12" />
        <Skeleton className="h-2.5 w-12" />
        <Skeleton className="h-2.5 w-12" />
        <Skeleton className="h-2.5 w-12" />
      </div>
    </div>
  );
}

export function SkeletonMarketDepth() {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
      {/* Buy Ladder Skeleton */}
      <div className="bg-slate-950/70 border border-slate-800 rounded-xl p-4 space-y-3">
        <div className="flex items-center justify-between pb-2 border-b border-slate-800">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-3 w-16" />
        </div>
        {[1, 2, 3, 4, 5].map((i) => (
          <div key={i} className="flex items-center justify-between py-1.5">
            <Skeleton className="h-3.5 w-16" />
            <Skeleton className="h-3.5 w-20" />
            <Skeleton className="h-3.5 w-10" />
          </div>
        ))}
      </div>

      {/* Sell Ladder Skeleton */}
      <div className="bg-slate-950/70 border border-slate-800 rounded-xl p-4 space-y-3">
        <div className="flex items-center justify-between pb-2 border-b border-slate-800">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-3 w-16" />
        </div>
        {[1, 2, 3, 4, 5].map((i) => (
          <div key={i} className="flex items-center justify-between py-1.5">
            <Skeleton className="h-3.5 w-16" />
            <Skeleton className="h-3.5 w-20" />
            <Skeleton className="h-3.5 w-10" />
          </div>
        ))}
      </div>
    </div>
  );
}

export function SkeletonStockExplorer() {
  return (
    <div className="space-y-6 animate-fadeIn">
      {/* Main Price Card Skeleton */}
      <div className="bg-gradient-to-br from-slate-900 to-slate-900/90 border border-slate-800 rounded-2xl p-6 sm:p-7 shadow-2xl space-y-6">
        <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 pb-6 border-b border-slate-800">
          <div className="space-y-2">
            <div className="flex items-center gap-3">
              <Skeleton className="h-8 w-44" />
              <Skeleton className="h-5 w-24 rounded-full" />
            </div>
            <Skeleton className="h-3.5 w-64" />
          </div>
          <div className="space-y-2 text-right">
            <Skeleton className="h-3.5 w-32 ml-auto" />
            <Skeleton className="h-9 w-40 ml-auto" />
          </div>
        </div>

        {/* Chart Skeleton */}
        <SkeletonChart height="h-[380px] sm:h-[420px]" />

        {/* OHLC Metric Grid Skeleton */}
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 sm:gap-4">
          {[1, 2, 3, 4].map((i) => (
            <div key={i} className="bg-slate-950/60 rounded-xl p-3.5 border border-slate-800 space-y-2">
              <Skeleton className="h-3 w-16" />
              <Skeleton className="h-6 w-24" />
            </div>
          ))}
        </div>

        {/* Secondary Metrics */}
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 sm:gap-4">
          {[1, 2, 3, 4].map((i) => (
            <div key={i} className="bg-slate-950/40 rounded-xl p-3 border border-slate-800/80 space-y-1.5">
              <Skeleton className="h-2.5 w-20" />
              <Skeleton className="h-5 w-28" />
            </div>
          ))}
        </div>
      </div>

      {/* Market Depth Ladder Skeleton */}
      <SkeletonMarketDepth />
    </div>
  );
}

export function SkeletonOverview() {
  return (
    <div className="flex flex-col gap-6 animate-fadeIn">
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-4">
        {[1, 2, 3, 4].map((i) => (
          <SkeletonCard key={i} />
        ))}
      </div>
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-6 space-y-4">
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-24 w-full" />
      </div>
    </div>
  );
}

export function SkeletonLeaderboard() {
  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-2xl overflow-hidden shadow-xl animate-fadeIn">
      <div className="p-4 sm:p-6 border-b border-slate-800 flex items-center justify-between">
        <Skeleton className="h-6 w-64" />
        <Skeleton className="h-3.5 w-28" />
      </div>
      <div className="p-4 space-y-3">
        {[1, 2, 3, 4, 5, 6].map((i) => (
          <div key={i} className="flex items-center justify-between py-2.5 border-b border-slate-800/50">
            <Skeleton className="h-4 w-12" />
            <Skeleton className="h-4 w-28" />
            <Skeleton className="h-4 w-20" />
            <Skeleton className="h-4 w-16" />
            <Skeleton className="h-4 w-16" />
            <Skeleton className="h-4 w-20 rounded-full" />
          </div>
        ))}
      </div>
    </div>
  );
}

export function SkeletonAnalytics() {
  return (
    <div className="flex flex-col gap-6 animate-fadeIn">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {[1, 2, 3, 4].map((i) => (
          <SkeletonCard key={i} />
        ))}
      </div>
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-6 space-y-4">
        <Skeleton className="h-5 w-64" />
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
          {[1, 2, 3, 4, 5, 6].map((i) => (
            <div key={i} className="bg-slate-950/70 p-3 rounded-lg border border-slate-800 space-y-2">
              <Skeleton className="h-3 w-16" />
              <Skeleton className="h-4 w-20" />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
