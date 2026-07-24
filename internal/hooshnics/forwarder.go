package hooshnics

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	gojson "github.com/goccy/go-json"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

const (
	kindRaw    = "raw"
	kindParsed = "parsed"
)

// Config controls the async Hooshnics mirror forwarder.
// Kept local to this package to avoid coupling with internal/config consumers.
type Config struct {
	Enabled    bool
	AuthToken  string
	RawURL     string
	ParsedURL  string
	BufferSize int
	SpillDir   string
	Timeout    time.Duration
	Workers    int
}

// RawMeta describes an inbound GPS payload for the raw ingest endpoint.
type RawMeta struct {
	IMEI       string
	SourceType string
	Encoding   string
	ReceivedAt time.Time
}

type job struct {
	kind    string
	url     string
	body    []byte
	headers map[string]string
	attempt int
}

// RetrySink parks failed Hooshnics HTTP jobs on a durable Redis stream.
type RetrySink interface {
	EnqueueHooshnicsRetry(ctx context.Context, kind, url string, body []byte, headersJSON string) error
}

// Forwarder mirrors inbound GPS traffic to Hooshnics without blocking the hot path.
// All public Forward* methods are non-blocking (channel enqueue / Redis retry).
type Forwarder struct {
	cfg    Config
	client *http.Client
	ch     chan job
	done   chan struct{}
	wg     sync.WaitGroup
	retry  RetrySink

	mu            sync.Mutex
	failCount     int
	circuitUntil  time.Time
}

const (
	hooshCircuitFailThreshold = 5
	hooshCircuitOpenDuration  = 30 * time.Second
)

// NewForwarder creates a forwarder when enabled and configured. Returns nil when disabled.
func NewForwarder(cfg Config) (*Forwarder, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		return nil, fmt.Errorf("HOOSHNICS_AUTH_TOKEN is required when mirror is enabled")
	}
	if strings.TrimSpace(cfg.RawURL) == "" && strings.TrimSpace(cfg.ParsedURL) == "" {
		return nil, fmt.Errorf("HOOSHNICS_RAW_URL or HOOSHNICS_PARSED_URL is required when mirror is enabled")
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 50000
	}
	if cfg.SpillDir == "" {
		cfg.SpillDir = "logs/hooshnics_spill"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxConnsPerHost:     20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: cfg.Timeout,
	}

	return &Forwarder{
		cfg: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
		ch:   make(chan job, cfg.BufferSize),
		done: make(chan struct{}),
	}, nil
}

// SetRetrySink configures durable Redis retry for failed/full-buffer dispatches.
// Prefer this over local disk spill for zero-loss deployments.
func (f *Forwarder) SetRetrySink(sink RetrySink) {
	if f == nil {
		return
	}
	f.retry = sink
}

// Start launches background workers.
func (f *Forwarder) Start() {
	if f == nil {
		return
	}
	for i := 0; i < f.cfg.Workers; i++ {
		f.wg.Add(1)
		go f.worker()
	}
	logger.Info("Hooshnics mirror forwarder started",
		zap.String("raw_url", f.cfg.RawURL),
		zap.String("parsed_url", f.cfg.ParsedURL),
		zap.Int("buffer_size", f.cfg.BufferSize),
		zap.Int("workers", f.cfg.Workers),
		zap.String("spill_dir", f.cfg.SpillDir))
}

// Close stops workers after draining in-flight jobs.
func (f *Forwarder) Close() {
	if f == nil {
		return
	}
	close(f.done)
	f.wg.Wait()
	logger.Info("Hooshnics mirror forwarder stopped")
}

// ForwardRaw queues a raw payload for Hooshnics ingest. Never blocks the caller.
func (f *Forwarder) ForwardRaw(payload []byte, meta RawMeta) {
	if f == nil || len(payload) == 0 || strings.TrimSpace(f.cfg.RawURL) == "" {
		return
	}

	body, headers, err := buildRawRequest(payload, meta)
	if err != nil {
		logger.Warn("Hooshnics raw mirror encode failed", zap.Error(err))
		f.spill(kindRaw, payload)
		return
	}

	f.enqueue(job{
		kind:    kindRaw,
		url:     f.cfg.RawURL,
		body:    body,
		headers: headers,
	})
}

