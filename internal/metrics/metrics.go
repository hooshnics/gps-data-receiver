package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// HTTP Metrics
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPRequestSize     *prometheus.HistogramVec
	HTTPResponseSize    *prometheus.HistogramVec
	HTTPActiveRequests  prometheus.Gauge

	// Queue Metrics
	QueueDepth              prometheus.Gauge
	QueueEnqueueTotal       prometheus.Counter
	QueueEnqueueErrors      prometheus.Counter
	QueueProcessedTotal     prometheus.Counter
	QueueProcessingDuration prometheus.Histogram

	// Sender Metrics
	SenderRequestsTotal   *prometheus.CounterVec
	SenderRequestDuration *prometheus.HistogramVec
	SenderRetryTotal      *prometheus.CounterVec
	SenderFailedTotal     *prometheus.CounterVec

	// Failed Packets Metrics
	FailedPacketsTotal prometheus.Counter

	// Parser Metrics
	ParseTotal         *prometheus.CounterVec
	ParseDuration      prometheus.Histogram
	RecordsParsedTotal prometheus.Counter
	DataDroppedTotal   *prometheus.CounterVec

	// Worker Metrics
	WorkerPoolSize    prometheus.Gauge
	WorkerActiveCount prometheus.Gauge
	WorkerIdleCount   prometheus.Gauge

	// Rate Limiting Metrics
	RateLimitHits *prometheus.CounterVec

	// Filter Metrics
	StoppageRecordsFilteredTotal prometheus.Counter
	FilterStateCount             prometheus.Gauge

	// Teltonika zero-drop path
	TeltonikaPacketsReceived   prometheus.Counter
	TeltonikaPacketsAcked      prometheus.Counter
	TeltonikaCRCFailures       prometheus.Counter
	TeltonikaIngestErrors      prometheus.Counter
	TeltonikaRedisWriteLatency prometheus.Histogram
	TeltonikaDuplicates        prometheus.Counter
	TeltonikaDecodeFailures    prometheus.Counter
	TeltonikaDeadLetterTotal   prometheus.Counter
	RawStreamDepth             prometheus.Gauge
	RawPendingCount            prometheus.Gauge
	PistatRetryDepth           prometheus.Gauge
	HooshnicsRetryDepth        prometheus.Gauge
	DeadLetterDepth            prometheus.Gauge
	ParserLatency              prometheus.Histogram
	TeltonikaActiveConnections prometheus.Gauge
	PostgresOpenConnections    prometheus.Gauge
	PostgresInUseConnections   prometheus.Gauge
	PostgresMaxOpenConnections prometheus.Gauge

	// External integration retry / circuit
	IntegrationRetryRequeued  *prometheus.CounterVec
	IntegrationRetryExhausted *prometheus.CounterVec
	HooshnicsCircuitOpenTotal prometheus.Counter

	// Hoosh IoT Gateway canonical names (Phase 1.1) — complement legacy gps_* series.
	ReceivedPacketsTotal   prometheus.Counter
	ParsedPacketsTotal     prometheus.Counter
	ParseErrorsTotal       prometheus.Counter
	RedisStreamLag         prometheus.Gauge
	RawStorageFailures     prometheus.Counter
	ForwardingFailures     *prometheus.CounterVec
	PostgresAsyncDrops     prometheus.Counter
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

		// Parser Metrics
		ParseTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gps_data_parse_total",
				Help: "Total number of parse attempts",
			},
			[]string{"status"}, // success or failure
		),
		ParseDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "gps_data_parse_duration_seconds",
				Help:    "Duration of GPS data parsing operations",
				Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
			},
		),
		RecordsParsedTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_records_parsed_total",
				Help: "Total number of individual GPS records successfully parsed",
			},
		),
		DataDroppedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gps_data_dropped_total",
				Help: "Total number of messages dropped due to parse/validation errors",
			},
			[]string{"reason"}, // parse_error, empty_result, marshal_error
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

		// Filter Metrics
		StoppageRecordsFilteredTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_receiver_stoppage_records_filtered_total",
				Help: "Total number of redundant stoppage records filtered",
			},
		),
		FilterStateCount: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_receiver_filter_state_count",
				Help: "Number of IMEIs being tracked by stoppage filter",
			},
		),

		TeltonikaPacketsReceived: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_teltonika_packets_received_total",
				Help: "Teltonika AVL frames read from TCP after length framing",
			},
		),
		TeltonikaPacketsAcked: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_teltonika_packets_acked_total",
				Help: "Teltonika AVL frames acknowledged after durable Redis XADD",
			},
		),
		TeltonikaCRCFailures: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_teltonika_crc_failures_total",
				Help: "Teltonika frames failing CRC/count validation (ACK=0)",
			},
		),
		TeltonikaIngestErrors: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_teltonika_ingest_errors_total",
				Help: "Durable Redis XADD failures (ACK withheld)",
			},
		),
		TeltonikaRedisWriteLatency: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "gps_teltonika_redis_write_seconds",
				Help:    "Latency of durable raw XADD before TCP ACK",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2},
			},
		),
		TeltonikaDuplicates: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_teltonika_duplicates_total",
				Help: "Duplicate frames/records skipped by idempotency",
			},
		),
		TeltonikaDecodeFailures: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_teltonika_decode_failures_total",
				Help: "Frames that passed CRC but failed full Codec decode",
			},
		),
		TeltonikaDeadLetterTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_teltonika_dead_letter_total",
				Help: "Payloads routed to gps:dead_letter",
			},
		),
		RawStreamDepth: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_raw_stream_depth",
				Help: "XLEN of gps:raw_incoming",
			},
		),
		RawPendingCount: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_raw_pending_count",
				Help: "Pending entries in raw consumer group",
			},
		),
		PistatRetryDepth: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_pistat_retry_depth",
				Help: "XLEN of gps:pistat_retry",
			},
		),
		HooshnicsRetryDepth: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_hooshnics_retry_depth",
				Help: "XLEN of gps:hooshnics_retry",
			},
		),
		DeadLetterDepth: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_dead_letter_depth",
				Help: "XLEN of gps:dead_letter",
			},
		),
		ParserLatency: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "gps_teltonika_parser_seconds",
				Help:    "Full Codec parse latency in raw workers",
				Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5},
			},
		),
		TeltonikaActiveConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_teltonika_active_connections",
				Help: "Active Teltonika TCP sessions currently being handled",
			},
		),
		PostgresOpenConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_postgres_open_connections",
				Help: "PostgreSQL pool open connections (database/sql Stats.OpenConnections)",
			},
		),
		PostgresInUseConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_postgres_in_use_connections",
				Help: "PostgreSQL pool connections currently in use",
			},
		),
		PostgresMaxOpenConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "gps_postgres_max_open_connections",
				Help: "PostgreSQL pool max open connections configured",
			},
		),
		IntegrationRetryRequeued: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gps_integration_retry_requeued_total",
				Help: "Retry stream requeues after failed delivery (with backoff)",
			},
			[]string{"destination"},
		),
		IntegrationRetryExhausted: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gps_integration_retry_exhausted_total",
				Help: "Retry attempts exhausted and moved to DLQ",
			},
			[]string{"destination"},
		),
		HooshnicsCircuitOpenTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_hooshnics_circuit_open_total",
				Help: "Times the Hooshnics circuit breaker opened",
			},
		),

		ReceivedPacketsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "received_packets_total",
				Help: "Gateway: packets accepted after TCP framing (mirrors gps_teltonika_packets_received_total)",
			},
		),
		ParsedPacketsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "parsed_packets_total",
				Help: "Gateway: successfully decoded telemetry records (points)",
			},
		),
		ParseErrorsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "parse_errors_total",
				Help: "Gateway: decode/parse failures after CRC validation",
			},
		),
		RedisStreamLag: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "redis_stream_lag",
				Help: "Gateway: raw consumer-group pending count (lag proxy)",
			},
		),
		RawStorageFailures: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "raw_storage_failures_total",
				Help: "Gateway: durable Redis XADD failures or local archive write failures",
			},
		),
		ForwardingFailures: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "forwarding_failures_total",
				Help: "Gateway: destination forward failures by target",
			},
			[]string{"destination"},
		),
		PostgresAsyncDrops: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "gps_postgres_async_queue_drops_total",
				Help: "Postgres AsyncWriter ops dropped when queue is full (no goroutine spawn)",
			},
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
	m.IncForwardingFailures("pistat")
}

