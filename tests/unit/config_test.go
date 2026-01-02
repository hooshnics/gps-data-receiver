package unit

import (
	"os"
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear environment variables
	os.Clearenv()

	// Set only required variables
	os.Setenv("DESTINATION_SERVERS", "http://server1.example.com,http://server2.example.com")

	cfg, err := config.Load()
	assert.NoError(t, err)
	assert.NotNil(t, cfg)

	// Check defaults
	assert.Equal(t, "8080", cfg.Server.Port)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, 5*time.Second, cfg.Server.RequestTimeout)
	assert.Equal(t, int64(1048576), cfg.Server.MaxRequestSize)

	assert.Equal(t, "localhost", cfg.Redis.Host)
	assert.Equal(t, "6379", cfg.Redis.Port)
	assert.Equal(t, 0, cfg.Redis.DB)

	assert.Equal(t, 50, cfg.Worker.Count)
	assert.Equal(t, 5, cfg.Retry.MaxAttempts)
	assert.Equal(t, 5*time.Second, cfg.Retry.DelayFirst)
	assert.Equal(t, 10*time.Second, cfg.Retry.DelaySubsequent)
}

func TestLoadConfig_CustomValues(t *testing.T) {
	// Set custom environment variables
	os.Clearenv()
	os.Setenv("SERVER_PORT", "9000")
	os.Setenv("SERVER_HOST", "127.0.0.1")
	os.Setenv("REQUEST_TIMEOUT", "10s")
	os.Setenv("MAX_REQUEST_SIZE", "2097152")

	os.Setenv("REDIS_HOST", "redis-server")
	os.Setenv("REDIS_PORT", "6380")
	os.Setenv("REDIS_DB", "1")

	os.Setenv("WORKER_COUNT", "100")
	os.Setenv("MAX_RETRY_ATTEMPTS", "3")
	os.Setenv("RETRY_DELAY_FIRST", "3s")
	os.Setenv("RETRY_DELAY_SUBSEQUENT", "5s")

	os.Setenv("DESTINATION_SERVERS", "http://s1.com,http://s2.com,http://s3.com")

	os.Setenv("RATE_LIMIT_REQUESTS_PER_SECOND", "500")
	os.Setenv("RATE_LIMIT_BURST", "1000")

	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_FORMAT", "console")

	cfg, err := config.Load()
	assert.NoError(t, err)
	assert.NotNil(t, cfg)

	// Check custom values
	assert.Equal(t, "9000", cfg.Server.Port)
	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, 10*time.Second, cfg.Server.RequestTimeout)
	assert.Equal(t, int64(2097152), cfg.Server.MaxRequestSize)

	assert.Equal(t, "redis-server", cfg.Redis.Host)
	assert.Equal(t, "6380", cfg.Redis.Port)
	assert.Equal(t, 1, cfg.Redis.DB)

	assert.Equal(t, 100, cfg.Worker.Count)
	assert.Equal(t, 3, cfg.Retry.MaxAttempts)
	assert.Equal(t, 3*time.Second, cfg.Retry.DelayFirst)
	assert.Equal(t, 5*time.Second, cfg.Retry.DelaySubsequent)

	assert.Len(t, cfg.HTTP.DestinationServers, 3)
	assert.Contains(t, cfg.HTTP.DestinationServers, "http://s1.com")

	assert.Equal(t, 500, cfg.RateLimit.RequestsPerSecond)
	assert.Equal(t, 1000, cfg.RateLimit.Burst)

	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "console", cfg.Logging.Format)
}

func TestLoadConfig_MissingDestinationServers(t *testing.T) {
	os.Clearenv()

	cfg, err := config.Load()
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "DESTINATION_SERVERS must be configured")
}

func TestGetDSN(t *testing.T) {
	mysqlCfg := config.MySQLConfig{
		Host:     "localhost",
		Port:     "3306",
		User:     "testuser",
		Password: "testpass",
		Database: "testdb",
	}

	dsn := mysqlCfg.GetDSN()
	expected := "testuser:testpass@tcp(localhost:3306)/testdb?charset=utf8mb4&parseTime=True&loc=Local"
	assert.Equal(t, expected, dsn)
}

func TestGetRedisAddr(t *testing.T) {
	redisCfg := config.RedisConfig{
		Host: "redis-host",
		Port: "6380",
	}

	addr := redisCfg.GetAddr()
	assert.Equal(t, "redis-host:6380", addr)
}

func TestLoadConfig_ServerList(t *testing.T) {
	os.Clearenv()
	
	testCases := []struct {
		name     string
		envValue string
		expected []string
	}{
		{
			name:     "Single server",
			envValue: "http://server1.com",
			expected: []string{"http://server1.com"},
		},
		{
			name:     "Multiple servers",
			envValue: "http://s1.com,http://s2.com,http://s3.com",
			expected: []string{"http://s1.com", "http://s2.com", "http://s3.com"},
		},
		{
			name:     "Servers with spaces",
			envValue: "http://s1.com , http://s2.com , http://s3.com",
			expected: []string{"http://s1.com", "http://s2.com", "http://s3.com"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("DESTINATION_SERVERS", tc.envValue)
			cfg, err := config.Load()
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, cfg.HTTP.DestinationServers)
		})
	}
}

