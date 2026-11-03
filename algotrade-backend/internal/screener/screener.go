package screener

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/kite"
)

var (
	istLocation = time.FixedZone("IST", 5*3600+1800)
)

// ScannedStock represents a high-liquidity F&O underlying stock available for Cash Intraday (MIS)
type ScannedStock struct {
	InstrumentToken int       `json:"instrument_token"`
	Tradingsymbol   string    `json:"tradingsymbol"`
	Name            string    `json:"name"`
	Exchange        string    `json:"exchange"`
	Segment         string    `json:"segment"`
	InstrumentType  string    `json:"instrument_type"`
	LotSize         int       `json:"lot_size"`
	TickSize        float64   `json:"tick_size"`
	LastPrice       float64   `json:"last_price"`
	MISLeverage     float64   `json:"mis_leverage"`
	MISMarginPct    float64   `json:"mis_margin_pct"`
	ScanDate        string    `json:"scan_date"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// UniverseSummary provides an overview of the scanned stock universe
type UniverseSummary struct {
	ScanDate    string         `json:"scan_date"`
	TotalStocks int            `json:"total_stocks"`
	LastUpdated string         `json:"last_updated"`
	Stocks      []ScannedStock `json:"stocks"`
}

type Service struct {
	db          *sql.DB
	kiteService *kite.Service
	mu          sync.Mutex
	httpClient  *http.Client
}

func NewService(db *sql.DB, kiteService *kite.Service) *Service {
	return &Service{
		db:          db,
		kiteService: kiteService,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// RunScan checks if today's scan exists; if not or if force is true, downloads instruments and builds the universe
func (service *Service) RunScan(isForceScan bool) (*UniverseSummary, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	todayIST := time.Now().In(istLocation).Format("2006-01-02")

	// If not forcing, check if we already have today's scanned universe
	if !isForceScan {
		cachedUniverseSummary, err := service.GetUniverse(todayIST)
		if err == nil && cachedUniverseSummary.ScanDate == todayIST && cachedUniverseSummary.TotalStocks >= 150 {
			slog.Info("Using cached stock universe for today", "date", todayIST, "stocks_count", cachedUniverseSummary.TotalStocks)
			return cachedUniverseSummary, nil
		}
	}

	slog.Info("Initiating 08:30 AM dynamic stock universe scan...", "date", todayIST)

	// Fetch all master instruments (publicly accessible across all exchanges in 1 stream)
	matchedStocks, err := service.fetchAndFilterMasterInstruments(todayIST)
	if err != nil {
		slog.Error("Failed to fetch master instruments, attempting fallback to cached universe", "error", err)
		return service.fallbackUniverse()
	}

	// Step 2: Atomic SQLite Transaction to persist the universe
	tx, err := service.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin sqlite transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	upsertQuery := `
		INSERT INTO scanned_stocks (
			instrument_token, tradingsymbol, name, exchange, segment, instrument_type,
			lot_size, tick_size, last_price, scan_date, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(instrument_token) DO UPDATE SET
			tradingsymbol = excluded.tradingsymbol,
			name = excluded.name,
			lot_size = excluded.lot_size,
			tick_size = excluded.tick_size,
			last_price = excluded.last_price,
			scan_date = excluded.scan_date,
			updated_at = CURRENT_TIMESTAMP;
	`
	stmt, err := tx.Prepare(upsertQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare upsert statement: %w", err)
	}
	defer stmt.Close()

	for _, stock := range matchedStocks {
		_, err := stmt.Exec(
			stock.InstrumentToken,
			stock.Tradingsymbol,
			stock.Name,
			stock.Exchange,
			stock.Segment,
			stock.InstrumentType,
			stock.LotSize,
			stock.TickSize,
			stock.LastPrice,
			stock.ScanDate,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to insert stock %s: %w", stock.Tradingsymbol, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit sqlite transaction: %w", err)
	}

	slog.Info("08:30 AM Stock universe persisted to SQLite successfully", "count", len(matchedStocks), "date", todayIST)

	return &UniverseSummary{
		ScanDate:    todayIST,
		TotalStocks: len(matchedStocks),
		LastUpdated: time.Now().In(istLocation).Format(time.RFC3339),
		Stocks:      matchedStocks,
	}, nil
}

// GetUniverse retrieves the scanned stock universe for a specific IST date (or latest if empty)
func (service *Service) GetUniverse(date string) (*UniverseSummary, error) {
	if date == "" {
		date = time.Now().In(istLocation).Format("2006-01-02")
	}

	query := `
		SELECT instrument_token, tradingsymbol, name, exchange, segment, instrument_type,
		       lot_size, tick_size, last_price, COALESCE(mis_leverage, 5.0), COALESCE(mis_margin_pct, 0.20), scan_date, updated_at
		FROM scanned_stocks
		WHERE scan_date = ?
		ORDER BY tradingsymbol ASC
	`
	rows, err := service.db.Query(query, date)
	if err != nil {
		return nil, fmt.Errorf("failed to query scanned stocks: %w", err)
	}
	defer rows.Close()

	var stocks []ScannedStock
	var lastUpdated time.Time

	for rows.Next() {
		var scannedStock ScannedStock
		var updatedStr string
		err := rows.Scan(
			&scannedStock.InstrumentToken,
			&scannedStock.Tradingsymbol,
			&scannedStock.Name,
			&scannedStock.Exchange,
			&scannedStock.Segment,
			&scannedStock.InstrumentType,
			&scannedStock.LotSize,
			&scannedStock.TickSize,
			&scannedStock.LastPrice,
			&scannedStock.MISLeverage,
			&scannedStock.MISMarginPct,
			&scannedStock.ScanDate,
			&updatedStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if scannedStock.MISLeverage <= 0 {
			scannedStock.MISLeverage = 5.0
			scannedStock.MISMarginPct = 0.20
		}

		if parsedTime, err := time.Parse(time.RFC3339, updatedStr); err == nil {
			scannedStock.UpdatedAt = parsedTime
			if parsedTime.After(lastUpdated) {
				lastUpdated = parsedTime
			}
		} else if parsedTime, err := time.Parse("2006-01-02 15:04:05", updatedStr); err == nil {
			scannedStock.UpdatedAt = parsedTime
			if parsedTime.After(lastUpdated) {
				lastUpdated = parsedTime
			}
		}

		stocks = append(stocks, scannedStock)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating scanned stocks: %w", err)
	}

	if len(stocks) == 0 {
		// If requested date has no scanned stocks, fallback to latest cached scan
		return service.fallbackUniverse()
	}

	return &UniverseSummary{
		ScanDate:    date,
		TotalStocks: len(stocks),
		LastUpdated: lastUpdated.In(istLocation).Format(time.RFC3339),
		Stocks:      stocks,
	}, nil
}

// fallbackUniverse returns the most recent cached stock universe in SQLite if today's fetch fails
func (service *Service) fallbackUniverse() (*UniverseSummary, error) {
	var latestDate string
	err := service.db.QueryRow("SELECT scan_date FROM scanned_stocks ORDER BY scan_date DESC LIMIT 1").Scan(&latestDate)
	if err != nil {
		return nil, fmt.Errorf("no fallback universe available in SQLite: %w", err)
	}

	slog.Warn("Falling back to previously cached stock universe", "cached_date", latestDate)
	return service.GetUniverse(latestDate)
}

// fetchAndFilterMasterInstruments streams the universal master instrument CSV and extracts F&O Cash Equities in a single pass
func (service *Service) fetchAndFilterMasterInstruments(todayIST string) ([]ScannedStock, error) {
	url := "https://api.kite.trade/instruments"
	resp, err := service.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download instruments from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download instruments from %s returned status %d", url, resp.StatusCode)
	}

	return parseAndFilterInstruments(resp.Body, todayIST)
}

// parseAndFilterInstruments parses the CSV stream and extracts active F&O Cash Equities
func parseAndFilterInstruments(readerStream io.Reader, todayIST string) ([]ScannedStock, error) {
	reader := csv.NewReader(readerStream)
	reader.ReuseRecord = true

	// Read header row
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read instrument csv header: %w", err)
	}

	columnIndicesByName := make(map[string]int)
	for columnIndex, columnName := range header {
		columnIndicesByName[strings.TrimSpace(columnName)] = columnIndex
	}

	getCol := func(record []string, name string) string {
		if index, exists := columnIndicesByName[name]; exists && index < len(record) {
			return record[index]
		}
		return ""
	}

	fnoUnderlyingSet := make(map[string]struct{})
	type rawNSECash struct {
		token     int
		symbol    string
		name      string
		lotSize   int
		tickSize  float64
		lastPrice float64
	}
	var nseCashList []rawNSECash

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // skip malformed row
		}

		segment := getCol(record, "segment")
		instType := getCol(record, "instrument_type")

		// 1. Detect F&O underlying stocks: in NFO, underlying company ticker is in 'name'
		if segment == "NFO-FUT" || segment == "NFO-OPT" {
			cleanName := strings.Trim(strings.TrimSpace(getCol(record, "name")), "\"")
			if cleanName != "" {
				fnoUnderlyingSet[cleanName] = struct{}{}
			}
		}

		// 2. Collect NSE Cash Equities
		if segment == "NSE" && instType == "EQ" {
			token, _ := strconv.Atoi(getCol(record, "instrument_token"))
			lotSize, _ := strconv.Atoi(getCol(record, "lot_size"))
			if lotSize <= 0 {
				lotSize = 1
			}
			tickSize, _ := strconv.ParseFloat(getCol(record, "tick_size"), 64)
			if tickSize <= 0 {
				tickSize = 0.05
			}
			lastPrice, _ := strconv.ParseFloat(getCol(record, "last_price"), 64)

			cleanSymbol := strings.TrimSpace(getCol(record, "tradingsymbol"))
			cleanName := strings.Trim(strings.TrimSpace(getCol(record, "name")), "\"")

			nseCashList = append(nseCashList, rawNSECash{
				token:     token,
				symbol:    cleanSymbol,
				name:      cleanName,
				lotSize:   lotSize,
				tickSize:  tickSize,
				lastPrice: lastPrice,
			})
		}
	}

	slog.Info("Identified unique F&O underlying symbols from NFO master",
		"fno_underlyings_count", len(fnoUnderlyingSet),
		"nse_cash_total", len(nseCashList),
	)

	nowUTC := time.Now().UTC()
	var matchedStocks []ScannedStock

	for _, cash := range nseCashList {
		if _, exists := fnoUnderlyingSet[cash.symbol]; exists {
			matchedStocks = append(matchedStocks, ScannedStock{
				InstrumentToken: cash.token,
				Tradingsymbol:   cash.symbol,
				Name:            cash.name,
				Exchange:        "NSE",
				Segment:         "NSE",
				InstrumentType:  "EQ",
				LotSize:         cash.lotSize,
				TickSize:        cash.tickSize,
				LastPrice:       cash.lastPrice,
				ScanDate:        todayIST,
				UpdatedAt:       nowUTC,
			})
		}
	}

	if len(matchedStocks) == 0 {
		return nil, fmt.Errorf("zero stocks matched between NFO underlyings and NSE Cash Equities")
	}

	slog.Info("Successfully filtered F&O Cash Equity intraday stocks", "matched_count", len(matchedStocks))
	return matchedStocks, nil
}
