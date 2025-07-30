package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Server ServerConfig
	R2     R2Config
	Cache  CacheConfig
}

type ServerConfig struct {
	Port string
}

type R2Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
}

type CacheConfig struct {
	TTL     time.Duration
	Cleanup time.Duration
}

const (
	CacheTTL     = 24 * time.Hour
	CacheCleanup = 30 * time.Minute
)

func Load() (*Config, error) {
	config := &Config{
		Server: ServerConfig{
			Port: getEnvWithDefault("PORT", "8080"),
		},
		Cache: CacheConfig{
			TTL:     CacheTTL,
			Cleanup: CacheCleanup,
		},
	}

	var missingVars []string

	if config.R2.Endpoint = os.Getenv("R2_ENDPOINT"); config.R2.Endpoint == "" {
		missingVars = append(missingVars, "R2_ENDPOINT")
	}

	if config.R2.AccessKey = os.Getenv("R2_ACCESS_KEY"); config.R2.AccessKey == "" {
		missingVars = append(missingVars, "R2_ACCESS_KEY")
	}

	if config.R2.SecretKey = os.Getenv("R2_SECRET_KEY"); config.R2.SecretKey == "" {
		missingVars = append(missingVars, "R2_SECRET_KEY")
	}

	if config.R2.Bucket = os.Getenv("R2_BUCKET"); config.R2.Bucket == "" {
		missingVars = append(missingVars, "R2_BUCKET")
	}

	if len(missingVars) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %v", missingVars)
	}

	return config, nil
}

func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
