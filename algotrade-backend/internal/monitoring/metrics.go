package monitoring

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"
)

// ComputeEODMetrics calculates the complete diagnostic report for a given trading date
func ComputeEODMetrics(db *sql.DB, tradingDate string) (*EODReportSummary, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	report := &EODReportSummary{
		TradingDate:        tradingDate,
		GeneratedAt:        time.Now(),
		SlippageRecords:    make([]SlippageAuditRecord, 0),
		ToxicSymbols:       make([]string, 0),
		ProbabilityBuckets: make([]ProbabilityBucket, 0),
		SessionBreakdowns:  make([]TimeOfDayBucket, 0),
		GearAttributions:   make([]GearAttributionRecord, 0),
		TradeLedger:        make([]PositionReportRecord, 0),
		ShadowSummary: ShadowComparisonSummary{
			RejectionBreakdown: make(map[string]int),
		},
	}

	// 1. Fetch Closed and Active Positions for Today
	rows, err := db.Query(`
		SELECT
			position_id, symbol, pattern_type, entry_price, exit_price, initial_stop, one_r,
			initial_qty, high_water_mark, low_water_mark, mfe_r, mae_r,
			intended_entry_price, entry_slippage_pct, exit_slippage_pct,
			realised_gross_pnl, total_charges, realised_net_pnl,
			current_gear, exit_reason, opened_at, closed_at
		FROM positions
		WHERE trading_date = ?
		ORDER BY opened_at ASC
	`, tradingDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query positions for EOD metrics: %w", err)
	}
	defer rows.Close()

	var totalGrossWins, totalGrossLosses float64
	var sumRealisedR, sumMFE, sumMAE float64
	var winnerMAEs []float64

	slippageMap := make(map[string]*struct {
		count         int
		sumIntended   float64
		sumActual     float64
		sumSlippage   float64
		maxSlippage   float64
	})

	gearMap := make(map[int]*struct {
		count int
		pnl   float64
	})
	for g := 1; g <= 4; g++ {
		gearMap[g] = &struct {
			count int
			pnl   float64
		}{}
	}

	todMap := map[string]*TimeOfDayBucket{
		"OPENING_DRIVE": {
			SessionName: "Opening Drive",
			TimeWindow:  "09:15 - 10:00",
		},
		"MID_MORNING": {
			SessionName: "Mid-Morning Discovery",
			TimeWindow:  "10:00 - 11:30",
		},
		"MIDDAY_LULL": {
			SessionName: "Mid-Day Compression",
			TimeWindow:  "11:30 - 13:30",
		},
		"CLOSING_SESSION": {
			SessionName: "Closing Session",
			TimeWindow:  "13:30 - 15:15",
		},
	}

	for rows.Next() {
		var rec PositionReportRecord
		var openStr, closeStr sql.NullString
		var exitPx, hwm, lwm, mfeR, maeR, intendedPx, entrySlip, exitSlip float64

		scanErr := rows.Scan(
			&rec.PositionID, &rec.Symbol, &rec.PatternType, &rec.EntryPrice, &exitPx, &rec.InitialStop, &rec.OneR,
			&rec.Quantity, &hwm, &lwm, &mfeR, &maeR,
			&intendedPx, &entrySlip, &exitSlip,
			&rec.RealisedGrossPnL, &rec.TotalCharges, &rec.RealisedNetPnL,
			&rec.MaxGearReached, &rec.ExitReason, &openStr, &closeStr,
		)
		if scanErr != nil {
			continue
		}

		rec.ExitPrice = exitPx
		rec.MFERMultiple = mfeR
		rec.MAERMultiple = maeR
		rec.IntendedEntryPrice = intendedPx
		rec.EntrySlippagePct = entrySlip
		rec.OpenedAt = openStr.String
		rec.ClosedAt = closeStr.String

		if rec.OneR > 0 && exitPx > 0 {
			rec.RealisedRMultiple = math.Round(((exitPx-rec.EntryPrice)/rec.OneR)*100.0) / 100.0
		}

		var openTime, closeTime time.Time
		if openStr.Valid && openStr.String != "" {
			openTime, _ = time.Parse(time.RFC3339, openStr.String)
			if openTime.IsZero() {
				openTime, _ = time.Parse("2006-01-02 15:04:05", openStr.String)
			}
		}
		if closeStr.Valid && closeStr.String != "" {
			closeTime, _ = time.Parse(time.RFC3339, closeStr.String)
			if closeTime.IsZero() {
				closeTime, _ = time.Parse("2006-01-02 15:04:05", closeStr.String)
			}
		}
		if !openTime.IsZero() && !closeTime.IsZero() {
			rec.DurationMinutes = int(closeTime.Sub(openTime).Minutes())
			if rec.DurationMinutes < 0 {
				rec.DurationMinutes = 0
			}
		}

		if rec.RealisedNetPnL > 0 {
			rec.IsWinner = true
			report.WinningTrades++
			totalGrossWins += rec.RealisedGrossPnL
			winnerMAEs = append(winnerMAEs, maeR)
		} else if rec.RealisedNetPnL < 0 {
			report.LosingTrades++
			totalGrossLosses += math.Abs(rec.RealisedGrossPnL)
		} else {
			report.BreakevenTrades++
		}

		report.TotalTrades++
		report.RealisedGrossPnL += rec.RealisedGrossPnL
		report.TotalCharges += rec.TotalCharges
		report.RealisedNetPnL += rec.RealisedNetPnL
		sumRealisedR += rec.RealisedRMultiple
		sumMFE += rec.MFERMultiple
		sumMAE += rec.MAERMultiple

		// Slippage tracking
		if intendedPx > 0 && rec.EntryPrice > 0 {
			slip := ((rec.EntryPrice - intendedPx) / intendedPx) * 100.0
			sData, ok := slippageMap[rec.Symbol]
			if !ok {
				sData = &struct {
					count       int
					sumIntended float64
					sumActual   float64
					sumSlippage float64
					maxSlippage float64
				}{}
				slippageMap[rec.Symbol] = sData
			}
			sData.count++
			sData.sumIntended += intendedPx
			sData.sumActual += rec.EntryPrice
			sData.sumSlippage += slip
			if math.Abs(slip) > sData.maxSlippage {
				sData.maxSlippage = math.Abs(slip)
			}
		}

		// Gear attribution
		gKey := rec.MaxGearReached
		if gKey < 1 {
			gKey = 1
		} else if gKey > 4 {
			gKey = 4
		}
		if gData, ok := gearMap[gKey]; ok {
			gData.count++
			gData.pnl += rec.RealisedNetPnL
		}

		// Session Time-of-Day attribution
		if !openTime.IsZero() {
			hhmm := openTime.Format("15:04")
			var todKey string
			switch {
			case hhmm >= "09:15" && hhmm < "10:00":
				todKey = "OPENING_DRIVE"
			case hhmm >= "10:00" && hhmm < "11:30":
				todKey = "MID_MORNING"
			case hhmm >= "11:30" && hhmm < "13:30":
				todKey = "MIDDAY_LULL"
			default:
				todKey = "CLOSING_SESSION"
			}
			if bucket, ok := todMap[todKey]; ok {
				bucket.TradeCount++
				if rec.IsWinner {
					bucket.WinningCount++
				}
				bucket.GrossPnL += rec.RealisedGrossPnL
				bucket.NetPnL += rec.RealisedNetPnL
				bucket.AverageR += rec.RealisedRMultiple
			}
		}

		report.TradeLedger = append(report.TradeLedger, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating positions: %w", err)
	}

	// 2. Compute Summary Ratios
	if report.TotalTrades > 0 {
		report.WinRatePct = math.Round((float64(report.WinningTrades)/float64(report.TotalTrades))*1000.0) / 10.0
		report.AverageRMultiple = math.Round((sumRealisedR/float64(report.TotalTrades))*100.0) / 100.0
		if totalGrossLosses > 0 {
			report.ProfitFactor = math.Round((totalGrossWins/totalGrossLosses)*100.0) / 100.0
		} else if totalGrossWins > 0 {
			report.ProfitFactor = 999.0
		}
	}

	report.RealisedGrossPnL = math.Round(report.RealisedGrossPnL*100.0) / 100.0
	report.TotalCharges = math.Round(report.TotalCharges*100.0) / 100.0
	report.RealisedNetPnL = math.Round(report.RealisedNetPnL*100.0) / 100.0

	// 3. MFE vs MAE Summary
	if report.TotalTrades > 0 {
		avgMFE := sumMFE / float64(report.TotalTrades)
		avgMAE := sumMAE / float64(report.TotalTrades)
		report.ExcursionSummary.AverageMFE = math.Round(avgMFE*100.0) / 100.0
		report.ExcursionSummary.AverageMAE = math.Round(avgMAE*100.0) / 100.0
		report.ExcursionSummary.AverageRealisedR = report.AverageRMultiple

		if math.Abs(avgMAE) > 0.001 {
			report.ExcursionSummary.EdgeRatio = math.Round((avgMFE/math.Abs(avgMAE))*100.0) / 100.0
		}
		if avgMFE > 0 {
			report.ExcursionSummary.CaptureEfficiency = math.Round((report.AverageRMultiple/avgMFE)*1000.0) / 10.0
		}

		// Evaluate winning trades MAE distribution
		if len(winnerMAEs) > 0 {
			sort.Float64s(winnerMAEs)
			mid := len(winnerMAEs) / 2
			report.ExcursionSummary.MedianMAEWinners = math.Round(winnerMAEs[mid]*100.0) / 100.0

			under06Count := 0
			for _, mae := range winnerMAEs {
				if mae >= -0.60 {
					under06Count++
				}
			}
			report.ExcursionSummary.PctWinnersUnder06R = math.Round((float64(under06Count)/float64(len(winnerMAEs)))*1000.0) / 10.0
			if report.ExcursionSummary.PctWinnersUnder06R >= 85.0 {
				report.ExcursionSummary.StopLossAlert = fmt.Sprintf(
					"%.1f%% of winning trades never breached -0.6R (Median MAE %.2fR). Initial stops may be 20-30%% too wide; tightening stops will safely increase position sizing.",
					report.ExcursionSummary.PctWinnersUnder06R, report.ExcursionSummary.MedianMAEWinners,
				)
			}
		}

		if report.ExcursionSummary.AverageMFE >= 2.0 && report.AverageRMultiple <= 1.1 {
			report.ExcursionSummary.TrailingStopAlert = fmt.Sprintf(
				"Trades averaged +%.2fR peak MFE but realized only +%.2fR (Capture Efficiency: %.1f%%). Trailing stop in Phase 3 may be giving back excessive open profit.",
				report.ExcursionSummary.AverageMFE, report.AverageRMultiple, report.ExcursionSummary.CaptureEfficiency,
			)
		}
	}

	// 4. Slippage Audit Records
	for sym, s := range slippageMap {
		if s.count == 0 {
			continue
		}
		avgSlip := s.sumSlippage / float64(s.count)
		isToxic := avgSlip > 0.15 || s.maxSlippage > 0.30
		rec := SlippageAuditRecord{
			Symbol:           sym,
			TradeCount:       s.count,
			AvgIntendedPrice: math.Round((s.sumIntended/float64(s.count))*100.0) / 100.0,
			AvgFillPrice:     math.Round((s.sumActual/float64(s.count))*100.0) / 100.0,
			AvgSlippagePct:   math.Round(avgSlip*1000.0) / 1000.0,
			MaxSlippagePct:   math.Round(s.maxSlippage*1000.0) / 1000.0,
			IsToxicLiquidity: isToxic,
		}
		if isToxic {
			rec.BlacklistReason = fmt.Sprintf("Average slippage (%.2f%%) exceeds toxic illiquidity ceiling (0.15%%)", avgSlip)
			report.ToxicSymbols = append(report.ToxicSymbols, sym)
		}
		report.SlippageRecords = append(report.SlippageRecords, rec)
	}
	sort.Slice(report.SlippageRecords, func(i, j int) bool {
		return report.SlippageRecords[i].AvgSlippagePct > report.SlippageRecords[j].AvgSlippagePct
	})

	// 5. Gear Attribution Records
	gearLabels := map[int]string{
		1: "Gear 1: Agile Dynamic Trail",
		2: "Gear 2: 50% Banked + Breakeven",
		3: "Gear 3: Coarse 5M Trail",
		4: "Gear 4: Shadow SL Guard",
	}
	for g := 1; g <= 4; g++ {
		report.GearAttributions = append(report.GearAttributions, GearAttributionRecord{
			Gear:        g,
			GearLabel:   gearLabels[g],
			TradesCount: gearMap[g].count,
			BankedPnL:   math.Round(gearMap[g].pnl*100.0) / 100.0,
		})
	}

	// 6. Time of Day Breakdowns
	for _, key := range []string{"OPENING_DRIVE", "MID_MORNING", "MIDDAY_LULL", "CLOSING_SESSION"} {
		b := todMap[key]
		if b.TradeCount > 0 {
			b.WinRatePct = math.Round((float64(b.WinningCount)/float64(b.TradeCount))*1000.0) / 10.0
			b.AverageR = math.Round((b.AverageR/float64(b.TradeCount))*100.0) / 100.0
		}
		b.GrossPnL = math.Round(b.GrossPnL*100.0) / 100.0
		b.NetPnL = math.Round(b.NetPnL*100.0) / 100.0
		report.SessionBreakdowns = append(report.SessionBreakdowns, *b)
	}

	// 7. Probability Calibration Buckets
	probBuckets := []struct {
		name string
		min  float64
		max  float64
	}{
		{"50–65% Moderate", 50.0, 65.0},
		{"65–75% Strong", 65.0, 75.0},
		{"75–85% High Confluence", 75.0, 85.0},
		{"85%+ Prime Outlier", 85.0, 100.0},
	}
	for _, pb := range probBuckets {
		bucket := ProbabilityBucket{
			BucketName: pb.name,
			MinScore:   pb.min,
			MaxScore:   pb.max,
		}

		// Query positions that match this score range from engine_signals
		pRows, pErr := db.Query(`
			SELECT p.realised_net_pnl, p.exit_price, p.entry_price, p.one_r
			FROM positions p
			INNER JOIN engine_signals s ON p.symbol = s.tradingsymbol AND date(p.opened_at) = date(s.signal_time)
			WHERE p.trading_date = ? AND s.score >= ? AND s.score < ?
		`, tradingDate, pb.min, pb.max)
		if pErr == nil {
			var sumR float64
			for pRows.Next() {
				var netPnL, exitP, entryP, oneR float64
				if sErr := pRows.Scan(&netPnL, &exitP, &entryP, &oneR); sErr == nil {
					bucket.TradeCount++
					if netPnL > 0 {
						bucket.WinningCount++
					}
					bucket.NetPnL += netPnL
					if oneR > 0 {
						sumR += (exitP - entryP) / oneR
					}
				}
			}
			if err := pRows.Err(); err != nil {
				pRows.Close()
				return nil, fmt.Errorf("error iterating probability bucket rows: %w", err)
			}
			pRows.Close()
			if bucket.TradeCount > 0 {
				bucket.WinRatePct = math.Round((float64(bucket.WinningCount)/float64(bucket.TradeCount))*1000.0) / 10.0
				bucket.AverageR = math.Round((sumR/float64(bucket.TradeCount))*100.0) / 100.0
				bucket.NetPnL = math.Round(bucket.NetPnL*100.0) / 100.0
			}
		}
		report.ProbabilityBuckets = append(report.ProbabilityBuckets, bucket)
	}

	// 8. Shadow Signals Tracking Comparison
	sRows, sErr := db.Query(`
		SELECT rejection_reason, outcome, simulated_r
		FROM shadow_signals
		WHERE trading_date = ?
	`, tradingDate)
	if sErr == nil {
		defer sRows.Close()
		var shadowWins int
		var sumShadowR float64
		for sRows.Next() {
			var reason, outcome string
			var simR float64
			if sScanErr := sRows.Scan(&reason, &outcome, &simR); sScanErr == nil {
				report.ShadowSummary.ShadowSignalCount++
				report.ShadowSummary.RejectionBreakdown[reason]++
				if outcome == "WIN_1R" || simR > 0 {
					shadowWins++
				}
				sumShadowR += simR
			}
		}
		if err := sRows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating shadow signals: %w", err)
		}
		if report.ShadowSummary.ShadowSignalCount > 0 {
			report.ShadowSummary.ShadowWinRatePct = math.Round((float64(shadowWins)/float64(report.ShadowSummary.ShadowSignalCount))*1000.0) / 10.0
			report.ShadowSummary.ShadowAverageR = math.Round((sumShadowR/float64(report.ShadowSummary.ShadowSignalCount))*100.0) / 100.0
		}
		report.ShadowSummary.ActualTradeCount = report.TotalTrades
		report.ShadowSummary.ActualWinRatePct = report.WinRatePct
		report.ShadowSummary.ActualAverageR = report.AverageRMultiple
		report.ShadowSummary.ActualTotalNetPnL = report.RealisedNetPnL

		if report.ShadowSummary.ShadowSignalCount >= 3 && report.ShadowSummary.ShadowWinRatePct > report.WinRatePct && report.ShadowSummary.ShadowAverageR > report.AverageRMultiple {
			report.ShadowSummary.FilterEfficiencyAlert = fmt.Sprintf(
				"WARNING: Rejected trades outperformed actual trades (Shadow Win Rate: %.1f%% vs Actual: %.1f%%; Shadow Avg R: %.2fR vs Actual: %.2fR). Pre-trade gatekeeper or heat filters may be rejecting profitable edge.",
				report.ShadowSummary.ShadowWinRatePct, report.WinRatePct, report.ShadowSummary.ShadowAverageR, report.AverageRMultiple,
			)
		}
	}

	return report, nil
}

