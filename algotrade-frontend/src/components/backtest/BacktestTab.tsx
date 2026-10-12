"use client";

import { useState, useEffect, useMemo } from "react";
import {
  BacktestRequest,
  BacktestResult,
  BacktestPreset,
  BacktestTrade,
} from "../types";
import {
  ResponsiveContainer,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts";

interface BacktestTabProps {
  backendUrl: string;
}

export default function BacktestTab({ backendUrl }: BacktestTabProps) {
  const [presets, setPresets] = useState<BacktestPreset[]>([]);
  const [startDate, setStartDate] = useState<string>("");
  const [endDate, setEndDate] = useState<string>("");
  const [initialCapital, setInitialCapital] = useState<number>(10000);
  const [riskPerTradePct, setRiskPerTradePct] = useState<number>(1.0);
  const [minScoreThreshold, setMinScoreThreshold] = useState<number>(75);
  const [trailingSlPct, setTrailingSlPct] = useState<number>(2.0);
  const [slippagePct, setSlippagePct] = useState<number>(0.05);
  const [leverageMultiplier, setLeverageMultiplier] = useState<number>(1.0);

  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<BacktestResult | null>(null);
  const [tradeFilter, setTradeFilter] = useState<"ALL" | "WINS" | "LOSSES">("ALL");
  const [activeChartTab, setActiveChartTab] = useState<"equity" | "drawdown">("equity");

  // Fetch presets on mount
  useEffect(() => {
    const fetchPresets = async () => {
      try {
        const res = await fetch(`${backendUrl}/api/backtest/presets`);
        if (res.ok) {
          const data: BacktestPreset[] = await res.json();
          setPresets(data);
          if (data.length > 0 && !startDate) {
            // Default to 90 Days preset
            const defaultPreset = data.find((p) => p.id === "90D") || data[0];
            setStartDate(defaultPreset.startDate);
            setEndDate(defaultPreset.endDate);
          }
        }
      } catch (err) {
        // Fallback default dates if backend unreached
        const today = new Date();
        const past = new Date();
        past.setDate(today.getDate() - 90);
        setStartDate(past.toISOString().split("T")[0]);
        setEndDate(today.toISOString().split("T")[0]);
      }
    };

    fetchPresets();
  }, [backendUrl]);

  // Execute Backtest Run
  const handleRunBacktest = async () => {
    if (!startDate || !endDate) return;
    setLoading(true);

    try {
      const payload: BacktestRequest = {
        startDate,
        endDate,
        initialCapital,
        riskPerTradePct,
        minScoreThreshold,
        trailingSlPct,
        slippagePct,
        leverageMultiplier,
      };

      const res = await fetch(`${backendUrl}/api/backtest/run`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

      if (res.ok) {
        const data: BacktestResult = await res.json();
        setResult(data);
      }
    } catch (err) {
      console.error("Backtest execution failed:", err);
    } finally {
      setLoading(false);
    }
  };

  const handleApplyPreset = (preset: BacktestPreset) => {
    setStartDate(preset.startDate);
    setEndDate(preset.endDate);
  };

  // Filtered trades
  const filteredTrades = useMemo(() => {
    if (!result) return [];
    if (tradeFilter === "WINS") return result.trades.filter((t) => t.netPnL > 0);
    if (tradeFilter === "LOSSES") return result.trades.filter((t) => t.netPnL <= 0);
    return result.trades;
  }, [result, tradeFilter]);

  // CSV Exporter
  const handleExportCsv = () => {
    if (!result || result.trades.length === 0) return;
    const headers = [
      "ID", "Date", "Symbol", "Score", "EntryPrice", "ExitPrice",
      "Quantity", "EntryTime", "ExitTime", "ExitReason",
      "GrossPnL", "StatutoryCharges", "NetPnL", "ReturnPct"
    ];
    const rows = result.trades.map((t) => [
      t.id, t.date, t.symbol, t.score, t.entryPrice, t.exitPrice,
      t.quantity, t.entryTime, t.exitTime, t.exitReason,
      t.grossPnL, t.totalCharges, t.netPnL, t.returnPct
    ]);
    const csvContent =
      "data:text/csv;charset=utf-8," +
      [headers.join(","), ...rows.map((r) => r.join(","))].join("\n");
    const encodedUri = encodeURI(csvContent);
    const link = document.createElement("a");
    link.setAttribute("href", encodedUri);
    link.setAttribute("download", `AlgoTrade_Backtest_${startDate}_to_${endDate}.csv`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  return (
    <div className="flex flex-col gap-6 animate-fadeIn pb-12">
      {/* Configuration & Controls Card */}
      <div className="bg-slate-900/80 border border-slate-800 rounded-2xl p-5 sm:p-6 shadow-xl backdrop-blur-md">
        <div className="flex flex-col md:flex-row items-start md:items-center justify-between pb-5 border-b border-slate-800 gap-3">
          <div>
            <div className="flex items-center gap-2">
              <span className="w-8 h-8 rounded-lg bg-emerald-500/10 border border-emerald-500/30 flex items-center justify-center text-emerald-400 font-bold text-base">
                🧪
              </span>
              <h2 className="text-xl sm:text-2xl font-black text-white tracking-tight">
                Strategy Backtest Lab
              </h2>
              <span className="text-xs px-2 py-0.5 rounded-full bg-slate-800 text-slate-300 border border-slate-700 font-mono">
                15-Min Momentum Breakout
              </span>
            </div>
            <p className="text-xs sm:text-sm text-slate-400 mt-1">
              Test historical performance across the Nifty 100 universe with 100-point scoring, trailing stops, slippage &amp; Indian taxes.
            </p>
          </div>

          {/* Preset Buttons */}
          <div className="flex items-center gap-1.5 flex-wrap">
            <span className="text-[11px] font-semibold text-slate-400 uppercase tracking-wider mr-1">
              Presets:
            </span>
            {presets.map((p) => (
              <button
                key={p.id}
                onClick={() => handleApplyPreset(p)}
                className={`px-3 py-1 rounded-lg text-xs font-medium transition cursor-pointer border ${
                  startDate === p.startDate && endDate === p.endDate
                    ? "bg-emerald-500/20 border-emerald-500/40 text-emerald-300 font-bold"
                    : "bg-slate-800/80 border-slate-700 text-slate-300 hover:bg-slate-700 hover:text-white"
                }`}
                title={p.description}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>

        {/* Parameter Inputs Grid */}
        <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-7 gap-3 sm:gap-4 mt-5">
          {/* Start Date */}
          <div>
            <label className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 block mb-1">
              Start Date
            </label>
            <input
              type="date"
              value={startDate}
              onChange={(e) => setStartDate(e.target.value)}
              className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-emerald-500/60 transition"
            />
          </div>

          {/* End Date */}
          <div>
            <label className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 block mb-1">
              End Date
            </label>
            <input
              type="date"
              value={endDate}
              onChange={(e) => setEndDate(e.target.value)}
              className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-emerald-500/60 transition"
            />
          </div>

          {/* Initial Capital */}
          <div>
            <label className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 block mb-1">
              Initial Capital (₹)
            </label>
            <input
              type="number"
              value={initialCapital}
              step={1000}
              min={1000}
              onChange={(e) => setInitialCapital(Number(e.target.value))}
              className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-emerald-500/60 transition"
            />
          </div>

          {/* Intraday Leverage (SEBI MIS) */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-[11px] font-semibold uppercase tracking-wider text-slate-400">
                Leverage: {leverageMultiplier.toFixed(1)}x
              </label>
              <span className="text-[10px] font-mono font-medium text-emerald-400">
                {leverageMultiplier === 1.0 ? "Cash" : `${leverageMultiplier}x MIS`}
              </span>
            </div>
            <input
              type="range"
              min="1.0"
              max="5.0"
              step="0.5"
              value={leverageMultiplier}
              onChange={(e) => setLeverageMultiplier(parseFloat(e.target.value))}
              className="w-full mt-2 accent-indigo-500 cursor-pointer"
            />
            <div className="flex items-center justify-between mt-1 text-[9px] font-mono text-slate-400">
              <span
                role="button"
                onClick={() => setLeverageMultiplier(1.0)}
                className={`cursor-pointer hover:text-white px-1 rounded ${leverageMultiplier === 1.0 ? "text-emerald-400 font-bold" : ""}`}
              >
                1x
              </span>
              <span
                role="button"
                onClick={() => setLeverageMultiplier(2.0)}
                className={`cursor-pointer hover:text-white px-1 rounded ${leverageMultiplier === 2.0 ? "text-emerald-400 font-bold" : ""}`}
              >
                2x
              </span>
              <span
                role="button"
                onClick={() => setLeverageMultiplier(3.0)}
                className={`cursor-pointer hover:text-white px-1 rounded ${leverageMultiplier === 3.0 ? "text-emerald-400 font-bold" : ""}`}
              >
                3x
              </span>
              <span
                role="button"
                onClick={() => setLeverageMultiplier(5.0)}
                className={`cursor-pointer hover:text-white px-1 rounded ${leverageMultiplier === 5.0 ? "text-emerald-400 font-bold" : ""}`}
              >
                5x
              </span>
            </div>
          </div>

          {/* Risk Per Trade % */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-[11px] font-semibold uppercase tracking-wider text-slate-400">
                Risk: {riskPerTradePct.toFixed(1)}%
              </label>
              <span className="text-[10px] font-mono font-medium text-emerald-400" title="Calculated from total position capacity (Capital × Leverage)">
                ₹{Math.round(initialCapital * leverageMultiplier * (riskPerTradePct / 100)).toLocaleString("en-IN")} max loss
              </span>
            </div>
            <input
              type="range"
              min="0.5"
              max="5.0"
              step="0.1"
              value={riskPerTradePct}
              onChange={(e) => setRiskPerTradePct(parseFloat(e.target.value))}
              className="w-full mt-2 accent-emerald-500 cursor-pointer"
            />
            <div className="flex items-center justify-between mt-1 text-[9px] font-mono text-slate-400">
              <span
                role="button"
                onClick={() => setRiskPerTradePct(1.0)}
                className={`cursor-pointer hover:text-white px-1 rounded ${riskPerTradePct === 1.0 ? "text-emerald-400 font-bold" : ""}`}
              >
                1%
              </span>
              <span
                role="button"
                onClick={() => setRiskPerTradePct(2.0)}
                className={`cursor-pointer hover:text-white px-1 rounded ${riskPerTradePct === 2.0 ? "text-emerald-400 font-bold" : ""}`}
              >
                2%
              </span>
              <span
                role="button"
                onClick={() => setRiskPerTradePct(3.0)}
                className={`cursor-pointer hover:text-white px-1 rounded ${riskPerTradePct === 3.0 ? "text-emerald-400 font-bold" : ""}`}
              >
                3%
              </span>
              <span
                role="button"
                onClick={() => setRiskPerTradePct(5.0)}
                className={`cursor-pointer hover:text-white px-1 rounded ${riskPerTradePct === 5.0 ? "text-emerald-400 font-bold" : ""}`}
              >
                5%
              </span>
            </div>
          </div>

          {/* Trailing Stop % */}
          <div>
            <label className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 block mb-1">
              Trail Buffer (%): {trailingSlPct.toFixed(1)}%
            </label>
            <input
              type="range"
              min="1.0"
              max="3.5"
              step="0.1"
              value={trailingSlPct}
              onChange={(e) => setTrailingSlPct(parseFloat(e.target.value))}
              className="w-full mt-2 accent-amber-500 cursor-pointer"
            />
          </div>

          {/* Min Score Threshold */}
          <div>
            <label className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 block mb-1">
              Min Score: {minScoreThreshold} pts
            </label>
            <input
              type="range"
              min="65"
              max="90"
              step="1"
              value={minScoreThreshold}
              onChange={(e) => setMinScoreThreshold(parseInt(e.target.value))}
              className="w-full mt-2 accent-sky-500 cursor-pointer"
            />
          </div>
        </div>

        {/* Run Action Button */}
        <div className="flex items-center justify-between mt-6 pt-4 border-t border-slate-800">
          <div className="text-xs text-slate-400 hidden sm:block font-mono">
            Simulates Nifty 100 constituents • Up to 5x SEBI MIS leverage • Models 0.05% slippage • 15:05 mandatory exit
          </div>
          <button
            onClick={handleRunBacktest}
            disabled={loading || !startDate || !endDate}
            className="w-full sm:w-auto px-6 py-2.5 rounded-xl font-bold text-sm bg-gradient-to-r from-emerald-500 to-teal-500 hover:from-emerald-400 hover:to-teal-400 text-slate-950 transition-all shadow-lg shadow-emerald-950/40 cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
          >
            {loading ? (
              <>
                <span className="w-4 h-4 border-2 border-slate-950 border-t-transparent rounded-full animate-spin" />
                <span>Running Simulation...</span>
              </>
            ) : (
              <>
                <span>▶ Run Backtest Simulation</span>
              </>
            )}
          </button>
        </div>
      </div>

      {/* Initial Empty State if No Run Yet */}
      {!result && !loading && (
        <div className="bg-slate-900/40 border border-dashed border-slate-800 rounded-2xl p-12 text-center flex flex-col items-center justify-center">
          <div className="w-14 h-14 rounded-2xl bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center text-emerald-400 text-2xl mb-4">
            📊
          </div>
          <h3 className="text-lg font-bold text-white">No Simulation Results Yet</h3>
          <p className="text-sm text-slate-400 max-w-md mt-1 leading-relaxed">
            Select your preferred historical date range and risk parameters above, then click <strong>&quot;Run Backtest Simulation&quot;</strong> to inspect performance metrics.
          </p>
          <button
            onClick={handleRunBacktest}
            className="mt-6 px-5 py-2 rounded-xl text-xs font-bold bg-slate-800 hover:bg-slate-700 text-emerald-400 border border-emerald-500/30 transition cursor-pointer"
          >
            Run 90-Day Sample Test Now →
          </button>
        </div>
      )}

      {/* Results View */}
      {result && (
        <>
          {/* Summary KPI Scorecard */}
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3 sm:gap-4">
            {/* Net Return */}
            <div className="bg-slate-900/80 border border-slate-800 rounded-xl p-4">
              <p className="text-[11px] text-slate-400 uppercase tracking-wider">Net Take-Home P&amp;L</p>
              <p
                className={`text-xl sm:text-2xl font-black font-mono mt-1 ${
                  result.summary.totalNetPnL >= 0 ? "text-emerald-400" : "text-rose-400"
                }`}
              >
                {result.summary.totalNetPnL >= 0 ? "+" : ""}₹
                {result.summary.totalNetPnL.toLocaleString("en-IN")}
              </p>
              <p
                className={`text-xs font-mono mt-0.5 ${
                  result.summary.totalReturnPct >= 0 ? "text-emerald-400/80" : "text-rose-400/80"
                }`}
              >
                {result.summary.totalReturnPct >= 0 ? "+" : ""}
                {result.summary.totalReturnPct.toFixed(2)}% on capital
              </p>
            </div>

            {/* Win Rate */}
            <div className="bg-slate-900/80 border border-slate-800 rounded-xl p-4">
              <p className="text-[11px] text-slate-400 uppercase tracking-wider">Win Rate %</p>
              <p className="text-xl sm:text-2xl font-black font-mono text-sky-400 mt-1">
                {result.summary.winRatePct.toFixed(1)}%
              </p>
              <p className="text-xs font-mono text-slate-400 mt-0.5">
                {result.summary.winTrades} Wins / {result.summary.lossTrades} Losses
              </p>
            </div>

            {/* Profit Factor */}
            <div className="bg-slate-900/80 border border-slate-800 rounded-xl p-4">
              <p className="text-[11px] text-slate-400 uppercase tracking-wider">Profit Factor</p>
              <p className="text-xl sm:text-2xl font-black font-mono text-amber-400 mt-1">
                {result.summary.profitFactor.toFixed(2)}
              </p>
              <p className="text-xs text-slate-400 mt-0.5">Gross Wins / Gross Losses</p>
            </div>

            {/* Max Drawdown */}
            <div className="bg-slate-900/80 border border-slate-800 rounded-xl p-4">
              <p className="text-[11px] text-slate-400 uppercase tracking-wider">Max Drawdown (MDD)</p>
              <p className="text-xl sm:text-2xl font-black font-mono text-rose-400 mt-1">
                -{result.summary.maxDrawdownPct.toFixed(1)}%
              </p>
              <p className="text-xs font-mono text-slate-400 mt-0.5">
                -₹{result.summary.maxDrawdownRupees.toLocaleString("en-IN")} from peak
              </p>
            </div>

            {/* Total Taxes Paid */}
            <div className="bg-slate-900/80 border border-slate-800 rounded-xl p-4">
              <p className="text-[11px] text-slate-400 uppercase tracking-wider">Statutory Taxes &amp; Fees</p>
              <p className="text-xl sm:text-2xl font-black font-mono text-purple-400 mt-1">
                ₹{result.summary.totalTaxes.toLocaleString("en-IN")}
              </p>
              <p className="text-xs text-slate-400 mt-0.5">STT, GST, Stamp &amp; Brokerage</p>
            </div>

            {/* Sharpe Ratio */}
            <div className="bg-slate-900/80 border border-slate-800 rounded-xl p-4">
              <p className="text-[11px] text-slate-400 uppercase tracking-wider">Sharpe Ratio</p>
              <p className="text-xl sm:text-2xl font-black font-mono text-teal-400 mt-1">
                {result.summary.sharpeRatio.toFixed(2)}
              </p>
              <p className="text-xs text-slate-400 mt-0.5">Annualized risk-adjusted</p>
            </div>
          </div>

          {/* Interactive Chart Container */}
          <div className="bg-slate-900/80 border border-slate-800 rounded-2xl p-5 sm:p-6 shadow-xl backdrop-blur-md">
            <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between pb-4 border-b border-slate-800 gap-3">
              <div>
                <h3 className="text-base sm:text-lg font-bold text-white tracking-tight">
                  {activeChartTab === "equity"
                    ? "Cumulative Equity Curve Progression"
                    : "Drawdown Underwater Analysis (%)"}
                </h3>
                <p className="text-xs text-slate-400 mt-0.5">
                  {activeChartTab === "equity"
                    ? "True capital growth deducting slippage and all Indian statutory duties."
                    : "Peak-to-trough equity reduction over the simulated calendar window."}
                </p>
              </div>

              {/* Chart Tab Switcher */}
              <div className="flex items-center gap-1.5 bg-slate-950 p-1 rounded-xl border border-slate-800">
                <button
                  onClick={() => setActiveChartTab("equity")}
                  className={`px-3 py-1 rounded-lg text-xs font-medium transition cursor-pointer ${
                    activeChartTab === "equity"
                      ? "bg-emerald-500/20 text-emerald-300 font-bold border border-emerald-500/30"
                      : "text-slate-400 hover:text-white"
                  }`}
                >
                  📈 Equity Curve
                </button>
                <button
                  onClick={() => setActiveChartTab("drawdown")}
                  className={`px-3 py-1 rounded-lg text-xs font-medium transition cursor-pointer ${
                    activeChartTab === "drawdown"
                      ? "bg-rose-500/20 text-rose-300 font-bold border border-rose-500/30"
                      : "text-slate-400 hover:text-white"
                  }`}
                >
                  📉 Underwater Drawdown
                </button>
              </div>
            </div>

            {/* Recharts Area Container */}
            <div className="h-[340px] sm:h-[400px] w-full pt-4">
              <ResponsiveContainer width="100%" height="100%">
                {activeChartTab === "equity" ? (
                  <AreaChart data={result.equityCurve} margin={{ top: 10, right: 10, left: 10, bottom: 0 }}>
                    <defs>
                      <linearGradient id="equityGrad" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#10b981" stopOpacity={0.4} />
                        <stop offset="95%" stopColor="#10b981" stopOpacity={0.0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" vertical={false} />
                    <XAxis
                      dataKey="date"
                      stroke="#64748b"
                      fontSize={11}
                      tickLine={false}
                      tickFormatter={(val) => val.slice(5)} // MM-DD
                    />
                    <YAxis
                      stroke="#64748b"
                      fontSize={11}
                      domain={["dataMin - 500", "dataMax + 500"]}
                      tickLine={false}
                      tickFormatter={(val) => `₹${Number(val).toLocaleString("en-IN")}`}
                    />
                    <Tooltip
                      content={({ active, payload }) => {
                        if (active && payload && payload.length) {
                          const pt = payload[0].payload;
                          return (
                            <div className="bg-slate-950 border border-slate-800 rounded-xl p-3 shadow-xl text-xs font-mono">
                              <p className="text-slate-400 font-bold">{pt.date}</p>
                              <p className="text-emerald-400 font-bold text-sm mt-1">
                                Capital: ₹{pt.capital.toLocaleString("en-IN")}
                              </p>
                              <p className="text-slate-300 mt-1">
                                Net Trade P&amp;L:{" "}
                                <span className={pt.netPnL >= 0 ? "text-emerald-400" : "text-rose-400"}>
                                  {pt.netPnL >= 0 ? "+" : ""}₹{pt.netPnL.toFixed(2)}
                                </span>
                              </p>
                              <p className="text-slate-400 mt-0.5">
                                Drawdown: -{pt.drawdownPct.toFixed(1)}%
                              </p>
                            </div>
                          );
                        }
                        return null;
                      }}
                    />
                    <Area
                      type="monotone"
                      dataKey="capital"
                      stroke="#10b981"
                      strokeWidth={2.5}
                      fillOpacity={1}
                      fill="url(#equityGrad)"
                    />
                  </AreaChart>
                ) : (
                  <AreaChart data={result.equityCurve} margin={{ top: 10, right: 10, left: 10, bottom: 0 }}>
                    <defs>
                      <linearGradient id="ddGrad" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#f43f5e" stopOpacity={0.4} />
                        <stop offset="95%" stopColor="#f43f5e" stopOpacity={0.0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" vertical={false} />
                    <XAxis
                      dataKey="date"
                      stroke="#64748b"
                      fontSize={11}
                      tickLine={false}
                      tickFormatter={(val) => val.slice(5)}
                    />
                    <YAxis
                      stroke="#64748b"
                      fontSize={11}
                      domain={[0, "dataMax + 2"]}
                      tickLine={false}
                      tickFormatter={(val) => `-${Number(val).toFixed(1)}%`}
                    />
                    <Tooltip
                      content={({ active, payload }) => {
                        if (active && payload && payload.length) {
                          const pt = payload[0].payload;
                          return (
                            <div className="bg-slate-950 border border-slate-800 rounded-xl p-3 shadow-xl text-xs font-mono">
                              <p className="text-slate-400 font-bold">{pt.date}</p>
                              <p className="text-rose-400 font-bold text-sm mt-1">
                                Drawdown: -{pt.drawdownPct.toFixed(2)}%
                              </p>
                            </div>
                          );
                        }
                        return null;
                      }}
                    />
                    <Area
                      type="monotone"
                      dataKey="drawdownPct"
                      stroke="#f43f5e"
                      strokeWidth={2}
                      fillOpacity={1}
                      fill="url(#ddGrad)"
                    />
                  </AreaChart>
                )}
              </ResponsiveContainer>
            </div>
          </div>

          {/* Trade Audit Table Card */}
          <div className="bg-slate-900/80 border border-slate-800 rounded-2xl p-5 sm:p-6 shadow-xl backdrop-blur-md">
            <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between pb-4 border-b border-slate-800 gap-3">
              <div className="flex items-center gap-3">
                <h3 className="text-base sm:text-lg font-bold text-white tracking-tight">
                  Simulated Trade Execution Log
                </h3>
                <span className="text-xs px-2 py-0.5 rounded-full bg-slate-800 text-slate-300 font-mono">
                  {filteredTrades.length} of {result.trades.length} trades
                </span>
              </div>

              {/* Filters and Export */}
              <div className="flex items-center gap-2">
                <div className="flex items-center gap-1 bg-slate-950 p-1 rounded-xl border border-slate-800 text-xs">
                  <button
                    onClick={() => setTradeFilter("ALL")}
                    className={`px-2.5 py-1 rounded-lg transition cursor-pointer ${
                      tradeFilter === "ALL"
                        ? "bg-slate-800 text-white font-bold"
                        : "text-slate-400 hover:text-white"
                    }`}
                  >
                    All ({result.trades.length})
                  </button>
                  <button
                    onClick={() => setTradeFilter("WINS")}
                    className={`px-2.5 py-1 rounded-lg transition cursor-pointer ${
                      tradeFilter === "WINS"
                        ? "bg-emerald-500/20 text-emerald-300 font-bold"
                        : "text-slate-400 hover:text-white"
                    }`}
                  >
                    Wins ({result.summary.winTrades})
                  </button>
                  <button
                    onClick={() => setTradeFilter("LOSSES")}
                    className={`px-2.5 py-1 rounded-lg transition cursor-pointer ${
                      tradeFilter === "LOSSES"
                        ? "bg-rose-500/20 text-rose-300 font-bold"
                        : "text-slate-400 hover:text-white"
                    }`}
                  >
                    Losses ({result.summary.lossTrades})
                  </button>
                </div>

                <button
                  onClick={handleExportCsv}
                  className="px-3 py-1.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-200 text-xs font-semibold border border-slate-700 transition cursor-pointer flex items-center gap-1.5"
                  title="Download CSV audit log"
                >
                  <span>Export CSV</span>
                  <span>📥</span>
                </button>
              </div>
            </div>

            {/* Table Container */}
            <div className="overflow-x-auto max-h-[500px] overflow-y-auto mt-4 scrollbar-thin scrollbar-thumb-slate-800">
              <table className="w-full text-left text-xs">
                <thead className="bg-slate-950/80 sticky top-0 z-10 text-[11px] font-semibold text-slate-400 uppercase tracking-wider border-b border-slate-800">
                  <tr>
                    <th className="py-3 px-3">#</th>
                    <th className="py-3 px-3">Date</th>
                    <th className="py-3 px-3">Stock</th>
                    <th className="py-3 px-3">Score</th>
                    <th className="py-3 px-3">Entry (09:30)</th>
                    <th className="py-3 px-3">Exit Price</th>
                    <th className="py-3 px-3">Qty</th>
                    <th className="py-3 px-3">Exit Reason</th>
                    <th className="py-3 px-3 text-right">Gross P&amp;L</th>
                    <th className="py-3 px-3 text-right">Taxes</th>
                    <th className="py-3 px-3 text-right">Net P&amp;L</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800/60 font-mono">
                  {filteredTrades.map((t) => (
                    <tr key={t.id} className="hover:bg-slate-800/30 transition-colors">
                      <td className="py-3 px-3 text-slate-500">{t.id}</td>
                      <td className="py-3 px-3 text-slate-300 whitespace-nowrap">{t.date}</td>
                      <td className="py-3 px-3">
                        <span className="font-bold text-white bg-slate-800 px-2 py-0.5 rounded border border-slate-700">
                          {t.symbol}
                        </span>
                      </td>
                      <td className="py-3 px-3 text-sky-400 font-bold">{t.score} pts</td>
                      <td className="py-3 px-3 text-slate-300">₹{t.entryPrice.toFixed(2)}</td>
                      <td className="py-3 px-3 text-slate-300">
                        ₹{t.exitPrice.toFixed(2)}
                        <span className="text-[10px] text-slate-500 ml-1">({t.exitTime})</span>
                      </td>
                      <td className="py-3 px-3 text-slate-300">{t.quantity}</td>
                      <td className="py-3 px-3">
                        <span
                          className={`px-2 py-0.5 rounded text-[10px] font-semibold uppercase tracking-wider border ${
                            t.exitReason === "TRAILING_SL"
                              ? "bg-amber-500/10 text-amber-300 border-amber-500/30"
                              : t.exitReason === "INITIAL_SL"
                              ? "bg-rose-500/10 text-rose-300 border-rose-500/30"
                              : "bg-purple-500/10 text-purple-300 border-purple-500/30"
                          }`}
                        >
                          {t.exitReason === "TRAILING_SL"
                            ? "Trailing SL"
                            : t.exitReason === "INITIAL_SL"
                            ? "Initial SL"
                            : "15:05 Close"}
                        </span>
                      </td>
                      <td
                        className={`py-3 px-3 text-right font-bold ${
                          t.grossPnL >= 0 ? "text-emerald-400" : "text-rose-400"
                        }`}
                      >
                        {t.grossPnL >= 0 ? "+" : ""}₹{t.grossPnL.toFixed(2)}
                      </td>
                      <td className="py-3 px-3 text-right text-slate-400">
                        ₹{t.totalCharges.toFixed(2)}
                      </td>
                      <td
                        className={`py-3 px-3 text-right font-black ${
                          t.netPnL >= 0 ? "text-emerald-400" : "text-rose-400"
                        }`}
                      >
                        {t.netPnL >= 0 ? "+" : ""}₹{t.netPnL.toFixed(2)}
                        <span className="text-[10px] block opacity-80 font-normal">
                          ({t.returnPct >= 0 ? "+" : ""}{t.returnPct.toFixed(2)}%)
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
