"use client";

import React, { useState, useEffect, useRef } from "react";
import { StockQuoteData, StockChartPoint, ZerodhaSessionData, ChartTimeframe, ChartInterval, ChartType } from "../types";
import StockPriceChart from "./StockPriceChart";
import MarketDepthWidget from "./MarketDepthWidget";
import StockMetricsOverview from "./StockMetricsOverview";
import ZerodhaAuthCard from "../auth/ZerodhaAuthCard";
import { SkeletonStockExplorer } from "../common/Skeleton";
import { TickUpdate } from "@/hooks/useEngineWebSocket";
import { apiFetch } from "@/lib/apiClient";
import { StockSearchCombobox } from "./StockSearchCombobox";

interface StockExplorerTabProps {
  backendUrl: string;
  session: ZerodhaSessionData | null;
  loginUrl: string | null;
  onAutoLogin: (password?: string, totpSecret?: string) => Promise<void>;
  onManualTokenSubmit: (token: string) => Promise<void>;
  activatingToken: boolean;
  autoLoggingIn: boolean;
  popularStocks: string[];
  latestTicks?: Record<string, TickUpdate>;
  subscribeSymbol?: (symbol: string) => void;
}

export default function StockExplorerTab({
  backendUrl,
  session,
  loginUrl,
  onAutoLogin,
  onManualTokenSubmit,
  activatingToken,
  autoLoggingIn,
  popularStocks,
  latestTicks,
  subscribeSymbol,
}: StockExplorerTabProps) {
  // Active inspected symbol (only updated on successful fetch)
  const [activeSymbol, setActiveSymbol] = useState("RELIANCE");
  const [quote, setQuote] = useState<StockQuoteData | null>(null);
  const [chartData, setChartData] = useState<StockChartPoint[]>([]);
  const [selectedTimeframe, setSelectedTimeframe] = useState<ChartTimeframe>("1D");
  const [selectedInterval, setSelectedInterval] = useState<ChartInterval>("5m");
  const [chartType, setChartType] = useState<ChartType>("area");
  const [loading, setLoading] = useState(false);
  const [chartLoading, setChartLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isLiveStreaming, setIsLiveStreaming] = useState(true);
  const [lastRefreshed, setLastRefreshed] = useState<Date | null>(null);

  // Micro-animation flash direction on live price ticks
  const [flashDirection, setFlashDirection] = useState<"up" | "down" | null>(null);
  const flashTimeoutRef = useRef<NodeJS.Timeout | null>(null);

  // Discretionary Manual Entry modal state
  const [showManualEntryModal, setShowManualEntryModal] = useState(false);
  const [manualQty, setManualQty] = useState<string>("");
  const [manualStopPrice, setManualStopPrice] = useState<string>("");
  const [executingOrder, setExecutingOrder] = useState(false);
  const [manualEntryAlert, setManualEntryAlert] = useState<{
    type: "success" | "error";
    message: string;
  } | null>(null);

  // Tracks active symbol to prevent interval race condition
  const activeSymbolRef = useRef(activeSymbol);
  activeSymbolRef.current = activeSymbol;

  // Tracks the last symbol, timeframe, and interval for which chart was loaded to avoid re-fetching on every poll
  const lastChartKeyRef = useRef<string>("");

  // Fetch chart data smoothly with selected timeframe and interval
  const fetchChartData = async (
    clean: string,
    tf: ChartTimeframe = selectedTimeframe,
    intvl: ChartInterval = selectedInterval,
    silent = false
  ) => {
    const chartKey = `${clean}_${tf}_${intvl}`;
    if (!silent && (chartData.length === 0 || lastChartKeyRef.current !== chartKey)) {
      setChartLoading(true);
    }
    try {
      const res = await apiFetch(
        `/api/dashboard/stock-chart?symbol=${encodeURIComponent(clean)}&timeframe=${encodeURIComponent(tf)}&interval=${encodeURIComponent(intvl)}`
      );
      if (res.ok) {
        const points: StockChartPoint[] = await res.json();
        if (Array.isArray(points)) {
          setChartData(points);
          lastChartKeyRef.current = chartKey;
        }
      }
    } catch (err) {
      console.warn("Chart fetch error:", err);
    } finally {
      setChartLoading(false);
    }
  };

  // Re-fetch chart data whenever the user toggles timeframe (1D, 1W, 1M, etc.) or interval (1m, 3m, 5m, 15m)
  useEffect(() => {
    if (activeSymbol) {
      const clean = activeSymbol.trim().toUpperCase().replace("NSE:", "");
      fetchChartData(clean, selectedTimeframe, selectedInterval, false);
    }
  }, [selectedTimeframe, selectedInterval]);

  // Fetch quote & chart data with seamless in-place validation (no loader flashes)
  const fetchSymbolData = async (symbolToFetch: string, isSilent = false) => {
    if (!symbolToFetch.trim()) return;
    const clean = symbolToFetch.trim().toUpperCase().replace("NSE:", "");

    // Only set top-level loading if this is an explicit user action and we don't have any quote yet
    if (!isSilent && !quote) {
      setLoading(true);
      setError(null);
    }

    try {
      // 1. Fetch live quote & depth
      const res = await apiFetch(`/api/dashboard/stock-quote?symbol=${encodeURIComponent(clean)}`);
      if (res.ok) {
        const data: StockQuoteData = await res.json();
        if (data.error === "ZERODHA_SESSION_REQUIRED" || !data.authenticated) {
          if (!isSilent) {
            setQuote(null);
            setChartData([]);
            setError("Zerodha session required. Please authenticate your Kite account below to stream live quotes from NSE.");
            setActiveSymbol("");
          }
        } else if (data.error === "SYMBOL_NOT_FOUND" || data.error) {
          if (!isSilent) {
            setQuote(null);
            setChartData([]);
            setError(data.message || `Symbol "${clean}" was not found on NSE. Please verify the trading symbol.`);
            setActiveSymbol("");
          }
        } else if (data.symbol && typeof data.lastPrice === "number") {
          // Valid incoming data: update smoothly without clearing or flashing
          setQuote(data);
          setActiveSymbol(clean);
          setError(null);
          setLastRefreshed(new Date());

          // 2. Fetch chart data if symbol/timeframe/interval changed or not yet loaded
          const chartKey = `${clean}_${selectedTimeframe}_${selectedInterval}`;
          if (lastChartKeyRef.current !== chartKey || chartData.length === 0) {
            fetchChartData(clean, selectedTimeframe, selectedInterval, isSilent);
          }
        }
      } else {
        if (!isSilent) {
          setQuote(null);
          setChartData([]);
          setError(`Failed to fetch quote for ${clean} (HTTP ${res.status})`);
          setActiveSymbol("");
        }
      }
    } catch (err: unknown) {
      if (!isSilent) {
        const msg = err instanceof Error ? err.message : String(err);
        setQuote(null);
        setChartData([]);
        setError(`Network error fetching quote for ${clean}: ${msg}`);
        setActiveSymbol("");
      }
    } finally {
      if (!isSilent) {
        setLoading(false);
      }
    }
  };

  // Initial load
  useEffect(() => {
    if (session?.active && activeSymbol) {
      fetchSymbolData(activeSymbol, false);
    }
  }, [session?.active]);

  // Dynamic WebSocket subscription when activeSymbol changes
  useEffect(() => {
    if (activeSymbol && subscribeSymbol) {
      subscribeSymbol(activeSymbol);
    }
  }, [activeSymbol, subscribeSymbol]);

  // Real-time WebSocket tick integration (zero HTTP polling lag)
  useEffect(() => {
    if (!activeSymbol || !latestTicks || !isLiveStreaming) return;
    const clean = activeSymbol.trim().toUpperCase().replace("NSE:", "");
    const tick = latestTicks[clean];
    if (!tick) return;

    setQuote((prevQuote) => {
      if (!prevQuote) return prevQuote;

      const prevLtp = prevQuote.lastPrice || 0;
      if (typeof tick.ltp === "number" && tick.ltp !== prevLtp) {
        const dir = tick.ltp > prevLtp ? "up" : "down";
        setFlashDirection(dir);
        if (flashTimeoutRef.current) clearTimeout(flashTimeoutRef.current);
        flashTimeoutRef.current = setTimeout(() => setFlashDirection(null), 600);
      }

      const prevClose = prevQuote.ohlc?.close;
      const ltp = tick.ltp ?? prevQuote.lastPrice;
      const changeVal: number = typeof tick.change === "number" ? tick.change : (prevClose && prevClose > 0 ? ltp - prevClose : (prevQuote.change ?? 0));
      const changePctVal: number = typeof tick.changePct === "number" ? tick.changePct : (prevClose && prevClose > 0 ? (changeVal / prevClose) * 100 : (prevQuote.changePct ?? 0));

      const updatedOhlc = prevQuote.ohlc
        ? {
            open: typeof tick.open === "number" && tick.open > 0 ? tick.open : prevQuote.ohlc.open,
            close: typeof tick.close === "number" && tick.close > 0 ? tick.close : prevQuote.ohlc.close,
            high: tick.high ? Math.max(prevQuote.ohlc.high, tick.high) : prevQuote.ohlc.high,
            low: tick.low ? Math.min(prevQuote.ohlc.low, tick.low) : prevQuote.ohlc.low,
          }
        : undefined;

      const updatedAveragePrice =
        typeof tick.averagePrice === "number" && tick.averagePrice > 0
          ? tick.averagePrice
          : prevQuote.averagePrice;
      const updatedLTQ =
        typeof tick.lastTradedQuantity === "number" && tick.lastTradedQuantity > 0
          ? tick.lastTradedQuantity
          : prevQuote.lastTradedQuantity;
      const updatedLTT =
        tick.lastTradedTime && tick.lastTradedTime.trim() !== ""
          ? tick.lastTradedTime
          : prevQuote.lastTradedTime;

      const updatedTotalBuy =
        typeof tick.totalBuyQuantity === "number" && tick.totalBuyQuantity > 0
          ? tick.totalBuyQuantity
          : prevQuote.totalBuyQuantity;
      const updatedTotalSell =
        typeof tick.totalSellQuantity === "number" && tick.totalSellQuantity > 0
          ? tick.totalSellQuantity
          : prevQuote.totalSellQuantity;
      const updatedBuyRatio =
        typeof tick.buyDepthRatio === "number"
          ? tick.buyDepthRatio
          : prevQuote.buyDepthRatio;
      const updatedDepth =
        tick.depth && tick.depth.buy && tick.depth.buy.length > 0
          ? tick.depth
          : prevQuote.depth;

      return {
        ...prevQuote,
        lastPrice: ltp,
        change: changeVal,
        changePct: changePctVal,
        volume: tick.volume || prevQuote.volume,
        averagePrice: updatedAveragePrice,
        lastTradedQuantity: updatedLTQ,
        lastTradedTime: updatedLTT,
        ohlc: updatedOhlc,
        totalBuyQuantity: updatedTotalBuy,
        totalSellQuantity: updatedTotalSell,
        buyDepthRatio: updatedBuyRatio,
        depth: updatedDepth,
      };
    });

    setLastRefreshed(new Date());

    // Update intraday price trajectory smoothly without re-fetching HTTP endpoints (only for 1D timeframe)
    if (selectedTimeframe === "1D") {
      setChartData((prev) => {
        if (!prev || prev.length === 0) return prev;
        const lastPoint = prev[prev.length - 1];
        const nowStr = new Date().toLocaleTimeString("en-IN", { hour12: false, hour: "2-digit", minute: "2-digit" });

        const openVal = lastPoint.open ?? lastPoint.price;
        const highVal = Math.max(lastPoint.high ?? lastPoint.price, tick.ltp);
        const lowVal = Math.min(lastPoint.low ?? lastPoint.price, tick.ltp);
        const closeVal = tick.ltp;

        const nowIso = new Date().toISOString();
        if (lastPoint.time === nowStr) {
          return [
            ...prev.slice(0, -1),
            { ...lastPoint, rawDate: lastPoint.rawDate || nowIso, price: tick.ltp, high: highVal, low: lowVal, close: closeVal, volume: tick.volume },
          ];
        } else {
          return [
            ...prev,
            { time: nowStr, rawDate: nowIso, price: tick.ltp, open: tick.ltp, high: tick.ltp, low: tick.ltp, close: tick.ltp, volume: tick.volume },
          ];
        }
      });
    }
  }, [latestTicks, activeSymbol, isLiveStreaming, selectedTimeframe]);

  // Discretionary manual order dispatch
  const handleExecuteManualEntry = async () => {
    if (!activeSymbol) return;
    setExecutingOrder(true);
    setManualEntryAlert(null);
    try {
      const clean = activeSymbol.trim().toUpperCase().replace("NSE:", "");
      const params = new URLSearchParams();
      params.append("symbol", clean);
      if (manualQty && parseInt(manualQty) > 0) {
        params.append("quantity", manualQty.trim());
      }
      if (manualStopPrice && parseFloat(manualStopPrice) > 0) {
        params.append("stopPrice", manualStopPrice.trim());
      }
      if (quote?.lastPrice && quote.lastPrice > 0) {
        params.append("entryPrice", quote.lastPrice.toString());
      }

      const res = await apiFetch(`/api/dashboard/strategy/manual-entry?${params.toString()}`, {
        method: "POST",
      });
      const data = await res.json();
      if (res.ok && data.success) {
        setManualEntryAlert({
          type: "success",
          message: `🚀 Order executed! Bought ${data.quantity} shares of ${data.symbol} @ ₹${Number(data.entryPrice).toFixed(2)}. Protective SL set at ₹${Number(data.stopPrice).toFixed(2)}. Strategy has taken full lifecycle control.`,
        });
        setTimeout(() => {
          setShowManualEntryModal(false);
          setManualEntryAlert(null);
        }, 3500);
      } else {
        setManualEntryAlert({
          type: "error",
          message: data.error || data.reason || "Manual entry failed. Check engine logs or active trade limit.",
        });
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setManualEntryAlert({
        type: "error",
        message: `Network error: ${msg}`,
      });
    } finally {
      setExecutingOrder(false);
    }
  };

  const handleChipClick = (sym: string) => {
    fetchSymbolData(sym, false);
  };

  return (
    <div className="flex flex-col gap-6 animate-fadeIn">
      {/* Zerodha Authentication Card (If Session is Inactive) */}
      {!session?.active && (
        <ZerodhaAuthCard
          session={session}
          loginUrl={loginUrl}
          onAutoLogin={onAutoLogin}
          onManualTokenSubmit={onManualTokenSubmit}
          activatingToken={activatingToken}
          autoLoggingIn={autoLoggingIn}
        />
      )}

      {/* Stock Explorer Search & Controls Card */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-5 sm:p-6 shadow-xl backdrop-blur space-y-4">
        <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
          <div>
            <h2 className="text-lg sm:text-xl font-bold text-white flex items-center gap-2">
              🔍 NSE Stock Explorer &amp; Market Depth
            </h2>
            <p className="text-xs text-slate-400 mt-0.5">
              Live quotes and Level-2 order book streamed directly from Zerodha Kite Connect.
            </p>
          </div>

          <div className="flex items-center gap-2">
            {/* Live Streaming Toggle */}
            <button
              type="button"
              onClick={() => setIsLiveStreaming(!isLiveStreaming)}
              className={`px-3 py-1.5 rounded-lg text-xs font-mono font-semibold border transition flex items-center gap-1.5 cursor-pointer ${
                isLiveStreaming
                  ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-400 hover:bg-emerald-500/20"
                  : "bg-slate-800 border-slate-700 text-slate-400 hover:text-white"
              }`}
            >
              <span
                className={`w-2 h-2 rounded-lg ${
                  isLiveStreaming ? "bg-emerald-400 animate-pulse" : "bg-slate-500"
                }`}
              />
              {isLiveStreaming ? "Live Stream" : "Paused"}
            </button>

            {lastRefreshed && (
              <span className="text-[11px] font-mono text-slate-500">
                {lastRefreshed.toLocaleTimeString()}
              </span>
            )}
          </div>
        </div>

        {/* Search Combobox */}
        <StockSearchCombobox
          currentSymbol={activeSymbol}
          onSelectStock={(sym) => fetchSymbolData(sym, false)}
          loading={loading}
        />

        {/* Popular Stocks Quick Chips */}
        <div className="flex items-center gap-2 flex-wrap pt-1">
          <span className="text-[11px] text-slate-400 uppercase tracking-wider font-semibold mr-1">
            Quick Chips:
          </span>
          {popularStocks.map((sym) => (
            <button
              key={sym}
              type="button"
              onClick={() => handleChipClick(sym)}
              className={`px-2.5 py-1 rounded-lg text-xs font-mono transition cursor-pointer border ${
                activeSymbol === sym
                  ? "bg-emerald-500/20 border-emerald-500/50 text-emerald-300 font-bold shadow-sm shadow-emerald-950"
                  : "bg-slate-800/80 hover:bg-slate-800 border-slate-700/80 text-slate-300 hover:text-white"
              }`}
            >
              {sym}
            </button>
          ))}
        </div>
      </div>

      {/* Error Banner */}
      {error && (
        <div className="p-4 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-sm flex items-start gap-3">
          <span className="text-lg">⚠️</span>
          <div>
            <p className="font-semibold">{error}</p>
            <p className="text-xs text-rose-400/80 mt-1">
              Check the spelling or choose one of the verified chips above. Live streaming is paused until a valid symbol is searched.
            </p>
          </div>
        </div>
      )}

      {/* Initial Loading State: Premium Skeleton Loader */}
      {loading && !quote && <SkeletonStockExplorer />}

      {/* Live Quote & Market Depth Display */}
      {quote && (
        <div className="space-y-6">
          {/* Main Price Card */}
          <div className="bg-gradient-to-br from-slate-900 to-slate-900/90 border border-slate-800 rounded-2xl p-6 sm:p-7 shadow-2xl relative z-20">
            <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 pb-6 border-b border-slate-800">
              <div>
                <div className="flex items-center gap-3">
                  <h1 className="text-2xl sm:text-3xl font-black text-white tracking-tight">
                    {quote.symbol}
                  </h1>
                  <span className="text-xs px-2.5 py-0.5 rounded-full bg-slate-800 text-slate-300 border border-slate-700 font-mono">
                    NSE Equity
                  </span>
                  {quote.liveFeed && (
                    <span className="text-xs px-2 py-0.5 rounded-full bg-emerald-500/20 text-emerald-400 border border-emerald-500/30 font-mono flex items-center gap-1">
                      <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse"></span>
                      STREAMING
                    </span>
                  )}
                </div>
                <p className="text-xs text-slate-400 font-mono mt-1">
                  Instrument Token: {quote.instrument} • Traded Time: {quote.lastTradedTime || quote.timestamp || "Market Close"}
                </p>
              </div>

              <div className="flex flex-col sm:flex-row items-start sm:items-center gap-4 sm:gap-6">
                {/* Discretionary Manual Buy Button */}
                <button
                  type="button"
                  onClick={() => {
                    setManualQty("");
                    if (quote.lastPrice) {
                      setManualStopPrice((quote.lastPrice * 0.992).toFixed(2));
                    }
                    setManualEntryAlert(null);
                    setShowManualEntryModal(true);
                  }}
                  className="px-4 py-2.5 rounded-lg bg-gradient-to-r from-emerald-500 to-teal-500 hover:from-emerald-400 hover:to-teal-400 text-slate-950 font-bold text-xs sm:text-sm shadow-lg shadow-emerald-950/60 transition-all flex items-center gap-2 cursor-pointer active:scale-95 border border-emerald-400/40"
                >
                  {/* <span className="text-base">⚡</span> */}
                  <span>Buy</span>
                </button>

                <div className="text-left sm:text-right">
                  <p className="text-xs text-slate-400 uppercase tracking-wider font-mono">Last Traded Price (LTP)</p>
                  <div className="flex items-baseline sm:justify-end gap-2 mt-0.5">
                    <span
                      className={`text-3xl sm:text-3xl font-mono font-black transition-all duration-300 ${
                        flashDirection === "up"
                          ? "text-emerald-400"
                          : flashDirection === "down"
                          ? "text-rose-400"
                          : "text-white"
                      }`}
                    >
                      ₹{quote.lastPrice ? quote.lastPrice.toFixed(2) : "0.00"}
                    </span>
                    {(() => {
                      const prevClose = quote.ohlc?.close;
                      const change = (typeof quote.change === "number" && quote.change !== 0)
                        ? quote.change
                        : (prevClose && prevClose > 0 ? quote.lastPrice - prevClose : quote.change || 0);
                      const changePct = (typeof quote.changePct === "number" && quote.changePct !== 0)
                        ? quote.changePct
                        : (prevClose && prevClose > 0 ? (change / prevClose) * 100 : quote.changePct || 0);

                      return (
                        <span className={`text-sm sm:text-base font-mono font-bold ${
                          change >= 0 ? "text-emerald-400" : "text-rose-400"
                        }`}>
                          {change >= 0 ? "+" : ""}₹{change.toFixed(2)} ({changePct >= 0 ? "+" : ""}{changePct.toFixed(2)}%)
                        </span>
                      );
                    })()}
                  </div>
                </div>
              </div>
            </div>

            {/* Curvy Interactive Price Chart with Multi-Timeframe and Candlestick Toggle */}
            <div className="mt-6">
              <StockPriceChart
                symbol={quote.symbol}
                data={chartData}
                prevClose={quote.ohlc?.close}
                pdh={quote.pdh}
                pdl={quote.pdl}
                dayHigh={quote.ohlc?.high}
                dayLow={quote.ohlc?.low}
                currentPrice={quote.lastPrice}
                loading={chartLoading}
                selectedTimeframe={selectedTimeframe}
                onTimeframeChange={setSelectedTimeframe}
                selectedInterval={selectedInterval}
                onIntervalChange={setSelectedInterval}
                chartType={chartType}
                onChartTypeChange={setChartType}
              />
            </div>

            {/* Comprehensive Market Metrics & Day's Range Thermometer */}
            <StockMetricsOverview quote={quote} />
          </div>

          {/* Level-2 5-Level Market Depth (Bids & Offers) */}
          {quote.depth && (
            <div className="relative z-10">
              <MarketDepthWidget quote={quote} />
            </div>
          )}
        </div>
      )}

      {/* Discretionary Manual Entry Modal */}
      {showManualEntryModal && quote && (
        <div className="fixed inset-0 z-50 bg-black/80 backdrop-blur-sm flex items-center justify-center p-4 animate-fadeIn">
          <div className="bg-slate-900 border border-slate-700 rounded-2xl max-w-lg w-full p-6 shadow-2xl relative space-y-5">
            <div className="flex items-start justify-between border-b border-slate-800 pb-4">
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-xl">⚡</span>
                  <h3 className="text-lg font-bold text-white">
                    Manual Buy &amp; Strategy Handover
                  </h3>
                </div>
                <p className="text-xs text-slate-400 mt-0.5">
                  Enter discretionary trade. Algorithmic engine takes full lifecycle management upon execution.
                </p>
              </div>
              <button
                type="button"
                onClick={() => {
                  setShowManualEntryModal(false);
                  setManualEntryAlert(null);
                }}
                className="text-slate-400 hover:text-white text-lg p-1 transition cursor-pointer"
              >
                ✕
              </button>
            </div>

            {/* Instrument Summary Card */}
            <div className="bg-slate-950/80 rounded-xl p-3.5 border border-slate-800 flex items-center justify-between font-mono">
              <div>
                <span className="text-xs text-slate-400 uppercase">Symbol</span>
                <p className="text-base font-bold text-white">{quote.symbol}</p>
              </div>
              <div className="text-right">
                <span className="text-xs text-slate-400 uppercase">Current LTP</span>
                <p className="text-base font-bold text-emerald-400">
                  ₹{quote.lastPrice ? quote.lastPrice.toFixed(2) : "--"}
                </p>
              </div>
            </div>

            {/* Input fields */}
            <div className="space-y-4">
              <div>
                <div className="flex items-center justify-between text-xs mb-1.5">
                  <label className="text-slate-300 font-semibold">Quantity (Shares)</label>
                  <span className="text-slate-400 font-mono text-[11px]">
                    Leave blank for Auto Risk Sizing (2.0% Risk + 5x Leverage)
                  </span>
                </div>
                <input
                  type="number"
                  min="1"
                  step="1"
                  value={manualQty}
                  onChange={(e) => setManualQty(e.target.value)}
                  placeholder="Auto-calculated by Position Sizing Engine..."
                  className="w-full bg-slate-950 border border-slate-700 focus:border-emerald-500 rounded-xl px-4 py-2.5 text-sm font-mono text-white placeholder-slate-500 outline-none transition"
                />
              </div>

              <div>
                <div className="flex items-center justify-between text-xs mb-1.5">
                  <label className="text-slate-300 font-semibold">Initial Protective Stop Loss (₹)</label>
                  <span className="text-slate-400 font-mono text-[11px]">
                    Default: 0.8% below market
                  </span>
                </div>
                <input
                  type="number"
                  step="0.05"
                  value={manualStopPrice}
                  onChange={(e) => setManualStopPrice(e.target.value)}
                  placeholder="e.g. 2415.00"
                  className="w-full bg-slate-950 border border-slate-700 focus:border-emerald-500 rounded-xl px-4 py-2.5 text-sm font-mono text-white placeholder-slate-500 outline-none transition"
                />
              </div>
            </div>

            {/* Automated Strategy Rules Checklist */}
            <div className="bg-slate-950/60 rounded-xl p-3.5 border border-slate-800 text-xs space-y-2 text-slate-300">
              <p className="font-bold text-slate-200 flex items-center gap-1.5">
                <span>🛡️</span> Algorithmic Handover Rules:
              </p>
              <ul className="space-y-1.5 text-[11px] text-slate-400 pl-4 list-disc">
                <li>
                  <strong className="text-emerald-400">Stage 1 Breakeven Lock:</strong> Moves stop loss to entry + 0.15% once profit hits +1.0%, protecting capital against adverse reversals (subject to market liquidity and stop-limit execution) and covering all statutory taxes.
                </li>
                <li>
                  <strong className="text-emerald-400">Stage 2 Trailing Stop:</strong> Trails at 0.8% behind subsequent high-water mark.
                </li>
                <li>
                  <strong className="text-amber-400">Mandatory EOD Square-Off:</strong> Automatically liquidates at 15:05 PM IST sharp if not exited.
                </li>
                <li>
                  <strong className="text-slate-300">Concurrency Guard:</strong> Enforces single active trade limit across the entire account.
                </li>
              </ul>
            </div>

            {/* Status Alert */}
            {manualEntryAlert && (
              <div
                className={`p-3 rounded-xl border text-xs font-mono ${
                  manualEntryAlert.type === "success"
                    ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-300"
                    : "bg-rose-500/10 border-rose-500/30 text-rose-300"
                }`}
              >
                {manualEntryAlert.message}
              </div>
            )}

            {/* Actions */}
            <div className="flex items-center justify-end gap-3 pt-2 border-t border-slate-800">
              <button
                type="button"
                onClick={() => {
                  setShowManualEntryModal(false);
                  setManualEntryAlert(null);
                }}
                disabled={executingOrder}
                className="px-4 py-2.5 rounded-xl text-xs font-semibold text-slate-400 hover:text-white transition cursor-pointer"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleExecuteManualEntry}
                disabled={executingOrder}
                className="px-5 py-2.5 rounded-xl bg-emerald-500 hover:bg-emerald-400 disabled:opacity-50 text-slate-950 font-bold text-xs tracking-wide transition flex items-center gap-2 cursor-pointer shadow-md shadow-emerald-950/50"
              >
                {executingOrder ? (
                  <span>Executing Order...</span>
                ) : (
                  <>
                    <span>🚀 Confirm Buy &amp; Hand Over</span>
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
