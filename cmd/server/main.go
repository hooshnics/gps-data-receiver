package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/api"
	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/filter"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/internal/queue"
	"github.com/gps-data-receiver/internal/sanitizer"
	"github.com/gps-data-receiver/internal/sender"
	"github.com/gps-data-receiver/internal/tracking"
	"github.com/gps-data-receiver/pkg/logger"
	socketio "github.com/ismhdez/socket.io-golang/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// filterOldData filters out GPS records older than the specified max age
func filterOldData(data []parser.ParsedGPSData, maxAge time.Duration) []parser.ParsedGPSData {
	if len(data) == 0 {
		return data
	}

	now := time.Now()
	filtered := make([]parser.ParsedGPSData, 0, len(data))

	for _, record := range data {
		// Parse the record's datetime (format: "2006-01-02 15:04:05")
		// ParseInLocation parses in local timezone instead of UTC
		recordTime, err := time.ParseInLocation("2006-01-02 15:04:05", record.DateTime, time.Local)
		if err != nil {
			// If we can't parse the time, include the record (fail-open)
			logger.Warn("Failed to parse record datetime, including record",
				zap.String("datetime", record.DateTime),
				zap.String("imei", record.IMEI),
				zap.Error(err))
			filtered = append(filtered, record)
			continue
		}

		// Calculate age
		age := now.Sub(recordTime)

		if age <= maxAge {
			// Record is recent enough, include it
			filtered = append(filtered, record)
		} else {
			// Record is too old, filter it out
			logger.Debug("Filtered old GPS record",
				zap.String("imei", record.IMEI),
				zap.String("datetime", record.DateTime),
				zap.Duration("age", age),
				zap.Duration("max_age", maxAge))
		}
	}

	return filtered
}

