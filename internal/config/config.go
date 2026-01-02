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
	Server   ServerConfig
	Redis    RedisConfig
	MySQL    MySQLConfig
	Worker   WorkerConfig
	Retry    RetryConfig
	HTTP     HTTPConfig
	RateLimit RateLimitConfig
	Logging  LoggingConfig
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
	Host          string
	Port          string
	Password      string
	DB            int
	StreamName    string
	ConsumerGroup string
	MaxLen        int64
}

// MySQLConfig holds MySQL configuration
type MySQLConfig struct {
	Host            string
	Port            string
	User            string
	Password        string
	Database        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// WorkerConfig holds worker pool configuration
type WorkerConfig struct {
	Count     int
	BatchSize int
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts       int
	DelayFirst        time.Duration
	DelaySubsequent   time.Duration
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
			Host:          getEnv("REDIS_HOST", "localhost"),
			Port:          getEnv("REDIS_PORT", "6379"),
			Password:      getEnv("REDIS_PASSWORD", ""),
			DB:            getInt("REDIS_DB", 0),
			StreamName:    getEnv("REDIS_STREAM_NAME", "gps:reports"),
			ConsumerGroup: getEnv("REDIS_CONSUMER_GROUP", "gps-workers"),
			MaxLen:        getInt64("REDIS_MAX_LEN", 10000),
		},
		MySQL: MySQLConfig{
			Host:            getEnv("MYSQL_HOST", "localhost"),
			Port:            getEnv("MYSQL_PORT", "3306"),
			User:            getEnv("MYSQL_USER", "root"),
			Password:        getEnv("MYSQL_PASSWORD", ""),
			Database:        getEnv("MYSQL_DATABASE", "gps_receiver"),
			MaxOpenConns:    getInt("MYSQL_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getInt("MYSQL_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: getDuration("MYSQL_CONN_MAX_LIFETIME", 300*time.Second),
		},
		Worker: WorkerConfig{
			Count:     getInt("WORKER_COUNT", 50),
			BatchSize: getInt("WORKER_BATCH_SIZE", 10),
		},
		Retry: RetryConfig{
			MaxAttempts:     getInt("MAX_RETRY_ATTEMPTS", 5),
			DelayFirst:      getDuration("RETRY_DELAY_FIRST", 5*time.Second),
			DelaySubsequent: getDuration("RETRY_DELAY_SUBSEQUENT", 10*time.Second),
		},
		HTTP: HTTPConfig{
			Timeout:            getDuration("HTTP_CLIENT_TIMEOUT", 30*time.Second),
			MaxIdleConns:       getInt("HTTP_CLIENT_MAX_IDLE_CONNS", 100),
			MaxConnsPerHost:    getInt("HTTP_CLIENT_MAX_CONNS_PER_HOST", 10),
			DestinationServers: getSlice("DESTINATION_SERVERS", []string{}),
		},
		RateLimit: RateLimitConfig{
			RequestsPerSecond: getInt("RATE_LIMIT_REQUESTS_PER_SECOND", 1000),
			Burst:             getInt("RATE_LIMIT_BURST", 2000),
		},
		Logging: LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
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

// GetDSN returns the MySQL DSN connection string
func (c *MySQLConfig) GetDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.Database)
}

// GetRedisAddr returns the Redis address
func (c *RedisConfig) GetAddr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

