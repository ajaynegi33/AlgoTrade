package trading

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
)

func round2(value float64) float64 {
	return math.Round(value*100.0) / 100.0
}

func formatHoldDuration(durationSeconds int64) string {
	if durationSeconds <= 0 {
		return "0s"
	}
	hours := durationSeconds / 3600
	minutes := (durationSeconds % 3600) / 60
	seconds := durationSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm %02ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func parseDBTimeIST(raw string, ist *time.Location) (time.Time, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, "—"
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			if layout == "15:04:05" {
				return t, raw
			}
			tIST := t.In(ist)
			return tIST, tIST.Format("15:04:05")
		}
	}
	return time.Time{}, raw
}

// GetTradeHistory returns the complete historical trade ledger, itemized 6-part Indian statutory taxes,
// child order legs, and strategy/gear attribution analytics.
func (manager *Manager) GetTradeHistory(modeFilter, fromDate, toDate string, limit int) *market.TradeHistoryResponseDTO {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	ist := market.ISTLocation
	modeFilter = strings.ToUpper(strings.TrimSpace(modeFilter))

	tradeHistoryItems := make([]market.TradeHistoryItemDTO, 0, 64)
	seenPosIDs := make(map[string]bool)

	if manager.db != nil {
		var whereClauses []string
		var args []interface{}

		if modeFilter == "PAPER" || modeFilter == "LIVE" {
			whereClauses = append(whereClauses, "p.mode = ?")
			args = append(args, modeFilter)
		}
		if fromDate != "" {
			whereClauses = append(whereClauses, "p.trading_date >= ?")
			args = append(args, fromDate)
		}
		if toDate != "" {
			whereClauses = append(whereClauses, "p.trading_date <= ?")
			args = append(args, toDate)
		}

		whereSQL := ""
		if len(whereClauses) > 0 {
			whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
		}
		args = append(args, limit)

		query := fmt.Sprintf(`
			SELECT
				p.position_id,
				p.trading_date,
				p.mode,
				p.symbol,
				COALESCE(ss.name, p.symbol) AS company_name,
				COALESCE(sm.sector_name, 'NSE Cash Equity') AS sector_name,
				COALESCE(ss.mis_leverage, 5.0) AS mis_leverage,
				p.instrument_token,
				p.state,
				p.pattern_type,
				p.entry_price,
				p.exit_price,
				p.initial_stop,
				p.current_stop,
				p.shadow_stop,
				p.breakeven_stop,
				p.high_water_mark,
				p.one_r,
				p.initial_qty,
				p.filled_entry_qty,
				p.filled_exit_qty,
				p.mod_count,
				p.current_gear,
				p.tranche1_done,
				p.realised_gross_pnl,
				p.total_charges,
				p.realised_net_pnl,
				p.exit_reason,
				p.opened_at,
				COALESCE(p.closed_at, '')
			FROM positions p
			LEFT JOIN scanned_stocks ss ON ss.tradingsymbol = p.symbol
			LEFT JOIN sector_metadata sm ON sm.tradingsymbol = p.symbol
			%s
			ORDER BY p.opened_at DESC
			LIMIT ?
		`, whereSQL)

		type rawPositionRecord struct {
			posID, trDate, mode, sym, compName, secName, state, pattern, exitReason, openedRaw, closedRaw string
			token                                                                                          uint32
			misLev, entryPx, exitPx, initStop, curStop, shStop, beStop, hwm, oneR, gross, charges, net     float64
			initQty, filledEntry, filledExit, modCnt, gear, t1Int                                          int
		}
		var rawPositions []rawPositionRecord

		rows, err := manager.db.Query(query, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var positionRecord rawPositionRecord
				if scanErr := rows.Scan(
					&positionRecord.posID, &positionRecord.trDate, &positionRecord.mode, &positionRecord.sym, &positionRecord.compName, &positionRecord.secName, &positionRecord.misLev,
					&positionRecord.token, &positionRecord.state, &positionRecord.pattern,
					&positionRecord.entryPx, &positionRecord.exitPx, &positionRecord.initStop, &positionRecord.curStop, &positionRecord.shStop, &positionRecord.beStop, &positionRecord.hwm, &positionRecord.oneR,
					&positionRecord.initQty, &positionRecord.filledEntry, &positionRecord.filledExit, &positionRecord.modCnt, &positionRecord.gear, &positionRecord.t1Int,
					&positionRecord.gross, &positionRecord.charges, &positionRecord.net, &positionRecord.exitReason, &positionRecord.openedRaw, &positionRecord.closedRaw,
				); scanErr == nil {
					rawPositions = append(rawPositions, positionRecord)
				}
			}
			if rErr := rows.Err(); rErr != nil {
				slog.Error("Failed iterating trade history rows from DB", "error", rErr)
			}

			for _, positionRecord := range rawPositions {
				act := market.ActivePositionDTO{
					PositionID:       positionRecord.posID,
					Mode:             positionRecord.mode,
					Symbol:           positionRecord.sym,
					InstrumentToken:  positionRecord.token,
					State:            positionRecord.state,
					PatternType:      positionRecord.pattern,
					EntryPrice:       positionRecord.entryPx,
					ExitPrice:        positionRecord.exitPx,
					InitialStop:      positionRecord.initStop,
					CurrentStop:      positionRecord.curStop,
					ShadowStop:       positionRecord.shStop,
					BreakevenStop:    positionRecord.beStop,
					HighWaterMark:    positionRecord.hwm,
					OneR:             positionRecord.oneR,
					InitialQty:       positionRecord.initQty,
					RemainingQty:     positionRecord.initQty - positionRecord.filledExit,
					ModCount:             positionRecord.modCnt,
					CurrentGear:          positionRecord.gear,
					HasTranche1Completed: positionRecord.t1Int != 0,
					RealisedGrossPnL:     positionRecord.gross,
					EstimatedCharges: positionRecord.charges,
					NetTotalPnL:      positionRecord.net,
					ExitReason:       positionRecord.exitReason,
					OpenedAt:         positionRecord.openedRaw,
					ClosedAt:         positionRecord.closedRaw,
				}
				if positionRecord.filledEntry > 0 {
					act.InitialQty = positionRecord.filledEntry
				}
				item := buildTradeHistoryFromDTO(manager.db, ist, act, positionRecord.trDate, positionRecord.compName, positionRecord.secName, positionRecord.misLev)
				tradeHistoryItems = append(tradeHistoryItems, item)
				seenPosIDs[positionRecord.posID] = true
			}
		}
	}

	// Merge any currently active positions in memory not yet in SQLite or still in market
	manager.mu.RLock()
	activeSnapshots := make([]market.ActivePositionDTO, 0, len(manager.activeWorkers))
	for _, worker := range manager.activeWorkers {
		activeSnapshots = append(activeSnapshots, worker.ToDTO())
	}
	memClosed := make([]market.ActivePositionDTO, len(manager.closedPositions))
	copy(memClosed, manager.closedPositions)
	manager.mu.RUnlock()

	todayStr := market.TodayDateIST()
	appendActivePosition := func(pos market.ActivePositionDTO) {
		if seenPosIDs[pos.PositionID] {
			return
		}
		if (modeFilter == "PAPER" || modeFilter == "LIVE") && pos.Mode != modeFilter {
			return
		}
		item := buildTradeHistoryFromDTO(manager.db, ist, pos, todayStr, pos.Symbol, "NSE Cash Equity", 5.0)
		tradeHistoryItems = append(tradeHistoryItems, item)
		seenPosIDs[pos.PositionID] = true
	}

	for _, act := range activeSnapshots {
		appendActivePosition(act)
	}

	for _, closedPosition := range memClosed {
		appendActivePosition(closedPosition)
	}

	summary := computeTradeAnalyticsSummary(tradeHistoryItems)
	return &market.TradeHistoryResponseDTO{
		Summary: summary,
		Trades:  tradeHistoryItems,
	}
}

