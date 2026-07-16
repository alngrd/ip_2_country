# IP to Country Service

A lightweight Go-based web service that maps IP addresses to country and city, with a multi-tier Redis-backed rate limiter.

## Features

- IPv4 and IPv6 address to country and city lookup
- CIDR range and exact IP matching in the database
- Multi-tier rate limiting per client IP (token bucket, per-connection cap, global IP ceiling, 404 abuse detection)
- Redis-backed rate limiting with in-memory fallback for single-instance or Redis-unavailable scenarios
- Load-balancer aware: reads real client IP from `X-Forwarded-For`
- CSV-based database for IP location data
- Graceful shutdown on SIGINT/SIGTERM
- Docker support
- Configurable via environment variables

## Getting Started

### Prerequisites

- Go 1.21 or higher
- IP location data CSV file
- Redis (optional but required for multi-instance deployments and full rate-limit behavior)

### Installation

1. Clone the repository:

```bash
git clone https://github.com/alngrd/ip_2_country.git
cd ip_2_country
```

2. Install dependencies:

```bash
go mod download
```

3. Copy `.env.example` to `.env` and fill in the required values:

```bash
cp .env.example .env
```

### Running the Service

```bash
go run main.go
```

The service starts on the configured port (default: 8080).

### Running Tests

**Unit tests:**

```bash
go test ./...
```

**End-to-end tests:**

E2e tests are gated behind the `e2e` build tag and require the CSV database file at `data/ip2country.csv`. They spin up a real HTTP server internally — no separately running service is needed.

```bash
go test -tags e2e -v ./test/e2e/
```

The e2e suite covers:
- IPv4 and IPv6 exact and CIDR lookups
- Error responses (invalid IP, missing param, unknown path, wrong HTTP method)
- Rate limiting behavior per client IP
- Response shape and `Content-Type` header
- Concurrent request handling (50 goroutines)

## API

### Endpoint

`GET /v1/find-country?ip=<ip_address>`

**Example:**

```bash
curl http://localhost:8080/v1/find-country?ip=8.8.8.8
```

**Success Response (200):**

```json
{
  "country": "United States",
  "city": "Mountain View"
}
```

**Error Response format:**

```json
{
  "error": "<message>"
}
```

**Error Status Codes:**

| Status | Meaning |
|--------|---------|
| `400 Bad Request` | Missing or invalid `ip` query parameter |
| `404 Not Found` | IP address not found in the database |
| `405 Method Not Allowed` | Non-GET request |
| `429 Too Many Requests` | Rate limit exceeded |
| `500 Internal Server Error` | Internal server error |

## Rate Limiting

Rate limiting is enforced per client IP. When `REDIS_URL` is set, all tiers run atomically in a single Lua script against Redis, making limits consistent across multiple instances. Without Redis, the service falls back to a per-instance in-memory sliding window (`RATE_LIMIT_RPS`).

The client IP is taken from the leftmost entry in `X-Forwarded-For` when present, falling back to `RemoteAddr`. Ensure your load balancer sets this header.

### Tiers (evaluated in order)

**Tier 3 — Blast Shield** *(evaluated first)*

A global per-IP sliding window cap. Evaluated before all other tiers so every request counts toward the ceiling regardless of what Tiers 1 or 2 decide.

| Config | Default | Description |
|--------|---------|-------------|
| `RATE_LIMIT_IP_LIMIT` | `2000` | Max requests allowed in the window |
| `RATE_LIMIT_IP_WINDOW` | `1m` | Sliding window duration |

---

**404 Blacklist** *(evaluated second)*

Tracks 404 responses in a sliding window. When the count crosses the threshold, the IP is blocked for an exponentially increasing duration. Each subsequent violation doubles the block time, capped at the configured maximum. The violation counter resets after 24 hours of clean behavior.

| Config | Default | Description |
|--------|---------|-------------|
| `RATE_LIMIT_NOT_FOUND_LIMIT` | `20` | 404s within the window before blocking |
| `RATE_LIMIT_NOT_FOUND_WINDOW` | `10m` | Sliding window for counting 404s |
| `RATE_LIMIT_NOT_FOUND_BASE_BLOCK_DURATION` | `1m` | Block duration for the first violation |
| `RATE_LIMIT_NOT_FOUND_MAX_BLOCK_DURATION` | `1h` | Maximum block duration |

