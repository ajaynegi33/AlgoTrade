package market

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/screener"
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

	// 2. Market Regime Context (Derived purely from universe breadth)
	regime := "SIDEWAYS_CHOP"
	switch {
	case breadthRatio >= 0.58:
		regime = "BULLISH_EXPANSION"
	case breadthRatio <= 0.40:
		regime = "BEARISH_EXPANSION"
	}

	// 3. Compute Sector Performance & Sector Restrictions (Granular bottom-up from tradeable stocks)
	type secPerf struct {
		id       uint8
		sumChg   float64
		totCount int
		abvCount int
	}
	secStats := make(map[uint8]*secPerf, 16)
	for id := range screener.SectorRegistry {
		secStats[id] = &secPerf{id: id}
	}

	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		if p, ok := secStats[s.SectorID]; ok {
			p.totCount++
			if s.LTP > s.ExchangeVWAP && s.ExchangeVWAP > 0 {
				p.abvCount++
			}
			if s.PrevClose > 0 && s.LTP > 0 {
				p.sumChg += ((s.LTP - s.PrevClose) / s.PrevClose) * 100.0
			}
		}
	}

	// Build deterministic list of sectors (SectorAuto=2 through SectorInfra=12)
	sectorDTOs := make([]SectorRankDTO, 0, 11)
	newSectorHealthMap := make(map[uint8]SectorHealth, 16)

	for id := uint8(2); id <= 12; id++ {
		p := secStats[id]
		if p == nil {
			continue
		}
		var breadth, avgChg float64
		if p.totCount > 0 {
			breadth = (float64(p.abvCount) / float64(p.totCount)) * 100.0
			avgChg = p.sumChg / float64(p.totCount)
		}
		secName := screener.GetSectorName(id)

		// Granular Sector Circuit Breaker:
		// If more than 60% of stocks in sector are below VWAP (breadth < 40%) OR average change < -0.5%
		isRestricted := false
		var restrictionReason string
		if p.totCount >= 2 {
			if breadth < 40.0 {
				isRestricted = true
				restrictionReason = fmt.Sprintf("Weak Breadth (%.0f%% > VWAP)", breadth)
			} else if avgChg < -0.50 {
				isRestricted = true
				restrictionReason = fmt.Sprintf("Heavy Selling Pressure (Avg %.2f%%)", avgChg)
			}
		}

		sectorDTOs = append(sectorDTOs, SectorRankDTO{
			SectorID:          id,
			SectorName:        secName,
			ChangePercent:     avgChg,
			BreadthAboveVWAP:  breadth,
			IsRestricted:      isRestricted,
			RestrictionReason: restrictionReason,
		})

		newSectorHealthMap[id] = SectorHealth{
			SectorID:          id,
			SectorName:        secName,
			AverageChange:     avgChg,
			BreadthAboveVWAP:  breadth,
			IsRestricted:      isRestricted,
			RestrictionReason: restrictionReason,
		}
	}
	a.sectorHealthMap = newSectorHealthMap

	// Deterministic Sort: Primary by ChangePercent DESC, Secondary tie-breaker by SectorID ASC
	slices.SortFunc(sectorDTOs, func(a, b SectorRankDTO) int {
		if a.ChangePercent > b.ChangePercent {
			return -1
		} else if a.ChangePercent < b.ChangePercent {
			return 1
		}
		if a.SectorID < b.SectorID {
			return -1
		} else if a.SectorID > b.SectorID {
			return 1
		}
		return 0
	})

	for i := range sectorDTOs {
		if i < 2 && sectorDTOs[i].ChangePercent > 0 {
			sectorDTOs[i].IsLeader = true
		}
		if i >= len(sectorDTOs)-2 && len(sectorDTOs) >= 4 && sectorDTOs[i].ChangePercent < 0 {
			sectorDTOs[i].IsLagging = true
		}
	}

	// 4. Quant Score Calculation for all active equities
	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		CalculateBullishScore(s, breadthRatio)
		CalculateBearishScore(s, breadthRatio)
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
		MarketState:      "OPEN",
		BreadthAboveVWAP: math.Round(breadthRatio*1000.0) / 10.0,
		MarketRegime:     regime,
	}

	// Invoke Phase 2 Autonomous Execution Engine every 1.0s
	var execState *ExecutionStateDTO
	if a.execHook != nil {
		execState = a.execHook.OnSecondTick(&a.stocks, a.activeIndices, bullishSorted, a.sectorHealthMap, pulseDTO)
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
		Sector:            screener.GetSectorName(s.SectorID),
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

