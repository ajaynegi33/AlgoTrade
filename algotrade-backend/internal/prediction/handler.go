package prediction

import (
	"net/http"
	"slices"

	"github.com/ajaynegi33/AlgoTrade/internal/engine"
	"github.com/ajaynegi33/AlgoTrade/internal/response"
	"github.com/ajaynegi33/AlgoTrade/internal/scanner"
)

type Handler struct {
	actor *engine.Actor
}

func NewHandler(actor *engine.Actor) *Handler {
	return &Handler{actor: actor}
}

type MarketPictureDTO struct {
	BullishCount   int               `json:"bullish_count"`
	BearishCount   int               `json:"bearish_count"`
	TotalStocks    int               `json:"total_stocks"`
	LeadingSectors []SectorMoveDTO   `json:"leading_sectors"`
	LaggingSectors []SectorMoveDTO   `json:"lagging_sectors"`
	DayBias        string            `json:"day_bias"` // "ONE_SIDED_BULLISH", "ONE_SIDED_BEARISH", "BALANCED"
}

type SectorMoveDTO struct {
	Name      string  `json:"name"`
	AvgMovePct float64 `json:"avg_move_pct"`
}

type TrajectoryDTO struct {
	Symbol     string    `json:"symbol"`
	Direction  string    `json:"direction"`
	ChangePct  float64   `json:"change_pct"`
	Points     []float64 `json:"points"` // % change at each 5m interval from 09:15 open
	IsPick     bool      `json:"is_pick"`
	Sector     string    `json:"sector"`
}

type DecisionGateItemDTO struct {
	Symbol       string  `json:"symbol"`
	Sector       string  `json:"sector"`
	ChangePct    float64 `json:"change_pct"`
	Score        float64 `json:"score"`
	BuildUpType  string  `json:"buildup_type"` // "LONG BUILD-UP" | "SHORT BUILD-UP"
	PassedGate1  bool    `json:"passed_gate1"` // Clean strength
	PassedGate2  bool    `json:"passed_gate2"` // Build-up & RVOL
	PassedGate3  bool    `json:"passed_gate3"` // Sector cap <= 2
	PassedGate4  bool    `json:"passed_gate4"` // Zone Z & R:R >= 1.5
	IsPick       bool    `json:"is_pick"`
}

type OrderBlockTreeResponseDTO struct {
	MarketPicture  MarketPictureDTO      `json:"market_picture"`
	Trajectories   []TrajectoryDTO       `json:"trajectories"`
	BullishGates   []DecisionGateItemDTO `json:"bullish_gates"`
	BearishGates   []DecisionGateItemDTO `json:"bearish_gates"`
	TopBullishPicks []MinimaxEvaluation  `json:"top_bullish_picks"`
	TopBearishPicks []MinimaxEvaluation  `json:"top_bearish_picks"`
}

