package kite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/kiteratelimiter"
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
func (service *HistoricalService) ClearNoSessionError() {
	service.mu.Lock()
	defer service.mu.Unlock()
	if !service.isHealthy && service.lastError == "No active Kite session" {
		service.isHealthy = true
		service.lastError = ""
	}
}

// IsHealthy returns whether the historical API is accessible
func (service *HistoricalService) IsHealthy() (bool, string) {
	service.mu.RLock()
	isHealthy := service.isHealthy
	lastError := service.lastError
	service.mu.RUnlock()

	if !isHealthy && lastError == "No active Kite session" && service.kiteService != nil {
		if session := service.kiteService.GetSession(); session != nil && session.AccessToken != "" {
			service.ClearNoSessionError()
			return true, ""
		}
	}
	return isHealthy, lastError
}

// SeedStockBaselines loads 75-slot baselines and today's completed 5m bars for a stock.
// 1. Checks SQLite historical_tod_baselines + historical_5m_bars first (0ms latency).
// 2. If missing or stale, fetches 30-day 5m candles from Kite Historical API with 2.5 req/s rate limiter.
// 3. Fallback: If 403 or API unavailable, uses cached baseline or estimates ATR from LTP (1.5%).
func (service *HistoricalService) SeedStockBaselines(token uint32, lastPrice float64) (*StockBaselineProfile, error) {
	var client *kiteconnect.Client
	var session *SessionInfo
	if service.kiteService != nil {
		client = service.kiteService.GetClient()
		session = service.kiteService.GetSession()
	}
	hasSession := client != nil && session != nil && session.AccessToken != ""

	// Step 1: Check SQLite cache
	cached, updatedToday, err := service.loadFromDatabase(token)
	if err == nil && cached != nil {
		cached.TodayBars = service.loadTodayBarsFromDatabase(token)
		if !hasSession {
			service.setHealthy(false, "No active Kite session")
			return cached, nil
		}

		nowIST := time.Now().In(ist)
		minsSince0915 := (nowIST.Hour()*60 + nowIST.Minute()) - (9*60 + 15)
		isWeekday := nowIST.Weekday() >= time.Monday && nowIST.Weekday() <= time.Friday
		needsTodayBars := isWeekday && minsSince0915 >= 5 && len(cached.TodayBars) == 0

		if updatedToday && !needsTodayBars {
			service.setHealthy(true, "")
			return cached, nil
		}
	}

	// Step 2: Fetch from Kite API
	if !hasSession {
		service.setHealthy(false, "No active Kite session")
		if cached != nil {
			return cached, nil
		}
		return service.generateFallbackProfile(token, lastPrice), nil
	}

	// Fetch 30 calendar days of 5-minute candles (guarantees >= 20 trading days + today's intraday bars)
	toTime := time.Now()
	fromTime := toTime.AddDate(0, 0, -30)

	var candles []kiteconnect.HistoricalData
	var fetchErr error

	var rateLimiter *kiteratelimiter.CentralKiteDispatcher
	if service.kiteService != nil {
		rateLimiter = service.kiteService.GetRateLimiter()
	}

	if rateLimiter != nil {
		contextWithTimeout, cancelTimeout := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelTimeout()

		responseInterface, executeErr := rateLimiter.ExecuteHistorical(contextWithTimeout, func() (interface{}, error) {
			return client.GetHistoricalData(int(token), "5minute", fromTime, toTime, false, false)
		})
		if executeErr != nil {
			fetchErr = executeErr
		} else if castCandles, isCandles := responseInterface.([]kiteconnect.HistoricalData); isCandles {
			candles = castCandles
		}
	} else {
		// Wait for fallback rate limiter (2.5 req/s)
		if service.rateTicker != nil {
			<-service.rateTicker.C
		}
		candles, fetchErr = client.GetHistoricalData(int(token), "5minute", fromTime, toTime, false, false)
	}

	if fetchErr != nil {
		errStr := fetchErr.Error()
		if strings.Contains(errStr, "403") || strings.Contains(strings.ToLower(errStr), "permission") {
			service.setHealthy(false, "Kite Historical API returned 403 Permission Denied — verify Historical API toggle in Kite Developer Console")
		} else {
			service.setHealthy(false, fmt.Sprintf("Kite Historical error: %v", fetchErr))
		}
		slog.Warn("Failed to fetch historical data from Kite, using fallback/cached baseline", "token", token, "error", fetchErr)
		if cached != nil {
			return cached, nil
		}
		return service.generateFallbackProfile(token, lastPrice), nil
	}

	service.setHealthy(true, "")

	// Step 3: Compute 75 TOD slots, ATR14, and extract today's 5m intraday bars
	profile := &StockBaselineProfile{Token: token}
	service.computeBaselines(candles, profile, lastPrice)

	// Step 4: Persist to SQLite
	go service.persistToDatabase(profile)

	return profile, nil
}

