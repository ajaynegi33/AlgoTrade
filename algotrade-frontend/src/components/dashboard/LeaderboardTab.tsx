"use client";

import { ScoredCandidate } from "../types";
import { SkeletonLeaderboard } from "../common/Skeleton";

interface LeaderboardTabProps {
  leaderboard: ScoredCandidate[];
  lastUpdated: Date | null;
  onSelectSymbol?: (symbol: string) => void;
}

export default function LeaderboardTab({
  leaderboard,
  lastUpdated,
  onSelectSymbol,
}: LeaderboardTabProps) {
  if (!leaderboard) {
    return <SkeletonLeaderboard />;
  }

  return (
    <div className="bg-slate-900/60 border border-slate-800 rounded-2xl overflow-hidden shadow-xl animate-fadeIn">
      <div className="p-4 sm:p-6 border-b border-slate-800 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
        <div>
          <h2 className="text-lg sm:text-xl font-bold text-white flex items-center gap-2">
            🏆 Top 10 Performers & Scoring Breakdown
          </h2>
          <p className="text-xs text-slate-400">
            Evaluated at 09:30:00 AM IST. Minimum threshold to trigger trade: 75 / 100 points.
          </p>
        </div>
        <span className="text-xs font-mono text-slate-500">
          Last updated: {lastUpdated ? lastUpdated.toLocaleTimeString() : "--:--:--"}
        </span>
      </div>

      {/* Desktop / Tablet Table View */}
      <div className="overflow-x-auto">
        <table className="w-full text-left text-xs sm:text-sm">
          <thead className="bg-slate-950/80 border-b border-slate-800 text-[11px] uppercase tracking-wider text-slate-400 font-mono">
            <tr>
              <th className="py-3 px-4">Rank</th>
              <th className="py-3 px-4">Symbol</th>
              <th className="py-3 px-4">LTP</th>
              <th className="py-3 px-4">Pre-Open Gap</th>
              <th className="py-3 px-4">RVOL (/30)</th>
              <th className="py-3 px-4">VWAP (/25)</th>
              <th className="py-3 px-4">Depth Imb (/25)</th>
              <th className="py-3 px-4">PDH+ATR (/20)</th>
              <th className="py-3 px-4">Total (/100)</th>
              <th className="py-3 px-4">Decision</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800/60 font-mono">
            {leaderboard.length === 0 ? (
              <tr>
                <td colSpan={10} className="py-8 text-center text-slate-500 font-sans">
                  No candidates evaluated yet. Waiting for 09:30:00 AM scoring evaluation.
                </td>
              </tr>
            ) : (
              leaderboard.map((item) => (
                <tr
                  key={item.symbol}
                  onClick={() => onSelectSymbol && onSelectSymbol(item.symbol)}
                  className={`hover:bg-slate-800/40 transition-colors cursor-pointer ${
                    item.rank === 1 && item.totalScore >= 75 ? "bg-emerald-500/5 font-semibold" : ""
                  }`}
                  title="Click to inspect this stock in Explorer"
                >
                  <td className="py-3.5 px-4 font-bold text-slate-300">#{item.rank}</td>
                  <td className="py-3.5 px-4 font-sans font-bold text-white flex items-center gap-2">
                    {item.symbol}
                    {item.rank === 1 && item.totalScore >= 75 && (
                      <span className="text-[10px] px-1.5 py-0.2 rounded bg-emerald-500 text-black font-mono">
                        TOP PICK
                      </span>
                    )}
                  </td>
                  <td className="py-3.5 px-4 text-slate-200">₹{item.ltp.toFixed(2)}</td>
                  <td className="py-3.5 px-4 text-emerald-400">+{item.gapPct.toFixed(2)}%</td>
                  <td className="py-3.5 px-4 text-slate-300">{item.rvolScore}</td>
                  <td className="py-3.5 px-4 text-slate-300">{item.vwapScore}</td>
                  <td className="py-3.5 px-4 text-slate-300">{item.depthImbalanceScore}</td>
                  <td className="py-3.5 px-4 text-slate-300">{item.pdhScore}</td>
                  <td className="py-3.5 px-4 font-bold text-base">
                    <span className={item.totalScore >= 75 ? "text-emerald-400" : "text-slate-400"}>
                      {item.totalScore}
                    </span>
                  </td>
                  <td className="py-3.5 px-4 font-sans">
                    <span
                      className={`px-2 py-0.5 rounded text-[11px] font-semibold tracking-wider ${
                        item.status === "SELECTED FOR TRADE"
                          ? "bg-emerald-500/20 text-emerald-400 border border-emerald-500/30"
                          : item.status === "RUNNER UP"
                          ? "bg-sky-500/10 text-sky-400 border border-sky-500/20"
                          : "bg-slate-800 text-slate-500"
                      }`}
                    >
                      {item.status}
                    </span>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
