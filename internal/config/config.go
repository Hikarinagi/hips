package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"
)

type Config struct {
	Server     ServerConfig
	R2         R2Config
	Cache      CacheConfig
	Concurrent ConcurrentConfig
	Network    NetworkConfig
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

type ConcurrentConfig struct {
	MaxWorkers   int           `json:"max_workers"`
	MaxQueueSize int           `json:"max_queue_size"`
	TaskTimeout  time.Duration `json:"task_timeout"`
	EnableAsync  bool          `json:"enable_async"`
	BufferSize   int           `json:"buffer_size"`
}

type NetworkConfig struct {
	MaxIdleConns        int           `json:"max_idle_conns"`
	MaxIdleConnsPerHost int           `json:"max_idle_conns_per_host"`
	MaxConnsPerHost     int           `json:"max_conns_per_host"`
	DialTimeout         time.Duration `json:"dial_timeout"`
	KeepAlive           time.Duration `json:"keep_alive"`
	IdleConnTimeout     time.Duration `json:"idle_conn_timeout"`
	RequestTimeout      time.Duration `json:"request_timeout"`
	DisableCompression  bool          `json:"disable_compression"`
}

const (
	CacheTTL     = 24 * time.Hour
	CacheCleanup = 30 * time.Minute

	DefaultMaxWorkers   = 0
	DefaultMaxQueueSize = 0
	DefaultTaskTimeout  = 30 * time.Second
	DefaultBufferSize   = 100

	// 网络配置默认值
	DefaultMaxIdleConns        = 100
	DefaultMaxIdleConnsPerHost = 20
	DefaultMaxConnsPerHost     = 50
	DefaultDialTimeout         = 10 * time.Second
	DefaultKeepAlive           = 30 * time.Second
	DefaultIdleConnTimeout     = 90 * time.Second
	DefaultRequestTimeout      = 60 * time.Second
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
		Concurrent: ConcurrentConfig{
			MaxWorkers:   getEnvIntWithDefault("MAX_WORKERS", DefaultMaxWorkers),
			MaxQueueSize: getEnvIntWithDefault("MAX_QUEUE_SIZE", DefaultMaxQueueSize),
			TaskTimeout:  getEnvDurationWithDefault("TASK_TIMEOUT", DefaultTaskTimeout),
			EnableAsync:  getEnvBoolWithDefault("ENABLE_ASYNC", true),
			BufferSize:   getEnvIntWithDefault("BUFFER_SIZE", DefaultBufferSize),
		},
		Network: NetworkConfig{
			MaxIdleConns:        getEnvIntWithDefault("NET_MAX_IDLE_CONNS", DefaultMaxIdleConns),
			MaxIdleConnsPerHost: getEnvIntWithDefault("NET_MAX_IDLE_CONNS_PER_HOST", DefaultMaxIdleConnsPerHost),
			MaxConnsPerHost:     getEnvIntWithDefault("NET_MAX_CONNS_PER_HOST", DefaultMaxConnsPerHost),
			DialTimeout:         getEnvDurationWithDefault("NET_DIAL_TIMEOUT", DefaultDialTimeout),
			KeepAlive:           getEnvDurationWithDefault("NET_KEEP_ALIVE", DefaultKeepAlive),
			IdleConnTimeout:     getEnvDurationWithDefault("NET_IDLE_CONN_TIMEOUT", DefaultIdleConnTimeout),
			RequestTimeout:      getEnvDurationWithDefault("NET_REQUEST_TIMEOUT", DefaultRequestTimeout),
			DisableCompression:  getEnvBoolWithDefault("NET_DISABLE_COMPRESSION", false),
		},
	}

	if config.Concurrent.MaxWorkers <= 0 {
		config.Concurrent.MaxWorkers = runtime.NumCPU() * 2
	}
	if config.Concurrent.MaxQueueSize <= 0 {
		config.Concurrent.MaxQueueSize = config.Concurrent.MaxWorkers * 10
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

func getEnvIntWithDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBoolWithDefault(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDurationWithDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}
