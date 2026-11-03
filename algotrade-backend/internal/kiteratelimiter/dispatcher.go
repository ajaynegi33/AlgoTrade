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
	inFlightWaitGroup sync.WaitGroup
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
		dispatcher.inFlightWaitGroup.Wait()
		slog.Info("Centralized Kite Channel Rate Limiter stopped")
	}
}

// EnqueueRequest routes a request envelope to the appropriate priority channel
func (dispatcher *CentralKiteDispatcher) EnqueueRequest(envelope KiteRequestEnvelope) error {
	if dispatcher.isStopped.Load() {
		return ErrDispatcherStopped
	}
	if envelope.ExecutionContext == nil {
		envelope.ExecutionContext = context.Background()
	}
	if envelope.ExecutionContext.Err() != nil {
		dispatcher.droppedStaleCount.Add(1)
		return envelope.ExecutionContext.Err()
	}

	switch envelope.PriorityLevel {
	case PriorityEmergencyCritical:
		// Emergency orders (stop-loss modifications, market liquidation sweeps) must never be dropped
		select {
		case dispatcher.emergencyPriorityChannel <- envelope:
			return nil
		case <-envelope.ExecutionContext.Done():
			dispatcher.droppedStaleCount.Add(1)
			return envelope.ExecutionContext.Err()
		case <-dispatcher.dispatcherContext.Done():
			return ErrDispatcherStopped
		case <-time.After(2 * time.Second):
			return ErrQueueSaturated
		}

	case PriorityStandardOrder, PriorityAccountSync:
		// Standard trade entries and live margin refreshes
		select {
		case dispatcher.standardOrderChannel <- envelope:
			return nil
		case <-envelope.ExecutionContext.Done():
			dispatcher.droppedStaleCount.Add(1)
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
			dispatcher.droppedStaleCount.Add(1)
			return envelope.ExecutionContext.Err()
		default:
			return ErrQueueSaturated
		}

	default:
		select {
		case dispatcher.standardOrderChannel <- envelope:
			return nil
		case <-envelope.ExecutionContext.Done():
			dispatcher.droppedStaleCount.Add(1)
			return envelope.ExecutionContext.Err()
		default:
			return ErrQueueSaturated
		}
	}
}

func (dispatcher *CentralKiteDispatcher) getTokenBucketForCategory(category EndpointCategory) *TokenBucket {
	switch category {
	case CategoryHistorical:
		return dispatcher.historicalTokenBucket
	case CategoryGeneral:
		return dispatcher.generalTokenBucket
	default:
		return dispatcher.orderTokenBucket
	}
}

func (dispatcher *CentralKiteDispatcher) drainRemainingQueuesOnShutdown() {
	for {
		select {
		case envelope := <-dispatcher.emergencyPriorityChannel:
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{ResponseError: ErrDispatcherStopped})
		case envelope := <-dispatcher.standardOrderChannel:
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{ResponseError: ErrDispatcherStopped})
		case envelope := <-dispatcher.backgroundSyncChannel:
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{ResponseError: ErrDispatcherStopped})
		default:
			return
		}
	}
}

