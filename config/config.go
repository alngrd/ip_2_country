package config

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port         string
	RateLimitRPS int
	DatabaseType string
	DatabasePath string
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables or defaults")
	}

	cfg := &Config{
		Port:         getEnv("PORT", "8080"),
		RateLimitRPS: getEnvAsInt("RATE_LIMIT_RPS", 10),
		DatabaseType: getEnv("DATABASE_TYPE", "csv"),
		DatabasePath: getEnv("DATABASE_PATH", "data/ip2country.csv"),
	}

	if cfg.RateLimitRPS <= 0 {
		return nil, fmt.Errorf("RATE_LIMIT_RPS must be greater than 0")
	}

	if cfg.DatabaseType == "" {
		return nil, fmt.Errorf("DATABASE_TYPE must be set")
	}

	if cfg.DatabasePath == "" {
		return nil, fmt.Errorf("DATABASE_PATH must be set")
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

