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

var ist = time.FixedZone("IST", 5*3600+1800)

// SlotBaseline holds the 20-day historical time-of-day baseline metrics for a single 5m slot
type SlotBaseline struct {
	SlotIdx      int     // 0 to 74
	MedianVolume uint64  // Median volume of just this 5m bar
	MedianCumVol uint64  // Median cumulative volume from 09:15 to this slot
	ATR14        float64 // 14-period ATR
}

// TodaySlotBar holds a single 5-minute intraday bar from today's trading session (09:15–15:25 IST)
type TodaySlotBar struct {
	SlotIdx   int
	Timestamp int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    uint64
}

// StockBaselineProfile holds all 75 TOD slots, ATR14, and today's intraday bars for a stock
type StockBaselineProfile struct {
	Token      uint32
	Slots      [75]SlotBaseline
	ATR14      float64
	IsFallback bool
	TodayBars  []TodaySlotBar
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
	svc := &HistoricalService{
		db:          db,
		kiteService: kiteService,
		rateTicker:  time.NewTicker(400 * time.Millisecond),
		isHealthy:   true,
	}

	if kiteService != nil {
		kiteService.RegisterOnAuthenticated(func() {
			svc.ClearNoSessionError()
		})
	}

	return svc
}

// ClearNoSessionError resets the "No active Kite session" warning state immediately when a session authenticates
func (h *HistoricalService) ClearNoSessionError() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.isHealthy && h.lastError == "No active Kite session" {
		h.isHealthy = true
		h.lastError = ""
	}
}

// IsHealthy returns whether the historical API is accessible
func (h *HistoricalService) IsHealthy() (bool, string) {
	h.mu.RLock()
	healthy := h.isHealthy
	lastErr := h.lastError
	h.mu.RUnlock()

	if !healthy && lastErr == "No active Kite session" && h.kiteService != nil {
		if sess := h.kiteService.GetSession(); sess != nil && sess.AccessToken != "" {
			h.ClearNoSessionError()
			return true, ""
		}
	}
	return healthy, lastErr
}

