package monitoring

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/diagnostics"
)

var (
	globalCollectorLock sync.RWMutex
	globalCollector     *EventCollector
)

// SetCollector registers the active process singleton collector
func SetCollector(collector *EventCollector) {
	globalCollectorLock.Lock()
	defer globalCollectorLock.Unlock()
	globalCollector = collector
}

// GetCollector retrieves the active process singleton collector
func GetCollector() *EventCollector {
	globalCollectorLock.RLock()
	defer globalCollectorLock.RUnlock()
	return globalCollector
}

// EventCollector drains async events from trading actors and batches writes to SQLite
type EventCollector struct {
	db             *sql.DB
	eventCh        chan any
	ctx            context.Context
	cancel         context.CancelFunc
	waitGroup      sync.WaitGroup
	isStarted      bool
	lastShadowEmit map[uint32]time.Time
	mu             sync.Mutex
}

// NewEventCollector creates a buffered non-blocking event collector
func NewEventCollector(db *sql.DB) *EventCollector {
	return &EventCollector{
		db:             db,
		eventCh:        make(chan any, 2048),
		lastShadowEmit: make(map[uint32]time.Time),
	}
}

// Start spawns the background batch ingestion worker
func (collector *EventCollector) Start(parentCtx context.Context) {
	collector.mu.Lock()
	if collector.isStarted {
		collector.mu.Unlock()
		return
	}
	collector.ctx, collector.cancel = context.WithCancel(parentCtx)
	collector.isStarted = true
	collector.mu.Unlock()

	collector.waitGroup.Add(1)
	go collector.batchFlushLoop()
	slog.Info("Monitoring EventCollector started")
}

// Stop flushes remaining events and shuts down cleanly
func (collector *EventCollector) Stop() {
	collector.mu.Lock()
	if !collector.isStarted {
		collector.mu.Unlock()
		return
	}
	collector.cancel()
	collector.isStarted = false
	collector.mu.Unlock()

	collector.waitGroup.Wait()
	// Flush any trailing events remaining in the channel
	collector.flushPending()
	slog.Info("Monitoring EventCollector stopped")
}

// EmitShadowSignal enqueues a rejected candidate setup without blocking the caller
func (collector *EventCollector) EmitShadowSignal(evt ShadowSignalEvent) {
	if collector == nil || collector.eventCh == nil {
		return
	}

	collector.mu.Lock()
	if lastTime, exists := collector.lastShadowEmit[evt.InstrumentToken]; exists && time.Since(lastTime) < 15*time.Minute {
		collector.mu.Unlock()
		return
	}
	if collector.lastShadowEmit == nil {
		collector.lastShadowEmit = make(map[uint32]time.Time)
	}
	collector.lastShadowEmit[evt.InstrumentToken] = time.Now()
	collector.mu.Unlock()

	select {
	case collector.eventCh <- evt:
	default:
		slog.Warn("Monitoring channel full; dropped ShadowSignalEvent", "symbol", evt.Symbol, "reason", evt.RejectionReason)
	}
}

// EmitTradeEvent enqueues a trade execution metric event without blocking the caller
func (collector *EventCollector) EmitTradeEvent(evt TradeExecutionEvent) {
	if collector == nil || collector.eventCh == nil {
		return
	}
	select {
	case collector.eventCh <- evt:
	default:
		slog.Warn("Monitoring channel full; dropped TradeExecutionEvent", "symbol", evt.Symbol)
	}
}

func (collector *EventCollector) batchFlushLoop() {
	defer collector.waitGroup.Done()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	batch := make([]any, 0, 128)

	for {
		select {
		case <-collector.ctx.Done():
			if len(batch) > 0 {
				collector.persistBatch(batch)
			}
			return
		case item := <-collector.eventCh:
			batch = append(batch, item)
			if len(batch) >= 100 {
				collector.persistBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				collector.persistBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

func (collector *EventCollector) flushPending() {
	batch := make([]any, 0, 128)
	for {
		select {
		case item := <-collector.eventCh:
			batch = append(batch, item)
			if len(batch) >= 100 {
				collector.persistBatch(batch)
				batch = batch[:0]
			}
		default:
			if len(batch) > 0 {
				collector.persistBatch(batch)
			}
			return
		}
	}
}

func (collector *EventCollector) persistBatch(batch []any) {
	if collector.db == nil || len(batch) == 0 {
		return
	}

	batchStartTime := time.Now()
	defer func() {
		batchElapsedDuration := time.Since(batchStartTime)
		diagnostics.RecordLatency("monitoring_sqlite_batch_persist", batchElapsedDuration, 50*time.Millisecond)
	}()

	tx, err := collector.db.Begin()
	if err != nil {
		slog.Error("Failed to start transaction for monitoring batch", "err", err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	stmtShadow, err := tx.Prepare(`
		INSERT INTO shadow_signals (
			trading_date, signal_time, symbol, instrument_token, pattern_type,
			bullish_score, rejection_reason, entry_price, hypothetical_stop,
			one_r, target_1r, outcome, simulated_r, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', 0.0, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		slog.Error("Failed to prepare shadow_signals insert", "err", err)
		return
	}
	defer stmtShadow.Close()

	stmtTrade, err := tx.Prepare(`
		UPDATE positions SET
			intended_entry_price = ?,
			entry_slippage_pct = ?,
			high_water_mark = MAX(high_water_mark, ?),
			low_water_mark = CASE WHEN low_water_mark = 0 THEN ? ELSE MIN(low_water_mark, ?) END,
			mfe_r = ?,
			mae_r = ?
		WHERE position_id = ?
	`)
	if err != nil {
		slog.Error("Failed to prepare positions update for trade event", "err", err)
		return
	}
	defer stmtTrade.Close()

	for _, item := range batch {
		switch evt := item.(type) {
		case ShadowSignalEvent:
			sigTime := evt.SignalTime
			if sigTime.IsZero() {
				sigTime = time.Now()
			}
			_, _ = stmtShadow.Exec(
				evt.TradingDate,
				sigTime.UTC().Format(time.RFC3339),
				evt.Symbol,
				evt.InstrumentToken,
				evt.PatternType,
				evt.BullishScore,
				evt.RejectionReason,
				evt.EntryPrice,
				evt.HypotheticalStop,
				evt.OneR,
				evt.Target1R,
			)
		case TradeExecutionEvent:
			_, _ = stmtTrade.Exec(
				evt.IntendedEntryPrice,
				evt.EntrySlippagePct,
				evt.HighWaterMark,
				evt.LowWaterMark,
				evt.LowWaterMark,
				evt.MFERMultiple,
				evt.MAERMultiple,
				evt.PositionID,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("Failed to commit monitoring batch transaction", "err", err)
	}
}
