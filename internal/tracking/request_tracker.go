package tracking

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// RequestStatus represents the status of a request
type RequestStatus string

const (
	StatusReceived   RequestStatus = "received"
	StatusQueued     RequestStatus = "queued"
	StatusProcessing RequestStatus = "processing"
	StatusSending    RequestStatus = "sending"
	StatusRetrying   RequestStatus = "retrying"
	StatusSuccess    RequestStatus = "success"
	StatusFailed     RequestStatus = "failed"
)

// RequestInfo holds detailed information about a request
type RequestInfo struct {
	RequestID     string         `json:"request_id"`
	ReceivedAt    time.Time      `json:"received_at"`
	ClientIP      string         `json:"client_ip"`
	PayloadSize   int            `json:"payload_size"`
	Status        RequestStatus  `json:"status"`
	QueuedAt      *time.Time     `json:"queued_at,omitempty"`
	ProcessedAt   *time.Time     `json:"processed_at,omitempty"`
	CompletedAt   *time.Time     `json:"completed_at,omitempty"`
	TargetServer  string         `json:"target_server,omitempty"`
	RetryCount    int            `json:"retry_count"`
	LastError     string         `json:"last_error,omitempty"`
	Duration      time.Duration  `json:"duration"`
	StatusHistory []StatusChange `json:"status_history"`
}

