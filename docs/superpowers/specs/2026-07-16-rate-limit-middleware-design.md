# Rate Limit Middleware Design

**Date:** 2026-07-16  
**Status:** Approved

## Goal

Move rate limiting out of the route handler into a global HTTP middleware so that every current and future endpoint is automatically rate limited without per-route wiring.

## Current State

- `Handler` struct holds `*ratelimit.RateLimiter`
- `FindCountry` calls `h.rateLimiter.Allow(clientIP)` inline before business logic
- `getClientIP` lives in `handlers/handlers.go`
- `NewHandler` accepts both `db` and `rateLimiter`

## Design

### `ratelimit/ratelimit.go`

Add a `Middleware` method on `RateLimiter`:

```go
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
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
```

Move `getClientIP` from `handlers/handlers.go` into this file — the rate limiter is the only consumer.

The underlying sliding-window-per-IP algorithm in `Allow` is unchanged.

### `handlers/handlers.go`

- Remove `rateLimiter *ratelimit.RateLimiter` field from `Handler`
- Remove `ratelimit` import
- Remove the `Allow` check and early return from `FindCountry`
- `NewHandler(db database.Database) *Handler` — takes only `db`

### `server/server.go`

- `SetupServer` gains a `rl *ratelimit.RateLimiter` parameter
- Wrap the entire mux: `Handler: rl.Middleware(mux)`
- Routes register normally with no middleware boilerplate

```go
func SetupServer(cfg *config.Config, handler *handlers.Handler, rl *ratelimit.RateLimiter) *http.Server {
    mux := http.NewServeMux()
    mux.HandleFunc("/v1/find-country", handler.FindCountry)

    return &http.Server{
        Addr:    ":" + cfg.Port,
        Handler: rl.Middleware(mux),
        ...
    }
}
```

### `main.go`

Pass `rl` to `SetupServer`:

```go
srv := server.SetupServer(cfg, handler, rl)
```

`handler` is constructed without the rate limiter:

```go
handler := handlers.NewHandler(db)
```

## Tests

- `TestFindCountry_RateLimited` and `TestFindCountry_RemoteAddrWithoutPort` move from `handlers/handlers_test.go` to `ratelimit/ratelimit_test.go` and test the middleware via `httptest`
- Handler tests no longer construct a `RateLimiter` — `newHandler` takes only `db` and `rps` is dropped
- `server/server_test.go` updated to pass a `RateLimiter` to `SetupServer`

## Behaviour

- Same sliding-window-per-IP algorithm, same 429 JSON response shape
- Every route on the mux is automatically rate limited — no per-route wiring required
- Easy to remove: delete `rl.Middleware(mux)` wrapper and the `ratelimit` package when a gateway takes over
