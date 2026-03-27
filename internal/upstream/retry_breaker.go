package upstream

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit open")

type RetryPolicy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

type CircuitBreaker struct {
	mu              sync.Mutex
	consecutiveFail int
	openUntil       time.Time
	failThreshold   int
	openFor         time.Duration
}

func NewCircuitBreaker(failThreshold int, openFor time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failThreshold: failThreshold,
		openFor:       openFor,
	}
}

func (cb *CircuitBreaker) Allow(now time.Time) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return now.After(cb.openUntil)
}

func (cb *CircuitBreaker) OnSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFail = 0
}

func (cb *CircuitBreaker) OnFailure(now time.Time) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFail++
	if cb.consecutiveFail >= cb.failThreshold {
		cb.openUntil = now.Add(cb.openFor)
	}
}

func DoWithRetry(ctx context.Context, pol RetryPolicy, fn func(context.Context) error) error {
	var last error
	for attempt := 0; attempt <= pol.MaxRetries; attempt++ {
		if err := fn(ctx); err == nil {
			return nil
		} else {
			last = err
		}

		// no sleep after last attempt
		if attempt == pol.MaxRetries {
			break
		}

		delay := pol.BaseDelay * time.Duration(1<<attempt)
		if delay > pol.MaxDelay {
			delay = pol.MaxDelay
		}
		// jitter 0.5x–1.5x
		jitter := 0.5 + rand.Float64()
		delay = time.Duration(float64(delay) * jitter)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return last
}