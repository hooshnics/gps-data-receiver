package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/api"
	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/queue"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) *queue.RedisQueue {
	cfg := &config.RedisConfig{
		Host:          "localhost",
		Port:          "6379",
		Password:      "",
		DB:            15, // Use test DB
		StreamName:    "gps:test:reports",
		ConsumerGroup: "test-workers",
		MaxLen:        1000,
	}

	q, err := queue.NewRedisQueue(cfg)
	if err != nil {
		t.Skipf("Redis not available: %v. Run 'docker-compose up -d redis' to run integration tests", err)
	}

	// Clean up stream before test
	ctx := context.Background()
	q.GetClient().Del(ctx, cfg.StreamName)

	return q
}

func TestAPIIntegration_ReceiveGPSData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	redisQueue := setupTestRedis(t)
	defer redisQueue.Close()

	handler := api.NewHandler(redisQueue, 0, nil)

	router := gin.New()
	router.Use(api.RequestIDMiddleware())
	router.POST("/api/gps/reports", handler.ReceiveGPSData)

	// Test data
	payload := map[string]interface{}{
		"device_id": "GPS001",
		"lat":       37.7749,
		"lon":       -122.4194,
		"timestamp": time.Now().Unix(),
		"speed":     45.5,
	}

	body, _ := json.Marshal(payload)

	// Send request
	req := httptest.NewRequest("POST", "/api/gps/reports", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "queued", response["status"])
	assert.NotEmpty(t, response["message_id"])

	// Verify data is in Redis
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Read from stream
	messages, err := redisQueue.GetClient().XRead(ctx, &redis.XReadArgs{
		Streams: []string{redisQueue.GetStreamName(), "0"},
		Count:   1,
	}).Result()

	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Messages, 1)

	// Verify the data matches
	msgData := messages[0].Messages[0].Values["data"].(string)
	assert.JSONEq(t, string(body), msgData)
}

func TestAPIIntegration_ConcurrentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	redisQueue := setupTestRedis(t)
	defer redisQueue.Close()

	handler := api.NewHandler(redisQueue, 0, nil)

	router := gin.New()
	router.Use(api.RequestIDMiddleware())
	router.POST("/api/gps/reports", handler.ReceiveGPSData)

	// Send multiple concurrent requests
	numRequests := 100
	responses := make(chan int, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			payload := map[string]interface{}{
				"device_id": id,
				"lat":       37.7749 + float64(id)*0.001,
				"lon":       -122.4194,
			}

			body, _ := json.Marshal(payload)
			req := httptest.NewRequest("POST", "/api/gps/reports", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			responses <- w.Code
		}(i)
	}

	// Collect responses
	successCount := 0
	for i := 0; i < numRequests; i++ {
		code := <-responses
		if code == http.StatusOK {
			successCount++
		}
	}

	assert.Equal(t, numRequests, successCount, "All requests should succeed")

	// Verify messages are in Redis
	time.Sleep(100 * time.Millisecond) // Small delay for Redis writes

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages, err := redisQueue.GetClient().XLen(ctx, redisQueue.GetStreamName()).Result()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, int(messages), numRequests, "All messages should be queued")
}

func TestAPIIntegration_HealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	redisQueue := setupTestRedis(t)
	defer redisQueue.Close()

	handler := api.NewHandler(redisQueue, 0, nil)

	router := gin.New()
	router.GET("/health", handler.Health)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "healthy", response["status"])
}
