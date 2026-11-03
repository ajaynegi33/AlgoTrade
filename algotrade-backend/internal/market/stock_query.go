package market

import (
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/screener"
)

func (actor *Actor) sqliteBarWriter() {
	if actor.db == nil {
		return
	}

	insertSQL := `
		INSERT INTO historical_5m_bars (instrument_token, bar_time, open, high, low, close, volume)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instrument_token, bar_time) DO UPDATE SET
			high = max(high, excluded.high),
			low = min(low, excluded.low),
			close = excluded.close,
			volume = excluded.volume;
	`
	stmt, err := actor.db.Prepare(insertSQL)
	if err != nil {
		slog.Error("Failed to prepare bar flush statement", "error", err)
		return
	}
	defer stmt.Close()

	for {
		select {
		case <-actor.ctx.Done():
			return
		case item := <-actor.barFlushChan:
			barTime := time.Unix(item.Bar.Timestamp, 0).In(istZone).Format("2006-01-02 15:04:05")
			_, _ = stmt.Exec(
				item.Token,
				barTime,
				item.Bar.Open,
				item.Bar.High,
				item.Bar.Low,
				item.Bar.Close,
				item.Bar.Volume,
			)
		}
	}
}

// GetStockDetail returns the full quantitative and order book state for a specific stock (serialized via Actor loop)
func (actor *Actor) GetStockDetail(symbol string) (*StockDetailDTO, bool) {
	if !actor.isRunning.Load() {
		return actor.getStockDetailInternal(symbol)
	}
	reply := make(chan detailResp, 1)
	select {
	case actor.detailReqChan <- detailReq{symbol: symbol, reply: reply}:
		select {
		case res := <-reply:
			return res.dto, res.isFound
		case <-time.After(2 * time.Second):
			return nil, false
		}
	case <-actor.ctx.Done():
		return nil, false
	}
}

