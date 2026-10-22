"use client";

import React, { useState, useEffect, useMemo } from "react";
import { useTradingStore } from "@/store/useTradingStore";
import { getKiteLoginURL } from "@/lib/api";
import { toast } from "sonner";

export default function StockUniverseView() {
  const {
    kiteStatus,
    isKiteLoading,
    universe,
    isUniverseLoading,
    isScanning,
    fetchKiteStatus,
    fetchUniverse,
    triggerScan,
  } = useTradingStore();

  const [searchQuery, setSearchQuery] = useState("");
  const [pageSize, setPageSize] = useState(25);
  const [currentPage, setCurrentPage] = useState(1);

  // Fetch Kite status and current Stock Universe on mount (uses cached store if already loaded)
  useEffect(() => {
    fetchKiteStatus();
    fetchUniverse();
  }, [fetchKiteStatus, fetchUniverse]);

  // Handle Kite Login redirect
  const handleConnectKite = async () => {
    try {
      const loginUrl = await getKiteLoginURL();
      window.location.href = loginUrl;
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Failed to retrieve Kite login URL");
    }
  };

  // Handle Manual 08:30 AM Scanner Trigger via Zustand
  const handleTriggerScan = async () => {
    const scanToast = toast.loading("Downloading daily instruments & filtering F&O Cash Equities...");
    try {
      await triggerScan();
      toast.dismiss(scanToast);
      toast.success("Scan complete! F&O Cash Equities updated.");
      setCurrentPage(1);
    } catch (err: unknown) {
      toast.dismiss(scanToast);
      toast.error(err instanceof Error ? err.message : "Scanner failed to update universe");
    }
  };

  // Search filtering
  const filteredStocks = useMemo(() => {
    if (!universe?.stocks) return [];
    const q = searchQuery.trim().toLowerCase();
    if (!q) return universe.stocks;

    return universe.stocks.filter(
      (s) =>
        s.tradingsymbol.toLowerCase().includes(q) ||
        s.name.toLowerCase().includes(q) ||
        String(s.instrument_token).includes(q)
    );
  }, [universe, searchQuery]);

  // Pagination calculation
  const totalPages = Math.max(1, Math.ceil(filteredStocks.length / pageSize));
  const paginatedStocks = useMemo(() => {
    const start = (currentPage - 1) * pageSize;
    return filteredStocks.slice(start, start + pageSize);
  }, [filteredStocks, currentPage, pageSize]);

  return (
    <div className="space-y-6">
      {/* =========================================================================
          1. Zerodha Kite Connect Authentication Bar
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-2xl p-5 shadow-xs transition-all">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div className="flex items-start sm:items-center gap-3.5">
            <div
              className={`w-11 h-11 rounded-2xl flex items-center justify-center shrink-0 border ${
                kiteStatus?.is_connected
                  ? "bg-emerald-50 border-emerald-200/80 text-emerald-700"
                  : "bg-amber-50 border-amber-200/80 text-amber-700"
              }`}
            >
              {kiteStatus?.is_connected ? (
                <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
              ) : (
                <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                </svg>
              )}
            </div>

            <div>
              <div className="flex items-center gap-2">
                <span className="font-bold text-slate-900 text-sm sm:text-base">
                  Zerodha Kite Connect
                </span>
                {isKiteLoading ? (
                  <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium bg-slate-100 text-slate-600">
                    Checking...
                  </span>
                ) : kiteStatus?.is_connected ? (
                  <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-emerald-50 text-emerald-800 border border-emerald-300/60">
                    <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
                    Session Active
                  </span>
                ) : (
                  <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-amber-50 text-amber-800 border border-amber-300/60">
                    <span className="w-2 h-2 rounded-full bg-amber-500" />
                    Disconnected
                  </span>
                )}
              </div>

              <p className="text-xs text-slate-500 mt-0.5">
                {kiteStatus?.is_connected ? (
                  <>
                    Logged in as <strong className="text-slate-800">{kiteStatus.user_name || kiteStatus.user_id}</strong> ({kiteStatus.user_id}) • Token valid until 06:00 AM IST
                  </>
                ) : kiteStatus?.is_configured ? (
                  "Zerodha API keys configured. Complete morning OAuth handshake to unlock trading."
                ) : (
                  "Add KITE_API_KEY and KITE_API_SECRET in backend .env to connect."
                )}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2.5 shrink-0">
            {kiteStatus?.is_connected ? (
              <button
                type="button"
                onClick={handleConnectKite}
                className="text-xs font-semibold text-slate-600 hover:text-slate-900 px-3.5 py-2 rounded-xl border border-slate-200 hover:bg-slate-50 transition-all cursor-pointer"
              >
                Re-Authenticate
              </button>
            ) : (
              <button
                type="button"
                onClick={handleConnectKite}
                className="text-xs font-bold text-white bg-emerald-600 hover:bg-emerald-700 active:scale-[0.98] px-4 py-2.5 rounded-xl shadow-xs transition-all flex items-center gap-2 cursor-pointer"
              >
                <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
                </svg>
                Connect Zerodha Kite
              </button>
            )}
          </div>
        </div>
      </div>

      {/* =========================================================================
          2. Overview Statistics Banner
          ========================================================================= */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="bg-white border border-slate-200/90 rounded-2xl p-4 shadow-xs">
          <div className="text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
            Total Universe
          </div>
          <div className="text-2xl font-black text-slate-900 mt-1">
            {isUniverseLoading ? "..." : universe?.total_stocks ?? 0}
          </div>
          <div className="text-[11px] text-emerald-700 font-medium mt-1">
            F&O Underlying Equities
          </div>
        </div>

        <div className="bg-white border border-slate-200/90 rounded-2xl p-4 shadow-xs">
          <div className="text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
            Execution Segment
          </div>
          <div className="text-2xl font-black text-slate-900 mt-1">
            NSE Cash
          </div>
          <div className="text-[11px] text-slate-500 font-medium mt-1">
            Instrument Type: EQ
          </div>
        </div>

        <div className="bg-white border border-slate-200/90 rounded-2xl p-4 shadow-xs">
          <div className="text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
            Intraday Margin
          </div>
          <div className="text-2xl font-black text-emerald-700 mt-1">
            5x MIS
          </div>
          <div className="text-[11px] text-slate-500 font-medium mt-1">
            100% Zerodha Approved
          </div>
        </div>

        <div className="bg-white border border-slate-200/90 rounded-2xl p-4 shadow-xs">
          <div className="text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
            Scan Schedule
          </div>
          <div className="text-2xl font-black text-slate-900 mt-1">
            08:30 AM
          </div>
          <div className="text-[11px] text-slate-500 font-medium mt-1">
            Pre-Market Master Sync
          </div>
        </div>
      </div>

      {/* =========================================================================
          3. Table Controls (Search, Page Size, Re-scan Action)
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-2xl shadow-xs overflow-hidden">
        <div className="p-4 sm:p-5 border-b border-slate-100 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div>
            <h2 className="font-bold text-slate-900 text-base">
              NSE Cash Stock Universe (F&O Backed)
            </h2>
            <p className="text-xs text-slate-500 mt-0.5">
              Strictly selected for maximum liquidity, minimal slippage, and zero circuit freeze risk.
            </p>
          </div>

          <div className="flex items-center gap-2.5">
            {/* Search Input */}
            <div className="relative">
              <svg
                className="w-4 h-4 text-slate-400 absolute left-3 top-1/2 -translate-y-1/2"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
              <input
                type="text"
                placeholder="Search symbol or name..."
                value={searchQuery}
                onChange={(e) => {
                  setSearchQuery(e.target.value);
                  setCurrentPage(1);
                }}
                className="pl-9 pr-3 py-2 text-xs rounded-xl border border-slate-200 bg-slate-50/50 focus:bg-white focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500 text-slate-900 placeholder-slate-400 w-48 sm:w-64 transition-all"
              />
            </div>

            {/* Re-Scan Button */}
            <button
              type="button"
              onClick={handleTriggerScan}
              disabled={isScanning}
              className="text-xs font-semibold text-slate-700 bg-slate-100 hover:bg-slate-200 active:scale-[0.98] disabled:opacity-60 px-3.5 py-2 rounded-xl transition-all flex items-center gap-2 cursor-pointer shrink-0"
              title="Download fresh instruments from Kite and refresh the stock universe"
            >
              <svg
                className={`w-3.5 h-3.5 ${isScanning ? "animate-spin text-emerald-600" : ""}`}
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                strokeWidth="2.5"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
                />
              </svg>
              {isScanning ? "Scanning..." : "Re-Scan"}
            </button>
          </div>
        </div>

        {/* =======================================================================
            4. Stock Table
            ======================================================================= */}
        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse">
            <thead>
              <tr className="bg-slate-50/80 border-b border-slate-100 text-[11px] font-bold text-slate-500 uppercase tracking-wider">
                <th className="py-3 px-4">#</th>
                <th className="py-3 px-4">Symbol</th>
                <th className="py-3 px-4">Company Name</th>
                <th className="py-3 px-4">Token</th>
                <th className="py-3 px-4">Lot Size</th>
                <th className="py-3 px-4">Tick</th>
                <th className="py-3 px-4">Type</th>
                <th className="py-3 px-4">Intraday Margin</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 text-xs">
              {isUniverseLoading ? (
                <>
                  {[1, 2, 3, 4, 5, 6, 7].map((i) => (
                    <tr key={i}>
                      <td className="py-4 px-4">
                        <div className="h-4 w-16 bg-slate-200/70 rounded-md animate-shimmer" />
                      </td>
                      <td className="py-4 px-4">
                        <div className="h-4 w-36 bg-slate-200/70 rounded-md animate-shimmer" />
                      </td>
                      <td className="py-4 px-4">
                        <div className="h-5 w-12 bg-slate-200/70 rounded-md animate-shimmer" />
                      </td>
                      <td className="py-4 px-4">
                        <div className="h-4 w-16 bg-slate-200/70 rounded-md animate-shimmer" />
                      </td>
                      <td className="py-4 px-4">
                        <div className="h-4 w-10 bg-slate-200/70 rounded-md animate-shimmer" />
                      </td>
                      <td className="py-4 px-4">
                        <div className="h-4 w-12 bg-slate-200/70 rounded-md animate-shimmer" />
                      </td>
                      <td className="py-4 px-4">
                        <div className="h-5 w-12 bg-slate-200/70 rounded-md animate-shimmer" />
                      </td>
                      <td className="py-4 px-4">
                        <div className="h-5 w-24 bg-slate-200/70 rounded-full animate-shimmer" />
                      </td>
                    </tr>
                  ))}
                </>
              ) : paginatedStocks.length === 0 ? (
                <tr>
                  <td colSpan={8} className="py-12 text-center text-slate-500">
                    <div className="max-w-xs mx-auto space-y-2">
                      <div className="w-10 h-10 rounded-2xl bg-slate-100 text-slate-400 flex items-center justify-center mx-auto">
                        <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
                        </svg>
                      </div>
                      <div className="font-bold text-slate-800 text-sm">
                        {searchQuery ? "No matching stocks found" : "No stocks scanned for today"}
                      </div>
                      <p className="text-xs text-slate-400">
                        {searchQuery
                          ? "Try a different search query."
                          : "Run the 08:30 AM scanner to automatically download and filter the active F&O cash stock universe."}
                      </p>
                      {!searchQuery && (
                        <button
                          type="button"
                          onClick={handleTriggerScan}
                          className="mt-3 text-xs font-bold text-white bg-emerald-600 hover:bg-emerald-700 px-4 py-2 rounded-xl transition-all cursor-pointer"
                        >
                          Run Scanner Now
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ) : (
                paginatedStocks.map((stock, idx) => {
                  const globalIdx = (currentPage - 1) * pageSize + idx + 1;
                  return (
                    <tr
                      key={stock.instrument_token}
                      className="hover:bg-slate-50/70 transition-colors group"
                    >
                      <td className="py-3 px-4 font-mono text-slate-400 text-[11px]">
                        {globalIdx}
                      </td>

                      <td className="py-3 px-4">
                        <div className="flex items-center gap-2">
                          <span className="font-extrabold text-slate-900 group-hover:text-emerald-700 transition-colors">
                            {stock.tradingsymbol}
                          </span>
                          <span className="text-[10px] font-bold px-1.5 py-0.5 rounded bg-slate-100 text-slate-600">
                            {stock.exchange}
                          </span>
                        </div>
                      </td>

                      <td className="py-3 px-4 text-slate-600 truncate max-w-xs font-medium">
                        {stock.name || stock.tradingsymbol}
                      </td>

                      <td className="py-3 px-4 font-mono text-slate-500 text-[11px]">
                        {stock.instrument_token}
                      </td>

                      <td className="py-3 px-4 font-mono text-slate-700">
                        {stock.lot_size}
                      </td>

                      <td className="py-3 px-4 font-mono text-slate-700">
                        ₹{stock.tick_size.toFixed(2)}
                      </td>

                      <td className="py-3 px-4">
                        <span className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-bold bg-blue-50 text-blue-700 border border-blue-200/60">
                          {stock.instrument_type}
                        </span>
                      </td>

                      <td className="py-3 px-4">
                        <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-[11px] font-semibold bg-emerald-50 text-emerald-800 border border-emerald-300/60">
                          <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
                          MIS 5x Eligible
                        </span>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        {/* =======================================================================
            5. Pagination Controls
            ======================================================================= */}
        {filteredStocks.length > 0 && (
          <div className="p-4 border-t border-slate-100 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs text-slate-500">
            <div>
              Showing{" "}
              <strong className="text-slate-900">
                {(currentPage - 1) * pageSize + 1}
              </strong>{" "}
              to{" "}
              <strong className="text-slate-900">
                {Math.min(currentPage * pageSize, filteredStocks.length)}
              </strong>{" "}
              of{" "}
              <strong className="text-slate-900">
                {filteredStocks.length}
              </strong>{" "}
              stocks
            </div>

            <div className="flex items-center gap-2">
              <select
                value={pageSize}
                onChange={(e) => {
                  setPageSize(Number(e.target.value));
                  setCurrentPage(1);
                }}
                className="py-1 px-2 rounded-lg border border-slate-200 bg-white text-xs text-slate-700 focus:outline-none cursor-pointer"
              >
                <option value={25}>25 per page</option>
                <option value={50}>50 per page</option>
                <option value={100}>100 per page</option>
              </select>

              <button
                type="button"
                onClick={() => setCurrentPage((p) => Math.max(1, p - 1))}
                disabled={currentPage === 1}
                className="px-2.5 py-1 rounded-lg border border-slate-200 bg-white hover:bg-slate-50 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer font-medium"
              >
                Prev
              </button>

              <span className="px-2 font-mono">
                {currentPage} / {totalPages}
              </span>

              <button
                type="button"
                onClick={() => setCurrentPage((p) => Math.min(totalPages, p + 1))}
                disabled={currentPage === totalPages}
                className="px-2.5 py-1 rounded-lg border border-slate-200 bg-white hover:bg-slate-50 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer font-medium"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
