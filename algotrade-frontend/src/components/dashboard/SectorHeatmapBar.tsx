"use client";

import React from "react";
import { SectorRankData } from "@/lib/types";

interface SectorHeatmapBarProps {
  sectors: SectorRankData[];
  selectedSectorId?: number | null;
  onSelectSector?: (sectorId: number | null) => void;
}

export function SectorHeatmapBar({ sectors, selectedSectorId, onSelectSector }: SectorHeatmapBarProps) {
  if (!sectors || sectors.length === 0) return null;

  return (
    <div className="bg-white border border-slate-200/90 rounded-3xl p-4 sm:p-5 shadow-xs">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span className="text-xs font-mono font-bold text-slate-900 uppercase tracking-wider">
            Sector Rotation
          </span>
          <span className="text-[11px] font-mono text-slate-400">
            (Ranked by Intraday Strength)
          </span>
        </div>

        {selectedSectorId && onSelectSector && (
          <button
            onClick={() => onSelectSector(null)}
            className="text-[11px] font-mono text-slate-500 hover:text-slate-800 underline transition-colors cursor-pointer"
          >
            Clear Filter
          </button>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2 sm:gap-2.5">
        {sectors.map((sec, idx) => {
          const isSelected = selectedSectorId === sec.sector_id;
          const isUp = sec.change_percent >= 0;

          return (
            <button
              key={sec.sector_id}
              onClick={() => onSelectSector && onSelectSector(isSelected ? null : sec.sector_id)}
              className={`flex items-center gap-2 px-3 py-1.5 sm:py-2 rounded-2xl border text-xs font-mono font-semibold transition-all cursor-pointer active:scale-95 ${
                isSelected
                  ? "bg-slate-900 text-white border-slate-900 shadow-xs"
                  : sec.is_leader
                  ? "bg-emerald-50/80 text-emerald-900 border-emerald-300 hover:bg-emerald-100/80"
                  : sec.is_lagging
                  ? "bg-rose-50/80 text-rose-900 border-rose-300 hover:bg-rose-100/80"
                  : "bg-slate-50/80 text-slate-700 border-slate-200 hover:bg-slate-100"
              }`}
            >
              <div className="flex items-center gap-1.5">
                <span className={`text-[10px] font-black px-1.5 py-0.5 rounded ${
                  isSelected ? "bg-slate-800 text-slate-300" : "bg-white/80 text-slate-600 border border-slate-200/60"
                }`}>
                  #{idx + 1}
                </span>
                <span className="font-bold">{sec.sector_name.replace("NIFTY ", "")}</span>
                {sec.is_restricted && (
                  <span
                    className="text-[9px] uppercase px-1.5 py-0.5 rounded bg-rose-100 text-rose-800 border border-rose-200 font-bold"
                    title={sec.restriction_reason || "Trading restricted in this sector"}
                  >
                    RESTRICTED
                  </span>
                )}
              </div>

              <div className="flex items-center gap-1.5">
                <span className={`font-mono font-bold ${
                  isSelected ? "text-white" : isUp ? "text-emerald-700" : "text-rose-700"
                }`}>
                  {isUp ? "+" : ""}{sec.change_percent.toFixed(2)}%
                </span>
                <span className={`text-[10px] ${isSelected ? "text-slate-300" : "text-slate-400"}`}>
                  ({sec.breadth_above_vwap.toFixed(0)}% &gt; VWAP)
                </span>
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
