package kite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

// SlotBaseline holds the 20-day historical time-of-day baseline metrics for a single 5m slot
type SlotBaseline struct {
	SlotIdx      int     // 0 to 74
	MedianVolume uint64  // Median volume of just this 5m bar
	MedianCumVol uint64  // Median cumulative volume from 09:15 to this slot
	ATR14        float64 // 14-period ATR
}

// StockBaselineProfile holds all 75 TOD slots and ATR for a stock
type StockBaselineProfile struct {
	Token uint32
	Slots [75]SlotBaseline
	ATR14 float64
}

// HistoricalService fetches 20-day 5m historical data and seeds TOD baselines
type HistoricalService struct {
	db          *sql.DB
	kiteService *Service
	rateTicker  *time.Ticker
	mu          sync.RWMutex
	isHealthy   bool
	lastError   string
}

// NewHistoricalService creates a rate-limited Kite Historical client
func NewHistoricalService(db *sql.DB, kiteService *Service) *HistoricalService {
	// Rate limit: 2.5 req/sec = 1 request every 400ms
	return &HistoricalService{
		db:          db,
		kiteService: kiteService,
		rateTicker:  time.NewTicker(400 * time.Millisecond),
		isHealthy:   true,
	}
}

// IsHealthy returns whether the historical API is accessible
func (h *HistoricalService) IsHealthy() (bool, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isHealthy, h.lastError
}

// SeedStockBaselines loads 75-slot baselines for a stock.
// 1. Checks SQLite historical_tod_baselines first (0ms latency).
// 2. If missing, fetches 20-day 5m candles from Kite Historical API with 2.5 req/s rate limiter.
// 3. Fallback: If 403 or API unavailable, estimates ATR from LTP (1.5%) and volume from average.
func (h *HistoricalService) SeedStockBaselines(token uint32, lastPrice float64) (*StockBaselineProfile, error) {
	profile := &StockBaselineProfile{Token: token}

	// Step 1: Check SQLite cache
	cached, err := h.loadFromDatabase(token)
	if err == nil && cached != nil {
		return cached, nil
	}

	// Step 2: Fetch from Kite API
	client := h.kiteService.GetClient()
	session := h.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		h.setHealthy(false, "No active Kite session")
		return h.generateFallbackProfile(token, lastPrice), nil
	}

	// Wait for rate limiter (2.5 req/s)
	<-h.rateTicker.C

	// Fetch 20 days of 5-minute candles
	toTime := time.Now()
	fromTime := toTime.AddDate(0, 0, -30) // 30 calendar days to guarantee 20 trading days

	candles, err := client.GetHistoricalData(int(token), "5minute", fromTime, toTime, false, false)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "403") || strings.Contains(strings.ToLower(errStr), "permission") {
			h.setHealthy(false, "Kite Historical API subscription (₹500/mo) is inactive or returned 403")
		} else {
			h.setHealthy(false, fmt.Sprintf("Kite Historical error: %v", err))
		}
		slog.Warn("Failed to fetch historical data from Kite, using calculated fallback", "token", token, "error", err)
		return h.generateFallbackProfile(token, lastPrice), nil
	}

	h.setHealthy(true, "")

	// Step 3: Compute 75 TOD slots and ATR14
	h.computeBaselines(candles, profile, lastPrice)

	// Step 4: Persist to SQLite
	go h.persistToDatabase(profile)

	return profile, nil
}

