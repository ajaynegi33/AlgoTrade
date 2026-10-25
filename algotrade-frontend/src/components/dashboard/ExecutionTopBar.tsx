"use client";

import React, { useState, memo } from "react";
import { ExecutionStateData } from "@/lib/types";
import { PaperFundsModal } from "./PaperFundsModal";
import { toast } from "sonner";

interface ExecutionTopBarProps {
  executionState?: ExecutionStateData | null;
  isUpdating: boolean;
  onUpdateSettings: (payload: {
    mode?: "PAPER" | "LIVE";
    auto_execute?: boolean;
    paper_capital?: number;
    risk_per_trade_pct?: number;
    max_open_positions?: number;
  }) => Promise<ExecutionStateData>;
  onSavePaperCapital: (capital: number) => Promise<ExecutionStateData>;
  onFlattenAll: () => Promise<number>;
}

export const ExecutionTopBar = memo(function ExecutionTopBar({
  executionState,
  isUpdating,
  onUpdateSettings,
  onSavePaperCapital,
  onFlattenAll,
}: ExecutionTopBarProps) {
  const [isPaperModalOpen, setIsPaperModalOpen] = useState(false);
  const [confirmLiveSwitch, setConfirmLiveSwitch] = useState(false);

  if (!executionState) {
    return null;
  }

  const { mode, auto_execute, gate_status, gate_reason, capital, daily_risk, active_positions } =
    executionState;

  const handleModeSwitch = async (nextMode: "PAPER" | "LIVE") => {
    if (nextMode === mode || isUpdating) return;
    if (nextMode === "LIVE" && !confirmLiveSwitch) {
      setConfirmLiveSwitch(true);
      return;
    }
    setConfirmLiveSwitch(false);
    try {
      await onUpdateSettings({ mode: nextMode });
      toast.success(`Switched execution mode to ${nextMode}`);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to switch execution mode";
      toast.error(msg);
    }
  };

  const handleToggleAutoExecute = async () => {
    if (isUpdating) return;
    const nextState = !auto_execute;
    try {
      await onUpdateSettings({ auto_execute: nextState });
      toast.success(
        nextState
          ? "Autonomous Execution ARMED — monitoring top candidates"
          : "Autonomous Execution PAUSED — active positions still managed"
      );
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to toggle autonomous execution";
      toast.error(msg);
    }
  };

  const handleEmergencyFlattenAll = async () => {
    if (isUpdating || !active_positions || active_positions.length === 0) return;
    try {
      const count = await onFlattenAll();
      toast.success(`Emergency flatten dispatched for ${count} open position(s)`);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to flatten positions";
      toast.error(msg);
    }
  };

  const gateBadgeColor = (() => {
    if (gate_status.includes("ARMED") || gate_status === "READY") {
      return "bg-emerald-50 text-emerald-800 border-emerald-300";
    }
    if (gate_status.includes("SELECTIVE")) {
      return "bg-indigo-50 text-indigo-800 border-indigo-300";
    }
    if (gate_status.includes("CIRCUIT_BREAKER")) {
      return "bg-rose-600 text-white border-rose-700";
    }
    if (gate_status.includes("LOCKED")) {
      return "bg-rose-50 text-rose-800 border-rose-300";
    }
    return "bg-amber-50 text-amber-800 border-amber-300";
  })();

  const winRatePct = daily_risk.win_rate_pct || 0;

  return (
    <>
      <div className="bg-white border border-slate-200/90 rounded-3xl p-5 sm:p-6 shadow-xs font-mono space-y-5">
        {/* Row 1: Mode Switcher, Auto-Execute Arm Toggle, Gate Status, and Emergency Flatten All */}
        <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4 pb-4 border-b border-slate-100">
          <div className="flex flex-wrap items-center gap-3">
            {/* PAPER / LIVE Segmented Switcher */}
            <div className="inline-flex rounded-2xl bg-slate-100 p-1 border border-slate-200">
              <button
                type="button"
                disabled={isUpdating}
                onClick={() => handleModeSwitch("PAPER")}
                className={`px-3.5 py-1.5 rounded-xl text-xs font-black transition-all cursor-pointer ${
                  mode === "PAPER"
                    ? "bg-slate-900 text-white shadow-2xs"
                    : "text-slate-600 hover:text-slate-900"
                }`}
              >
                PAPER SIM
              </button>
              <button
                type="button"
                disabled={isUpdating}
                onClick={() => handleModeSwitch("LIVE")}
                className={`px-3.5 py-1.5 rounded-xl text-xs font-black transition-all cursor-pointer ${
                  mode === "LIVE"
                    ? "bg-emerald-600 text-white shadow-2xs"
                    : "text-slate-600 hover:text-slate-900"
                }`}
              >
                LIVE ZERODHA
              </button>
            </div>

            {/* Autonomous Execution Arm / Pause Button */}
            <button
              type="button"
              disabled={isUpdating}
              onClick={handleToggleAutoExecute}
              className={`inline-flex items-center gap-2 px-4 py-2 rounded-2xl text-xs font-black border transition-all cursor-pointer ${
                auto_execute
                  ? "bg-emerald-600 text-white border-emerald-700 shadow-xs"
                  : "bg-slate-50 text-slate-700 border-slate-300 hover:bg-slate-100"
              }`}
            >
              <span
                className={`w-2 h-2 rounded-full ${
                  auto_execute ? "bg-white animate-ping" : "bg-slate-400"
                }`}
              />
              <span>{auto_execute ? "AUTO-EXEC: ARMED" : "AUTO-EXEC: PAUSED"}</span>
            </button>

            {/* Gate Status Pill */}
            <span
              className={`text-[11px] font-bold px-3 py-1 rounded-full border ${gateBadgeColor}`}
            >
              GATE: {gate_status}
            </span>

            {/* 2-Step Safety Confirmation before switching to Real-Money LIVE Zerodha Mode */}
            {confirmLiveSwitch && mode === "PAPER" && (
              <div className="inline-flex items-center gap-2 px-3 py-1.5 rounded-2xl bg-amber-50 border border-amber-300 text-amber-900 text-xs">
                <span className="font-bold">Confirm Live Zerodha Capital?</span>
                <button
                  type="button"
                  onClick={() => handleModeSwitch("LIVE")}
                  className="px-2.5 py-0.5 rounded-lg bg-emerald-600 hover:bg-emerald-700 text-white font-black cursor-pointer"
                >
                  YES, GO LIVE
                </button>
                <button
                  type="button"
                  onClick={() => setConfirmLiveSwitch(false)}
                  className="px-2 py-0.5 rounded-lg bg-slate-200 hover:bg-slate-300 text-slate-700 font-bold cursor-pointer"
                >
                  Cancel
                </button>
              </div>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-2.5">
            {mode === "PAPER" && (
              <button
                type="button"
                onClick={() => setIsPaperModalOpen(true)}
                className="px-3.5 py-2 rounded-xl border border-slate-200 bg-slate-50 hover:bg-slate-100 text-xs font-bold text-slate-800 transition-colors cursor-pointer"
              >
                Configure Paper Funds
              </button>
            )}

            <button
              type="button"
              disabled={isUpdating || !active_positions || active_positions.length === 0}
              onClick={handleEmergencyFlattenAll}
              className="px-4 py-2 rounded-xl bg-rose-600 hover:bg-rose-700 disabled:bg-slate-100 disabled:text-slate-400 disabled:border-slate-200 text-white text-xs font-black tracking-wide transition-all cursor-pointer shadow-xs"
            >
              EMERGENCY FLATTEN ALL ({active_positions?.length || 0})
            </button>
          </div>
        </div>

        {/* Row 2: Live Gate Diagnostic Reason Banner */}
        <div className="rounded-2xl bg-slate-50 border border-slate-200/80 px-4 py-2.5 flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-xs">
          <div className="flex items-center gap-2 text-slate-700">
            <span className="font-bold uppercase text-[10px] text-slate-400">
              Execution Gate Telemetry:
            </span>
            <span className="font-semibold text-slate-900">{gate_reason}</span>
          </div>
          <div className="text-[11px] text-slate-500 shrink-0">
            Circuit Breaker:{" "}
            <strong
              className={
                !daily_risk.circuit_breaker_locked ? "text-emerald-700" : "text-rose-700"
              }
            >
              {daily_risk.circuit_breaker_locked
                ? daily_risk.lock_reason || "LOCKED"
                : "OK"}
            </strong>{" "}
            ({daily_risk.consecutive_losses} consec losses)
          </div>
        </div>

        {/* Row 3: 6-Metric Capital, Margin & Risk Budget Strip */}
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 uppercase font-bold block">
              Net Equity ({capital.mode})
            </span>
            <span className="text-sm font-black text-slate-900 mt-1 block">
              ₹{capital.net_equity.toLocaleString("en-IN", { maximumFractionDigits: 0 })}
            </span>
          </div>

          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 uppercase font-bold block">
              Free Margin
            </span>
            <span className="text-sm font-black text-slate-900 mt-1 block">
              ₹{capital.free_margin.toLocaleString("en-IN", { maximumFractionDigits: 0 })}
            </span>
          </div>

          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 uppercase font-bold block">
              Blocked Margin
            </span>
            <span className="text-sm font-black text-slate-900 mt-1 block">
              ₹{capital.blocked_margin.toLocaleString("en-IN", { maximumFractionDigits: 0 })}
            </span>
          </div>

          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 uppercase font-bold block">
              Risk / Trade ({(capital.risk_per_trade_pct * 100).toFixed(2)}%)
            </span>
            <span className="text-sm font-black text-emerald-700 mt-1 block">
              ₹{capital.current_risk_budget.toLocaleString("en-IN", { maximumFractionDigits: 0 })}
            </span>
          </div>

          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 uppercase font-bold block">
              Realised Net PnL
            </span>
            <span
              className={`text-sm font-black mt-1 block ${
                capital.realised_net_pnl >= 0 ? "text-emerald-700" : "text-rose-700"
              }`}
            >
              {capital.realised_net_pnl >= 0 ? "+" : ""}₹
              {capital.realised_net_pnl.toLocaleString("en-IN", {
                minimumFractionDigits: 2,
                maximumFractionDigits: 2,
              })}
            </span>
          </div>

          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 uppercase font-bold block">
              Session Win Rate
            </span>
            <span className="text-sm font-black text-slate-900 mt-1 block">
              {winRatePct.toFixed(0)}% ({daily_risk.wins}W / {daily_risk.losses}L)
            </span>
          </div>
        </div>
      </div>

      <PaperFundsModal
        isOpen={isPaperModalOpen}
        currentCapital={capital.paper_capital}
        onClose={() => setIsPaperModalOpen(false)}
        onSave={async (newCap) => {
          await onSavePaperCapital(newCap);
        }}
      />
    </>
  );
});
