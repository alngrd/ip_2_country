package config_test

// Black-box tests: only Load() (exported) is exercised here,
// so package config_test is the right choice over package config.

import (
	"testing"
	"time"

	"ip2country/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("RATE_LIMIT_RPS", "")
	t.Setenv("DATABASE_URL", "csv:data/ip2country.csv")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error with defaults: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port: expected 8080, got %s", cfg.Port)
	}
	if cfg.RateLimitRPS != 10 {
		t.Errorf("RateLimitRPS: expected 10, got %d", cfg.RateLimitRPS)
	}
}

func TestLoad_EnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("RATE_LIMIT_RPS", "50")
	t.Setenv("DATABASE_URL", "csv:/tmp/custom.csv")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port: expected 9090, got %s", cfg.Port)
	}
	if cfg.RateLimitRPS != 50 {
		t.Errorf("RateLimitRPS: expected 50, got %d", cfg.RateLimitRPS)
	}
	if cfg.DatabaseURL != "csv:/tmp/custom.csv" {
		t.Errorf("DatabaseURL: expected csv:/tmp/custom.csv, got %s", cfg.DatabaseURL)
	}
}

func TestLoad_DatabaseURL_Validation(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "empty DATABASE_URL errors", url: "", wantErr: true},
		{name: "valid csv URL succeeds", url: "csv:data/ip2country.csv", wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RATE_LIMIT_RPS", "10")
			t.Setenv("DATABASE_URL", tc.url)

			_, err := config.Load()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoad_Defaults_NewBurstAndNotFoundFields(t *testing.T) {
	t.Setenv("DATABASE_URL", "csv:data/ip2country.csv")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error with defaults: %v", err)
	}
	if cfg.RateLimitBurstCapacity != 20 {
		t.Errorf("BurstCapacity: expected 20, got %d", cfg.RateLimitBurstCapacity)
	}
	if cfg.RateLimitBurstRefillRatePerSec != 10.0 {
		t.Errorf("BurstRefillRatePerSec: expected 10.0, got %f", cfg.RateLimitBurstRefillRatePerSec)
	}
	if cfg.RateLimitNotFoundBaseBlockDuration != time.Minute {
		t.Errorf("NotFoundBaseBlockDuration: expected 1m, got %v", cfg.RateLimitNotFoundBaseBlockDuration)
	}
	if cfg.RateLimitNotFoundMaxBlockDuration != time.Hour {
		t.Errorf("NotFoundMaxBlockDuration: expected 1h, got %v", cfg.RateLimitNotFoundMaxBlockDuration)
	}
}

func TestLoad_BurstCapacity_Validation(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "csv:data/ip2country.csv"}
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "zero errors", value: "0", wantErr: true},
		{name: "negative errors", value: "-1", wantErr: true},
		{name: "valid positive", value: "10", wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range base {
				t.Setenv(k, v)
			}
			t.Setenv("RATE_LIMIT_BURST_CAPACITY", tc.value)
			_, err := config.Load()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for RATE_LIMIT_BURST_CAPACITY=%s, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoad_BurstRefillRate_Validation(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "csv:data/ip2country.csv"}
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "zero errors", value: "0", wantErr: true},
		{name: "negative errors", value: "-5", wantErr: true},
		{name: "valid positive", value: "5.5", wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range base {
				t.Setenv(k, v)
			}
			t.Setenv("RATE_LIMIT_BURST_REFILL_RATE_PER_SEC", tc.value)
			_, err := config.Load()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for RATE_LIMIT_BURST_REFILL_RATE_PER_SEC=%s, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoad_NotFoundBlockDuration_Validation(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "csv:data/ip2country.csv"}
	tests := []struct {
		name     string
		base     string
		max      string
		wantErr  bool
	}{
		{name: "zero base errors", base: "0s", max: "1h", wantErr: true},
		{name: "max equal to base errors", base: "1m", max: "1m", wantErr: true},
		{name: "max less than base errors", base: "10m", max: "1m", wantErr: true},
		{name: "valid base and max", base: "1m", max: "1h", wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range base {
				t.Setenv(k, v)
			}
			t.Setenv("RATE_LIMIT_NOT_FOUND_BASE_BLOCK_DURATION", tc.base)
			t.Setenv("RATE_LIMIT_NOT_FOUND_MAX_BLOCK_DURATION", tc.max)
			_, err := config.Load()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error (base=%s, max=%s), got nil", tc.base, tc.max)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoad_RateLimitRPS_Validation(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "csv:data/ip2country.csv",
	}

	tests := []struct {
		name    string
		value   string
		wantErr bool
		wantRPS int // only checked when wantErr=false
	}{
		{name: "zero", value: "0", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
		{name: "valid positive", value: "5", wantErr: false, wantRPS: 5},
		// Non-numeric is an invalid value and should return an error.
		{name: "non-numeric returns error", value: "abc", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range base {
				t.Setenv(k, v)
			}
			t.Setenv("RATE_LIMIT_RPS", tc.value)

			cfg, err := config.Load()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for RATE_LIMIT_RPS=%s, got nil", tc.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.RateLimitRPS != tc.wantRPS {
				t.Errorf("RateLimitRPS: expected %d, got %d", tc.wantRPS, cfg.RateLimitRPS)
			}
		})
	}
}
