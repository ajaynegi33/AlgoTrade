"use client";

import React, { useState, useEffect, useMemo, useCallback } from "react";
import Link from "next/link";
import { getTradeHistory } from "@/lib/api";
import {
  TradeHistoryItemData,
  TradeHistoryResponseData,
} from "@/lib/types";
import { useTradingStore } from "@/store/useTradingStore";
import { toast } from "sonner";

type ColumnId =
  | "company_name"
  | "sector"
  | "trading_date"
  | "opened_at"
  | "closed_at"
  | "hold_duration"
  | "pattern_type"
  | "quantity"
  | "buy_price"
  | "sell_price"
  | "total_invested"
  | "margin_blocked"
  | "total_sell_value"
  | "gross_pnl"
  | "total_taxes"
  | "tax_brokerage_stt"
  | "tax_exchange_gst_stamp"
  | "net_pnl"
  | "net_return_pct"
  | "r_metrics"
  | "stops_hwm"
  | "sl_gear_mods"
  | "exit_reason";

interface ColumnDef {
  id: ColumnId;
  label: string;
  shortLabel: string;
  group: "core" | "tax" | "engine";
  align?: "left" | "right" | "center";
}

const ALL_COLUMNS: ColumnDef[] = [
  { id: "company_name", label: "Company Name", shortLabel: "Company Name", group: "core", align: "left" },
  { id: "sector", label: "Sector", shortLabel: "Sector", group: "core", align: "left" },
  { id: "trading_date", label: "Date & Mode", shortLabel: "Date/Mode", group: "core", align: "left" },
  { id: "opened_at", label: "Purchase Time", shortLabel: "Buy Time", group: "core", align: "left" },
  { id: "closed_at", label: "Sell Time", shortLabel: "Sell Time", group: "core", align: "left" },
  { id: "hold_duration", label: "Hold Duration", shortLabel: "Duration", group: "core", align: "right" },
  { id: "pattern_type", label: "Strategy Pattern", shortLabel: "Pattern", group: "engine", align: "left" },
  { id: "quantity", label: "Quantity (Shares)", shortLabel: "Quantity", group: "core", align: "right" },
  { id: "buy_price", label: "Buy Price / Share", shortLabel: "Buy Price", group: "core", align: "right" },
  { id: "sell_price", label: "Sell Price / Share", shortLabel: "Sell Price", group: "core", align: "right" },
  { id: "total_invested", label: "Total Invested (Buy Value)", shortLabel: "Total Invested", group: "core", align: "right" },
  { id: "margin_blocked", label: "5x MIS Margin Blocked", shortLabel: "5x Margin", group: "core", align: "right" },
  { id: "total_sell_value", label: "Total Sell Value", shortLabel: "Sell Value", group: "core", align: "right" },
  { id: "gross_pnl", label: "Total Profit (Gross PnL)", shortLabel: "Gross Profit", group: "core", align: "right" },
  { id: "total_taxes", label: "Total Tax Paid", shortLabel: "Total Tax", group: "tax", align: "right" },
  { id: "tax_brokerage_stt", label: "Brokerage + STT", shortLabel: "Brok + STT", group: "tax", align: "right" },
  { id: "tax_exchange_gst_stamp", label: "NSE + Stamp + GST + SEBI", shortLabel: "NSE/GST/Stamp", group: "tax", align: "right" },
  { id: "net_pnl", label: "Profit After Tax (Net PnL)", shortLabel: "Profit After Tax", group: "core", align: "right" },
  { id: "net_return_pct", label: "Net ROI % (Invested / Margin)", shortLabel: "Net ROI %", group: "core", align: "right" },
  { id: "r_metrics", label: "Realised R / Peak MFE / Capture", shortLabel: "R-Multiple & MFE", group: "engine", align: "right" },
  { id: "stops_hwm", label: "Initial SL / Final SL / HWM", shortLabel: "SL & HWM Levels", group: "engine", align: "right" },
  { id: "sl_gear_mods", label: "Final Gear & SL Mods (25 Cap)", shortLabel: "Gear & SL Mods", group: "engine", align: "center" },
  { id: "exit_reason", label: "Exit Reason & +1R Scale-Out", shortLabel: "Exit Reason", group: "engine", align: "left" },
];

const PRESET_RECOMMENDED: ColumnId[] = [
  "company_name",
  "opened_at",
  "closed_at",
  "hold_duration",
  "quantity",
  "buy_price",
  "sell_price",
  "total_invested",
  "gross_pnl",
  "total_taxes",
  "net_pnl",
  "net_return_pct",
  "r_metrics",
  "exit_reason",
];

const PRESET_TAX_AUDIT: ColumnId[] = [
  "company_name",
  "trading_date",
  "quantity",
  "buy_price",
  "sell_price",
  "total_invested",
  "margin_blocked",
  "total_sell_value",
  "gross_pnl",
  "tax_brokerage_stt",
  "tax_exchange_gst_stamp",
  "total_taxes",
  "net_pnl",
  "net_return_pct",
];

const PRESET_ENGINE_TELEMETRY: ColumnId[] = [
  "company_name",
  "pattern_type",
  "opened_at",
  "closed_at",
  "hold_duration",
  "buy_price",
  "sell_price",
  "stops_hwm",
  "r_metrics",
  "sl_gear_mods",
  "exit_reason",
  "net_pnl",
];

type SortKey =
  | "purchase_time"
  | "total_invested"
  | "quantity"
  | "gross_pnl"
  | "total_taxes"
  | "net_pnl"
  | "net_return_pct"
  | "realised_r_multiple"
  | "hold_duration_sec";

function formatINR(val: number, decimals = 2): string {
  const abs = Math.abs(val);
  const formatted = abs.toLocaleString("en-IN", {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  });
  if (val < 0) return `-₹${formatted}`;
  return `₹${formatted}`;
}

function formatSignedINR(val: number, decimals = 2): string {
  const abs = Math.abs(val);
  const formatted = abs.toLocaleString("en-IN", {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  });
  if (val > 0) return `+₹${formatted}`;
  if (val < 0) return `-₹${formatted}`;
  return `₹${formatted}`;
}

function formatTimestampShort(raw: string, fallbackDate = ""): { datePart: string; timePart: string } {
  if (!raw) return { datePart: fallbackDate, timePart: "—" };
  const cleaned = raw.replace("T", " ").replace("Z", "");
  const parts = cleaned.split(" ");
  if (parts.length >= 2) {
    return {
      datePart: parts[0],
      timePart: parts[1].slice(0, 8),
    };
  }
  return { datePart: fallbackDate, timePart: raw.slice(0, 8) };
}

