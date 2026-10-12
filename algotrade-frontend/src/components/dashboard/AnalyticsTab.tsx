"use client";

import { AnalyticsData } from "../types";
import { SkeletonAnalytics } from "../common/Skeleton";

interface AnalyticsTabProps {
  analytics: AnalyticsData | null;
}

export function AnalyticsTab({ analytics }: AnalyticsTabProps) {
  if (!analytics) {
    return <SkeletonAnalytics />;
  }

  return (
    <div className="flex flex-col gap-6 animate-fadeIn">
      {/* Top Financial Summary Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4 backdrop-blur-sm transition-all hover:border-slate-700/60">
          <p className="text-xs text-slate-400">Today&apos;s Net Profit (Take-Home)</p>
          <p
            className={`text-xl sm:text-2xl font-bold font-mono mt-1 ${
              !analytics
                ? "text-slate-400"
                : analytics.todayNetPnL >= 0
                ? "text-emerald-400"
                : "text-rose-400"
            }`}
          >
            {analytics
              ? `${analytics.todayNetPnL >= 0 ? "+" : ""}₹${analytics.todayNetPnL.toFixed(2)}`
              : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">
            {analytics
              ? `Gross: ₹${analytics.todayGrossPnL.toFixed(2)}`
              : "Gross: --"}
          </p>
        </div>

        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4 backdrop-blur-sm transition-all hover:border-slate-700/60">
          <p className="text-xs text-slate-400">Cumulative Net P&amp;L (Till Date)</p>
          <p
            className={`text-xl sm:text-2xl font-bold font-mono mt-1 ${
              !analytics
                ? "text-slate-400"
                : analytics.cumulativeNetPnL >= 0
                ? "text-emerald-400"
                : "text-rose-400"
            }`}
          >
            {analytics
              ? `${analytics.cumulativeNetPnL >= 0 ? "+" : ""}₹${analytics.cumulativeNetPnL.toFixed(2)}`
              : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">After all taxes &amp; brokerage</p>
        </div>

        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4 backdrop-blur-sm transition-all hover:border-slate-700/60">
          <p className="text-xs text-slate-400">Total Taxes &amp; Fees Paid</p>
          <p className="text-xl sm:text-2xl font-bold font-mono text-rose-400 mt-1">
            {analytics ? `₹${analytics.cumulativeTaxesPaid.toFixed(2)}` : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">Govt + Exchange + Zerodha</p>
        </div>

        <div className="bg-slate-900/60 border border-slate-800 rounded-xl p-4 backdrop-blur-sm transition-all hover:border-slate-700/60">
          <p className="text-xs text-slate-400">Win Rate / Profit Factor</p>
          <p className="text-xl sm:text-2xl font-bold font-mono text-sky-400 mt-1">
            {analytics ? `${analytics.winRatePct.toFixed(1)}%` : "--"}
          </p>
          <p className="text-[10px] text-slate-500 mt-1">
            {analytics
              ? `Profit Factor: ${analytics.profitFactor.toFixed(2)}`
              : "Profit Factor: --"}
          </p>
        </div>
      </div>

      {/* Indian Statutory Tax Deductions Table */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl p-5 sm:p-6 shadow-xl backdrop-blur-sm">
        <h3 className="text-base sm:text-lg font-bold text-white mb-2 flex items-center gap-2">
          🏛️ Indian Equity Statutory Charges &amp; Brokerage Breakdown (Per Trade)
        </h3>
        <p className="text-xs text-slate-400 mb-4">
          Every trade executed in the engine accounts for the exact legal taxation framework of the National Stock Exchange (NSE).
        </p>

        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3 text-xs font-mono">
          <div className="bg-slate-950/70 p-3 rounded-lg border border-slate-800 hover:border-slate-700/60 transition-all">
            <p className="text-slate-400 font-sans text-[11px]">Brokerage</p>
            <p className="font-bold text-white mt-1">₹20 or 0.03%</p>
            <p className="text-[9px] text-slate-500">Buy &amp; Sell</p>
          </div>
          <div className="bg-slate-950/70 p-3 rounded-lg border border-slate-800 hover:border-slate-700/60 transition-all">
            <p className="text-slate-400 font-sans text-[11px]">STT (Tax)</p>
            <p className="font-bold text-white mt-1">0.025%</p>
            <p className="text-[9px] text-slate-500">Sell turnover only</p>
          </div>
          <div className="bg-slate-950/70 p-3 rounded-lg border border-slate-800 hover:border-slate-700/60 transition-all">
            <p className="text-slate-400 font-sans text-[11px]">NSE Exchange</p>
            <p className="font-bold text-white mt-1">0.00297%</p>
            <p className="text-[9px] text-slate-500">Turnover total</p>
          </div>
          <div className="bg-slate-950/70 p-3 rounded-lg border border-slate-800 hover:border-slate-700/60 transition-all">
            <p className="text-slate-400 font-sans text-[11px]">SEBI Fee</p>
            <p className="font-bold text-white mt-1">0.0001%</p>
            <p className="text-[9px] text-slate-500">₹10 / Crore</p>
          </div>
          <div className="bg-slate-950/70 p-3 rounded-lg border border-slate-800 hover:border-slate-700/60 transition-all">
            <p className="text-slate-400 font-sans text-[11px]">Stamp Duty</p>
            <p className="font-bold text-white mt-1">0.003%</p>
            <p className="text-[9px] text-slate-500">Buy turnover only</p>
          </div>
          <div className="bg-slate-950/70 p-3 rounded-lg border border-slate-800 hover:border-slate-700/60 transition-all">
            <p className="text-slate-400 font-sans text-[11px]">GST</p>
            <p className="font-bold text-white mt-1">18%</p>
            <p className="text-[9px] text-slate-500">On fees &amp; brokerage</p>
          </div>
        </div>
      </div>

      {/* Trade History Audit Trail */}
      <div className="bg-slate-900/60 border border-slate-800 rounded-2xl overflow-hidden shadow-xl backdrop-blur-sm">
        <div className="p-4 sm:p-5 border-b border-slate-800 flex items-center justify-between">
          <h3 className="text-base font-bold text-white">Execution Audit Log &amp; Settle Records</h3>
          {analytics && analytics.allTradesHistory.length > 0 && (
            <span className="text-xs text-slate-400 font-mono">
              Total Trades: {analytics.allTradesHistory.length}
            </span>
          )}
        </div>
        {analytics && analytics.allTradesHistory.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs sm:text-sm font-mono">
              <thead className="bg-slate-950/80 text-[11px] uppercase tracking-wider text-slate-400 border-b border-slate-800">
                <tr>
                  <th className="py-3 px-4">Symbol</th>
                  <th className="py-3 px-4">Qty</th>
                  <th className="py-3 px-4">Buy Price</th>
                  <th className="py-3 px-4">Sell Price</th>
                  <th className="py-3 px-4">Gross P&amp;L</th>
                  <th className="py-3 px-4">Taxes Paid</th>
                  <th className="py-3 px-4">Net Take-Home</th>
                  <th className="py-3 px-4">Exit Reason</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/60">
                {analytics.allTradesHistory.map((t) => (
                  <tr
                    key={t.id}
                    className="hover:bg-slate-800/40 transition-colors cursor-pointer"
                  >
                    <td className="py-3 px-4 font-bold text-white font-sans">{t.symbol}</td>
                    <td className="py-3 px-4">{t.quantity}</td>
                    <td className="py-3 px-4">₹{t.buyPrice.toFixed(2)}</td>
                    <td className="py-3 px-4">₹{t.sellPrice.toFixed(2)}</td>
                    <td
                      className={`py-3 px-4 font-bold ${
                        t.grossPnL >= 0 ? "text-emerald-400" : "text-rose-400"
                      }`}
                    >
                      {t.grossPnL >= 0 ? "+" : ""}₹{t.grossPnL.toFixed(2)}
                    </td>
                    <td className="py-3 px-4 text-rose-400">-₹{t.totalCharges.toFixed(2)}</td>
                    <td
                      className={`py-3 px-4 font-black ${
                        t.netPnL >= 0 ? "text-emerald-400" : "text-rose-400"
                      }`}
                    >
                      {t.netPnL >= 0 ? "+" : ""}₹{t.netPnL.toFixed(2)}
                    </td>
                    <td className="py-3 px-4 text-slate-400 text-xs font-sans">{t.exitReason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="p-8 text-center text-slate-500 text-sm">
            No completed trades recorded yet. Run a simulated or live session to see the settlement log.
          </div>
        )}
      </div>
    </div>
  );
}

export default AnalyticsTab;
