package engine

import (
	"math"
	"slices"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/scanner"
)

// computeAndBroadcastSnapshot runs on the 1-second ticker to calculate breadth, regime, and ranking,
// invoke the execution hook, and broadcast RankedSnapshot to connected WebSockets.
func (a *Actor) computeAndBroadcastSnapshot() {
	nowUnix := time.Now().Unix()

	// 1. Calculate Market Breadth (% > VWAP)
	var aboveVWAPCount, activeQuotedCount int
	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		if s.LTP > 0 && s.ExchangeVWAP > 0 {
			activeQuotedCount++
			if s.LTP > s.ExchangeVWAP {
				aboveVWAPCount++
			}
		}
	}
	var breadthRatio float64 = 0.5
	if activeQuotedCount > 0 {
		breadthRatio = float64(aboveVWAPCount) / float64(activeQuotedCount)
	}

	prevBreadth5s := a.breadthHist[a.breadthHistIdx]
	if prevBreadth5s == 0 {
		prevBreadth5s = breadthRatio
	}
	a.breadthHist[a.breadthHistIdx] = breadthRatio
	a.breadthHistIdx = (a.breadthHistIdx + 1) % 6
	breadthDrop5s := breadthRatio - prevBreadth5s

	// 2. Market Pulse & Multi-Horizon Dynamic Regime Context
	var niftyLTP, niftyChg, bankLTP, bankChg float64
	var niftyIntradayRet, niftyDayCLV, niftyVel15m, niftyShockDrop float64
	niftyDayCLV = 0.50
	var niftyStock *StockMetrics
	if a.hasNifty {
		niftyStock = &a.stocks[a.niftyIdx]
		niftyLTP = niftyStock.LTP
		if niftyStock.PrevClose > 0 && niftyLTP > 0 {
			niftyChg = ((niftyLTP - niftyStock.PrevClose) / niftyStock.PrevClose) * 100.0
		}
		if niftyStock.Open > 0 && niftyLTP > 0 {
			niftyIntradayRet = ((niftyLTP - niftyStock.Open) / niftyStock.Open) * 100.0
		}
		if niftyStock.High > niftyStock.Low && niftyLTP > 0 {
			niftyDayCLV = (niftyLTP - niftyStock.Low) / (niftyStock.High - niftyStock.Low)
		}
		lookback3 := 0
		if niftyStock.CurrentBarIdx >= 3 {
			lookback3 = int(niftyStock.CurrentBarIdx) - 3
		}
		ref15m := niftyStock.SessionBars[lookback3].Close
		if ref15m <= 0 {
			ref15m = niftyStock.Open
		}
		if ref15m > 0 && niftyLTP > 0 {
			niftyVel15m = ((niftyLTP - ref15m) / ref15m) * 100.0
		}
		if niftyStock.RollingPeak > 0 && niftyLTP > 0 {
			niftyShockDrop = ((niftyLTP - niftyStock.RollingPeak) / niftyStock.RollingPeak) * 100.0
		}
	}
	if a.hasBankNifty {
		b := &a.stocks[a.bankNiftyIdx]
		bankLTP = b.LTP
		if b.PrevClose > 0 && bankLTP > 0 {
			bankChg = ((bankLTP - b.PrevClose) / b.PrevClose) * 100.0
		}
	}

	// Update FLASH_SHOCK state
	if niftyShockDrop <= -0.18 || (breadthDrop5s <= -0.12 && niftyVel15m <= -0.20) {
		a.macroShockExp = nowUnix + 180
		a.inMacroShock.Store(true)
	} else if a.inMacroShock.Load() {
		if nowUnix >= a.macroShockExp || (niftyVel15m >= 0.05 && breadthRatio >= 0.52) {
			a.inMacroShock.Store(false)
		}
	}

	// 6-State Dynamic Intraday Regime Classification
	regime := "SIDEWAYS_CHOP"
	switch {
	case a.inMacroShock.Load():
		regime = "FLASH_SHOCK"
	case niftyDayCLV < 0.35 && niftyVel15m <= -0.18 && breadthRatio < 0.48:
		// Gap-up rollover / afternoon institutional distribution
		regime = "INTRADAY_DISTRIBUTION"
	case breadthRatio <= 0.42 && (niftyIntradayRet < -0.20 || niftyChg < -0.35) && niftyDayCLV < 0.40:
		regime = "BEARISH_EXPANSION"
	case niftyChg < 0 && niftyIntradayRet >= 0.25 && niftyDayCLV >= 0.65 && breadthRatio >= 0.58 && niftyVel15m >= 0.10:
		// Gap-down followed by strong intraday DII/FII V-recovery
		regime = "V_RECOVERY_BULLISH"
	case breadthRatio >= 0.58 && (niftyIntradayRet >= 0.15 || niftyChg >= 0.25) && niftyDayCLV >= 0.55:
		regime = "BULLISH_EXPANSION"
	}

	// 3. Compute Sector Performance & Identify Leaders
	type secPerf struct {
		id       uint8
		chg      float64
		breadth  float64
		totCount int
		abvCount int
	}
	secStats := make(map[uint8]*secPerf)
	for id := range scanner.SectorRegistry {
		secStats[id] = &secPerf{id: id}
	}

	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		if p, ok := secStats[s.SectorID]; ok {
			p.totCount++
			if s.LTP > s.ExchangeVWAP && s.ExchangeVWAP > 0 {
				p.abvCount++
			}
		}
	}

	// Build deterministic list of sectors (SectorAuto=2 through SectorInfra=12)
	sectorDTOs := make([]SectorRankDTO, 0, 11)
	for id := uint8(2); id <= 12; id++ {
		p := secStats[id]
		if p == nil {
			continue
		}
		if p.totCount > 0 {
			p.breadth = (float64(p.abvCount) / float64(p.totCount)) * 100.0
		}
		var secChg float64
		if sIdx, ok := a.sectorIndices[id]; ok {
			st := &a.stocks[sIdx]
			if st.PrevClose > 0 && st.LTP > 0 {
				secChg = ((st.LTP - st.PrevClose) / st.PrevClose) * 100.0
			}
		} else if id == scanner.SectorBank && a.hasBankNifty {
			b := &a.stocks[a.bankNiftyIdx]
			if b.PrevClose > 0 && b.LTP > 0 {
				secChg = ((b.LTP - b.PrevClose) / b.PrevClose) * 100.0
			}
		}
		p.chg = secChg

		sectorDTOs = append(sectorDTOs, SectorRankDTO{
			SectorID:         id,
			SectorName:       scanner.GetSectorName(id),
			ChangePercent:    secChg,
			BreadthAboveVWAP: p.breadth,
		})
	}

	// Deterministic Sort: Primary by ChangePercent DESC, Secondary tie-breaker by SectorID ASC
	slices.SortFunc(sectorDTOs, func(a, b SectorRankDTO) int {
		if a.ChangePercent > b.ChangePercent {
			return -1
		} else if a.ChangePercent < b.ChangePercent {
			return 1
		}
		// Deterministic tie-breaker prevents UI pills from shuffling when change is 0.00%
		if a.SectorID < b.SectorID {
			return -1
		} else if a.SectorID > b.SectorID {
			return 1
		}
		return 0
	})

	leaderMap := make(map[uint8]bool, 2)
	laggingMap := make(map[uint8]bool, 2)

	// Only assign leaders/laggards if there is real price movement
	hasMovement := false
	for _, s := range sectorDTOs {
		if s.ChangePercent != 0.0 {
			hasMovement = true
			break
		}
	}

	if hasMovement {
		for i := range sectorDTOs {
			if i < 2 && sectorDTOs[i].ChangePercent > 0 {
				sectorDTOs[i].IsLeader = true
				leaderMap[sectorDTOs[i].SectorID] = true
			}
			if i >= len(sectorDTOs)-2 && len(sectorDTOs) >= 4 && sectorDTOs[i].ChangePercent < 0 {
				sectorDTOs[i].IsLagging = true
				laggingMap[sectorDTOs[i].SectorID] = true
			}
		}
	}

	// 4. Quant Score Calculation for all active equities
	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		var secStock *StockMetrics
		if sIdx, ok := a.sectorIndices[s.SectorID]; ok {
			secStock = &a.stocks[sIdx]
		} else if s.SectorID == scanner.SectorBank && a.hasBankNifty {
			secStock = &a.stocks[a.bankNiftyIdx]
		}
		isLeader := leaderMap[s.SectorID]
		isLagging := laggingMap[s.SectorID]

		CalculateBullishScore(s, niftyStock, secStock, isLeader, breadthRatio)
		CalculateBearishScore(s, niftyStock, secStock, isLagging, breadthRatio)
	}

	// 5. Quicksort Active Indices by BullishScore (~2 microseconds)
	bullishSorted := make([]uint16, len(a.activeIndices))
	copy(bullishSorted, a.activeIndices)
	slices.SortFunc(bullishSorted, func(i, j uint16) int {
		s1 := a.stocks[i].BullishScore
		s2 := a.stocks[j].BullishScore
		if s1 > s2 {
			return -1
		} else if s1 < s2 {
			return 1
		}
		return 0
	})

	pulseDTO := MarketPulseDTO{
		MarketState:            "OPEN",
		NiftyLTP:               niftyLTP,
		NiftyChangePercent:     niftyChg,
		BankNiftyLTP:           bankLTP,
		BankNiftyChangePercent: bankChg,
		BreadthAboveVWAP:       math.Round(breadthRatio*1000.0) / 10.0,
		MarketRegime:           regime,
	}

	// Invoke Phase 2 Autonomous Execution Engine every 1.0s (even when 0 browser tabs are open!)
	var execState *ExecutionStateDTO
	if a.execHook != nil {
		execState = a.execHook.OnSecondTick(&a.stocks, a.activeIndices, bullishSorted, leaderMap, pulseDTO, a.inMacroShock.Load())
	}

	if a.wsHub == nil || a.wsHub.ClientCount() == 0 {
		return // Zero connected clients: skip DTO allocations & WebSocket broadcast
	}

	// Bearish Sort for Top 5 Bearish Outliers
	bearishSorted := make([]uint16, len(a.activeIndices))
	copy(bearishSorted, a.activeIndices)
	slices.SortFunc(bearishSorted, func(i, j uint16) int {
		s1 := a.stocks[i].BearishScore
		s2 := a.stocks[j].BearishScore
		if s1 > s2 {
			return -1
		} else if s1 < s2 {
			return 1
		}
		return 0
	})

	// Build Top 5 Bullish Outliers
	topBullish := make([]StockRankDTO, 0, 5)
	for rk, idx := range bullishSorted {
		if rk >= 5 {
			break
		}
		dto := a.toStockDTO(&a.stocks[idx], rk+1)
		topBullish = append(topBullish, dto)
	}

	// Build Top 5 Bearish Outliers
	topBearish := make([]StockRankDTO, 0, 5)
	for rk, idx := range bearishSorted {
		if rk >= 5 {
			break
		}
		dto := a.toStockDTO(&a.stocks[idx], rk+1)
		topBearish = append(topBearish, dto)
	}

	// Build Full 213 Watchlist Table
	allStocks := make([]StockRankDTO, len(bullishSorted))
	for rk, idx := range bullishSorted {
		allStocks[rk] = a.toStockDTO(&a.stocks[idx], rk+1)
	}

	// Historical API Status
	histHealthy, histErr := a.histClient.IsHealthy()

	snapshot := RankedSnapshot{
		Timestamp:        nowUnix,
		HistoricalActive: histHealthy,
		HistoricalError:  histErr,
		MarketPulse:      pulseDTO,
		Sectors:          sectorDTOs,
		TopBullish:       topBullish,
		TopBearish:       topBearish,
		AllStocks:        allStocks,
		Execution:        execState,
	}

	a.wsHub.Broadcast(snapshot)
}

