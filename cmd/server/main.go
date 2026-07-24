package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	gojson "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/gps-data-receiver/internal/api"
	archivelib "github.com/gps-data-receiver/internal/archive"
	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/device"
	"github.com/gps-data-receiver/internal/filter"
	"github.com/gps-data-receiver/internal/hooshnics"
	"github.com/gps-data-receiver/internal/ingest"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/internal/queue"
	"github.com/gps-data-receiver/internal/sanitizer"
	"github.com/gps-data-receiver/internal/sender"
	"github.com/gps-data-receiver/internal/storage"
	teltonikacodec "github.com/gps-data-receiver/internal/teltonika/codec"
	teltonikatcp "github.com/gps-data-receiver/internal/teltonika/tcp"
	"github.com/gps-data-receiver/internal/tracking"
	"github.com/gps-data-receiver/pkg/logger"
	socketio "github.com/ismhdez/socket.io-golang/v4"
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

	if strings.EqualFold(os.Getenv("ENVIRONMENT"), "production") {
		if cfg.Redis.Password == "" {
			logger.Warn("SECURITY: REDIS_PASSWORD is empty in production — Redis must not be reachable outside the trusted network")
		}
		if cfg.Teltonika.TCPEnabled && len(cfg.Teltonika.IMEIWhitelist) == 0 {
			logger.Warn("SECURITY: TELTONIKA_IMEI_WHITELIST is empty — TCP :5055 accepts any IMEI; restrict via firewall and/or whitelist")
		}
		if cfg.Postgres.Enabled && (cfg.Postgres.Password == "" || cfg.Postgres.Password == "gps") {
			logger.Warn("SECURITY: PostgreSQL is using a default/empty password — rotate credentials for production")
		}
	}

	// Initialize metrics
	metrics.InitMetrics()
	logger.Info("Prometheus metrics initialized")

	// Initialize request tracker with sampling support for high throughput
	tracking.InitGlobalTrackerWithConfig(
		cfg.Tracking.MaxRequests,
		cfg.Tracking.SampleRate,
		cfg.Tracking.Enabled,
	)
	logger.Info("Request tracking initialized",
		zap.Bool("enabled", cfg.Tracking.Enabled),
		zap.Float64("sample_rate", cfg.Tracking.SampleRate),
		zap.Int("max_requests", cfg.Tracking.MaxRequests))

	// Initialize Redis queue
	redisQueue, err := queue.NewRedisQueue(&cfg.Redis)
	if err != nil {
		logger.Fatal("Failed to initialize Redis queue", zap.Error(err))
	}
	defer redisQueue.Close()

	// Initialize HTTP sender
	httpSender := sender.NewHTTPSender(cfg)
	defer httpSender.Close()
	logger.Info("Outgoing rate limit configured",
		zap.Int("rps_per_destination", cfg.OutgoingRateLimit.RequestsPerSecond),
		zap.Int("burst_per_destination", cfg.OutgoingRateLimit.BurstSize))

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

	// Hooshnics async mirror — isolated from PiStat DESTINATION_SERVERS path.
	hooshnicsForwarder, err := hooshnics.NewForwarder(hooshnics.Config{
		Enabled:    cfg.Hooshnics.Enabled,
		AuthToken:  cfg.Hooshnics.AuthToken,
		RawURL:     cfg.Hooshnics.RawURL,
		ParsedURL:  cfg.Hooshnics.ParsedURL,
		BufferSize: cfg.Hooshnics.BufferSize,
		SpillDir:   cfg.Hooshnics.SpillDir,
		Timeout:    cfg.Hooshnics.Timeout,
		Workers:    cfg.Hooshnics.Workers,
	})
	if err != nil {
		logger.Fatal("Failed to initialize Hooshnics mirror", zap.Error(err))
	}
	if hooshnicsForwarder != nil {
		hooshnicsForwarder.SetRetrySink(redisQueue)
		hooshnicsForwarder.Start()
		defer hooshnicsForwarder.Close()
	} else {
		logger.Info("Hooshnics mirror disabled")
	}

	// Socket.IO for real-time delivery status broadcast. Created early so messageHandler can emit delivery events.
	io := socketio.New()

	// Wrap Socket.IO with async broadcaster for non-blocking high-throughput broadcasts
	asyncBroadcaster := api.NewAsyncBroadcaster(io, 10000)
	if asyncBroadcaster != nil {
		defer asyncBroadcaster.Close()
	}

	var postgresStore *storage.PostgresStore
	var postgresWriter *storage.AsyncWriter
	if cfg.Postgres.Enabled {
		store, err := storage.NewPostgresStore(cfg.Postgres.DSN(), cfg.Worker.Count)
		if err != nil {
			logger.Fatal("Failed to initialize PostgreSQL storage", zap.Error(err))
		}
		postgresStore = store
		postgresWriter = storage.NewAsyncWriter(store, 0)
		defer postgresWriter.Close()
		defer postgresStore.Close()
		logger.Info("PostgreSQL storage enabled",
			zap.String("host", cfg.Postgres.Host),
			zap.String("db", cfg.Postgres.DBName),
			zap.Int("max_open_conns", cfg.Worker.Count))
	}

	// Create message handler that sends data to destination servers and broadcasts on success
	messageHandler := func(ctx context.Context, data []byte) error {
		start := time.Now()

		// 1. Parse GPS data (skip UTF-8 sanitize for binary Teltonika payloads)
		parseStart := time.Now()
		var parseResult parser.ParseResult
		var err error
		switch teltonikacodec.DetectFormat(data) {
		case teltonikacodec.FormatTeltonikaQueued, teltonikacodec.FormatTeltonikaAVL:
			parseResult, err = parser.ParseWithIMEI(data, "", cfg.Teltonika.TimezoneOffset)
		default:
			parseResult, err = parser.ParseWithIMEI(sanitizer.SanitizeUTF8(data), "", cfg.Teltonika.TimezoneOffset)
		}
		parseDuration := time.Since(parseStart)

		enqueueInvalid := func(records []parser.InvalidRecord) {
			if postgresWriter != nil && len(records) > 0 {
				postgresWriter.EnqueueInvalid(records)
			}
		}
		storeFullInvalidPayload := func(reason string) {
			enqueueInvalid([]parser.InvalidRecord{{
				RawData: string(data),
				Reason:  reason,
			}})
		}

		if err != nil {
			logger.Error("Failed to parse GPS data - dropping message",
				zap.Error(err),
				zap.Int("payload_bytes", len(data)))
			storeFullInvalidPayload(err.Error())
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordParseFailure()
				metrics.AppMetrics.RecordDataDropped("parse_error")
			}
			return nil // Return nil to ACK and drop the message (no retry)
		}

		if len(parseResult.Invalid) > 0 {
			enqueueInvalid(parseResult.Invalid)
		}

		parsed := parseResult.Records

		// Handle empty parse results
		if len(parsed) == 0 {
			logger.Warn("No valid GPS records found - dropping message",
				zap.Int("payload_bytes", len(data)))
			if len(parseResult.Invalid) == 0 {
				storeFullInvalidPayload("no valid GPS records found")
			}
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

		// 3. No time filtering - pass all parsed data to stoppage filter
		timeFilteredData := parsed

		// 4. Filter redundant stoppage data
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

		// 5. Wrap filtered data in "data" field and re-encode as JSON for destination
		// Using goccy/go-json for faster marshaling
		wrappedData := map[string]interface{}{
			"data": filteredData,
		}
		jsonData, err := gojson.Marshal(wrappedData)
		if err != nil {
			logger.Error("Failed to marshal parsed data - dropping message",
				zap.Error(err))
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordDataDropped("marshal_error")
			}
			return nil // Return nil to ACK and drop the message
		}

		// Optional async parsed mirror — does not block or alter PiStat send below.
		if hooshnicsForwarder != nil {
			hooshnicsForwarder.ForwardParsed(jsonData)
		}

		// 6. Send filtered data to destination servers
		deliveryID := uuid.New().String()
		emitDelivery := func(status, targetServer string) {
			if asyncBroadcaster == nil {
				return
			}
			asyncBroadcaster.Emit("gps-delivery", map[string]interface{}{
				"delivery_id":   deliveryID,
				"status":        status,
				"target_server": targetServer,
				"payload":       string(jsonData),
				"payload_size":  len(jsonData),
				"record_count":  len(filteredData),
				"updated_at":    time.Now().UTC().Format(time.RFC3339),
			})
		}

		emitDelivery("sending", "")

		sendObserver := sender.FuncSendObserver{
			OnAttemptFunc: func(server string, attempt int) {
				emitDelivery("sending", server)
			},
		}
		result := httpSender.Send(ctx, jsonData, sendObserver)

		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordQueueProcessing(time.Since(start))
		}

		if !result.Success {
			emitDelivery("failed", result.TargetServer)
			if postgresWriter != nil {
				errMsg := ""
				if result.Error != nil {
					errMsg = result.Error.Error()
				}
				postgresWriter.EnqueueFailed(filteredData, result.TargetServer, errMsg)
			}
			// Zero-loss: park on retry stream instead of dropping or blocking forever.
			if parkErr := redisQueue.EnqueuePistatRetry(ctx, jsonData, result.Attempt); parkErr != nil {
				logger.Warn("PiStat retry park failed — leaving message for queue redelivery",
					zap.String("target_server", result.TargetServer),
					zap.Error(parkErr),
					zap.Error(result.Error))
				return result.Error
			}
			logger.Warn("PiStat send exhausted — payload moved to gps:pistat_retry",
				zap.String("target_server", result.TargetServer),
				zap.Int("attempts", result.Attempt),
				zap.Error(result.Error))
			return nil
		}

		emitDelivery("delivered", result.TargetServer)

		if postgresWriter != nil {
			postgresWriter.EnqueueSuccess(filteredData)
		}

		return nil
	}

	// Initialize consumer with worker pool (HTTP / legacy gps:reports path).
	consumer := queue.NewConsumer(
		redisQueue,
		cfg.Worker.Count,
		cfg.Worker.BatchSize,
		cfg.Retry.MaxAttempts,
		messageHandler,
	)
	consumer.Start()
	logger.Info("Worker pool started (gps:reports)", zap.Int("workers", cfg.Worker.Count))

	// Durable Teltonika raw ingest consumers (gps:raw_incoming).
	var emitAdapter ingest.DeliveryEmitter
	if asyncBroadcaster != nil {
		emitAdapter = ingest.BroadcasterAdapter{EmitFn: asyncBroadcaster.Emit}
	}
	rawProcessor := &ingest.Processor{
		Queue:      redisQueue,
		Mirror:     hooshnicsForwarder,
		Sender:     httpSender,
		Filter:     stoppageFilter,
		Postgres:   postgresWriter,
		Emitter:    emitAdapter,
		TZOffset:   cfg.Teltonika.TimezoneOffset,
		Drivers:    device.DefaultRegistry(cfg.Teltonika.TimezoneOffset),
		ForwardRaw: cfg.Hooshnics.ForwardRaw,
	}
	logger.Info("Hoosh IoT Gateway raw processor ready",
		zap.Bool("hooshnics_forward_raw", cfg.Hooshnics.ForwardRaw),
		zap.Bool("hooshnics_mirror", hooshnicsForwarder != nil))
	rawConsumer := queue.NewRawConsumer(
		redisQueue,
		cfg.Worker.Count,
		cfg.Worker.BatchSize,
		rawProcessor.ProcessRawAVL,
	)
	if cfg.Archive.Enabled {
		aw, err := archivelib.NewWriter(cfg.Archive.Dir, cfg.Archive.Strict)
		if err != nil {
			logger.Fatal("Failed to initialize raw archive writer", zap.Error(err))
		}
		rawConsumer.SetArchiver(aw)
		defer aw.Close()
		logger.Info("Raw AVL archive enabled",
			zap.String("dir", cfg.Archive.Dir),
			zap.Bool("strict", cfg.Archive.Strict))
	}
	rawConsumer.Start()
	defer rawConsumer.Stop()
	logger.Info("Raw ingest worker pool started (gps:raw_incoming)", zap.Int("workers", cfg.Worker.Count))

	retryConsumers := queue.NewRetryConsumers(redisQueue, httpSender, nil, cfg.Hooshnics.AuthToken)
	retryConsumers.Start()
	defer retryConsumers.Stop()

	// Lightweight durable-stream depth / pending gauges for Prometheus.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			raw, pistat, hoosh, dlq, err := redisQueue.StreamDepths(ctx)
			pending, perr := redisQueue.RawPendingCount(ctx)
			cancel()
			if err == nil && perr == nil && metrics.AppMetrics != nil {
				metrics.AppMetrics.UpdateDurableStreamDepths(raw, pending, pistat, hoosh, dlq)
			}
			if postgresStore != nil && metrics.AppMetrics != nil {
				st := postgresStore.PoolStats()
				metrics.AppMetrics.UpdatePostgresPool(st.OpenConnections, st.InUse, st.MaxOpenConnections)
			}
		}
	}()

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
	handler := api.NewHandler(redisQueue, cfg.Redis.QueueBackpressureLimit, postgresStore, cfg.Teltonika.TimezoneOffset, hooshnicsForwarder)

	// Teltonika TCP: durable Redis XADD before ACK (zero-loss).
	teltonikaServer := teltonikatcp.NewServer(cfg.Teltonika, redisQueue)
	if teltonikaServer != nil {
		if err := teltonikaServer.Start(context.Background()); err != nil {
			logger.Fatal("Failed to start Teltonika TCP server", zap.Error(err))
		}
		defer teltonikaServer.Stop()
	}

	// Setup routes
	router.POST("/api/gps/reports", handler.ReceiveGPSData)
	router.GET("/api/gps/records", handler.QueryGPSRecords)
	router.GET("/api/gps/path", handler.QueryGPSPath)
	router.GET("/api/gps/failed-records", handler.QueryFailedGPSRecords)
	router.GET("/api/gps/invalid-records", handler.QueryInvalidGPSRecords)
	router.GET("/health", handler.Health)
	router.HEAD("/health", handler.Health)
	router.GET("/ready", handler.Ready)
	router.HEAD("/ready", handler.Ready)

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

	// Create HTTP server with optimized settings for high throughput
	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadTimeout:       cfg.Server.RequestTimeout,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      cfg.Server.RequestTimeout + 5*time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64KB (reduced for GPS data which has small headers)
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

	// Ordered graceful shutdown (zero-loss):
	// 1) stop accepting new Teltonika TCP (defer Stop already registered — call explicitly first)
	// 2) stop raw / reports / retry consumers after in-flight finish
	// 3) stop HTTP
	if teltonikaServer != nil {
		teltonikaServer.Stop()
	}
	rawConsumer.Stop()
	consumer.Stop()
	retryConsumers.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited gracefully")
}
