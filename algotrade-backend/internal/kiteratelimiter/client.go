package kiteratelimiter

import (
	"context"
	"fmt"
	"time"
)

// ExecuteRequest submits an operation through the centralized rate limiter and awaits the result synchronously
func (dispatcher *CentralKiteDispatcher) ExecuteRequest(
	executionContext context.Context,
	priorityLevel RequestPriority,
	endpointCategory EndpointCategory,
	actionFunction func() (interface{}, error),
) (interface{}, error) {
	if dispatcher == nil || dispatcher.isStopped.Load() {
		return nil, ErrDispatcherStopped
	}
	if executionContext == nil {
		executionContext = context.Background()
	}
	if executionContext.Err() != nil {
		dispatcher.droppedStaleCount.Add(1)
		return nil, executionContext.Err()
	}

	requestID := fmt.Sprintf("REQ-%d", time.Now().UnixNano())
	responseChannel := make(chan KiteResponseEnvelope, 1)

	envelope := KiteRequestEnvelope{
		RequestID:        requestID,
		PriorityLevel:    priorityLevel,
		Category:         endpointCategory,
		ExecutionContext: executionContext,
		ActionFunction:   actionFunction,
		ResponseChannel:  responseChannel,
		EnqueuedAt:       time.Now(),
	}

	enqueueError := dispatcher.EnqueueRequest(envelope)
	if enqueueError != nil {
		return nil, enqueueError
	}

	select {
	case responseEnvelope := <-responseChannel:
		return responseEnvelope.ResponseData, responseEnvelope.ResponseError
	case <-executionContext.Done():
		return nil, executionContext.Err()
	case <-dispatcher.dispatcherContext.Done():
		return nil, ErrDispatcherStopped
	}
}

// ExecuteEmergencyOrder routes a critical stop-loss or market exit at top priority
func (dispatcher *CentralKiteDispatcher) ExecuteEmergencyOrder(
	executionContext context.Context,
	actionFunction func() (interface{}, error),
) (interface{}, error) {
	return dispatcher.ExecuteRequest(executionContext, PriorityEmergencyCritical, CategoryOrder, actionFunction)
}

// ExecuteStandardOrder routes a new limit buy order or standard scale-out order
func (dispatcher *CentralKiteDispatcher) ExecuteStandardOrder(
	executionContext context.Context,
	actionFunction func() (interface{}, error),
) (interface{}, error) {
	return dispatcher.ExecuteRequest(executionContext, PriorityStandardOrder, CategoryOrder, actionFunction)
}

// ExecuteAccountSync routes live margin checks and order ledger reconciliations
func (dispatcher *CentralKiteDispatcher) ExecuteAccountSync(
	executionContext context.Context,
	actionFunction func() (interface{}, error),
) (interface{}, error) {
	return dispatcher.ExecuteRequest(executionContext, PriorityAccountSync, CategoryGeneral, actionFunction)
}

// ExecuteHistorical routes historical candle downloads subject to Zerodha's 3 req/sec ceiling
func (dispatcher *CentralKiteDispatcher) ExecuteHistorical(
	executionContext context.Context,
	actionFunction func() (interface{}, error),
) (interface{}, error) {
	return dispatcher.ExecuteRequest(executionContext, PriorityBackgroundSync, CategoryHistorical, actionFunction)
}

// ExecuteBackgroundSync routes universe leverage batch syncs and screener scans
func (dispatcher *CentralKiteDispatcher) ExecuteBackgroundSync(
	executionContext context.Context,
	actionFunction func() (interface{}, error),
) (interface{}, error) {
	return dispatcher.ExecuteRequest(executionContext, PriorityBackgroundSync, CategoryGeneral, actionFunction)
}