// RecordRateLimitHit records a rate limit hit
func (m *Metrics) RecordRateLimitHit(clientIP string) {
	m.RateLimitHits.WithLabelValues(clientIP).Inc()
}

// UpdateQueueDepth updates the queue depth gauge
func (m *Metrics) UpdateQueueDepth(depth int64) {
	m.QueueDepth.Set(float64(depth))
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

// RecordParseSuccess records a successful parse operation
func (m *Metrics) RecordParseSuccess(duration time.Duration, recordCount int) {
	m.ParseTotal.WithLabelValues("success").Inc()
	m.ParseDuration.Observe(duration.Seconds())
	m.RecordsParsedTotal.Add(float64(recordCount))
}

// RecordParseFailure records a failed parse operation
func (m *Metrics) RecordParseFailure() {
	m.ParseTotal.WithLabelValues("failure").Inc()
}

// RecordDataDropped records a dropped message
func (m *Metrics) RecordDataDropped(reason string) {
	m.DataDroppedTotal.WithLabelValues(reason).Inc()
}

// RecordStoppageFiltered records filtered stoppage records
func (m *Metrics) RecordStoppageFiltered(count int) {
	m.StoppageRecordsFilteredTotal.Add(float64(count))
}

// UpdateFilterStateCount updates the filter state count metric
func (m *Metrics) UpdateFilterStateCount(count int) {
	m.FilterStateCount.Set(float64(count))
}

func (m *Metrics) IncTeltonikaReceived() {
	if m == nil {
		return
	}
	m.TeltonikaPacketsReceived.Inc()
	m.ReceivedPacketsTotal.Inc()
}

func (m *Metrics) IncTeltonikaAcked() {
	if m == nil {
		return
	}
	m.TeltonikaPacketsAcked.Inc()
}

func (m *Metrics) IncTeltonikaCRCFailure() {
	if m == nil {
		return
	}
	m.TeltonikaCRCFailures.Inc()
}

func (m *Metrics) IncTeltonikaIngestError() {
	if m == nil {
		return
	}
	m.TeltonikaIngestErrors.Inc()
	m.RawStorageFailures.Inc()
}

func (m *Metrics) ObserveTeltonikaRedisWrite(d time.Duration) {
	if m == nil {
		return
	}
	m.TeltonikaRedisWriteLatency.Observe(d.Seconds())
}

func (m *Metrics) IncTeltonikaDuplicate() {
	if m == nil {
		return
	}
	m.TeltonikaDuplicates.Inc()
}

func (m *Metrics) IncTeltonikaDecodeFailure() {
	if m == nil {
		return
	}
	m.TeltonikaDecodeFailures.Inc()
}

func (m *Metrics) IncTeltonikaDeadLetter() {
	if m == nil {
		return
	}
	m.TeltonikaDeadLetterTotal.Inc()
}

func (m *Metrics) ObserveParserLatency(d time.Duration) {
	if m == nil {
		return
	}
	m.ParserLatency.Observe(d.Seconds())
}

func (m *Metrics) UpdateDurableStreamDepths(raw, pending, pistat, hoosh, dlq int64) {
	if m == nil {
		return
	}
	m.RawStreamDepth.Set(float64(raw))
	m.RawPendingCount.Set(float64(pending))
	m.RedisStreamLag.Set(float64(pending))
	m.PistatRetryDepth.Set(float64(pistat))
	m.HooshnicsRetryDepth.Set(float64(hoosh))
	m.DeadLetterDepth.Set(float64(dlq))
}

func (m *Metrics) IncParsedPackets(n int) {
	if m == nil || n <= 0 {
		return
	}
	m.ParsedPacketsTotal.Add(float64(n))
}

func (m *Metrics) IncParseErrors() {
	if m == nil {
		return
	}
	m.ParseErrorsTotal.Inc()
}

func (m *Metrics) IncRawStorageFailure() {
	if m == nil {
		return
	}
	m.RawStorageFailures.Inc()
}

func (m *Metrics) IncForwardingFailures(destination string) {
	if m == nil {
		return
	}
	if destination == "" {
		destination = "unknown"
	}
	m.ForwardingFailures.WithLabelValues(destination).Inc()
}

func (m *Metrics) IncPostgresAsyncDrop() {
	if m == nil {
		return
	}
	m.PostgresAsyncDrops.Inc()
}

func (m *Metrics) SetTeltonikaActiveConnections(n int64) {
	if m == nil {
		return
	}
	m.TeltonikaActiveConnections.Set(float64(n))
}

func (m *Metrics) UpdatePostgresPool(open, inUse, maxOpen int) {
	if m == nil {
		return
	}
	m.PostgresOpenConnections.Set(float64(open))
	m.PostgresInUseConnections.Set(float64(inUse))
	m.PostgresMaxOpenConnections.Set(float64(maxOpen))
}

func (m *Metrics) IncIntegrationRetryRequeued(destination string) {
	if m == nil {
		return
	}
	m.IntegrationRetryRequeued.WithLabelValues(destination).Inc()
}

func (m *Metrics) IncIntegrationRetryExhausted(destination string) {
	if m == nil {
		return
	}
	m.IntegrationRetryExhausted.WithLabelValues(destination).Inc()
}

func (m *Metrics) IncHooshnicsCircuitOpen() {
	if m == nil {
		return
	}
	m.HooshnicsCircuitOpenTotal.Inc()
}