// ForwardTeltonikaAVL forwards a native Teltonika AVL frame with session IMEI metadata.
func (f *Forwarder) ForwardTeltonikaAVL(imei string, frame []byte) {
	if f == nil || len(frame) == 0 {
		return
	}
	f.ForwardRaw(frame, RawMeta{
		IMEI:       imei,
		SourceType: "teltonika",
		Encoding:   "hex",
		ReceivedAt: time.Now().UTC(),
	})
}

// ForwardHTTPBody forwards an HTTP ingest body (port 80/8080) to Hooshnics raw ingest.
func (f *Forwarder) ForwardHTTPBody(body []byte, imei string, contentType string) {
	if f == nil || len(body) == 0 {
		return
	}

	meta := RawMeta{
		IMEI:       imei,
		ReceivedAt: time.Now().UTC(),
	}

	ct := strings.ToLower(strings.TrimSpace(contentType))
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	switch {
	case strings.HasPrefix(ct, "application/octet-stream"):
		meta.SourceType = "teltonika"
		meta.Encoding = "hex"
	case len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '['):
		meta.SourceType = "hooshnic"
		meta.Encoding = "json"
	case looksLikeHex(body):
		meta.SourceType = "teltonika"
		meta.Encoding = "hex"
	default:
		meta.SourceType = "http"
		if utf8.Valid(body) {
			meta.Encoding = "text"
		} else {
			meta.Encoding = "hex"
		}
	}

	f.ForwardRaw(body, meta)
}

// ForwardParsed queues parsed JSON ({"data":[...]}) for the optional parsed ingest endpoint.
func (f *Forwarder) ForwardParsed(payload []byte) {
	if f == nil || len(payload) == 0 || strings.TrimSpace(f.cfg.ParsedURL) == "" {
		return
	}
	f.enqueue(job{
		kind: kindParsed,
		url:  f.cfg.ParsedURL,
		body: append([]byte(nil), payload...),
		headers: map[string]string{
			"Content-Type": "application/json",
		},
	})
}

func (f *Forwarder) enqueue(j job) {
	select {
	case f.ch <- j:
	default:
		logger.Warn("Hooshnics mirror buffer full — parking on Redis retry stream",
			zap.String("kind", j.kind),
			zap.Int("payload_size", len(j.body)))
		f.parkRetry(j)
	}
}

func (f *Forwarder) worker() {
	defer f.wg.Done()
	for {
		select {
		case <-f.done:
			for {
				select {
				case j := <-f.ch:
					f.deliver(j)
				default:
					return
				}
			}
		case j := <-f.ch:
			f.deliver(j)
		}
	}
}

func (f *Forwarder) deliver(j job) {
	if f.circuitOpen() {
		logger.Warn("Hooshnics circuit open — parking on retry without HTTP attempts",
			zap.String("kind", j.kind))
		f.parkRetry(j)
		return
	}

	const maxAttempts = 5
	backoff := 500 * time.Millisecond

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		j.attempt = attempt
		status, err := f.post(j)
		if err == nil && status >= 200 && status < 300 {
			f.recordSuccess()
			logger.Debug("Hooshnics mirror delivered",
				zap.String("kind", j.kind),
				zap.Int("status", status),
				zap.Int("attempt", attempt),
				zap.Int("payload_size", len(j.body)))
			return
		}

		if err == nil && status >= 400 && status < 500 && status != 429 {
			logger.Warn("Hooshnics mirror rejected payload",
				zap.String("kind", j.kind),
				zap.Int("status", status),
				zap.Int("payload_size", len(j.body)))
			f.parkRetry(j)
			return
		}

		logger.Warn("Hooshnics mirror delivery failed",
			zap.String("kind", j.kind),
			zap.Int("attempt", attempt),
			zap.Int("status", status),
			zap.Error(err))

		if attempt == maxAttempts {
			f.recordFailure()
			f.parkRetry(j)
			return
		}

		timer := time.NewTimer(backoff)
		select {
		case <-f.done:
			timer.Stop()
			f.parkRetry(j)
			return
		case <-timer.C:
		}
		backoff *= 2
		if backoff > 15*time.Second {
			backoff = 15 * time.Second
		}
	}
}

func (f *Forwarder) circuitOpen() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return time.Now().Before(f.circuitUntil)
}