func (actor *Actor) getStockDetailInternal(symbol string) (*StockDetailDTO, bool) {
	upperSym := strings.ToUpper(strings.TrimSpace(symbol))

	targetIdx, found := actor.stockIndexBySymbol[upperSym]

	// Dynamic fallback: check SQLite scanned_stocks table in case stock was added dynamically
	if !found && actor.db != nil {
		var token uint32
		var tradingsymbol string
		var lotSize int
		var tickSize, lastPrice, misLeverage, misMarginPct float64
		err := actor.db.QueryRow(`
			SELECT instrument_token, tradingsymbol, lot_size, tick_size, last_price,
			       COALESCE(mis_leverage, 5.0), COALESCE(mis_margin_pct, 0.20)
			FROM scanned_stocks
			WHERE tradingsymbol = ?
			ORDER BY scan_date DESC LIMIT 1
		`, upperSym).Scan(&token, &tradingsymbol, &lotSize, &tickSize, &lastPrice, &misLeverage, &misMarginPct)
		if err == nil && token > 0 {
			secID, _, _ := screener.ResolveSector(actor.db, tradingsymbol)
			if tickSize <= 0 {
				tickSize = 0.05
			}
			targetIdx = actor.registerTrackedStock(token, tradingsymbol, secID, float32(tickSize))
			actor.stocks[targetIdx].LotSize = uint32(lotSize)
			actor.stocks[targetIdx].PrevClose = lastPrice
			if misLeverage > 0 {
				actor.stocks[targetIdx].MISLeverage = float32(misLeverage)
				if misMarginPct > 0 {
					actor.stocks[targetIdx].MISMarginFraction = float32(misMarginPct)
				} else {
					actor.stocks[targetIdx].MISMarginFraction = float32(1.0 / misLeverage)
				}
			} else {
				actor.stocks[targetIdx].MISLeverage = 5.0
				actor.stocks[targetIdx].MISMarginFraction = 0.20
			}
			actor.stocks[targetIdx].IsTradeable = true
			found = true
		}
	}

	if !found {
		return nil, false
	}

	stockMetrics := &actor.stocks[targetIdx]

	var chg, chgPct float64
	if stockMetrics.PrevClose > 0 && stockMetrics.LTP > 0 {
		chg = stockMetrics.LTP - stockMetrics.PrevClose
		chgPct = (chg / stockMetrics.PrevClose) * 100.0
	}

	var vwapDist, vwapDistPct float64
	if stockMetrics.ExchangeVWAP > 0 && stockMetrics.LTP > 0 {
		vwapDist = stockMetrics.LTP - stockMetrics.ExchangeVWAP
		vwapDistPct = (vwapDist / stockMetrics.ExchangeVWAP) * 100.0
	}

	openingRangeHigh, openingRangeLow, isRangeReady := stockMetrics.OpeningRange15m()

	// Compute intraday normalized Close Location Value oscillator [-1.0, +1.0] for StockFalsificationGrid.tsx
	// (-1 = Day Low, 0 = Midpoint, +1 = Day High; distinct from [0.0, 1.0] single-bar CLV used in math_ranking/gatekeeper)
	var closeLocationValueOscillator float64
	if stockMetrics.High > stockMetrics.Low {
		closeLocationValueOscillator = ((stockMetrics.LTP - stockMetrics.Low) - (stockMetrics.High - stockMetrics.LTP)) / (stockMetrics.High - stockMetrics.Low)
	}

	// Format session bars
	bars := make([]SessionBarDTO, 0, int(stockMetrics.CurrentBarIdx)+1)
	for candleIndex := 0; candleIndex <= int(stockMetrics.CurrentBarIdx) && candleIndex < 75; candleIndex++ {
		bar := stockMetrics.SessionBars[candleIndex]
		if bar.Open > 0 || bar.Close > 0 || bar.Volume > 0 {
			barTimeStr := time.Unix(bar.Timestamp, 0).In(istZone).Format("15:04")
			bars = append(bars, SessionBarDTO{
				Timestamp: bar.Timestamp,
				Time:      barTimeStr,
				Open:      bar.Open,
				High:      bar.High,
				Low:       bar.Low,
				Close:     bar.Close,
				Volume:    bar.Volume,
			})
		}
	}

	// Format 5-Level Depth
	bids := make([]DepthLevelDTO, 5)
	asks := make([]DepthLevelDTO, 5)
	var totalBidQty, totalAskQty uint64
	var bidTop2, bidRest3, askTop2, askRest3 uint64

	for lvl := 0; lvl < 5; lvl++ {
		bidLevel := stockMetrics.BidLevels[lvl]
		bids[lvl] = DepthLevelDTO{
			Price:  math.Round(float64(bidLevel.Price)*100.0) / 100.0,
			Qty:    bidLevel.Qty,
			Orders: bidLevel.Orders,
		}
		totalBidQty += uint64(bidLevel.Qty)
		if lvl < 2 {
			bidTop2 += uint64(bidLevel.Qty)
		} else {
			bidRest3 += uint64(bidLevel.Qty)
		}

		askLevel := stockMetrics.AskLevels[lvl]
		asks[lvl] = DepthLevelDTO{
			Price:  math.Round(float64(askLevel.Price)*100.0) / 100.0,
			Qty:    askLevel.Qty,
			Orders: askLevel.Orders,
		}
		totalAskQty += uint64(askLevel.Qty)
		if lvl < 2 {
			askTop2 += uint64(askLevel.Qty)
		} else {
			askRest3 += uint64(askLevel.Qty)
		}
	}

	antiSpoofRatio := ComputeAntiSpoofRatio(bidTop2, bidRest3, askTop2, askRest3)

	// Decode reasons
	reasonsList := make([]string, 0)
	if stockMetrics.Reasons&ReasonVWAPSweetSpot != 0 {
		reasonsList = append(reasonsList, "VWAP Sweet Spot")
	}
	if stockMetrics.Reasons&ReasonRVOLSurge != 0 {
		reasonsList = append(reasonsList, "High RVOL Volume Surge")
	}
	if stockMetrics.Reasons&ReasonSectorLeader != 0 {
		reasonsList = append(reasonsList, "Leading Sector Tailwinds")
	}
	if stockMetrics.Reasons&ReasonOrderBookWall != 0 {
		reasonsList = append(reasonsList, "Anti-Spoof Order Depth Wall")
	}
	if stockMetrics.Reasons&ReasonORBBreakout != 0 {
		reasonsList = append(reasonsList, "15m Opening Range Breakout")
	}
	if stockMetrics.Reasons&ReasonHighEV != 0 {
		reasonsList = append(reasonsList, "High Expected Value (EV ≥ +0.40R)")
	}
	if stockMetrics.Reasons&ReasonBearishBreakdown != 0 {
		reasonsList = append(reasonsList, "Bearish Breakdown Below VWAP")
	}
	if stockMetrics.Reasons&ReasonSectorLagging != 0 {
		reasonsList = append(reasonsList, "Lagging Sector Drag")
	}
	if stockMetrics.Reasons&PenaltyWideSpread != 0 {
		reasonsList = append(reasonsList, "Penalty: Wide Bid-Ask Spread (>0.10%)")
	}
	if stockMetrics.Reasons&PenaltyBelowPrevClose != 0 {
		reasonsList = append(reasonsList, "Penalty: Trading Below Previous Close")
	}
	if stockMetrics.Reasons&PenaltyOverextended != 0 {
		reasonsList = append(reasonsList, "Penalty: Overextended > 1.5 ATR From VWAP")
	}
	if stockMetrics.Reasons&PenaltyLowVolume != 0 {
		reasonsList = append(reasonsList, "Penalty: Dry Volume (<1.0x RVOL)")
	}
	if stockMetrics.Reasons&PenaltyPullbackRedBar != 0 {
		reasonsList = append(reasonsList, "Penalty: Red Pullback Bar Unreclaimed")
	}
	if stockMetrics.Reasons&PenaltyExhaustedTrend != 0 {
		reasonsList = append(reasonsList, "Penalty: 3rd+ Late-Stage Pullback")
	}
	if stockMetrics.Reasons&PenaltyIcebergSell != 0 {
		reasonsList = append(reasonsList, "Penalty: Iceberg Sell Distribution")
	}

	dto := &StockDetailDTO{
		Symbol:            stockMetrics.SymbolString(),
		Token:             stockMetrics.InstrumentToken,
		Sector:            screener.GetSectorName(stockMetrics.SectorID),
		SectorID:          stockMetrics.SectorID,
		LotSize:           stockMetrics.LotSize,
		TickSize:          stockMetrics.TickSize,
		IsTradeable:       stockMetrics.IsTradeable,
		LTP:               stockMetrics.LTP,
		Open:              stockMetrics.Open,
		High:              stockMetrics.High,
		Low:               stockMetrics.Low,
		PrevClose:         stockMetrics.PrevClose,
		Change:            chg,
		ChangePercent:     chgPct,
		VWAP:              stockMetrics.ExchangeVWAP,
		EMA9:              stockMetrics.EMA9,
		EMA9Slope:         stockMetrics.EMA9Slope,
		VWAPDistance:      vwapDist,
		VWAPDistancePct:   vwapDistPct,
		ORH:               openingRangeHigh,
		ORL:               openingRangeLow,
		HasOR15m:          isRangeReady,
		CLV:               closeLocationValueOscillator,
		ATR14:             stockMetrics.ATR14,
		ADX14:             stockMetrics.ADX14,
		Volume:            stockMetrics.Volume,
		RVOLSession:       stockMetrics.RVOLSession,
		RVOLPace5m:        stockMetrics.RVOLPace5m,
		BullishScore:      stockMetrics.BullishScore,
		BearishScore:      stockMetrics.BearishScore,
		CompositeRS:       stockMetrics.CompositeRS,
		BuyerRatio:        stockMetrics.SmoothedBuyerRatio,
		AggressorBuyRatio: stockMetrics.AggressorBuyRatio,
		EV:                stockMetrics.EV,
		PWin:              stockMetrics.PWin,
		StopProxy:         stockMetrics.StopProxy,
		TargetProxy:       stockMetrics.TargetProxy,
		Reasons:           reasonsList,
		Bids:              bids,
		Asks:              asks,
		TotalBidQty:       totalBidQty,
		TotalAskQty:       totalAskQty,
		AntiSpoofRatio:    antiSpoofRatio,
		Bars:              bars,
		LastUpdated:       stockMetrics.LastUpdatedUnix,
	}

	return dto, true
}

