package kiteratelimiter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CentralKiteDispatcher is the single-writer actor managing prioritized channels and rate tokens
type CentralKiteDispatcher struct {
	emergencyPriorityChannel chan KiteRequestEnvelope
	standardOrderChannel     chan KiteRequestEnvelope
	backgroundSyncChannel    chan KiteRequestEnvelope

	orderTokenBucket      *TokenBucket
	historicalTokenBucket *TokenBucket
	generalTokenBucket    *TokenBucket

	circuitPauseUntil time.Time
	circuitPauseMutex sync.RWMutex

	totalDispatchedCount atomic.Uint64
	droppedStaleCount    atomic.Uint64
	http429BackoffCount  atomic.Uint64

	dispatcherContext context.Context
	cancelDispatcher  context.CancelFunc
	shutdownWaitGroup sync.WaitGroup
	isStopped         atomic.Bool
}

// NewCentralKiteDispatcher initializes and starts the central dispatcher loop
func NewCentralKiteDispatcher() *CentralKiteDispatcher {
	dispatcherContext, cancelDispatcher := context.WithCancel(context.Background())

	dispatcher := &CentralKiteDispatcher{
		emergencyPriorityChannel: make(chan KiteRequestEnvelope, 64),
		standardOrderChannel:     make(chan KiteRequestEnvelope, 128),
		backgroundSyncChannel:    make(chan KiteRequestEnvelope, 512),
		orderTokenBucket:         NewTokenBucket(8.0, 4.0), // 8.0 req/sec (Official limit: 10/sec)
		historicalTokenBucket:    NewTokenBucket(2.5, 2.0), // 2.5 req/sec (Official limit: 3/sec)
		generalTokenBucket:       NewTokenBucket(8.0, 4.0), // 8.0 req/sec (Official limit: 10/sec)
		dispatcherContext:        dispatcherContext,
		cancelDispatcher:         cancelDispatcher,
	}

	dispatcher.shutdownWaitGroup.Add(1)
	go dispatcher.runLoop()

	slog.Info("Centralized Kite Channel Rate Limiter initialized",
		"order_rate_limit", "8.0 req/s",
		"historical_rate_limit", "2.5 req/s",
		"general_rate_limit", "8.0 req/s",
	)
	return dispatcher
}

// Stop shuts down the central dispatcher and drains remaining critical requests
func (dispatcher *CentralKiteDispatcher) Stop() {
	if dispatcher.isStopped.CompareAndSwap(false, true) {
		dispatcher.cancelDispatcher()
		dispatcher.shutdownWaitGroup.Wait()
		slog.Info("Centralized Kite Channel Rate Limiter stopped")
	}
}

// EnqueueRequest routes a request envelope to the appropriate priority channel
func (dispatcher *CentralKiteDispatcher) EnqueueRequest(envelope KiteRequestEnvelope) error {
	if dispatcher.isStopped.Load() {
		return ErrDispatcherStopped
	}

	switch envelope.PriorityLevel {
	case PriorityEmergencyCritical:
		// Emergency orders (stop-loss modifications, market liquidation sweeps) must never be dropped
		select {
		case dispatcher.emergencyPriorityChannel <- envelope:
			return nil
		case <-envelope.ExecutionContext.Done():
			return envelope.ExecutionContext.Err()
		case <-time.After(2 * time.Second):
			return ErrQueueSaturated
		}

	case PriorityStandardOrder, PriorityAccountSync:
		// Standard trade entries and live margin refreshes
		select {
		case dispatcher.standardOrderChannel <- envelope:
			return nil
		case <-envelope.ExecutionContext.Done():
			return envelope.ExecutionContext.Err()
		default:
			return ErrQueueSaturated
		}

	case PriorityBackgroundSync:
		// Background 20-day historical data backfills and screener scans
		select {
		case dispatcher.backgroundSyncChannel <- envelope:
			return nil
		case <-envelope.ExecutionContext.Done():
			return envelope.ExecutionContext.Err()
		default:
			return ErrQueueSaturated
		}

	default:
		select {
		case dispatcher.standardOrderChannel <- envelope:
			return nil
		default:
			return ErrQueueSaturated
		}
	}
}

// runLoop executes the prioritized event loop draining emergency orders first
func (dispatcher *CentralKiteDispatcher) runLoop() {
	defer dispatcher.shutdownWaitGroup.Done()

	for {
		// Tier 1: Check emergency channel immediately
		select {
		case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
			dispatcher.processEnvelope(emergencyEnvelope)
			continue
		case <-dispatcher.dispatcherContext.Done():
			return
		default:
		}

		// Tier 2: Check standard order channel
		select {
		case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
			dispatcher.processEnvelope(emergencyEnvelope)
			continue
		case standardEnvelope := <-dispatcher.standardOrderChannel:
			dispatcher.processEnvelope(standardEnvelope)
			continue
		case <-dispatcher.dispatcherContext.Done():
			return
		default:
		}

		// Tier 3: Block on all three priority channels simultaneously
		select {
		case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
			dispatcher.processEnvelope(emergencyEnvelope)
		case standardEnvelope := <-dispatcher.standardOrderChannel:
			dispatcher.processEnvelope(standardEnvelope)
		case backgroundEnvelope := <-dispatcher.backgroundSyncChannel:
			dispatcher.processEnvelope(backgroundEnvelope)
		case <-dispatcher.dispatcherContext.Done():
			return
		}
	}
}