// filterFutureData filters out GPS records with timestamps in the future (beyond tolerance)
func filterFutureData(data []parser.ParsedGPSData, tolerance time.Duration) []parser.ParsedGPSData {
	if len(data) == 0 {
		return data
	}

	now := time.Now()
	filtered := make([]parser.ParsedGPSData, 0, len(data))

	for _, record := range data {
		// Parse the record's datetime (format: "2006-01-02 15:04:05")
		// ParseInLocation parses in local timezone instead of UTC
		recordTime, err := time.ParseInLocation("2006-01-02 15:04:05", record.DateTime, time.Local)
		if err != nil {
			// If we can't parse the time, include the record (fail-open)
			logger.Warn("Failed to parse record datetime, including record",
				zap.String("datetime", record.DateTime),
				zap.String("imei", record.IMEI),
				zap.Error(err))
			filtered = append(filtered, record)
			continue
		}

		// Calculate how far in the future this record is
		futureOffset := recordTime.Sub(now)

		// Allow past data (negative offset) and future data within tolerance
		if futureOffset <= 0 || futureOffset <= tolerance {
			// Record is in the past or within acceptable future range
			filtered = append(filtered, record)
		} else {
			// Record is too far in the future, filter it out
			logger.Debug("Filtered future GPS record",
				zap.String("imei", record.IMEI),
				zap.String("datetime", record.DateTime),
				zap.Duration("future_offset", futureOffset),
				zap.Duration("tolerance", tolerance))
		}
	}

	return filtered
}

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	if err := logger.InitLogger(cfg.Logging.Level, cfg.Logging.Format); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting GPS Data Receiver",
		zap.String("environment", os.Getenv("ENVIRONMENT")),
		zap.String("version", "1.0.0"))

	// Initialize metrics
	metrics.InitMetrics()
	logger.Info("Prometheus metrics initialized")

	// Initialize request tracker (max 10000 requests in memory)
	tracking.InitGlobalTracker(10000)
	logger.Info("Request tracking initialized")

	// Initialize Redis queue
	redisQueue, err := queue.NewRedisQueue(&cfg.Redis)
	if err != nil {
		logger.Fatal("Failed to initialize Redis queue", zap.Error(err))
	}
	defer redisQueue.Close()

	// Initialize HTTP sender
	httpSender := sender.NewHTTPSender(cfg)
	defer httpSender.Close()

	// Initialize stoppage filter
	stoppageFilter := filter.NewStoppageFilter(
		redisQueue.GetClient(),
		cfg.Filter.Enabled,
		cfg.Filter.RedisSyncInterval,
	)
	stoppageFilter.Start()
	defer stoppageFilter.Close()

	logger.Info("Stoppage filter initialized",
		zap.Bool("enabled", cfg.Filter.Enabled),
		zap.Duration("sync_interval", cfg.Filter.RedisSyncInterval))

	// Socket.IO for real-time broadcast (received + delivered). Created early so messageHandler can emit delivered events.
	io := socketio.New()

	// Create message handler that sends data to destination servers and broadcasts on success
	messageHandler := func(ctx context.Context, data []byte) error {
		start := time.Now()

		// 1. Sanitize UTF-8 encoding
		sanitized := sanitizer.SanitizeUTF8(data)

		// 2. Parse GPS data
		parseStart := time.Now()
		parsed, err := parser.Parse(sanitized)
		parseDuration := time.Since(parseStart)

		if err != nil {
			logger.Error("Failed to parse GPS data - dropping message",
				zap.Error(err),
				zap.String("raw_data", string(data)))
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordParseFailure()
				metrics.AppMetrics.RecordDataDropped("parse_error")
			}
			return nil // Return nil to ACK and drop the message (no retry)
		}

		// Handle empty parse results
		if len(parsed) == 0 {
			logger.Warn("No valid GPS records found - dropping message",
				zap.String("raw_data", string(data)))
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordParseFailure()
				metrics.AppMetrics.RecordDataDropped("empty_result")
			}
			return nil // Return nil to ACK and drop the message
		}

		// Record successful parse
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordParseSuccess(parseDuration, len(parsed))
		}

		logger.Debug("GPS data parsed successfully",
			zap.Int("record_count", len(parsed)),
			zap.Duration("parse_duration", parseDuration))

		// 3. Filter old data (older than 2 days)
		ageFilteredData := filterOldData(parsed, 48*time.Hour)

		if len(ageFilteredData) < len(parsed) {
			oldCount := len(parsed) - len(ageFilteredData)
			logger.Info("Filtered old GPS records",
				zap.Int("original_count", len(parsed)),
				zap.Int("filtered_count", len(ageFilteredData)),
				zap.Int("old_records", oldCount))
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordDataDropped("old_data")
			}
		}

		// Handle case where all records were filtered due to age
		if len(ageFilteredData) == 0 {
			logger.Debug("All records filtered (too old) - ACKing message",
				zap.Int("original_count", len(parsed)))
			return nil // ACK message without sending
		}

		// 4. Filter future data (tolerance: 5 minutes for clock drift)
		timeFilteredData := filterFutureData(ageFilteredData, 5*time.Minute)

		if len(timeFilteredData) < len(ageFilteredData) {
			futureCount := len(ageFilteredData) - len(timeFilteredData)
			logger.Info("Filtered future GPS records",
				zap.Int("original_count", len(ageFilteredData)),
				zap.Int("filtered_count", len(timeFilteredData)),
				zap.Int("future_records", futureCount))
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordDataDropped("future_data")
			}
		}

		// Handle case where all records were filtered due to future timestamps
		if len(timeFilteredData) == 0 {
			logger.Debug("All records filtered (future timestamps) - ACKing message",
				zap.Int("original_count", len(ageFilteredData)))
			return nil // ACK message without sending
		}

		// 5. Filter redundant stoppage data
		filteredData := stoppageFilter.FilterData(timeFilteredData)

		// Handle case where all records were filtered
		if len(filteredData) == 0 {
			logger.Debug("All records filtered (redundant stoppages) - ACKing message",
				zap.Int("original_count", len(parsed)))
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordStoppageFiltered(len(parsed))
			}
			return nil // ACK message without sending
		}

		// Record filtering metrics
		if len(filteredData) < len(parsed) && metrics.AppMetrics != nil {
			filteredCount := len(parsed) - len(filteredData)
			metrics.AppMetrics.RecordStoppageFiltered(filteredCount)
			logger.Info("Filtered redundant stoppage records",
				zap.Int("original_count", len(parsed)),
				zap.Int("filtered_count", len(filteredData)),
				zap.Int("removed_count", filteredCount))
		}

		// 6. Wrap filtered data in "data" field and re-encode as JSON for destination
		wrappedData := map[string]interface{}{
			"data": filteredData,
		}
		jsonData, err := json.Marshal(wrappedData)
		if err != nil {
			logger.Error("Failed to marshal parsed data - dropping message",
				zap.Error(err))
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordDataDropped("marshal_error")
			}
			return nil // Return nil to ACK and drop the message
		}

		// 7. Send filtered data to destination servers
		result := httpSender.Send(ctx, jsonData)

		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordQueueProcessing(time.Since(start))
		}

		if !result.Success {
			logger.Warn("Send failed, message will be retried via queue redelivery",
				zap.String("target_server", result.TargetServer),
				zap.Int("attempts", result.Attempt),
				zap.Error(result.Error))
			return result.Error
		}

		// Broadcast delivered packet to frontend (same pattern as received in handler)
		payload := map[string]interface{}{
			"delivered_at":  time.Now().UTC().Format(time.RFC3339),
			"target_server": result.TargetServer,
			"payload":       string(jsonData),
			"payload_size":  len(jsonData),
			"record_count":  len(parsed),
		}
		if err := io.Emit("gps-delivered", payload); err != nil {
			logger.Debug("Broadcast gps-delivered failed", zap.Error(err))
		}

		return nil
	}

	// Initialize consumer with worker pool.
	// maxRetries controls total queue delivery attempts before a message is dropped.
	consumer := queue.NewConsumer(
		redisQueue,
		cfg.Worker.Count,
		cfg.Worker.BatchSize,
		cfg.Retry.MaxAttempts,
		messageHandler,
	)

	// Start consumer workers
	consumer.Start()
	logger.Info("Worker pool started", zap.Int("workers", cfg.Worker.Count))

	// Initialize worker metrics with correct pool size
	if metrics.AppMetrics != nil {
		metrics.AppMetrics.InitWorkerCounts(cfg.Worker.Count)
	}

	// Setup Gin router
	if cfg.Logging.Format == "json" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Create rate limiter
	rateLimiter := api.NewRateLimiter(
		cfg.RateLimit.RequestsPerSecond,
		cfg.RateLimit.Burst,
	)

	// Apply middleware
	router.Use(api.RecoveryMiddleware())
	router.Use(api.RequestIDMiddleware())
	router.Use(api.LoggingMiddleware())
	router.Use(api.RateLimitMiddleware(rateLimiter))
	router.Use(api.ContentTypeMiddleware())
	router.Use(api.RequestSizeLimitMiddleware(cfg.Server.MaxRequestSize))

	// Wrap Socket.IO so WebSocket upgrade succeeds when proxied (e.g. Vite dev: Origin is localhost:5173, Host is localhost:8080).
	// The ismhdez library uses gorilla/websocket default CheckOrigin; rewriting Origin to match Host allows the upgrade.
	socketHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			r.Header.Set("Origin", "http://"+r.Host)
		}
		io.ServeHTTP(w, r)
	})
	router.Any("/socket.io/*path", gin.WrapH(socketHandler))
	logger.Info("Socket.IO enabled at /socket.io/")

	// Initialize handler (with backpressure limit from config; 0 = use 90% of Redis MaxLen)
	handler := api.NewHandler(redisQueue, cfg.Redis.QueueBackpressureLimit, io)

	// Setup routes
	router.POST("/api/gps/reports", handler.ReceiveGPSData)
	router.GET("/health", handler.Health)
	router.GET("/ready", handler.Ready)

	// Prometheus metrics endpoint
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Monitoring endpoints
	router.GET("/monitoring/requests", handler.ListRequests)
	router.GET("/monitoring/requests/:id", handler.GetRequestDetails)
	router.GET("/monitoring/statistics", handler.GetStatistics)

	// Frontend: when app is accessed on the main domain (GET /), serve the built Vue app from web/dist
	const frontendDir = "web/dist"
	indexPath := filepath.Join(frontendDir, "index.html")
	if dir, err := os.Stat(frontendDir); err == nil && dir.IsDir() {
		router.Static("/assets", filepath.Join(frontendDir, "assets"))
		router.StaticFile("/favicon.svg", filepath.Join(frontendDir, "favicon.svg"))
		// Main domain: root path serves the frontend app
		router.GET("/", func(c *gin.Context) {
			c.File(indexPath)
		})
		// SPA fallback: unknown GET paths serve index.html so client-side routing works
		router.NoRoute(func(c *gin.Context) {
			if c.Request.Method != http.MethodGet {
				c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
				return
			}
			path := c.Request.URL.Path
			cleanPath := filepath.Clean(filepath.Join(frontendDir, strings.TrimPrefix(path, "/")))
			if rel, err := filepath.Rel(frontendDir, cleanPath); err != nil || strings.HasPrefix(rel, "..") {
				c.File(indexPath)
				return
			}
			if f, err := os.Stat(cleanPath); err == nil && !f.IsDir() {
				c.File(cleanPath)
				return
			}
			c.File(indexPath)
		})
		logger.Info("Serving frontend from " + frontendDir + " at /")
	} else {
		// No built frontend: main domain still responds with a minimal status page
		router.GET("/", func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>GPS Data Receiver</title></head>
<body style="font-family:sans-serif;max-width:40em;margin:2em auto;padding:0 1em;">
<h1>GPS Data Receiver</h1>
<p>Backend is running. To load the app UI, build the frontend: <code>make web-build</code> then restart, or use the dev server: <code>make web-dev</code> (port 5173).</p>
<p><a href="/health">Health</a> · <a href="/ready">Ready</a></p>
</body></html>`))
		})
		logger.Info("Frontend not built (web/dist missing); serving status at /")
	}

	// Create HTTP server
	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:           addr,
		Handler:        router,
		ReadTimeout:    cfg.Server.RequestTimeout,
		WriteTimeout:   cfg.Server.RequestTimeout + 5*time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	// Start background metrics updater (more frequent updates for better monitoring)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.SetWorkerPoolSize(cfg.Worker.Count)

				activeWorkers := consumer.GetActiveWorkerCount()

				// Update filter state count metric
				filterStateCount := stoppageFilter.GetStateCount()
				metrics.AppMetrics.UpdateFilterStateCount(filterStateCount)

				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				queueLen, err := redisQueue.GetClient().XLen(ctx, redisQueue.GetStreamName()).Result()
				cancel()

				if err == nil {
					metrics.AppMetrics.UpdateQueueDepth(queueLen)

					if queueLen > 100 && activeWorkers == 0 {
						logger.Warn("Queue depth high but no active workers",
							zap.Int64("queue_depth", queueLen),
							zap.Int("active_workers", activeWorkers),
							zap.Int("pool_size", cfg.Worker.Count))
					}
				} else {
					logger.Error("Failed to get queue length", zap.Error(err))
				}
			}
		}
	}()

	// Start background request tracker cleanup
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			if tracking.GlobalTracker != nil {
				removed := tracking.GlobalTracker.Cleanup(30 * time.Minute)
				if removed > 0 {
					logger.Info("Cleaned up old tracked requests", zap.Int("removed", removed))
				}
			}
		}
	}()

	// Start server in goroutine
	go func() {
		logger.Info("Starting HTTP server", zap.String("address", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start HTTP server", zap.Error(err))
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop consumer workers first
	consumer.Stop()

	// Shutdown HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited gracefully")
}
