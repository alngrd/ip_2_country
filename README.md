# IP to Country Service

A lightweight Go-based web service that provides IP address to country/city lookup functionality with rate limiting support.

## Features

- IP address to country and city lookup
- Rate limiting per IP address
- CSV-based database for IP location data
- Docker support
- Configurable via environment variables

## Getting Started

### Prerequisites

- Go 1.21 or higher
- IP location data CSV file

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

### API Usage

**Endpoint:** `GET /find-country?ip=<ip_address>`

**Example:**

```bash
curl http://localhost:8080/find-country?ip=8.8.8.8
```

**Response:**

```json
{
  "country": "United States",
  "city": "Mountain View"
}
```

**Error Responses:**

- `400 Bad Request`: Missing or invalid IP parameter
- `404 Not Found`: IP address not found in database
- `429 Too Many Requests`: Rate limit exceeded
- `500 Internal Server Error`: Server error

## Docker

Build and run with Docker:

```bash
docker build -t ip2country .
docker run -p 8080:8080 ip2country
```

## Configuration

The service can be configured using environment variables or a `.env` file:

- `PORT`: Server port (default: 8080)
- `RATE_LIMIT_RPS`: Rate limit requests per second per IP (default: 10)
- `DATABASE_TYPE`: Database type (default: csv)
- `DATABASE_PATH`: Path to database file (default: data/ip2country.csv)

## Project Structure

```
ip_2_country/
├── config/          # Configuration management
├── database/        # Database abstraction and CSV implementation
├── handlers/        # HTTP request handlers
├── ratelimit/       # Rate limiting implementation
├── server/          # Server setup and lifecycle
├── data/            # IP location data CSV file
└── main.go          # Application entry point
```