// processEnvelope validates context, acquires rate tokens, and executes the action safely
func (dispatcher *CentralKiteDispatcher) processEnvelope(envelope KiteRequestEnvelope) {
	// 1. Just-In-Time Context Expiration Check (Drop stale requests without wasting rate limit tokens)
	if envelope.ExecutionContext.Err() != nil {
		dispatcher.droppedStaleCount.Add(1)
		dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
			ResponseError: ErrRequestTimedOutInQueue,
		})
		return
	}

	// 2. Check for active HTTP 429 Circuit Pause
	pauseDuration := dispatcher.getActiveCircuitPauseDuration()
	if pauseDuration > 0 {
		if envelope.PriorityLevel == PriorityEmergencyCritical {
			// For emergency orders, wait out the remaining cooldown rather than dropping
			time.Sleep(pauseDuration)
		} else {
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: ErrCircuitPaused,
			})
			return
		}
	}

	// 3. Select endpoint-specific token bucket
	var tokenBucket *TokenBucket
	switch envelope.Category {
	case CategoryHistorical:
		tokenBucket = dispatcher.historicalTokenBucket
	case CategoryOrder:
		tokenBucket = dispatcher.orderTokenBucket
	default:
		tokenBucket = dispatcher.generalTokenBucket
	}

	// 4. Block until rate token is available
	tokenAcquireError := tokenBucket.Acquire(envelope.ExecutionContext)
	if tokenAcquireError != nil {
		dispatcher.droppedStaleCount.Add(1)
		dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
			ResponseError: tokenAcquireError,
		})
		return
	}

	// 5. Execute action closure safely with panic recovery
	executionStartTime := time.Now()
	var responseData interface{}
	var responseError error

	func() {
		defer func() {
			if recoveredPanic := recover(); recoveredPanic != nil {
				responseError = fmt.Errorf("panic during kite API invocation: %v", recoveredPanic)
			}
		}()
		responseData, responseError = envelope.ActionFunction()
	}()

	executionDuration := time.Since(executionStartTime)

	// 6. Inspect error for HTTP 429 rate limit backoff
	if responseError != nil {
		errorString := strings.ToLower(responseError.Error())
		if strings.Contains(errorString, "429") || strings.Contains(errorString, "too many requests") {
			dispatcher.http429BackoffCount.Add(1)
			slog.Warn("Upstream Zerodha Kite HTTP 429 detected; activating 1500ms circuit pause",
				"request_id", envelope.RequestID,
				"category", envelope.Category,
			)
			dispatcher.setCircuitPause(time.Now().Add(1500 * time.Millisecond))
		}
	}

	dispatcher.totalDispatchedCount.Add(1)
	dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
		ResponseData:      responseData,
		ResponseError:     responseError,
		ExecutionDuration: executionDuration,
	})
}

func (dispatcher *CentralKiteDispatcher) deliverResponse(responseChannel chan KiteResponseEnvelope, responseEnvelope KiteResponseEnvelope) {
	if responseChannel == nil {
		return
	}
	select {
	case responseChannel <- responseEnvelope:
	default:
		// Caller abandoned waiting channel; discard without hanging
	}
}

func (dispatcher *CentralKiteDispatcher) setCircuitPause(pauseExpiry time.Time) {
	dispatcher.circuitPauseMutex.Lock()
	defer dispatcher.circuitPauseMutex.Unlock()
	dispatcher.circuitPauseUntil = pauseExpiry
}

func (dispatcher *CentralKiteDispatcher) getActiveCircuitPauseDuration() time.Duration {
	dispatcher.circuitPauseMutex.RLock()
	defer dispatcher.circuitPauseMutex.RUnlock()
	if dispatcher.circuitPauseUntil.After(time.Now()) {
		return time.Until(dispatcher.circuitPauseUntil)
	}
	return 0
}

// GetStats returns real-time diagnostic telemetry for monitoring and health dashboards
func (dispatcher *CentralKiteDispatcher) GetStats() RateLimiterStatsDTO {
	dispatcher.circuitPauseMutex.RLock()
	pauseExpiry := dispatcher.circuitPauseUntil
	isPaused := pauseExpiry.After(time.Now())
	dispatcher.circuitPauseMutex.RUnlock()

	return RateLimiterStatsDTO{
		EmergencyQueueDepth:  len(dispatcher.emergencyPriorityChannel),
		StandardQueueDepth:   len(dispatcher.standardOrderChannel),
		BackgroundQueueDepth: len(dispatcher.backgroundSyncChannel),
		TotalDispatchedCount: dispatcher.totalDispatchedCount.Load(),
		DroppedStaleCount:    dispatcher.droppedStaleCount.Load(),
		Http429BackoffCount:  dispatcher.http429BackoffCount.Load(),
		IsCircuitPaused:      isPaused,
		CircuitPauseExpiry:   pauseExpiry,
	}
}