func (service *HistoricalService) computeBaselines(candles []kiteconnect.HistoricalData, profile *StockBaselineProfile, lastPrice float64) {
	if len(candles) < 20 {
		fallback := service.generateFallbackProfile(profile.Token, lastPrice)
		*profile = *fallback
		return
	}

	nowIST := time.Now().In(ist)
	todayYear, todayMonth, todayDay := nowIST.Date()

	// Bucket candles by slot index: 09:15 is slot 0, 09:20 is slot 1 ... 15:25 is slot 74
	slotVolumes := make([][]uint64, 75)
	var trueRanges []float64
	var todayBars []TodaySlotBar

	for candleIndex, candle := range candles {
		// Parse candle time in IST
		istTime := candle.Date.Time.In(ist)
		hour, min := istTime.Hour(), istTime.Minute()

		slot := ((hour*60 + min) - (9*60 + 15)) / 5
		if slot >= 0 && slot < 75 {
			slotVolumes[slot] = append(slotVolumes[slot], uint64(candle.Volume))

			candleYear, candleMonth, candleDay := istTime.Date()
			if candleYear == todayYear && candleMonth == todayMonth && candleDay == todayDay {
				todayBars = append(todayBars, TodaySlotBar{
					SlotIdx:   slot,
					Timestamp: istTime.Unix(),
					Open:      candle.Open,
					High:      candle.High,
					Low:       candle.Low,
					Close:     candle.Close,
					Volume:    uint64(candle.Volume),
				})
			}
		}

		// Calculate True Range for 5-minute intraday ATR14.
		// Falsification Guard (Bug #8): On slot 0 (09:15 AM opening bar) or across calendar days,
		// candles[candleIndex-1].Close is yesterday's 15:25 PM close. Including |High - prevClose| or |Low - prevClose|
		// would inject the entire overnight gap into 5-minute bar volatility, inflating ATR14 by 2x-4x all morning.
		trueRange := candle.High - candle.Low
		if candleIndex > 0 && slot > 0 {
			prevIST := candles[candleIndex-1].Date.Time.In(ist)
			if istTime.YearDay() == prevIST.YearDay() && istTime.Year() == prevIST.Year() {
				prevClose := candles[candleIndex-1].Close
				trueRangeHighClose := math.Abs(candle.High - prevClose)
				trueRangeLowClose := math.Abs(candle.Low - prevClose)
				trueRange = math.Max(trueRange, math.Max(trueRangeHighClose, trueRangeLowClose))
			}
		}
		if trueRange >= 0 {
			trueRanges = append(trueRanges, trueRange)
		}
	}

	// Compute ATR14 (last 14 periods)
	if len(trueRanges) >= 14 {
		sumTrueRange := 0.0
		for _, tr := range trueRanges[len(trueRanges)-14:] {
			sumTrueRange += tr
		}
		profile.ATR14 = sumTrueRange / 14.0
	} else if lastPrice > 0 {
		profile.ATR14 = lastPrice * 0.015
	}

	// Compute median volume per slot and running cumulative volume
	var runningCumVol uint64
	for slot := 0; slot < 75; slot++ {
		slotVolumeList := slotVolumes[slot]
		var medianVol uint64 = 1000 // Default floor
		if len(slotVolumeList) > 0 {
			sort.Slice(slotVolumeList, func(volumeIndexA, volumeIndexB int) bool {
				return slotVolumeList[volumeIndexA] < slotVolumeList[volumeIndexB]
			})
			medianVol = slotVolumeList[len(slotVolumeList)/2]
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

func (service *HistoricalService) generateFallbackProfile(token uint32, lastPrice float64) *StockBaselineProfile {
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

func (service *HistoricalService) loadFromDatabase(token uint32) (*StockBaselineProfile, bool, error) {
	if service.db == nil {
		return nil, false, fmt.Errorf("no database connection")
	}

	query := `SELECT slot_idx, median_volume, atr14, COALESCE(updated_at, '') FROM historical_tod_baselines WHERE instrument_token = ? ORDER BY slot_idx ASC`
	rows, err := service.db.Query(query, token)
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
		var volume uint64
		var averageTrueRange float64
		var updatedStr string
		if err := rows.Scan(&slot, &volume, &averageTrueRange, &updatedStr); err == nil && slot >= 0 && slot < 75 {
			runningCum += volume
			profile.Slots[slot] = SlotBaseline{
				SlotIdx:      slot,
				MedianVolume: volume,
				MedianCumVol: runningCum,
				ATR14:        averageTrueRange,
			}
			profile.ATR14 = averageTrueRange
			if updatedStr != "" {
				latestUpdatedStr = updatedStr
			}
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("error iterating baseline rows: %w", err)
	}

	if count == 75 {
		todayIST := time.Now().In(ist).Format("2006-01-02")
		updatedToday := strings.HasPrefix(latestUpdatedStr, todayIST)
		return profile, updatedToday, nil
	}
	return nil, false, fmt.Errorf("incomplete baseline in database: %d/75 slots", count)
}

func (service *HistoricalService) loadTodayBarsFromDatabase(token uint32) []TodaySlotBar {
	if service.db == nil {
		return nil
	}
	todayPrefix := time.Now().In(ist).Format("2006-01-02") + "%"

	rows, err := service.db.Query(`
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
		var openPrice, highPrice, lowPrice, closePrice float64
		var volume uint64
		if err := rows.Scan(&barTimeStr, &openPrice, &highPrice, &lowPrice, &closePrice, &volume); err != nil {
			continue
		}
		parsedTime, parseErr := time.ParseInLocation("2006-01-02 15:04:05", barTimeStr, ist)
		if parseErr != nil {
			parsedTime, parseErr = time.Parse(time.RFC3339, barTimeStr)
			if parseErr == nil {
				parsedTime = parsedTime.In(ist)
			}
		}
		if parseErr == nil {
			slot := ((parsedTime.Hour()*60 + parsedTime.Minute()) - (9*60 + 15)) / 5
			if slot >= 0 && slot < 75 {
				bars = append(bars, TodaySlotBar{
					SlotIdx:   slot,
					Timestamp: parsedTime.Unix(),
					Open:      openPrice,
					High:      highPrice,
					Low:       lowPrice,
					Close:     closePrice,
					Volume:    volume,
				})
			}
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed iterating today bars from database", "token", token, "error", err)
	}
	return bars
}

func (service *HistoricalService) persistToDatabase(profile *StockBaselineProfile) {
	if service.db == nil || profile == nil || profile.IsFallback {
		return
	}

	tx, err := service.db.Begin()
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
		slotBaseline := profile.Slots[slot]
		_, _ = stmt.Exec(profile.Token, slotBaseline.SlotIdx, slotBaseline.MedianVolume, profile.ATR14)
	}

	if len(profile.TodayBars) > 0 {
		barStmt, barStmtErr := tx.Prepare(`
			INSERT INTO historical_5m_bars (instrument_token, bar_time, open, high, low, close, volume)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(instrument_token, bar_time) DO UPDATE SET
				high = max(high, excluded.high),
				low = min(low, excluded.low),
				close = excluded.close,
				volume = max(volume, excluded.volume)
		`)
		if barStmtErr == nil {
			defer barStmt.Close()
			for _, todayBar := range profile.TodayBars {
				barTimeStr := time.Unix(todayBar.Timestamp, 0).In(ist).Format("2006-01-02 15:04:05")
				_, _ = barStmt.Exec(profile.Token, barTimeStr, todayBar.Open, todayBar.High, todayBar.Low, todayBar.Close, todayBar.Volume)
			}
		}
	}

	_ = tx.Commit()
}

func (service *HistoricalService) setHealthy(isHealthy bool, errorMessage string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.isHealthy = isHealthy
	service.lastError = errorMessage
}
