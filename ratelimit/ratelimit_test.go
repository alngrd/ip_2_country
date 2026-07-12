package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
	rl := &RateLimiter{
		ratePerSec:  1,
		windowSize:  50 * time.Millisecond,
		stopCleanup: make(chan bool),
	}
	rl.cleanupTick = time.NewTicker(1 * time.Minute)
	go rl.cleanup()
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
	// Second Stop should not panic (stopCleanup channel is closed)
}

func TestCleanup_RemovesStaleEntries(t *testing.T) {
	// Build a RateLimiter with a very short cleanup tick so we don't wait a minute.
	rl := &RateLimiter{
		ratePerSec:  10,
		windowSize:  time.Second,
		stopCleanup: make(chan bool),
	}
	rl.cleanupTick = time.NewTicker(20 * time.Millisecond)
	go rl.cleanup()
	defer rl.Stop()

	// Inject a stale entry directly — last request was 10 minutes ago.
	stale := &ipState{requests: []time.Time{time.Now().Add(-10 * time.Minute)}}
	rl.states.Store("stale.ip", stale)

	// Add a fresh entry that must survive cleanup.
	rl.Allow("fresh.ip")

	// Wait long enough for at least one cleanup tick to fire.
	time.Sleep(60 * time.Millisecond)

	if _, exists := rl.states.Load("stale.ip"); exists {
		t.Error("stale entry should have been removed by cleanup")
	}
	if _, exists := rl.states.Load("fresh.ip"); !exists {
		t.Error("fresh entry should not have been removed by cleanup")
	}
}
