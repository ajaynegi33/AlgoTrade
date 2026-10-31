package monitoring

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GenerateHTMLReport produces an email-friendly, responsive HTML report
func GenerateHTMLReport(summary *EODReportSummary) string {
	pnlColor := "#10b981"
	if summary.RealisedNetPnL < 0 {
		pnlColor = "#ef4444"
	}

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>EOD Diagnostic Report</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background-color: #0f172a; color: #f8fafc; margin: 0; padding: 24px; line-height: 1.5; }
  .container { max-width: 960px; margin: 0 auto; background-color: #1e293b; border-radius: 12px; border: 1px solid #334155; padding: 32px; }
  h1 { font-size: 24px; font-weight: 700; margin-top: 0; color: #38bdf8; display: flex; align-items: center; justify-content: space-between; }
  .badge-date { font-size: 14px; font-weight: 500; background-color: #0f172a; padding: 4px 12px; border-radius: 6px; border: 1px solid #475569; color: #94a3b8; }
  .kpi-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 16px; margin: 24px 0; }
  .kpi-card { background-color: #0f172a; border: 1px solid #334155; border-radius: 8px; padding: 16px; }
  .kpi-title { font-size: 12px; font-weight: 600; text-transform: uppercase; color: #94a3b8; letter-spacing: 0.05em; }
  .kpi-value { font-size: 24px; font-weight: 700; margin-top: 6px; }
  .kpi-sub { font-size: 12px; color: #64748b; margin-top: 4px; }
  .section-title { font-size: 18px; font-weight: 600; color: #e2e8f0; margin-top: 32px; margin-bottom: 12px; border-bottom: 1px solid #334155; padding-bottom: 8px; }
  .alert-box { padding: 14px 18px; border-radius: 8px; margin: 16px 0; font-size: 14px; font-weight: 500; }
  .alert-warning { background-color: rgba(245, 158, 11, 0.15); border: 1px solid #f59e0b; color: #fbbf24; }
  .alert-danger { background-color: rgba(239, 68, 68, 0.15); border: 1px solid #ef4444; color: #f87171; }
  .alert-info { background-color: rgba(56, 189, 248, 0.15); border: 1px solid #38bdf8; color: #7dd3fc; }
  table { width: 100%; border-collapse: collapse; margin: 16px 0; font-size: 13px; }
  th { text-align: left; padding: 10px 12px; background-color: #0f172a; color: #94a3b8; font-weight: 600; border-bottom: 2px solid #334155; }
  td { padding: 10px 12px; border-bottom: 1px solid #334155; color: #cbd5e1; }
  tr:hover td { background-color: #273549; }
  .text-right { text-align: right; }
  .text-center { text-align: center; }
  .pill-win { background-color: #065f46; color: #34d399; padding: 2px 8px; border-radius: 4px; font-weight: 600; font-size: 11px; }
  .pill-loss { background-color: #7f1d1d; color: #f87171; padding: 2px 8px; border-radius: 4px; font-weight: 600; font-size: 11px; }
  .pill-toxic { background-color: #7f1d1d; color: #fca5a5; padding: 2px 8px; border-radius: 4px; font-weight: 700; font-size: 11px; }
  .footer { margin-top: 40px; font-size: 12px; color: #64748b; text-align: center; border-top: 1px solid #334155; padding-top: 20px; }
</style>
</head>
<body>
<div class="container">
`)

	fmt.Fprintf(&sb, `  <h1>
    <span>AlgoTrade EOD Diagnostic Report</span>
    <span class="badge-date">%s</span>
  </h1>
  <p style="color: #94a3b8; margin-top: -8px; font-size: 14px;">Automated Intraday Optimization & Attribution Ledger • Generated at %s</p>

  <!-- 1. Executive Performance KPIs -->
  <div class="kpi-grid">
    <div class="kpi-card">
      <div class="kpi-title">Realised Net PnL</div>
      <div class="kpi-value" style="color: %s">₹%.2f</div>
      <div class="kpi-sub">Gross: ₹%.2f | Tax: ₹%.2f</div>
    </div>
    <div class="kpi-card">
      <div class="kpi-title">Win Rate</div>
      <div class="kpi-value" style="color: #38bdf8;">%.1f%%</div>
      <div class="kpi-sub">%d Won • %d Lost • %d BE</div>
    </div>
    <div class="kpi-card">
      <div class="kpi-title">Profit Factor</div>
      <div class="kpi-value">%.2f</div>
      <div class="kpi-sub">Average R: %+.2fR</div>
    </div>
    <div class="kpi-card">
      <div class="kpi-title">Edge Ratio (MFE/MAE)</div>
      <div class="kpi-value" style="color: #a78bfa;">%.2fx</div>
      <div class="kpi-sub">Capture: %.1f%%</div>
    </div>
  </div>
`,
		summary.TradingDate,
		summary.GeneratedAt.Format("15:04:05 IST"),
		pnlColor, summary.RealisedNetPnL,
		summary.RealisedGrossPnL, summary.TotalCharges,
		summary.WinRatePct,
		summary.WinningTrades, summary.LosingTrades, summary.BreakevenTrades,
		summary.ProfitFactor,
		summary.AverageRMultiple,
		summary.ExcursionSummary.EdgeRatio,
		summary.ExcursionSummary.CaptureEfficiency,
	)

	// 2. Stop Loss & Trailing Optimization Alerts
	if summary.ExcursionSummary.StopLossAlert != "" {
		fmt.Fprintf(&sb, `<div class="alert-box alert-warning"><strong>Stop Loss Width Optimization:</strong> %s</div>`, summary.ExcursionSummary.StopLossAlert)
	}
	if summary.ExcursionSummary.TrailingStopAlert != "" {
		fmt.Fprintf(&sb, `<div class="alert-box alert-warning"><strong>Trailing Stop Optimization:</strong> %s</div>`, summary.ExcursionSummary.TrailingStopAlert)
	}
	if summary.ShadowSummary.FilterEfficiencyAlert != "" {
		fmt.Fprintf(&sb, `<div class="alert-box alert-danger"><strong>Shadow Signals Warning:</strong> %s</div>`, summary.ShadowSummary.FilterEfficiencyAlert)
	}

	// 3. MFE vs MAE Diagnostic Table
	fmt.Fprintf(&sb, `
  <div class="section-title">1. MFE vs. MAE Excursion Analysis</div>
  <table>
    <thead>
      <tr>
        <th>Metric</th>
        <th>Value</th>
        <th>Diagnostic Meaning</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <td><strong>Average MFE (Peak Profit)</strong></td>
        <td>%+.2fR</td>
        <td>Average highest unrealized profit reached before exit</td>
      </tr>
      <tr>
        <td><strong>Average MAE (Max Drawdown)</strong></td>
        <td>%+.2fR</td>
        <td>Average deepest adverse price dip during trade lifetime</td>
      </tr>
      <tr>
        <td><strong>Median MAE of Winning Trades</strong></td>
        <td>%+.2fR</td>
        <td>Benchmark for how far winners drop before becoming profitable</td>
      </tr>
      <tr>
        <td><strong>Winners Staying Above -0.6R</strong></td>
        <td>%.1f%%</td>
        <td>If >85%%, initial stops are wider than required by true market volatility</td>
      </tr>
      <tr>
        <td><strong>Exit Capture Efficiency</strong></td>
        <td>%.1f%%</td>
        <td>Realized R vs. Peak MFE (Higher is better; &lt;40%% indicates loose trailing)</td>
      </tr>
    </tbody>
  </table>
`,
		summary.ExcursionSummary.AverageMFE,
		summary.ExcursionSummary.AverageMAE,
		summary.ExcursionSummary.MedianMAEWinners,
		summary.ExcursionSummary.PctWinnersUnder06R,
		summary.ExcursionSummary.CaptureEfficiency,
	)

	// 4. Shadow Tracking of Rejected Trades
	fmt.Fprintf(&sb, `
  <div class="section-title">2. Shadow Tracking (Actual vs. Rejected Candidates)</div>
  <table>
    <thead>
      <tr>
        <th>Execution Stream</th>
        <th class="text-center">Count</th>
        <th class="text-center">Win Rate</th>
        <th class="text-right">Avg R</th>
        <th class="text-right">Net PnL</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <td><strong>Actual Executed Trades</strong></td>
        <td class="text-center">%d</td>
        <td class="text-center">%.1f%%</td>
        <td class="text-right">%+.2fR</td>
        <td class="text-right" style="color: %s">₹%.2f</td>
      </tr>
      <tr>
        <td><strong>Shadow (Rejected Setups)</strong></td>
        <td class="text-center">%d</td>
        <td class="text-center">%.1f%%</td>
        <td class="text-right">%+.2fR</td>
        <td class="text-right">Simulated 1R Target</td>
      </tr>
    </tbody>
  </table>
`,
		summary.ShadowSummary.ActualTradeCount,
		summary.ShadowSummary.ActualWinRatePct,
		summary.ShadowSummary.ActualAverageR,
		pnlColor, summary.ShadowSummary.ActualTotalNetPnL,
		summary.ShadowSummary.ShadowSignalCount,
		summary.ShadowSummary.ShadowWinRatePct,
		summary.ShadowSummary.ShadowAverageR,
	)

	// 5. Slippage & Friction Audit
	sb.WriteString(`
  <div class="section-title">3. Slippage & Friction Audit</div>
`)
	if len(summary.ToxicSymbols) > 0 {
		fmt.Fprintf(&sb, `<div class="alert-box alert-danger"><strong>Toxic Illiquidity Detected:</strong> %s exceeded the 0.15%% average slippage limit. Immediate blacklisting recommended.</div>`, strings.Join(summary.ToxicSymbols, ", "))
	}
	sb.WriteString(`
  <table>
    <thead>
      <tr>
        <th>Symbol</th>
        <th class="text-center">Trades</th>
        <th class="text-right">Avg Intended Price</th>
        <th class="text-right">Avg Fill Price</th>
        <th class="text-right">Avg Slippage %</th>
        <th class="text-right">Max Slippage %</th>
        <th class="text-center">Status</th>
      </tr>
    </thead>
    <tbody>
`)
	if len(summary.SlippageRecords) == 0 {
		sb.WriteString(`<tr><td colspan="7" class="text-center" style="color: #64748b;">No trades executed today</td></tr>`)
	} else {
		for _, slippageRecord := range summary.SlippageRecords {
			statusPill := `<span class="pill-win">Clean</span>`
			if slippageRecord.IsToxicLiquidity {
				statusPill = `<span class="pill-toxic">TOXIC ILLIQUID</span>`
			}
			fmt.Fprintf(&sb, `
      <tr>
        <td><strong>%s</strong></td>
        <td class="text-center">%d</td>
        <td class="text-right">₹%.2f</td>
        <td class="text-right">₹%.2f</td>
        <td class="text-right">%+.3f%%</td>
        <td class="text-right">%+.3f%%</td>
        <td class="text-center">%s</td>
      </tr>`,
				slippageRecord.Symbol, slippageRecord.TradeCount, slippageRecord.AvgIntendedPrice, slippageRecord.AvgFillPrice,
				slippageRecord.AvgSlippagePct, slippageRecord.MaxSlippagePct, statusPill,
			)
		}
	}
	sb.WriteString(`
    </tbody>
  </table>
`)

	// 6. Probability Calibration Matrix
	sb.WriteString(`
  <div class="section-title">4. Probability Calibration Table</div>
  <table>
    <thead>
      <tr>
        <th>Confluence Bucket</th>
        <th class="text-center">Trades</th>
        <th class="text-center">Win Rate</th>
        <th class="text-right">Avg R</th>
        <th class="text-right">Net PnL</th>
      </tr>
    </thead>
    <tbody>
`)
	for _, probabilityBucket := range summary.ProbabilityBuckets {
		pColor := "#94a3b8"
		if probabilityBucket.NetPnL > 0 {
			pColor = "#10b981"
		} else if probabilityBucket.NetPnL < 0 {
			pColor = "#ef4444"
		}
		fmt.Fprintf(&sb, `
      <tr>
        <td><strong>%s</strong></td>
        <td class="text-center">%d</td>
        <td class="text-center">%.1f%%</td>
        <td class="text-right">%+.2fR</td>
        <td class="text-right" style="color: %s">₹%.2f</td>
      </tr>`,
			probabilityBucket.BucketName, probabilityBucket.TradeCount, probabilityBucket.WinRatePct, probabilityBucket.AverageR, pColor, probabilityBucket.NetPnL,
		)
	}
	sb.WriteString(`
    </tbody>
  </table>
`)

	// 7. Time of Day Performance
	sb.WriteString(`
  <div class="section-title">5. Intraday Session Breakdown</div>
  <table>
    <thead>
      <tr>
        <th>Session Window</th>
        <th>Hours</th>
        <th class="text-center">Trades</th>
        <th class="text-center">Win Rate</th>
        <th class="text-right">Avg R</th>
        <th class="text-right">Net PnL</th>
      </tr>
    </thead>
    <tbody>
`)
	for _, sessionBreakdown := range summary.SessionBreakdowns {
		sColor := "#94a3b8"
		if sessionBreakdown.NetPnL > 0 {
			sColor = "#10b981"
		} else if sessionBreakdown.NetPnL < 0 {
			sColor = "#ef4444"
		}
		fmt.Fprintf(&sb, `
      <tr>
        <td><strong>%s</strong></td>
        <td>%s</td>
        <td class="text-center">%d</td>
        <td class="text-center">%.1f%%</td>
        <td class="text-right">%+.2fR</td>
        <td class="text-right" style="color: %s">₹%.2f</td>
      </tr>`,
			sessionBreakdown.SessionName, sessionBreakdown.TimeWindow, sessionBreakdown.TradeCount, sessionBreakdown.WinRatePct, sessionBreakdown.AverageR, sColor, sessionBreakdown.NetPnL,
		)
	}
	sb.WriteString(`
    </tbody>
  </table>
`)

	// 8. Trailing Stop Gear Attribution
	sb.WriteString(`
  <div class="section-title">6. Trailing Stop Gear Attribution</div>
  <table>
    <thead>
      <tr>
        <th>Gear Level</th>
        <th class="text-center">Positions Reached</th>
        <th class="text-right">Banked Net PnL</th>
      </tr>
    </thead>
    <tbody>
`)
	for _, gearAttribution := range summary.GearAttributions {
		gColor := "#94a3b8"
		if gearAttribution.BankedPnL > 0 {
			gColor = "#10b981"
		} else if gearAttribution.BankedPnL < 0 {
			gColor = "#ef4444"
		}
		fmt.Fprintf(&sb, `
      <tr>
        <td><strong>%s</strong></td>
        <td class="text-center">%d</td>
        <td class="text-right" style="color: %s">₹%.2f</td>
      </tr>`,
			gearAttribution.GearLabel, gearAttribution.TradesCount, gColor, gearAttribution.BankedPnL,
		)
	}
	sb.WriteString(`
    </tbody>
  </table>
`)

	// 9. Detailed Trade Ledger
	sb.WriteString(`
  <div class="section-title">7. Complete Trade Ledger</div>
  <table>
    <thead>
      <tr>
        <th>Symbol</th>
        <th>Pattern</th>
        <th class="text-right">Entry</th>
        <th class="text-right">Exit</th>
        <th class="text-right">1R (₹)</th>
        <th class="text-right">MFE</th>
        <th class="text-right">MAE</th>
        <th class="text-right">Realised R</th>
        <th class="text-right">Net PnL</th>
        <th>Exit Reason</th>
      </tr>
    </thead>
    <tbody>
`)
	if len(summary.TradeLedger) == 0 {
		sb.WriteString(`<tr><td colspan="10" class="text-center" style="color: #64748b;">No trades executed today</td></tr>`)
	} else {
		for _, tradeRecord := range summary.TradeLedger {
			pill := `<span class="pill-win">WIN</span>`
			tColor := "#10b981"
			if !tradeRecord.IsWinner {
				pill = `<span class="pill-loss">LOSS</span>`
				tColor = "#ef4444"
			}
			fmt.Fprintf(&sb, `
      <tr>
        <td><strong>%s</strong> %s</td>
        <td>%s</td>
        <td class="text-right">₹%.2f</td>
        <td class="text-right">₹%.2f</td>
        <td class="text-right">₹%.2f</td>
        <td class="text-right">%+.2fR</td>
        <td class="text-right">%+.2fR</td>
        <td class="text-right">%+.2fR</td>
        <td class="text-right" style="color: %s; font-weight: 600;">₹%.2f</td>
        <td style="font-size: 11px; color: #94a3b8;">%s</td>
      </tr>`,
				tradeRecord.Symbol, pill, tradeRecord.PatternType, tradeRecord.EntryPrice, tradeRecord.ExitPrice, tradeRecord.OneR,
				tradeRecord.MFERMultiple, tradeRecord.MAERMultiple, tradeRecord.RealisedRMultiple,
				tColor, tradeRecord.RealisedNetPnL, tradeRecord.ExitReason,
			)
		}
	}
	sb.WriteString(`
    </tbody>
  </table>

  <div class="footer">
    AlgoTrade High-Frequency Quantitative Execution Engine • Confidential Diagnostic Report
  </div>
</div>
</body>
</html>`)

	return sb.String()
}

// GenerateMarkdownReport produces a GitHub-Flavored Markdown summary
func GenerateMarkdownReport(summary *EODReportSummary) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# AlgoTrade EOD Diagnostic Report — %s\n\n", summary.TradingDate)
	fmt.Fprintf(&sb, "*Generated at %s*\n\n", summary.GeneratedAt.Format("2006-01-02 15:04:05 IST"))

	// Key Metrics Table
	sb.WriteString("## Executive Performance Summary\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|---|---|\n")
	fmt.Fprintf(&sb, "| **Realised Net PnL** | **₹%.2f** |\n", summary.RealisedNetPnL)
	fmt.Fprintf(&sb, "| Realised Gross PnL | ₹%.2f |\n", summary.RealisedGrossPnL)
	fmt.Fprintf(&sb, "| Total Statutory Charges & Brokerage | ₹%.2f |\n", summary.TotalCharges)
	fmt.Fprintf(&sb, "| Win Rate | %.1f%% (%d Won / %d Lost / %d BE) |\n", summary.WinRatePct, summary.WinningTrades, summary.LosingTrades, summary.BreakevenTrades)
	fmt.Fprintf(&sb, "| Profit Factor | %.2f |\n", summary.ProfitFactor)
	fmt.Fprintf(&sb, "| Average Realised R | %+.2fR |\n", summary.AverageRMultiple)
	fmt.Fprintf(&sb, "| Edge Ratio (Avg MFE / |MAE|) | %.2fx |\n", summary.ExcursionSummary.EdgeRatio)
	fmt.Fprintf(&sb, "| Exit Capture Efficiency | %.1f%% |\n\n", summary.ExcursionSummary.CaptureEfficiency)

	// Diagnostic Alerts
	if summary.ExcursionSummary.StopLossAlert != "" {
		fmt.Fprintf(&sb, "> [!WARNING]\n> **Stop Loss Width**: %s\n\n", summary.ExcursionSummary.StopLossAlert)
	}
	if summary.ExcursionSummary.TrailingStopAlert != "" {
		fmt.Fprintf(&sb, "> [!WARNING]\n> **Trailing Stop Width**: %s\n\n", summary.ExcursionSummary.TrailingStopAlert)
	}
	if summary.ShadowSummary.FilterEfficiencyAlert != "" {
		fmt.Fprintf(&sb, "> [!CAUTION]\n> **Filter Efficiency**: %s\n\n", summary.ShadowSummary.FilterEfficiencyAlert)
	}

	// MFE vs MAE
	sb.WriteString("## 1. MFE vs. MAE Excursion Breakdown\n\n")
	sb.WriteString("| Metric | Value | Diagnostic Insight |\n")
	sb.WriteString("|---|---|---|\n")
	fmt.Fprintf(&sb, "| Average MFE | %+.2fR | Peak favorable run before exit |\n", summary.ExcursionSummary.AverageMFE)
	fmt.Fprintf(&sb, "| Average MAE | %+.2fR | Deepest adverse dip during trade |\n", summary.ExcursionSummary.AverageMAE)
	fmt.Fprintf(&sb, "| Median MAE of Winners | %+.2fR | Benchmark drawdown on winning trades |\n", summary.ExcursionSummary.MedianMAEWinners)
	fmt.Fprintf(&sb, "| Winners Staying Above -0.6R | %.1f%% | Target >85%% for tight stop optimization |\n\n", summary.ExcursionSummary.PctWinnersUnder06R)

	// Shadow Tracking
	sb.WriteString("## 2. Shadow Tracking (Actual vs. Rejected Candidates)\n\n")
	sb.WriteString("| Stream | Trades | Win Rate | Average R | Total Net PnL |\n")
	sb.WriteString("|---|---|---|---|---|\n")
	fmt.Fprintf(&sb, "| **Actual Trades** | %d | %.1f%% | %+.2fR | ₹%.2f |\n", summary.ShadowSummary.ActualTradeCount, summary.ShadowSummary.ActualWinRatePct, summary.ShadowSummary.ActualAverageR, summary.ShadowSummary.ActualTotalNetPnL)
	fmt.Fprintf(&sb, "| **Shadow Setups** | %d | %.1f%% | %+.2fR | Simulated 1R |\n\n", summary.ShadowSummary.ShadowSignalCount, summary.ShadowSummary.ShadowWinRatePct, summary.ShadowSummary.ShadowAverageR)

	// Slippage Audit
	sb.WriteString("## 3. Slippage & Friction Audit\n\n")
	if len(summary.ToxicSymbols) > 0 {
		fmt.Fprintf(&sb, "> [!CAUTION]\n> **Toxic Illiquid Stocks Flagged for Blacklist**: %s\n\n", strings.Join(summary.ToxicSymbols, ", "))
	}
	sb.WriteString("| Symbol | Trades | Avg Intended | Avg Fill | Avg Slippage | Max Slippage | Status |\n")
	sb.WriteString("|---|---|---|---|---|---|---|\n")
	if len(summary.SlippageRecords) == 0 {
		sb.WriteString("| *None* | 0 | ₹0.00 | ₹0.00 | 0.000% | 0.000% | Clean |\n\n")
	} else {
		for _, slippageRecord := range summary.SlippageRecords {
			status := "Clean"
			if slippageRecord.IsToxicLiquidity {
				status = "**TOXIC**"
			}
			fmt.Fprintf(&sb, "| %s | %d | ₹%.2f | ₹%.2f | %+.3f%% | %+.3f%% | %s |\n",
				slippageRecord.Symbol, slippageRecord.TradeCount, slippageRecord.AvgIntendedPrice, slippageRecord.AvgFillPrice, slippageRecord.AvgSlippagePct, slippageRecord.MaxSlippagePct, status)
		}
		sb.WriteString("\n")
	}

	// Probability Calibration
	sb.WriteString("## 4. Probability Calibration Table\n\n")
	sb.WriteString("| Score Bucket | Trades | Win Rate | Average R | Net PnL |\n")
	sb.WriteString("|---|---|---|---|---|\n")
	for _, probabilityBucket := range summary.ProbabilityBuckets {
		fmt.Fprintf(&sb, "| %s | %d | %.1f%% | %+.2fR | ₹%.2f |\n", probabilityBucket.BucketName, probabilityBucket.TradeCount, probabilityBucket.WinRatePct, probabilityBucket.AverageR, probabilityBucket.NetPnL)
	}
	sb.WriteString("\n")

	// Session Breakdown
	sb.WriteString("## 5. Intraday Session Breakdown\n\n")
	sb.WriteString("| Session | Hours | Trades | Win Rate | Average R | Net PnL |\n")
	sb.WriteString("|---|---|---|---|---|---|\n")
	for _, sessionBreakdown := range summary.SessionBreakdowns {
		fmt.Fprintf(&sb, "| %s | %s | %d | %.1f%% | %+.2fR | ₹%.2f |\n", sessionBreakdown.SessionName, sessionBreakdown.TimeWindow, sessionBreakdown.TradeCount, sessionBreakdown.WinRatePct, sessionBreakdown.AverageR, sessionBreakdown.NetPnL)
	}
	sb.WriteString("\n")

	// Gear Attribution
	sb.WriteString("## 6. Trailing Stop Gear Attribution\n\n")
	sb.WriteString("| Gear Level | Positions Reached | Banked Net PnL |\n")
	sb.WriteString("|---|---|---|\n")
	for _, gearAttribution := range summary.GearAttributions {
		fmt.Fprintf(&sb, "| %s | %d | ₹%.2f |\n", gearAttribution.GearLabel, gearAttribution.TradesCount, gearAttribution.BankedPnL)
	}
	sb.WriteString("\n")

	// Trade Ledger
	sb.WriteString("## 7. Trade Ledger\n\n")
	sb.WriteString("| Symbol | Pattern | Entry | Exit | 1R | MFE | MAE | Realised R | Net PnL | Exit Reason |\n")
	sb.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	if len(summary.TradeLedger) == 0 {
		sb.WriteString("| *No trades closed today* | - | - | - | - | - | - | - | - | - |\n")
	} else {
		for _, tradeRecord := range summary.TradeLedger {
			fmt.Fprintf(&sb, "| %s | %s | ₹%.2f | ₹%.2f | ₹%.2f | %+.2fR | %+.2fR | %+.2fR | ₹%.2f | %s |\n",
				tradeRecord.Symbol, tradeRecord.PatternType, tradeRecord.EntryPrice, tradeRecord.ExitPrice, tradeRecord.OneR, tradeRecord.MFERMultiple, tradeRecord.MAERMultiple, tradeRecord.RealisedRMultiple, tradeRecord.RealisedNetPnL, tradeRecord.ExitReason)
		}
	}
	sb.WriteString("\n")

	return sb.String()
}

// SaveReportFiles writes both HTML and Markdown reports to disk and records them in SQLite
func SaveReportFiles(db *sql.DB, summary *EODReportSummary, reportsDir string) (string, string, error) {
	if reportsDir == "" {
		reportsDir = "reports"
	}
	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create reports directory %s: %w", reportsDir, err)
	}

	htmlContent := GenerateHTMLReport(summary)
	mdContent := GenerateMarkdownReport(summary)

	htmlPath := filepath.Join(reportsDir, fmt.Sprintf("eod_%s.html", summary.TradingDate))
	mdPath := filepath.Join(reportsDir, fmt.Sprintf("eod_%s.md", summary.TradingDate))

	if err := os.WriteFile(htmlPath, []byte(htmlContent), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write HTML report: %w", err)
	}
	if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write Markdown report: %w", err)
	}

	if db != nil {
		summaryJSON, _ := json.Marshal(summary)
		_, _ = db.Exec(`
			INSERT INTO daily_eod_reports (
				trading_date, report_markdown, report_html, summary_json, is_email_sent, email_recipient, created_at
			) VALUES (?, ?, ?, ?, 0, '', CURRENT_TIMESTAMP)
			ON CONFLICT(trading_date) DO UPDATE SET
				report_markdown = excluded.report_markdown,
				report_html = excluded.report_html,
				summary_json = excluded.summary_json;
		`, summary.TradingDate, mdContent, htmlContent, string(summaryJSON))
	}

	return htmlPath, mdPath, nil
}
