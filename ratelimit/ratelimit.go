package ratelimit

import (
	"sync"
	"time"
)

type RateLimiter struct {
	mu          sync.Mutex
	requests    map[string][]time.Time
	ratePerSec  int
	windowSize  time.Duration
	cleanupTick *time.Ticker
	stopCleanup chan bool
}

func NewRateLimiter(requestsPerSecond int) *RateLimiter {
	rl := &RateLimiter{
		requests:    make(map[string][]time.Time),
		ratePerSec:  requestsPerSecond,
		windowSize:  time.Second,
		stopCleanup: make(chan bool),
	}

	rl.cleanupTick = time.NewTicker(1 * time.Minute)
	go rl.cleanup() // cleanup old entries periodically

	return rl
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.windowSize)

	requests, exists := rl.requests[key]
	if !exists {
		requests = make([]time.Time, 0)
	}

	// filter out old requests
	filtered := make([]time.Time, 0)
	for _, reqTime := range requests {
		if reqTime.After(cutoff) {
			filtered = append(filtered, reqTime)
		}
	}

	if len(filtered) >= rl.ratePerSec {
		rl.requests[key] = filtered
		return false
	}

	filtered = append(filtered, now)
	rl.requests[key] = filtered
	return true
}

func (rl *RateLimiter) cleanup() {
	for {
		select {
		case <-rl.cleanupTick.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-5 * time.Minute)
			for key, requests := range rl.requests {
				hasRecent := false
				for _, reqTime := range requests {
					if reqTime.After(cutoff) {
						hasRecent = true
						break
					}
				}
				if !hasRecent {
					delete(rl.requests, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCleanup:
			return
		}
	}
}

func (rl *RateLimiter) Stop() {
	rl.cleanupTick.Stop()
	close(rl.stopCleanup)
}

