package sender

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// ErrRateLimited is returned when a destination responds with HTTP 429.
type ErrRateLimited struct {
	Server     string
	RetryAfter time.Duration
}

func (e *ErrRateLimited) Error() string {
	return fmt.Sprintf("rate limited by %s, retry after %s", e.Server, e.RetryAfter)
}

// RetryDelay returns how long to wait before redelivering the message.
func (e *ErrRateLimited) RetryDelay() time.Duration {
	return e.RetryAfter
}

// HTTPSender handles sending data to destination servers
type HTTPSender struct {
	client       *http.Client
	loadBalancer *LoadBalancer
	retryConfig  *config.RetryConfig
	limiters     map[string]*rate.Limiter
	pausedUntil  map[string]time.Time
	failCount    map[string]int
	mu           sync.RWMutex
}

const (
	circuitFailThreshold = 5
	circuitOpenDuration  = 30 * time.Second
)

// NewHTTPSender creates a new HTTP sender
func NewHTTPSender(cfg *config.Config) *HTTPSender {
	transport := &http.Transport{
		MaxIdleConns:        cfg.HTTP.MaxIdleConns,
		MaxConnsPerHost:     cfg.HTTP.MaxConnsPerHost,
		MaxIdleConnsPerHost: cfg.HTTP.MaxConnsPerHost,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableCompression:  false,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 15 * time.Second,
		WriteBufferSize:       32 * 1024,
		ReadBufferSize:        32 * 1024,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.HTTP.Timeout,
	}

	loadBalancer := NewLoadBalancer(cfg.HTTP.DestinationServers)

	limiters := make(map[string]*rate.Limiter, len(cfg.HTTP.DestinationServers))
	if cfg.OutgoingRateLimit.RequestsPerSecond > 0 {
		rps := rate.Limit(cfg.OutgoingRateLimit.RequestsPerSecond)
		burst := cfg.OutgoingRateLimit.BurstSize
		if burst <= 0 {
			burst = cfg.OutgoingRateLimit.RequestsPerSecond * 2
		}
		for _, server := range cfg.HTTP.DestinationServers {
			limiters[server] = rate.NewLimiter(rps, burst)
		}
	}

	pausedUntil := make(map[string]time.Time, len(cfg.HTTP.DestinationServers))
	failCount := make(map[string]int, len(cfg.HTTP.DestinationServers))
	for _, server := range cfg.HTTP.DestinationServers {
		pausedUntil[server] = time.Time{}
		failCount[server] = 0
	}

	logger.Info("HTTP sender initialized",
		zap.Int("server_count", loadBalancer.ServerCount()),
		zap.Strings("servers", loadBalancer.GetServers()),
		zap.Int("outgoing_rate_limit_rps", cfg.OutgoingRateLimit.RequestsPerSecond),
		zap.Int("outgoing_rate_limit_burst", cfg.OutgoingRateLimit.BurstSize),
		zap.Int("circuit_fail_threshold", circuitFailThreshold),
		zap.Duration("circuit_open", circuitOpenDuration))

	return &HTTPSender{
		client:       client,
		loadBalancer: loadBalancer,
		retryConfig:  &cfg.Retry,
		limiters:     limiters,
		pausedUntil:  pausedUntil,
		failCount:    failCount,
	}
}

// SendResult contains the result of a send operation
type SendResult struct {
	Success      bool
	TargetServer string
	Attempt      int
	Error        error
}

// SendObserver receives notifications during a send operation.
type SendObserver interface {
	OnAttempt(server string, attempt int)
}

// FuncSendObserver implements SendObserver with optional callbacks.
type FuncSendObserver struct {
	OnAttemptFunc func(server string, attempt int)
}

func (f FuncSendObserver) OnAttempt(server string, attempt int) {
	if f.OnAttemptFunc != nil {
		f.OnAttemptFunc(server, attempt)
	}
}

func notifyAttempt(observers []SendObserver, server string, attempt int) {
	for _, observer := range observers {
		if observer != nil {
			observer.OnAttempt(server, attempt)
		}
	}
}

