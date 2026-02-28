package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application configuration
type Config struct {
	Server    ServerConfig
	Redis     RedisConfig
	Worker    WorkerConfig
	Retry     RetryConfig
	HTTP      HTTPConfig
	RateLimit RateLimitConfig
	Logging   LoggingConfig
	Filter    FilterConfig
	Tracking  TrackingConfig
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port           string
	Host           string
	RequestTimeout time.Duration
	MaxRequestSize int64
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host                   string
	Port                   string
	Password               string
	DB                     int
	StreamName             string
	ConsumerGroup          string
	MaxLen                 int64
	PoolSize               int
	QueueBackpressureLimit int64 // Reject new items when depth >= this (0 = use 90% of MaxLen)
}

// WorkerConfig holds worker pool configuration
type WorkerConfig struct {
	Count     int
	BatchSize int
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts     int
	DelayFirst      time.Duration
	DelaySubsequent time.Duration
}

// HTTPConfig holds HTTP client configuration
type HTTPConfig struct {
	Timeout            time.Duration
	MaxIdleConns       int
	MaxConnsPerHost    int
	DestinationServers []string
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	RequestsPerSecond int
	Burst             int
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string
	Format string
}

// FilterConfig holds stoppage filter configuration
type FilterConfig struct {
	Enabled           bool
	RedisSyncInterval time.Duration
}

// TrackingConfig holds request tracking configuration
type TrackingConfig struct {
	Enabled     bool
	SampleRate  float64 // 0.0-1.0, percentage of requests to track (1.0 = 100%)
	MaxRequests int     // Maximum requests to keep in memory
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file if it exists (ignore error if not found)
	_ = godotenv.Load()

	config := &Config{
		Server: ServerConfig{
			Port:           getEnv("SERVER_PORT", "8080"),
			Host:           getEnv("SERVER_HOST", "0.0.0.0"),
			RequestTimeout: getDuration("REQUEST_TIMEOUT", 5*time.Second),
			MaxRequestSize: getInt64("MAX_REQUEST_SIZE", 1048576), // 1MB default
		},
		Redis: RedisConfig{
			Host:                   getEnv("REDIS_HOST", "localhost"),
			Port:                   getEnv("REDIS_PORT", "6379"),
			Password:               getEnv("REDIS_PASSWORD", ""),
			DB:                     getInt("REDIS_DB", 0),
			StreamName:             getEnv("REDIS_STREAM_NAME", "gps:reports"),
			ConsumerGroup:          getEnv("REDIS_CONSUMER_GROUP", "gps-workers"),
			MaxLen:                 getInt64("REDIS_MAX_LEN", 50000),        // Increased for 10K RPS
			PoolSize:               getInt("REDIS_POOL_SIZE", 500),          // Increased for 10K RPS
			QueueBackpressureLimit: getInt64("QUEUE_BACKPRESSURE_LIMIT", 0), // 0 = 90% of MaxLen
		},
		Worker: WorkerConfig{
			Count:     getInt("WORKER_COUNT", 100),      // Increased for 10K RPS
			BatchSize: getInt("WORKER_BATCH_SIZE", 100), // Increased for 10K RPS
		},
		Retry: RetryConfig{
			MaxAttempts:     getInt("MAX_RETRY_ATTEMPTS", 3),
			DelayFirst:      getDuration("RETRY_DELAY_FIRST", 500*time.Millisecond), // Reduced for faster retries
			DelaySubsequent: getDuration("RETRY_DELAY_SUBSEQUENT", 1*time.Second),
		},
		HTTP: HTTPConfig{
			Timeout:            getDuration("HTTP_CLIENT_TIMEOUT", 10*time.Second), // Reduced timeout
			MaxIdleConns:       getInt("HTTP_CLIENT_MAX_IDLE_CONNS", 500),          // Increased for 10K RPS
			MaxConnsPerHost:    getInt("HTTP_CLIENT_MAX_CONNS_PER_HOST", 100),      // Increased for 10K RPS
			DestinationServers: getSlice("DESTINATION_SERVERS", []string{}),
		},
		RateLimit: RateLimitConfig{
			RequestsPerSecond: getInt("RATE_LIMIT_REQUESTS_PER_SECOND", 15000), // Increased for 10K RPS
			Burst:             getInt("RATE_LIMIT_BURST", 20000),               // Increased for 10K RPS
		},
		Logging: LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		Filter: FilterConfig{
			Enabled:           getBool("FILTER_ENABLED", false),
			RedisSyncInterval: getDuration("FILTER_REDIS_SYNC_INTERVAL", 30*time.Second),
		},
		Tracking: TrackingConfig{
			Enabled:     getBool("TRACKING_ENABLED", true),
			SampleRate:  getFloat64("TRACKING_SAMPLE_RATE", 0.01), // 1% sampling by default
			MaxRequests: getInt("TRACKING_MAX_REQUESTS", 10000),
		},
	}

	// Validate required fields
	if len(config.HTTP.DestinationServers) == 0 {
		return nil, fmt.Errorf("DESTINATION_SERVERS must be configured")
	}

	return config, nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// getInt gets an integer environment variable or returns a default value
func getInt(key string, defaultValue int) int {
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

// getInt64 gets an int64 environment variable or returns a default value
func getInt64(key string, defaultValue int64) int64 {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseInt(valueStr, 10, 64)
	if err != nil {
		return defaultValue
	}
	return value
}

// getDuration gets a duration environment variable or returns a default value
func getDuration(key string, defaultValue time.Duration) time.Duration {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := time.ParseDuration(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

// getSlice gets a comma-separated string as a slice
func getSlice(key string, defaultValue []string) []string {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	parts := strings.Split(valueStr, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// getBool gets a boolean environment variable or returns a default value
func getBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

// getFloat64 gets a float64 environment variable or returns a default value
func getFloat64(key string, defaultValue float64) float64 {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return defaultValue
	}
	return value
}

// GetRedisAddr returns the Redis address
func (c *RedisConfig) GetAddr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}