func buildTradeHistoryFromDTO(
	db *sql.DB,
	ist *time.Location,
	act market.ActivePositionDTO,
	tradingDate, compName, secName string,
	misLev float64,
) market.TradeHistoryItemDTO {
	qty := act.InitialQty
	if qty <= 0 {
		qty = 1
	}
	if misLev < 1.0 {
		misLev = 5.0
	}
	if compName == "" {
		compName = act.Symbol
	}
	if secName == "" {
		secName = "NSE Cash Equity"
	}

	buyPrice := act.EntryPrice
	sellPrice := act.ExitPrice
	gross := act.RealisedGrossPnL
	if sellPrice <= 0 && qty > 0 && math.Abs(gross) > 1e-6 {
		sellPrice = math.Max(0.05, buyPrice+(gross/float64(qty)))
	}
	if sellPrice <= 0 {
		sellPrice = buyPrice
	}

	totalInvested := float64(qty) * buyPrice
	marginBlocked := totalInvested / misLev
	totalSellValue := float64(qty) * sellPrice
	priceDiff := sellPrice - buyPrice

	if math.Abs(gross) < 1e-6 && qty > 0 && math.Abs(priceDiff) > 1e-6 {
		gross = totalSellValue - totalInvested
	}

	taxDTO := CalculatePositionTaxBreakdown(buyPrice, sellPrice, qty, act.HasTranche1Completed)

	netAfterTax := round2(gross - taxDTO.TotalCharges)

	var roiInvested, roiMargin float64
	if totalInvested > 0 {
		roiInvested = (netAfterTax / totalInvested) * 100.0
	}
	if marginBlocked > 0 {
		roiMargin = (netAfterTax / marginBlocked) * 100.0
	}

	tOpen, openClock := parseDBTimeIST(act.OpenedAt, ist)
	tClose, closeClock := parseDBTimeIST(act.ClosedAt, ist)
	var holdSec int64
	if !tOpen.IsZero() && !tClose.IsZero() && tClose.After(tOpen) {
		holdSec = int64(tClose.Sub(tOpen).Seconds())
	}

	finalStop := act.CurrentStop
	if act.ShadowStop > finalStop {
		finalStop = act.ShadowStop
	}

	var realisedR, peakMFER, rCapturePct float64
	if act.OneR > 0 {
		realisedR = (sellPrice - buyPrice) / act.OneR
		if act.HighWaterMark > buyPrice {
			peakMFER = (act.HighWaterMark - buyPrice) / act.OneR
		}
		if peakMFER > 0.05 && realisedR > 0 {
			rCapturePct = math.Min(100.0, (realisedR/peakMFER)*100.0)
		}
	}

	var orderLegs []market.TradeOrderLegDTO
	if db != nil {
		oRows, err := db.Query(`
			SELECT order_id, position_id, leg_type, transaction_type, order_type,
			       quantity, filled_quantity, price, trigger_price, average_price,
			       status, mod_count, updated_at
			FROM orders
			WHERE position_id = ?
			ORDER BY updated_at ASC
		`, act.PositionID)
		if err == nil {
			defer oRows.Close()
			for oRows.Next() {
				var leg market.TradeOrderLegDTO
				var updRaw string
				if err := oRows.Scan(
					&leg.OrderID, &leg.PositionID, &leg.LegType, &leg.TransactionType, &leg.OrderType,
					&leg.Quantity, &leg.FilledQuantity, &leg.Price, &leg.TriggerPrice, &leg.AveragePrice,
					&leg.Status, &leg.ModCount, &updRaw,
				); err == nil {
					_, leg.UpdatedAt = parseDBTimeIST(updRaw, ist)
					orderLegs = append(orderLegs, leg)
				}
			}
			if oErr := oRows.Err(); oErr != nil {
				slog.Error("Failed iterating position order legs from DB", "position_id", act.PositionID, "error", oErr)
			}
		}
	}

	exitReason := act.ExitReason
	if exitReason == "" {
		if act.State == StateClosed {
			exitReason = "SL_OR_TARGET_EXIT"
		} else {
			exitReason = "ACTIVE_IN_MARKET"
		}
	}

	return market.TradeHistoryItemDTO{
		PositionID:            act.PositionID,
		TradingDate:           tradingDate,
		Mode:                  act.Mode,
		Symbol:                act.Symbol,
		CompanyName:           compName,
		Sector:                secName,
		InstrumentToken:       act.InstrumentToken,
		State:                 act.State,
		PatternType:           act.PatternType,
		PurchaseTime:          openClock,
		SellTime:              closeClock,
		HoldDurationSec:       holdSec,
		HoldDurationLabel:     formatHoldDuration(holdSec),
		Quantity:              qty,
		BuyPrice:              round2(buyPrice),
		SellPrice:             round2(sellPrice),
		TotalInvested:         round2(totalInvested),
		MarginBlocked:         round2(marginBlocked),
		TotalSellValue:        round2(totalSellValue),
		PriceChangePerShare:   round2(priceDiff),
		GrossPnL:              round2(gross),
		Taxes:                 taxDTO,
		NetPnLAfterTax:        netAfterTax,
		ReturnOnInvestedPct:   round2(roiInvested),
		ReturnOnMarginPct:     round2(roiMargin),
		InitialStop:           round2(act.InitialStop),
		FinalStop:             round2(finalStop),
		BreakevenStop:         round2(act.BreakevenStop),
		HighWaterMark:         round2(act.HighWaterMark),
		OneR:                  round2(act.OneR),
		RealisedRMultiple:     round2(realisedR),
		PeakMFER:              round2(peakMFER),
		RCaptureEfficiencyPct: round2(rCapturePct),
		ModCount:              act.ModCount,
		FinalGear:             act.CurrentGear,
		HasTranche1Completed:  act.HasTranche1Completed,
		ExitReason:            exitReason,
		Orders:                orderLegs,
	}
}

