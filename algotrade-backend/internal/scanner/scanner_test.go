package scanner

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajaynegi33/AlgoTrade/internal/database"
)

func TestParseAndFilterInstruments(t *testing.T) {
	sampleCSV := `instrument_token,exchange_token,tradingsymbol,name,last_price,expiry,strike,tick_size,lot_size,instrument_type,segment,exchange
408065,1594,INFY,INFOSYS,1850.50,,,0.05,1,EQ,NSE,NSE
738561,2885,RELIANCE,RELIANCE INDUSTRIES,2940.00,,,0.05,1,EQ,NSE,NSE
123456,9999,NIFTY26OCTFUT,"NIFTY",25000.00,2026-10-29,,0.05,50,FUT,NFO-FUT,NFO
789012,8888,RELIANCE26OCTFUT,"RELIANCE",2950.00,2026-10-29,,0.05,250,FUT,NFO-FUT,NFO
`

	stocks, err := parseAndFilterInstruments(strings.NewReader(sampleCSV), "2026-10-22")
	if err != nil {
		t.Fatalf("Failed to parse and filter instruments: %v", err)
	}

	if len(stocks) != 1 {
		t.Fatalf("Expected 1 matched F&O Cash stock, got %d", len(stocks))
	}

	if stocks[0].Tradingsymbol != "RELIANCE" || stocks[0].InstrumentToken != 738561 {
		t.Errorf("Unexpected matched stock: %+v", stocks[0])
	}
	if stocks[0].Segment != "NSE" || stocks[0].InstrumentType != "EQ" {
		t.Errorf("Expected NSE EQ, got %s %s", stocks[0].Segment, stocks[0].InstrumentType)
	}
}

func TestScannerDatabasePersistence(t *testing.T) {
	tempDir := t.TempDir()
	db, err := database.Init(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	service := NewService(db, nil)

	// Verify querying nonexistent date returns error when table is empty
	_, err = service.GetUniverse("2099-01-01")
	if err == nil {
		t.Fatal("Expected error when querying nonexistent date on empty db")
	}

	// Insert a baseline stock for 2026-10-22
	_, err = db.Exec(`
		INSERT INTO scanned_stocks (instrument_token, tradingsymbol, name, exchange, segment, instrument_type, lot_size, tick_size, last_price, scan_date)
		VALUES (4774913, 'ICICIPRULI', 'ICICI PRU LIFE', 'NSE', 'NSE', 'EQ', 1, 0.05, 550.0, '2026-10-22')
	`)
	if err != nil {
		t.Fatalf("Failed to insert initial stock: %v", err)
	}

	// Verify querying a nonexistent date now returns the fallback universe
	fb, err := service.GetUniverse("2099-01-01")
	if err != nil {
		t.Fatalf("Expected fallback universe, got error: %v", err)
	}
	if fb.TotalStocks != 1 || fb.Stocks[0].Tradingsymbol != "ICICIPRULI" {
		t.Fatalf("Unexpected fallback stocks: %+v", fb.Stocks)
	}

	// Test Upsert with symbol rename (ICICIPRULI -> ICICILIFE with same token)
	_, err = db.Exec(`
		INSERT INTO scanned_stocks (instrument_token, tradingsymbol, name, exchange, segment, instrument_type, lot_size, tick_size, last_price, scan_date)
		VALUES (4774913, 'ICICILIFE', 'ICICI LIFE INSURANCE', 'NSE', 'NSE', 'EQ', 1, 0.05, 560.0, '2026-10-23')
		ON CONFLICT(instrument_token) DO UPDATE SET
			tradingsymbol = excluded.tradingsymbol,
			name = excluded.name,
			scan_date = excluded.scan_date;
	`)
	if err != nil {
		t.Fatalf("Token-based upsert failed on symbol rename: %v", err)
	}

	u23, err := service.GetUniverse("2026-10-23")
	if err != nil || u23.Stocks[0].Tradingsymbol != "ICICILIFE" {
		t.Fatalf("Expected updated symbol ICICILIFE, got: %+v, err: %v", u23, err)
	}
}
