package monitoring

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GenerateHTMLReport produces an institutional, email-client resilient, mobile-responsive HTML report
func GenerateHTMLReport(summary *EODReportSummary) string {
	if summary.TotalTrades == 0 {
		return renderZeroTradeHTML(summary)
	}
	return renderActiveTradeHTML(summary)
}

// GenerateMarkdownReport produces a GitHub-Flavored Markdown summary
func GenerateMarkdownReport(summary *EODReportSummary) string {
	if summary.TotalTrades == 0 {
		return renderZeroTradeMarkdown(summary)
	}
	return renderActiveTradeMarkdown(summary)
}

// renderZeroTradeHTML renders an executive Capital Preservation & Pre-Trade Gate Audit report
func renderZeroTradeHTML(summary *EODReportSummary) string {
	var builder strings.Builder

	executionMode := summary.ExecutionMode
	if executionMode == "" {
		executionMode = "PAPER"
	}

	autoExecuteLabel := `<span style="color: #059669; font-weight: 700;">● ARMED (Active)</span>`
	if !summary.IsAutoExecuteArmed {
		autoExecuteLabel = `<span style="color: #d97706; font-weight: 700;">● PAUSED</span>`
	}

	circuitBreakerLabel := `<span style="color: #059669; font-weight: 700;">NOMINAL (Pass)</span>`
	if summary.IsCircuitBreakerLocked {
		circuitBreakerLabel = fmt.Sprintf(`<span style="color: #dc2626; font-weight: 700;">LOCKED (%s)</span>`, summary.CircuitBreakerReason)
	}

	verdictText := summary.ZeroTradeVerdict
	if verdictText == "" {
		verdictText = "0 trades executed — system preserved 100% capital in accordance with quantitative risk criteria."
	}

	builder.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AlgoTrade EOD Diagnostic Report</title>
<style>
  body { margin: 0; padding: 16px; background-color: #f8fafc; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; color: #0f172a; -webkit-font-smoothing: antialiased; }
  .email-container { width: 100%; max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; border: 1px solid #e2e8f0; overflow: hidden; box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05); }
  .table-scroll { width: 100%; overflow-x: auto; -webkit-overflow-scrolling: touch; margin: 16px 0; border: 1px solid #e2e8f0; border-radius: 8px; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th { background-color: #f8fafc; color: #475569; font-weight: 600; padding: 10px 12px; border-bottom: 1px solid #e2e8f0; text-transform: uppercase; font-size: 10px; letter-spacing: 0.05em; text-align: left; }
  td { padding: 10px 12px; border-bottom: 1px solid #f1f5f9; color: #1e293b; }
  tr:last-child td { border-bottom: none; }
  .font-mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-variant-numeric: tabular-nums; }
  @media only screen and (max-width: 600px) {
    body { padding: 8px; }
    .header-box { padding: 16px !important; }
    .body-box { padding: 16px !important; }
  }
</style>
</head>
<body style="margin: 0; padding: 16px; background-color: #f8fafc; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; color: #0f172a;">
<div class="email-container" style="width: 100%; max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; border: 1px solid #e2e8f0; overflow: hidden;">
`)

	// Header
	fmt.Fprintf(&builder, `
  <div class="header-box" style="padding: 24px 24px 18px 24px; border-bottom: 1px solid #f1f5f9; background-color: #ffffff;">
    <table style="width: 100%%; border-collapse: collapse;">
      <tr>
        <td style="padding: 0; vertical-align: top;">
          <div style="font-size: 18px; font-weight: 800; color: #0f172a; letter-spacing: -0.02em;">AlgoTrade EOD Report</div>
          <div style="font-size: 12px; color: #64748b; margin-top: 3px;">Capital Preservation & Pre-Trade Gate Audit</div>
        </td>
        <td style="padding: 0; vertical-align: top; text-align: right;">
          <span style="display: inline-block; padding: 3px 8px; border-radius: 6px; font-size: 11px; font-weight: 600; background-color: #f1f5f9; color: #334155; border: 1px solid #e2e8f0;">%s</span>
          <div style="font-size: 11px; color: #94a3b8; margin-top: 4px;">%s</div>
        </td>
      </tr>
    </table>
  </div>
  <div class="body-box" style="padding: 24px;">
`,
		summary.TradingDate,
		summary.GeneratedAt.Format("15:04:05 IST"),
	)

	// Capital Preservation Status Banner
	builder.WriteString(`
    <!-- Capital Preservation Banner -->
    <div style="background-color: #ecfdf5; border: 1px solid #a7f3d0; border-radius: 8px; padding: 16px 20px; margin-bottom: 20px;">
      <div style="font-size: 11px; font-weight: 700; text-transform: uppercase; color: #047857; letter-spacing: 0.05em;">Capital Preservation Status</div>
      <div style="font-size: 22px; font-weight: 800; color: #065f46; margin: 4px 0; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;">₹0.00 Drawdown • 100% Capital Preserved</div>
      <div style="font-size: 12px; color: #047857; line-height: 1.4;">Zero trades executed today. Risk boundaries were strictly enforced, avoiding market whipsaws and reserving full capital.</div>
    </div>
`)

	// Engine Health & Gate Status Checklist
	fmt.Fprintf(&builder, `
    <!-- Engine Gate Status -->
    <table style="width: 100%%; border-collapse: separate; border-spacing: 0; margin-bottom: 20px; border: 1px solid #e2e8f0; border-radius: 8px; overflow: hidden; background-color: #f8fafc;">
      <tr>
        <td style="width: 33.33%%; padding: 12px; border-right: 1px solid #e2e8f0; vertical-align: top;">
          <div style="font-size: 10px; font-weight: 700; text-transform: uppercase; color: #64748b; letter-spacing: 0.05em;">Execution Mode</div>
          <div style="font-size: 13px; font-weight: 700; color: #0f172a; margin-top: 2px;">%s</div>
          <div style="font-size: 11px; margin-top: 3px;">%s</div>
        </td>
        <td style="width: 33.33%%; padding: 12px; border-right: 1px solid #e2e8f0; vertical-align: top;">
          <div style="font-size: 10px; font-weight: 700; text-transform: uppercase; color: #64748b; letter-spacing: 0.05em;">Circuit Breaker</div>
          <div style="font-size: 12px; margin-top: 2px;">%s</div>
          <div style="font-size: 11px; color: #64748b; margin-top: 3px;">0.00%% Max Loss</div>
        </td>
        <td style="width: 33.33%%; padding: 12px; vertical-align: top;">
          <div style="font-size: 10px; font-weight: 700; text-transform: uppercase; color: #64748b; letter-spacing: 0.05em;">Candidate Setups</div>
          <div style="font-size: 13px; font-weight: 700; color: #0f172a; margin-top: 2px; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;">%d Evaluated</div>
          <div style="font-size: 11px; color: #64748b; margin-top: 3px;">%d Filtered by Gates</div>
        </td>
      </tr>
    </table>
`,
		executionMode,
		autoExecuteLabel,
		circuitBreakerLabel,
		summary.ShadowSummary.ShadowSignalCount,
		summary.ShadowSummary.ShadowSignalCount,
	)

	// Diagnostic Verdict Box
	fmt.Fprintf(&builder, `
    <!-- Diagnostic Verdict Callout -->
    <div style="background-color: #eff6ff; border-left: 4px solid #3b82f6; border-radius: 4px; padding: 14px 16px; margin-bottom: 24px;">
      <div style="font-size: 10px; font-weight: 700; text-transform: uppercase; color: #1e40af; letter-spacing: 0.05em;">Diagnostic Verdict</div>
      <div style="font-size: 13px; font-weight: 600; color: #1e293b; line-height: 1.5; margin-top: 4px;">%s</div>
    </div>
`, verdictText)

	// Filter Efficiency Alert (if applicable)
	if summary.ShadowSummary.FilterEfficiencyAlert != "" {
		fmt.Fprintf(&builder, `
    <div style="background-color: #fef2f2; border: 1px solid #fecaca; border-radius: 6px; padding: 12px 14px; margin-bottom: 20px; font-size: 12px; color: #991b1b; line-height: 1.4;">
      <strong>Pre-Trade Filter Alert:</strong> %s
    </div>
`, summary.ShadowSummary.FilterEfficiencyAlert)
	}

	// Itemized Pre-Trade Gate Audit Table
	builder.WriteString(`
    <!-- Pre-Trade Gate Audit -->
    <div style="font-size: 14px; font-weight: 700; color: #0f172a; margin-bottom: 2px;">Pre-Trade Gate Audit (Filtered Candidates)</div>
    <div style="font-size: 12px; color: #64748b; margin-bottom: 12px;">Granular post-mortem of candidates detected by pattern algorithms and why execution was withheld.</div>
`)

	if len(summary.ShadowSummary.RejectedSignals) > 0 {
		builder.WriteString(`
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th style="padding: 8px 10px; background-color: #f8fafc; color: #475569; font-size: 10px; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid #e2e8f0;">Time</th>
            <th style="padding: 8px 10px; background-color: #f8fafc; color: #475569; font-size: 10px; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid #e2e8f0;">Symbol</th>
            <th style="padding: 8px 10px; background-color: #f8fafc; color: #475569; font-size: 10px; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid #e2e8f0;">Pattern</th>
            <th style="padding: 8px 10px; background-color: #f8fafc; color: #475569; font-size: 10px; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid #e2e8f0; text-align: center;">Score</th>
            <th style="padding: 8px 10px; background-color: #f8fafc; color: #475569; font-size: 10px; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid #e2e8f0; text-align: right;">Price</th>
            <th style="padding: 8px 10px; background-color: #f8fafc; color: #475569; font-size: 10px; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid #e2e8f0;">Rejection Barrier</th>
            <th style="padding: 8px 10px; background-color: #f8fafc; color: #475569; font-size: 10px; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid #e2e8f0; text-align: center;">Simulated</th>
          </tr>
        </thead>
        <tbody>
`)
		for _, signalDetail := range summary.ShadowSummary.RejectedSignals {
			outcomeBadge := `<span style="color: #64748b; font-size: 11px;">PENDING</span>`
			if signalDetail.Outcome == "WIN_1R" || signalDetail.SimulatedR > 0 {
				outcomeBadge = fmt.Sprintf(`<span style="color: #059669; font-weight: 700; font-size: 11px; background-color: #ecfdf5; padding: 2px 6px; border-radius: 4px;">WIN (+%.2fR)</span>`, signalDetail.SimulatedR)
			} else if signalDetail.Outcome == "LOSS_1R" || signalDetail.SimulatedR < 0 {
				outcomeBadge = `<span style="color: #dc2626; font-weight: 700; font-size: 11px; background-color: #fef2f2; padding: 2px 6px; border-radius: 4px;">LOSS (-1.00R)</span>`
			}

			fmt.Fprintf(&builder, `
          <tr>
            <td style="padding: 8px 10px; border-bottom: 1px solid #f1f5f9; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 11px; color: #64748b;">%s</td>
            <td style="padding: 8px 10px; border-bottom: 1px solid #f1f5f9; font-weight: 700; color: #0f172a;">%s</td>
            <td style="padding: 8px 10px; border-bottom: 1px solid #f1f5f9; font-size: 11px; color: #475569;">%s</td>
            <td style="padding: 8px 10px; border-bottom: 1px solid #f1f5f9; text-align: center; font-weight: 600; color: #2563eb; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;">%.1f%%</td>
            <td style="padding: 8px 10px; border-bottom: 1px solid #f1f5f9; text-align: right; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; color: #0f172a;">₹%.2f</td>
            <td style="padding: 8px 10px; border-bottom: 1px solid #f1f5f9;"><span style="font-size: 11px; color: #991b1b; background-color: #fee2e2; padding: 2px 6px; border-radius: 4px; font-weight: 600;">%s</span></td>
            <td style="padding: 8px 10px; border-bottom: 1px solid #f1f5f9; text-align: center;">%s</td>
          </tr>
`,
				signalDetail.SignalTime,
				signalDetail.Symbol,
				signalDetail.PatternType,
				signalDetail.BullishScore,
				signalDetail.EntryPrice,
				signalDetail.RejectionReason,
				outcomeBadge,
			)
		}
		builder.WriteString(`
        </tbody>
      </table>
    </div>
`)
		fmt.Fprintf(&builder, `
    <div style="font-size: 11px; color: #64748b; margin-top: 6px; margin-bottom: 24px;">
      <strong>Candidate Benchmark:</strong> %d Setups Scanned • %.1f%% Simulated Win Rate • %+.2fR Avg Return
    </div>
`,
			summary.ShadowSummary.ShadowSignalCount,
			summary.ShadowSummary.ShadowWinRatePct,
			summary.ShadowSummary.ShadowAverageR,
		)
	} else {
		builder.WriteString(`
    <div style="background-color: #f8fafc; border: 1px solid #e2e8f0; border-radius: 8px; padding: 20px; text-align: center; color: #64748b; font-size: 13px; margin: 16px 0;">
      No candidate setups met technical breakout / confluence thresholds today. Market volatility was insufficient to trigger pre-trade qualification.
    </div>
`)
	}

	// Footer
	builder.WriteString(`
    <div style="border-top: 1px solid #f1f5f9; padding-top: 16px; margin-top: 24px; text-align: center; font-size: 11px; color: #94a3b8;">
      AlgoTrade High-Frequency Quantitative Execution Engine • Confidential Diagnostic Report
    </div>
  </div>
</div>
</body>
</html>`)

	return builder.String()
}

// renderActiveTradeHTML renders the standard performance audit when positions were executed
func renderActiveTradeHTML(summary *EODReportSummary) string {
	pnlColor := "#059669"
	if summary.RealisedNetPnL < 0 {
		pnlColor = "#dc2626"
	}

	var builder strings.Builder
	builder.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AlgoTrade EOD Diagnostic Report</title>
<style>
  body { margin: 0; padding: 16px; background-color: #f8fafc; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; color: #0f172a; -webkit-font-smoothing: antialiased; }
  .email-container { width: 100%; max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; border: 1px solid #e2e8f0; overflow: hidden; box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05); }
  .header-box { padding: 24px 24px 18px 24px; border-bottom: 1px solid #f1f5f9; background-color: #ffffff; }
  .body-box { padding: 24px; }
  .kpi-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; margin-bottom: 24px; }
  .kpi-card { background-color: #f8fafc; border: 1px solid #e2e8f0; border-radius: 8px; padding: 14px; }
  .kpi-title { font-size: 11px; font-weight: 700; text-transform: uppercase; color: #64748b; letter-spacing: 0.05em; }
  .kpi-value { font-size: 22px; font-weight: 800; margin-top: 4px; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-variant-numeric: tabular-nums; }
  .kpi-sub { font-size: 11px; color: #64748b; margin-top: 4px; }
  .section-title { font-size: 14px; font-weight: 700; color: #0f172a; margin-top: 28px; margin-bottom: 10px; }
  .table-scroll { width: 100%; overflow-x: auto; -webkit-overflow-scrolling: touch; margin: 12px 0; border: 1px solid #e2e8f0; border-radius: 8px; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th { background-color: #f8fafc; color: #475569; font-weight: 600; padding: 8px 10px; border-bottom: 1px solid #e2e8f0; text-transform: uppercase; font-size: 10px; letter-spacing: 0.05em; text-align: left; }
  td { padding: 8px 10px; border-bottom: 1px solid #f1f5f9; color: #1e293b; }
  tr:last-child td { border-bottom: none; }
  .text-right { text-align: right; }
  .text-center { text-align: center; }
  .font-mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-variant-numeric: tabular-nums; }
  .pill-win { background-color: #ecfdf5; color: #059669; padding: 2px 6px; border-radius: 4px; font-weight: 700; font-size: 10px; }
  .pill-loss { background-color: #fef2f2; color: #dc2626; padding: 2px 6px; border-radius: 4px; font-weight: 700; font-size: 10px; }
  .pill-toxic { background-color: #fee2e2; color: #991b1b; padding: 2px 6px; border-radius: 4px; font-weight: 700; font-size: 10px; }
  @media only screen and (max-width: 600px) {
    body { padding: 8px; }
    .header-box { padding: 16px !important; }
    .body-box { padding: 16px !important; }
    .kpi-grid { grid-template-columns: 1fr; }
  }
</style>
</head>
<body style="margin: 0; padding: 16px; background-color: #f8fafc; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; color: #0f172a;">
<div class="email-container" style="width: 100%; max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; border: 1px solid #e2e8f0; overflow: hidden;">
`)

	// Header
	fmt.Fprintf(&builder, `
  <div class="header-box" style="padding: 24px 24px 18px 24px; border-bottom: 1px solid #f1f5f9; background-color: #ffffff;">
    <table style="width: 100%%; border-collapse: collapse;">
      <tr>
        <td style="padding: 0; vertical-align: top;">
          <div style="font-size: 18px; font-weight: 800; color: #0f172a; letter-spacing: -0.02em;">AlgoTrade EOD Report</div>
          <div style="font-size: 12px; color: #64748b; margin-top: 3px;">Automated Intraday Optimization & Attribution Ledger</div>
        </td>
        <td style="padding: 0; vertical-align: top; text-align: right;">
          <span style="display: inline-block; padding: 3px 8px; border-radius: 6px; font-size: 11px; font-weight: 600; background-color: #f1f5f9; color: #334155; border: 1px solid #e2e8f0;">%s</span>
          <div style="font-size: 11px; color: #94a3b8; margin-top: 4px;">%s</div>
        </td>
      </tr>
    </table>
  </div>
  <div class="body-box" style="padding: 24px;">

    <!-- 1. Executive Performance KPIs -->
    <div class="kpi-grid">
      <div class="kpi-card" style="background-color: #f8fafc; border: 1px solid #e2e8f0; border-radius: 8px; padding: 14px;">
        <div class="kpi-title" style="font-size: 11px; font-weight: 700; text-transform: uppercase; color: #64748b;">Realised Net PnL</div>
        <div class="kpi-value" style="font-size: 22px; font-weight: 800; color: %s; margin-top: 4px;">₹%.2f</div>
        <div class="kpi-sub" style="font-size: 11px; color: #64748b; margin-top: 4px;">Gross: ₹%.2f | Tax: ₹%.2f</div>
      </div>
      <div class="kpi-card" style="background-color: #f8fafc; border: 1px solid #e2e8f0; border-radius: 8px; padding: 14px;">
        <div class="kpi-title" style="font-size: 11px; font-weight: 700; text-transform: uppercase; color: #64748b;">Win Rate</div>
        <div class="kpi-value" style="font-size: 22px; font-weight: 800; color: #2563eb; margin-top: 4px;">%.1f%%</div>
        <div class="kpi-sub" style="font-size: 11px; color: #64748b; margin-top: 4px;">%d Won • %d Lost • %d BE</div>
      </div>
      <div class="kpi-card" style="background-color: #f8fafc; border: 1px solid #e2e8f0; border-radius: 8px; padding: 14px;">
        <div class="kpi-title" style="font-size: 11px; font-weight: 700; text-transform: uppercase; color: #64748b;">Profit Factor</div>
        <div class="kpi-value" style="font-size: 22px; font-weight: 800; color: #0f172a; margin-top: 4px;">%.2f</div>
        <div class="kpi-sub" style="font-size: 11px; color: #64748b; margin-top: 4px;">Average R: %+.2fR</div>
      </div>
      <div class="kpi-card" style="background-color: #f8fafc; border: 1px solid #e2e8f0; border-radius: 8px; padding: 14px;">
        <div class="kpi-title" style="font-size: 11px; font-weight: 700; text-transform: uppercase; color: #64748b;">Edge Ratio (MFE/MAE)</div>
        <div class="kpi-value" style="font-size: 22px; font-weight: 800; color: #7c3aed; margin-top: 4px;">%.2fx</div>
        <div class="kpi-sub" style="font-size: 11px; color: #64748b; margin-top: 4px;">Capture: %.1f%%</div>
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

	// Diagnostic Alerts
	if summary.ExcursionSummary.StopLossAlert != "" {
		fmt.Fprintf(&builder, `<div style="background-color: #fffbeb; border: 1px solid #fde68a; border-radius: 6px; padding: 12px 14px; margin-bottom: 14px; font-size: 12px; color: #92400e;"><strong>Stop Loss Optimization:</strong> %s</div>`, summary.ExcursionSummary.StopLossAlert)
	}
	if summary.ExcursionSummary.TrailingStopAlert != "" {
		fmt.Fprintf(&builder, `<div style="background-color: #fffbeb; border: 1px solid #fde68a; border-radius: 6px; padding: 12px 14px; margin-bottom: 14px; font-size: 12px; color: #92400e;"><strong>Trailing Stop Optimization:</strong> %s</div>`, summary.ExcursionSummary.TrailingStopAlert)
	}
	if summary.ShadowSummary.FilterEfficiencyAlert != "" {
		fmt.Fprintf(&builder, `<div style="background-color: #fef2f2; border: 1px solid #fecaca; border-radius: 6px; padding: 12px 14px; margin-bottom: 14px; font-size: 12px; color: #991b1b;"><strong>Shadow Signals Alert:</strong> %s</div>`, summary.ShadowSummary.FilterEfficiencyAlert)
	}

	// 1. MFE vs MAE Excursion Analysis
	fmt.Fprintf(&builder, `
    <div class="section-title">1. MFE vs. MAE Excursion Analysis</div>
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Metric</th>
            <th class="text-right">Value</th>
            <th>Diagnostic Meaning</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>Average MFE (Peak Profit)</strong></td>
            <td class="text-right font-mono" style="font-weight: 700; color: #059669;">%+.2fR</td>
            <td>Highest average unrealized profit reached before exit</td>
          </tr>
          <tr>
            <td><strong>Average MAE (Max Drawdown)</strong></td>
            <td class="text-right font-mono" style="font-weight: 700; color: #dc2626;">%+.2fR</td>
            <td>Deepest adverse price excursion during trade lifetime</td>
          </tr>
          <tr>
            <td><strong>Median MAE of Winning Trades</strong></td>
            <td class="text-right font-mono">%+.2fR</td>
            <td>Benchmark for how far winners drop before becoming profitable</td>
          </tr>
          <tr>
            <td><strong>Winners Staying Above -0.6R</strong></td>
            <td class="text-right font-mono">%.1f%%</td>
            <td>If &gt;85%%, initial stops are wider than required by market volatility</td>
          </tr>
          <tr>
            <td><strong>Exit Capture Efficiency</strong></td>
            <td class="text-right font-mono">%.1f%%</td>
            <td>Realized R vs. Peak MFE (&lt;40%% indicates loose trailing stops)</td>
          </tr>
        </tbody>
      </table>
    </div>
`,
		summary.ExcursionSummary.AverageMFE,
		summary.ExcursionSummary.AverageMAE,
		summary.ExcursionSummary.MedianMAEWinners,
		summary.ExcursionSummary.PctWinnersUnder06R,
		summary.ExcursionSummary.CaptureEfficiency,
	)

	// 2. Shadow Tracking (Actual vs. Rejected Candidates)
	fmt.Fprintf(&builder, `
    <div class="section-title">2. Shadow Tracking (Actual vs. Rejected Candidates)</div>
    <div class="table-scroll">
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
            <td class="text-center font-mono">%d</td>
            <td class="text-center font-mono">%.1f%%</td>
            <td class="text-right font-mono">%+.2fR</td>
            <td class="text-right font-mono" style="color: %s; font-weight: 700;">₹%.2f</td>
          </tr>
          <tr>
            <td><strong>Shadow (Rejected Setups)</strong></td>
            <td class="text-center font-mono">%d</td>
            <td class="text-center font-mono">%.1f%%</td>
            <td class="text-right font-mono">%+.2fR</td>
            <td class="text-right" style="color: #64748b; font-size: 11px;">Simulated 1R Target</td>
          </tr>
        </tbody>
      </table>
    </div>
`,
		summary.ShadowSummary.ActualTradeCount,
		summary.ShadowSummary.ActualWinRatePct,
		summary.ShadowSummary.ActualAverageR,
		pnlColor, summary.ShadowSummary.ActualTotalNetPnL,
		summary.ShadowSummary.ShadowSignalCount,
		summary.ShadowSummary.ShadowWinRatePct,
		summary.ShadowSummary.ShadowAverageR,
	)

	// 3. Slippage & Friction Audit
	builder.WriteString(`
    <div class="section-title">3. Slippage & Friction Audit</div>
`)
	if len(summary.ToxicSymbols) > 0 {
		fmt.Fprintf(&builder, `<div style="background-color: #fee2e2; border: 1px solid #fecaca; border-radius: 6px; padding: 10px 12px; margin-bottom: 10px; font-size: 12px; color: #991b1b;"><strong>Toxic Illiquidity Detected:</strong> %s exceeded the 0.15%% average slippage limit. Immediate blacklisting recommended.</div>`, strings.Join(summary.ToxicSymbols, ", "))
	}
	builder.WriteString(`
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Symbol</th>
            <th class="text-center">Trades</th>
            <th class="text-right">Avg Intended</th>
            <th class="text-right">Avg Fill</th>
            <th class="text-right">Avg Slippage</th>
            <th class="text-right">Max Slippage</th>
            <th class="text-center">Status</th>
          </tr>
        </thead>
        <tbody>
`)
	if len(summary.SlippageRecords) == 0 {
		builder.WriteString(`<tr><td colspan="7" class="text-center" style="color: #64748b;">No trades executed today</td></tr>`)
	} else {
		for _, slippageRecord := range summary.SlippageRecords {
			statusPill := `<span class="pill-win">Clean</span>`
			if slippageRecord.IsToxicLiquidity {
				statusPill = `<span class="pill-toxic">TOXIC</span>`
			}
			fmt.Fprintf(&builder, `
          <tr>
            <td><strong>%s</strong></td>
            <td class="text-center font-mono">%d</td>
            <td class="text-right font-mono">₹%.2f</td>
            <td class="text-right font-mono">₹%.2f</td>
            <td class="text-right font-mono">%+.3f%%</td>
            <td class="text-right font-mono">%+.3f%%</td>
            <td class="text-center">%s</td>
          </tr>`,
				slippageRecord.Symbol, slippageRecord.TradeCount, slippageRecord.AvgIntendedPrice, slippageRecord.AvgFillPrice,
				slippageRecord.AvgSlippagePct, slippageRecord.MaxSlippagePct, statusPill,
			)
		}
	}
	builder.WriteString(`
        </tbody>
      </table>
    </div>
`)

	// 4. Probability Calibration Table
	builder.WriteString(`
    <div class="section-title">4. Probability Calibration Table</div>
    <div class="table-scroll">
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
		bucketColor := "#64748b"
		if probabilityBucket.NetPnL > 0 {
			bucketColor = "#059669"
		} else if probabilityBucket.NetPnL < 0 {
			bucketColor = "#dc2626"
		}
		fmt.Fprintf(&builder, `
          <tr>
            <td><strong>%s</strong></td>
            <td class="text-center font-mono">%d</td>
            <td class="text-center font-mono">%.1f%%</td>
            <td class="text-right font-mono">%+.2fR</td>
            <td class="text-right font-mono" style="color: %s; font-weight: 600;">₹%.2f</td>
          </tr>`,
			probabilityBucket.BucketName, probabilityBucket.TradeCount, probabilityBucket.WinRatePct, probabilityBucket.AverageR, bucketColor, probabilityBucket.NetPnL,
		)
	}
	builder.WriteString(`
        </tbody>
      </table>
    </div>
`)

	// 5. Intraday Session Breakdown
	builder.WriteString(`
    <div class="section-title">5. Intraday Session Breakdown</div>
    <div class="table-scroll">
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
		sessionColor := "#64748b"
		if sessionBreakdown.NetPnL > 0 {
			sessionColor = "#059669"
		} else if sessionBreakdown.NetPnL < 0 {
			sessionColor = "#dc2626"
		}
		fmt.Fprintf(&builder, `
          <tr>
            <td><strong>%s</strong></td>
            <td style="color: #64748b; font-size: 11px;">%s</td>
            <td class="text-center font-mono">%d</td>
            <td class="text-center font-mono">%.1f%%</td>
            <td class="text-right font-mono">%+.2fR</td>
            <td class="text-right font-mono" style="color: %s; font-weight: 600;">₹%.2f</td>
          </tr>`,
			sessionBreakdown.SessionName, sessionBreakdown.TimeWindow, sessionBreakdown.TradeCount, sessionBreakdown.WinRatePct, sessionBreakdown.AverageR, sessionColor, sessionBreakdown.NetPnL,
		)
	}
	builder.WriteString(`
        </tbody>
      </table>
    </div>
`)

	// 6. Trailing Stop Gear Attribution
	builder.WriteString(`
    <div class="section-title">6. Trailing Stop Gear Attribution</div>
    <div class="table-scroll">
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
		gearColor := "#64748b"
		if gearAttribution.BankedPnL > 0 {
			gearColor = "#059669"
		} else if gearAttribution.BankedPnL < 0 {
			gearColor = "#dc2626"
		}
		fmt.Fprintf(&builder, `
          <tr>
            <td><strong>%s</strong></td>
            <td class="text-center font-mono">%d</td>
            <td class="text-right font-mono" style="color: %s; font-weight: 600;">₹%.2f</td>
          </tr>`,
			gearAttribution.GearLabel, gearAttribution.TradesCount, gearColor, gearAttribution.BankedPnL,
		)
	}
	builder.WriteString(`
        </tbody>
      </table>
    </div>
`)

	// 7. Detailed Trade Ledger
	builder.WriteString(`
    <div class="section-title">7. Complete Trade Ledger</div>
    <div class="table-scroll">
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
		builder.WriteString(`<tr><td colspan="10" class="text-center" style="color: #64748b;">No trades executed today</td></tr>`)
	} else {
		for _, tradeRecord := range summary.TradeLedger {
			pill := `<span class="pill-win">WIN</span>`
			tradeColor := "#059669"
			if !tradeRecord.IsWinner {
				pill = `<span class="pill-loss">LOSS</span>`
				tradeColor = "#dc2626"
			}
			fmt.Fprintf(&builder, `
          <tr>
            <td><strong>%s</strong> %s</td>
            <td style="font-size: 11px; color: #475569;">%s</td>
            <td class="text-right font-mono">₹%.2f</td>
            <td class="text-right font-mono">₹%.2f</td>
            <td class="text-right font-mono">₹%.2f</td>
            <td class="text-right font-mono">%+.2fR</td>
            <td class="text-right font-mono">%+.2fR</td>
            <td class="text-right font-mono">%+.2fR</td>
            <td class="text-right font-mono" style="color: %s; font-weight: 700;">₹%.2f</td>
            <td style="font-size: 11px; color: #64748b;">%s</td>
          </tr>`,
				tradeRecord.Symbol, pill, tradeRecord.PatternType, tradeRecord.EntryPrice, tradeRecord.ExitPrice, tradeRecord.OneR,
				tradeRecord.MFERMultiple, tradeRecord.MAERMultiple, tradeRecord.RealisedRMultiple,
				tradeColor, tradeRecord.RealisedNetPnL, tradeRecord.ExitReason,
			)
		}
	}
	builder.WriteString(`
        </tbody>
      </table>
    </div>

    <div style="border-top: 1px solid #f1f5f9; padding-top: 16px; margin-top: 24px; text-align: center; font-size: 11px; color: #94a3b8;">
      AlgoTrade High-Frequency Quantitative Execution Engine • Confidential Diagnostic Report
    </div>
  </div>
</div>
</body>
</html>`)

	return builder.String()
}

// renderZeroTradeMarkdown renders GitHub-flavored Markdown for zero-trade sessions
func renderZeroTradeMarkdown(summary *EODReportSummary) string {
	var builder strings.Builder

	executionMode := summary.ExecutionMode
	if executionMode == "" {
		executionMode = "PAPER"
	}

	autoStatus := "ARMED"
	if !summary.IsAutoExecuteArmed {
		autoStatus = "PAUSED"
	}

	cbStatus := "NOMINAL (Pass)"
	if summary.IsCircuitBreakerLocked {
		cbStatus = fmt.Sprintf("LOCKED (%s)", summary.CircuitBreakerReason)
	}

	fmt.Fprintf(&builder, "# AlgoTrade EOD Diagnostic Report — %s\n\n", summary.TradingDate)
	fmt.Fprintf(&builder, "*Capital Preservation & Pre-Trade Gate Audit • Generated at %s*\n\n", summary.GeneratedAt.Format("2006-01-02 15:04:05 IST"))

	builder.WriteString("## Capital Preservation Status\n\n")
	builder.WriteString("| Metric | Value |\n")
	builder.WriteString("|---|---|\n")
	builder.WriteString("| **Realised Net Drawdown** | **₹0.00 (100% Preserved)** |\n")
	fmt.Fprintf(&builder, "| Execution Mode | %s (Auto-Execute: %s) |\n", executionMode, autoStatus)
	fmt.Fprintf(&builder, "| Circuit Breaker Gate | %s |\n", cbStatus)
	fmt.Fprintf(&builder, "| Candidate Setups Evaluated | %d Candidates |\n", summary.ShadowSummary.ShadowSignalCount)
	fmt.Fprintf(&builder, "| Setups Filtered by Pre-Trade Gates | %d Candidates |\n\n", summary.ShadowSummary.ShadowSignalCount)

	verdictText := summary.ZeroTradeVerdict
	if verdictText == "" {
		verdictText = "0 trades executed — system preserved 100% capital in accordance with quantitative risk criteria."
	}
	fmt.Fprintf(&builder, "> [!NOTE]\n> **Diagnostic Verdict**: %s\n\n", verdictText)

	if summary.ShadowSummary.FilterEfficiencyAlert != "" {
		fmt.Fprintf(&builder, "> [!CAUTION]\n> **Pre-Trade Filter Alert**: %s\n\n", summary.ShadowSummary.FilterEfficiencyAlert)
	}

	builder.WriteString("## Pre-Trade Gate Audit (Filtered Candidates)\n\n")
	if len(summary.ShadowSummary.RejectedSignals) > 0 {
		builder.WriteString("| Time | Symbol | Pattern | Score | Price | 1R Risk | Rejection Barrier | Outcome |\n")
		builder.WriteString("|---|---|---|---|---|---|---|---|\n")
		for _, signalDetail := range summary.ShadowSummary.RejectedSignals {
			outcome := signalDetail.Outcome
			if signalDetail.Outcome == "WIN_1R" || signalDetail.SimulatedR > 0 {
				outcome = fmt.Sprintf("WIN (+%.2fR)", signalDetail.SimulatedR)
			}
			fmt.Fprintf(&builder, "| %s | **%s** | %s | %.1f%% | ₹%.2f | ₹%.2f | `%s` | %s |\n",
				signalDetail.SignalTime, signalDetail.Symbol, signalDetail.PatternType, signalDetail.BullishScore,
				signalDetail.EntryPrice, signalDetail.OneR, signalDetail.RejectionReason, outcome)
		}
		fmt.Fprintf(&builder, "\n*Candidate Benchmark: %d Setups Scanned • %.1f%% Simulated Win Rate • %+.2fR Avg Return*\n\n",
			summary.ShadowSummary.ShadowSignalCount, summary.ShadowSummary.ShadowWinRatePct, summary.ShadowSummary.ShadowAverageR)
	} else {
		builder.WriteString("*No candidate setups met technical breakout / confluence thresholds today. Market volatility was insufficient to trigger pre-trade qualification.*\n\n")
	}

	return builder.String()
}

// renderActiveTradeMarkdown renders GitHub-flavored Markdown for sessions with executed trades
func renderActiveTradeMarkdown(summary *EODReportSummary) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# AlgoTrade EOD Diagnostic Report — %s\n\n", summary.TradingDate)
	fmt.Fprintf(&builder, "*Generated at %s*\n\n", summary.GeneratedAt.Format("2006-01-02 15:04:05 IST"))

	// Key Metrics Table
	builder.WriteString("## Executive Performance Summary\n\n")
	builder.WriteString("| Metric | Value |\n")
	builder.WriteString("|---|---|\n")
	fmt.Fprintf(&builder, "| **Realised Net PnL** | **₹%.2f** |\n", summary.RealisedNetPnL)
	fmt.Fprintf(&builder, "| Realised Gross PnL | ₹%.2f |\n", summary.RealisedGrossPnL)
	fmt.Fprintf(&builder, "| Total Statutory Charges & Brokerage | ₹%.2f |\n", summary.TotalCharges)
	fmt.Fprintf(&builder, "| Win Rate | %.1f%% (%d Won / %d Lost / %d BE) |\n", summary.WinRatePct, summary.WinningTrades, summary.LosingTrades, summary.BreakevenTrades)
	fmt.Fprintf(&builder, "| Profit Factor | %.2f |\n", summary.ProfitFactor)
	fmt.Fprintf(&builder, "| Average Realised R | %+.2fR |\n", summary.AverageRMultiple)
	fmt.Fprintf(&builder, "| Edge Ratio (Avg MFE / |MAE|) | %.2fx |\n", summary.ExcursionSummary.EdgeRatio)
	fmt.Fprintf(&builder, "| Exit Capture Efficiency | %.1f%% |\n\n", summary.ExcursionSummary.CaptureEfficiency)

	// Diagnostic Alerts
	if summary.ExcursionSummary.StopLossAlert != "" {
		fmt.Fprintf(&builder, "> [!WARNING]\n> **Stop Loss Width**: %s\n\n", summary.ExcursionSummary.StopLossAlert)
	}
	if summary.ExcursionSummary.TrailingStopAlert != "" {
		fmt.Fprintf(&builder, "> [!WARNING]\n> **Trailing Stop Width**: %s\n\n", summary.ExcursionSummary.TrailingStopAlert)
	}
	if summary.ShadowSummary.FilterEfficiencyAlert != "" {
		fmt.Fprintf(&builder, "> [!CAUTION]\n> **Filter Efficiency**: %s\n\n", summary.ShadowSummary.FilterEfficiencyAlert)
	}

	// MFE vs MAE
	builder.WriteString("## 1. MFE vs. MAE Excursion Breakdown\n\n")
	builder.WriteString("| Metric | Value | Diagnostic Insight |\n")
	builder.WriteString("|---|---|---|\n")
	fmt.Fprintf(&builder, "| Average MFE | %+.2fR | Peak favorable run before exit |\n", summary.ExcursionSummary.AverageMFE)
	fmt.Fprintf(&builder, "| Average MAE | %+.2fR | Deepest adverse dip during trade |\n", summary.ExcursionSummary.AverageMAE)
	fmt.Fprintf(&builder, "| Median MAE of Winners | %+.2fR | Benchmark drawdown on winning trades |\n", summary.ExcursionSummary.MedianMAEWinners)
	fmt.Fprintf(&builder, "| Winners Staying Above -0.6R | %.1f%% | Target >85%% for tight stop optimization |\n\n", summary.ExcursionSummary.PctWinnersUnder06R)

	// Shadow Tracking
	builder.WriteString("## 2. Shadow Tracking (Actual vs. Rejected Candidates)\n\n")
	builder.WriteString("| Stream | Trades | Win Rate | Average R | Total Net PnL |\n")
	builder.WriteString("|---|---|---|---|---|\n")
	fmt.Fprintf(&builder, "| **Actual Trades** | %d | %.1f%% | %+.2fR | ₹%.2f |\n", summary.ShadowSummary.ActualTradeCount, summary.ShadowSummary.ActualWinRatePct, summary.ShadowSummary.ActualAverageR, summary.ShadowSummary.ActualTotalNetPnL)
	fmt.Fprintf(&builder, "| **Shadow Setups** | %d | %.1f%% | %+.2fR | Simulated 1R |\n\n", summary.ShadowSummary.ShadowSignalCount, summary.ShadowSummary.ShadowWinRatePct, summary.ShadowSummary.ShadowAverageR)

	// Slippage Audit
	builder.WriteString("## 3. Slippage & Friction Audit\n\n")
	if len(summary.ToxicSymbols) > 0 {
		fmt.Fprintf(&builder, "> [!CAUTION]\n> **Toxic Illiquid Stocks Flagged for Blacklist**: %s\n\n", strings.Join(summary.ToxicSymbols, ", "))
	}
	builder.WriteString("| Symbol | Trades | Avg Intended | Avg Fill | Avg Slippage | Max Slippage | Status |\n")
	builder.WriteString("|---|---|---|---|---|---|---|\n")
	if len(summary.SlippageRecords) == 0 {
		builder.WriteString("| *None* | 0 | ₹0.00 | ₹0.00 | 0.000% | 0.000% | Clean |\n\n")
	} else {
		for _, slippageRecord := range summary.SlippageRecords {
			status := "Clean"
			if slippageRecord.IsToxicLiquidity {
				status = "**TOXIC**"
			}
			fmt.Fprintf(&builder, "| %s | %d | ₹%.2f | ₹%.2f | %+.3f%% | %+.3f%% | %s |\n",
				slippageRecord.Symbol, slippageRecord.TradeCount, slippageRecord.AvgIntendedPrice, slippageRecord.AvgFillPrice, slippageRecord.AvgSlippagePct, slippageRecord.MaxSlippagePct, status)
		}
		builder.WriteString("\n")
	}

	// Probability Calibration
	builder.WriteString("## 4. Probability Calibration Table\n\n")
	builder.WriteString("| Score Bucket | Trades | Win Rate | Average R | Net PnL |\n")
	builder.WriteString("|---|---|---|---|---|\n")
	for _, probabilityBucket := range summary.ProbabilityBuckets {
		fmt.Fprintf(&builder, "| %s | %d | %.1f%% | %+.2fR | ₹%.2f |\n", probabilityBucket.BucketName, probabilityBucket.TradeCount, probabilityBucket.WinRatePct, probabilityBucket.AverageR, probabilityBucket.NetPnL)
	}
	builder.WriteString("\n")

	// Session Breakdown
	builder.WriteString("## 5. Intraday Session Breakdown\n\n")
	builder.WriteString("| Session | Hours | Trades | Win Rate | Average R | Net PnL |\n")
	builder.WriteString("|---|---|---|---|---|---|\n")
	for _, sessionBreakdown := range summary.SessionBreakdowns {
		fmt.Fprintf(&builder, "| %s | %s | %d | %.1f%% | %+.2fR | ₹%.2f |\n", sessionBreakdown.SessionName, sessionBreakdown.TimeWindow, sessionBreakdown.TradeCount, sessionBreakdown.WinRatePct, sessionBreakdown.AverageR, sessionBreakdown.NetPnL)
	}
	builder.WriteString("\n")

	// Gear Attribution
	builder.WriteString("## 6. Trailing Stop Gear Attribution\n\n")
	builder.WriteString("| Gear Level | Positions Reached | Banked Net PnL |\n")
	builder.WriteString("|---|---|---|\n")
	for _, gearAttribution := range summary.GearAttributions {
		fmt.Fprintf(&builder, "| %s | %d | ₹%.2f |\n", gearAttribution.GearLabel, gearAttribution.TradesCount, gearAttribution.BankedPnL)
	}
	builder.WriteString("\n")

	// Trade Ledger
	builder.WriteString("## 7. Trade Ledger\n\n")
	builder.WriteString("| Symbol | Pattern | Entry | Exit | 1R | MFE | MAE | Realised R | Net PnL | Exit Reason |\n")
	builder.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	if len(summary.TradeLedger) == 0 {
		builder.WriteString("| *No trades closed today* | - | - | - | - | - | - | - | - | - |\n")
	} else {
		for _, tradeRecord := range summary.TradeLedger {
			fmt.Fprintf(&builder, "| %s | %s | ₹%.2f | ₹%.2f | ₹%.2f | %+.2fR | %+.2fR | %+.2fR | ₹%.2f | %s |\n",
				tradeRecord.Symbol, tradeRecord.PatternType, tradeRecord.EntryPrice, tradeRecord.ExitPrice, tradeRecord.OneR, tradeRecord.MFERMultiple, tradeRecord.MAERMultiple, tradeRecord.RealisedRMultiple, tradeRecord.RealisedNetPnL, tradeRecord.ExitReason)
		}
	}
	builder.WriteString("\n")

	return builder.String()
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
