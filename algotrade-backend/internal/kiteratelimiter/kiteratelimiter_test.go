package kiteratelimiter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTokenBucket_AcquireAndCadence(t *testing.T) {
	// 5 tokens/second, burst of 1
	tokenBucket := NewTokenBucket(5.0, 1.0)
	testContext := context.Background()

	// First acquire consumes the initial token immediately
	startTime := time.Now()
	acquireError := tokenBucket.Acquire(testContext)
	if acquireError != nil {
		t.Fatalf("first acquire failed: %v", acquireError)
	}

	// Second acquire must wait ~200ms (1.0 / 5.0 = 200ms)
	acquireError = tokenBucket.Acquire(testContext)
	if acquireError != nil {
		t.Fatalf("second acquire failed: %v", acquireError)
	}
	elapsedDuration := time.Since(startTime)
	if elapsedDuration < 150*time.Millisecond {
		t.Fatalf("expected rate pacing of ~200ms, but completed in %v", elapsedDuration)
	}
}

func TestDispatcher_PriorityPreemption(t *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// Drain general token bucket burst capacity so background tasks must queue waiting for tokens
	for drainIndex := 0; drainIndex < 4; drainIndex++ {
		dispatcher.generalTokenBucket.TryAcquire()
	}

	var executionOrder []string
	var orderMutex sync.Mutex

	// Fill the background queue with 5 background tasks
	for backgroundIndex := 1; backgroundIndex <= 5; backgroundIndex++ {
		taskName := fmt.Sprintf("BACKGROUND-%d", backgroundIndex)
		go func(name string) {
			_, _ = dispatcher.ExecuteBackgroundSync(context.Background(), func() (interface{}, error) {
				orderMutex.Lock()
				executionOrder = append(executionOrder, name)
				orderMutex.Unlock()
				return name, nil
			})
		}(taskName)
	}

	time.Sleep(15 * time.Millisecond) // Ensure background tasks are queued in backgroundSyncChannel

	// Now enqueue 1 emergency critical order
	emergencyDoneChannel := make(chan struct{})
	go func() {
		_, _ = dispatcher.ExecuteEmergencyOrder(context.Background(), func() (interface{}, error) {
			orderMutex.Lock()
			executionOrder = append(executionOrder, "EMERGENCY-STOP-LOSS")
			orderMutex.Unlock()
			close(emergencyDoneChannel)
			return "emergency_finished", nil
		})
	}()

	select {
	case <-emergencyDoneChannel:
	case <-time.After(3 * time.Second):
		t.Fatalf("emergency order timed out")
	}

	orderMutex.Lock()
	defer orderMutex.Unlock()
	if len(executionOrder) == 0 {
		t.Fatalf("expected executions, got none")
	}
	// The very first item executed must be the emergency stop-loss!
	if executionOrder[0] != "EMERGENCY-STOP-LOSS" {
		t.Fatalf("expected EMERGENCY-STOP-LOSS to preempt background tasks, but got: %s (full order: %v)", executionOrder[0], executionOrder)
	}
}

func TestDispatcher_JustInTimeContextCancellation(t *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// Create an already-canceled context
	canceledContext, cancelFunction := context.WithCancel(context.Background())
	cancelFunction()

	_, executionError := dispatcher.ExecuteStandardOrder(canceledContext, func() (interface{}, error) {
		t.Fatalf("action must not execute when context is canceled")
		return nil, nil
	})

	if executionError == nil {
		t.Fatalf("expected error on canceled context, got nil")
	}
}

func TestDispatcher_PanicRecovery(t *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	_, executionError := dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		panic("simulated unhandled nil pointer dereference in caller")
	})

	if executionError == nil {
		t.Fatalf("expected panic to be recovered as an error, got nil")
	}

	// Verify dispatcher survived and can execute subsequent requests
	response, nextError := dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		return "alive_and_well", nil
	})
	if nextError != nil || response != "alive_and_well" {
		t.Fatalf("dispatcher died after panic: resp=%v, err=%v", response, nextError)
	}
}

func TestDispatcher_Http429CircuitPause(t *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// Simulate an upstream Zerodha 429 response
	_, _ = dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		return nil, errors.New("429 Too Many Requests: Rate limit exceeded")
	})

	stats := dispatcher.GetStats()
	if stats.Http429BackoffCount != 1 {
		t.Fatalf("expected Http429BackoffCount to be 1, got %d", stats.Http429BackoffCount)
	}
	if !stats.IsCircuitPaused {
		t.Fatalf("expected IsCircuitPaused to be true after 429 error")
	}

	// Standard order should now be immediately rejected with ErrCircuitPaused during cooldown
	_, rejectedError := dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		return "should_not_run", nil
	})
	if !errors.Is(rejectedError, ErrCircuitPaused) {
		t.Fatalf("expected ErrCircuitPaused, got %v", rejectedError)
	}
}