// Send sends data to destination servers with retry logic and exponential backoff.
// Each retry rotates to the next server so a single unhealthy host doesn't block the worker.
func (s *HTTPSender) Send(ctx context.Context, data []byte, observers ...SendObserver) *SendResult {
	serverCount := s.loadBalancer.ServerCount()
	if serverCount == 0 {
		return &SendResult{
			Success: false,
			Error:   fmt.Errorf("no servers available"),
		}
	}

	var lastErr error
	var lastServer string
	var rateLimitErr *ErrRateLimited

	for attempt := 1; attempt <= s.retryConfig.MaxAttempts; attempt++ {
		targetServer, foundAvailable := s.nextAvailableServer(serverCount)
		if !foundAvailable {
			if rateLimitErr != nil {
				return &SendResult{
					Success:      false,
					TargetServer: rateLimitErr.Server,
					Attempt:      attempt - 1,
					Error:        rateLimitErr,
				}
			}
			break
		}

		lastServer = targetServer
		notifyAttempt(observers, targetServer, attempt)

		sendStart := time.Now()
		err := s.sendToServer(ctx, targetServer, data)
		sendDuration := time.Since(sendStart)

		if err == nil {
			logger.Debug("Message sent successfully",
				zap.String("server", targetServer),
				zap.Int("attempt", attempt),
				zap.Int("payload_size", len(data)))

			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordSenderRequest(targetServer, "success", sendDuration)
			}
			s.recordSuccess(targetServer)

			return &SendResult{
				Success:      true,
				TargetServer: targetServer,
				Attempt:      attempt,
			}
		}

		lastErr = err
		s.recordFailure(targetServer)

		var rlErr *ErrRateLimited
		if errors.As(err, &rlErr) {
			rateLimitErr = rlErr
			s.pauseServer(targetServer, time.Now().Add(rlErr.RetryAfter))

			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordSenderRequest(targetServer, "rate_limited", sendDuration)
			}

			logger.Warn("Destination rate limited, pausing server",
				zap.String("server", targetServer),
				zap.Duration("retry_after", rlErr.RetryAfter),
				zap.Int("attempt", attempt))

			continue
		}

		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordSenderRequest(targetServer, "error", sendDuration)
			if attempt > 1 {
				metrics.AppMetrics.RecordSenderRetry(targetServer, attempt)
			}
		}

		logger.Warn("Failed to send message",
			zap.String("server", targetServer),
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", s.retryConfig.MaxAttempts),
			zap.Error(err))

		if attempt >= s.retryConfig.MaxAttempts {
			break
		}

		delay := s.getRetryDelay(attempt)
		select {
		case <-ctx.Done():
			return &SendResult{
				Success:      false,
				TargetServer: targetServer,
				Attempt:      attempt,
				Error:        fmt.Errorf("context cancelled: %w", ctx.Err()),
			}
		case <-time.After(delay):
		}
	}

	if rateLimitErr != nil {
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordSenderFailure(rateLimitErr.Server)
		}

		return &SendResult{
			Success:      false,
			TargetServer: rateLimitErr.Server,
			Attempt:      s.retryConfig.MaxAttempts,
			Error:        rateLimitErr,
		}
	}

	if metrics.AppMetrics != nil {
		metrics.AppMetrics.RecordSenderFailure(lastServer)
	}

	return &SendResult{
		Success:      false,
		TargetServer: lastServer,
		Attempt:      s.retryConfig.MaxAttempts,
		Error:        fmt.Errorf("max retry attempts reached: %w", lastErr),
	}
}

func (s *HTTPSender) nextAvailableServer(serverCount int) (string, bool) {
	for i := 0; i < serverCount; i++ {
		candidate := s.loadBalancer.NextServer()
		if !s.isPaused(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func (s *HTTPSender) isPaused(server string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	until, ok := s.pausedUntil[server]
	return ok && time.Now().Before(until)
}

func (s *HTTPSender) pauseServer(server string, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.pausedUntil[server]; !ok || until.After(existing) {
		s.pausedUntil[server] = until
	}
}

func (s *HTTPSender) recordSuccess(server string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCount[server] = 0
}

func (s *HTTPSender) recordFailure(server string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCount[server]++
	if s.failCount[server] >= circuitFailThreshold {
		until := time.Now().Add(circuitOpenDuration)
		if existing, ok := s.pausedUntil[server]; !ok || until.After(existing) {
			s.pausedUntil[server] = until
		}
		logger.Warn("Circuit open for destination after consecutive failures",
			zap.String("server", server),
			zap.Int("failures", s.failCount[server]),
			zap.Duration("open_for", circuitOpenDuration))
		s.failCount[server] = 0
	}
}

// sendToServer sends data to a specific server
func (s *HTTPSender) sendToServer(ctx context.Context, serverURL string, data []byte) error {
	if limiter, ok := s.limiters[serverURL]; ok && limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter wait: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", serverURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GPS-Data-Receiver/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Discard body to allow connection reuse
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		if retryAfter <= 0 {
			retryAfter = 5 * time.Second
		}
		return &ErrRateLimited{
			Server:     serverURL,
			RetryAfter: retryAfter,
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned non-success status: %d", resp.StatusCode)
	}

	return nil
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(header); err == nil {
		return time.Duration(seconds) * time.Second
	}

	if t, err := http.ParseTime(header); err == nil {
		delay := time.Until(t)
		if delay < 0 {
			return 0
		}
		return delay
	}

	return 0
}

// getRetryDelay returns an exponentially increasing delay with jitter:
// base * 2^(attempt-1) +-25%  (capped at 30s).
func (s *HTTPSender) getRetryDelay(attempt int) time.Duration {
	base := s.retryConfig.DelayFirst
	exp := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(base) * exp)

	const maxDelay = 30 * time.Second
	if delay > maxDelay {
		delay = maxDelay
	}

	jitter := float64(delay) * 0.25
	delay = time.Duration(float64(delay) + (rand.Float64()*2-1)*jitter)

	return delay
}

// Close closes the HTTP client
func (s *HTTPSender) Close() {
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
}
