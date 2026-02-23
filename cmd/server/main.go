package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/api"
	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/internal/queue"
	"github.com/gps-data-receiver/internal/sender"
	"github.com/gps-data-receiver/internal/tracking"
	"github.com/gps-data-receiver/pkg/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

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

	// Create message handler that sends data to destination servers
	messageHandler := func(ctx context.Context, data []byte) error {
		start := time.Now()

		result := httpSender.Send(ctx, data)

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

	// Initialize handler (with backpressure limit from config; 0 = use 90% of Redis MaxLen)
	handler := api.NewHandler(redisQueue, cfg.Redis.QueueBackpressureLimit)

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