func (h *HistoricalService) computeBaselines(candles []kiteconnect.HistoricalData, profile *StockBaselineProfile, lastPrice float64) {
	if len(candles) < 20 {
		fallback := h.generateFallbackProfile(profile.Token, lastPrice)
		*profile = *fallback
		return
	}

	// Bucket candles by slot index: 09:15 is slot 0, 09:20 is slot 1 ... 15:25 is slot 74
	slotVolumes := make([][]uint64, 75)
	var trueRanges []float64

	for i, c := range candles {
		// Parse candle time in IST
		istTime := c.Date.Time.In(time.FixedZone("IST", 5*3600+1800))
		hour, min := istTime.Hour(), istTime.Minute()

		slot := ((hour*60 + min) - (9*60 + 15)) / 5
		if slot >= 0 && slot < 75 {
			slotVolumes[slot] = append(slotVolumes[slot], uint64(c.Volume))
		}

		// Calculate True Range for 5-minute intraday ATR14.
		// Falsification Guard (Bug #8): On slot 0 (09:15 AM opening bar) or across calendar days,
		// candles[i-1].Close is yesterday's 15:25 PM close. Including |High - prevClose| or |Low - prevClose|
		// would inject the entire overnight gap into 5-minute bar volatility, inflating ATR14 by 2x-4x all morning.
		tr := c.High - c.Low
		if i > 0 && slot > 0 {
			prevIST := candles[i-1].Date.Time.In(time.FixedZone("IST", 5*3600+1800))
			if istTime.YearDay() == prevIST.YearDay() && istTime.Year() == prevIST.Year() {
				prevClose := candles[i-1].Close
				tr2 := math.Abs(c.High - prevClose)
				tr3 := math.Abs(c.Low - prevClose)
				tr = math.Max(tr, math.Max(tr2, tr3))
			}
		}
		if tr >= 0 {
			trueRanges = append(trueRanges, tr)
		}
	}

	// Compute ATR14 (last 14 periods)
	if len(trueRanges) >= 14 {
		sumTR := 0.0
		for _, tr := range trueRanges[len(trueRanges)-14:] {
			sumTR += tr
		}
		profile.ATR14 = sumTR / 14.0
	} else if lastPrice > 0 {
		profile.ATR14 = lastPrice * 0.015
	}

	// Compute median volume per slot and running cumulative volume
	var runningCumVol uint64
	for slot := 0; slot < 75; slot++ {
		vols := slotVolumes[slot]
		var medianVol uint64 = 1000 // Default floor
		if len(vols) > 0 {
			sort.Slice(vols, func(i, j int) bool { return vols[i] < vols[j] })
			medianVol = vols[len(vols)/2]
			if medianVol < 500 {
				medianVol = 500
			}
		}
		runningCumVol += medianVol
		profile.Slots[slot] = SlotBaseline{
			SlotIdx:      slot,
			MedianVolume: medianVol,
			MedianCumVol: runningCumVol,
			ATR14:        profile.ATR14,
		}
	}
}

func (h *HistoricalService) generateFallbackProfile(token uint32, lastPrice float64) *StockBaselineProfile {
	profile := &StockBaselineProfile{Token: token}
	atr := lastPrice * 0.015 // Default 1.5% volatility
	if atr <= 0 {
		atr = 5.0
	}
	profile.ATR14 = atr

	var runningCum uint64
	for slot := 0; slot < 75; slot++ {
		slotVol := uint64(5000) // Default 5k shares per 5m slot
		runningCum += slotVol
		profile.Slots[slot] = SlotBaseline{
			SlotIdx:      slot,
			MedianVolume: slotVol,
			MedianCumVol: runningCum,
			ATR14:        atr,
		}
	}
	return profile
}

func (h *HistoricalService) loadFromDatabase(token uint32) (*StockBaselineProfile, error) {
	if h.db == nil {
		return nil, fmt.Errorf("no database connection")
	}

	query := `SELECT slot_idx, median_volume, atr14 FROM historical_tod_baselines WHERE instrument_token = ? ORDER BY slot_idx ASC`
	rows, err := h.db.Query(query, token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profile := &StockBaselineProfile{Token: token}
	count := 0
	var runningCum uint64

	for rows.Next() {
		var slot int
		var vol uint64
		var atr float64
		if err := rows.Scan(&slot, &vol, &atr); err == nil && slot >= 0 && slot < 75 {
			runningCum += vol
			profile.Slots[slot] = SlotBaseline{
				SlotIdx:      slot,
				MedianVolume: vol,
				MedianCumVol: runningCum,
				ATR14:        atr,
			}
			profile.ATR14 = atr
			count++
		}
	}

	if count == 75 {
		return profile, nil
	}
	return nil, fmt.Errorf("incomplete baseline in database: %d/75 slots", count)
}

func (h *HistoricalService) persistToDatabase(profile *StockBaselineProfile) {
	if h.db == nil {
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO historical_tod_baselines (instrument_token, slot_idx, median_volume, atr14, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(instrument_token, slot_idx) DO UPDATE SET
			median_volume = excluded.median_volume,
			atr14 = excluded.atr14,
			updated_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return
	}
	defer stmt.Close()

	for slot := 0; slot < 75; slot++ {
		s := profile.Slots[slot]
		_, _ = stmt.Exec(profile.Token, s.SlotIdx, s.MedianVolume, profile.ATR14)
	}

	_ = tx.Commit()
}

func (h *HistoricalService) setHealthy(healthy bool, errMsg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isHealthy = healthy
	h.lastError = errMsg
}
