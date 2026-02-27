package api

import (
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/internal/queue"
	"github.com/gps-data-receiver/internal/tracking"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// BroadcastEmitter can broadcast events to connected clients (e.g. Socket.IO). May be nil.
type BroadcastEmitter interface {
	Emit(event string, args ...interface{}) error
}

// broadcastMessage represents a message to be broadcast asynchronously
type broadcastMessage struct {
	event   string
	payload interface{}
}

// AsyncBroadcaster wraps a BroadcastEmitter with an async channel for non-blocking broadcasts
type AsyncBroadcaster struct {
	emitter BroadcastEmitter
	ch      chan broadcastMessage
	done    chan struct{}
}

// NewAsyncBroadcaster creates a new async broadcaster with the specified buffer size
func NewAsyncBroadcaster(emitter BroadcastEmitter, bufferSize int) *AsyncBroadcaster {
	if emitter == nil {
		return nil
	}
	ab := &AsyncBroadcaster{
		emitter: emitter,
		ch:      make(chan broadcastMessage, bufferSize),
		done:    make(chan struct{}),
	}
	go ab.worker()
	return ab
}

// Emit queues a message for async broadcast (non-blocking, drops if buffer full)
func (ab *AsyncBroadcaster) Emit(event string, args ...interface{}) error {
	if len(args) == 0 {
		return nil
	}
	select {
	case ab.ch <- broadcastMessage{event: event, payload: args[0]}:
	default:
		// Buffer full, drop message (non-blocking for high throughput)
	}
	return nil
}

// worker processes broadcast messages from the channel
func (ab *AsyncBroadcaster) worker() {
	for {
		select {
		case <-ab.done:
			return
		case msg := <-ab.ch:
			if err := ab.emitter.Emit(msg.event, msg.payload); err != nil {
				logger.Debug("Async broadcast failed",
					zap.String("event", msg.event),
					zap.Error(err))
			}
		}
	}
}

// Close stops the async broadcaster
func (ab *AsyncBroadcaster) Close() {
	close(ab.done)
}

// Handler handles HTTP requests
type Handler struct {
	queue             *queue.RedisQueue
	backpressureLimit int64
	broadcast         BroadcastEmitter

	// Cached depth so we don't need a separate XLEN call on every request;
	// updated atomically after each EnqueueWithDepth pipeline call.
	cachedDepth atomic.Int64
}

// NewHandler creates a new handler. backpressureLimit is the queue depth at which to return 503 (0 = 90% of Redis MaxLen).
// broadcast is optional; when non-nil, received GPS packets are broadcast (e.g. for real-time frontend).
func NewHandler(q *queue.RedisQueue, backpressureLimit int64, broadcast BroadcastEmitter) *Handler {
	h := &Handler{
		queue:             q,
		backpressureLimit: backpressureLimit,
		broadcast:         broadcast,
	}
	return h
}

// ReceiveGPSData handles POST /api/gps/reports
func (h *Handler) ReceiveGPSData(c *gin.Context) {
	clientIP := c.ClientIP()
	requestID := c.GetHeader("X-Request-ID")

	body, err := readBody(c)
	if err != nil {
		return
	}

	if tracking.GlobalTracker != nil && requestID != "" {
		tracking.GlobalTracker.TrackRequest(requestID, clientIP, body)
	}

	if len(body) == 0 {
		logger.Warn("Empty request body")
		if tracking.GlobalTracker != nil && requestID != "" {
			tracking.GlobalTracker.UpdateStatus(requestID, tracking.StatusFailed, "Empty request body")
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Request body is empty"})
		return
	}

	limit := h.backpressureLimit
	if limit <= 0 {
		limit = (h.queue.GetMaxLen() * 9) / 10
	}

	if limit > 0 && h.cachedDepth.Load() >= limit {
		logger.Warn("Queue backpressure: rejecting request",
			zap.Int64("queue_depth", h.cachedDepth.Load()),
			zap.Int64("limit", limit))
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordQueueEnqueue(false)
		}
		if tracking.GlobalTracker != nil && requestID != "" {
			tracking.GlobalTracker.UpdateStatus(requestID, tracking.StatusFailed, "Queue full (backpressure)")
		}
		c.Header("Retry-After", strconv.Itoa(5))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Queue is full, try again later"})
		return
	}

	messageID, depth, err := h.queue.EnqueueWithDepth(c.Request.Context(), body)
	if err != nil {
		logger.Error("Failed to enqueue message", zap.Error(err))
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordQueueEnqueue(false)
		}
		if tracking.GlobalTracker != nil && requestID != "" {
			tracking.GlobalTracker.UpdateStatus(requestID, tracking.StatusFailed, "Failed to enqueue")
			tracking.GlobalTracker.UpdateError(requestID, err.Error())
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Failed to queue message"})
		return
	}

	h.cachedDepth.Store(depth)

	if metrics.AppMetrics != nil {
		metrics.AppMetrics.RecordQueueEnqueue(true)
	}

	if tracking.GlobalTracker != nil && requestID != "" {
		tracking.GlobalTracker.UpdateStatus(requestID, tracking.StatusQueued, "Enqueued successfully")
	}

	logger.Debug("GPS data received and queued",
		zap.String("message_id", messageID),
		zap.Int("payload_size", len(body)))

	if h.broadcast != nil {
		payload := map[string]interface{}{
			"message_id":   messageID,
			"received_at":  time.Now().UTC().Format(time.RFC3339),
			"payload":      string(body),
			"payload_size": len(body),
		}
		if err := h.broadcast.Emit("gps-packet", payload); err != nil {
			logger.Debug("Broadcast gps-packet failed", zap.Error(err))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "queued",
		"message_id": messageID,
	})
}

// readBody reads and returns the request body, sending an error response if it fails.
func readBody(c *gin.Context) ([]byte, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Error("Failed to read request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return nil, err
	}
	return body, nil
}

// Health handles GET /health
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "gps-data-receiver",
	})
}

// Ready handles GET /ready
func (h *Handler) Ready(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}

// GetRequestDetails handles GET /monitoring/requests/:id
func (h *Handler) GetRequestDetails(c *gin.Context) {
	requestID := c.Param("id")

	if tracking.GlobalTracker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Request tracking not enabled"})
		return
	}

	req, exists := tracking.GlobalTracker.GetRequest(requestID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Request not found"})
		return
	}

	c.JSON(http.StatusOK, req)
}

// ListRequests handles GET /monitoring/requests
func (h *Handler) ListRequests(c *gin.Context) {
	if tracking.GlobalTracker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Request tracking not enabled"})
		return
	}

	status := c.Query("status")

	var requests []*tracking.RequestInfo
	if status != "" {
		requests = tracking.GlobalTracker.GetRequestsByStatus(tracking.RequestStatus(status))
	} else {
		requests = tracking.GlobalTracker.GetAllRequests()
	}

	c.JSON(http.StatusOK, gin.H{
		"requests": requests,
		"count":    len(requests),
	})
}

// GetStatistics handles GET /monitoring/statistics
func (h *Handler) GetStatistics(c *gin.Context) {
	if tracking.GlobalTracker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Request tracking not enabled"})
		return
	}

	stats := tracking.GlobalTracker.GetStatistics()
	c.JSON(http.StatusOK, stats)
}

// GetCachedDepth returns the last-known queue depth. Used by main for metrics without extra Redis calls.
func (h *Handler) GetCachedDepth() int64 {
	return h.cachedDepth.Load()
}
