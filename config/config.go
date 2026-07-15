package config

import (
	"fmt"
	"log"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	Port         string `env:"PORT"           envDefault:"8080"`
	RateLimitRPS int    `env:"RATE_LIMIT_RPS" envDefault:"10"`
	DatabaseURL  string `env:"DATABASE_URL,notEmpty"`
	RedisURL     string `env:"REDIS_URL"`
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

	return cfg, nil
}
