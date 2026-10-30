package monitoring

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory sqlite db: %v", err)
	}

	queries := []string{
		`CREATE TABLE IF NOT EXISTS positions (
			position_id TEXT PRIMARY KEY,
			trading_date TEXT NOT NULL,
			mode TEXT NOT NULL,
			symbol TEXT NOT NULL,
			instrument_token INTEGER NOT NULL,
			state TEXT NOT NULL,
			pattern_type TEXT NOT NULL,
			entry_order_id TEXT NOT NULL DEFAULT '',
			sl_order_id TEXT NOT NULL DEFAULT '',
			scale_out_order_id TEXT NOT NULL DEFAULT '',
			entry_price REAL NOT NULL DEFAULT 0,
			exit_price REAL NOT NULL DEFAULT 0,
			initial_stop REAL NOT NULL DEFAULT 0,
			current_stop REAL NOT NULL DEFAULT 0,
			shadow_stop REAL NOT NULL DEFAULT 0,
			breakeven_stop REAL NOT NULL DEFAULT 0,
			base_buffer REAL NOT NULL DEFAULT 0,
			high_water_mark REAL NOT NULL DEFAULT 0,
			low_water_mark REAL NOT NULL DEFAULT 0,
			one_r REAL NOT NULL DEFAULT 0,
			mfe_r REAL NOT NULL DEFAULT 0,
			mae_r REAL NOT NULL DEFAULT 0,
			intended_entry_price REAL NOT NULL DEFAULT 0,
			entry_slippage_pct REAL NOT NULL DEFAULT 0,
			exit_slippage_pct REAL NOT NULL DEFAULT 0,
			initial_qty INTEGER NOT NULL DEFAULT 0,
			filled_entry_qty INTEGER NOT NULL DEFAULT 0,
			filled_exit_qty INTEGER NOT NULL DEFAULT 0,
			mod_count INTEGER NOT NULL DEFAULT 0,
			current_gear INTEGER NOT NULL DEFAULT 1,
			tranche1_done INTEGER NOT NULL DEFAULT 0,
			realised_gross_pnl REAL NOT NULL DEFAULT 0,
			total_charges REAL NOT NULL DEFAULT 0,
			realised_net_pnl REAL NOT NULL DEFAULT 0,
			exit_reason TEXT NOT NULL DEFAULT '',
			opened_at DATETIME NOT NULL,
			closed_at DATETIME
		);`,
		`CREATE TABLE IF NOT EXISTS shadow_signals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trading_date TEXT NOT NULL,
			signal_time DATETIME NOT NULL,
			symbol TEXT NOT NULL,
			instrument_token INTEGER NOT NULL,
			pattern_type TEXT NOT NULL,
			bullish_score REAL NOT NULL,
			rejection_reason TEXT NOT NULL,
			entry_price REAL NOT NULL,
			hypothetical_stop REAL NOT NULL,
			one_r REAL NOT NULL,
			target_1r REAL NOT NULL,
			outcome TEXT NOT NULL DEFAULT 'PENDING',
			simulated_r REAL NOT NULL DEFAULT 0.0,
			resolved_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS daily_eod_reports (
			trading_date TEXT PRIMARY KEY,
			report_markdown TEXT NOT NULL,
			report_html TEXT NOT NULL,
			summary_json TEXT NOT NULL,
			is_email_sent INTEGER NOT NULL DEFAULT 0,
			email_recipient TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS historical_5m_bars (
			instrument_token INTEGER NOT NULL,
			bar_time DATETIME NOT NULL,
			open REAL NOT NULL,
			high REAL NOT NULL,
			low REAL NOT NULL,
			close REAL NOT NULL,
			volume INTEGER NOT NULL,
			PRIMARY KEY (instrument_token, bar_time)
		);`,
		`CREATE TABLE IF NOT EXISTS engine_signals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			signal_time DATETIME NOT NULL,
			instrument_token INTEGER NOT NULL,
			tradingsymbol TEXT NOT NULL,
			score REAL NOT NULL,
			ev REAL NOT NULL,
			p_win REAL NOT NULL,
			ltp REAL NOT NULL,
			vwap REAL NOT NULL,
			stop_proxy REAL NOT NULL,
			target_proxy REAL NOT NULL,
			reasons INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS scanned_stocks (
			instrument_token INTEGER PRIMARY KEY,
			tradingsymbol TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			exchange TEXT NOT NULL DEFAULT 'NSE',
			segment TEXT NOT NULL DEFAULT 'NSE',
			instrument_type TEXT NOT NULL DEFAULT 'EQ',
			lot_size INTEGER NOT NULL DEFAULT 1,
			tick_size REAL NOT NULL DEFAULT 0.05,
			last_price REAL DEFAULT 0.0,
			scan_date TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("Migration query failed: %v", err)
		}
	}
	return db
}

func TestEventCollector_ShadowAndTradeEvents(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	collector := NewEventCollector(db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector.Start(ctx)

	// Emit Shadow Signal
	collector.EmitShadowSignal(ShadowSignalEvent{
		TradingDate:      "2026-10-30",
		SignalTime:       time.Now(),
		Symbol:           "INFY",
		InstrumentToken:  408065,
		PatternType:      "VWAP_PULLBACK_RECLAIM",
		BullishScore:     78.5,
		RejectionReason:  "Max Portfolio Heat 2.0% reached",
		EntryPrice:       1500.0,
		HypotheticalStop: 1485.0,
		OneR:             15.0,
		Target1R:         1515.0,
	})

	// Insert base position to receive trade event
	_, err := db.Exec(`
		INSERT INTO positions (position_id, trading_date, mode, symbol, instrument_token, state, pattern_type, opened_at)
		VALUES ('POS-1', '2026-10-30', 'PAPER', 'TCS', 2953213, 'ACTIVE', 'ORB_15M_BREAKOUT', CURRENT_TIMESTAMP)
	`)
	if err != nil {
		t.Fatalf("Failed to insert dummy position: %v", err)
	}

	collector.EmitTradeEvent(TradeExecutionEvent{
		PositionID:         "POS-1",
		TradingDate:        "2026-10-30",
		Symbol:             "TCS",
		IntendedEntryPrice: 3500.0,
		ActualFillPrice:    3502.0,
		EntrySlippagePct:   0.057,
		HighWaterMark:      3540.0,
		LowWaterMark:       3495.0,
		MFERMultiple:       2.0,
		MAERMultiple:       -0.25,
	})

	collector.Stop()

	// Verify shadow signal recorded
	var shadowCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM shadow_signals WHERE symbol = 'INFY'").Scan(&shadowCount); err != nil || shadowCount != 1 {
		t.Fatalf("Expected 1 shadow signal for INFY, got %d, err: %v", shadowCount, err)
	}

	// Verify trade event updated positions
	var intendedPx, entrySlip, hwm, lwm float64
	if err := db.QueryRow("SELECT intended_entry_price, entry_slippage_pct, high_water_mark, low_water_mark FROM positions WHERE position_id = 'POS-1'").Scan(&intendedPx, &entrySlip, &hwm, &lwm); err != nil {
		t.Fatalf("Failed to query updated position: %v", err)
	}
	if intendedPx != 3500.0 || hwm != 3540.0 || lwm != 3495.0 {
		t.Errorf("Unexpected position update: intended=%.2f, hwm=%.2f, lwm=%.2f", intendedPx, hwm, lwm)
	}
}

func TestShadowSignalsResolution_With5mBars(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	now := time.Now()
	// Insert shadow signal
	_, err := db.Exec(`
		INSERT INTO shadow_signals (
			trading_date, signal_time, symbol, instrument_token, pattern_type,
			bullish_score, rejection_reason, entry_price, hypothetical_stop, one_r, target_1r, outcome
		) VALUES (
			'2026-10-30', ?, 'RELIANCE', 738561, 'VWAP_PULLBACK_RECLAIM',
			82.0, 'Portfolio Heat 2.0% Cap', 2500.0, 2480.0, 20.0, 2520.0, 'PENDING'
		)
	`, now.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("Failed to insert shadow signal: %v", err)
	}

	// Insert 5m bars where target 2520 is reached
	_, err = db.Exec(`
		INSERT INTO historical_5m_bars (instrument_token, bar_time, open, high, low, close, volume)
		VALUES
			(738561, ?, 2501.0, 2510.0, 2495.0, 2508.0, 10000),
			(738561, ?, 2508.0, 2525.0, 2505.0, 2522.0, 15000)
	`, now.Add(5*time.Minute).UTC().Format("2006-01-02 15:04:05"), now.Add(10*time.Minute).UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("Failed to insert 5m bars: %v", err)
	}

	resolved, err := ResolveShadowSignals(db, "2026-10-30")
	if err != nil {
		t.Fatalf("ResolveShadowSignals failed: %v", err)
	}
	if resolved != 1 {
		t.Fatalf("Expected 1 signal resolved, got %d", resolved)
	}

	var outcome string
	var simR float64
	if err := db.QueryRow("SELECT outcome, simulated_r FROM shadow_signals WHERE symbol = 'RELIANCE'").Scan(&outcome, &simR); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if outcome != "WIN_1R" || simR != 1.0 {
		t.Errorf("Expected WIN_1R with simulated_r=1.0, got %s / %.2f", outcome, simR)
	}
}

func TestComputeEODMetrics_AndExcursionAlerts(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Insert 3 closed trades
	// Trade 1: Winner (+2R), MFE +3.0R, MAE -0.3R (shallow dip)
	// Trade 2: Winner (+1.5R), MFE +2.5R, MAE -0.2R (shallow dip)
	// Trade 3: Loser (-1R), MFE +0.5R, MAE -1.0R (stopped out)
	openT1 := "2026-10-30 09:30:00"
	closeT1 := "2026-10-30 10:15:00"
	openT2 := "2026-10-30 10:45:00"
	closeT2 := "2026-10-30 11:30:00"
	openT3 := "2026-10-30 12:00:00"
	closeT3 := "2026-10-30 12:45:00"

	_, err := db.Exec(`
		INSERT INTO positions (
			position_id, trading_date, mode, symbol, instrument_token, state, pattern_type,
			entry_price, exit_price, initial_stop, one_r, initial_qty,
			high_water_mark, low_water_mark, mfe_r, mae_r,
			intended_entry_price, entry_slippage_pct, exit_slippage_pct,
			realised_gross_pnl, total_charges, realised_net_pnl,
			current_gear, exit_reason, opened_at, closed_at
		) VALUES
		('P1', '2026-10-30', 'PAPER', 'SBIN', 779521, 'CLOSED', 'VWAP_PULLBACK_RECLAIM',
		 500.0, 520.0, 490.0, 10.0, 100,
		 530.0, 497.0, 3.0, -0.3,
		 500.0, 0.0, 0.0,
		 2000.0, 100.0, 1900.0,
		 3, 'TRAIL_GEAR_3', ?, ?),

		('P2', '2026-10-30', 'PAPER', 'HDFCBANK', 341249, 'CLOSED', 'ORB_15M_BREAKOUT',
		 1500.0, 1530.0, 1480.0, 20.0, 50,
		 1550.0, 1496.0, 2.5, -0.2,
		 1495.0, 0.334, 0.0, -- Toxic slippage > 0.15%
		 1500.0, 120.0, 1380.0,
		 2, 'SCALE_OUT_BE', ?, ?),

		('P3', '2026-10-30', 'PAPER', 'ICICIBANK', 1270529, 'CLOSED', 'VWAP_PULLBACK_RECLAIM',
		 1000.0, 990.0, 990.0, 10.0, 100,
		 1005.0, 990.0, 0.5, -1.0,
		 1000.0, 0.0, 0.0,
		 -1000.0, 80.0, -1080.0,
		 1, 'INITIAL_SL_HIT', ?, ?)
	`, openT1, closeT1, openT2, closeT2, openT3, closeT3)
	if err != nil {
		t.Fatalf("Failed to insert positions: %v", err)
	}

	summary, err := ComputeEODMetrics(db, "2026-10-30")
	if err != nil {
		t.Fatalf("ComputeEODMetrics failed: %v", err)
	}

	if summary.TotalTrades != 3 {
		t.Errorf("Expected 3 total trades, got %d", summary.TotalTrades)
	}
	if summary.WinningTrades != 2 || summary.LosingTrades != 1 {
		t.Errorf("Expected 2 wins, 1 loss, got %d wins, %d losses", summary.WinningTrades, summary.LosingTrades)
	}
	if summary.RealisedNetPnL != 2200.0 {
		t.Errorf("Expected net PnL 2200.0, got %.2f", summary.RealisedNetPnL)
	}

	// Verify Stop Loss Alert triggered (100% of winners stayed above -0.6R)
	if summary.ExcursionSummary.PctWinnersUnder06R != 100.0 {
		t.Errorf("Expected 100%% winners under 0.6R, got %.1f%%", summary.ExcursionSummary.PctWinnersUnder06R)
	}
	if summary.ExcursionSummary.StopLossAlert == "" {
		t.Errorf("Expected StopLossAlert to be populated")
	}

	// Verify Toxic Liquidity Flagged for HDFCBANK (slippage 0.334% > 0.15%)
	var foundHDFC bool
	for _, slip := range summary.SlippageRecords {
		if slip.Symbol == "HDFCBANK" {
			foundHDFC = true
			if !slip.IsToxicLiquidity {
				t.Errorf("Expected HDFCBANK to be marked toxic")
			}
		}
	}
	if !foundHDFC {
		t.Errorf("Expected HDFCBANK in slippage records")
	}

	// Verify HTML and Markdown generation
	html := GenerateHTMLReport(summary)
	md := GenerateMarkdownReport(summary)
	if len(html) < 200 || len(md) < 100 {
		t.Errorf("HTML or Markdown report suspiciously short")
	}

	// Save Report Files
	tempDir := t.TempDir()
	htmlPath, mdPath, saveErr := SaveReportFiles(db, summary, tempDir)
	if saveErr != nil {
		t.Fatalf("SaveReportFiles failed: %v", saveErr)
	}
	if _, statErr := os.Stat(htmlPath); statErr != nil {
		t.Errorf("HTML file not found at %s", htmlPath)
	}
	if _, statErr := os.Stat(mdPath); statErr != nil {
		t.Errorf("Markdown file not found at %s", mdPath)
	}

	// Check SQLite record
	var recordedDate string
	if err := db.QueryRow("SELECT trading_date FROM daily_eod_reports WHERE trading_date = '2026-10-30'").Scan(&recordedDate); err != nil || recordedDate != "2026-10-30" {
		t.Errorf("Expected daily_eod_reports entry for 2026-10-30, got %s", recordedDate)
	}
}

func TestResendEmailDispatch_MockServer(t *testing.T) {
	var receivedAuthHeader string
	var receivedPayload ResendEmailPayload

	mockTransport := mockRoundTripper(func(req *http.Request) (*http.Response, error) {
		receivedAuthHeader = req.Header.Get("Authorization")
		_ = json.NewDecoder(req.Body).Decode(&receivedPayload)

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id": "msg_mock12345"}`)),
			Header:     make(http.Header),
		}, nil
	})

	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	http.DefaultTransport = mockTransport

	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := SendEODReportEmail(ctx, db, "re_test_key_xyz", "desk@example.com", "2026-10-30", "<h1>Test Report</h1>")
	if err != nil {
		t.Fatalf("SendEODReportEmail failed: %v", err)
	}

	if receivedAuthHeader != "Bearer re_test_key_xyz" {
		t.Errorf("Expected 'Bearer re_test_key_xyz', got '%s'", receivedAuthHeader)
	}
	if len(receivedPayload.To) != 1 || receivedPayload.To[0] != "desk@example.com" {
		t.Errorf("Unexpected recipient: %v", receivedPayload.To)
	}
	if receivedPayload.Subject != "AlgoTrade EOD Diagnostic Report — 2026-10-30" {
		t.Errorf("Unexpected subject: %s", receivedPayload.Subject)
	}
}

type mockRoundTripper func(req *http.Request) (*http.Response, error)

func (f mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
