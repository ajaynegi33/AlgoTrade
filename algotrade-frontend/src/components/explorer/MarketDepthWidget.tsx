"use client";

import { useState, useMemo } from "react";
import { StockQuoteData } from "../types";

interface MarketDepthWidgetProps {
  quote: StockQuoteData;
}

export default function MarketDepthWidget({ quote }: MarketDepthWidgetProps) {
  const [showInfoModal, setShowInfoModal] = useState(false);

  // Total Market Order Book Volumes
  const totalBuy = quote.totalBuyQuantity || 0;
  const totalSell = quote.totalSellQuantity || 0;
  const totalOrders = totalBuy + totalSell;
  const buyPct = totalOrders > 0 ? (totalBuy / totalOrders) * 100 : 50;
  const sellPct = totalOrders > 0 ? (totalSell / totalOrders) * 100 : 50;

  // Immediate Top-5 Depth Volumes & Ratios
  const buyLevels = quote.depth?.buy || [];
  const sellLevels = quote.depth?.sell || [];

  const { top5BuyQty, top5SellQty, top5BuyPct, maxDepthQty, avgOrderSize } = useMemo(() => {
    const t5Buy = buyLevels.reduce((acc, lvl) => acc + (lvl.quantity || 0), 0);
    const t5Sell = sellLevels.reduce((acc, lvl) => acc + (lvl.quantity || 0), 0);
    const t5Total = t5Buy + t5Sell;
    const t5Pct = t5Total > 0 ? (t5Buy / t5Total) * 100 : 50;

    const allQtys = [...buyLevels.map((b) => b.quantity), ...sellLevels.map((s) => s.quantity)];
    const maxQty = Math.max(...allQtys, 1);

    const totalOrdersCount = [...buyLevels, ...sellLevels].reduce((acc, lvl) => acc + (lvl.orders || 1), 0);
    const avgSize = totalOrdersCount > 0 ? (t5Buy + t5Sell) / totalOrdersCount : 100;

    return {
      top5BuyQty: t5Buy,
      top5SellQty: t5Sell,
      top5BuyPct: t5Pct,
      maxDepthQty: maxQty,
      avgOrderSize: avgSize,
    };
  }, [buyLevels, sellLevels]);

  // Real-Time Bid-Ask Spread & Quantitative Micro-Price
  const bestBid = buyLevels[0]?.price || 0;
  const bestAsk = sellLevels[0]?.price || 0;
  const bestBidQty = buyLevels[0]?.quantity || 0;
  const bestAskQty = sellLevels[0]?.quantity || 0;

  const spread = bestAsk > 0 && bestBid > 0 ? bestAsk - bestBid : 0;
  const spreadPct = bestBid > 0 ? (spread / bestBid) * 100 : 0;

  // Quantitative Micro-Price: Volume-Weighted Bid-Ask Equilibrium
  const microPrice =
    bestBidQty + bestAskQty > 0
      ? (bestBid * bestAskQty + bestAsk * bestBidQty) / (bestBidQty + bestAskQty)
      : (bestBid + bestAsk) / 2;

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-2xl overflow-hidden shadow-2xl backdrop-blur relative">
      {/* Widget Header & Metrics */}
      <div className="p-4 sm:p-5 border-b border-slate-800 flex flex-col lg:flex-row items-start lg:items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <h3 className="text-base font-bold text-white flex items-center gap-2">
            <span>📊</span>
            <span>Market Depth</span>

          </h3>

          {/* Interactive Info Button (ℹ) */}
          <div className="relative">
            <button
              type="button"
              onClick={() => setShowInfoModal(!showInfoModal)}
              onMouseEnter={() => setShowInfoModal(true)}
              className="w-5 h-5 rounded-full bg-slate-800 hover:bg-slate-700 border border-slate-600 text-slate-300 hover:text-white flex items-center justify-center text-xs font-bold transition cursor-pointer shadow-sm"
              title="Click or hover for Market Depth explanation guide"
              aria-label="Market Depth Explanation Guide"
            >
              ℹ
            </button>

            {/* Interactive Info Card Popover */}
            {showInfoModal && (
              <>
                {/* Backdrop closer on mobile */}
                <div
                  className="fixed inset-0 z-40 lg:hidden"
                  onClick={() => setShowInfoModal(false)}
                />
                <div
                  onMouseLeave={() => setShowInfoModal(false)}
                  className="absolute left-0 top-7 z-50 w-96 sm:w-150 bg-slate-950/95 border border-slate-700 rounded-xl p-4 shadow-2xl backdrop-blur text-xs text-slate-300 space-y-3 animate-fadeIn"
                >
                  <div className="flex items-center justify-between border-b border-slate-800 pb-2">
                    <p className="font-bold text-white flex items-center gap-1.5">
                      <span>📖</span>Market Depth Guide
                    </p>
                  </div>

                  <div className="space-y-2.5 text-[11px] leading-relaxed max-h-100 overflow-y-auto pr-1">
                    <div>
                      <strong className="text-emerald-400">Buy orders % vs Sell orders %:</strong>
                      <p className="text-slate-400 mt-0.5">
                        Macro ratio of total pending buy bids vs sell offers queued on the National Stock Exchange (NSE) book.
                      </p>
                    </div>

                    <div>
                      <strong className="text-sky-400">Top-5 Immediate Pressure:</strong>
                      <p className="text-slate-400 mt-0.5">
                        Ratio of shares queued strictly at the front 5 price levels. This represents the live order flow about to execute right now.
                      </p>
                    </div>

                    <div>
                      <strong className="text-emerald-400">Bid (Buy) Side:</strong>
                      <p className="text-slate-400 mt-0.5">
                        Price buyers are willing to pay. Highest bid is at the top. Green shaded bars reflect volume depth.
                      </p>
                    </div>

                    <div>
                      <strong className="text-rose-400">Ask (Sell) Side:</strong>
                      <p className="text-slate-400 mt-0.5">
                        Price sellers are willing to accept. Lowest ask is at the top. Rose shaded bars reflect volume depth.
                      </p>
                    </div>

                    <div>
                      <strong className="text-amber-400">Bid-Ask Spread:</strong>
                      <p className="text-slate-400 mt-0.5">
                        Difference between Best Ask and Best Bid (<code>Ask - Bid</code>). A tight spread (&lt; 0.03%) ensures high liquidity and minimal entry slippage.
                      </p>
                    </div>

                    <div>
                      <strong className="text-purple-400">Whale Wall (🐋):</strong>
                      <p className="text-slate-400 mt-0.5">
                        Highlights a price level where average shares per order ticket is &gt;2.5× normal, signaling an institutional block or support/resistance wall.
                      </p>
                    </div>

                    <div>
                      <strong className="text-slate-200">Micro-Price:</strong>
                      <p className="text-slate-400 mt-0.5">
                        Quantitative volume-weighted fair equilibrium price between Best Bid and Best Ask.
                      </p>
                    </div>
                  </div>
                </div>
              </>
            )}
          </div>
        </div>

        {/* Live Badges: Spread & Top-5 Microstructure Pressure */}
        <div className="flex flex-wrap items-center gap-2 font-mono text-xs">
          {/* Spread Indicator */}
          {bestAsk > 0 && bestBid > 0 && (
            <div
              className={`px-2.5 py-1 rounded-lg border flex items-center gap-1.5 ${
                spreadPct <= 0.03
                  ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-400"
                  : spreadPct <= 0.06
                  ? "bg-amber-500/10 border-amber-500/30 text-amber-300"
                  : "bg-rose-500/10 border-rose-500/30 text-rose-300"
              }`}
              title="Difference between Best Offer and Best Bid price"
            >
              <span className="text-[10px] text-slate-400 font-sans">Spread:</span>
              <strong className="font-bold">₹{spread.toFixed(2)}</strong>
              <span className="text-[10px]">({spreadPct.toFixed(3)}%)</span>
              <span
                className={`w-1.5 h-1.5 rounded-full ${
                  spreadPct <= 0.03 ? "bg-emerald-400" : "bg-amber-400"
                }`}
              />
            </div>
          )}

          {/* Top-5 Pressure Badge */}
          <div
            className={`px-2.5 py-1 rounded-lg border flex items-center gap-1.5 ${
              top5BuyPct >= 55
                ? "bg-emerald-500/15 border-emerald-500/40 text-emerald-300"
                : top5BuyPct <= 45
                ? "bg-rose-500/15 border-rose-500/40 text-rose-300"
                : "bg-slate-800 border-slate-700 text-slate-300"
            }`}
            title="Front 5-level immediate order book volume imbalance"
          >
            <span className="text-[10px] text-slate-400 font-sans">Top-5 Pressure:</span>
            <strong className="font-bold">{top5BuyPct.toFixed(1)}% Buyers</strong>
            <span>{top5BuyPct >= 55 ? "▲" : top5BuyPct <= 45 ? "▼" : "•"}</span>
          </div>
        </div>
      </div>

      {/* Dual-Tone Horizontal Buy vs Sell Volume Ratio Bar */}
      <div className="p-4 sm:p-5 bg-slate-950/40 border-b border-slate-800 space-y-2.5">
        <div className="flex items-baseline justify-between font-mono">
          <div>
            <span className="text-xs text-slate-400 font-sans uppercase tracking-wider">Buy orders</span>
            <p className="text-lg sm:text-2xl font-black text-emerald-400 tracking-tight mt-0.5">
              {buyPct.toFixed(2)}%
            </p>
          </div>
          <div className="text-right">
            <span className="text-xs text-slate-400 font-sans uppercase tracking-wider">Sell orders</span>
            <p className="text-lg sm:text-2xl font-black text-rose-400 tracking-tight mt-0.5">
              {sellPct.toFixed(2)}%
            </p>
          </div>
        </div>

        {/* Dual-Tone Horizontal Ratio Segmented Bar */}
        <div className="w-full h-3 sm:h-3.5 rounded-full bg-slate-950 border border-slate-800 flex overflow-hidden shadow-inner p-0.5">
          <div
            className="h-full bg-emerald-500 rounded-l-full transition-all duration-500 ease-out shadow-[0_0_10px_rgba(16,185,129,0.5)]"
            style={{ width: `${Math.max(1, Math.min(99, buyPct))}%` }}
          />
          <div
            className="h-full bg-rose-500 rounded-r-full transition-all duration-500 ease-out shadow-[0_0_10px_rgba(244,63,94,0.5)]"
            style={{ width: `${Math.max(1, Math.min(99, sellPct))}%` }}
          />
        </div>

        {/* Total Quantities Underneath Bar */}
        <div className="flex items-center justify-between text-xs font-mono text-slate-400 pt-0.5">
          <span>
            Total Bids:{" "}
            <strong className="text-emerald-400 font-bold">
              {totalBuy ? totalBuy.toLocaleString("en-IN") : "--"}
            </strong>
          </span>
          <span>
            Total Asks:{" "}
            <strong className="text-rose-400 font-bold">
              {totalSell ? totalSell.toLocaleString("en-IN") : "--"}
            </strong>
          </span>
        </div>
      </div>

      {/* Level-2 5-Level Order Book (Strict CSS Grid for Pixel-Perfect Alignment) */}
      <div className="grid grid-cols-1 md:grid-cols-2 divide-y md:divide-y-0 md:divide-x divide-slate-800 font-mono text-xs">
        {/* Left Side: Bids (Buyers) */}
        <div className="p-4 sm:p-5 flex flex-col justify-between">
          <div>
            {/* Header: Strict CSS Grid 12 Columns */}
            <div className="grid grid-cols-12 text-[11px] font-mono uppercase tracking-wider text-slate-400 font-bold pb-2.5 mb-2 border-b border-slate-800 px-2 select-none">
              <span className="col-span-3 text-left">Orders</span>
              <span className="col-span-4 text-right">Quantity</span>
              <span className="col-span-5 text-right text-emerald-400">Bid Price</span>
            </div>

            {/* Bid Rows (Top 5 Levels) */}
            <div className="space-y-1.5">
              {buyLevels.length > 0 ? (
                buyLevels.map((lvl, idx) => {
                  const fillPct = (lvl.quantity / maxDepthQty) * 100;
                  const isWhale = lvl.orders > 0 && lvl.quantity / lvl.orders > avgOrderSize * 2.5;

                  return (
                    <div
                      key={idx}
                      className="relative grid grid-cols-12 py-1.5 px-2 rounded items-center font-mono text-xs overflow-hidden hover:bg-slate-800/40 transition group"
                    >
                      {/* Proportional Liquidity Depth Fill Bar */}
                      <div
                        className="absolute right-0 top-0 bottom-0 bg-emerald-500/15 pointer-events-none transition-all duration-300 rounded-r"
                        style={{ width: `${Math.max(2, Math.min(100, fillPct))}%` }}
                      />

                      {/* Column 1: Orders (col-span-3 text-left) */}
                      <span className="col-span-3 text-left text-slate-400 text-[11px] relative z-10">
                        {lvl.orders}
                      </span>

                      {/* Column 2: Quantity (col-span-4 text-right) */}
                      <span className="col-span-4 text-right text-slate-200 font-semibold relative z-10 flex items-center justify-end gap-1">
                        <span>{lvl.quantity.toLocaleString("en-IN")}</span>
                        {isWhale && (
                          <span
                            title="Institutional Whale Order: Avg size > 2.5x normal"
                            className="text-[11px] cursor-help"
                          >
                            🐋
                          </span>
                        )}
                      </span>

                      {/* Column 3: Bid Price (col-span-5 text-right) */}
                      <span className="col-span-5 text-right text-emerald-400 font-bold relative z-10">
                        ₹{lvl.price.toFixed(2)}
                      </span>
                    </div>
                  );
                })
              ) : (
                <div className="text-center py-6 text-slate-500 text-xs font-mono">
                  No active bids received on current feed
                </div>
              )}
            </div>
          </div>

          {/* Bid Summary Footer */}
          <div className="pt-3 mt-4 border-t border-slate-800/80 flex items-center justify-between text-xs text-slate-400">
            <span>Bid Total (Top-5):</span>
            <strong className="text-emerald-400 font-bold font-mono">
              {top5BuyQty ? top5BuyQty.toLocaleString("en-IN") : "--"}
            </strong>
          </div>
        </div>

        {/* Right Side: Asks / Offers (Sellers) */}
        <div className="p-4 sm:p-5 flex flex-col justify-between">
          <div>
            {/* Header: Strict CSS Grid 12 Columns */}
            <div className="grid grid-cols-12 text-[11px] font-mono uppercase tracking-wider text-slate-400 font-bold pb-2.5 mb-2 border-b border-slate-800 px-2 select-none">
              <span className="col-span-5 text-left text-rose-400">Ask Price</span>
              <span className="col-span-4 text-right">Quantity</span>
              <span className="col-span-3 text-right">Orders</span>
            </div>

            {/* Ask Rows (Top 5 Levels) */}
            <div className="space-y-1.5">
              {sellLevels.length > 0 ? (
                sellLevels.map((lvl, idx) => {
                  const fillPct = (lvl.quantity / maxDepthQty) * 100;
                  const isWhale = lvl.orders > 0 && lvl.quantity / lvl.orders > avgOrderSize * 2.5;

                  return (
                    <div
                      key={idx}
                      className="relative grid grid-cols-12 py-1.5 px-2 rounded items-center font-mono text-xs overflow-hidden hover:bg-slate-800/40 transition group"
                    >
                      {/* Proportional Liquidity Depth Fill Bar */}
                      <div
                        className="absolute left-0 top-0 bottom-0 bg-rose-500/15 pointer-events-none transition-all duration-300 rounded-l"
                        style={{ width: `${Math.max(2, Math.min(100, fillPct))}%` }}
                      />

                      {/* Column 1: Ask Price (col-span-5 text-left) */}
                      <span className="col-span-5 text-left text-rose-400 font-bold relative z-10">
                        ₹{lvl.price.toFixed(2)}
                      </span>

                      {/* Column 2: Quantity (col-span-4 text-right) */}
                      <span className="col-span-4 text-right text-slate-200 font-semibold relative z-10 flex items-center justify-end gap-1">
                        <span>{lvl.quantity.toLocaleString("en-IN")}</span>
                        {isWhale && (
                          <span
                            title="Institutional Whale Order: Avg size > 2.5x normal"
                            className="text-[11px] cursor-help"
                          >
                            🐋
                          </span>
                        )}
                      </span>

                      {/* Column 3: Orders (col-span-3 text-right) */}
                      <span className="col-span-3 text-right text-slate-400 text-[11px] relative z-10">
                        {lvl.orders}
                      </span>
                    </div>
                  );
                })
              ) : (
                <div className="text-center py-6 text-slate-500 text-xs font-mono">
                  No active asks received on current feed
                </div>
              )}
            </div>
          </div>

          {/* Ask Summary Footer */}
          <div className="pt-3 mt-4 border-t border-slate-800/80 flex items-center justify-between text-xs text-slate-400">
            <span>Ask Total (Top-5):</span>
            <strong className="text-rose-400 font-bold font-mono">
              {top5SellQty ? top5SellQty.toLocaleString("en-IN") : "--"}
            </strong>
          </div>
        </div>
      </div>

      {/* Quantitative Equilibrium Strip (Micro-Price) */}
      {bestBid > 0 && bestAsk > 0 && (
        <div className="bg-slate-950/80 border-t border-slate-800/80 px-4 sm:px-6 py-2.5 flex flex-col sm:flex-row items-center justify-between gap-2 text-[11px] font-mono text-slate-400">
          <div className="flex items-center gap-2">
            <span>Equilibrium Micro-Price:</span>
            <strong className="text-white font-bold">₹{microPrice.toFixed(2)}</strong>
            <span className="text-[10px] text-slate-500">
              ({microPrice >= (bestBid + bestAsk) / 2 ? "▲ Ask-Tilted" : "▼ Bid-Tilted"})
            </span>
          </div>

          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse"></span>
              <span className="text-emerald-400">Live 0ms WebSocket Order Flow</span>
            </span>
          </div>
        </div>
      )}
    </div>
  );
}
