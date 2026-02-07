package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// HTTP Metrics
	HTTPRequestsTotal       *prometheus.CounterVec
	HTTPRequestDuration     *prometheus.HistogramVec
	HTTPRequestSize         *prometheus.HistogramVec
	HTTPResponseSize        *prometheus.HistogramVec
	HTTPActiveRequests      prometheus.Gauge
	
	// Queue Metrics
	QueueDepth              prometheus.Gauge
	QueueEnqueueTotal       prometheus.Counter
	QueueEnqueueErrors      prometheus.Counter
	QueueProcessedTotal     prometheus.Counter
	QueueProcessingDuration prometheus.Histogram
	
	// Sender Metrics
	SenderRequestsTotal     *prometheus.CounterVec
	SenderRequestDuration   *prometheus.HistogramVec
	SenderRetryTotal        *prometheus.CounterVec
	SenderFailedTotal       *prometheus.CounterVec
	
	// Failed Packets Metrics
	FailedPacketsTotal      prometheus.Counter
	FailedPacketsStored     prometheus.Counter
	FailedPacketsInDB       prometheus.Gauge
	
	// Worker Metrics
	WorkerPoolSize          prometheus.Gauge
	WorkerActiveCount       prometheus.Gauge
	WorkerIdleCount         prometheus.Gauge
	
	// Rate Limiting Metrics
	RateLimitHits           *prometheus.CounterVec
	
	// System Metrics (Go runtime)
	// These are automatically collected by Prometheus Go client
}

var AppMetrics *Metrics

// InitMetrics initializes all Prometheus metrics
func InitMetrics() *Metrics {
	m := &Metrics{
		// HTTP Metrics
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gps_receiver_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gps_receiver_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "endpoint", "status"},
		),
		HTTPRequestSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gps_receiver_http_request_size_bytes",
				Help:    "HTTP request size in bytes",
				Buckets: []float64{100, 1000, 10000, 100000, 1000000},
			},
			[]string{"method", "endpoint"},
		),
		HTTPResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gps_receiver_http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: []float64{100, 1000, 10000, 100000},
			},
			[]string{"method", "endpoint"},
		),
		HTTPActiveRequests: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_receiver_http_active_requests",
				Help: "Number of active HTTP requests",
			},
		),
		
		// Queue Metrics
		QueueDepth: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_receiver_queue_depth",
				Help: "Current number of messages in the queue",
			},
		),
		QueueEnqueueTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_receiver_queue_enqueue_total",
				Help: "Total number of messages enqueued",
			},
		),
		QueueEnqueueErrors: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_receiver_queue_enqueue_errors_total",
				Help: "Total number of enqueue errors",
			},
		),
		QueueProcessedTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_receiver_queue_processed_total",
				Help: "Total number of messages processed from queue",
			},
		),
		QueueProcessingDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "gps_receiver_queue_processing_duration_seconds",
				Help:    "Time taken to process a message from queue",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
		),
		
		// Sender Metrics
		SenderRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gps_receiver_sender_requests_total",
				Help: "Total number of requests sent to destination servers",
			},
			[]string{"server", "status"},
		),
		SenderRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gps_receiver_sender_request_duration_seconds",
				Help:    "Duration of requests to destination servers",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
			},
			[]string{"server"},
		),
		SenderRetryTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gps_receiver_sender_retry_total",
				Help: "Total number of retry attempts",
			},
			[]string{"server", "attempt"},
		),
		SenderFailedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gps_receiver_sender_failed_total",
				Help: "Total number of permanently failed sends",
			},
			[]string{"server"},
		),
		
		// Failed Packets Metrics
		FailedPacketsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_receiver_failed_packets_total",
				Help: "Total number of packets that failed after all retries",
			},
		),
		FailedPacketsStored: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_receiver_failed_packets_stored_total",
				Help: "Total number of failed packets stored in database",
			},
		),
		FailedPacketsInDB: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_receiver_failed_packets_in_db",
				Help: "Current number of failed packets in database",
			},
		),
		
		// Worker Metrics
		WorkerPoolSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_receiver_worker_pool_size",
				Help: "Total number of workers in the pool",
			},
		),
		WorkerActiveCount: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_receiver_worker_active_count",
				Help: "Number of currently active workers",
			},
		),
		WorkerIdleCount: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_receiver_worker_idle_count",
				Help: "Number of currently idle workers",
			},
		),
		
		// Rate Limiting Metrics
		RateLimitHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gps_receiver_rate_limit_hits_total",
				Help: "Total number of rate limit hits",
			},
			[]string{"client_ip"},
		),
	}
	
	AppMetrics = m
	return m
}

