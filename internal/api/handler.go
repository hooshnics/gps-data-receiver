package api

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/internal/queue"
	"github.com/gps-data-receiver/internal/tracking"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// Handler handles HTTP requests
type Handler struct {
	queue *queue.RedisQueue
}

// NewHandler creates a new handler
func NewHandler(q *queue.RedisQueue) *Handler {
	return &Handler{
		queue: q,
	}
}

// ReceiveGPSData handles POST /api/gps/reports
func (h *Handler) ReceiveGPSData(c *gin.Context) {
	clientIP := c.ClientIP()
	requestID := c.GetHeader("X-Request-ID")

	// Read raw body without parsing or validation
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Error("Failed to read request body",
			zap.Error(err))

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to read request body",
		})
		return
	}

	// Track request
	if tracking.GlobalTracker != nil && requestID != "" {
		tracking.GlobalTracker.TrackRequest(requestID, clientIP, body)
	}

	// Check if body is empty
	if len(body) == 0 {
		logger.Warn("Empty request body")

		if tracking.GlobalTracker != nil && requestID != "" {
			tracking.GlobalTracker.UpdateStatus(requestID, tracking.StatusFailed, "Empty request body")
		}

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Request body is empty",
		})
		return
	}

	// Enqueue to Redis (non-blocking)
	messageID, err := h.queue.Enqueue(c.Request.Context(), body)
	if err != nil {
		logger.Error("Failed to enqueue message",
			zap.Error(err))

		// Record metrics
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordQueueEnqueue(false)
		}

		if tracking.GlobalTracker != nil && requestID != "" {
			tracking.GlobalTracker.UpdateStatus(requestID, tracking.StatusFailed, "Failed to enqueue")
			tracking.GlobalTracker.UpdateError(requestID, err.Error())
		}

		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Failed to queue message",
		})
		return
	}

	// Record successful enqueue
	if metrics.AppMetrics != nil {
		metrics.AppMetrics.RecordQueueEnqueue(true)
	}

	if tracking.GlobalTracker != nil && requestID != "" {
		tracking.GlobalTracker.UpdateStatus(requestID, tracking.StatusQueued, "Enqueued successfully")
	}

	// Return success immediately
	logger.Info("GPS data received and queued",
		zap.String("message_id", messageID),
		zap.Int("payload_size", len(body)))

	c.JSON(http.StatusOK, gin.H{
		"status":     "queued",
		"message_id": messageID,
	})
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
	// Could add checks for Redis, MySQL connections here
	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}

// GetRequestDetails handles GET /monitoring/requests/:id
func (h *Handler) GetRequestDetails(c *gin.Context) {
	requestID := c.Param("id")

	if tracking.GlobalTracker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Request tracking not enabled",
		})
		return
	}

	req, exists := tracking.GlobalTracker.GetRequest(requestID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Request not found",
		})
		return
	}

	c.JSON(http.StatusOK, req)
}

// ListRequests handles GET /monitoring/requests
func (h *Handler) ListRequests(c *gin.Context) {
	if tracking.GlobalTracker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Request tracking not enabled",
		})
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
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Request tracking not enabled",
		})
		return
	}

	stats := tracking.GlobalTracker.GetStatistics()
	c.JSON(http.StatusOK, stats)
}
