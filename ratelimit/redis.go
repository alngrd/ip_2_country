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
// KEYS[1]  rate_limit:tb:{IP}              – tier 1 token bucket hash (tokens, last_refill)
// KEYS[2]  rate_limit:ip:{IP}              – tier 3 sorted-set (global per-IP cap)
// KEYS[3]  rate_limit:port:{IP}:{port}     – tier 2 sorted-set (per TCP connection)
// KEYS[4]  rate_limit:404:block:{IP}       – 404 backoff block key (exists when blocked)
//
// ARGV[1]  now_ns              current Unix time in nanoseconds
// ARGV[2]  burst_capacity      tier-1 token bucket capacity (max burst size)
// ARGV[3]  burst_refill_rate   tier-1 tokens refilled per second
// ARGV[4]  ip_limit            tier-3 request cap
// ARGV[5]  ip_window_ns        tier-3 window in nanoseconds
// ARGV[6]  port_limit          tier-2 request cap
// ARGV[7]  port_window_ns      tier-2 window in nanoseconds
//
// Returns 0 (allowed), 1 (tier 1), 2 (tier 2), 3 (tier 3), 4 (404 blacklist).
//
// Evaluation order: tier 3 → 404 block → tier 1 → tier 2.
var checkScript = goredis.NewScript(`
local now_ns           = tonumber(ARGV[1])
local burst_capacity   = tonumber(ARGV[2])
local burst_refill_rate = tonumber(ARGV[3])
local ip_limit         = tonumber(ARGV[4])
local ip_window_ns     = tonumber(ARGV[5])
local port_limit       = tonumber(ARGV[6])
local port_window_ns   = tonumber(ARGV[7])

local now_sec = now_ns / 1e9

-- Tier 3: Blast Shield — global per-IP ceiling.
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now_ns - ip_window_ns)
if redis.call('ZCARD', KEYS[2]) >= ip_limit then
    return 3
end
redis.call('ZADD', KEYS[2], now_ns, now_ns)
redis.call('PEXPIRE', KEYS[2], math.floor(ip_window_ns / 1000000))

-- 404 blacklist: block IPs currently in an exponential-backoff block period.
if redis.call('EXISTS', KEYS[4]) == 1 then
    return 4
end

-- Tier 1: Token Bucket — per-IP burst control.
local data        = redis.call('HMGET', KEYS[1], 'tokens', 'last_refill')
local tokens      = tonumber(data[1])
local last_refill = tonumber(data[2])

if tokens == nil then
    tokens      = burst_capacity
    last_refill = now_sec
end

local elapsed = math.max(0, now_sec - last_refill)
tokens = math.min(burst_capacity, tokens + elapsed * burst_refill_rate)

if tokens < 1 then
    redis.call('HMSET', KEYS[1], 'tokens', tokens, 'last_refill', now_sec)
    redis.call('EXPIRE', KEYS[1], 3600)
    return 1
end

redis.call('HMSET', KEYS[1], 'tokens', tokens - 1, 'last_refill', now_sec)
redis.call('EXPIRE', KEYS[1], 3600)

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

// record404Script tracks 404s in a sliding window. When the count crosses the
// threshold it increments a violation counter and sets an exponential-backoff
// block key (duration = base * 2^(violations-1), capped at max), then clears
// the log so the next window starts fresh.
//
// KEYS[1]  rate_limit:404:log:{IP}        – sorted-set of 404 timestamps
// KEYS[2]  rate_limit:404:violations:{IP} – cumulative violation count
// KEYS[3]  rate_limit:404:block:{IP}      – block key (exists while blocked)
//
// ARGV[1]  now_ns           current Unix time in nanoseconds
// ARGV[2]  window_ns        sliding window size in nanoseconds
// ARGV[3]  threshold        404 count that triggers a block
// ARGV[4]  base_block_sec   block duration for the first violation (seconds)
// ARGV[5]  max_block_sec    maximum block duration (seconds)
var record404Script = goredis.NewScript(`
local now_ns         = tonumber(ARGV[1])
local window_ns      = tonumber(ARGV[2])
local threshold      = tonumber(ARGV[3])
local base_block_sec = tonumber(ARGV[4])
local max_block_sec  = tonumber(ARGV[5])

redis.call('ZADD', KEYS[1], now_ns, now_ns)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now_ns - window_ns)
redis.call('PEXPIRE', KEYS[1], math.floor(window_ns / 1000000))

local count = redis.call('ZCARD', KEYS[1])
if count >= threshold then
    local violations = tonumber(redis.call('INCR', KEYS[2]))
    redis.call('EXPIRE', KEYS[2], 86400)
    local block_sec = math.min(base_block_sec * (2 ^ (violations - 1)), max_block_sec)
    redis.call('SET', KEYS[3], 1, 'EX', math.floor(block_sec))
    redis.call('DEL', KEYS[1])
end
return 0
`)

type redisStore struct {
	client   *goredis.Client
	fallback *memoryStore

	burstCapacity   float64
	burstRefillRate float64

	portLimit    int
	portWindowNs int64

	ipLimit    int
	ipWindowNs int64

	notFoundLimit        int
	notFoundWindowNs     int64
	notFoundBaseBlockSec int
	notFoundMaxBlockSec  int
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
		client:   client,
		fallback: newMemoryStore(opts.MemoryFallbackRPS),

		burstCapacity:   float64(opts.BurstCapacity),
		burstRefillRate: opts.BurstRefillRatePerSec,

		portLimit:    opts.PortLimit,
		portWindowNs: opts.PortWindow.Nanoseconds(),

		ipLimit:    opts.IPLimit,
		ipWindowNs: opts.IPWindow.Nanoseconds(),

		notFoundLimit:        opts.NotFoundLimit,
		notFoundWindowNs:     opts.NotFoundWindow.Nanoseconds(),
		notFoundBaseBlockSec: int(opts.NotFoundBaseBlockDuration.Seconds()),
		notFoundMaxBlockSec:  int(opts.NotFoundMaxBlockDuration.Seconds()),
	}, nil
}

func (b *redisStore) allow(ip, port string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	now := time.Now().UnixNano()

	result, err := checkScript.Run(ctx, b.client,
		[]string{
			"rate_limit:tb:" + ip,
			"rate_limit:ip:" + ip,
			portKey(ip, port),
			"rate_limit:404:block:" + ip,
		},
		now, b.burstCapacity, b.burstRefillRate,
		b.ipLimit, b.ipWindowNs,
		b.portLimit, b.portWindowNs,
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

	now := time.Now().UnixNano()

	if err := record404Script.Run(ctx, b.client,
		[]string{
			"rate_limit:404:log:" + ip,
			"rate_limit:404:violations:" + ip,
			"rate_limit:404:block:" + ip,
		},
		now, b.notFoundWindowNs, b.notFoundLimit,
		b.notFoundBaseBlockSec, b.notFoundMaxBlockSec,
	).Err(); err != nil && err != goredis.Nil {
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