// RecordHTTPRequest records an HTTP request metric
func (m *Metrics) RecordHTTPRequest(method, endpoint, status string, duration time.Duration, requestSize, responseSize int) {
	m.HTTPRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, endpoint, status).Observe(duration.Seconds())
	m.HTTPRequestSize.WithLabelValues(method, endpoint).Observe(float64(requestSize))
	m.HTTPResponseSize.WithLabelValues(method, endpoint).Observe(float64(responseSize))
}

// RecordQueueEnqueue records a queue enqueue operation
func (m *Metrics) RecordQueueEnqueue(success bool) {
	m.QueueEnqueueTotal.Inc()
	if !success {
		m.QueueEnqueueErrors.Inc()
	}
}

// RecordQueueProcessing records queue processing metrics
func (m *Metrics) RecordQueueProcessing(duration time.Duration) {
	m.QueueProcessedTotal.Inc()
	m.QueueProcessingDuration.Observe(duration.Seconds())
}

// RecordSenderRequest records a sender request
func (m *Metrics) RecordSenderRequest(server, status string, duration time.Duration) {
	m.SenderRequestsTotal.WithLabelValues(server, status).Inc()
	m.SenderRequestDuration.WithLabelValues(server).Observe(duration.Seconds())
}

// RecordSenderRetry records a retry attempt
func (m *Metrics) RecordSenderRetry(server string, attempt int) {
	m.SenderRetryTotal.WithLabelValues(server, string(rune(attempt+'0'))).Inc()
}

// RecordSenderFailure records a permanent failure
func (m *Metrics) RecordSenderFailure(server string) {
	m.SenderFailedTotal.WithLabelValues(server).Inc()
	m.FailedPacketsTotal.Inc()
}

// RecordFailedPacketStored records a failed packet being stored
func (m *Metrics) RecordFailedPacketStored() {
	m.FailedPacketsStored.Inc()
}

// RecordRateLimitHit records a rate limit hit
func (m *Metrics) RecordRateLimitHit(clientIP string) {
	m.RateLimitHits.WithLabelValues(clientIP).Inc()
}

// UpdateQueueDepth updates the queue depth gauge
func (m *Metrics) UpdateQueueDepth(depth int64) {
	m.QueueDepth.Set(float64(depth))
}

// UpdateFailedPacketsInDB updates the failed packets in DB gauge
func (m *Metrics) UpdateFailedPacketsInDB(count int64) {
	m.FailedPacketsInDB.Set(float64(count))
}

// SetWorkerPoolSize updates worker pool size metric
func (m *Metrics) SetWorkerPoolSize(poolSize int) {
	m.WorkerPoolSize.Set(float64(poolSize))
}

// InitWorkerCounts initializes worker pool metrics
func (m *Metrics) InitWorkerCounts(poolSize int) {
	m.WorkerPoolSize.Set(float64(poolSize))
	m.WorkerIdleCount.Set(float64(poolSize))
}

// IncActiveWorker increments active worker count and decrements idle count
func (m *Metrics) IncActiveWorker() {
	m.WorkerActiveCount.Inc()
	m.WorkerIdleCount.Dec()
}

// DecActiveWorker decrements active worker count and increments idle count
func (m *Metrics) DecActiveWorker() {
	m.WorkerActiveCount.Dec()
	m.WorkerIdleCount.Inc()
}