func (a *Actor) toStockDTO(s *StockMetrics, rank int) StockRankDTO {
	var chgPercent float64
	if s.PrevClose > 0 {
		chgPercent = ((s.LTP - s.PrevClose) / s.PrevClose) * 100.0
	}

	return StockRankDTO{
		Rank:              rank,
		Token:             s.InstrumentToken,
		Symbol:            s.SymbolString(),
		Sector:            scanner.GetSectorName(s.SectorID),
		SectorID:          s.SectorID,
		LTP:               s.LTP,
		ChangePercent:     math.Round(chgPercent*100.0) / 100.0,
		VWAP:              s.ExchangeVWAP,
		BullishScore:      math.Round(s.BullishScore*10.0) / 10.0,
		BearishScore:      math.Round(s.BearishScore*10.0) / 10.0,
		RVOLSession:       math.Round(s.RVOLSession*10.0) / 10.0,
		RVOLPace5m:        math.Round(s.RVOLPace5m*10.0) / 10.0,
		CompositeRS:       math.Round(s.CompositeRS*100.0) / 100.0,
		BuyerRatio:        float32(math.Round(float64(s.SmoothedBuyerRatio)*10000.0)) / 10000.0,
		AggressorBuyRatio: float32(math.Round(float64(s.AggressorBuyRatio)*10000.0)) / 10000.0,
		EV:                math.Round(s.EV*100.0) / 100.0,
		PWin:              math.Round(s.PWin*100.0) / 100.0,
		StopProxy:         s.StopProxy,
		TargetProxy:       s.TargetProxy,
		Reasons:           uint16(s.Reasons),
		PullbackCount:     s.PullbackCount,
	}
}