// runLoop executes the prioritized event loop draining emergency orders first
func (dispatcher *CentralKiteDispatcher) runLoop() {
	defer dispatcher.shutdownWaitGroup.Done()
	defer dispatcher.drainRemainingQueuesOnShutdown()

	for {
		// Tier 1: Drain any waiting emergency requests first
		select {
		case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
			dispatcher.processEmergencyEnvelope(emergencyEnvelope)
			continue
		case <-dispatcher.dispatcherContext.Done():
			return
		default:
		}

		// Tier 2: Drain standard orders and account sync requests (if no emergency request is waiting)
		select {
		case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
			dispatcher.processEmergencyEnvelope(emergencyEnvelope)
			continue
		case standardEnvelope := <-dispatcher.standardOrderChannel:
			dispatcher.processStandardEnvelope(standardEnvelope)
			continue
		case <-dispatcher.dispatcherContext.Done():
			return
		default:
		}

		// Tier 3: Block on all three priority channels simultaneously when idle or draining background queue
		select {
		case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
			dispatcher.processEmergencyEnvelope(emergencyEnvelope)
		case standardEnvelope := <-dispatcher.standardOrderChannel:
			select {
			case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
				dispatcher.processEmergencyEnvelope(emergencyEnvelope)
			default:
			}
			dispatcher.processStandardEnvelope(standardEnvelope)
		case backgroundEnvelope := <-dispatcher.backgroundSyncChannel:
			select {
			case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
				dispatcher.processEmergencyEnvelope(emergencyEnvelope)
			case standardEnvelope := <-dispatcher.standardOrderChannel:
				dispatcher.processStandardEnvelope(standardEnvelope)
			default:
			}
			dispatcher.processBackgroundEnvelope(backgroundEnvelope)
		case <-dispatcher.dispatcherContext.Done():
			return
		}
	}
}

// processEmergencyEnvelope immediately acquires order tokens and launches critical emergency actions
func (dispatcher *CentralKiteDispatcher) processEmergencyEnvelope(envelope KiteRequestEnvelope) {
	if envelope.ExecutionContext != nil && envelope.ExecutionContext.Err() != nil {
		dispatcher.droppedStaleCount.Add(1)
		dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
			ResponseError: ErrRequestTimedOutInQueue,
		})
		return
	}

	tokenBucket := dispatcher.getTokenBucketForCategory(envelope.Category)
	for {
		if envelope.ExecutionContext != nil && envelope.ExecutionContext.Err() != nil {
			dispatcher.droppedStaleCount.Add(1)
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: envelope.ExecutionContext.Err(),
			})
			return
		}
		if tokenBucket.TryAcquire() {
			break
		}
		waitDuration := tokenBucket.WaitDurationForNextToken()
		select {
		case <-time.After(waitDuration):
		case <-envelope.ExecutionContext.Done():
			dispatcher.droppedStaleCount.Add(1)
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: envelope.ExecutionContext.Err(),
			})
			return
		case <-dispatcher.dispatcherContext.Done():
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: ErrDispatcherStopped,
			})
			return
		}
	}

	dispatcher.executeWithGrantedToken(envelope)
}

// processStandardEnvelope handles standard order token acquisition and dispatch
func (dispatcher *CentralKiteDispatcher) processStandardEnvelope(envelope KiteRequestEnvelope) {
	if envelope.ExecutionContext != nil && envelope.ExecutionContext.Err() != nil {
		dispatcher.droppedStaleCount.Add(1)
		dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
			ResponseError: ErrRequestTimedOutInQueue,
		})
		return
	}

	if dispatcher.getActiveCircuitPauseDuration() > 0 {
		dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
			ResponseError: ErrCircuitPaused,
		})
		return
	}

	tokenBucket := dispatcher.getTokenBucketForCategory(envelope.Category)
	for {
		if envelope.ExecutionContext != nil && envelope.ExecutionContext.Err() != nil {
			dispatcher.droppedStaleCount.Add(1)
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: envelope.ExecutionContext.Err(),
			})
			return
		}

		if tokenBucket.TryAcquire() {
			break
		}

		waitDuration := tokenBucket.WaitDurationForNextToken()
		select {
		case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
			dispatcher.processEmergencyEnvelope(emergencyEnvelope)
		case <-time.After(waitDuration):
		case <-envelope.ExecutionContext.Done():
			dispatcher.droppedStaleCount.Add(1)
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: envelope.ExecutionContext.Err(),
			})
			return
		case <-dispatcher.dispatcherContext.Done():
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: ErrDispatcherStopped,
			})
			return
		}
	}

	dispatcher.executeWithGrantedToken(envelope)
}

