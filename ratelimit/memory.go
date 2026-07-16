package ratelimit

import (
	"sync"
	"time"
)

type ipState struct {
	mu       sync.Mutex
	requests []time.Time
}

type memoryStore struct {
	states      sync.Map
	ratePerSec  int
	windowSize  time.Duration
	cleanupTick *time.Ticker
	stopCleanup chan bool
	startOnce   sync.Once
	stopOnce    sync.Once
}

func newMemoryStore(requestsPerSecond int) *memoryStore {
	return &memoryStore{
		ratePerSec:  requestsPerSecond,
		windowSize:  time.Second,
		stopCleanup: make(chan bool),
	}
}

// allow applies a basic sliding-window check keyed on IP only.
// Returns 0 (allowed) or 3 (blast-shield equivalent blocked).
func (b *memoryStore) allow(ip, port string) int {
	b.startOnce.Do(func() {
		if b.cleanupTick == nil {
			b.cleanupTick = time.NewTicker(1 * time.Minute)
			go b.cleanup()
		}
	})

	key := ip + ":" + port
	v, _ := b.states.LoadOrStore(key, &ipState{})
	state := v.(*ipState)

	state.mu.Lock()
	defer state.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-b.windowSize)

	filtered := make([]time.Time, 0)
	for _, reqTime := range state.requests {
		if reqTime.After(cutoff) {
			filtered = append(filtered, reqTime)
		}
	}

	if len(filtered) >= b.ratePerSec {
		state.requests = filtered
		return 3
	}

	state.requests = append(filtered, now)
	return 0
}

func (b *memoryStore) cleanup() {
	for {
		select {
		case <-b.cleanupTick.C:
			cutoff := time.Now().Add(-5 * time.Minute)
			b.states.Range(func(key, value any) bool {
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
					b.states.Delete(key)
				}
				return true
			})
		case <-b.stopCleanup:
			return
		}
	}
}

func (b *memoryStore) record404(_ string) {}

func (b *memoryStore) stop() {
	b.stopOnce.Do(func() {
		b.startOnce.Do(func() {}) // prevent cleanup from starting after stop
		if b.cleanupTick != nil {
			b.cleanupTick.Stop()
		}
		close(b.stopCleanup)
	})
}
