package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/api"
	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/queue"
)

func setupBenchRedis(b *testing.B) *queue.RedisQueue {
	b.Helper()

	cfg := &config.RedisConfig{
		Host:          "localhost",
		Port:          "6379",
		Password:      "",
		DB:            15,
		StreamName:    "gps:bench:reports",
		ConsumerGroup: "bench-workers",
		MaxLen:        100000,
		PoolSize:      100,
	}

	q, err := queue.NewRedisQueue(cfg)
	if err != nil {
		b.Skipf("Redis not available: %v", err)
	}

	ctx := context.Background()
	_ = q.GetClient().Del(ctx, cfg.StreamName).Err()
	b.Cleanup(func() {
		_ = q.GetClient().Del(ctx, cfg.StreamName).Err()
		q.Close()
	})

	return q
}

func setupBenchRouter(b *testing.B, q *queue.RedisQueue) *gin.Engine {
	b.Helper()
	gin.SetMode(gin.ReleaseMode)

	rateLimiter := api.NewRateLimiter(15000, 20000)

	router := gin.New()
	router.Use(api.RequestIDMiddleware())
	router.Use(api.RateLimitMiddleware(rateLimiter))
	router.Use(api.ContentTypeMiddleware())

	handler := api.NewHandler(q, 0, nil, 0, nil)
	router.POST("/api/gps/reports", handler.ReceiveGPSData)
	return router
}

func benchPayload(deviceID int) []byte {
	payload := map[string]interface{}{
		"device_id": deviceID,
		"lat":       37.7749,
		"lon":       -122.4194,
		"timestamp": time.Now().Unix(),
		"speed":     45.5,
	}
	body, _ := json.Marshal(payload)
	return body
}

// BenchmarkReceiveGPSData measures in-process handler throughput (requires Redis).
func BenchmarkReceiveGPSData(b *testing.B) {
	q := setupBenchRedis(b)
	router := setupBenchRouter(b, q)
	body := benchPayload(1)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		id := 0
		for pb.Next() {
			id++
			req := httptest.NewRequest("POST", "/api/gps/reports", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "req/s")
}

// BenchmarkReceiveGPSDataNoRateLimit measures peak handler throughput without rate limiting.
func BenchmarkReceiveGPSDataNoRateLimit(b *testing.B) {
	q := setupBenchRedis(b)
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(api.RequestIDMiddleware())
	router.Use(api.ContentTypeMiddleware())
	handler := api.NewHandler(q, 0, nil, 0, nil)
	router.POST("/api/gps/reports", handler.ReceiveGPSData)

	body := benchPayload(1)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("POST", "/api/gps/reports", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "req/s")
}

// BenchmarkEnqueueOnly measures Redis enqueue throughput without HTTP overhead.
func BenchmarkEnqueueOnly(b *testing.B) {
	q := setupBenchRedis(b)
	body := benchPayload(1)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = q.EnqueueWithDepth(ctx, body)
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "enqueue/s")
}