// processBackgroundEnvelope handles background task token acquisition and dispatch
func (dispatcher *CentralKiteDispatcher) processBackgroundEnvelope(envelope KiteRequestEnvelope) {
	if envelope.ExecutionContext != nil && envelope.ExecutionContext.Err() != nil {
		dispatcher.droppedStaleCount.Add(1)
		dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
			ResponseError: ErrRequestTimedOutInQueue,
		})
		return
	}

	if dispatcher.getActiveCircuitPauseDuration() > 0 {
		dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
			ResponseError: ErrCircuitPaused,
		})
		return
	}

	tokenBucket := dispatcher.getTokenBucketForCategory(envelope.Category)

	for {
		if envelope.ExecutionContext != nil && envelope.ExecutionContext.Err() != nil {
			dispatcher.droppedStaleCount.Add(1)
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: envelope.ExecutionContext.Err(),
			})
			return
		}

		if tokenBucket.TryAcquire() {
			break
		}

		waitDuration := tokenBucket.WaitDurationForNextToken()
		select {
		case emergencyEnvelope := <-dispatcher.emergencyPriorityChannel:
			dispatcher.processEmergencyEnvelope(emergencyEnvelope)
		case standardEnvelope := <-dispatcher.standardOrderChannel:
			dispatcher.processStandardEnvelope(standardEnvelope)
		case <-time.After(waitDuration):
		case <-envelope.ExecutionContext.Done():
			dispatcher.droppedStaleCount.Add(1)
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: envelope.ExecutionContext.Err(),
			})
			return
		case <-dispatcher.dispatcherContext.Done():
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: ErrDispatcherStopped,
			})
			return
		}
	}

	dispatcher.executeWithGrantedToken(envelope)
}

// executeWithGrantedToken launches the action closure in an asynchronous worker goroutine
func (dispatcher *CentralKiteDispatcher) executeWithGrantedToken(envelope KiteRequestEnvelope) {
	dispatcher.inFlightWaitGroup.Add(1)
	go func(requestEnvelope KiteRequestEnvelope) {
		defer dispatcher.inFlightWaitGroup.Done()
		dispatcher.executeWorker(requestEnvelope)
	}(envelope)
}

// executeWorker handles circuit pauses, panic recovery, and returns responses
func (dispatcher *CentralKiteDispatcher) executeWorker(envelope KiteRequestEnvelope) {
	if envelope.ExecutionContext != nil && envelope.ExecutionContext.Err() != nil {
		dispatcher.droppedStaleCount.Add(1)
		dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
			ResponseError: envelope.ExecutionContext.Err(),
		})
		return
	}

	// 1. Check for active HTTP 429 Circuit Pause with interruptible wait for emergency orders
	pauseDuration := dispatcher.getActiveCircuitPauseDuration()
	if pauseDuration > 0 {
		if envelope.PriorityLevel == PriorityEmergencyCritical {
			select {
			case <-time.After(pauseDuration):
			case <-envelope.ExecutionContext.Done():
				dispatcher.droppedStaleCount.Add(1)
				dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
					ResponseError: envelope.ExecutionContext.Err(),
				})
				return
			case <-dispatcher.dispatcherContext.Done():
				dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
					ResponseError: ErrDispatcherStopped,
				})
				return
			}
		} else {
			dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
				ResponseError: ErrCircuitPaused,
			})
			return
		}
	}

	// Re-verify context after any circuit pause wait before invoking the live API closure
	if envelope.ExecutionContext != nil && envelope.ExecutionContext.Err() != nil {
		dispatcher.droppedStaleCount.Add(1)
		dispatcher.deliverResponse(envelope.ResponseChannel, KiteResponseEnvelope{
			ResponseError: envelope.ExecutionContext.Err(),
		})
		return
	}

	// 2. Execute action closure safely with panic recovery
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

	// 3. Inspect error for HTTP 429 rate limit backoff
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

