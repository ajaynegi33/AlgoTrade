"use client";

import React, { useEffect, useState, useMemo, useCallback, Suspense } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { getStockDetail } from "@/lib/api";
import { StockDetailData } from "@/lib/types";
import { useTradingStore } from "@/store/useTradingStore";
import { useEngineWebSocket } from "@/hooks/useEngineWebSocket";
import { StockHeaderHero } from "@/components/stock/StockHeaderHero";
import { StockIntradayChart } from "@/components/stock/StockIntradayChart";
import { StockOrderBook5Level } from "@/components/stock/StockOrderBook5Level";
import { StockFalsificationGrid } from "@/components/stock/StockFalsificationGrid";
import { StockPositionCalculator } from "@/components/stock/StockPositionCalculator";

function StockDetailSkeleton() {
  return (
    <div className="space-y-6 animate-pulse w-full font-mono">
      {/* Hero Banner skeleton */}
      <div className="h-44 bg-slate-200/70 rounded-3xl w-full" />
      {/* Chart skeleton */}
      <div className="h-96 bg-slate-200/70 rounded-3xl w-full" />
      {/* Order Book Depth skeleton */}
      <div className="h-72 bg-slate-200/70 rounded-3xl w-full" />
      {/* Falsification Grid skeleton */}
      <div className="h-48 bg-slate-200/70 rounded-3xl w-full" />
      {/* Risk Calculator skeleton */}
      <div className="h-80 bg-slate-200/70 rounded-3xl w-full" />
    </div>
  );
}

