"use client";

import { useState } from "react";
import { ScoredCandidate } from "../types";
import { TickUpdate } from "@/hooks/useEngineWebSocket";
import { SkeletonLeaderboard } from "../common/Skeleton";

interface LeaderboardTabProps {
  leaderboard: ScoredCandidate[];
  lastUpdated: Date | null;
  marketPhase?: string;
  backendUrl?: string;
  latestTicks?: Record<string, TickUpdate>;
  onSelectSymbol?: (symbol: string) => void;
}

export default function LeaderboardTab({
  leaderboard,
  lastUpdated,
  marketPhase,
  backendUrl,
  latestTicks,
  onSelectSymbol,
}: LeaderboardTabProps) {
  const [triggeringScan, setTriggeringScan] = useState(false);
  const [triggerMessage, setTriggerMessage] = useState<string | null>(null);
  const [showExplanation, setShowExplanation] = useState(false);

  if (!leaderboard) {
    return <SkeletonLeaderboard />;
  }

  const formatPhase = (phase: string) => {
    switch (phase) {
      case "PRE_MARKET":
        return { label: "Pre-Market Preparation (Before 09:00)", color: "text-slate-400 bg-slate-500/10 border-slate-500/30", icon: "☕" };
      case "PRE_OPEN":
        return { label: "Pre-Open Auction (09:00 - 09:15)", color: "text-amber-400 bg-amber-500/10 border-amber-500/30", icon: "🌅" };
      case "OPENING_DISCOVERY":
        return { label: "Opening Discovery (09:15 - 09:20)", color: "text-amber-400 bg-amber-500/10 border-amber-500/30", icon: "🌅" };
      case "WAVE_1_MORNING_MOMENTUM":
        return { label: "Wave 1: Morning Momentum (09:20 - 10:30)", color: "text-emerald-400 bg-emerald-500/10 border-emerald-500/30", icon: "🌊" };
      case "MIDDAY_CONSOLIDATION":
        return { label: "Midday Consolidation (10:30 - 12:30)", color: "text-blue-400 bg-blue-500/10 border-blue-500/30", icon: "⚖️" };
      case "WAVE_2_EUROPEAN_INFLUX":
        return { label: "Wave 2: European Liquidity (12:30 - 13:45)", color: "text-cyan-400 bg-cyan-500/10 border-cyan-500/30", icon: "🌍" };
      case "WAVE_3_AFTERNOON_RALLY":
        return { label: "Wave 3: Afternoon Breakout (13:45 - 14:45)", color: "text-purple-400 bg-purple-500/10 border-purple-500/30", icon: "🚀" };
      case "SESSION_SHUTDOWN":
        return { label: "Session Cooldown (14:45 - 15:05)", color: "text-orange-400 bg-orange-500/10 border-orange-500/30", icon: "🔒" };
      case "SQUARE_OFF":
        return { label: "Mandatory Square-Off (15:05 - 15:30)", color: "text-rose-400 bg-rose-500/10 border-rose-500/30", icon: "⏹️" };
      case "MARKET_CLOSED":
        return { label: "Market Closed (Session Ended 15:30)", color: "text-slate-400 bg-slate-800/80 border-slate-700", icon: "🌙" };
      default:
        return { label: phase ? phase.replace(/_/g, " ") : "Live Session", color: "text-slate-400 bg-slate-800 border-slate-700", icon: "⚡" };
    }
  };

  const currentPhaseInfo = formatPhase(marketPhase || "OPENING_DISCOVERY");

  const handleManualScan = async () => {
    const url = backendUrl || process.env.NEXT_PUBLIC_BACKEND_URL || "http://localhost:8080";
    setTriggeringScan(true);
    setTriggerMessage(null);
    try {
      const res = await fetch(`${url}/api/dashboard/scan/trigger`, { method: "POST" });
      const data = await res.json();
      if (res.ok && data.success) {
        setTriggerMessage("✅ Continuous scan cycle executed! Leaderboard updated.");
      } else {
        setTriggerMessage(data.message || "Scan cycle completed.");
      }
    } catch {
      setTriggerMessage("Failed to reach backend engine.");
    } finally {
      setTriggeringScan(false);
      setTimeout(() => setTriggerMessage(null), 5000);
    }
  };

  return (
    <div className="space-y-4 animate-fadeIn">
      {/* Header Card */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-4 sm:p-6 shadow-xl">
        <div className="flex flex-col lg:flex-row items-start lg:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2.5 flex-wrap">
              <h2 className="text-lg sm:text-xl font-bold text-white flex items-center gap-2">
                🏆 Top Performers &amp; Continuous Scoring Breakdown
              </h2>
              <span className={`px-2.5 py-0.5 rounded-full text-xs font-semibold border flex items-center gap-1.5 ${currentPhaseInfo.color}`}>
                <span>{currentPhaseInfo.icon}</span>
                <span>{currentPhaseInfo.label}</span>
              </span>
            </div>
            <p className="text-xs text-slate-400 mt-1">
              ⚡ Real-time WebSocket tick stream across all 210 F&amp;O stocks with 0ms Breakout Radar. Threshold: ≥ 75 / 100 points + Mandatory PDH Breakout Gate.
            </p>
          </div>

          <div className="flex items-center gap-3 flex-wrap">
            <div className="flex items-center gap-2 text-xs font-mono text-slate-400 bg-slate-950/70 px-3 py-1.5 rounded-lg border border-slate-800">
              <span>{lastUpdated ? lastUpdated.toLocaleTimeString() : "--:--:--"}</span>
            </div>

            <button
              onClick={handleManualScan}
              disabled={triggeringScan}
              className="px-3.5 py-1.5 rounded-lg text-xs font-bold bg-slate-800 hover:bg-slate-700 text-white border border-slate-700 transition flex items-center gap-1.5 cursor-pointer disabled:opacity-50"
              title="Trigger an immediate scan cycle across all 210 F&O stocks"
            >
              {triggeringScan ? (
                <>
                  <span className="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin" />
                  <span>Scanning...</span>
                </>
              ) : (
                <>
                  <span>⚡</span>
                  <span>Scan Now</span>
                </>
              )}
            </button>
          </div>
        </div>

        {triggerMessage && (
          <div className="mt-3 p-2.5 rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs font-mono">
            {triggerMessage}
          </div>
        )}
      </div>

      {/* Diagnostics / FAQ Accordion */}
      <div className="bg-slate-900/40 border border-slate-800 rounded-xl p-3.5">
        <button
          onClick={() => setShowExplanation(!showExplanation)}
          className="w-full flex items-center justify-between text-left text-xs font-semibold text-slate-300 hover:text-white transition cursor-pointer"
        >
          <span className="flex items-center gap-2">
            <span>💡</span>
            <span>Why can a stock score 80/100 points and still be marked DISQUALIFIED?</span>
          </span>
          <span className="text-slate-500 text-sm font-mono">{showExplanation ? "▲" : "▼"}</span>
        </button>

        {showExplanation && (
          <div className="mt-3 pt-3 border-t border-slate-800/80 text-xs text-slate-400 space-y-2 leading-relaxed">
            <p>
              In professional quantitative trading, high volume and VWAP momentum alone are not enough. The engine enforces two layers of defense:
            </p>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-2">
              <div className="bg-slate-950/70 p-3 rounded-lg border border-slate-800">
                <span className="font-bold text-amber-300 block mb-1">1. Mandatory Hard Gate (LTP &gt; PDH)</span>
                <p className="text-[11px] text-slate-400">
                  Price must break strictly above the Previous Day High (<code className="text-amber-200">PDH &gt; 0 pts</code>). Buying below yesterday&apos;s high is buying inside historical range resistance, where institutional traps occur. If <code className="text-amber-200">PDH+ATR = 0</code>, the stock is disqualified regardless of points.
                </p>
              </div>
              <div className="bg-slate-950/70 p-3 rounded-lg border border-slate-800">
                <span className="font-bold text-emerald-300 block mb-1">2. Points Score (Threshold ≥ 75)</span>
                <p className="text-[11px] text-slate-400">
                  Once hard gates pass, the stock needs ≥ 75/100 points across RVOL (/30), VWAP distance (/25), Order Book depth buyer dominance (/25), and Breakout ATR headroom (/20).
                </p>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Leaderboard Table View */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl overflow-hidden shadow-xl">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs sm:text-sm">
            <thead className="bg-slate-950/80 border-b border-slate-800 text-[11px] uppercase tracking-wider text-slate-400 font-mono">
              <tr>
                <th className="py-3 px-4">Rank</th>
                <th className="py-3 px-4">Symbol</th>
                <th className="py-3 px-4">LTP</th>
                <th className="py-3 px-4">Day Change %</th>
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
                  <td colSpan={11} className="py-10 text-center text-slate-500 font-sans">
                    <div className="flex flex-col items-center justify-center gap-2">
                      <span className="text-xl">📡</span>
                      <span>Real-time WebSocket engine is streaming the 210 F&amp;O universe...</span>
                      <span className="text-xs text-slate-600">Click &quot;Scan Now&quot; above to run an instant quantitative scan.</span>
                    </div>
                  </td>
                </tr>
              ) : (
                leaderboard.map((item) => {
                  const isTopPick = item.rank === 1 && item.status === "SELECTED FOR TRADE";
                  const isQualified = item.status === "SELECTED FOR TRADE" || item.status === "RUNNER UP";
                  const failedPdhGate = item.totalScore >= 75 && item.pdhScore === 0;
                  const liveTick = latestTicks?.[item.symbol];
                  const currentLtp = liveTick?.ltp || item.ltp;

                  return (
                    <tr
                      key={item.symbol}
                      onClick={() => onSelectSymbol && onSelectSymbol(item.symbol)}
                      className={`hover:bg-slate-800/40 transition-colors cursor-pointer ${
                        isTopPick ? "bg-emerald-500/10 font-semibold" : ""
                      }`}
                      title="Click to inspect this stock in Explorer"
                    >
                      <td className="py-3.5 px-4 font-bold text-slate-300">#{item.rank}</td>
                      <td className="py-3.5 px-4 font-sans font-bold text-white flex items-center gap-2">
                        {item.symbol}
                        {isTopPick && (
                          <span className="text-[10px] px-1.5 py-0.2 rounded bg-emerald-500 text-black font-mono font-bold animate-pulse">
                            TOP PICK
                          </span>
                        )}
                      </td>
                      <td className="py-3.5 px-4 font-mono font-semibold text-white">
                        ₹{currentLtp.toFixed(2)}
                      </td>
                      <td className="py-3.5 px-4 font-mono font-semibold" title="Live Day Change % (LTP vs Previous Day Close)">
                        {liveTick && liveTick.changePct !== undefined ? (
                          <span
                            className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-bold ${
                              liveTick.changePct >= 0
                                ? "bg-emerald-500/10 text-emerald-400 border border-emerald-500/30"
                                : "bg-rose-500/10 text-rose-400 border border-rose-500/30"
                            }`}
                          >
                            <span>{liveTick.changePct >= 0 ? "▲" : "▼"}</span>
                            <span>{liveTick.changePct >= 0 ? `+${liveTick.changePct.toFixed(2)}%` : `${liveTick.changePct.toFixed(2)}%`}</span>
                          </span>
                        ) : (
                          <span className="text-slate-500 font-normal">--</span>
                        )}
                      </td>
                      {(() => {
                        const isZeroGap = Math.abs(item.gapPct) < 0.005;
                        const gapColor = isZeroGap ? "text-slate-400" : item.gapPct > 0 ? "text-emerald-400" : "text-rose-400";
                        const gapText = isZeroGap ? "0.00%" : item.gapPct > 0 ? `+${item.gapPct.toFixed(2)}%` : `${item.gapPct.toFixed(2)}%`;
                        return (
                          <td className={`py-3.5 px-4 font-mono ${gapColor}`} title="Pre-Open Gap % (Open vs Previous Day Close)">
                            <span>{gapText}</span>
                          </td>
                        );
                      })()}
                      <td className="py-3.5 px-4 text-slate-300">{item.rvolScore}</td>
                      <td className="py-3.5 px-4 text-slate-300">{item.vwapScore}</td>
                      <td className="py-3.5 px-4 text-slate-300">{item.depthImbalanceScore}</td>
                      <td className="py-3.5 px-4 text-slate-300">
                        {item.pdhScore > 0 ? (
                          <span className="text-emerald-400 font-bold">+{item.pdhScore}</span>
                        ) : (
                          <span className="text-slate-500" title="Trading below Previous Day High (PDH)">0</span>
                        )}
                      </td>
                      <td className="py-3.5 px-4 font-bold text-base">
                        <span className={item.totalScore >= 75 ? (isQualified ? "text-emerald-400" : "text-amber-400") : "text-slate-400"}>
                          {item.totalScore}
                        </span>
                      </td>
                      <td className="py-3.5 px-4 font-sans">
                        {item.status === "SELECTED FOR TRADE" ? (
                          <span className="px-2.5 py-1 rounded text-[11px] font-bold tracking-wider bg-emerald-500/20 text-emerald-400 border border-emerald-500/40 shadow-sm shadow-emerald-950">
                            SELECTED FOR TRADE
                          </span>
                        ) : item.status === "RUNNER UP" ? (
                          <span className="px-2.5 py-1 rounded text-[11px] font-semibold tracking-wider bg-sky-500/10 text-sky-400 border border-sky-500/30">
                            RUNNER UP (QUALIFIED)
                          </span>
                        ) : failedPdhGate ? (
                          <span
                            className="px-2 py-0.5 rounded text-[10px] font-semibold tracking-wider bg-amber-500/10 text-amber-400 border border-amber-500/30 flex items-center gap-1 w-fit"
                            title="Candidate scored 75+ points but failed mandatory Hard Gate: LTP is below Previous Day High (PDH)"
                          >
                            <span>⚠️</span>
                            <span>DISQUALIFIED (LTP &lt; PDH)</span>
                          </span>
                        ) : (
                          <span className="px-2 py-0.5 rounded text-[11px] font-semibold tracking-wider bg-slate-800 text-slate-500">
                            DISQUALIFIED (&lt; 75 PTS)
                          </span>
                        )}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
