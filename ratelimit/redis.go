package ratelimit

import (
	"context"
	"fmt"
	"log"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// slidingWindowScript is an atomic Lua script that implements a sliding window
// counter using a sorted set. Members are scored by their arrival timestamp (ns).
// Returns 1 if the request is allowed, 0 if the limit is exceeded.
var slidingWindowScript = goredis.NewScript(`
local key    = KEYS[1]
local now    = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit  = tonumber(ARGV[3])

redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)
local count = redis.call('ZCARD', key)
if count < limit then
    redis.call('ZADD', key, now, now)
    redis.call('PEXPIRE', key, math.ceil(window / 1000000))
    return 1
end
return 0
`)

type redisStore struct {
	client     *goredis.Client
	fallback   *memoryStore
	ratePerSec int
	windowNs   int64
}

func newRedisStore(requestsPerSecond int, redisURL string) (*redisStore, error) {
	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL: %w", err)
	}

	client := goredis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("ratelimit: redis unavailable at startup: %v", err)
	}

	return &redisStore{
		client:     client,
		fallback:   newMemoryStore(requestsPerSecond),
		ratePerSec: requestsPerSecond,
		windowNs:   int64(time.Second),
	}, nil
}

func (b *redisStore) allow(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	now := time.Now().UnixNano()

	result, err := slidingWindowScript.Run(ctx, b.client,
		[]string{"rl:" + key},
		now, b.windowNs, b.ratePerSec,
	).Int()
	if err != nil {
		log.Printf("ratelimit: redis error (falling back to in-memory): %v", err)
		return b.fallback.allow(key)
	}
	return result == 1
}

func (b *redisStore) stop() {
	b.client.Close()
	b.fallback.stop()
}
