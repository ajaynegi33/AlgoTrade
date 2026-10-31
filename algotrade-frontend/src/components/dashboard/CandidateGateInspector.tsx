"use client";

import React, { memo } from "react";
import Link from "next/link";
import { CandidateGateDiagnosticData } from "@/lib/types";

interface CandidateGateInspectorProps {
  candidates?: CandidateGateDiagnosticData[];
}

function GateCheckPill({ label, ok }: { label: string; ok: boolean }) {
  return (
    <span
      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-[10px] font-bold border ${
        ok
          ? "bg-emerald-50 text-emerald-800 border-emerald-200/90"
          : "bg-slate-100 text-slate-400 border-slate-200/80"
      }`}
    >
      <span className={`w-1.5 h-1.5 rounded-full ${ok ? "bg-emerald-500" : "bg-slate-300"}`} />
      <span>{label}</span>
    </span>
  );
}

export const CandidateGateInspector = memo(function CandidateGateInspector({
  candidates = [],
}: CandidateGateInspectorProps) {
  if (!candidates || candidates.length === 0) {
    return null;
  }

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs font-mono space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 border-b border-slate-100 pb-3.5">
        <div>
          <h3 className="text-xs font-black text-slate-900 uppercase tracking-wider">
            Top 5 Candidate Execution Gate Inspector
          </h3>
          <p className="text-[11px] text-slate-500 mt-0.5">
            Live 6-Stage Autonomous Gate Evaluation (Stage 0 Regime • Score • RVOL • Depth • Setup (VWAP/ORB) • 5x MIS &amp; 1.5x Tax Edge)
          </p>
        </div>
        <span className="text-[10px] font-bold px-2.5 py-1 rounded-full bg-slate-100 text-slate-700 border border-slate-200 shrink-0">
          REAL-TIME 1Hz AUDIT
        </span>
      </div>

      <div className="divide-y divide-slate-100">
        {candidates.map((c) => {
          const isReady = c.overall_status === "READY";
          const isActive = c.overall_status === "ACTIVE_OR_COOLDOWN";

          return (
            <div
              key={c.symbol}
              className="py-3 first:pt-0 last:pb-0 flex flex-col lg:flex-row lg:items-center justify-between gap-3"
            >
              {/* Left: Rank, Symbol, Sector, Score, RVOL */}
              <div className="flex items-center gap-3 min-w-0">
                <span className="w-6 h-6 rounded-lg bg-slate-100 border border-slate-200 text-[11px] font-black text-slate-700 flex items-center justify-center shrink-0">
                  #{c.rank}
                </span>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <Link
                      href={`/stock/${encodeURIComponent(c.symbol)}`}
                      className="text-sm font-black text-slate-900 hover:text-emerald-700 hover:underline transition-colors"
                    >
                      {c.symbol}
                    </Link>
                    <span className="text-[10px] text-slate-400 truncate">{c.sector}</span>
                    <span className="text-xs font-bold text-slate-800">
                      ₹{c.ltp.toFixed(2)}
                    </span>
                  </div>
                  <div className="text-[10px] text-slate-500 flex items-center gap-2 mt-0.5">
                    <span>
                      Score: <strong className="text-slate-800">{c.bullish_score.toFixed(1)}</strong>
                    </span>
                    <span>•</span>
                    <span>
                      RVOL: <strong className="text-slate-800">{c.composite_rvol.toFixed(2)}x</strong>
                    </span>
                    <span>•</span>
                    <span>
                      EV:{" "}
                      <strong className={c.ev >= 0.25 ? "text-emerald-700" : "text-slate-700"}>
                        {c.ev >= 0 ? "+" : ""}
                        {c.ev.toFixed(2)}R
                      </strong>
                    </span>
                  </div>
                </div>
              </div>

              {/* Center: 6-Gate Checklist Pills */}
              <div className="flex flex-wrap items-center gap-1.5">
                <GateCheckPill label="1. Regime" ok={c.regime_ok} />
                <GateCheckPill label="2. Score/EV" ok={c.score_ok} />
                <GateCheckPill label="3. Book/OBI" ok={c.book_quality_ok} />
                <GateCheckPill label="4. RVOL" ok={c.rvol_ok} />
                <GateCheckPill
                  label={
                    c.pattern_matched === "VWAP_PULLBACK_RECLAIM"
                      ? "5. VWAP Pullback"
                      : c.pattern_matched === "FAILED_BREAKDOWN_SPRING"
                      ? "5. Bear Trap Spring"
                      : c.pattern_matched === "ORB_RETEST_RECLAIM"
                      ? "5. ORB Retest"
                      : c.pattern_matched === "ORB_15M_BREAKOUT"
                      ? "5. 15m ORB"
                      : "5. Setup (VWAP/Spring/ORB)"
                  }
                  ok={Boolean(c.pattern_matched)}
                />
                <GateCheckPill label="6. Size & Tax Edge" ok={c.sizing_approved} />
              </div>

              {/* Right: Verdict & Blocking Reason */}
              <div className="flex items-center gap-2 lg:justify-end shrink-0">
                <span
                  className={`text-[10px] font-black px-2.5 py-1 rounded-full border ${
                    isReady
                      ? "bg-emerald-600 text-white border-emerald-700"
                      : isActive
                      ? "bg-indigo-50 text-indigo-800 border-indigo-200"
                      : "bg-amber-50 text-amber-900 border-amber-200"
                  }`}
                >
                  {isReady ? "QUALIFIED" : isActive ? "IN POSITION / COOLDOWN" : "HOLDING"}
                </span>
                <span className="text-[11px] text-slate-600 max-w-xs truncate" title={c.blocking_reason}>
                  {c.blocking_reason}
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
});
