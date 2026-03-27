package ratelimit

import (
	"sync"
	"time"
)

type tokenBucket struct {
	tokens     float64
	last       time.Time
	rate       float64
	burst      float64
	lastAccess time.Time
}

type IPLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*tokenBucket
	rate      float64
	burst     float64
	evictIdle time.Duration
}

func NewIPLimiter(rps int, burst int, evictIdle time.Duration) *IPLimiter {
	limit := &IPLimiter{
		buckets:   make(map[string]*tokenBucket),
		rate:      float64(rps),
		burst:     float64(burst),
		evictIdle: evictIdle,
	}
	go limit.evictLoop()
	return limit
}

func (limit *IPLimiter) Allow(ip string) bool {
	now := time.Now()
	limit.mu.Lock()
	defer limit.mu.Unlock()

	bucket, ok := limit.buckets[ip]
	if !ok {
		bucket = &tokenBucket{tokens: limit.burst, last: now, rate: limit.rate, burst: limit.burst}
		limit.buckets[ip] = bucket
	}
	bucket.lastAccess = now

	// refill
	elapsed := now.Sub(bucket.last).Seconds()
	bucket.last = now
	bucket.tokens += elapsed * bucket.rate
	if bucket.tokens > bucket.burst {
		bucket.tokens = bucket.burst
	}

	if bucket.tokens >= 1 {
		bucket.tokens -= 1
		return true
	}
	return false
}

func (limit *IPLimiter) evictLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		limit.mu.Lock()
		for ip, bucket := range limit.buckets {
			if now.Sub(bucket.lastAccess) > limit.evictIdle {
				delete(limit.buckets, ip)
			}
		}
		limit.mu.Unlock()
	}
}