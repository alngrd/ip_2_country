# IP to Country Service

A lightweight Go-based web service that provides IP address to country/city lookup functionality with rate limiting support.

## Features

- IPv4 and IPv6 address to country and city lookup
- CIDR range and exact IP matching in the database
- Sliding window rate limiting per IP address
- CSV-based database for IP location data
- Graceful shutdown on SIGINT/SIGTERM
- Docker support
- Configurable via environment variables

## Getting Started

### Prerequisites

- Go 1.21 or higher
- IP location data CSV file

### Installation

1. Clone the repository:

```bash
git clone <repository-url>
cd ip_2_country
```

2. Install dependencies:

```bash
go mod download
```

3. Configure environment variables (optional - defaults are provided):
   Create a `.env` file or set environment variables:

```
PORT=8080
RATE_LIMIT_RPS=10
DATABASE_TYPE=csv
DATABASE_PATH=data/ip2country.csv
```

### Running the Service

```bash
go run main.go
```

The service will start on the configured port (default: 8080).

### Running Tests

```bash
go test ./...
```

End-to-end tests are in `test/e2e/` and require a running service with the default CSV database.

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

## Docker

Build and run with Docker:

```bash
docker build -t ip2country .
docker run -p 8080:8080 ip2country
```

## Configuration

The service is configured via environment variables or a `.env` file:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `RATE_LIMIT_RPS` | `10` | Max requests per second per IP (must be > 0) |
| `DATABASE_TYPE` | `csv` | Database type (currently only `csv`) |
| `DATABASE_PATH` | `data/ip2country.csv` | Path to the CSV database file |

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
├── config/          # Configuration loading
├── database/        # Database abstraction, CSV implementation
├── handlers/        # HTTP request handlers
├── ratelimit/       # Sliding window rate limiter
├── server/          # Server setup, start, and graceful shutdown
├── test/e2e/        # End-to-end tests
├── data/            # IP location data CSV file
└── main.go          # Application entry point
```