export default function TradeHistoryAndTaxPage() {
  const liveSnapshot = useTradingStore((s) => s.liveSnapshot);

  const [historyData, setHistoryData] = useState<TradeHistoryResponseData | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [isRefreshing, setIsRefreshing] = useState<boolean>(false);

  // Filter States
  const [modeFilter, setModeFilter] = useState<"ALL" | "PAPER" | "LIVE">("ALL");
  const [outcomeFilter, setOutcomeFilter] = useState<"ALL" | "WINNERS" | "LOSERS" | "OPEN">("ALL");
  const [patternFilter, setPatternFilter] = useState<string>("ALL");
  const [datePreset, setDatePreset] = useState<"ALL" | "TODAY" | "7D" | "30D" | "CUSTOM">("ALL");
  const [fromDate, setFromDate] = useState<string>("");
  const [toDate, setToDate] = useState<string>("");
  const [searchQuery, setSearchQuery] = useState<string>("");

  // Column Visibility Filter State
  const [visibleColumns, setVisibleColumns] = useState<ColumnId[]>(PRESET_RECOMMENDED);
  const [showColumnPicker, setShowColumnPicker] = useState<boolean>(true);
  const [showEngineDiagnostics, setShowEngineDiagnostics] = useState<boolean>(true);

  // Sorting & Expandable Row State
  const [sortKey, setSortKey] = useState<SortKey>("purchase_time");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");
  const [expandedPositionId, setExpandedPositionId] = useState<string | null>(null);

  const loadHistory = useCallback(
    async (silent = false) => {
      if (!silent) setIsLoading(true);
      else setIsRefreshing(true);

      try {
        const res = await getTradeHistory({
          mode: modeFilter,
          from: fromDate || undefined,
          to: toDate || undefined,
          limit: 500,
        });
        setHistoryData(res);
        useTradingStore.getState().setTradeSummary(res.summary);
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : "Failed to load trade history";
        if (!silent) toast.error(msg);
      } finally {
        setIsLoading(false);
        setIsRefreshing(false);
      }
    },
    [modeFilter, fromDate, toDate]
  );

  useEffect(() => {
    loadHistory(false);
  }, [loadHistory]);

  // Auto-refresh when active/closed positions count changes in the live WebSocket stream
  const liveActiveCount = liveSnapshot?.execution?.active_positions?.length ?? 0;
  const liveClosedCount = liveSnapshot?.execution?.closed_positions?.length ?? 0;
  useEffect(() => {
    if (liveActiveCount > 0 || liveClosedCount > 0) {
      loadHistory(true);
    }
  }, [liveActiveCount, liveClosedCount, loadHistory]);

  // Apply Date Preset inside event handler (never calling Date.now() during render)
  const handleSelectDatePreset = (preset: "ALL" | "TODAY" | "7D" | "30D") => {
    setDatePreset(preset);
    if (preset === "ALL") {
      setFromDate("");
      setToDate("");
      return;
    }
    const now = new Date();
    const istOffsetMs = 5.5 * 60 * 60 * 1000;
    const istNow = new Date(now.getTime() + now.getTimezoneOffset() * 60 * 1000 + istOffsetMs);
    const toStr = istNow.toISOString().slice(0, 10);

    if (preset === "TODAY") {
      setFromDate(toStr);
      setToDate(toStr);
    } else if (preset === "7D") {
      const from = new Date(istNow.getTime() - 7 * 24 * 60 * 60 * 1000);
      setFromDate(from.toISOString().slice(0, 10));
      setToDate(toStr);
    } else if (preset === "30D") {
      const from = new Date(istNow.getTime() - 30 * 24 * 60 * 60 * 1000);
      setFromDate(from.toISOString().slice(0, 10));
      setToDate(toStr);
    }
  };

  const toggleColumn = (colId: ColumnId) => {
    setVisibleColumns((prev) =>
      prev.includes(colId) ? prev.filter((c) => c !== colId) : [...prev, colId]
    );
  };

  const isColVisible = useCallback(
    (colId: ColumnId) => visibleColumns.includes(colId),
    [visibleColumns]
  );

  // Filter & Sort Trades on Client
  const filteredTrades = useMemo(() => {
    const raw = historyData?.trades ?? [];
    const q = searchQuery.trim().toLowerCase();

    const filtered = raw.filter((t) => {
      if (outcomeFilter === "WINNERS" && !(t.state === "CLOSED" && t.net_pnl_after_tax > 0)) {
        return false;
      }
      if (outcomeFilter === "LOSERS" && !(t.state === "CLOSED" && t.net_pnl_after_tax <= 0)) {
        return false;
      }
      if (outcomeFilter === "OPEN" && t.state === "CLOSED") {
        return false;
      }
      if (patternFilter !== "ALL" && !t.pattern_type.includes(patternFilter)) {
        return false;
      }
      if (q) {
        const matchSymbol = t.symbol.toLowerCase().includes(q);
        const matchCompany = (t.company_name || "").toLowerCase().includes(q);
        const matchSector = (t.sector || "").toLowerCase().includes(q);
        const matchReason = (t.exit_reason || "").toLowerCase().includes(q);
        const matchId = t.position_id.toLowerCase().includes(q);
        if (!matchSymbol && !matchCompany && !matchSector && !matchReason && !matchId) {
          return false;
        }
      }
      return true;
    });

    return [...filtered].sort((a, b) => {
      const dir = sortDir === "asc" ? 1 : -1;
      switch (sortKey) {
        case "total_invested":
          return (a.total_invested - b.total_invested) * dir;
        case "quantity":
          return (a.quantity - b.quantity) * dir;
        case "gross_pnl":
          return (a.gross_pnl - b.gross_pnl) * dir;
        case "total_taxes":
          return (a.taxes.total_charges - b.taxes.total_charges) * dir;
        case "net_pnl":
          return (a.net_pnl_after_tax - b.net_pnl_after_tax) * dir;
        case "net_return_pct":
          return (a.return_on_invested_pct - b.return_on_invested_pct) * dir;
        case "realised_r_multiple":
          return (a.realised_r_multiple - b.realised_r_multiple) * dir;
        case "hold_duration_sec":
          return (a.hold_duration_sec - b.hold_duration_sec) * dir;
        case "purchase_time":
        default: {
          const ka = `${a.trading_date} ${a.purchase_time}`;
          const kb = `${b.trading_date} ${b.purchase_time}`;
          return ka.localeCompare(kb) * dir;
        }
      }
    });
  }, [historyData, outcomeFilter, patternFilter, searchQuery, sortKey, sortDir]);

  // Dynamic Aggregated Metrics for the Filtered View
  const filteredMetrics = useMemo(() => {
    let totalInvested = 0;
    let totalMargin = 0;
    let totalSellValue = 0;
    let totalGrossPnl = 0;
    let totalTaxes = 0;
    let totalNetPnl = 0;
    let wins = 0;
    let losses = 0;
    let closedCount = 0;
    let openCount = 0;
    let sumR = 0;
    let sumMfeR = 0;
    let sumRCapture = 0;
    let rCaptureCount = 0;
    let grossProfit = 0;
    let grossLossAbs = 0;
    let taxBrokerage = 0;
    let taxStt = 0;
    let taxExchange = 0;
    let taxSebi = 0;
    let taxStamp = 0;
    let taxGst = 0;

    for (const t of filteredTrades) {
      totalInvested += t.total_invested;
      totalMargin += t.margin_blocked;
      totalSellValue += t.total_sell_value;
      totalGrossPnl += t.gross_pnl;
      totalTaxes += t.taxes.total_charges;
      totalNetPnl += t.net_pnl_after_tax;
      taxBrokerage += t.taxes.brokerage;
      taxStt += t.taxes.stt;
      taxExchange += t.taxes.exchange_txn;
      taxSebi += t.taxes.sebi_charges;
      taxStamp += t.taxes.stamp_duty;
      taxGst += t.taxes.gst;

      if (t.state === "CLOSED") {
        closedCount++;
        sumR += t.realised_r_multiple;
        sumMfeR += t.peak_mfe_r;
        if (t.peak_mfe_r >= 0.2) {
          sumRCapture += t.r_capture_efficiency_pct;
          rCaptureCount++;
        }
        if (t.net_pnl_after_tax > 0) {
          wins++;
          grossProfit += t.gross_pnl;
        } else {
          losses++;
          if (t.gross_pnl < 0) grossLossAbs += Math.abs(t.gross_pnl);
        }
      } else {
        openCount++;
      }
    }

    const winRatePct = closedCount > 0 ? (wins / closedCount) * 100 : 0;
    const avgR = closedCount > 0 ? sumR / closedCount : 0;
    const avgMfeR = closedCount > 0 ? sumMfeR / closedCount : 0;
    const avgRCapture = rCaptureCount > 0 ? sumRCapture / rCaptureCount : 0;
    const profitFactor = grossLossAbs > 0 ? grossProfit / grossLossAbs : grossProfit > 0 ? 99.9 : 0;
    const taxDragPct = grossProfit > 0 ? (totalTaxes / grossProfit) * 100 : 0;
    const expectancy = closedCount > 0 ? totalNetPnl / closedCount : 0;

    return {
      totalCount: filteredTrades.length,
      closedCount,
      openCount,
      wins,
      losses,
      winRatePct,
      totalInvested,
      totalMargin,
      totalSellValue,
      totalTurnover: totalInvested + totalSellValue,
      totalGrossPnl,
      totalTaxes,
      totalNetPnl,
      grossProfit,
      grossLossAbs,
      profitFactor,
      taxDragPct,
      expectancy,
      avgR,
      avgMfeR,
      avgRCapture,
      taxBrokerage,
      taxStt,
      taxExchange,
      taxSebi,
      taxStamp,
      taxGst,
    };
  }, [filteredTrades]);

  const handleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((prev) => (prev === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  };

  // Export CSV Handler (All 34 Detailed Columns + Summary Row)
  const handleExportCSV = () => {
    if (filteredTrades.length === 0) {
      toast.info("No trades in current filter to export.");
      return;
    }

    const headers = [
      "Position ID",
      "Trading Date",
      "Mode",
      "State",
      "Symbol",
      "Company Name",
      "Sector",
      "Pattern",
      "Purchase Time",
      "Sell Time",
      "Hold Duration",
      "Hold Seconds",
      "Quantity",
      "Buy Price (INR)",
      "Sell Price (INR)",
      "Total Invested / Buy Value (INR)",
      "5x MIS Margin Blocked (INR)",
      "Total Sell Value (INR)",
      "Total Turnover (INR)",
      "Gross Profit / Loss (INR)",
      "Brokerage (INR)",
      "STT 0.025% Sell (INR)",
      "NSE Exchange Txn (INR)",
      "SEBI Turnover Fee (INR)",
      "Stamp Duty 0.003% Buy (INR)",
      "GST 18% (INR)",
      "Total Tax Paid (INR)",
      "Net Profit / Loss After Tax (INR)",
      "Net ROI % on Invested",
      "Net ROI % on 5x Margin",
      "Initial Stop (INR)",
      "Breakeven Stop (INR)",
      "Final Stop (INR)",
      "High Water Mark (INR)",
      "1R Risk Per Share (INR)",
      "Realised R-Multiple",
      "Peak MFE R-Multiple",
      "R-Capture Efficiency %",
      "SL Modifications Used",
      "Final Gear",
      "50% Banked at +1R",
      "Exit Reason",
    ];

    const escapeCsv = (val: string | number | boolean) => {
      const str = String(val ?? "");
      if (str.includes(",") || str.includes('"') || str.includes("\n")) {
        return `"${str.replace(/"/g, '""')}"`;
      }
      return str;
    };

    const rows = filteredTrades.map((t) => [
      t.position_id,
      t.trading_date,
      t.mode,
      t.state,
      t.symbol,
      t.company_name || t.symbol,
      t.sector,
      t.pattern_type,
      t.purchase_time,
      t.sell_time || "OPEN",
      t.hold_duration_label,
      t.hold_duration_sec,
      t.quantity,
      t.buy_price.toFixed(2),
      t.sell_price.toFixed(2),
      t.total_invested.toFixed(2),
      t.margin_blocked.toFixed(2),
      t.total_sell_value.toFixed(2),
      (t.total_invested + t.total_sell_value).toFixed(2),
      t.gross_pnl.toFixed(2),
      t.taxes.brokerage.toFixed(2),
      t.taxes.stt.toFixed(2),
      t.taxes.exchange_txn.toFixed(2),
      t.taxes.sebi_charges.toFixed(2),
      t.taxes.stamp_duty.toFixed(2),
      t.taxes.gst.toFixed(2),
      t.taxes.total_charges.toFixed(2),
      t.net_pnl_after_tax.toFixed(2),
      t.return_on_invested_pct.toFixed(4),
      t.return_on_margin_pct.toFixed(4),
      t.initial_stop.toFixed(2),
      t.breakeven_stop.toFixed(2),
      t.final_stop.toFixed(2),
      t.high_water_mark.toFixed(2),
      t.one_r.toFixed(2),
      t.realised_r_multiple.toFixed(2),
      t.peak_mfe_r.toFixed(2),
      t.r_capture_efficiency_pct.toFixed(1),
      t.mod_count,
      t.final_gear,
      t.tranche1_done ? "YES" : "NO",
      t.exit_reason,
    ]);

    // Append Summary Footer Row
    rows.push([
      "TOTAL_SUMMARY",
      "",
      modeFilter,
      `${filteredMetrics.closedCount} Closed / ${filteredMetrics.openCount} Open`,
      `${filteredMetrics.totalCount} Trades`,
      `Win Rate: ${filteredMetrics.winRatePct.toFixed(1)}%`,
      "",
      "",
      "",
      "",
      "",
      "",
      filteredTrades.reduce((acc, item) => acc + item.quantity, 0),
      "",
      "",
      filteredMetrics.totalInvested.toFixed(2),
      filteredMetrics.totalMargin.toFixed(2),
      filteredMetrics.totalSellValue.toFixed(2),
      filteredMetrics.totalTurnover.toFixed(2),
      filteredMetrics.totalGrossPnl.toFixed(2),
      filteredMetrics.taxBrokerage.toFixed(2),
      filteredMetrics.taxStt.toFixed(2),
      filteredMetrics.taxExchange.toFixed(2),
      filteredMetrics.taxSebi.toFixed(2),
      filteredMetrics.taxStamp.toFixed(2),
      filteredMetrics.taxGst.toFixed(2),
      filteredMetrics.totalTaxes.toFixed(2),
      filteredMetrics.totalNetPnl.toFixed(2),
      "",
      "",
      "",
      "",
      "",
      "",
      "",
      filteredMetrics.avgR.toFixed(2),
      filteredMetrics.avgMfeR.toFixed(2),
      filteredMetrics.avgRCapture.toFixed(1),
      "",
      "",
      "",
      "",
    ]);

    const csvContent =
      "\uFEFF" +
      [headers.map(escapeCsv).join(","), ...rows.map((r) => r.map(escapeCsv).join(","))].join("\n");

    const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    const dateStamp = new Date().toISOString().slice(0, 10);
    link.href = url;
    link.setAttribute("download", `algotrade_trade_tax_ledger_${dateStamp}.csv`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);

    toast.success(`Exported ${filteredTrades.length} trades with full tax audit to CSV`);
  };

  // Export JSON Handler
  const handleExportJSON = () => {
    if (filteredTrades.length === 0) {
      toast.info("No trades in current filter to export.");
      return;
    }
    const payload = {
      exported_at: new Date().toISOString(),
      filters: {
        mode: modeFilter,
        outcome: outcomeFilter,
        pattern: patternFilter,
        from_date: fromDate || "ALL",
        to_date: toDate || "ALL",
      },
      summary: filteredMetrics,
      trades: filteredTrades,
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    const dateStamp = new Date().toISOString().slice(0, 10);
    link.href = url;
    link.setAttribute("download", `algotrade_trade_tax_ledger_${dateStamp}.json`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);

    toast.success(`Exported ${filteredTrades.length} trades & engine telemetry to JSON`);
  };

  const summary = historyData?.summary;

  return (
    <div className="space-y-6 font-mono">
      {/* =========================================================================
          1. Executive PnL, Capital & Statutory Tax Summary (6 Institutional Cards)
          ========================================================================= */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
        {/* Card 1: Net Profit / Loss After All Taxes */}
        <div
          className={`rounded-3xl p-5 border shadow-xs flex flex-col justify-between ${
            filteredMetrics.totalNetPnl >= 0
              ? "bg-emerald-50/50 border-emerald-200/90"
              : "bg-rose-50/50 border-rose-200/90"
          }`}
        >
          <div>
            <span className="text-[10px] font-bold uppercase tracking-wider text-slate-500 block">
              Profit / Loss After Tax
            </span>
            <div
              className={`text-2xl font-black mt-2 tracking-tight ${
                filteredMetrics.totalNetPnl >= 0 ? "text-emerald-800" : "text-rose-800"
              }`}
            >
              {formatSignedINR(filteredMetrics.totalNetPnl)}
            </div>
          </div>
          <div className="mt-3 pt-2.5 border-t border-slate-200/60 text-[11px] text-slate-600 flex items-center justify-between">
            <span>Expectancy/Trade:</span>
            <strong
              className={
                filteredMetrics.expectancy >= 0 ? "text-emerald-800" : "text-rose-700"
              }
            >
              {formatSignedINR(filteredMetrics.expectancy)}
            </strong>
          </div>
        </div>

        {/* Card 2: Total Profit / Gross PnL (Before Tax) */}
        <div className="bg-white rounded-3xl p-5 border border-slate-200/90 shadow-xs flex flex-col justify-between">
          <div>
            <span className="text-[10px] font-bold uppercase tracking-wider text-slate-400 block">
              Gross Profit / Loss
            </span>
            <div
              className={`text-2xl font-black mt-2 tracking-tight ${
                filteredMetrics.totalGrossPnl >= 0 ? "text-slate-900" : "text-rose-700"
              }`}
            >
              {formatSignedINR(filteredMetrics.totalGrossPnl)}
            </div>
          </div>
          <div className="mt-3 pt-2.5 border-t border-slate-100 text-[11px] text-slate-500 flex items-center justify-between">
            <span>Won: +{formatINR(filteredMetrics.grossProfit, 0)}</span>
            <span className="text-rose-600">Lost: -{formatINR(filteredMetrics.grossLossAbs, 0)}</span>
          </div>
        </div>

        {/* Card 3: Total Statutory Taxes Paid */}
        <div className="bg-amber-50/40 rounded-3xl p-5 border border-amber-200/90 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[10px] font-bold uppercase tracking-wider text-amber-900/70">
                Total Tax &amp; Levies Paid
              </span>
              <span className="text-[9px] font-bold px-1.5 py-0.5 rounded bg-amber-100 text-amber-900">
                NSE MIS
              </span>
            </div>
            <div className="text-2xl font-black text-amber-950 mt-2 tracking-tight">
              {formatINR(filteredMetrics.totalTaxes)}
            </div>
          </div>
          <div className="mt-3 pt-2.5 border-t border-amber-200/60 text-[11px] text-amber-900/80 flex items-center justify-between">
            <span>Tax Drag on Wins:</span>
            <strong className="font-black">{filteredMetrics.taxDragPct.toFixed(1)}%</strong>
          </div>
        </div>

        {/* Card 4: Total Capital Invested & 5x Margin */}
        <div className="bg-white rounded-3xl p-5 border border-slate-200/90 shadow-xs flex flex-col justify-between">
          <div>
            <span className="text-[10px] font-bold uppercase tracking-wider text-slate-400 block">
              Total Invested (Buy Value)
            </span>
            <div className="text-xl font-black text-slate-900 mt-2 tracking-tight">
              {formatINR(filteredMetrics.totalInvested, 0)}
            </div>
          </div>
          <div className="mt-3 pt-2.5 border-t border-slate-100 text-[11px] text-slate-500 flex items-center justify-between">
            <span>5x Margin Used:</span>
            <strong className="text-slate-800">{formatINR(filteredMetrics.totalMargin, 0)}</strong>
          </div>
        </div>

        {/* Card 5: Win Rate & Profit Factor */}
        <div className="bg-white rounded-3xl p-5 border border-slate-200/90 shadow-xs flex flex-col justify-between">
          <div>
            <div className="flex items-center justify-between">
              <span className="text-[10px] font-bold uppercase tracking-wider text-slate-400">
                Win Rate ({filteredMetrics.closedCount} Closed)
              </span>
              {filteredMetrics.openCount > 0 && (
                <span className="text-[9px] font-bold px-1.5 py-0.5 rounded bg-emerald-100 text-emerald-800">
                  {filteredMetrics.openCount} OPEN
                </span>
              )}
            </div>
            <div className="text-2xl font-black text-slate-900 mt-2 tracking-tight">
              {filteredMetrics.winRatePct.toFixed(1)}%
            </div>
          </div>
          <div className="mt-3 pt-2.5 border-t border-slate-100 text-[11px] text-slate-500 flex items-center justify-between">
            <span>
              <strong className="text-emerald-700">{filteredMetrics.wins}W</strong> /{" "}
              <strong className="text-rose-700">{filteredMetrics.losses}L</strong>
            </span>
            <span>
              PF: <strong className="text-slate-900">{filteredMetrics.profitFactor.toFixed(2)}</strong>
            </span>
          </div>
        </div>

        {/* Card 6: Engine R-Multiple & MFE Capture Efficiency */}
        <div className="bg-white rounded-3xl p-5 border border-slate-200/90 shadow-xs flex flex-col justify-between">
          <div>
            <span className="text-[10px] font-bold uppercase tracking-wider text-slate-400 block">
              Avg Realised R / Peak MFE
            </span>
            <div className="text-xl font-black text-slate-900 mt-2 tracking-tight">
              <span
                className={
                  filteredMetrics.avgR >= 0 ? "text-emerald-700" : "text-rose-700"
                }
              >
                {filteredMetrics.avgR >= 0 ? "+" : ""}
                {filteredMetrics.avgR.toFixed(2)}R
              </span>{" "}
              <span className="text-xs font-normal text-slate-400">/</span>{" "}
              <span className="text-sm text-indigo-700">
                +{filteredMetrics.avgMfeR.toFixed(2)}R
              </span>
            </div>
          </div>
          <div className="mt-3 pt-2.5 border-t border-slate-100 text-[11px] text-slate-500 flex items-center justify-between">
            <span>SL R-Capture:</span>
            <strong className="text-indigo-700">{filteredMetrics.avgRCapture.toFixed(1)}%</strong>
          </div>
        </div>
      </div>

      {/* =========================================================================
          2. 6-Component Indian Statutory Tax Breakdown Strip
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs space-y-3">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
          <div>
            <div className="flex items-center gap-2">
              <h2 className="text-xs font-black text-slate-900 uppercase tracking-wider">
                Indian Equity Intraday (MIS) Statutory Tax &amp; Charge Audit
              </h2>
              <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-slate-100 text-slate-700">
                Turnover: {formatINR(filteredMetrics.totalTurnover, 0)}
              </span>
            </div>
            <p className="text-[11px] text-slate-500 mt-0.5">
              Exact Zerodha Kite NSE Cash Intraday contract-note formula across all {filteredMetrics.totalCount} filtered trades.
            </p>
          </div>

          <button
            type="button"
            onClick={() => setShowEngineDiagnostics((prev) => !prev)}
            className="text-xs font-bold text-emerald-700 hover:text-emerald-800 flex items-center gap-1 cursor-pointer self-start sm:self-auto"
          >
            <span>
              {showEngineDiagnostics
                ? "Hide Engine Diagnostics ▲"
                : "Show Engine & Pattern Diagnostics ▼"}
            </span>
          </button>
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-2.5 text-xs">
          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 block uppercase">
              1. Brokerage (0.03% / ₹20)
            </span>
            <strong className="text-sm font-black text-slate-900 mt-0.5 block">
              {formatINR(filteredMetrics.taxBrokerage)}
            </strong>
          </div>
          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 block uppercase">
              2. STT (0.025% Sell)
            </span>
            <strong className="text-sm font-black text-slate-900 mt-0.5 block">
              {formatINR(filteredMetrics.taxStt)}
            </strong>
          </div>
          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 block uppercase">
              3. NSE Exchange (0.00297%)
            </span>
            <strong className="text-sm font-black text-slate-900 mt-0.5 block">
              {formatINR(filteredMetrics.taxExchange)}
            </strong>
          </div>
          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 block uppercase">
              4. Stamp Duty (0.003% Buy)
            </span>
            <strong className="text-sm font-black text-slate-900 mt-0.5 block">
              {formatINR(filteredMetrics.taxStamp)}
            </strong>
          </div>
          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 block uppercase">
              5. SEBI Turnover (0.0001%)
            </span>
            <strong className="text-sm font-black text-slate-900 mt-0.5 block">
              {formatINR(filteredMetrics.taxSebi)}
            </strong>
          </div>
          <div className="p-3 rounded-2xl bg-slate-50/80 border border-slate-200/70">
            <span className="text-[10px] text-slate-400 block uppercase">
              6. GST (18% on Brok+NSE+SEBI)
            </span>
            <strong className="text-sm font-black text-slate-900 mt-0.5 block">
              {formatINR(filteredMetrics.taxGst)}
            </strong>
          </div>
        </div>

        {/* =====================================================================
            3. Collapsible Engine & Strategy Attribution Panels
            ===================================================================== */}
        {showEngineDiagnostics && summary && (
          <div className="pt-3 border-t border-slate-100 grid grid-cols-1 lg:grid-cols-3 gap-4">
            {/* Panel A: Pattern Performance Comparison */}
            <div className="rounded-2xl border border-slate-200/80 bg-slate-50/40 p-4 space-y-2.5">
              <div className="flex items-center justify-between">
                <span className="text-[11px] font-black text-slate-900 uppercase">
                  Strategy Pattern Attribution
                </span>
                <span className="text-[10px] text-slate-400">Pattern A vs Pattern B</span>
              </div>
              {summary.by_pattern.length === 0 ? (
                <p className="text-xs text-slate-400">No closed pattern data yet.</p>
              ) : (
                <div className="space-y-2">
                  {summary.by_pattern.map((p) => (
                    <div
                      key={p.pattern_type}
                      className="p-2.5 rounded-xl bg-white border border-slate-200/80 flex items-center justify-between text-xs"
                    >
                      <div>
                        <span className="font-black text-slate-900 block">
                          {p.pattern_type}
                        </span>
                        <span className="text-[10px] text-slate-500">
                          {p.trades} Trades • Win Rate {p.win_rate_pct.toFixed(0)}% • Avg{" "}
                          {p.avg_r_multiple >= 0 ? "+" : ""}
                          {p.avg_r_multiple.toFixed(2)}R
                        </span>
                      </div>
                      <div className="text-right">
                        <strong
                          className={`font-black block ${
                            p.net_pnl_after_tax >= 0 ? "text-emerald-700" : "text-rose-700"
                          }`}
                        >
                          {formatSignedINR(p.net_pnl_after_tax)}
                        </strong>
                        <span className="text-[10px] text-amber-800">
                          Tax: {formatINR(p.total_taxes)}
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Panel B: Exit Reason Distribution */}
            <div className="rounded-2xl border border-slate-200/80 bg-slate-50/40 p-4 space-y-2.5">
              <div className="flex items-center justify-between">
                <span className="text-[11px] font-black text-slate-900 uppercase">
                  Exit Reason Audit
                </span>
                <span className="text-[10px] text-slate-400">How Positions Closed</span>
              </div>
              {summary.by_exit_reason.length === 0 ? (
                <p className="text-xs text-slate-400">No exits recorded yet.</p>
              ) : (
                <div className="space-y-2">
                  {summary.by_exit_reason.map((er) => (
                    <div
                      key={er.exit_reason}
                      className="p-2.5 rounded-xl bg-white border border-slate-200/80 flex items-center justify-between text-xs"
                    >
                      <div>
                        <span className="font-bold text-slate-800">{er.exit_reason}</span>
                        <span className="ml-2 text-[10px] px-1.5 py-0.5 rounded bg-slate-100 text-slate-600 font-bold">
                          {er.count}x
                        </span>
                      </div>
                      <strong
                        className={`font-black ${
                          er.net_pnl_after_tax >= 0 ? "text-emerald-700" : "text-rose-700"
                        }`}
                      >
                        {formatSignedINR(er.net_pnl_after_tax)}
                      </strong>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Panel C: Extremes & Hold Telemetry */}
            <div className="rounded-2xl border border-slate-200/80 bg-slate-50/40 p-4 space-y-2.5 flex flex-col justify-between">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-[11px] font-black text-slate-900 uppercase">
                    Trade Extremes &amp; +1R Scale-Out
                  </span>
                  <span className="text-[10px] text-slate-400">Post-Tax</span>
                </div>

                <div className="p-2.5 rounded-xl bg-white border border-slate-200/80 flex items-center justify-between text-xs">
                  <div>
                    <span className="text-[10px] text-slate-400 block uppercase">
                      Largest Win (Net)
                    </span>
                    <strong className="text-slate-900">
                      Avg Win: {formatSignedINR(summary.avg_win_rupees)}
                    </strong>
                  </div>
                  <strong className="text-emerald-700 font-black">
                    {formatSignedINR(summary.max_win_rupees)}
                  </strong>
                </div>

                <div className="p-2.5 rounded-xl bg-white border border-slate-200/80 flex items-center justify-between text-xs">
                  <div>
                    <span className="text-[10px] text-slate-400 block uppercase">
                      Largest Loss (Net)
                    </span>
                    <strong className="text-slate-900">
                      Avg Loss: {formatSignedINR(summary.avg_loss_rupees)}
                    </strong>
                  </div>
                  <strong className="text-rose-700 font-black">
                    {formatSignedINR(summary.max_loss_rupees)}
                  </strong>
                </div>
              </div>

              <div className="pt-2 border-t border-slate-200/60 flex items-center justify-between text-[11px] text-slate-600">
                <span>+1R Scale-Out Hit Rate: {summary.tranche1_scale_out_rate.toFixed(1)}%</span>
                <span>Avg Hold: {Math.round(summary.avg_hold_duration_sec / 60)}m</span>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* =========================================================================
          4. Filter Bar, Column Selector Pills & Export Toolbar
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs space-y-4">
        {/* Top Row: Filters + Search + Export Buttons */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          {/* Left: Date Preset & Mode & Outcome Pills */}
          <div className="flex flex-wrap items-center gap-2">
            {/* Date Range Presets */}
            <div className="inline-flex items-center rounded-xl bg-slate-100 p-1 border border-slate-200/80">
              {(
                [
                  { id: "ALL", label: "All Dates" },
                  { id: "TODAY", label: "Today" },
                  { id: "7D", label: "7D" },
                  { id: "30D", label: "30D" },
                ] as const
              ).map((d) => (
                <button
                  key={d.id}
                  type="button"
                  onClick={() => handleSelectDatePreset(d.id)}
                  className={`px-2.5 py-1 rounded-lg text-[11px] font-bold transition-colors cursor-pointer ${
                    datePreset === d.id
                      ? "bg-slate-900 text-white"
                      : "text-slate-600 hover:text-slate-900"
                  }`}
                >
                  {d.label}
                </button>
              ))}
            </div>

            {/* Custom Date Inputs */}
            <div className="flex items-center gap-1.5 text-xs">
              <input
                type="date"
                value={fromDate}
                onChange={(e) => {
                  setDatePreset("CUSTOM");
                  setFromDate(e.target.value);
                }}
                aria-label="From Date"
                className="px-2.5 py-1.5 rounded-xl border border-slate-200 bg-slate-50 text-[11px] text-slate-800 focus:outline-none focus:border-emerald-500"
              />
              <span className="text-slate-400">to</span>
              <input
                type="date"
                value={toDate}
                onChange={(e) => {
                  setDatePreset("CUSTOM");
                  setToDate(e.target.value);
                }}
                aria-label="To Date"
                className="px-2.5 py-1.5 rounded-xl border border-slate-200 bg-slate-50 text-[11px] text-slate-800 focus:outline-none focus:border-emerald-500"
              />
            </div>

            {/* Mode Filter */}
            <div className="inline-flex items-center rounded-xl bg-slate-100 p-1 border border-slate-200/80">
              {(["ALL", "PAPER", "LIVE"] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => setModeFilter(m)}
                  className={`px-2.5 py-1 rounded-lg text-[11px] font-bold transition-colors cursor-pointer ${
                    modeFilter === m
                      ? "bg-slate-900 text-white"
                      : "text-slate-600 hover:text-slate-900"
                  }`}
                >
                  {m}
                </button>
              ))}
            </div>

            {/* Outcome Filter */}
            <div className="inline-flex items-center rounded-xl bg-slate-100 p-1 border border-slate-200/80">
              {(
                [
                  { id: "ALL", label: "All" },
                  { id: "WINNERS", label: "Winners" },
                  { id: "LOSERS", label: "Losers" },
                  { id: "OPEN", label: "Open" },
                ] as const
              ).map((o) => (
                <button
                  key={o.id}
                  type="button"
                  onClick={() => setOutcomeFilter(o.id)}
                  className={`px-2.5 py-1 rounded-lg text-[11px] font-bold transition-colors cursor-pointer ${
                    outcomeFilter === o.id
                      ? "bg-emerald-700 text-white"
                      : "text-slate-600 hover:text-slate-900"
                  }`}
                >
                  {o.label}
                </button>
              ))}
            </div>

            {/* Pattern Filter */}
            <select
              value={patternFilter}
              onChange={(e) => setPatternFilter(e.target.value)}
              aria-label="Filter by Pattern"
              className="px-3 py-1.5 rounded-xl border border-slate-200 bg-slate-50 text-[11px] font-bold text-slate-700 focus:outline-none focus:border-emerald-500 cursor-pointer"
            >
              <option value="ALL">All Patterns</option>
              <option value="VWAP_PULLBACK">Pattern A: VWAP Pullback</option>
              <option value="ORB_15M">Pattern B: 15m ORB</option>
            </select>
          </div>

          {/* Right: Search Input + Refresh + Export CSV & JSON */}
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative w-56 sm:w-64">
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search symbol, company, exit..."
                className="w-full pl-3 pr-3 py-1.5 rounded-xl border border-slate-200 bg-slate-50 focus:bg-white text-xs text-slate-900 placeholder:text-slate-400 focus:outline-none focus:border-emerald-500"
              />
            </div>

            <button
              type="button"
              onClick={() => loadHistory(true)}
              disabled={isRefreshing}
              className="px-3 py-1.5 rounded-xl border border-slate-200 bg-slate-50 hover:bg-slate-100 text-xs font-bold text-slate-700 cursor-pointer transition-colors"
            >
              {isRefreshing ? "Syncing..." : "Refresh"}
            </button>

            <button
              type="button"
              onClick={handleExportCSV}
              className="px-3.5 py-1.5 rounded-xl bg-emerald-600 hover:bg-emerald-700 text-white text-xs font-black cursor-pointer transition-colors flex items-center gap-1.5 shadow-2xs"
            >
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
              </svg>
              <span>Export CSV</span>
            </button>

            <button
              type="button"
              onClick={handleExportJSON}
              className="px-3 py-1.5 rounded-xl bg-slate-900 hover:bg-slate-800 text-white text-xs font-bold cursor-pointer transition-colors"
            >
              Export JSON
            </button>
          </div>
        </div>

        {/* Bottom Row: Interactive Column Filter (Presets + Clickable Column Pills) */}
        <div className="pt-3 border-t border-slate-100 space-y-2.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-[10px] font-black uppercase tracking-wider text-slate-400">
                Table Column View Presets:
              </span>
              <button
                type="button"
                onClick={() => setVisibleColumns(PRESET_RECOMMENDED)}
                className="px-2.5 py-1 rounded-lg text-[11px] font-bold bg-emerald-50 text-emerald-800 border border-emerald-200 hover:bg-emerald-100 cursor-pointer transition-colors"
              >
                Recommended ({PRESET_RECOMMENDED.length})
              </button>
              <button
                type="button"
                onClick={() => setVisibleColumns(PRESET_TAX_AUDIT)}
                className="px-2.5 py-1 rounded-lg text-[11px] font-bold bg-amber-50 text-amber-900 border border-amber-200 hover:bg-amber-100 cursor-pointer transition-colors"
              >
                Tax &amp; Contract Note Audit ({PRESET_TAX_AUDIT.length})
              </button>
              <button
                type="button"
                onClick={() => setVisibleColumns(PRESET_ENGINE_TELEMETRY)}
                className="px-2.5 py-1 rounded-lg text-[11px] font-bold bg-indigo-50 text-indigo-800 border border-indigo-200 hover:bg-indigo-100 cursor-pointer transition-colors"
              >
                Engine &amp; SL Telemetry ({PRESET_ENGINE_TELEMETRY.length})
              </button>
              <button
                type="button"
                onClick={() => setVisibleColumns(ALL_COLUMNS.map((c) => c.id))}
                className="px-2.5 py-1 rounded-lg text-[11px] font-bold bg-slate-100 text-slate-800 border border-slate-200 hover:bg-slate-200 cursor-pointer transition-colors"
              >
                Show All ({ALL_COLUMNS.length} Columns)
              </button>
            </div>

            <button
              type="button"
              onClick={() => setShowColumnPicker((prev) => !prev)}
              className="text-[11px] font-bold text-slate-500 hover:text-slate-900 cursor-pointer"
            >
              {showColumnPicker
                ? `Hide Column Toggles (${visibleColumns.length}/${ALL_COLUMNS.length} Active) ▲`
                : `Customize Columns (${visibleColumns.length}/${ALL_COLUMNS.length} Active) ▼`}
            </button>
          </div>

          {showColumnPicker && (
            <div className="flex flex-wrap items-center gap-1.5 pt-1">
              {ALL_COLUMNS.map((col) => {
                const active = visibleColumns.includes(col.id);
                return (
                  <button
                    key={col.id}
                    type="button"
                    onClick={() => toggleColumn(col.id)}
                    className={`px-2.5 py-1 rounded-lg text-[10px] font-bold border transition-all cursor-pointer flex items-center gap-1 ${
                      active
                        ? "bg-slate-900 text-white border-slate-900"
                        : "bg-slate-50 text-slate-500 border-slate-200 hover:text-slate-800 hover:bg-slate-100"
                    }`}
                  >
                    <span>{active ? "✓" : "+"}</span>
                    <span>{col.shortLabel}</span>
                  </button>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* =========================================================================
          5. Institutional Trade History & Tax Ledger Table (Sticky Header + Sticky Symbol)
          ========================================================================= */}
      <div className="bg-white border border-slate-200/90 rounded-3xl overflow-hidden shadow-xs">
        <div className="p-4 sm:p-5 border-b border-slate-100 flex flex-col sm:flex-row sm:items-center justify-between gap-2">
          <div>
            <h3 className="text-xs font-black text-slate-900 uppercase tracking-wider">
              Complete Trade &amp; Tax Audit Table ({filteredTrades.length} Trades)
            </h3>
            <p className="text-[11px] text-slate-500 mt-0.5">
              Click any trade row to expand its itemized Indian Contract-Note Tax Receipt and OMS Child Order Legs.
            </p>
          </div>
          <div className="text-[11px] text-slate-500">
            Showing <strong className="text-slate-900">{visibleColumns.length + 1}</strong> columns • Scroll horizontally for more
          </div>
        </div>

        {isLoading ? (
          <div className="p-12 text-center text-xs text-slate-500">
            Loading trade history and computing statutory tax breakdown...
          </div>
        ) : filteredTrades.length === 0 ? (
          <div className="p-12 text-center space-y-2">
            <p className="text-sm font-black text-slate-700">
              No Matching Trades Found
            </p>
            <p className="text-xs text-slate-400 max-w-md mx-auto">
              When the autonomous engine executes Paper or Live intraday trades, every position, child order leg, and 6-part statutory tax calculation will be recorded here automatically.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto max-h-[680px]">
            <table className="w-full text-left border-separate border-spacing-0 text-xs">
              <thead>
                <tr className="bg-slate-50 text-slate-500 text-[10px] font-black uppercase tracking-wider">
                  {/* Pinned First Column: Symbol */}
                  <th
                    scope="col"
                    className="sticky top-0 left-0 z-20 bg-slate-100 py-3.5 px-4 border-b border-r border-slate-200 whitespace-nowrap"
                  >
                    Symbol
                  </th>

                  {isColVisible("company_name") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 whitespace-nowrap"
                    >
                      Company Name
                    </th>
                  )}

                  {isColVisible("sector") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 whitespace-nowrap"
                    >
                      Sector
                    </th>
                  )}

                  {isColVisible("trading_date") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 whitespace-nowrap"
                    >
                      Date / Mode
                    </th>
                  )}

                  {isColVisible("opened_at") && (
                    <th
                      scope="col"
                      onClick={() => handleSort("purchase_time")}
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 whitespace-nowrap cursor-pointer hover:text-slate-900"
                    >
                      Purchase Time {sortKey === "purchase_time" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                    </th>
                  )}

                  {isColVisible("closed_at") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 whitespace-nowrap"
                    >
                      Sell Time
                    </th>
                  )}

                  {isColVisible("hold_duration") && (
                    <th
                      scope="col"
                      onClick={() => handleSort("hold_duration_sec")}
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap cursor-pointer hover:text-slate-900"
                    >
                      Duration {sortKey === "hold_duration_sec" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                    </th>
                  )}

                  {isColVisible("pattern_type") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 whitespace-nowrap"
                    >
                      Pattern
                    </th>
                  )}

                  {isColVisible("quantity") && (
                    <th
                      scope="col"
                      onClick={() => handleSort("quantity")}
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap cursor-pointer hover:text-slate-900"
                    >
                      Qty {sortKey === "quantity" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                    </th>
                  )}

                  {isColVisible("buy_price") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap"
                    >
                      Buy Price (₹)
                    </th>
                  )}

                  {isColVisible("sell_price") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap"
                    >
                      Sell Price (₹)
                    </th>
                  )}

                  {isColVisible("total_invested") && (
                    <th
                      scope="col"
                      onClick={() => handleSort("total_invested")}
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap cursor-pointer hover:text-slate-900"
                    >
                      Total Invested {sortKey === "total_invested" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                    </th>
                  )}

                  {isColVisible("margin_blocked") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap"
                    >
                      5x Margin Used
                    </th>
                  )}

                  {isColVisible("total_sell_value") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap"
                    >
                      Total Sell Value
                    </th>
                  )}

                  {isColVisible("gross_pnl") && (
                    <th
                      scope="col"
                      onClick={() => handleSort("gross_pnl")}
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap cursor-pointer hover:text-slate-900"
                    >
                      Total Profit (Gross) {sortKey === "gross_pnl" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                    </th>
                  )}

                  {isColVisible("tax_brokerage_stt") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-amber-50/70 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap text-amber-900"
                    >
                      Brok + STT
                    </th>
                  )}

                  {isColVisible("tax_exchange_gst_stamp") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-amber-50/70 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap text-amber-900"
                    >
                      NSE + Stamp + GST
                    </th>
                  )}

                  {isColVisible("total_taxes") && (
                    <th
                      scope="col"
                      onClick={() => handleSort("total_taxes")}
                      className="sticky top-0 z-10 bg-amber-50/90 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap text-amber-900 cursor-pointer hover:text-amber-950"
                    >
                      Tax Paid {sortKey === "total_taxes" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                    </th>
                  )}

                  {isColVisible("net_pnl") && (
                    <th
                      scope="col"
                      onClick={() => handleSort("net_pnl")}
                      className="sticky top-0 z-10 bg-slate-100 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap text-slate-900 cursor-pointer"
                    >
                      Profit After Tax {sortKey === "net_pnl" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                    </th>
                  )}

                  {isColVisible("net_return_pct") && (
                    <th
                      scope="col"
                      onClick={() => handleSort("net_return_pct")}
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap cursor-pointer hover:text-slate-900"
                    >
                      Net ROI % {sortKey === "net_return_pct" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                    </th>
                  )}

                  {isColVisible("r_metrics") && (
                    <th
                      scope="col"
                      onClick={() => handleSort("realised_r_multiple")}
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap cursor-pointer hover:text-slate-900"
                    >
                      Realised R / Peak MFE {sortKey === "realised_r_multiple" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                    </th>
                  )}

                  {isColVisible("stops_hwm") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-right whitespace-nowrap"
                    >
                      Init SL / Final SL / HWM
                    </th>
                  )}

                  {isColVisible("sl_gear_mods") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 text-center whitespace-nowrap"
                    >
                      Gear / SL Mods
                    </th>
                  )}

                  {isColVisible("exit_reason") && (
                    <th
                      scope="col"
                      className="sticky top-0 z-10 bg-slate-50 py-3.5 px-3.5 border-b border-slate-200 whitespace-nowrap"
                    >
                      Exit Reason
                    </th>
                  )}
                </tr>
              </thead>

              <tbody>
                {filteredTrades.map((t: TradeHistoryItemData) => {
                  const isExpanded = expandedPositionId === t.position_id;
                  const buyTime = formatTimestampShort(t.purchase_time, t.trading_date);
                  const sellTime = formatTimestampShort(t.sell_time, t.trading_date);
                  const isWinner = t.net_pnl_after_tax > 0;
                  const brokPlusStt = t.taxes.brokerage + t.taxes.stt;
                  const otherTaxes =
                    t.taxes.exchange_txn +
                    t.taxes.stamp_duty +
                    t.taxes.sebi_charges +
                    t.taxes.gst;
                  const totalTurnover = t.total_invested + t.total_sell_value;

                  return (
                    <React.Fragment key={t.position_id}>
                      <tr
                        onClick={() =>
                          setExpandedPositionId(isExpanded ? null : t.position_id)
                        }
                        className={`cursor-pointer transition-colors group ${
                          isExpanded
                            ? "bg-emerald-50/40"
                            : "hover:bg-slate-50/90"
                        }`}
                      >
                        {/* Sticky Row Header: Symbol + Expand Chevron */}
                        <th
                          scope="row"
                          className="sticky left-0 z-10 bg-white group-hover:bg-slate-50 py-3.5 px-4 border-b border-r border-slate-100 font-black text-slate-900 whitespace-nowrap"
                        >
                          <div className="flex items-center gap-2">
                            <span className="text-[10px] text-slate-400">
                              {isExpanded ? "▼" : "▶"}
                            </span>
                            <div>
                              <Link
                                href={`/search/${encodeURIComponent(t.symbol)}`}
                                onClick={(e) => e.stopPropagation()}
                                className="hover:text-emerald-700 hover:underline"
                              >
                                {t.symbol}
                              </Link>
                              {t.state !== "CLOSED" && (
                                <span className="ml-1.5 text-[9px] px-1.5 py-0.5 rounded bg-emerald-100 text-emerald-800 font-bold">
                                  LIVE
                                </span>
                              )}
                            </div>
                          </div>
                        </th>

                        {isColVisible("company_name") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-slate-700 font-semibold whitespace-nowrap max-w-[200px] truncate">
                            {t.company_name || t.symbol}
                          </td>
                        )}

                        {isColVisible("sector") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-slate-500 whitespace-nowrap">
                            {t.sector}
                          </td>
                        )}

                        {isColVisible("trading_date") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 whitespace-nowrap">
                            <span className="text-slate-800 font-semibold">{t.trading_date}</span>
                            <span
                              className={`ml-1.5 text-[9px] font-bold px-1.5 py-0.5 rounded ${
                                t.mode === "LIVE"
                                  ? "bg-rose-100 text-rose-800"
                                  : "bg-slate-100 text-slate-700"
                              }`}
                            >
                              {t.mode}
                            </span>
                          </td>
                        )}

                        {isColVisible("opened_at") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 whitespace-nowrap">
                            <span className="font-bold text-slate-900">{buyTime.timePart}</span>
                            {buyTime.datePart && (
                              <span className="block text-[10px] text-slate-400">
                                {buyTime.datePart}
                              </span>
                            )}
                          </td>
                        )}

                        {isColVisible("closed_at") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 whitespace-nowrap">
                            {t.state === "CLOSED" ? (
                              <>
                                <span className="font-bold text-slate-900">
                                  {sellTime.timePart}
                                </span>
                                {sellTime.datePart && (
                                  <span className="block text-[10px] text-slate-400">
                                    {sellTime.datePart}
                                  </span>
                                )}
                              </>
                            ) : (
                              <span className="text-[10px] font-bold text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded-full border border-emerald-200">
                                ACTIVE
                              </span>
                            )}
                          </td>
                        )}

                        {isColVisible("hold_duration") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right font-semibold text-slate-700 whitespace-nowrap">
                            {t.hold_duration_label}
                          </td>
                        )}

                        {isColVisible("pattern_type") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 whitespace-nowrap">
                            <span className="text-[10px] font-bold px-2 py-0.5 rounded bg-slate-100 text-slate-800">
                              {t.pattern_type}
                            </span>
                          </td>
                        )}

                        {isColVisible("quantity") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right font-black text-slate-900 whitespace-nowrap">
                            {t.quantity.toLocaleString("en-IN")}
                          </td>
                        )}

                        {isColVisible("buy_price") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right font-semibold text-slate-900 whitespace-nowrap">
                            ₹{t.buy_price.toFixed(2)}
                          </td>
                        )}

                        {isColVisible("sell_price") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right font-semibold text-slate-900 whitespace-nowrap">
                            ₹{t.sell_price.toFixed(2)}
                          </td>
                        )}

                        {isColVisible("total_invested") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right font-bold text-slate-900 whitespace-nowrap">
                            {formatINR(t.total_invested)}
                          </td>
                        )}

                        {isColVisible("margin_blocked") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right text-slate-600 whitespace-nowrap">
                            {formatINR(t.margin_blocked)}
                          </td>
                        )}

                        {isColVisible("total_sell_value") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right text-slate-700 whitespace-nowrap">
                            {formatINR(t.total_sell_value)}
                          </td>
                        )}

                        {isColVisible("gross_pnl") && (
                          <td
                            className={`py-3.5 px-3.5 border-b border-slate-100 text-right font-bold whitespace-nowrap ${
                              t.gross_pnl >= 0 ? "text-emerald-700" : "text-rose-700"
                            }`}
                          >
                            {formatSignedINR(t.gross_pnl)}
                          </td>
                        )}

                        {isColVisible("tax_brokerage_stt") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right text-amber-900 bg-amber-50/20 whitespace-nowrap">
                            ₹{brokPlusStt.toFixed(2)}
                            <span className="block text-[9px] text-slate-400">
                              B:₹{t.taxes.brokerage.toFixed(0)} + S:₹{t.taxes.stt.toFixed(1)}
                            </span>
                          </td>
                        )}

                        {isColVisible("tax_exchange_gst_stamp") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right text-amber-900 bg-amber-50/20 whitespace-nowrap">
                            ₹{otherTaxes.toFixed(2)}
                            <span className="block text-[9px] text-slate-400">
                              GST:₹{t.taxes.gst.toFixed(1)} + Stamp:₹{t.taxes.stamp_duty.toFixed(1)}
                            </span>
                          </td>
                        )}

                        {isColVisible("total_taxes") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right font-bold text-amber-900 bg-amber-50/40 whitespace-nowrap">
                            -₹{t.taxes.total_charges.toFixed(2)}
                          </td>
                        )}

                        {isColVisible("net_pnl") && (
                          <td
                            className={`py-3.5 px-3.5 border-b border-slate-100 text-right font-black whitespace-nowrap ${
                              isWinner
                                ? "text-emerald-800 bg-emerald-50/40"
                                : "text-rose-800 bg-rose-50/40"
                            }`}
                          >
                            {formatSignedINR(t.net_pnl_after_tax)}
                          </td>
                        )}

                        {isColVisible("net_return_pct") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right whitespace-nowrap">
                            <span
                              className={`font-bold ${
                                t.return_on_invested_pct >= 0 ? "text-emerald-700" : "text-rose-700"
                              }`}
                            >
                              {t.return_on_invested_pct >= 0 ? "+" : ""}
                              {t.return_on_invested_pct.toFixed(2)}%
                            </span>
                            <span className="block text-[9px] text-slate-400">
                              5x Margin: {t.return_on_margin_pct >= 0 ? "+" : ""}
                              {t.return_on_margin_pct.toFixed(2)}%
                            </span>
                          </td>
                        )}

                        {isColVisible("r_metrics") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right whitespace-nowrap">
                            <span
                              className={`font-black ${
                                t.realised_r_multiple >= 0 ? "text-emerald-700" : "text-rose-700"
                              }`}
                            >
                              {t.realised_r_multiple >= 0 ? "+" : ""}
                              {t.realised_r_multiple.toFixed(2)}R
                            </span>
                            <span className="text-slate-400"> / </span>
                            <span className="text-indigo-700 font-semibold">
                              +{t.peak_mfe_r.toFixed(2)}R
                            </span>
                            {t.peak_mfe_r >= 0.2 && (
                              <span className="block text-[9px] text-slate-400">
                                Capture: {t.r_capture_efficiency_pct.toFixed(0)}%
                              </span>
                            )}
                          </td>
                        )}

                        {isColVisible("stops_hwm") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-right whitespace-nowrap text-[11px]">
                            <span className="text-rose-700">₹{t.initial_stop.toFixed(2)}</span>
                            <span className="text-slate-400"> → </span>
                            <span className="font-bold text-slate-800">
                              ₹{t.final_stop.toFixed(2)}
                            </span>
                            <span className="block text-[9px] text-emerald-700">
                              HWM: ₹{t.high_water_mark.toFixed(2)}
                            </span>
                          </td>
                        )}

                        {isColVisible("sl_gear_mods") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 text-center whitespace-nowrap">
                            <span className="text-[10px] font-bold px-2 py-0.5 rounded bg-slate-100 text-slate-800">
                              G{t.final_gear} • {t.mod_count}/25
                            </span>
                          </td>
                        )}

                        {isColVisible("exit_reason") && (
                          <td className="py-3.5 px-3.5 border-b border-slate-100 whitespace-nowrap">
                            <span className="font-semibold text-slate-800">
                              {t.exit_reason}
                            </span>
                            {t.tranche1_done && (
                              <span className="ml-1.5 text-[9px] font-bold px-1.5 py-0.5 rounded bg-emerald-100 text-emerald-800">
                                +1R 50% BANKED
                              </span>
                            )}
                          </td>
                        )}
                      </tr>

                      {/* =========================================================
                          Expandable Row Drawer: Contract Note Tax Receipt + Child Order Legs
                          ========================================================= */}
                      {isExpanded && (
                        <tr className="bg-slate-50/90">
                          <td
                            colSpan={visibleColumns.length + 1}
                            className="p-4 sm:p-6 border-b border-slate-200"
                          >
                            <div className="grid grid-cols-1 lg:grid-cols-12 gap-5">
                              {/* Left 5 Cols: Itemized Indian Contract-Note Tax Receipt */}
                              <div className="lg:col-span-5 bg-white rounded-2xl border border-slate-200/90 p-4 space-y-3">
                                <div className="flex items-center justify-between border-b border-slate-100 pb-2.5">
                                  <div>
                                    <h4 className="text-xs font-black text-slate-900 uppercase">
                                      Indian Intraday Contract-Note Tax Receipt
                                    </h4>
                                    <span className="text-[10px] text-slate-400">
                                      Position ID: {t.position_id} • {t.company_name || t.symbol}
                                    </span>
                                  </div>
                                  <span className="text-[10px] font-bold px-2 py-0.5 rounded bg-amber-100 text-amber-900">
                                    NSE EQ MIS
                                  </span>
                                </div>

                                <div className="space-y-1.5 text-xs">
                                  <div className="flex justify-between text-slate-600">
                                    <span>
                                      Buy Value ({t.quantity} × ₹{t.buy_price.toFixed(2)}):
                                    </span>
                                    <strong className="text-slate-900">
                                      {formatINR(t.total_invested)}
                                    </strong>
                                  </div>
                                  <div className="flex justify-between text-slate-600">
                                    <span>
                                      Sell Value ({t.quantity} × ₹{t.sell_price.toFixed(2)}):
                                    </span>
                                    <strong className="text-slate-900">
                                      {formatINR(t.total_sell_value)}
                                    </strong>
                                  </div>
                                  <div className="flex justify-between text-slate-600 pb-1.5 border-b border-slate-100">
                                    <span>Total Exchange Turnover:</span>
                                    <strong className="text-slate-900">
                                      {formatINR(totalTurnover)}
                                    </strong>
                                  </div>

                                  <div className="flex justify-between text-slate-600 pt-1">
                                    <span>1. Brokerage (0.03% or ₹20/order):</span>
                                    <span>₹{t.taxes.brokerage.toFixed(2)}</span>
                                  </div>
                                  <div className="flex justify-between text-slate-600">
                                    <span>2. STT (0.025% on Sell Value):</span>
                                    <span>₹{t.taxes.stt.toFixed(2)}</span>
                                  </div>
                                  <div className="flex justify-between text-slate-600">
                                    <span>3. NSE Exchange Txn (0.00297%):</span>
                                    <span>₹{t.taxes.exchange_txn.toFixed(2)}</span>
                                  </div>
                                  <div className="flex justify-between text-slate-600">
                                    <span>4. SEBI Turnover Fee (0.0001%):</span>
                                    <span>₹{t.taxes.sebi_charges.toFixed(2)}</span>
                                  </div>
                                  <div className="flex justify-between text-slate-600">
                                    <span>5. Stamp Duty (0.003% on Buy Value):</span>
                                    <span>₹{t.taxes.stamp_duty.toFixed(2)}</span>
                                  </div>
                                  <div className="flex justify-between text-slate-600 pb-1.5 border-b border-slate-100">
                                    <span>6. GST (18% on Brok + NSE + SEBI):</span>
                                    <span>₹{t.taxes.gst.toFixed(2)}</span>
                                  </div>

                                  <div className="flex justify-between text-amber-900 font-bold pt-1">
                                    <span>Total Statutory Taxes Paid:</span>
                                    <span>-₹{t.taxes.total_charges.toFixed(2)}</span>
                                  </div>
                                  <div className="flex justify-between text-slate-700 font-bold">
                                    <span>Total Profit / Loss (Gross):</span>
                                    <span>{formatSignedINR(t.gross_pnl)}</span>
                                  </div>
                                  <div className="flex justify-between text-sm font-black pt-1.5 border-t border-slate-200">
                                    <span className="text-slate-900">
                                      Net Profit / Loss After Tax:
                                    </span>
                                    <span
                                      className={
                                        t.net_pnl_after_tax >= 0
                                          ? "text-emerald-700"
                                          : "text-rose-700"
                                      }
                                    >
                                      {formatSignedINR(t.net_pnl_after_tax)}
                                    </span>
                                  </div>
                                </div>
                              </div>

                              {/* Right 7 Cols: OMS Child Order Legs & Stop-Loss Audit */}
                              <div className="lg:col-span-7 bg-white rounded-2xl border border-slate-200/90 p-4 space-y-3 flex flex-col justify-between">
                                <div className="space-y-3">
                                  <div className="flex items-center justify-between border-b border-slate-100 pb-2.5">
                                    <div>
                                      <h4 className="text-xs font-black text-slate-900 uppercase">
                                        OMS Child Order Legs &amp; Dynamic SL Trajectory
                                      </h4>
                                      <span className="text-[10px] text-slate-400">
                                        1R = ₹{t.one_r.toFixed(2)}/share • Breakeven Stop = ₹
                                        {t.breakeven_stop.toFixed(2)} • Final Gear {t.final_gear}
                                      </span>
                                    </div>
                                    <Link
                                      href={`/search/${encodeURIComponent(t.symbol)}`}
                                      className="text-xs font-bold text-emerald-700 hover:underline"
                                    >
                                      Open {t.symbol} Chart →
                                    </Link>
                                  </div>

                                  {t.orders && t.orders.length > 0 ? (
                                    <div className="overflow-x-auto">
                                      <table className="w-full text-left text-[11px] border-collapse">
                                        <thead>
                                          <tr className="border-b border-slate-200 text-[10px] text-slate-400 uppercase">
                                            <th className="py-1.5 pr-3">Purpose</th>
                                            <th className="py-1.5 pr-3">Side / Type</th>
                                            <th className="py-1.5 pr-3 text-right">Filled / Req</th>
                                            <th className="py-1.5 pr-3 text-right">Avg Fill / Trigger</th>
                                            <th className="py-1.5 pr-3">Status</th>
                                            <th className="py-1.5 text-right">Updated</th>
                                          </tr>
                                        </thead>
                                        <tbody className="divide-y divide-slate-100">
                                          {t.orders.map((leg) => (
                                            <tr key={leg.order_id}>
                                              <td className="py-2 pr-3 font-bold text-slate-900">
                                                {leg.leg_type}
                                              </td>
                                              <td className="py-2 pr-3">
                                                <span
                                                  className={`font-bold ${
                                                    leg.transaction_type === "BUY"
                                                      ? "text-emerald-700"
                                                      : "text-rose-700"
                                                  }`}
                                                >
                                                  {leg.transaction_type}
                                                </span>{" "}
                                                <span className="text-slate-500">
                                                  ({leg.order_type})
                                                </span>
                                              </td>
                                              <td className="py-2 pr-3 text-right font-semibold">
                                                {leg.filled_quantity}/{leg.quantity}
                                              </td>
                                              <td className="py-2 pr-3 text-right">
                                                ₹
                                                {(leg.average_price > 0
                                                  ? leg.average_price
                                                  : leg.price
                                                ).toFixed(2)}
                                                {leg.trigger_price > 0 && (
                                                  <span className="text-[10px] text-slate-400 block">
                                                    Trig: ₹{leg.trigger_price.toFixed(2)}
                                                  </span>
                                                )}
                                              </td>
                                              <td className="py-2 pr-3">
                                                <span className="px-1.5 py-0.5 rounded bg-slate-100 text-slate-700 font-bold text-[10px]">
                                                  {leg.status}
                                                </span>
                                              </td>
                                              <td className="py-2 text-right text-slate-500">
                                                {formatTimestampShort(leg.updated_at).timePart}
                                              </td>
                                            </tr>
                                          ))}
                                        </tbody>
                                      </table>
                                    </div>
                                  ) : (
                                    <p className="text-xs text-slate-400">
                                      Position summary recorded directly in ledger.
                                    </p>
                                  )}
                                </div>

                                <div className="pt-2.5 border-t border-slate-100 grid grid-cols-2 sm:grid-cols-4 gap-2 text-[11px]">
                                  <div className="p-2 rounded-xl bg-slate-50">
                                    <span className="text-[9px] text-slate-400 block uppercase">
                                      Initial Stop (-1.0R)
                                    </span>
                                    <strong className="text-rose-700">
                                      ₹{t.initial_stop.toFixed(2)}
                                    </strong>
                                  </div>
                                  <div className="p-2 rounded-xl bg-slate-50">
                                    <span className="text-[9px] text-slate-400 block uppercase">
                                      Tax-Adj Breakeven
                                    </span>
                                    <strong className="text-slate-800">
                                      ₹{t.breakeven_stop.toFixed(2)}
                                    </strong>
                                  </div>
                                  <div className="p-2 rounded-xl bg-slate-50">
                                    <span className="text-[9px] text-slate-400 block uppercase">
                                      Final Active Stop
                                    </span>
                                    <strong className="text-indigo-700">
                                      ₹{t.final_stop.toFixed(2)}
                                    </strong>
                                  </div>
                                  <div className="p-2 rounded-xl bg-slate-50">
                                    <span className="text-[9px] text-slate-400 block uppercase">
                                      Peak High Water Mark
                                    </span>
                                    <strong className="text-emerald-700">
                                      ₹{t.high_water_mark.toFixed(2)} (+{t.peak_mfe_r.toFixed(2)}R)
                                    </strong>
                                  </div>
                                </div>
                              </div>
                            </div>
                          </td>
                        </tr>
                      )}
                    </React.Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