func TestDispatcher_QueueSaturationProtection(t *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// Hold the dispatcher with a sleeping task
	blockerChannel := make(chan struct{})
	go func() {
		_, _ = dispatcher.ExecuteEmergencyOrder(context.Background(), func() (interface{}, error) {
			<-blockerChannel
			return nil, nil
		})
	}()

	time.Sleep(10 * time.Millisecond)

	// Rapidly fill the standard order queue beyond its capacity (128) using direct EnqueueRequest
	var saturatedCount int
	for requestIndex := 0; requestIndex < 200; requestIndex++ {
		envelope := KiteRequestEnvelope{
			RequestID:        fmt.Sprintf("REQ-SAT-%d", requestIndex),
			PriorityLevel:    PriorityStandardOrder,
			Category:         CategoryOrder,
			ExecutionContext: context.Background(),
			ActionFunction: func() (interface{}, error) {
				return "fast", nil
			},
			ResponseChannel: make(chan KiteResponseEnvelope, 1),
			EnqueuedAt:      time.Now(),
		}
		enqueueError := dispatcher.EnqueueRequest(envelope)
		if errors.Is(enqueueError, ErrQueueSaturated) {
			saturatedCount++
		}
	}

	close(blockerChannel)

	if saturatedCount == 0 {
		t.Fatalf("expected queue saturation rejections when exceeding buffer capacity")
	}
}

func TestDispatcher_ExecuteAccountSyncAndHistorical(t *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// 1. Account sync execution
	accountSyncResponse, accountSyncError := dispatcher.ExecuteAccountSync(context.Background(), func() (interface{}, error) {
		return "account_margins_synced", nil
	})
	if accountSyncError != nil || accountSyncResponse != "account_margins_synced" {
		t.Fatalf("account sync execution failed: resp=%v, err=%v", accountSyncResponse, accountSyncError)
	}

	// 2. Historical data execution
	historicalResponse, historicalError := dispatcher.ExecuteHistorical(context.Background(), func() (interface{}, error) {
		return "historical_candles_synced", nil
	})
	if historicalError != nil || historicalResponse != "historical_candles_synced" {
		t.Fatalf("historical execution failed: resp=%v, err=%v", historicalResponse, historicalError)
	}

	stats := dispatcher.GetStats()
	if stats.TotalDispatchedCount < 2 {
		t.Fatalf("expected at least 2 processed requests, got %d", stats.TotalDispatchedCount)
	}
}

func TestDispatcher_EmergencyPreemptionDuringSlowBackgroundCall(t *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	backgroundFinishedChannel := make(chan struct{})
	emergencyFinishedChannel := make(chan struct{})

	// 1. Launch a slow background task (simulating 200ms historical download)
	go func() {
		_, _ = dispatcher.ExecuteBackgroundSync(context.Background(), func() (interface{}, error) {
			time.Sleep(200 * time.Millisecond)
			close(backgroundFinishedChannel)
			return "slow_background_done", nil
		})
	}()

	// Give a tiny moment for the background task to be picked up by the dispatcher
	time.Sleep(20 * time.Millisecond)

	// 2. Launch an emergency stop loss order while background task is still in flight across the network
	startTime := time.Now()
	go func() {
		_, _ = dispatcher.ExecuteEmergencyOrder(context.Background(), func() (interface{}, error) {
			close(emergencyFinishedChannel)
			return "emergency_stop_done", nil
		})
	}()

	// 3. The emergency order must finish BEFORE the slow background task finishes
	select {
	case <-emergencyFinishedChannel:
		emergencyElapsed := time.Since(startTime)
		// Emergency order was dispatched and completed without waiting for the 200ms background call
		if emergencyElapsed > 150*time.Millisecond {
			t.Fatalf("emergency order was blocked by slow background call, elapsed: %v", emergencyElapsed)
		}
	case <-backgroundFinishedChannel:
		t.Fatalf("slow background call finished before emergency order! Head-of-line blocking occurred.")
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for execution")
	}

	<-backgroundFinishedChannel
}

func TestDispatcher_InterruptibleCircuitPauseOnContextCancel(t *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// 1. Trigger an upstream 429 to engage 1500ms circuit pause
	_, _ = dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		return nil, errors.New("429 Too Many Requests: Rate limit exceeded")
	})

	stats := dispatcher.GetStats()
	if !stats.IsCircuitPaused {
		t.Fatalf("expected circuit to be paused")
	}

	// 2. Submit an emergency order with a short 30ms context timeout
	shortContext, cancelTimeout := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelTimeout()

	startTime := time.Now()
	_, executionError := dispatcher.ExecuteEmergencyOrder(shortContext, func() (interface{}, error) {
		return "should_not_run", nil
	})

	elapsedDuration := time.Since(startTime)
	if executionError == nil {
		t.Fatalf("expected context timeout error, got nil")
	}
	// The wait should abort in ~30ms, not hang for the full 1500ms cooldown
	if elapsedDuration > 200*time.Millisecond {
		t.Fatalf("circuit pause sleep was not interrupted by context cancellation, elapsed: %v", elapsedDuration)
	}
}
