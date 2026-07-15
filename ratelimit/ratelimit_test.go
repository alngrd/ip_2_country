package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func makeMiddlewareReq(rl *RateLimiter, remoteAddr, xff string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rr := httptest.NewRecorder()
	Middleware(rl)(okHandler).ServeHTTP(rr, req)
	return rr
}

func TestMiddleware_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(1)
	defer rl.Stop()

	rr := makeMiddlewareReq(rl, "1.2.3.4:1000", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestMiddleware_BlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(1)
	defer rl.Stop()

	makeMiddlewareReq(rl, "5.5.5.5:1234", "")
	rr := makeMiddlewareReq(rl, "5.5.5.5:1234", "")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
}

func TestMiddleware_RemoteAddrWithoutPort(t *testing.T) {
	rl := NewRateLimiter(1)
	defer rl.Stop()

	if rr := makeMiddlewareReq(rl, "10.0.0.1", ""); rr.Code != http.StatusOK {
		t.Fatalf("first request should succeed, got %d", rr.Code)
	}
	if rr := makeMiddlewareReq(rl, "10.0.0.1", ""); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("second request should be rate-limited, got %d", rr.Code)
	}
}

func TestMiddleware_XForwardedFor_SeparateBucketsPerClientIP(t *testing.T) {
	rl := NewRateLimiter(1)
	defer rl.Stop()

	if rr := makeMiddlewareReq(rl, "10.0.0.1:1234", "5.5.5.5"); rr.Code != http.StatusOK {
		t.Fatalf("client A first request: expected 200, got %d", rr.Code)
	}
	if rr := makeMiddlewareReq(rl, "10.0.0.1:1234", "6.6.6.6"); rr.Code != http.StatusOK {
		t.Fatalf("client B first request: expected 200 (separate bucket), got %d", rr.Code)
	}
	if rr := makeMiddlewareReq(rl, "10.0.0.1:1234", "5.5.5.5"); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("client A second request: expected 429, got %d", rr.Code)
	}
}

func TestMiddleware_XForwardedFor_MultipleProxies_UsesClientIP(t *testing.T) {
	rl := NewRateLimiter(1)
	defer rl.Stop()

	makeMiddlewareReq(rl, "10.0.0.1:1234", "5.5.5.5, 10.0.0.2, 10.0.0.3")
	rr := makeMiddlewareReq(rl, "10.0.0.1:1234", "5.5.5.5, 10.0.0.2, 10.0.0.3")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 (leftmost IP bucketed), got %d", rr.Code)
	}
}

func TestAllow_UnderLimit(t *testing.T) {
	rl := NewRateLimiter(5)
	defer rl.Stop()

	for i := 0; i < 5; i++ {
		if !rl.Allow("1.1.1.1") {
			t.Fatalf("request %d should be allowed (limit=5)", i+1)
		}
	}
}

func TestAllow_ExceedsLimit(t *testing.T) {
	rl := NewRateLimiter(3)
	defer rl.Stop()

	for i := 0; i < 3; i++ {
		rl.Allow("2.2.2.2")
	}
	if rl.Allow("2.2.2.2") {
		t.Error("4th request within window should be denied (limit=3)")
	}
}

func TestAllow_DifferentIPsAreIndependent(t *testing.T) {
	rl := NewRateLimiter(1)
	defer rl.Stop()

	if !rl.Allow("10.0.0.1") {
		t.Fatal("first request for 10.0.0.1 should be allowed")
	}
	if !rl.Allow("10.0.0.2") {
		t.Fatal("first request for 10.0.0.2 should be allowed (different IP)")
	}
	if rl.Allow("10.0.0.1") {
		t.Error("second request for 10.0.0.1 should be denied")
	}
}

func TestAllow_WindowReset(t *testing.T) {
	ms := &memoryStore{
		ratePerSec:  1,
		windowSize:  50 * time.Millisecond,
		stopCleanup: make(chan bool),
	}
	ms.cleanupTick = time.NewTicker(1 * time.Minute)
	go ms.cleanup()
	rl := &RateLimiter{b: ms}
	defer rl.Stop()

	if !rl.Allow("3.3.3.3") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("3.3.3.3") {
		t.Fatal("second request within window should be denied")
	}

	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("3.3.3.3") {
		t.Error("first request after window expiry should be allowed")
	}
}

func TestAllow_Concurrent(t *testing.T) {
	const limit = 10
	rl := NewRateLimiter(limit)
	defer rl.Stop()

	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rl.Allow("4.4.4.4") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if int(allowed.Load()) != limit {
		t.Errorf("expected exactly %d allowed, got %d", limit, allowed.Load())
	}
}

func TestAllow_ZeroRateNeverAllows(t *testing.T) {
	rl := NewRateLimiter(0)
	defer rl.Stop()

	// ratePerSec=0: len(filtered) >= 0 is always true, so every request denied
	if rl.Allow("5.5.5.5") {
		t.Error("with limit=0, all requests should be denied")
	}
}

func TestStop_IdempotentCleanup(t *testing.T) {
	rl := NewRateLimiter(10)
	rl.Stop()
	rl.Stop()
}

func TestNewRateLimiterWithRedis_InvalidURL_FallsBackToMemory(t *testing.T) {
	rl := NewRateLimiterWithRedis(2, "redis://invalid-host-that-does-not-exist:6379")
	defer rl.Stop()

	if !rl.Allow("1.1.1.1") {
		t.Fatal("first request should be allowed")
	}
	if !rl.Allow("1.1.1.1") {
		t.Fatal("second request should be allowed (limit=2)")
	}
	if rl.Allow("1.1.1.1") {
		t.Error("third request should be denied (limit=2)")
	}
}

func TestNewRateLimiterWithRedis_EmptyURL_UsesMemory(t *testing.T) {
	rl := NewRateLimiterWithRedis(1, "")
	defer rl.Stop()

	if !rl.Allow("2.2.2.2") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("2.2.2.2") {
		t.Error("second request should be denied")
	}
}

func TestMemoryStore_CleanupStartsLazily(t *testing.T) {
	ms := newMemoryStore(10)

	if ms.cleanupTick != nil {
		t.Fatal("cleanupTick should be nil before first allow call")
	}

	ms.allow("1.1.1.1")

	if ms.cleanupTick == nil {
		t.Error("cleanupTick should be set after first allow call")
	}

	ms.stop()
}

func TestCleanup_RemovesStaleEntries(t *testing.T) {
	// Build a memoryStore with a very short cleanup tick so we don't wait a minute.
	ms := &memoryStore{
		ratePerSec:  10,
		windowSize:  time.Second,
		stopCleanup: make(chan bool),
	}
	ms.cleanupTick = time.NewTicker(20 * time.Millisecond)
	go ms.cleanup()
	rl := &RateLimiter{b: ms}
	defer rl.Stop()

	// Inject a stale entry directly — last request was 10 minutes ago.
	stale := &ipState{requests: []time.Time{time.Now().Add(-10 * time.Minute)}}
	ms.states.Store("stale.ip", stale)

	// Add a fresh entry that must survive cleanup.
	rl.Allow("fresh.ip")

	// Wait long enough for at least one cleanup tick to fire.
	time.Sleep(60 * time.Millisecond)

	if _, exists := ms.states.Load("stale.ip"); exists {
		t.Error("stale entry should have been removed by cleanup")
	}
	if _, exists := ms.states.Load("fresh.ip"); !exists {
		t.Error("fresh entry should not have been removed by cleanup")
	}
}