// SeedStockBaselines loads 75-slot baselines and today's completed 5m bars for a stock.
// 1. Checks SQLite historical_tod_baselines + historical_5m_bars first (0ms latency).
// 2. If missing or stale, fetches 30-day 5m candles from Kite Historical API with 2.5 req/s rate limiter.
// 3. Fallback: If 403 or API unavailable, uses cached baseline or estimates ATR from LTP (1.5%).
func (h *HistoricalService) SeedStockBaselines(token uint32, lastPrice float64) (*StockBaselineProfile, error) {
	var client *kiteconnect.Client
	var session *SessionInfo
	if h.kiteService != nil {
		client = h.kiteService.GetClient()
		session = h.kiteService.GetSession()
	}
	hasSession := client != nil && session != nil && session.AccessToken != ""

	// Step 1: Check SQLite cache
	cached, updatedToday, err := h.loadFromDatabase(token)
	if err == nil && cached != nil {
		cached.TodayBars = h.loadTodayBarsFromDatabase(token)
		if !hasSession {
			h.setHealthy(false, "No active Kite session")
			return cached, nil
		}

		nowIST := time.Now().In(ist)
		minsSince0915 := (nowIST.Hour()*60 + nowIST.Minute()) - (9*60 + 15)
		isWeekday := nowIST.Weekday() >= time.Monday && nowIST.Weekday() <= time.Friday
		needsTodayBars := isWeekday && minsSince0915 >= 5 && len(cached.TodayBars) == 0

		if updatedToday && !needsTodayBars {
			h.setHealthy(true, "")
			return cached, nil
		}
	}

	// Step 2: Fetch from Kite API
	if !hasSession {
		h.setHealthy(false, "No active Kite session")
		if cached != nil {
			return cached, nil
		}
		return h.generateFallbackProfile(token, lastPrice), nil
	}

	// Wait for rate limiter (2.5 req/s)
	if h.rateTicker != nil {
		<-h.rateTicker.C
	}

	// Fetch 30 calendar days of 5-minute candles (guarantees >= 20 trading days + today's intraday bars)
	toTime := time.Now()
	fromTime := toTime.AddDate(0, 0, -30)

	candles, err := client.GetHistoricalData(int(token), "5minute", fromTime, toTime, false, false)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "403") || strings.Contains(strings.ToLower(errStr), "permission") {
			h.setHealthy(false, "Kite Historical API returned 403 Permission Denied — verify Historical API toggle in Kite Developer Console")
		} else {
			h.setHealthy(false, fmt.Sprintf("Kite Historical error: %v", err))
		}
		slog.Warn("Failed to fetch historical data from Kite, using fallback/cached baseline", "token", token, "error", err)
		if cached != nil {
			return cached, nil
		}
		return h.generateFallbackProfile(token, lastPrice), nil
	}

	h.setHealthy(true, "")

	// Step 3: Compute 75 TOD slots, ATR14, and extract today's 5m intraday bars
	profile := &StockBaselineProfile{Token: token}
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

	nowIST := time.Now().In(ist)
	todayYear, todayMonth, todayDay := nowIST.Date()

	// Bucket candles by slot index: 09:15 is slot 0, 09:20 is slot 1 ... 15:25 is slot 74
	slotVolumes := make([][]uint64, 75)
	var trueRanges []float64
	var todayBars []TodaySlotBar

	for i, c := range candles {
		// Parse candle time in IST
		istTime := c.Date.Time.In(ist)
		hour, min := istTime.Hour(), istTime.Minute()

		slot := ((hour*60 + min) - (9*60 + 15)) / 5
		if slot >= 0 && slot < 75 {
			slotVolumes[slot] = append(slotVolumes[slot], uint64(c.Volume))

			cy, cm, cd := istTime.Date()
			if cy == todayYear && cm == todayMonth && cd == todayDay {
				todayBars = append(todayBars, TodaySlotBar{
					SlotIdx:   slot,
					Timestamp: istTime.Unix(),
					Open:      c.Open,
					High:      c.High,
					Low:       c.Low,
					Close:     c.Close,
					Volume:    uint64(c.Volume),
				})
			}
		}

		// Calculate True Range for 5-minute intraday ATR14.
		// Falsification Guard (Bug #8): On slot 0 (09:15 AM opening bar) or across calendar days,
		// candles[i-1].Close is yesterday's 15:25 PM close. Including |High - prevClose| or |Low - prevClose|
		// would inject the entire overnight gap into 5-minute bar volatility, inflating ATR14 by 2x-4x all morning.
		tr := c.High - c.Low
		if i > 0 && slot > 0 {
			prevIST := candles[i-1].Date.Time.In(ist)
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
	profile.IsFallback = false
	profile.TodayBars = todayBars
}

func (h *HistoricalService) generateFallbackProfile(token uint32, lastPrice float64) *StockBaselineProfile {
	profile := &StockBaselineProfile{
		Token:      token,
		IsFallback: true,
	}
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

func (h *HistoricalService) loadFromDatabase(token uint32) (*StockBaselineProfile, bool, error) {
	if h.db == nil {
		return nil, false, fmt.Errorf("no database connection")
	}

	query := `SELECT slot_idx, median_volume, atr14, COALESCE(updated_at, '') FROM historical_tod_baselines WHERE instrument_token = ? ORDER BY slot_idx ASC`
	rows, err := h.db.Query(query, token)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	profile := &StockBaselineProfile{
		Token:      token,
		IsFallback: false,
	}
	count := 0
	var runningCum uint64
	var latestUpdatedStr string

	for rows.Next() {
		var slot int
		var vol uint64
		var atr float64
		var updatedStr string
		if err := rows.Scan(&slot, &vol, &atr, &updatedStr); err == nil && slot >= 0 && slot < 75 {
			runningCum += vol
			profile.Slots[slot] = SlotBaseline{
				SlotIdx:      slot,
				MedianVolume: vol,
				MedianCumVol: runningCum,
				ATR14:        atr,
			}
			profile.ATR14 = atr
			if updatedStr != "" {
				latestUpdatedStr = updatedStr
			}
			count++
		}
	}

	if count == 75 {
		todayIST := time.Now().In(ist).Format("2006-01-02")
		updatedToday := strings.HasPrefix(latestUpdatedStr, todayIST)
		return profile, updatedToday, nil
	}
	return nil, false, fmt.Errorf("incomplete baseline in database: %d/75 slots", count)
}

func (h *HistoricalService) loadTodayBarsFromDatabase(token uint32) []TodaySlotBar {
	if h.db == nil {
		return nil
	}
	todayPrefix := time.Now().In(ist).Format("2006-01-02") + "%"

	rows, err := h.db.Query(`
		SELECT bar_time, open, high, low, close, volume
		FROM historical_5m_bars
		WHERE instrument_token = ? AND bar_time LIKE ?
		ORDER BY bar_time ASC
	`, token, todayPrefix)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var bars []TodaySlotBar
	for rows.Next() {
		var barTimeStr string
		var openPx, highPx, lowPx, closePx float64
		var vol uint64
		if err := rows.Scan(&barTimeStr, &openPx, &highPx, &lowPx, &closePx, &vol); err != nil {
			continue
		}
		t, parseErr := time.ParseInLocation("2006-01-02 15:04:05", barTimeStr, ist)
		if parseErr != nil {
			t, parseErr = time.Parse(time.RFC3339, barTimeStr)
			if parseErr == nil {
				t = t.In(ist)
			}
		}
		if parseErr == nil {
			slot := ((t.Hour()*60 + t.Minute()) - (9*60 + 15)) / 5
			if slot >= 0 && slot < 75 {
				bars = append(bars, TodaySlotBar{
					SlotIdx:   slot,
					Timestamp: t.Unix(),
					Open:      openPx,
					High:      highPx,
					Low:       lowPx,
					Close:     closePx,
					Volume:    vol,
				})
			}
		}
	}
	return bars
}

func (h *HistoricalService) persistToDatabase(profile *StockBaselineProfile) {
	if h.db == nil || profile == nil || profile.IsFallback {
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

	if len(profile.TodayBars) > 0 {
		barStmt, bErr := tx.Prepare(`
			INSERT INTO historical_5m_bars (instrument_token, bar_time, open, high, low, close, volume)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(instrument_token, bar_time) DO UPDATE SET
				high = max(high, excluded.high),
				low = min(low, excluded.low),
				close = excluded.close,
				volume = max(volume, excluded.volume)
		`)
		if bErr == nil {
			defer barStmt.Close()
			for _, tb := range profile.TodayBars {
				barTimeStr := time.Unix(tb.Timestamp, 0).In(ist).Format("2006-01-02 15:04:05")
				_, _ = barStmt.Exec(profile.Token, barTimeStr, tb.Open, tb.High, tb.Low, tb.Close, tb.Volume)
			}
		}
	}

	_ = tx.Commit()
}

func (h *HistoricalService) setHealthy(healthy bool, errMsg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isHealthy = healthy
	h.lastError = errMsg
}
