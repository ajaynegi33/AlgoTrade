package kiteratelimiter

import (
	"context"
	"math"
	"sync"
	"time"
)

// TokenBucket regulates execution cadence using a floating-point continuous refill algorithm
type TokenBucket struct {
	refillRatePerSecond float64
	burstCapacity       float64
	currentTokens       float64
	lastRefillTime      time.Time
	bucketMutex         sync.Mutex
}

// NewTokenBucket creates a token bucket initialized with full burst capacity
func NewTokenBucket(refillRatePerSecond, burstCapacity float64) *TokenBucket {
	if refillRatePerSecond <= 0 {
		refillRatePerSecond = 1.0
	}
	if burstCapacity <= 0 {
		burstCapacity = 1.0
	}
	return &TokenBucket{
		refillRatePerSecond: refillRatePerSecond,
		burstCapacity:       burstCapacity,
		currentTokens:       burstCapacity,
		lastRefillTime:      time.Now(),
	}
}

// TryAcquire attempts to consume one token immediately without blocking.
// Returns true if a token was granted, false otherwise.
func (bucket *TokenBucket) TryAcquire() bool {
	bucket.bucketMutex.Lock()
	defer bucket.bucketMutex.Unlock()

	currentTime := time.Now()
	elapsedSeconds := currentTime.Sub(bucket.lastRefillTime).Seconds()
	bucket.currentTokens = math.Min(bucket.burstCapacity, bucket.currentTokens+elapsedSeconds*bucket.refillRatePerSecond)
	bucket.lastRefillTime = currentTime

	if bucket.currentTokens >= 1.0 {
		bucket.currentTokens -= 1.0
		return true
	}
	return false
}

// WaitDurationForNextToken returns the duration until at least 1.0 token is available.
// Returns 0 if a token is already available.
func (bucket *TokenBucket) WaitDurationForNextToken() time.Duration {
	bucket.bucketMutex.Lock()
	defer bucket.bucketMutex.Unlock()

	currentTime := time.Now()
	elapsedSeconds := currentTime.Sub(bucket.lastRefillTime).Seconds()
	bucket.currentTokens = math.Min(bucket.burstCapacity, bucket.currentTokens+elapsedSeconds*bucket.refillRatePerSecond)
	bucket.lastRefillTime = currentTime

	if bucket.currentTokens >= 1.0 {
		return 0
	}
	remainingTokensNeeded := 1.0 - bucket.currentTokens
	return time.Duration((remainingTokensNeeded / bucket.refillRatePerSecond) * float64(time.Second))
}

// Acquire blocks until one token becomes available or the context is cancelled
func (bucket *TokenBucket) Acquire(executionContext context.Context) error {
	for {
		bucket.bucketMutex.Lock()
		currentTime := time.Now()
		elapsedSeconds := currentTime.Sub(bucket.lastRefillTime).Seconds()
		bucket.currentTokens = math.Min(bucket.burstCapacity, bucket.currentTokens+elapsedSeconds*bucket.refillRatePerSecond)
		bucket.lastRefillTime = currentTime

		if bucket.currentTokens >= 1.0 {
			bucket.currentTokens -= 1.0
			bucket.bucketMutex.Unlock()
			return nil
		}

		remainingTokensNeeded := 1.0 - bucket.currentTokens
		waitDuration := time.Duration((remainingTokensNeeded / bucket.refillRatePerSecond) * float64(time.Second))
		bucket.bucketMutex.Unlock()

		select {
		case <-executionContext.Done():
			return executionContext.Err()
		case <-time.After(waitDuration):
		}
	}
}

