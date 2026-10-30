package monitoring

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// CandleBar represents an OHLC bar used for shadow tracking simulation
type CandleBar struct {
	BarTime time.Time
	High    float64
	Low     float64
	Close   float64
}

// ResolveShadowSignals evaluates pending shadow signals against intraday bars for the trading date
func ResolveShadowSignals(db *sql.DB, tradingDate string) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database connection is nil")
	}

	rows, err := db.Query(`
		SELECT id, instrument_token, symbol, signal_time, entry_price, hypothetical_stop, target_1r, one_r
		FROM shadow_signals
		WHERE trading_date = ? AND outcome = 'PENDING'
	`, tradingDate)
	if err != nil {
		return 0, fmt.Errorf("failed to query pending shadow signals: %w", err)
	}
	defer rows.Close()

	type PendingSignal struct {
		ID               int64
		Token            int64
		Symbol           string
		SignalTimeStr    string
		EntryPrice       float64
		HypotheticalStop float64
		Target1R         float64
		OneR             float64
	}

	var pending []PendingSignal
	for rows.Next() {
		var p PendingSignal
		if scanErr := rows.Scan(&p.ID, &p.Token, &p.Symbol, &p.SignalTimeStr, &p.EntryPrice, &p.HypotheticalStop, &p.Target1R, &p.OneR); scanErr == nil {
			pending = append(pending, p)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating shadow signals: %w", err)
	}

	if len(pending) == 0 {
		return 0, nil
	}

	updateStmt, err := db.Prepare(`
		UPDATE shadow_signals
		SET outcome = ?, simulated_r = ?, resolved_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare shadow update stmt: %w", err)
	}
	defer updateStmt.Close()

	resolvedCount := 0
	for _, sig := range pending {
		sigTime, _ := time.Parse(time.RFC3339, sig.SignalTimeStr)
		if sigTime.IsZero() {
			sigTime, _ = time.Parse("2006-01-02 15:04:05", sig.SignalTimeStr)
		}

		bars, fetchErr := fetchSubsequentBars(db, sig.Token, sigTime)
		outcome := "PENDING"
		simulatedR := 0.0

		if fetchErr == nil && len(bars) > 0 {
			outcome, simulatedR = evaluateBarsOutcome(sig.EntryPrice, sig.HypotheticalStop, sig.Target1R, sig.OneR, bars)
		} else {
			// Fallback: Check last price from scanned_stocks
			var lastPrice float64
			_ = db.QueryRow("SELECT last_price FROM scanned_stocks WHERE instrument_token = ?", sig.Token).Scan(&lastPrice)
			if lastPrice > 0 && sig.OneR > 0 {
				if lastPrice >= sig.Target1R {
					outcome = "WIN_1R"
					simulatedR = 1.0
				} else if lastPrice <= sig.HypotheticalStop {
					outcome = "LOSS_1R"
					simulatedR = -1.0
				} else {
					outcome = "EOD_EXPIRE"
					simulatedR = (lastPrice - sig.EntryPrice) / sig.OneR
				}
			}
		}

		if outcome != "PENDING" {
			if _, execErr := updateStmt.Exec(outcome, simulatedR, sig.ID); execErr == nil {
				resolvedCount++
			}
		}
	}

	slog.Info("Resolved shadow signals", "date", tradingDate, "resolved", resolvedCount, "total_pending", len(pending))
	return resolvedCount, nil
}

func fetchSubsequentBars(db *sql.DB, token int64, afterTime time.Time) ([]CandleBar, error) {
	var rows *sql.Rows
	var err error

	if !afterTime.IsZero() {
		rows, err = db.Query(`
			SELECT bar_time, high, low, close
			FROM historical_5m_bars
			WHERE instrument_token = ? AND bar_time >= ?
			ORDER BY bar_time ASC
		`, token, afterTime.UTC().Format("2006-01-02 15:04:05"))
	} else {
		rows, err = db.Query(`
			SELECT bar_time, high, low, close
			FROM historical_5m_bars
			WHERE instrument_token = ?
			ORDER BY bar_time ASC
		`, token)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bars []CandleBar
	for rows.Next() {
		var b CandleBar
		var tStr string
		if scanErr := rows.Scan(&tStr, &b.High, &b.Low, &b.Close); scanErr == nil {
			b.BarTime, _ = time.Parse(time.RFC3339, tStr)
			bars = append(bars, b)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating historical bars: %w", err)
	}
	return bars, nil
}

func evaluateBarsOutcome(entryPx, stopPx, targetPx, oneR float64, bars []CandleBar) (string, float64) {
	if oneR <= 0 || len(bars) == 0 {
		return "PENDING", 0.0
	}

	for _, bar := range bars {
		hitTarget := bar.High >= targetPx
		hitStop := bar.Low <= stopPx

		if hitTarget && !hitStop {
			return "WIN_1R", 1.0
		}
		if hitStop && !hitTarget {
			return "LOSS_1R", -1.0
		}
		if hitTarget && hitStop {
			// Conservative resolution when bar covers both target and stop:
			// Assume stop was touched first
			return "LOSS_1R", -1.0
		}
	}

	// Neither hit target nor stop before market close
	lastClose := bars[len(bars)-1].Close
	simulatedR := (lastClose - entryPx) / oneR
	return "EOD_EXPIRE", simulatedR
}

