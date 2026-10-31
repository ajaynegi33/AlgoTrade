package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	dbInstance     *sql.DB
	readDBInstance *sql.DB
	dbMutex        sync.RWMutex
)

// Init sets up the universal SQLite database connection with WAL mode and runs table migrations.
func Init(dbPath string) (*sql.DB, error) {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if dbInstance != nil {
		return dbInstance, nil
	}

	if dbPath == "" {
		dbPath = "data/algotrade.db"
	}

	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	// Open SQLite connection
	// _pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)
	connStr := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", dbPath)
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Concurrency tuning: SQLite WAL mode supports concurrent reads and serializes writes
	// with busy_timeout(5000), preventing Go connection pool starvation across goroutines.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	// Open dedicated concurrent read pool on WAL database
	readOnlyDB, readOnlyErr := sql.Open("sqlite", connStr)
	if readOnlyErr == nil {
		readOnlyDB.SetMaxOpenConns(8)
		readOnlyDB.SetMaxIdleConns(4)
		readOnlyDB.SetConnMaxLifetime(time.Hour)
		readDBInstance = readOnlyDB
	} else {
		readDBInstance = db
	}

	dbInstance = db
	slog.Info("Universal SQLite database initialized successfully", "path", dbPath, "mode", "WAL")
	return dbInstance, nil
}

// Close closes the database connection.
func Close() error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if readDBInstance != nil && readDBInstance != dbInstance {
		_ = readDBInstance.Close()
		readDBInstance = nil
	}
	if dbInstance != nil {
		err := dbInstance.Close()
		dbInstance = nil
		return err
	}
	return nil
}

func runMigrations(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS kite_sessions (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			user_id TEXT NOT NULL,
			user_name TEXT NOT NULL,
			api_key TEXT NOT NULL,
			access_token TEXT NOT NULL,
			public_token TEXT,
			login_time TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
		`CREATE INDEX IF NOT EXISTS idx_scanned_stocks_date ON scanned_stocks(scan_date);`,
		`CREATE TABLE IF NOT EXISTS sector_metadata (
			tradingsymbol TEXT PRIMARY KEY,
			sector_name TEXT NOT NULL,
			sector_token INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS historical_tod_baselines (
			instrument_token INTEGER NOT NULL,
			slot_idx INTEGER NOT NULL,
			median_volume INTEGER NOT NULL,
			atr14 REAL NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (instrument_token, slot_idx)
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
		`CREATE TABLE IF NOT EXISTS probability_calibration_matrix (
			factor_key TEXT PRIMARY KEY,
			win_weight REAL NOT NULL,
			sample_count INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
		`CREATE INDEX IF NOT EXISTS idx_engine_signals_time ON engine_signals(signal_time);`,
		`CREATE TABLE IF NOT EXISTS execution_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			mode TEXT NOT NULL DEFAULT 'PAPER',
			auto_execute INTEGER NOT NULL DEFAULT 0,
			paper_capital REAL NOT NULL DEFAULT 100000.0,
			risk_per_trade_pct REAL NOT NULL DEFAULT 0.005,
			max_open_positions INTEGER NOT NULL DEFAULT 4,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`INSERT OR IGNORE INTO execution_settings (id, mode, auto_execute, paper_capital, risk_per_trade_pct, max_open_positions)
		 VALUES (1, 'PAPER', 0, 100000.0, 0.005, 4);`,
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
		`CREATE INDEX IF NOT EXISTS idx_positions_date_mode ON positions(trading_date, mode);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_active_symbol_date ON positions(symbol, trading_date) WHERE state != 'CLOSED';`,
		`CREATE TABLE IF NOT EXISTS orders (
			order_id TEXT PRIMARY KEY,
			position_id TEXT NOT NULL,
			leg_type TEXT NOT NULL,
			symbol TEXT NOT NULL,
			transaction_type TEXT NOT NULL,
			order_type TEXT NOT NULL,
			quantity INTEGER NOT NULL,
			filled_quantity INTEGER NOT NULL DEFAULT 0,
			price REAL NOT NULL DEFAULT 0,
			trigger_price REAL NOT NULL DEFAULT 0,
			average_price REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			status_message TEXT NOT NULL DEFAULT '',
			mod_count INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_orders_position ON orders(position_id);`,
		`CREATE TABLE IF NOT EXISTS daily_risk_ledger (
			trading_date TEXT NOT NULL,
			mode TEXT NOT NULL,
			trades_taken INTEGER NOT NULL DEFAULT 0,
			wins INTEGER NOT NULL DEFAULT 0,
			losses INTEGER NOT NULL DEFAULT 0,
			consecutive_losses INTEGER NOT NULL DEFAULT 0,
			realised_gross_pnl REAL NOT NULL DEFAULT 0,
			total_charges REAL NOT NULL DEFAULT 0,
			realised_net_pnl REAL NOT NULL DEFAULT 0,
			peak_net_pnl REAL NOT NULL DEFAULT 0,
			circuit_breaker_locked INTEGER NOT NULL DEFAULT 0,
			lock_reason TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (trading_date, mode)
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
		`CREATE INDEX IF NOT EXISTS idx_shadow_signals_date ON shadow_signals(trading_date);`,
		`CREATE TABLE IF NOT EXISTS daily_eod_reports (
			trading_date TEXT PRIMARY KEY,
			report_markdown TEXT NOT NULL,
			report_html TEXT NOT NULL,
			summary_json TEXT NOT NULL,
			is_email_sent INTEGER NOT NULL DEFAULT 0,
			email_recipient TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("migration query failed: %w (query: %s)", err, query)
		}
	}

	// Idempotent column migrations
	_, _ = db.Exec(`ALTER TABLE scanned_stocks ADD COLUMN mis_leverage REAL NOT NULL DEFAULT 0.0;`)
	_, _ = db.Exec(`ALTER TABLE scanned_stocks ADD COLUMN mis_margin_pct REAL NOT NULL DEFAULT 0.0;`)
	_, _ = db.Exec(`ALTER TABLE positions ADD COLUMN exit_price REAL NOT NULL DEFAULT 0.0;`)
	_, _ = db.Exec(`ALTER TABLE positions ADD COLUMN low_water_mark REAL NOT NULL DEFAULT 0.0;`)
	_, _ = db.Exec(`ALTER TABLE positions ADD COLUMN mfe_r REAL NOT NULL DEFAULT 0.0;`)
	_, _ = db.Exec(`ALTER TABLE positions ADD COLUMN mae_r REAL NOT NULL DEFAULT 0.0;`)
	_, _ = db.Exec(`ALTER TABLE positions ADD COLUMN intended_entry_price REAL NOT NULL DEFAULT 0.0;`)
	_, _ = db.Exec(`ALTER TABLE positions ADD COLUMN entry_slippage_pct REAL NOT NULL DEFAULT 0.0;`)
	_, _ = db.Exec(`ALTER TABLE positions ADD COLUMN exit_slippage_pct REAL NOT NULL DEFAULT 0.0;`)

	return nil
}