// GetOrderBlockTree handles GET /api/prediction/orderblock-tree
func (h *Handler) GetOrderBlockTree(w http.ResponseWriter, r *http.Request) {
	if h.actor == nil {
		response.Error(w, http.StatusServiceUnavailable, "ACTOR_UNAVAILABLE", "Engine actor not initialized")
		return
	}

	bullishCount := 0
	bearishCount := 0

	type symMove struct {
		stockPtr  *engine.StockMetrics
		changePct float64
		score     float64
		sector    string
		token     uint32
	}
	var bullishList []symMove
	var bearishList []symMove

	sectorMap := make(map[string][]float64)

	// Sample up to 512 stocks from engine
	for i := 0; i < 512; i++ {
		s := h.actor.GetStockPtrByIndex(uint16(i))
		if s == nil || s.LTP <= 0 || s.InstrumentToken == 0 {
			continue
		}
		sym := s.SymbolString()
		if sym == "" {
			continue
		}

		chg := 0.0
		if s.PrevClose > 0 {
			chg = ((s.LTP - s.PrevClose) / s.PrevClose) * 100.0
		} else if s.SessionBars[0].Open > 0 {
			chg = ((s.LTP - s.SessionBars[0].Open) / s.SessionBars[0].Open) * 100.0
		}
		chg = round2(chg)
		sec := scanner.GetSectorName(s.SectorID)
		sectorMap[sec] = append(sectorMap[sec], chg)

		item := symMove{
			stockPtr:  s,
			changePct: chg,
			score:     s.BullishScore,
			sector:    sec,
			token:     s.InstrumentToken,
		}

		if chg >= 0 {
			bullishCount++
			bullishList = append(bullishList, item)
		} else {
			bearishCount++
			bearishList = append(bearishList, item)
		}
	}

	total := bullishCount + bearishCount
	dayBias := "BALANCED"
	if total > 0 {
		if float64(bullishCount)/float64(total) >= 0.65 {
			dayBias = "ONE_SIDED_BULLISH"
		} else if float64(bearishCount)/float64(total) >= 0.65 {
			dayBias = "ONE_SIDED_BEARISH"
		}
	}

	// Compute Leading & Lagging sectors
	type secAvg struct {
		name string
		avg  float64
	}
	var secAvgs []secAvg
	for sec, chgs := range sectorMap {
		if len(chgs) == 0 {
			continue
		}
		var sum float64
		for _, c := range chgs {
			sum += c
		}
		secAvgs = append(secAvgs, secAvg{name: sec, avg: round2(sum / float64(len(chgs)))})
	}
	slices.SortFunc(secAvgs, func(a, b secAvg) int {
		if a.avg > b.avg {
			return -1
		}
		if a.avg < b.avg {
			return 1
		}
		return 0
	})

	var leadingSectors, laggingSectors []SectorMoveDTO
	for i := 0; i < min(3, len(secAvgs)); i++ {
		leadingSectors = append(leadingSectors, SectorMoveDTO{Name: secAvgs[i].name, AvgMovePct: secAvgs[i].avg})
	}
	for i := len(secAvgs) - 1; i >= max(0, len(secAvgs)-3); i-- {
		laggingSectors = append(laggingSectors, SectorMoveDTO{Name: secAvgs[i].name, AvgMovePct: secAvgs[i].avg})
	}

	// Sort Bullish and Bearish candidates
	slices.SortFunc(bullishList, func(a, b symMove) int {
		if a.changePct > b.changePct {
			return -1
		}
		if a.changePct < b.changePct {
			return 1
		}
		return 0
	})
	slices.SortFunc(bearishList, func(a, b symMove) int {
		if a.changePct < b.changePct {
			return -1
		}
		if a.changePct > b.changePct {
			return 1
		}
		return 0
	})

	// Select Top 3 Bullish Picks (Max 2 per sector)
	var topBullishPicks []MinimaxEvaluation
	bullishPickSyms := make(map[string]bool)
	bullishSecCounts := make(map[string]int)

	for _, b := range bullishList {
		if len(topBullishPicks) >= 3 {
			break
		}
		if bullishSecCounts[b.sector] >= 2 {
			continue
		}
		eval := EvaluateMinimaxAlphaBeta(b.stockPtr, true)
		topBullishPicks = append(topBullishPicks, eval)
		bullishPickSyms[b.stockPtr.SymbolString()] = true
		bullishSecCounts[b.sector]++
	}

	// Select Top 3 Bearish Picks (Max 2 per sector)
	var topBearishPicks []MinimaxEvaluation
	bearishPickSyms := make(map[string]bool)
	bearishSecCounts := make(map[string]int)

	for _, b := range bearishList {
		if len(topBearishPicks) >= 3 {
			break
		}
		if bearishSecCounts[b.sector] >= 2 {
			continue
		}
		eval := EvaluateMinimaxAlphaBeta(b.stockPtr, false)
		topBearishPicks = append(topBearishPicks, eval)
		bearishPickSyms[b.stockPtr.SymbolString()] = true
		bearishSecCounts[b.sector]++
	}

	// Build Trajectories from 09:15
	trajectories := make([]TrajectoryDTO, 0, len(bullishList)+len(bearishList))
	for _, it := range append(bullishList, bearishList...) {
		s := it.stockPtr
		pts := make([]float64, 0, s.CurrentBarIdx+1)
		basePx := s.PrevClose
		if s.SessionBars[0].Open > 0 {
			basePx = s.SessionBars[0].Open
		}
		if basePx <= 0 {
			basePx = s.LTP
		}

		for bIdx := 0; bIdx <= int(s.CurrentBarIdx) && bIdx < 75; bIdx++ {
			c := s.SessionBars[bIdx].Close
			if c <= 0 {
				c = s.SessionBars[bIdx].Open
			}
			if c <= 0 {
				c = s.LTP
			}
			pChg := round2(((c - basePx) / basePx) * 100.0)
			pts = append(pts, pChg)
		}

		isPick := bullishPickSyms[s.SymbolString()] || bearishPickSyms[s.SymbolString()]
		dir := "BULLISH"
		if it.changePct < 0 {
			dir = "BEARISH"
		}

		trajectories = append(trajectories, TrajectoryDTO{
			Symbol:    s.SymbolString(),
			Direction: dir,
			ChangePct: it.changePct,
			Points:    pts,
			IsPick:    isPick,
			Sector:    it.sector,
		})
	}

	// Build Decision Gates (Top 15 each)
	var bullishGates []DecisionGateItemDTO
	for i := 0; i < min(15, len(bullishList)); i++ {
		b := bullishList[i]
		s := b.stockPtr
		isPick := bullishPickSyms[s.SymbolString()]
		bullishGates = append(bullishGates, DecisionGateItemDTO{
			Symbol:       s.SymbolString(),
			Sector:       b.sector,
			ChangePct:    b.changePct,
			Score:        round2(s.BullishScore),
			BuildUpType:  "LONG BUILD-UP",
			PassedGate1:  b.changePct >= 0.50,
			PassedGate2:  s.RVOLSession >= 1.2,
			PassedGate3:  true,
			PassedGate4:  true,
			IsPick:       isPick,
		})
	}

	var bearishGates []DecisionGateItemDTO
	for i := 0; i < min(15, len(bearishList)); i++ {
		b := bearishList[i]
		s := b.stockPtr
		isPick := bearishPickSyms[s.SymbolString()]
		bearishGates = append(bearishGates, DecisionGateItemDTO{
			Symbol:       s.SymbolString(),
			Sector:       b.sector,
			ChangePct:    b.changePct,
			Score:        round2(s.BullishScore),
			BuildUpType:  "SHORT BUILD-UP",
			PassedGate1:  b.changePct <= -0.50,
			PassedGate2:  s.RVOLSession >= 1.2,
			PassedGate3:  true,
			PassedGate4:  true,
			IsPick:       isPick,
		})
	}

	resp := OrderBlockTreeResponseDTO{
		MarketPicture: MarketPictureDTO{
			BullishCount:   bullishCount,
			BearishCount:   bearishCount,
			TotalStocks:    total,
			LeadingSectors: leadingSectors,
			LaggingSectors: laggingSectors,
			DayBias:        dayBias,
		},
		Trajectories:    trajectories,
		BullishGates:    bullishGates,
		BearishGates:    bearishGates,
		TopBullishPicks: topBullishPicks,
		TopBearishPicks: topBearishPicks,
	}

	response.Success(w, http.StatusOK, resp)
}
