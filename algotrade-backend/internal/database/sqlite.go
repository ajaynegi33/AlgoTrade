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
	dbInstance *sql.DB
	dbMutex    sync.RWMutex
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

	// Concurrency tuning: SQLite handles single-writer concurrency reliably
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
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

	dbInstance = db
	slog.Info("Universal SQLite database initialized successfully", "path", dbPath, "mode", "WAL")
	return dbInstance, nil
}

// GetDB returns the active SQLite database instance. Panics if Init() has not been called.
func GetDB() *sql.DB {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	if dbInstance == nil {
		panic("database.GetDB called before database.Init")
	}
	return dbInstance
}

// Close closes the database connection.
func Close() error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

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
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("migration query failed: %w (query: %s)", err, query)
		}
	}

	return nil
}
