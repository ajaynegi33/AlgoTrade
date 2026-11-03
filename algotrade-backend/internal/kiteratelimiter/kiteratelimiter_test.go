package kiteratelimiter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTokenBucket_AcquireAndCadence(testingInstance *testing.T) {
	// 5 tokens/second, burst of 1
	tokenBucket := NewTokenBucket(5.0, 1.0)
	testContext := context.Background()

	// First acquire consumes the initial token immediately
	startTime := time.Now()
	acquireError := tokenBucket.Acquire(testContext)
	if acquireError != nil {
		testingInstance.Fatalf("first acquire failed: %v", acquireError)
	}

	// Second acquire must wait ~200ms (1.0 / 5.0 = 200ms)
	acquireError = tokenBucket.Acquire(testContext)
	if acquireError != nil {
		testingInstance.Fatalf("second acquire failed: %v", acquireError)
	}
	elapsedDuration := time.Since(startTime)
	if elapsedDuration < 150*time.Millisecond {
		testingInstance.Fatalf("expected rate pacing of ~200ms, but completed in %v", elapsedDuration)
	}
}

func TestDispatcher_PriorityPreemption(testingInstance *testing.T) {
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
		testingInstance.Fatalf("emergency order timed out")
	}

	orderMutex.Lock()
	defer orderMutex.Unlock()
	if len(executionOrder) == 0 {
		testingInstance.Fatalf("expected executions, got none")
	}
	// The very first item executed must be the emergency stop-loss!
	if executionOrder[0] != "EMERGENCY-STOP-LOSS" {
		testingInstance.Fatalf("expected EMERGENCY-STOP-LOSS to preempt background tasks, but got: %s (full order: %v)", executionOrder[0], executionOrder)
	}
}

func TestDispatcher_JustInTimeContextCancellation(testingInstance *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// Create an already-canceled context
	canceledContext, cancelFunction := context.WithCancel(context.Background())
	cancelFunction()

	_, executionError := dispatcher.ExecuteStandardOrder(canceledContext, func() (interface{}, error) {
		testingInstance.Fatalf("action must not execute when context is canceled")
		return nil, nil
	})

	if executionError == nil {
		testingInstance.Fatalf("expected error on canceled context, got nil")
	}
}

func TestDispatcher_PanicRecovery(testingInstance *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	_, executionError := dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		panic("simulated unhandled nil pointer dereference in caller")
	})

	if executionError == nil {
		testingInstance.Fatalf("expected panic to be recovered as an error, got nil")
	}

	// Verify dispatcher survived and can execute subsequent requests
	response, nextError := dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		return "alive_and_well", nil
	})
	if nextError != nil || response != "alive_and_well" {
		testingInstance.Fatalf("dispatcher died after panic: resp=%v, err=%v", response, nextError)
	}
}

func TestDispatcher_Http429CircuitPause(testingInstance *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// Simulate an upstream Zerodha 429 response
	_, _ = dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		return nil, errors.New("429 Too Many Requests: Rate limit exceeded")
	})

	stats := dispatcher.GetStats()
	if stats.Http429BackoffCount != 1 {
		testingInstance.Fatalf("expected Http429BackoffCount to be 1, got %d", stats.Http429BackoffCount)
	}
	if !stats.IsCircuitPaused {
		testingInstance.Fatalf("expected IsCircuitPaused to be true after 429 error")
	}

	// Standard order should now be immediately rejected with ErrCircuitPaused during cooldown
	_, rejectedError := dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		return "should_not_run", nil
	})
	if !errors.Is(rejectedError, ErrCircuitPaused) {
		testingInstance.Fatalf("expected ErrCircuitPaused, got %v", rejectedError)
	}
}

func TestDispatcher_QueueSaturationProtection(testingInstance *testing.T) {
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
		testingInstance.Fatalf("expected queue saturation rejections when exceeding buffer capacity")
	}
}

func TestDispatcher_ExecuteAccountSyncAndHistorical(testingInstance *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// 1. Account sync execution
	accountSyncResponse, accountSyncError := dispatcher.ExecuteAccountSync(context.Background(), func() (interface{}, error) {
		return "account_margins_synced", nil
	})
	if accountSyncError != nil || accountSyncResponse != "account_margins_synced" {
		testingInstance.Fatalf("account sync execution failed: resp=%v, err=%v", accountSyncResponse, accountSyncError)
	}

	// 2. Historical data execution
	historicalResponse, historicalError := dispatcher.ExecuteHistorical(context.Background(), func() (interface{}, error) {
		return "historical_candles_synced", nil
	})
	if historicalError != nil || historicalResponse != "historical_candles_synced" {
		testingInstance.Fatalf("historical execution failed: resp=%v, err=%v", historicalResponse, historicalError)
	}

	stats := dispatcher.GetStats()
	if stats.TotalDispatchedCount < 2 {
		testingInstance.Fatalf("expected at least 2 processed requests, got %d", stats.TotalDispatchedCount)
	}
}

