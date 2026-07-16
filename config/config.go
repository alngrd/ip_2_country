package config

import (
	"fmt"
	"log"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	Port        string `env:"PORT"         envDefault:"8080"`
	DatabaseURL string `env:"DATABASE_URL,notEmpty"`
	RedisURL    string `env:"REDIS_URL"`

	RateLimitRPS int `env:"RATE_LIMIT_RPS" envDefault:"10"`

	RateLimitBurstCapacity        int     `env:"RATE_LIMIT_BURST_CAPACITY"          envDefault:"20"`
	RateLimitBurstRefillRatePerSec float64 `env:"RATE_LIMIT_BURST_REFILL_RATE_PER_SEC" envDefault:"10"`

	RateLimitPerPortLimit  int           `env:"RATE_LIMIT_PER_PORT_LIMIT"  envDefault:"300"`
	RateLimitPerPortWindow time.Duration `env:"RATE_LIMIT_PER_PORT_WINDOW" envDefault:"1m"`

	RateLimitIPLimit  int           `env:"RATE_LIMIT_IP_LIMIT"  envDefault:"2000"`
	RateLimitIPWindow time.Duration `env:"RATE_LIMIT_IP_WINDOW" envDefault:"1m"`

	RateLimitNotFoundLimit             int           `env:"RATE_LIMIT_NOT_FOUND_LIMIT"               envDefault:"20"`
	RateLimitNotFoundWindow            time.Duration `env:"RATE_LIMIT_NOT_FOUND_WINDOW"              envDefault:"10m"`
	RateLimitNotFoundBaseBlockDuration time.Duration `env:"RATE_LIMIT_NOT_FOUND_BASE_BLOCK_DURATION" envDefault:"1m"`
	RateLimitNotFoundMaxBlockDuration  time.Duration `env:"RATE_LIMIT_NOT_FOUND_MAX_BLOCK_DURATION"  envDefault:"1h"`
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables or defaults")
	}

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	if cfg.RateLimitRPS <= 0 {
		return nil, fmt.Errorf("RATE_LIMIT_RPS must be greater than 0")
	}
	if cfg.RateLimitBurstCapacity <= 0 {
		return nil, fmt.Errorf("RATE_LIMIT_BURST_CAPACITY must be greater than 0")
	}
	if cfg.RateLimitBurstRefillRatePerSec <= 0 {
		return nil, fmt.Errorf("RATE_LIMIT_BURST_REFILL_RATE_PER_SEC must be greater than 0")
	}
	if cfg.RateLimitNotFoundBaseBlockDuration <= 0 {
		return nil, fmt.Errorf("RATE_LIMIT_NOT_FOUND_BASE_BLOCK_DURATION must be greater than 0")
	}
	if cfg.RateLimitNotFoundMaxBlockDuration <= cfg.RateLimitNotFoundBaseBlockDuration {
		return nil, fmt.Errorf("RATE_LIMIT_NOT_FOUND_MAX_BLOCK_DURATION must be greater than RATE_LIMIT_NOT_FOUND_BASE_BLOCK_DURATION")
	}

	return cfg, nil
}