func computeTradeAnalyticsSummary(trades []market.TradeHistoryItemDTO) market.TradeAnalyticsSummaryDTO {
	var sum market.TradeAnalyticsSummaryDTO
	sum.ByPattern = []market.PatternPerformanceDTO{}
	sum.ByExitReason = []market.ExitReasonStatDTO{}

	if len(trades) == 0 {
		return sum
	}

	patternMap := make(map[string]*market.PatternPerformanceDTO)
	reasonMap := make(map[string]*market.ExitReasonStatDTO)

	var (
		totalR, winRSum, lossRSum             float64
		winRupeeSum, lossRupeeSum             float64
		totalHoldSec                          int64
		t1Count, captureCount                 int
		captureSum                            float64
	)

	for _, trade := range trades {
		if trade.Quantity <= 0 {
			continue
		}
		sum.TotalTrades++
		sum.TotalInvestedTurnover += trade.TotalInvested
		sum.TotalSellTurnover += trade.TotalSellValue
		sum.TotalMarginDeployed += trade.MarginBlocked
		sum.RealisedGrossPnL += trade.GrossPnL
		sum.RealisedNetPnL += trade.NetPnLAfterTax

		if trade.GrossPnL >= 0 {
			sum.GrossProfitRupees += trade.GrossPnL
		} else {
			sum.GrossLossRupees += trade.GrossPnL
		}

		// Accumulate 6-part statutory tax breakdown
		sum.Taxes.BuyBrokerage += trade.Taxes.BuyBrokerage
		sum.Taxes.SellBrokerage += trade.Taxes.SellBrokerage
		sum.Taxes.Brokerage += trade.Taxes.Brokerage
		sum.Taxes.STT += trade.Taxes.STT
		sum.Taxes.ExchangeTxn += trade.Taxes.ExchangeTxn
		sum.Taxes.SEBICharges += trade.Taxes.SEBICharges
		sum.Taxes.StampDuty += trade.Taxes.StampDuty
		sum.Taxes.GST += trade.Taxes.GST
		sum.Taxes.TotalCharges += trade.Taxes.TotalCharges

		if trade.NetPnLAfterTax > 0.50 {
			sum.Wins++
			winRSum += trade.RealisedRMultiple
			winRupeeSum += trade.NetPnLAfterTax
			if trade.NetPnLAfterTax > sum.MaxWinRupees {
				sum.MaxWinRupees = trade.NetPnLAfterTax
			}
		} else if trade.NetPnLAfterTax < -0.50 {
			sum.Losses++
			lossRSum += trade.RealisedRMultiple
			lossRupeeSum += trade.NetPnLAfterTax
			if trade.NetPnLAfterTax < sum.MaxLossRupees {
				sum.MaxLossRupees = trade.NetPnLAfterTax
			}
		} else {
			sum.BreakevenTrades++
		}

		totalR += trade.RealisedRMultiple
		totalHoldSec += trade.HoldDurationSec
		if trade.HasTranche1Completed {
			t1Count++
		}
		if trade.RCaptureEfficiencyPct > 0 {
			captureCount++
			captureSum += trade.RCaptureEfficiencyPct
		}

		// Pattern attribution
		patternType := trade.PatternType
		if patternType == "" {
			patternType = "VWAP_PULLBACK_RECLAIM"
		}
		patternStat, exists := patternMap[patternType]
		if !exists {
			patternStat = &market.PatternPerformanceDTO{PatternType: patternType}
			patternMap[patternType] = patternStat
		}
		patternStat.Trades++
		if trade.NetPnLAfterTax > 0 {
			patternStat.Wins++
		}
		patternStat.AvgRMultiple += trade.RealisedRMultiple
		patternStat.GrossPnL += trade.GrossPnL
		patternStat.TotalTaxes += trade.Taxes.TotalCharges
		patternStat.NetPnLAfterTax += trade.NetPnLAfterTax

		// Exit Reason attribution
		exitReason := trade.ExitReason
		reasonStat, exists := reasonMap[exitReason]
		if !exists {
			reasonStat = &market.ExitReasonStatDTO{ExitReason: exitReason}
			reasonMap[exitReason] = reasonStat
		}
		reasonStat.Count++
		reasonStat.AvgRMultiple += trade.RealisedRMultiple
		reasonStat.GrossPnL += trade.GrossPnL
		reasonStat.TotalTaxes += trade.Taxes.TotalCharges
		reasonStat.NetPnLAfterTax += trade.NetPnLAfterTax
	}

	if sum.TotalTrades > 0 {
		totalTradeCount := float64(sum.TotalTrades)
		sum.WinRatePct = round2((float64(sum.Wins) / totalTradeCount) * 100.0)
		sum.ExpectancyPerTrade = round2(sum.RealisedNetPnL / totalTradeCount)
		sum.AvgRMultiple = round2(totalR / totalTradeCount)
		sum.AvgHoldDurationSec = totalHoldSec / int64(sum.TotalTrades)
		sum.Tranche1ScaleOutRate = round2((float64(t1Count) / totalTradeCount) * 100.0)
	}
	if sum.Wins > 0 {
		sum.AvgWinR = round2(winRSum / float64(sum.Wins))
		sum.AvgWinRupees = round2(winRupeeSum / float64(sum.Wins))
	}
	if sum.Losses > 0 {
		sum.AvgLossR = round2(lossRSum / float64(sum.Losses))
		sum.AvgLossRupees = round2(lossRupeeSum / float64(sum.Losses))
	}
	if captureCount > 0 {
		sum.AvgRCaptureEfficiency = round2(captureSum / float64(captureCount))
	}
	if math.Abs(sum.GrossLossRupees) > 0.01 {
		sum.ProfitFactor = round2(sum.GrossProfitRupees / math.Abs(sum.GrossLossRupees))
	} else if sum.GrossProfitRupees > 0 {
		sum.ProfitFactor = 99.9
	}
	if sum.GrossProfitRupees > 0 {
		sum.TaxDragPctOfGrossWin = round2((sum.Taxes.TotalCharges / sum.GrossProfitRupees) * 100.0)
	}

	sum.TotalInvestedTurnover = round2(sum.TotalInvestedTurnover)
	sum.TotalSellTurnover = round2(sum.TotalSellTurnover)
	sum.TotalMarginDeployed = round2(sum.TotalMarginDeployed)
	sum.GrossProfitRupees = round2(sum.GrossProfitRupees)
	sum.GrossLossRupees = round2(sum.GrossLossRupees)
	sum.RealisedGrossPnL = round2(sum.RealisedGrossPnL)
	sum.RealisedNetPnL = round2(sum.RealisedNetPnL)
	sum.MaxWinRupees = round2(sum.MaxWinRupees)
	sum.MaxLossRupees = round2(sum.MaxLossRupees)

	sum.Taxes.BuyBrokerage = round2(sum.Taxes.BuyBrokerage)
	sum.Taxes.SellBrokerage = round2(sum.Taxes.SellBrokerage)
	sum.Taxes.Brokerage = round2(sum.Taxes.Brokerage)
	sum.Taxes.STT = round2(sum.Taxes.STT)
	sum.Taxes.ExchangeTxn = round2(sum.Taxes.ExchangeTxn)
	sum.Taxes.SEBICharges = round2(sum.Taxes.SEBICharges)
	sum.Taxes.StampDuty = round2(sum.Taxes.StampDuty)
	sum.Taxes.GST = round2(sum.Taxes.GST)
	sum.Taxes.TotalCharges = round2(sum.Taxes.TotalCharges)

	for _, patternBreakdown := range patternMap {
		if patternBreakdown.Trades > 0 {
			patternBreakdown.WinRatePct = round2((float64(patternBreakdown.Wins) / float64(patternBreakdown.Trades)) * 100.0)
			patternBreakdown.AvgRMultiple = round2(patternBreakdown.AvgRMultiple / float64(patternBreakdown.Trades))
		}
		patternBreakdown.GrossPnL = round2(patternBreakdown.GrossPnL)
		patternBreakdown.TotalTaxes = round2(patternBreakdown.TotalTaxes)
		patternBreakdown.NetPnLAfterTax = round2(patternBreakdown.NetPnLAfterTax)
		sum.ByPattern = append(sum.ByPattern, *patternBreakdown)
	}
	slices.SortFunc(sum.ByPattern, func(statA, statB market.PatternPerformanceDTO) int {
		if statA.Trades != statB.Trades {
			return statB.Trades - statA.Trades
		}
		return strings.Compare(statA.PatternType, statB.PatternType)
	})

	for _, reasonBreakdown := range reasonMap {
		if reasonBreakdown.Count > 0 {
			reasonBreakdown.AvgRMultiple = round2(reasonBreakdown.AvgRMultiple / float64(reasonBreakdown.Count))
		}
		reasonBreakdown.GrossPnL = round2(reasonBreakdown.GrossPnL)
		reasonBreakdown.TotalTaxes = round2(reasonBreakdown.TotalTaxes)
		reasonBreakdown.NetPnLAfterTax = round2(reasonBreakdown.NetPnLAfterTax)
		sum.ByExitReason = append(sum.ByExitReason, *reasonBreakdown)
	}
	slices.SortFunc(sum.ByExitReason, func(statA, statB market.ExitReasonStatDTO) int {
		if statA.Count != statB.Count {
			return statB.Count - statA.Count
		}
		return strings.Compare(statA.ExitReason, statB.ExitReason)
	})

	return sum
}
