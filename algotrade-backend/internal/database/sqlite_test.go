package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteInitAndMigrations(testingInstance *testing.T) {
	tempDir := testingInstance.TempDir()
	dbPath := filepath.Join(tempDir, "test_algotrade.db")

	db, err := Init(dbPath)
	if err != nil {
		testingInstance.Fatalf("Failed to initialize SQLite: %v", err)
	}

	if db == nil {
		testingInstance.Fatal("Expected non-nil db instance")
	}

	// Verify tables exist
	tables := []string{"kite_sessions", "scanned_stocks"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			testingInstance.Fatalf("Table %s not found in SQLite schema: %v", table, err)
		}
		if name != table {
			testingInstance.Fatalf("Expected table %s, got %s", table, name)
		}
	}

	// Verify WAL mode
	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode;").Scan(&journalMode)
	if err != nil {
		testingInstance.Fatalf("Failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		testingInstance.Errorf("Expected journal_mode to be 'wal', got %s", journalMode)
	}

	if err := Close(); err != nil {
		testingInstance.Fatalf("Failed to close db: %v", err)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		testingInstance.Fatalf("Database file was not created at %s", dbPath)
	}
}
