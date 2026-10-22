package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteInitAndMigrations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_algotrade.db")

	db, err := Init(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize SQLite: %v", err)
	}

	if db == nil {
		t.Fatal("Expected non-nil db instance")
	}

	if GetDB() != db {
		t.Fatal("GetDB() returned different instance")
	}

	// Verify tables exist
	tables := []string{"kite_sessions", "scanned_stocks"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("Table %s not found in SQLite schema: %v", table, err)
		}
		if name != table {
			t.Fatalf("Expected table %s, got %s", table, name)
		}
	}

	// Verify WAL mode
	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode;").Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("Expected journal_mode to be 'wal', got %s", journalMode)
	}

	if err := Close(); err != nil {
		t.Fatalf("Failed to close db: %v", err)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("Database file was not created at %s", dbPath)
	}
}