func TestDispatcher_EmergencyPreemptionDuringSlowBackgroundCall(testingInstance *testing.T) {
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
			testingInstance.Fatalf("emergency order was blocked by slow background call, elapsed: %v", emergencyElapsed)
		}
	case <-backgroundFinishedChannel:
		testingInstance.Fatalf("slow background call finished before emergency order! Head-of-line blocking occurred.")
	case <-time.After(3 * time.Second):
		testingInstance.Fatalf("timed out waiting for execution")
	}

	<-backgroundFinishedChannel
}

func TestDispatcher_InterruptibleCircuitPauseOnContextCancel(testingInstance *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// 1. Trigger an upstream 429 to engage 1500ms circuit pause
	_, _ = dispatcher.ExecuteStandardOrder(context.Background(), func() (interface{}, error) {
		return nil, errors.New("429 Too Many Requests: Rate limit exceeded")
	})

	stats := dispatcher.GetStats()
	if !stats.IsCircuitPaused {
		testingInstance.Fatalf("expected circuit to be paused")
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
		testingInstance.Fatalf("expected context timeout error, got nil")
	}
	// The wait should abort in ~30ms, not hang for the full 1500ms cooldown
	if elapsedDuration > 200*time.Millisecond {
		testingInstance.Fatalf("circuit pause sleep was not interrupted by context cancellation, elapsed: %v", elapsedDuration)
	}
}

func TestPhase3_CanceledContextStressAndCategoryBucketIsolation(testingContext *testing.T) {
	dispatcher := NewCentralKiteDispatcher()
	defer dispatcher.Stop()

	// 1. 1,000-Iteration Canceled Context Stress Test (VULN-2):
	// Submitting a pre-canceled context across all priorities and direct EnqueueRequest must NEVER execute ActionFunction.
	canceledContext, cancelFunction := context.WithCancel(context.Background())
	cancelFunction()

	for iteration := 0; iteration < 1000; iteration++ {
		_, err := dispatcher.ExecuteStandardOrder(canceledContext, func() (interface{}, error) {
			testingContext.Fatalf("VULN-2 regression: canceled StandardOrder executed on iteration %d", iteration)
			return nil, nil
		})
		if err == nil {
			testingContext.Fatalf("expected error on canceled StandardOrder at iteration %d", iteration)
		}

		enqueueErr := dispatcher.EnqueueRequest(KiteRequestEnvelope{
			RequestID:        fmt.Sprintf("CANCELED-ENQ-%d", iteration),
			PriorityLevel:    PriorityStandardOrder,
			Category:         CategoryOrder,
			ExecutionContext: canceledContext,
			ActionFunction: func() (interface{}, error) {
				testingContext.Fatalf("VULN-2 regression: canceled EnqueueRequest executed on iteration %d", iteration)
				return nil, nil
			},
			ResponseChannel: make(chan KiteResponseEnvelope, 1),
			EnqueuedAt:      time.Now(),
		})
		if enqueueErr == nil {
			testingContext.Fatalf("expected EnqueueRequest to reject canceled context at iteration %d", iteration)
		}
	}

	// 2. Category Bucket Isolation:
	// Exhaust historicalTokenBucket (2.0 burst capacity), while leaving generalTokenBucket (4.0 burst capacity) untouched.
	// A 3rd CategoryHistorical request must wait ~400ms (1.0 / 2.5 req/s) rather than bypassing via generalTokenBucket!
	for drainIndex := 0; drainIndex < 2; drainIndex++ {
		dispatcher.historicalTokenBucket.TryAcquire()
	}
	historicalStart := time.Now()
	_, histErr := dispatcher.ExecuteHistorical(context.Background(), func() (interface{}, error) {
		return "candles", nil
	})
	if histErr != nil {
		testingContext.Fatalf("ExecuteHistorical failed: %v", histErr)
	}
	historicalElapsed := time.Since(historicalStart)
	if historicalElapsed < 300*time.Millisecond {
		testingContext.Fatalf("expected CategoryHistorical to be throttled by historicalTokenBucket (~400ms), but completed in %v", historicalElapsed)
	}

	// 3. AccountSync (CategoryGeneral) must use generalTokenBucket and NOT drain orderTokenBucket:
	for drainIndex := 0; drainIndex < 4; drainIndex++ {
		dispatcher.orderTokenBucket.TryAcquire()
	}
	accountStart := time.Now()
	_, acctErr := dispatcher.ExecuteAccountSync(context.Background(), func() (interface{}, error) {
		return "margins", nil
	})
	if acctErr != nil {
		testingContext.Fatalf("ExecuteAccountSync failed: %v", acctErr)
	}
	if time.Since(accountStart) > 80*time.Millisecond {
		testingContext.Fatalf("expected ExecuteAccountSync (CategoryGeneral) to use generalTokenBucket immediately even when orderTokenBucket is empty")
	}
}

