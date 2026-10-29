package market

import (
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/screener"
)

func (a *Actor) sqliteBarWriter() {
	if a.db == nil {
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
	stmt, err := a.db.Prepare(insertSQL)
	if err != nil {
		slog.Error("Failed to prepare bar flush statement", "error", err)
		return
	}
	defer stmt.Close()

	for {
		select {
		case <-a.ctx.Done():
			return
		case item := <-a.barFlushChan:
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
func (a *Actor) GetStockDetail(symbol string) (*StockDetailDTO, bool) {
	if !a.isRunning.Load() {
		return a.getStockDetailInternal(symbol)
	}
	reply := make(chan detailResp, 1)
	select {
	case a.detailReqChan <- detailReq{symbol: symbol, reply: reply}:
		select {
		case res := <-reply:
			return res.dto, res.found
		case <-time.After(2 * time.Second):
			return nil, false
		}
	case <-a.ctx.Done():
		return nil, false
	}
}

func (a *Actor) getStockDetailInternal(symbol string) (*StockDetailDTO, bool) {
	upperSym := strings.ToUpper(strings.TrimSpace(symbol))

	targetIdx, found := a.symbolMap[upperSym]

	// Dynamic fallback: check SQLite scanned_stocks table in case stock was added dynamically
	if !found && a.db != nil {
		var token uint32
		var tradingsymbol string
		var lotSize int
		var tickSize, lastPrice float64
		err := a.db.QueryRow(`
			SELECT instrument_token, tradingsymbol, lot_size, tick_size, last_price
			FROM scanned_stocks
			WHERE tradingsymbol = ?
			ORDER BY scan_date DESC LIMIT 1
		`, upperSym).Scan(&token, &tradingsymbol, &lotSize, &tickSize, &lastPrice)
		if err == nil && token > 0 {
			secID, _, _ := screener.ResolveSector(a.db, tradingsymbol)
			if tickSize <= 0 {
				tickSize = 0.05
			}
			targetIdx = a.addStock(token, tradingsymbol, secID, float32(tickSize))
			a.stocks[targetIdx].LotSize = uint32(lotSize)
			a.stocks[targetIdx].PrevClose = lastPrice
			a.stocks[targetIdx].IsTradeable = true
			found = true
		}
	}

	if !found {
		return nil, false
	}

	s := &a.stocks[targetIdx]

	var chg, chgPct float64
	if s.PrevClose > 0 && s.LTP > 0 {
		chg = s.LTP - s.PrevClose
		chgPct = (chg / s.PrevClose) * 100.0
	}

	var vwapDist, vwapDistPct float64
	if s.ExchangeVWAP > 0 && s.LTP > 0 {
		vwapDist = s.LTP - s.ExchangeVWAP
		vwapDistPct = (vwapDist / s.ExchangeVWAP) * 100.0
	}

	orh, orl, hasOR := s.OpeningRange15m()

	// Compute intraday normalized Close Location Value oscillator [-1.0, +1.0] for StockFalsificationGrid.tsx
	// (-1 = Day Low, 0 = Midpoint, +1 = Day High; distinct from [0.0, 1.0] single-bar CLV used in math_ranking/gatekeeper)
	var clv float64
	if s.High > s.Low {
		clv = ((s.LTP - s.Low) - (s.High - s.LTP)) / (s.High - s.Low)
	}

	// Format session bars
	bars := make([]SessionBarDTO, 0, int(s.CurrentBarIdx)+1)
	for i := 0; i <= int(s.CurrentBarIdx) && i < 75; i++ {
		b := s.SessionBars[i]
		if b.Open > 0 || b.Close > 0 || b.Volume > 0 {
			barTimeStr := time.Unix(b.Timestamp, 0).In(istZone).Format("15:04")
			bars = append(bars, SessionBarDTO{
				Timestamp: b.Timestamp,
				Time:      barTimeStr,
				Open:      b.Open,
				High:      b.High,
				Low:       b.Low,
				Close:     b.Close,
				Volume:    b.Volume,
			})
		}
	}

	// Format 5-Level Depth
	bids := make([]DepthLevelDTO, 5)
	asks := make([]DepthLevelDTO, 5)
	var totalBidQty, totalAskQty uint64
	var bidTop2, bidRest3, askTop2, askRest3 uint64

	for lvl := 0; lvl < 5; lvl++ {
		b := s.BidLevels[lvl]
		bids[lvl] = DepthLevelDTO{
			Price:  math.Round(float64(b.Price)*100.0) / 100.0,
			Qty:    b.Qty,
			Orders: b.Orders,
		}
		totalBidQty += uint64(b.Qty)
		if lvl < 2 {
			bidTop2 += uint64(b.Qty)
		} else {
			bidRest3 += uint64(b.Qty)
		}

		aL := s.AskLevels[lvl]
		asks[lvl] = DepthLevelDTO{
			Price:  math.Round(float64(aL.Price)*100.0) / 100.0,
			Qty:    aL.Qty,
			Orders: aL.Orders,
		}
		totalAskQty += uint64(aL.Qty)
		if lvl < 2 {
			askTop2 += uint64(aL.Qty)
		} else {
			askRest3 += uint64(aL.Qty)
		}
	}

	antiSpoofRatio := ComputeAntiSpoofRatio(bidTop2, bidRest3, askTop2, askRest3)

	// Decode reasons
	reasonsList := make([]string, 0)
	if s.Reasons&ReasonVWAPSweetSpot != 0 {
		reasonsList = append(reasonsList, "VWAP Sweet Spot")
	}
	if s.Reasons&ReasonRVOLSurge != 0 {
		reasonsList = append(reasonsList, "High RVOL Volume Surge")
	}
	if s.Reasons&ReasonSectorLeader != 0 {
		reasonsList = append(reasonsList, "Leading Sector Tailwinds")
	}
	if s.Reasons&ReasonOrderBookWall != 0 {
		reasonsList = append(reasonsList, "Anti-Spoof Order Depth Wall")
	}
	if s.Reasons&ReasonORBBreakout != 0 {
		reasonsList = append(reasonsList, "15m Opening Range Breakout")
	}
	if s.Reasons&ReasonHighEV != 0 {
		reasonsList = append(reasonsList, "High Expected Value (EV ≥ +0.40R)")
	}
	if s.Reasons&ReasonBearishBreakdown != 0 {
		reasonsList = append(reasonsList, "Bearish Breakdown Below VWAP")
	}
	if s.Reasons&ReasonSectorLagging != 0 {
		reasonsList = append(reasonsList, "Lagging Sector Drag")
	}
	if s.Reasons&PenaltyWideSpread != 0 {
		reasonsList = append(reasonsList, "Penalty: Wide Bid-Ask Spread (>0.10%)")
	}
	if s.Reasons&PenaltyBelowPrevClose != 0 {
		reasonsList = append(reasonsList, "Penalty: Trading Below Previous Close")
	}
	if s.Reasons&PenaltyOverextended != 0 {
		reasonsList = append(reasonsList, "Penalty: Overextended > 1.5 ATR From VWAP")
	}
	if s.Reasons&PenaltyLowVolume != 0 {
		reasonsList = append(reasonsList, "Penalty: Dry Volume (<1.0x RVOL)")
	}
	if s.Reasons&PenaltyPullbackRedBar != 0 {
		reasonsList = append(reasonsList, "Penalty: Red Pullback Bar Unreclaimed")
	}
	if s.Reasons&PenaltyExhaustedTrend != 0 {
		reasonsList = append(reasonsList, "Penalty: 3rd+ Late-Stage Pullback")
	}
	if s.Reasons&PenaltyIcebergSell != 0 {
		reasonsList = append(reasonsList, "Penalty: Iceberg Sell Distribution")
	}

	dto := &StockDetailDTO{
		Symbol:            s.SymbolString(),
		Token:             s.InstrumentToken,
		Sector:            screener.GetSectorName(s.SectorID),
		SectorID:          s.SectorID,
		LotSize:           s.LotSize,
		TickSize:          s.TickSize,
		IsTradeable:       s.IsTradeable,
		LTP:               s.LTP,
		Open:              s.Open,
		High:              s.High,
		Low:               s.Low,
		PrevClose:         s.PrevClose,
		Change:            chg,
		ChangePercent:     chgPct,
		VWAP:              s.ExchangeVWAP,
		VWAPDistance:      vwapDist,
		VWAPDistancePct:   vwapDistPct,
		ORH:               orh,
		ORL:               orl,
		HasOR15m:          hasOR,
		CLV:               clv,
		ATR14:             s.ATR14,
		ADX14:             s.ADX14,
		Volume:            s.Volume,
		RVOLSession:       s.RVOLSession,
		RVOLPace5m:        s.RVOLPace5m,
		BullishScore:      s.BullishScore,
		BearishScore:      s.BearishScore,
		CompositeRS:       s.CompositeRS,
		BuyerRatio:        s.SmoothedBuyerRatio,
		AggressorBuyRatio: s.AggressorBuyRatio,
		EV:                s.EV,
		PWin:              s.PWin,
		StopProxy:         s.StopProxy,
		TargetProxy:       s.TargetProxy,
		Reasons:           reasonsList,
		Bids:              bids,
		Asks:              asks,
		TotalBidQty:       totalBidQty,
		TotalAskQty:       totalAskQty,
		AntiSpoofRatio:    antiSpoofRatio,
		Bars:              bars,
		LastUpdated:       s.LastUpdatedUnix,
	}

	return dto, true
}

