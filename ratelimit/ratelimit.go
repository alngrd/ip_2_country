package ratelimit

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// Options configures the RateLimiter. All values are required when using the
// Redis-backed store; zero values disable the corresponding check.
type Options struct {
	RedisURL          string
	MemoryFallbackRPS int

	BurstCapacity         int
	BurstRefillRatePerSec float64

	PortLimit  int
	PortWindow time.Duration

	IPLimit  int
	IPWindow time.Duration

	NotFoundLimit             int
	NotFoundWindow            time.Duration
	NotFoundBaseBlockDuration time.Duration
	NotFoundMaxBlockDuration  time.Duration
}

type store interface {
	allow(ip, port string) int
	record404(ip string)
	stop()
}

type RateLimiter struct {
	b store
}

// NewRateLimiter returns a memory-backed RateLimiter. Intended for tests.
func NewRateLimiter(requestsPerSecond int) *RateLimiter {
	return &RateLimiter{b: newMemoryStore(requestsPerSecond)}
}

// NewRateLimiterWithRedis returns a Redis-backed RateLimiter using opts,
// falling back to in-memory when RedisURL is empty or unreachable.
func NewRateLimiterWithRedis(opts Options) *RateLimiter {
	if opts.RedisURL != "" {
		b, err := newRedisStore(opts)
		if err != nil {
			log.Printf("ratelimit: invalid Redis URL (%v), falling back to in-memory", err)
		} else {
			return &RateLimiter{b: b}
		}
	}
	return NewRateLimiter(opts.MemoryFallbackRPS)
}

func (rl *RateLimiter) Stop() {
	rl.b.stop()
}

func Middleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, port := clientIPAndPort(r)

			if rl.b.allow(ip, port) != 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
				return
			}

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			if rec.status == http.StatusNotFound {
				rl.b.record404(ip)
			}

		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture the response status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// clientIPAndPort parses the request once to return the client IP and the
// source port of the TCP connection. IP is taken from X-Forwarded-For when
// present (real client behind a load balancer); port always comes from
// RemoteAddr (the LB's upstream port, which maps one-to-one with client
// connections under HTTP/2).
func clientIPAndPort(r *http.Request) (ip, port string) {
	remoteIP, remotePort, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}
	port = remotePort

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i != -1 {
			return strings.TrimSpace(xff[:i]), port
		}
		return strings.TrimSpace(xff), port
	}
	return remoteIP, port
}