// StatusChange represents a status change event
type StatusChange struct {
	Status    RequestStatus `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
	Message   string        `json:"message,omitempty"`
}

// RequestTracker tracks requests for monitoring with optional sampling
type RequestTracker struct {
	requests   map[string]*RequestInfo
	mu         sync.RWMutex
	maxSize    int
	sampleRate float64 // 0.0-1.0, percentage of requests to track
	enabled    bool

	// Lock-free statistics counters for high-throughput tracking
	totalReceived atomic.Int64
	totalQueued   atomic.Int64
	totalSuccess  atomic.Int64
	totalFailed   atomic.Int64
}

// NewRequestTracker creates a new request tracker with default settings
func NewRequestTracker(maxSize int) *RequestTracker {
	return NewRequestTrackerWithSampling(maxSize, 1.0, true)
}

// NewRequestTrackerWithSampling creates a request tracker with configurable sampling
func NewRequestTrackerWithSampling(maxSize int, sampleRate float64, enabled bool) *RequestTracker {
	if sampleRate < 0 {
		sampleRate = 0
	}
	if sampleRate > 1 {
		sampleRate = 1
	}
	return &RequestTracker{
		requests:   make(map[string]*RequestInfo),
		maxSize:    maxSize,
		sampleRate: sampleRate,
		enabled:    enabled,
	}
}

// shouldSample determines if this request should be tracked based on sample rate
func (rt *RequestTracker) shouldSample() bool {
	if !rt.enabled {
		return false
	}
	if rt.sampleRate >= 1.0 {
		return true
	}
	if rt.sampleRate <= 0 {
		return false
	}
	return rand.Float64() < rt.sampleRate
}

// IsEnabled returns whether tracking is enabled
func (rt *RequestTracker) IsEnabled() bool {
	return rt.enabled
}

// TrackRequest starts tracking a new request (respects sampling rate)
func (rt *RequestTracker) TrackRequest(requestID, clientIP string, payload []byte) {
	// Always increment counter (lock-free)
	rt.totalReceived.Add(1)

	// Check if we should sample this request
	if !rt.shouldSample() {
		return
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Limit size to prevent memory issues
	if len(rt.requests) >= rt.maxSize {
		rt.evictOldest()
	}

	now := time.Now()
	rt.requests[requestID] = &RequestInfo{
		RequestID:   requestID,
		ReceivedAt:  now,
		ClientIP:    clientIP,
		PayloadSize: len(payload),
		Status:      StatusReceived,
		StatusHistory: []StatusChange{
			{
				Status:    StatusReceived,
				Timestamp: now,
				Message:   "Request received",
			},
		},
	}
}

// UpdateStatus updates the status of a request
func (rt *RequestTracker) UpdateStatus(requestID string, status RequestStatus, message string) {
	// Update lock-free counters regardless of whether request is tracked
	switch status {
	case StatusQueued:
		rt.totalQueued.Add(1)
	case StatusSuccess:
		rt.totalSuccess.Add(1)
	case StatusFailed:
		rt.totalFailed.Add(1)
	}

	if !rt.enabled {
		return
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	req, exists := rt.requests[requestID]
	if !exists {
		return
	}

	now := time.Now()
	req.Status = status
	req.StatusHistory = append(req.StatusHistory, StatusChange{
		Status:    status,
		Timestamp: now,
		Message:   message,
	})

	switch status {
	case StatusQueued:
		req.QueuedAt = &now
	case StatusProcessing:
		req.ProcessedAt = &now
	case StatusSuccess, StatusFailed:
		req.CompletedAt = &now
		req.Duration = now.Sub(req.ReceivedAt)
	}
}

// UpdateTargetServer sets the target server for a request
func (rt *RequestTracker) UpdateTargetServer(requestID, server string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if req, exists := rt.requests[requestID]; exists {
		req.TargetServer = server
	}
}

// UpdateRetryCount increments the retry count
func (rt *RequestTracker) UpdateRetryCount(requestID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if req, exists := rt.requests[requestID]; exists {
		req.RetryCount++
	}
}

// UpdateError sets the last error for a request
func (rt *RequestTracker) UpdateError(requestID, errorMsg string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if req, exists := rt.requests[requestID]; exists {
		req.LastError = errorMsg
	}
}

// GetRequest retrieves a request by ID
func (rt *RequestTracker) GetRequest(requestID string) (*RequestInfo, bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	req, exists := rt.requests[requestID]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent external modification
	reqCopy := *req
	return &reqCopy, true
}

// GetAllRequests returns all tracked requests
func (rt *RequestTracker) GetAllRequests() []*RequestInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	requests := make([]*RequestInfo, 0, len(rt.requests))
	for _, req := range rt.requests {
		reqCopy := *req
		requests = append(requests, &reqCopy)
	}

	return requests
}

// GetRequestsByStatus returns requests filtered by status
func (rt *RequestTracker) GetRequestsByStatus(status RequestStatus) []*RequestInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	requests := make([]*RequestInfo, 0)
	for _, req := range rt.requests {
		if req.Status == status {
			reqCopy := *req
			requests = append(requests, &reqCopy)
		}
	}

	return requests
}

// GetStatistics returns tracking statistics
func (rt *RequestTracker) GetStatistics() map[string]interface{} {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	// Count by status
	statusCounts := make(map[RequestStatus]int)
	for _, req := range rt.requests {
		statusCounts[req.Status]++
	}

	return map[string]interface{}{
		"total_tracked":  len(rt.requests),
		"total_received": rt.totalReceived.Load(),
		"total_queued":   rt.totalQueued.Load(),
		"total_success":  rt.totalSuccess.Load(),
		"total_failed":   rt.totalFailed.Load(),
		"sample_rate":    rt.sampleRate,
		"enabled":        rt.enabled,
		"by_status":      statusCounts,
		"queued_waiting": statusCounts[StatusQueued],
		"processing":     statusCounts[StatusProcessing],
		"retrying":       statusCounts[StatusRetrying],
	}
}

// evictOldest removes the oldest completed request
func (rt *RequestTracker) evictOldest() {
	var oldestID string
	var oldestTime time.Time

	// Find oldest completed request
	for id, req := range rt.requests {
		if req.Status == StatusSuccess || req.Status == StatusFailed {
			if oldestID == "" || req.ReceivedAt.Before(oldestTime) {
				oldestID = id
				oldestTime = req.ReceivedAt
			}
		}
	}

	// If no completed requests, remove oldest in general
	if oldestID == "" {
		for id, req := range rt.requests {
			if oldestID == "" || req.ReceivedAt.Before(oldestTime) {
				oldestID = id
				oldestTime = req.ReceivedAt
			}
		}
	}

	if oldestID != "" {
		delete(rt.requests, oldestID)
	}
}

// Cleanup removes old completed requests
func (rt *RequestTracker) Cleanup(olderThan time.Duration) int {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for id, req := range rt.requests {
		if (req.Status == StatusSuccess || req.Status == StatusFailed) && req.ReceivedAt.Before(cutoff) {
			delete(rt.requests, id)
			removed++
		}
	}

	return removed
}

// Global request tracker instance
var GlobalTracker *RequestTracker

// InitGlobalTracker initializes the global request tracker with default sampling
func InitGlobalTracker(maxSize int) {
	GlobalTracker = NewRequestTracker(maxSize)
}

// InitGlobalTrackerWithConfig initializes the global request tracker with config
func InitGlobalTrackerWithConfig(maxSize int, sampleRate float64, enabled bool) {
	GlobalTracker = NewRequestTrackerWithSampling(maxSize, sampleRate, enabled)
}