Example progression (defaults): 1st violation → 1 min block, 2nd → 2 min, 3rd → 4 min, … capped at 1 hour.

---

**Tier 1 — Token Bucket** *(evaluated third)*

A token bucket per client IP. The bucket starts full and refills at a fixed rate. Requests consume one token; when the bucket is empty the request is rejected. This allows legitimate bursts up to the bucket capacity while enforcing a sustained rate.

| Config | Default | Description |
|--------|---------|-------------|
| `RATE_LIMIT_BURST_CAPACITY` | `20` | Bucket capacity (maximum burst size) |
| `RATE_LIMIT_BURST_REFILL_RATE_PER_SEC` | `10` | Tokens refilled per second (sustained rate) |

---

**Tier 2 — Per-Connection Cap** *(evaluated last)*

A sliding window cap keyed on IP + source port, limiting requests per TCP connection. Skipped when the source port is unavailable.

| Config | Default | Description |
|--------|---------|-------------|
| `RATE_LIMIT_PER_PORT_LIMIT` | `300` | Max requests per connection in the window |
| `RATE_LIMIT_PER_PORT_WINDOW` | `1m` | Sliding window duration |

## Load Balancer

The service is designed to run behind a load balancer:

- **Client IP**: read from `X-Forwarded-For` (leftmost entry). Your LB must set this header — all major LBs (nginx, HAProxy, AWS ALB) do so by default.
- **Shared rate-limit state**: configure `REDIS_URL` so all instances share the same counters. Without it, each instance enforces limits independently.
- **Tier 2 (per-connection cap)**: uses the source port from `RemoteAddr`, which behind a LB is the LB's upstream port rather than the original client port. To preserve the original client port, configure your LB to use PROXY Protocol and update the server to parse it.

## Docker

Build and run with Docker:

```bash
docker build -t ip2country .
docker run -p 8080:8080 ip2country
```

## Configuration

All settings are read from environment variables or a `.env` file. See `.env.example` for a full reference.

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server listen port |
| `DATABASE_URL` | *(required)* | Path or URL to the IP location CSV file |
| `REDIS_URL` | *(empty)* | Redis connection URL; uses in-memory fallback when empty |
| `RATE_LIMIT_RPS` | `10` | In-memory fallback requests/sec when Redis is unavailable |
| `RATE_LIMIT_BURST_CAPACITY` | `20` | Token bucket capacity (Tier 1) |
| `RATE_LIMIT_BURST_REFILL_RATE_PER_SEC` | `10` | Token bucket refill rate in tokens/sec (Tier 1) |
| `RATE_LIMIT_PER_PORT_LIMIT` | `300` | Max requests per connection per window (Tier 2) |
| `RATE_LIMIT_PER_PORT_WINDOW` | `1m` | Tier 2 sliding window duration |
| `RATE_LIMIT_IP_LIMIT` | `2000` | Max requests per IP per window (Tier 3) |
| `RATE_LIMIT_IP_WINDOW` | `1m` | Tier 3 sliding window duration |
| `RATE_LIMIT_NOT_FOUND_LIMIT` | `20` | 404s before blocking (404 blacklist) |
| `RATE_LIMIT_NOT_FOUND_WINDOW` | `10m` | Sliding window for 404 counting |
| `RATE_LIMIT_NOT_FOUND_BASE_BLOCK_DURATION` | `1m` | First-violation block duration |
| `RATE_LIMIT_NOT_FOUND_MAX_BLOCK_DURATION` | `1h` | Maximum block duration (exponential backoff cap) |

## CSV Database Format

The CSV file must have columns in the order `ip, city, country`. An optional header row is auto-detected and skipped.

```
ip,city,country
8.8.8.8,Mountain View,United States
192.168.1.0/24,San Francisco,United States
2001:db8::/32,Amsterdam,Netherlands
```

Both single IP addresses and CIDR ranges are supported. CIDR lookups use longest-prefix matching.

## Project Structure

```
ip_2_country/
├── config/          # Environment-based configuration loading
├── database/        # Database abstraction and CSV implementation
├── handlers/        # HTTP request handlers
├── ratelimit/       # Multi-tier Redis-backed rate limiter
├── server/          # Server setup, middleware chain, graceful shutdown
├── test/e2e/        # End-to-end tests
├── data/            # IP location data CSV file
└── main.go          # Application entry point
```
