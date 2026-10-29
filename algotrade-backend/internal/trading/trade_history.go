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

func round2(v float64) float64 {
	return math.Round(v*100.0) / 100.0
}

func formatHoldDuration(sec int64) string {
	if sec <= 0 {
		return "0s"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm %02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
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
func (m *Manager) GetTradeHistory(modeFilter, fromDate, toDate string, limit int) *market.TradeHistoryResponseDTO {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	ist := market.ISTLocation
	modeFilter = strings.ToUpper(strings.TrimSpace(modeFilter))

	items := make([]market.TradeHistoryItemDTO, 0, 64)
	seenPosIDs := make(map[string]bool)

	if m.db != nil {
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

		type rawDBPos struct {
			posID, trDate, mode, sym, compName, secName, state, pattern, exitReason, openedRaw, closedRaw string
			token                                                                                          uint32
			misLev, entryPx, exitPx, initStop, curStop, shStop, beStop, hwm, oneR, gross, charges, net     float64
			initQty, filledEntry, filledExit, modCnt, gear, t1Int                                          int
		}
		var rawList []rawDBPos

		rows, err := m.db.Query(query, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var r rawDBPos
				if scanErr := rows.Scan(
					&r.posID, &r.trDate, &r.mode, &r.sym, &r.compName, &r.secName, &r.misLev,
					&r.token, &r.state, &r.pattern,
					&r.entryPx, &r.exitPx, &r.initStop, &r.curStop, &r.shStop, &r.beStop, &r.hwm, &r.oneR,
					&r.initQty, &r.filledEntry, &r.filledExit, &r.modCnt, &r.gear, &r.t1Int,
					&r.gross, &r.charges, &r.net, &r.exitReason, &r.openedRaw, &r.closedRaw,
				); scanErr == nil {
					rawList = append(rawList, r)
				}
			}
			if rErr := rows.Err(); rErr != nil {
				slog.Error("Failed iterating trade history rows from DB", "error", rErr)
			}

			for _, r := range rawList {
				act := market.ActivePositionDTO{
					PositionID:       r.posID,
					Mode:             r.mode,
					Symbol:           r.sym,
					InstrumentToken:  r.token,
					State:            r.state,
					PatternType:      r.pattern,
					EntryPrice:       r.entryPx,
					ExitPrice:        r.exitPx,
					InitialStop:      r.initStop,
					CurrentStop:      r.curStop,
					ShadowStop:       r.shStop,
					BreakevenStop:    r.beStop,
					HighWaterMark:    r.hwm,
					OneR:             r.oneR,
					InitialQty:       r.initQty,
					RemainingQty:     r.initQty - r.filledExit,
					ModCount:         r.modCnt,
					CurrentGear:      r.gear,
					Tranche1Done:     r.t1Int != 0,
					RealisedGrossPnL: r.gross,
					EstimatedCharges: r.charges,
					NetTotalPnL:      r.net,
					ExitReason:       r.exitReason,
					OpenedAt:         r.openedRaw,
					ClosedAt:         r.closedRaw,
				}
				if r.filledEntry > 0 {
					act.InitialQty = r.filledEntry
				}
				item := buildTradeHistoryFromDTO(m.db, ist, act, r.trDate, r.compName, r.secName, r.misLev)
				items = append(items, item)
				seenPosIDs[r.posID] = true
			}
		}
	}

	// Merge any currently active positions in memory not yet in SQLite or still in market
	m.mu.RLock()
	activeSnapshots := make([]market.ActivePositionDTO, 0, len(m.activeWorkers))
	for _, w := range m.activeWorkers {
		activeSnapshots = append(activeSnapshots, w.ToDTO())
	}
	memClosed := make([]market.ActivePositionDTO, len(m.closedPositions))
	copy(memClosed, m.closedPositions)
	m.mu.RUnlock()

	todayStr := market.TodayDateIST()
	appendActivePosition := func(pos market.ActivePositionDTO) {
		if seenPosIDs[pos.PositionID] {
			return
		}
		if (modeFilter == "PAPER" || modeFilter == "LIVE") && pos.Mode != modeFilter {
			return
		}
		item := buildTradeHistoryFromDTO(m.db, ist, pos, todayStr, pos.Symbol, "NSE Cash Equity", 5.0)
		items = append(items, item)
		seenPosIDs[pos.PositionID] = true
	}

	for _, act := range activeSnapshots {
		appendActivePosition(act)
	}

	for _, cp := range memClosed {
		appendActivePosition(cp)
	}

	summary := computeTradeAnalyticsSummary(items)
	return &market.TradeHistoryResponseDTO{
		Summary: summary,
		Trades:  items,
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

	tb := CalculatePositionTaxBreakdown(buyPrice, sellPrice, qty, act.Tranche1Done)
	taxDTO := market.TaxBreakdownDTO{
		BuyBrokerage:  tb.BuyBrokerage,
		SellBrokerage: tb.SellBrokerage,
		Brokerage:     round2(tb.BuyBrokerage + tb.SellBrokerage),
		STT:           tb.STT,
		ExchangeTxn:   tb.ExchangeTxn,
		SEBICharges:   tb.SEBICharges,
		StampDuty:     tb.StampDuty,
		GST:           tb.GST,
		TotalCharges:  tb.TotalCharges,
	}

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
		Tranche1Done:          act.Tranche1Done,
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

	for _, t := range trades {
		if t.Quantity <= 0 {
			continue
		}
		sum.TotalTrades++
		sum.TotalInvestedTurnover += t.TotalInvested
		sum.TotalSellTurnover += t.TotalSellValue
		sum.TotalMarginDeployed += t.MarginBlocked
		sum.RealisedGrossPnL += t.GrossPnL
		sum.RealisedNetPnL += t.NetPnLAfterTax

		if t.GrossPnL >= 0 {
			sum.GrossProfitRupees += t.GrossPnL
		} else {
			sum.GrossLossRupees += t.GrossPnL
		}

		// Accumulate 6-part statutory tax breakdown
		sum.Taxes.BuyBrokerage += t.Taxes.BuyBrokerage
		sum.Taxes.SellBrokerage += t.Taxes.SellBrokerage
		sum.Taxes.Brokerage += t.Taxes.Brokerage
		sum.Taxes.STT += t.Taxes.STT
		sum.Taxes.ExchangeTxn += t.Taxes.ExchangeTxn
		sum.Taxes.SEBICharges += t.Taxes.SEBICharges
		sum.Taxes.StampDuty += t.Taxes.StampDuty
		sum.Taxes.GST += t.Taxes.GST
		sum.Taxes.TotalCharges += t.Taxes.TotalCharges

		if t.NetPnLAfterTax > 0.50 {
			sum.Wins++
			winRSum += t.RealisedRMultiple
			winRupeeSum += t.NetPnLAfterTax
			if t.NetPnLAfterTax > sum.MaxWinRupees {
				sum.MaxWinRupees = t.NetPnLAfterTax
			}
		} else if t.NetPnLAfterTax < -0.50 {
			sum.Losses++
			lossRSum += t.RealisedRMultiple
			lossRupeeSum += t.NetPnLAfterTax
			if t.NetPnLAfterTax < sum.MaxLossRupees {
				sum.MaxLossRupees = t.NetPnLAfterTax
			}
		} else {
			sum.BreakevenTrades++
		}

		totalR += t.RealisedRMultiple
		totalHoldSec += t.HoldDurationSec
		if t.Tranche1Done {
			t1Count++
		}
		if t.RCaptureEfficiencyPct > 0 {
			captureCount++
			captureSum += t.RCaptureEfficiencyPct
		}

		// Pattern attribution
		pat := t.PatternType
		if pat == "" {
			pat = "VWAP_PULLBACK_RECLAIM"
		}
		pStat, ok := patternMap[pat]
		if !ok {
			pStat = &market.PatternPerformanceDTO{PatternType: pat}
			patternMap[pat] = pStat
		}
		pStat.Trades++
		if t.NetPnLAfterTax > 0 {
			pStat.Wins++
		}
		pStat.AvgRMultiple += t.RealisedRMultiple
		pStat.GrossPnL += t.GrossPnL
		pStat.TotalTaxes += t.Taxes.TotalCharges
		pStat.NetPnLAfterTax += t.NetPnLAfterTax

		// Exit Reason attribution
		rsn := t.ExitReason
		rStat, ok := reasonMap[rsn]
		if !ok {
			rStat = &market.ExitReasonStatDTO{ExitReason: rsn}
			reasonMap[rsn] = rStat
		}
		rStat.Count++
		rStat.AvgRMultiple += t.RealisedRMultiple
		rStat.GrossPnL += t.GrossPnL
		rStat.TotalTaxes += t.Taxes.TotalCharges
		rStat.NetPnLAfterTax += t.NetPnLAfterTax
	}

	if sum.TotalTrades > 0 {
		n := float64(sum.TotalTrades)
		sum.WinRatePct = round2((float64(sum.Wins) / n) * 100.0)
		sum.ExpectancyPerTrade = round2(sum.RealisedNetPnL / n)
		sum.AvgRMultiple = round2(totalR / n)
		sum.AvgHoldDurationSec = totalHoldSec / int64(sum.TotalTrades)
		sum.Tranche1ScaleOutRate = round2((float64(t1Count) / n) * 100.0)
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

	for _, p := range patternMap {
		if p.Trades > 0 {
			p.WinRatePct = round2((float64(p.Wins) / float64(p.Trades)) * 100.0)
			p.AvgRMultiple = round2(p.AvgRMultiple / float64(p.Trades))
		}
		p.GrossPnL = round2(p.GrossPnL)
		p.TotalTaxes = round2(p.TotalTaxes)
		p.NetPnLAfterTax = round2(p.NetPnLAfterTax)
		sum.ByPattern = append(sum.ByPattern, *p)
	}
	slices.SortFunc(sum.ByPattern, func(a, b market.PatternPerformanceDTO) int {
		if a.Trades != b.Trades {
			return b.Trades - a.Trades
		}
		return strings.Compare(a.PatternType, b.PatternType)
	})

	for _, r := range reasonMap {
		if r.Count > 0 {
			r.AvgRMultiple = round2(r.AvgRMultiple / float64(r.Count))
		}
		r.GrossPnL = round2(r.GrossPnL)
		r.TotalTaxes = round2(r.TotalTaxes)
		r.NetPnLAfterTax = round2(r.NetPnLAfterTax)
		sum.ByExitReason = append(sum.ByExitReason, *r)
	}
	slices.SortFunc(sum.ByExitReason, func(a, b market.ExitReasonStatDTO) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.ExitReason, b.ExitReason)
	})

	return sum
}
