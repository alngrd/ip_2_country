package ratelimit

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
)

type store interface {
	allow(key string) bool
	stop()
}

type RateLimiter struct {
	b store
}

func NewRateLimiter(requestsPerSecond int) *RateLimiter {
	return &RateLimiter{b: newMemoryStore(requestsPerSecond)}
}

// NewRateLimiterWithRedis returns a Redis-backed RateLimiter when redisURL is
// non-empty and valid, falling back to in-memory otherwise.
func NewRateLimiterWithRedis(requestsPerSecond int, redisURL string) *RateLimiter {
	if redisURL != "" {
		b, err := newRedisStore(requestsPerSecond, redisURL)
		if err != nil {
			log.Printf("ratelimit: invalid Redis URL (%v), falling back to in-memory", err)
		} else {
			return &RateLimiter{b: b}
		}
	}
	return NewRateLimiter(requestsPerSecond)
}

func (rl *RateLimiter) Allow(key string) bool {
	return rl.b.allow(key)
}

func (rl *RateLimiter) Stop() {
	rl.b.stop()
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
