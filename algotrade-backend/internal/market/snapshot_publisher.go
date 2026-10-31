package market

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/diagnostics"
	"github.com/ajaynegi33/AlgoTrade/internal/screener"
)

// computeAndBroadcastSnapshot runs on the 1-second ticker to calculate breadth, regime, and ranking,
// invoke the execution hook, and broadcast RankedSnapshot to connected WebSockets.
func (actor *Actor) computeAndBroadcastSnapshot() {
	computationStartTime := time.Now()
	defer func() {
		computationDuration := time.Since(computationStartTime)
		diagnostics.RecordLatency("market_snapshot_broadcast", computationDuration, 10*time.Millisecond)
	}()

	nowUnix := computationStartTime.Unix()

	// 1. Calculate Market Breadth (% > VWAP)
	var aboveVWAPCount, activeQuotedCount int
	for _, stockIndex := range actor.activeStockIndices {
		stockMetric := &actor.stocks[stockIndex]
		if stockMetric.LTP > 0 && stockMetric.ExchangeVWAP > 0 {
			activeQuotedCount++
			if stockMetric.LTP > stockMetric.ExchangeVWAP {
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
	sectorStatisticsByID := make(map[uint8]*secPerf, 16)
	for id := range screener.SectorRegistry {
		sectorStatisticsByID[id] = &secPerf{id: id}
	}

	for _, stockIndex := range actor.activeStockIndices {
		stockMetric := &actor.stocks[stockIndex]
		if sectorPerformance, ok := sectorStatisticsByID[stockMetric.SectorID]; ok {
			sectorPerformance.totCount++
			if stockMetric.LTP > stockMetric.ExchangeVWAP && stockMetric.ExchangeVWAP > 0 {
				sectorPerformance.abvCount++
			}
			if stockMetric.PrevClose > 0 && stockMetric.LTP > 0 {
				sectorPerformance.sumChg += ((stockMetric.LTP - stockMetric.PrevClose) / stockMetric.PrevClose) * 100.0
			}
		}
	}

	// Build deterministic list of sectors (SectorAuto=2 through SectorInfra=12)
	sectorRankings := make([]SectorRankDTO, 0, 11)
	newSectorHealthMap := make(map[uint8]SectorHealth, 16)

	for id := uint8(2); id <= 12; id++ {
		sectorPerformance := sectorStatisticsByID[id]
		if sectorPerformance == nil {
			continue
		}
		var breadth, avgChg float64
		if sectorPerformance.totCount > 0 {
			breadth = (float64(sectorPerformance.abvCount) / float64(sectorPerformance.totCount)) * 100.0
			avgChg = sectorPerformance.sumChg / float64(sectorPerformance.totCount)
		}
		secName := screener.GetSectorName(id)

		// Granular Sector Circuit Breaker:
		// If more than 60% of stocks in sector are below VWAP (breadth < 40%) OR average change < -0.5%
		isRestricted := false
		var restrictionReason string
		if sectorPerformance.totCount >= 2 {
			if breadth < 40.0 {
				isRestricted = true
				restrictionReason = fmt.Sprintf("Weak Breadth (%.0f%% > VWAP)", breadth)
			} else if avgChg < -0.50 {
				isRestricted = true
				restrictionReason = fmt.Sprintf("Heavy Selling Pressure (Avg %.2f%%)", avgChg)
			}
		}

		sectorRankings = append(sectorRankings, SectorRankDTO{
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
	actor.sectorHealthMap = newSectorHealthMap

	// Deterministic Sort: Primary by ChangePercent DESC, Secondary tie-breaker by SectorID ASC
	slices.SortFunc(sectorRankings, func(a, b SectorRankDTO) int {
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

	for i := range sectorRankings {
		if i < 2 && sectorRankings[i].ChangePercent > 0 {
			sectorRankings[i].IsLeader = true
		}
		if i >= len(sectorRankings)-2 && len(sectorRankings) >= 4 && sectorRankings[i].ChangePercent < 0 {
			sectorRankings[i].IsLagging = true
		}
	}

	// 4. Quant Score Calculation for all active equities
	for _, stockIndex := range actor.activeStockIndices {
		stockMetric := &actor.stocks[stockIndex]
		CalculateBullishScore(stockMetric, breadthRatio)
		CalculateBearishScore(stockMetric, breadthRatio)
	}

	// 5. Quicksort Active Indices by BullishScore (~2 microseconds)
	bullishSorted := make([]uint16, len(actor.activeStockIndices))
	copy(bullishSorted, actor.activeStockIndices)
	slices.SortFunc(bullishSorted, func(i, j uint16) int {
		s1 := actor.stocks[i].BullishScore
		s2 := actor.stocks[j].BullishScore
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
	if actor.execHook != nil {
		execState = actor.execHook.OnSecondTick(&actor.stocks, actor.activeStockIndices, bullishSorted, actor.sectorHealthMap, pulseDTO)
	}

	if actor.wsHub == nil || actor.wsHub.ClientCount() == 0 {
		return // Zero connected clients: skip DTO allocations & WebSocket broadcast
	}

	// Bearish Sort for Top 5 Bearish Outliers
	bearishSorted := make([]uint16, len(actor.activeStockIndices))
	copy(bearishSorted, actor.activeStockIndices)
	slices.SortFunc(bearishSorted, func(i, j uint16) int {
		s1 := actor.stocks[i].BearishScore
		s2 := actor.stocks[j].BearishScore
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
		dto := actor.ConvertStockToDTO(&actor.stocks[idx], rk+1)
		topBullish = append(topBullish, dto)
	}

	// Build Top 5 Bearish Outliers
	topBearish := make([]StockRankDTO, 0, 5)
	for rk, idx := range bearishSorted {
		if rk >= 5 {
			break
		}
		dto := actor.ConvertStockToDTO(&actor.stocks[idx], rk+1)
		topBearish = append(topBearish, dto)
	}

	// Build Full 213 Watchlist Table
	allStocks := make([]StockRankDTO, len(bullishSorted))
	for rk, idx := range bullishSorted {
		allStocks[rk] = actor.ConvertStockToDTO(&actor.stocks[idx], rk+1)
	}

	// Historical API Status
	histHealthy, histErr := actor.histClient.IsHealthy()

	snapshot := RankedSnapshot{
		Timestamp:        nowUnix,
		HistoricalActive: histHealthy,
		HistoricalError:  histErr,
		MarketPulse:      pulseDTO,
		Sectors:          sectorRankings,
		TopBullish:       topBullish,
		TopBearish:       topBearish,
		AllStocks:        allStocks,
		Execution:        execState,
	}

	actor.wsHub.Broadcast(snapshot)
}

func (actor *Actor) ConvertStockToDTO(stockMetrics *StockMetrics, rank int) StockRankDTO {
	var chgPercent float64
	if stockMetrics.PrevClose > 0 {
		chgPercent = ((stockMetrics.LTP - stockMetrics.PrevClose) / stockMetrics.PrevClose) * 100.0
	}

	return StockRankDTO{
		Rank:              rank,
		Token:             stockMetrics.InstrumentToken,
		Symbol:            stockMetrics.SymbolString(),
		Sector:            screener.GetSectorName(stockMetrics.SectorID),
		SectorID:          stockMetrics.SectorID,
		LTP:               stockMetrics.LTP,
		ChangePercent:     math.Round(chgPercent*100.0) / 100.0,
		VWAP:              stockMetrics.ExchangeVWAP,
		EMA9:              math.Round(stockMetrics.EMA9*100.0) / 100.0,
		EMA9Slope:         math.Round(stockMetrics.EMA9Slope*1000.0) / 1000.0,
		BullishScore:      math.Round(stockMetrics.BullishScore*10.0) / 10.0,
		BearishScore:      math.Round(stockMetrics.BearishScore*10.0) / 10.0,
		RVOLSession:       math.Round(stockMetrics.RVOLSession*10.0) / 10.0,
		RVOLPace5m:        math.Round(stockMetrics.RVOLPace5m*10.0) / 10.0,
		CompositeRS:       math.Round(stockMetrics.CompositeRS*100.0) / 100.0,
		BuyerRatio:        float32(math.Round(float64(stockMetrics.SmoothedBuyerRatio)*10000.0)) / 10000.0,
		AggressorBuyRatio: float32(math.Round(float64(stockMetrics.AggressorBuyRatio)*10000.0)) / 10000.0,
		EV:                math.Round(stockMetrics.EV*100.0) / 100.0,
		PWin:              math.Round(stockMetrics.PWin*100.0) / 100.0,
		StopProxy:         stockMetrics.StopProxy,
		TargetProxy:       stockMetrics.TargetProxy,
		Reasons:           uint16(stockMetrics.Reasons),
		PullbackCount:     stockMetrics.PullbackCount,
	}
}