function StockDetailContent() {
  const params = useParams();

  const symbolParam = typeof params?.symbol === "string" ? params.symbol : "";
  const symbol = decodeURIComponent(symbolParam).trim().toUpperCase();

  const [baseStock, setBaseStock] = useState<StockDetailData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const { executionState } = useTradingStore();
  const { snapshot } = useEngineWebSocket();

  // Load stock details on mount and refresh every 3 seconds
  useEffect(() => {
    if (!symbol) return;
    let isCancelled = false;

    const loadData = async () => {
      try {
        const data = await getStockDetail(symbol);
        if (!isCancelled) {
          setBaseStock(data);
          setError(null);
          setLoading(false);
        }
      } catch (err: unknown) {
        if (!isCancelled) {
          setError(err instanceof Error ? err.message : `Failed to load stock ${symbol}`);
          setLoading(false);
        }
      }
    };

    loadData();

    // 3-second background polling to keep 5-level order book and 5m bars updated
    const timer = setInterval(() => {
      if (!document.hidden) {
        getStockDetail(symbol).then(
          (data) => {
            if (!isCancelled) setBaseStock(data);
          },
          () => {}
        );
      }
    }, 3000);

    return () => {
      isCancelled = true;
      clearInterval(timer);
    };
  }, [symbol]);

  // Derive live stock by overlaying 1.0s WebSocket tick updates directly onto base stock
  const stock: StockDetailData | null = useMemo(() => {
    if (!baseStock) return null;
    if (!snapshot?.all_stocks || snapshot.all_stocks.length === 0) return baseStock;

    const liveMatch = snapshot.all_stocks.find((s) => s.symbol.toUpperCase() === symbol);
    if (!liveMatch) return baseStock;

    const vwapDist = liveMatch.vwap > 0 ? liveMatch.ltp - liveMatch.vwap : baseStock.vwap_distance;
    const vwapDistPct = liveMatch.vwap > 0 ? ((liveMatch.ltp - liveMatch.vwap) / liveMatch.vwap) * 100 : baseStock.vwap_distance_pct;
    const liveChange = baseStock.prev_close > 0 ? liveMatch.ltp - baseStock.prev_close : baseStock.change;

    return {
      ...baseStock,
      ltp: liveMatch.ltp,
      change: liveChange,
      change_percent: liveMatch.change_percent,
      vwap: liveMatch.vwap,
      vwap_distance: vwapDist,
      vwap_distance_pct: vwapDistPct,
      bullish_score: liveMatch.bullish_score,
      bearish_score: liveMatch.bearish_score,
      rvol_session: liveMatch.rvol_session,
      rvol_pace_5m: liveMatch.rvol_pace_5m,
      composite_rs: liveMatch.composite_rs,
      buyer_ratio: liveMatch.buyer_ratio,
      aggressor_buy_ratio: liveMatch.aggressor_buy_ratio,
      ev: liveMatch.ev,
      p_win: liveMatch.p_win,
      stop_proxy: liveMatch.stop_proxy,
      target_proxy: liveMatch.target_proxy,
      last_updated: snapshot?.timestamp || baseStock.last_updated,
    };
  }, [baseStock, snapshot, symbol]);

  // Find if this stock currently has an active intraday position in the engine
  const activePosition = useMemo(() => {
    const livePositions = snapshot?.execution?.active_positions ?? executionState?.active_positions ?? [];
    return livePositions.find((p) => p.symbol.toUpperCase() === symbol) || null;
  }, [snapshot, executionState, symbol]);

  const handleManualRefresh = useCallback(async () => {
    if (!symbol) return;
    try {
      const data = await getStockDetail(symbol);
      setBaseStock(data);
      setError(null);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : `Failed to refresh stock ${symbol}`);
    }
  }, [symbol]);

  // Loading Skeleton State
  if (loading && !stock) {
    return <StockDetailSkeleton />;
  }

  // Error State
  if (error || !stock) {
    return (
      <div className="min-h-[500px] flex items-center justify-center p-6 font-mono">
        <div className="max-w-md w-full bg-white border border-rose-200 rounded-3xl p-8 text-center shadow-xs">
          <div className="w-14 h-14 mx-auto mb-4 rounded-2xl bg-rose-50 border border-rose-200 flex items-center justify-center text-rose-600 text-2xl">
            ⚠
          </div>
          <h2 className="text-lg font-bold text-slate-800 mb-2">
            Stock Not Found: {symbol}
          </h2>
          <p className="text-xs text-slate-500 mb-6 leading-relaxed">
            {error || `Unable to locate trading metrics or tick stream for "${symbol}" in the active NSE F&O universe.`}
          </p>
          <div className="flex items-center justify-center gap-3">
            <button
              type="button"
              onClick={handleManualRefresh}
              className="px-4 py-2 rounded-xl bg-slate-900 text-white text-xs font-semibold hover:bg-slate-800 transition-colors cursor-pointer"
            >
              Retry Connection
            </button>
            <Link
              href="/leaderboard"
              className="px-4 py-2 rounded-xl bg-slate-100 text-slate-700 text-xs font-semibold hover:bg-slate-200 transition-colors"
            >
              Back to Watchlist
            </Link>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6 w-full flex flex-col font-mono">
      {/* 1. Hero Stock Header Banner */}
      <StockHeaderHero stock={stock} />

      {/* 2. 5-Minute Intraday Terminal Chart with 9 EMA, VWAP & In-Trade Overlays */}
      <StockIntradayChart
        bars={stock.bars || []}
        vwap={stock.vwap}
        orh={stock.orh}
        orl={stock.orl}
        hasOR15m={stock.has_or15m}
        dayHigh={stock.high}
        dayLow={stock.low}
        changePercent={stock.change_percent}
        activePosition={activePosition}
      />

      {/* 3. Institutional 5-Level Order Book & Pressure */}
      <StockOrderBook5Level
        bids={stock.bids || []}
        asks={stock.asks || []}
        totalBidQty={stock.total_bid_qty}
        totalAskQty={stock.total_ask_qty}
        antiSpoofRatio={stock.anti_spoof_ratio}
        aggressorBuyRatio={stock.aggressor_buy_ratio}
        ltp={stock.ltp}
      />

      {/* 4. Falsification Mathematical Signals Grid */}
      <StockFalsificationGrid stock={stock} />

      {/* 5. Autonomous Engine Execution Specification */}
      <StockPositionCalculator stock={stock} />
    </div>
  );
}

export default function StockDetailPage() {
  return (
    <Suspense fallback={<StockDetailSkeleton />}>
      <StockDetailContent />
    </Suspense>
  );
}

