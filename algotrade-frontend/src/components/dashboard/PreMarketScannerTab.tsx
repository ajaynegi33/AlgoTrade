"use client";

import { useState, useEffect } from "react";

export interface PreMarketCandidate {
  id: number;
  scanDate: string;
  symbol: string;
  previousClose: number;
  openPrice: number;
  gapPct: number;
  preOpenVolume: number;
  pdh?: number;
  upperCircuit?: number;
  lowerCircuit?: number;
  circuitHeadroomPct?: number;
  rankingScore?: number;
  rankOrder?: number;
  status: "SHORTLISTED" | "QUALIFIED" | "RESERVE" | "REJECTED";
  rejectionReason?: string | null;
  scannedAt: string;
}

export default function PreMarketScannerTab() {
  const [candidates, setCandidates] = useState<PreMarketCandidate[]>([]);
  const [shortlisted, setShortlisted] = useState<PreMarketCandidate[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [scanning, setScanning] = useState<boolean>(false);
  const [syncingUniverse, setSyncingUniverse] = useState<boolean>(false);
  const [filter, setFilter] = useState<"ALL" | "SHORTLISTED" | "REJECTED">("ALL");
  const [lastScanTime, setLastScanTime] = useState<string | null>(null);
  const [universeInfo, setUniverseInfo] = useState<{
    totalFnoSymbols: number;
    totalEquities: number;
    lastSyncedAt: string | null;
  } | null>(null);

  const BACKEND_URL = process.env.NEXT_PUBLIC_BACKEND_URL || "http://localhost:8080";

  const fetchScanResults = async () => {
    try {
      setLoading(true);
      const res = await fetch(`${BACKEND_URL}/api/scanner/latest`);
      if (res.ok) {
        const data = await res.json();
        setCandidates(data.allCandidates || []);
        setShortlisted(data.shortlisted || []);
        if (data.lastScanTimestamp) {
          setLastScanTime(new Date(data.lastScanTimestamp).toLocaleTimeString("en-IN"));
        }
      }
    } catch (err) {
      console.error("Failed to load pre-market scan results:", err);
    } finally {
      setLoading(false);
    }
  };

  const fetchUniverseInfo = async () => {
    try {
      const res = await fetch(`${BACKEND_URL}/api/scanner/universe`);
      if (res.ok) {
        const data = await res.json();
        setUniverseInfo({
          totalFnoSymbols: data.totalFnoSymbols || 0,
          totalEquities: data.totalEquities || 0,
          lastSyncedAt: data.lastSyncedAt,
        });
      }
    } catch (err) {
      console.error("Failed to load universe info:", err);
    }
  };

  const handleTriggerScan = async () => {
    try {
      setScanning(true);
      const res = await fetch(`${BACKEND_URL}/api/scanner/trigger`, { method: "POST" });
      if (res.ok) {
        await fetchScanResults();
      }
    } catch (err) {
      console.error("Failed to trigger pre-market scan:", err);
    } finally {
      setScanning(false);
    }
  };

  const handleSyncUniverse = async () => {
    try {
      setSyncingUniverse(true);
      const res = await fetch(`${BACKEND_URL}/api/scanner/universe/sync`, { method: "POST" });
      if (res.ok) {
        await fetchUniverseInfo();
        await fetchScanResults();
      }
    } catch (err) {
      console.error("Failed to sync market universe:", err);
    } finally {
      setSyncingUniverse(false);
    }
  };

  useEffect(() => {
    fetchScanResults();
    fetchUniverseInfo();
  }, []);

  const filteredList = candidates.filter((c) => {
    if (filter === "SHORTLISTED") return c.status === "SHORTLISTED";
    if (filter === "REJECTED") return c.status === "REJECTED";
    return true;
  });

  return (
    <div className="flex flex-col gap-6 animate-fadeIn pb-12">
      {/* Header & Controls Card */}
      <div className="bg-slate-900/80 border border-slate-800 rounded-2xl p-5 sm:p-6 shadow-xl backdrop-blur-md">
        <div className="flex flex-col md:flex-row items-start md:items-center justify-between pb-5 border-b border-slate-800 gap-4">
          <div>
            <div className="flex items-center gap-2 flex-wrap">
              <span className="w-8 h-8 rounded-lg bg-amber-500/10 border border-amber-500/30 flex items-center justify-center text-amber-400 font-bold text-base">
                🌅
              </span>
              <h2 className="text-xl sm:text-2xl font-black text-white tracking-tight">
                09:12 Pre-Market Auction Scanner
              </h2>
              <span className="text-xs px-2.5 py-0.5 rounded-full bg-amber-500/20 text-amber-300 border border-amber-500/40 font-mono font-bold">
                NSE Buffer Window
              </span>
              <span className="text-xs px-2.5 py-0.5 rounded-full bg-emerald-500/20 text-emerald-300 border border-emerald-500/40 font-mono font-bold">
                Zero Fixed Lists • Live Exchange Sync
              </span>
            </div>
            <p className="text-xs sm:text-sm text-slate-400 mt-1">
              Scans only the <strong className="text-white font-semibold">{universeInfo?.totalFnoSymbols || 210} high-liquidity F&amp;O stocks</strong> (filtered dynamically from all {universeInfo?.totalEquities || 2979} listed NSE equities) directly via Kite master stream.
            </p>
          </div>

          <div className="flex items-center gap-2 sm:gap-3 flex-wrap">
            {lastScanTime && (
              <span className="text-xs text-slate-400 font-mono hidden sm:inline">
                Last Scan: <strong className="text-white">{lastScanTime}</strong>
              </span>
            )}
            <button
              onClick={handleSyncUniverse}
              disabled={syncingUniverse}
              title="Downloads fresh daily instrument master from exchange"
              className="px-3 py-2 rounded-xl text-xs font-semibold bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 transition cursor-pointer disabled:opacity-50 flex items-center gap-1.5"
            >
              {syncingUniverse ? (
                <>
                  <span className="w-3 h-3 border-2 border-slate-300 border-t-transparent rounded-full animate-spin" />
                  <span>Syncing...</span>
                </>
              ) : (
                <>
                  <span>🔄</span>
                  <span>Sync Universe ({universeInfo?.totalFnoSymbols || 210})</span>
                </>
              )}
            </button>
            <button
              onClick={handleTriggerScan}
              disabled={scanning}
              className="px-4 py-2 rounded-xl text-xs font-bold bg-gradient-to-r from-amber-500 to-orange-500 hover:from-amber-400 hover:to-orange-400 text-slate-950 transition shadow-lg shadow-amber-950/40 cursor-pointer disabled:opacity-50 flex items-center gap-2"
            >
              {scanning ? (
                <>
                  <span className="w-3.5 h-3.5 border-2 border-slate-950 border-t-transparent rounded-full animate-spin" />
                  <span>Scanning Market...</span>
                </>
              ) : (
                <>
                  <span>⚡ Run Pre-Market Scan</span>
                </>
              )}
            </button>
          </div>
        </div>

        {/* Diagnostic KPI Strip */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 sm:gap-4 mt-5">
          <div className="bg-slate-950/60 border border-slate-800/80 rounded-xl p-3.5">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">Universe Screened</span>
            <span className="text-xl font-black font-mono text-white mt-0.5 block">{candidates.length || universeInfo?.totalFnoSymbols || 210} Stocks</span>
            <span className="text-[10px] text-emerald-400">Active F&amp;O Equities Only</span>
          </div>

          <div className="bg-slate-950/60 border border-slate-800/80 rounded-xl p-3.5">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">Shortlisted Today</span>
            <span className="text-xl font-black font-mono text-emerald-400 mt-0.5 block">{shortlisted.length} Candidates</span>
            <span className="text-[10px] text-slate-500">Passed all 4 quantitative gates</span>
          </div>

          <div className="bg-slate-950/60 border border-slate-800/80 rounded-xl p-3.5">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">Gap Sweet Spot</span>
            <span className="text-xl font-black font-mono text-amber-400 mt-0.5 block">+0.60% to +2.50%</span>
            <span className="text-[10px] text-slate-500">Filters flat and exhausted gaps</span>
          </div>

          <div className="bg-slate-950/60 border border-slate-800/80 rounded-xl p-3.5">
            <span className="text-[11px] text-slate-400 uppercase tracking-wider block">Next Pipeline Step</span>
            <span className="text-xl font-black font-mono text-sky-400 mt-0.5 block">09:15 - 09:30</span>
            <span className="text-[10px] text-slate-500">Continuous 15-Min ORB Validation</span>
          </div>
        </div>
      </div>

      {/* Shortlisted Momentum Candidates Cards */}
      {shortlisted.length > 0 && (
        <div>
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-base font-bold text-white tracking-tight flex items-center gap-2">
              <span>🎯 Top Shortlisted Momentum Candidates</span>
              <span className="text-xs px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-300 border border-emerald-500/30 font-mono">
                Active Watchlist
              </span>
            </h3>
            <span className="text-xs text-slate-400">Streamed to KiteTicker for 09:15 open</span>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {shortlisted.map((c, idx) => (
              <div
                key={c.id}
                className="bg-slate-900/90 border border-slate-800 rounded-xl p-4 shadow-lg hover:border-slate-700 transition"
              >
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-2">
                    <span className="w-6 h-6 rounded-lg bg-amber-500/20 text-amber-300 border border-amber-500/30 flex items-center justify-center text-xs font-bold font-mono">
                      #{idx + 1}
                    </span>
                    <div>
                      <h4 className="font-black text-white text-base tracking-tight">{c.symbol}</h4>
                      <span className="text-[10px] text-slate-400 font-mono">
                        Prev Close: ₹{c.previousClose.toFixed(2)}
                      </span>
                    </div>
                  </div>
                  <div className="text-right">
                    <div className="flex items-center justify-end gap-1.5 flex-wrap">
                      {c.rankingScore !== undefined && c.rankingScore !== null && (
                        <span className="px-1.5 py-0.5 rounded text-[10px] font-mono font-bold bg-amber-500/15 text-amber-300 border border-amber-500/30">
                          Score {c.rankingScore.toFixed(2)}
                        </span>
                      )}
                      <span className="px-2 py-0.5 rounded text-xs font-mono font-bold bg-emerald-500/20 text-emerald-300 border border-emerald-500/30">
                        +{c.gapPct.toFixed(2)}%
                      </span>
                    </div>
                    <span className="block text-[10px] text-slate-400 font-mono mt-0.5">
                      Open: ₹{c.openPrice.toFixed(2)}
                    </span>
                  </div>
                </div>

                <div className="grid grid-cols-3 gap-2 mt-4 pt-3 border-t border-slate-800 text-[11px] font-mono">
                  <div>
                    <span className="text-slate-500 block text-[10px]">Pre-Open Vol</span>
                    <span className="font-bold text-white">{c.preOpenVolume.toLocaleString("en-IN")}</span>
                  </div>
                  <div>
                    <span className="text-slate-500 block text-[10px]">Upper Circuit</span>
                    <span className="font-bold text-purple-400">
                      {c.upperCircuit ? `₹${c.upperCircuit.toFixed(1)}` : "Flexed"}
                    </span>
                  </div>
                  <div>
                    <span className="text-slate-500 block text-[10px]">Circuit Room</span>
                    <span className="font-bold text-teal-400">
                      {c.circuitHeadroomPct ? `+${c.circuitHeadroomPct.toFixed(1)}%` : "Safe"}
                    </span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Complete Audit & Rejection Telemetry Table */}
      <div className="bg-slate-900/80 border border-slate-800 rounded-2xl p-5 sm:p-6 shadow-xl backdrop-blur-md">
        <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between pb-4 border-b border-slate-800 gap-3">
          <div>
            <h3 className="text-base font-bold text-white tracking-tight">
              Pre-Market Screening Audit &amp; Rejection Diagnostics
            </h3>
            <p className="text-xs text-slate-400 mt-0.5">
              Full diagnostic record explaining why each stock was accepted or rejected.
            </p>
          </div>

          <div className="flex items-center gap-1 bg-slate-950 p-1 rounded-xl border border-slate-800 text-xs">
            <button
              onClick={() => setFilter("ALL")}
              className={`px-3 py-1 rounded-lg transition cursor-pointer ${
                filter === "ALL" ? "bg-slate-800 text-white font-bold" : "text-slate-400 hover:text-white"
              }`}
            >
              All ({candidates.length})
            </button>
            <button
              onClick={() => setFilter("SHORTLISTED")}
              className={`px-3 py-1 rounded-lg transition cursor-pointer ${
                filter === "SHORTLISTED" ? "bg-emerald-500/20 text-emerald-300 font-bold" : "text-slate-400 hover:text-white"
              }`}
            >
              Shortlisted ({shortlisted.length})
            </button>
            <button
              onClick={() => setFilter("REJECTED")}
              className={`px-3 py-1 rounded-lg transition cursor-pointer ${
                filter === "REJECTED" ? "bg-rose-500/20 text-rose-300 font-bold" : "text-slate-400 hover:text-white"
              }`}
            >
              Rejected ({candidates.filter((c) => c.status === "REJECTED").length})
            </button>
          </div>
        </div>

        <div className="overflow-x-auto max-h-[480px] overflow-y-auto mt-4 scrollbar-thin scrollbar-thumb-slate-800">
          <table className="w-full text-left text-xs font-mono">
            <thead className="bg-slate-950/80 sticky top-0 z-10 text-[11px] font-semibold text-slate-400 uppercase tracking-wider border-b border-slate-800">
              <tr>
                <th className="py-3 px-3">#</th>
                <th className="py-3 px-3">Stock</th>
                <th className="py-3 px-3">Prev Close</th>
                <th className="py-3 px-3">Open (09:12)</th>
                <th className="py-3 px-3">Gap %</th>
                <th className="py-3 px-3">Auction Vol</th>
                <th className="py-3 px-3">Circuit Room</th>
                <th className="py-3 px-3">Score</th>
                <th className="py-3 px-3">Status</th>
                <th className="py-3 px-3">Diagnostic Reason</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60">
              {filteredList.map((c) => (
                <tr key={c.id} className="hover:bg-slate-800/30 transition-colors">
                  <td className="py-2.5 px-3 text-slate-500">{c.rankOrder || "-"}</td>
                  <td className="py-2.5 px-3">
                    <span className="font-bold text-white bg-slate-800 px-2 py-0.5 rounded border border-slate-700">
                      {c.symbol}
                    </span>
                  </td>
                  <td className="py-2.5 px-3 text-slate-300">₹{c.previousClose.toFixed(2)}</td>
                  <td className="py-2.5 px-3 text-slate-300">₹{c.openPrice.toFixed(2)}</td>
                  <td className="py-2.5 px-3">
                    <span className={`font-bold ${c.gapPct >= 0 ? "text-emerald-400" : "text-rose-400"}`}>
                      {c.gapPct >= 0 ? "+" : ""}{c.gapPct.toFixed(2)}%
                    </span>
                  </td>
                  <td className="py-2.5 px-3 text-slate-300">{c.preOpenVolume.toLocaleString("en-IN")}</td>
                  <td className="py-2.5 px-3 text-slate-400">
                    {c.circuitHeadroomPct ? `+${c.circuitHeadroomPct.toFixed(1)}%` : "Safe"}
                  </td>
                  <td className="py-2.5 px-3">
                    <span className="font-bold text-amber-300 font-mono bg-amber-500/10 px-1.5 py-0.5 rounded border border-amber-500/20 text-[11px]">
                      {c.rankingScore !== undefined && c.rankingScore !== null ? c.rankingScore.toFixed(2) : "-"}
                    </span>
                  </td>
                  <td className="py-2.5 px-3">
                    <span
                      className={`px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider border ${
                        c.status === "SHORTLISTED"
                          ? "bg-emerald-500/10 text-emerald-300 border-emerald-500/30"
                          : c.status === "RESERVE"
                          ? "bg-amber-500/10 text-amber-300 border-amber-500/30"
                          : "bg-rose-500/10 text-rose-300 border-rose-500/30"
                      }`}
                    >
                      {c.status}
                    </span>
                  </td>
                  <td className="py-2.5 px-3 text-slate-400 text-[11px]">
                    {c.rejectionReason || <span className="text-emerald-400/80">✓ Passed all gates</span>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
