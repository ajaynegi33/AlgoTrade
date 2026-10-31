package kiteratelimiter

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrQueueSaturated is returned when non-emergency queues are full
	ErrQueueSaturated = errors.New("kite rate limiter queue is saturated; request rejected")
	// ErrDispatcherStopped is returned when requests are submitted after dispatcher shutdown
	ErrDispatcherStopped = errors.New("kite rate limiter dispatcher is stopped")
	// ErrRequestTimedOutInQueue is returned when context expires before the request leaves the queue
	ErrRequestTimedOutInQueue = errors.New("kite request timed out while waiting in channel queue")
	// ErrCircuitPaused is returned when requests are rejected during an active HTTP 429 backoff
	ErrCircuitPaused = errors.New("kite rate limiter is temporarily paused due to upstream HTTP 429")
)

// RequestPriority represents the execution urgency of a Kite API request
type RequestPriority int

const (
	// PriorityEmergencyCritical is reserved for stop-loss orders, emergency market sweeps, and cancellations
	PriorityEmergencyCritical RequestPriority = 1
	// PriorityStandardOrder is used for new limit buy entries and profit scale-outs
	PriorityStandardOrder RequestPriority = 2
	// PriorityAccountSync is used for live margin queries and order ledger synchronization
	PriorityAccountSync RequestPriority = 3
	// PriorityBackgroundSync is used for historical candle backfills, universe leverage queries, and screener scans
	PriorityBackgroundSync RequestPriority = 4
)

// EndpointCategory identifies the rate limit bucket enforced by Zerodha
type EndpointCategory string

const (
	// CategoryOrder applies to order placement, modification, and cancellation (10 req/s limit)
	CategoryOrder EndpointCategory = "ORDER"
	// CategoryHistorical applies strictly to historical candle queries (3 req/s limit)
	CategoryHistorical EndpointCategory = "HISTORICAL"
	// CategoryGeneral applies to quotes, margins, and user profile queries (10 req/s limit)
	CategoryGeneral EndpointCategory = "GENERAL"
)

// KiteRequestEnvelope packages an operation for asynchronous processing by the central dispatcher
type KiteRequestEnvelope struct {
	RequestID        string
	PriorityLevel    RequestPriority
	Category         EndpointCategory
	ExecutionContext context.Context
	ActionFunction   func() (interface{}, error)
	ResponseChannel  chan KiteResponseEnvelope
	EnqueuedAt       time.Time
}

// KiteResponseEnvelope returns the outcome of the API operation back to the caller
type KiteResponseEnvelope struct {
	ResponseData      interface{}
	ResponseError     error
	ExecutionDuration time.Duration
}

// RateLimiterStatsDTO contains real-time diagnostic telemetry for the monitoring dashboard
type RateLimiterStatsDTO struct {
	EmergencyQueueDepth  int       `json:"emergency_queue_depth"`
	StandardQueueDepth   int       `json:"standard_queue_depth"`
	BackgroundQueueDepth int       `json:"background_queue_depth"`
	TotalDispatchedCount uint64    `json:"total_dispatched_count"`
	DroppedStaleCount    uint64    `json:"dropped_stale_count"`
	Http429BackoffCount  uint64    `json:"http_429_backoff_count"`
	IsCircuitPaused      bool      `json:"is_circuit_paused"`
	CircuitPauseExpiry   time.Time `json:"circuit_pause_expiry,omitempty"`
}

