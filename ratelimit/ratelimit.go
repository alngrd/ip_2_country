package ratelimit

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ipState struct {
	mu       sync.Mutex
	requests []time.Time
}

type RateLimiter struct {
	states      sync.Map
	ratePerSec  int
	windowSize  time.Duration
	cleanupTick *time.Ticker
	stopCleanup chan bool
}

func NewRateLimiter(requestsPerSecond int) *RateLimiter {
	rl := &RateLimiter{
		ratePerSec:  requestsPerSecond,
		windowSize:  time.Second,
		stopCleanup: make(chan bool),
	}

	rl.cleanupTick = time.NewTicker(1 * time.Minute)
	go rl.cleanup()

	return rl
}

func (rl *RateLimiter) Allow(key string) bool {
	v, _ := rl.states.LoadOrStore(key, &ipState{})
	state := v.(*ipState)

	state.mu.Lock()
	defer state.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.windowSize)

	filtered := make([]time.Time, 0)
	for _, reqTime := range state.requests {
		if reqTime.After(cutoff) {
			filtered = append(filtered, reqTime)
		}
	}

	if len(filtered) >= rl.ratePerSec {
		state.requests = filtered
		return false
	}

	state.requests = append(filtered, now)
	return true
}

func (rl *RateLimiter) cleanup() {
	for {
		select {
		case <-rl.cleanupTick.C:
			cutoff := time.Now().Add(-5 * time.Minute)
			rl.states.Range(func(key, value any) bool {
				state := value.(*ipState)
				state.mu.Lock()
				hasRecent := false
				for _, reqTime := range state.requests {
					if reqTime.After(cutoff) {
						hasRecent = true
						break
					}
				}
				state.mu.Unlock()
				if !hasRecent {
					rl.states.Delete(key)
				}
				return true
			})
		case <-rl.stopCleanup:
			return
		}
	}
}

func Middleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.Allow(getClientIP(r)) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i != -1 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func (rl *RateLimiter) Stop() {
	rl.cleanupTick.Stop()
	close(rl.stopCleanup)
}
