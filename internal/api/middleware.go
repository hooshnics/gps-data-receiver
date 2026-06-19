package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// RequestIDMiddleware adds a unique request ID to each request
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// LoggingMiddleware logs request details and records metrics.
// Normal requests are logged at Debug level to avoid I/O bottlenecks under load;
// slow (>1s) or failed (4xx/5xx) requests are logged at Info/Warn.
func LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		if metrics.AppMetrics != nil {
			metrics.AppMetrics.HTTPActiveRequests.Inc()
			defer metrics.AppMetrics.HTTPActiveRequests.Dec()
		}

		c.Next()

		duration := time.Since(start)
		statusCode := c.Writer.Status()
		requestSize := int(c.Request.ContentLength)
		responseSize := c.Writer.Size()

		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordHTTPRequest(
				method,
				path,
				fmt.Sprintf("%d", statusCode),
				duration,
				requestSize,
				responseSize,
			)
		}

		requestID, _ := c.Get("request_id")
		rid, _ := requestID.(string)

		switch {
		case statusCode >= 500:
			logger.Warn("Request completed with server error",
				zap.String("request_id", rid),
				zap.String("method", method),
				zap.String("path", path),
				zap.Int("status", statusCode),
				zap.Duration("duration", duration))
		case statusCode >= 400:
			logger.Info("Request completed with client error",
				zap.String("request_id", rid),
				zap.String("method", method),
				zap.String("path", path),
				zap.Int("status", statusCode),
				zap.Duration("duration", duration))
		case duration > 1*time.Second:
			logger.Info("Slow request completed",
				zap.String("request_id", rid),
				zap.String("method", method),
				zap.String("path", path),
				zap.Int("status", statusCode),
				zap.Duration("duration", duration))
		default:
			logger.Debug("Request completed",
				zap.String("request_id", rid),
				zap.String("method", method),
				zap.String("path", path),
				zap.Int("status", statusCode),
				zap.Duration("duration", duration))
		}
	}
}

// RecoveryMiddleware recovers from panics
func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				requestID, _ := c.Get("request_id")

				logger.Error("Panic recovered",
					zap.String("request_id", requestID.(string)),
					zap.Any("error", err),
					zap.String("path", c.Request.URL.Path))

				c.JSON(500, gin.H{
					"error":      "Internal server error",
					"request_id": requestID,
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}

// rateLimiterEntry pairs a limiter with the time it was last accessed.
type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimiterShard is a single shard of the rate limiter
type rateLimiterShard struct {
	limiters map[string]*rateLimiterEntry
	mu       sync.RWMutex
}

// shardCount determines the number of shards (must be power of 2 for fast modulo)
const shardCount = 256

// RateLimiter holds rate limiting configuration per IP using sharding to reduce contention
type RateLimiter struct {
	shards [shardCount]*rateLimiterShard
	rate   rate.Limit
	burst  int
}

// NewRateLimiter creates a new sharded rate limiter and starts background cleanup goroutines.
func NewRateLimiter(requestsPerSecond, burst int) *RateLimiter {
	rl := &RateLimiter{
		rate:  rate.Limit(requestsPerSecond),
		burst: burst,
	}

	for i := 0; i < shardCount; i++ {
		rl.shards[i] = &rateLimiterShard{
			limiters: make(map[string]*rateLimiterEntry),
		}
	}

	go rl.cleanupLoop()
	return rl
}

// getShard returns the shard for a given IP using FNV-1a hash
func (rl *RateLimiter) getShard(ip string) *rateLimiterShard {
	h := uint32(2166136261) // FNV-1a offset basis
	for i := 0; i < len(ip); i++ {
		h ^= uint32(ip[i])
		h *= 16777619 // FNV-1a prime
	}
	return rl.shards[h&(shardCount-1)] // Fast modulo for power of 2
}

// getLimiter gets or creates a limiter for an IP
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	now := time.Now()
	shard := rl.getShard(ip)

	shard.mu.RLock()
	entry, exists := shard.limiters[ip]
	shard.mu.RUnlock()

	if exists {
		entry.lastSeen = now
		return entry.limiter
	}

	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, exists = shard.limiters[ip]
	if exists {
		entry.lastSeen = now
		return entry.limiter
	}

	limiter := rate.NewLimiter(rl.rate, rl.burst)
	shard.limiters[ip] = &rateLimiterEntry{limiter: limiter, lastSeen: now}

	return limiter
}

// cleanupLoop evicts stale entries from all shards every 5 minutes.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		for _, shard := range rl.shards {
			shard.mu.Lock()
			for ip, entry := range shard.limiters {
				if entry.lastSeen.Before(cutoff) {
					delete(shard.limiters, ip)
				}
			}
			shard.mu.Unlock()
		}
	}
}

// RateLimitMiddleware implements per-IP rate limiting
func RateLimitMiddleware(rl *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := rl.getLimiter(ip)

		if !limiter.Allow() {
			requestID, _ := c.Get("request_id")

			logger.Warn("Rate limit exceeded",
				zap.String("request_id", requestID.(string)),
				zap.String("client_ip", ip))

			// Record rate limit hit
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordRateLimitHit(ip)
			}

			c.JSON(429, gin.H{
				"error":      "Too many requests",
				"request_id": requestID,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// TimeoutMiddleware adds a timeout to requests
func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(c.Request.Context())
		c.Next()
	}
}

// ContentTypeMiddleware validates Content-Type header.
// Skips validation for /socket.io/ (Socket.IO handshake and transport use non-JSON content types).
func ContentTypeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/socket.io") {
			c.Next()
			return
		}
		if c.Request.Method == "POST" || c.Request.Method == "PUT" {
			contentType := c.GetHeader("Content-Type")
			if contentType != "application/json" && contentType != "" {
				requestID, _ := c.Get("request_id")

				logger.Warn("Invalid Content-Type",
					zap.String("request_id", requestID.(string)),
					zap.String("content_type", contentType))

				c.JSON(400, gin.H{
					"error":      "Content-Type must be application/json",
					"request_id": requestID,
				})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// RequestSizeLimitMiddleware limits the size of request bodies
func RequestSizeLimitMiddleware(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		c.Next()
	}
}
