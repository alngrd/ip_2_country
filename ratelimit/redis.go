package ratelimit

import (
	"context"
	"fmt"
	"log"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// checkScript runs all rate-limit tiers atomically.
//
// KEYS[1]  rate_limit:freq:{IP}           – tier 1 hash (last_ts, suspicion)
// KEYS[2]  rate_limit:ip:{IP}             – tier 3 sorted-set (global per-IP cap)
// KEYS[3]  rate_limit:port:{IP}:{port}     – tier 2 sorted-set (per TCP connection)
// KEYS[4]  rate_limit:404:{IP}            – 404 counter (scrape blacklist)
//
// ARGV[1]  now_ns                    current Unix time in nanoseconds
// ARGV[2]  burst_suspicion_threshold tier-1 block threshold
// ARGV[3]  ip_limit                  tier-3 request cap
// ARGV[4]  ip_window_ns              tier-3 window in nanoseconds
// ARGV[5]  port_limit                tier-2 request cap
// ARGV[6]  port_window_ns            tier-2 window in nanoseconds
// ARGV[7]  not_found_limit           404 block threshold
//
// Returns 0 (allowed), 1 (tier 1), 2 (tier 2), 3 (tier 3), 4 (404 blacklist).
//
// Evaluation order: tier 3 → 404 blacklist → tier 1 → tier 2.
// Tier 3 is first so every request counts toward the global IP ceiling before
// any other tier can drop it.
var checkScript = goredis.NewScript(`
local now_ns                    = tonumber(ARGV[1])
local burst_suspicion_threshold = tonumber(ARGV[2])
local ip_limit                  = tonumber(ARGV[3])
local ip_window_ns              = tonumber(ARGV[4])
local port_limit                = tonumber(ARGV[5])
local port_window_ns            = tonumber(ARGV[6])
local not_found_limit           = tonumber(ARGV[7])

-- Tier 3: Blast Shield — global per-IP ceiling.
-- First so every request is counted regardless of what other tiers decide.
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now_ns - ip_window_ns)
if redis.call('ZCARD', KEYS[2]) >= ip_limit then
    return 3
end
redis.call('ZADD', KEYS[2], now_ns, now_ns)
redis.call('PEXPIRE', KEYS[2], math.floor(ip_window_ns / 1000000))

-- 404 blacklist: block IPs that have accumulated too many not-found responses.
local not_found_count = tonumber(redis.call('GET', KEYS[4])) or 0
if not_found_count >= not_found_limit then
    return 4
end

-- Tier 1: Velocity Detection.
-- Suspicion increments on sub-50ms intervals and decays by 1 on healthy
-- intervals, so clients that burst briefly and then slow down can recover.
local data      = redis.call('HMGET', KEYS[1], 'last_ts', 'suspicion')
local last_ts   = tonumber(data[1]) or 0
local suspicion = tonumber(data[2]) or 0

if last_ts > 0 and (now_ns - last_ts) < 50000000 then
    suspicion = suspicion + 1
elseif suspicion > 0 then
    suspicion = suspicion - 1
end
redis.call('HSET', KEYS[1], 'last_ts', now_ns, 'suspicion', suspicion)
redis.call('EXPIRE', KEYS[1], 60)
if suspicion > burst_suspicion_threshold then
    return 1
end

-- Tier 2: Per-connection cap (IP + source port).
-- Skipped when the port is unavailable.
if KEYS[3] ~= '' then
    redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', now_ns - port_window_ns)
    if redis.call('ZCARD', KEYS[3]) >= port_limit then
        return 2
    end
    redis.call('ZADD', KEYS[3], now_ns, now_ns)
    redis.call('PEXPIRE', KEYS[3], math.floor(port_window_ns / 1000000))
end

return 0
`)

// record404Script atomically increments the 404 counter and refreshes its TTL.
// The TTL is refreshed on every 404 so persistent scrapers stay blacklisted
// as long as they keep hitting dead endpoints.
var record404Script = goredis.NewScript(`
redis.call('INCR', KEYS[1])
redis.call('EXPIRE', KEYS[1], ARGV[1])
`)

type redisStore struct {
	client                  *goredis.Client
	fallback                *memoryStore
	burstSuspicionThreshold int
	portLimit    int
	portWindowNs int64
	ipLimit                 int
	ipWindowNs              int64
	notFoundLimit           int
	notFoundWindowSec       int
}

func newRedisStore(opts Options) (*redisStore, error) {
	parsedOpts, err := goredis.ParseURL(opts.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL: %w", err)
	}

	client := goredis.NewClient(parsedOpts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("ratelimit: redis unavailable at startup: %v", err)
	}

	return &redisStore{
		client:                  client,
		fallback:                newMemoryStore(opts.MemoryFallbackRPS),
		burstSuspicionThreshold: opts.BurstSuspicionThreshold,
		portLimit:    opts.PortLimit,
		portWindowNs: opts.PortWindow.Nanoseconds(),
		ipLimit:                 opts.IPLimit,
		ipWindowNs:              opts.IPWindow.Nanoseconds(),
		notFoundLimit:           opts.NotFoundLimit,
		notFoundWindowSec:       int(opts.NotFoundWindow.Seconds()),
	}, nil
}

func (b *redisStore) allow(ip, port string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	now := time.Now().UnixNano()

	result, err := checkScript.Run(ctx, b.client,
		[]string{
			"rate_limit:freq:" + ip,
			"rate_limit:ip:" + ip,
			portKey(ip, port),
			"rate_limit:404:" + ip,
		},
		now, b.burstSuspicionThreshold, b.ipLimit, b.ipWindowNs,
		b.portLimit, b.portWindowNs, b.notFoundLimit,
	).Int()
	if err != nil {
		log.Printf("ratelimit: redis error (falling back to in-memory): %v", err)
		return b.fallback.allow(ip, port)
	}
	return result
}

func (b *redisStore) record404(ip string) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := record404Script.Run(ctx, b.client,
		[]string{"rate_limit:404:" + ip},
		b.notFoundWindowSec,
	).Err(); err != nil {
		log.Printf("ratelimit: failed to record 404 for %s: %v", ip, err)
	}
}

func (b *redisStore) stop() {
	b.client.Close()
	b.fallback.stop()
}

// portKey returns the Tier 2 Redis key keyed on IP + source port, or an empty
// string when the port is unavailable so the Lua script skips Tier 2 entirely.
func portKey(ip, port string) string {
	if port == "" {
		return ""
	}
	return "rate_limit:port:" + ip + ":" + port
}