func (f *Forwarder) recordSuccess() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failCount = 0
}

func (f *Forwarder) recordFailure() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failCount++
	if f.failCount >= hooshCircuitFailThreshold {
		f.circuitUntil = time.Now().Add(hooshCircuitOpenDuration)
		logger.Warn("Hooshnics circuit open after consecutive delivery failures",
			zap.Int("failures", f.failCount),
			zap.Duration("open_for", hooshCircuitOpenDuration))
		f.failCount = 0
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.IncHooshnicsCircuitOpen()
		}
	}
}

func (f *Forwarder) post(j job) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), f.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.url, bytes.NewReader(j.body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Ingest-Token", f.cfg.AuthToken)
	req.Header.Set("X-Gps-Mirror", "gps-receiver")
	for k, v := range j.headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func (f *Forwarder) parkRetry(j job) {
	if metrics.AppMetrics != nil {
		metrics.AppMetrics.IncForwardingFailures("hooshnics")
	}
	if f.retry != nil {
		headersJSON := "{}"
		if len(j.headers) > 0 {
			if b, err := gojson.Marshal(j.headers); err == nil {
				headersJSON = string(b)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.retry.EnqueueHooshnicsRetry(ctx, j.kind, j.url, j.body, headersJSON); err != nil {
			logger.Error("Hooshnics Redis retry enqueue failed — falling back to disk spill",
				zap.String("kind", j.kind),
				zap.Error(err))
			f.spill(j.kind, j.body)
			return
		}
		logger.Warn("Hooshnics payload parked on Redis retry stream",
			zap.String("kind", j.kind),
			zap.Int("payload_size", len(j.body)))
		return
	}
	f.spill(j.kind, j.body)
}

func (f *Forwarder) spill(kind string, body []byte) {
	dir := filepath.Join(f.cfg.SpillDir, kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Error("Hooshnics spill mkdir failed", zap.Error(err))
		return
	}
	name := fmt.Sprintf("%d.json", time.Now().UnixNano())
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		logger.Error("Hooshnics spill write failed", zap.String("path", path), zap.Error(err))
		return
	}
	logger.Warn("Hooshnics payload spilled to disk",
		zap.String("kind", kind),
		zap.String("path", path),
		zap.Int("payload_size", len(body)))
}

func buildRawRequest(payload []byte, meta RawMeta) ([]byte, map[string]string, error) {
	encoding := strings.ToLower(strings.TrimSpace(meta.Encoding))
	sourceType := strings.TrimSpace(meta.SourceType)
	imei := strings.TrimSpace(meta.IMEI)
	receivedAt := meta.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}

	trimmed := bytes.TrimLeft(payload, " \t\r\n")
	isJSON := len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') && utf8.Valid(payload)

	// Plain JSON device payloads (Hooshnic ASCII) — forward as-is.
	if isJSON && (encoding == "" || encoding == "json") && sourceType != "teltonika" {
		headers := map[string]string{
			"Content-Type": "application/json",
		}
		if imei != "" {
			headers["X-Device-Imei"] = imei
		}
		if sourceType != "" {
			headers["X-Gps-Source-Type"] = sourceType
		}
		return append([]byte(nil), payload...), headers, nil
	}

	payloadStr := string(payload)
	enc := encoding
	if enc == "" || enc == "binary" || enc == "hex" || !utf8.Valid(payload) {
		enc = "hex"
		payloadStr = hex.EncodeToString(payload)
	} else if enc == "json" && !isJSON {
		enc = "hex"
		payloadStr = hex.EncodeToString(payload)
	}

	if sourceType == "" {
		if enc == "hex" {
			sourceType = "teltonika"
		} else {
			sourceType = "http"
		}
	}

	envelope := map[string]interface{}{
		"payload":          payloadStr,
		"payload_encoding": enc,
		"source_type":      sourceType,
		"received_at":      receivedAt.UTC().Format(time.RFC3339Nano),
	}
	if imei != "" {
		envelope["imei"] = imei
	}

	body, err := gojson.Marshal(envelope)
	if err != nil {
		return nil, nil, err
	}
	return body, map[string]string{"Content-Type": "application/json"}, nil
}

func looksLikeHex(payload []byte) bool {
	s := strings.TrimSpace(string(payload))
	if len(s) < 20 || len(s)%2 != 0 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
