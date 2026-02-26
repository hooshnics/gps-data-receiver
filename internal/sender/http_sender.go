package sender

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// HTTPSender handles sending data to destination servers
type HTTPSender struct {
	client       *http.Client
	loadBalancer *LoadBalancer
	retryConfig  *config.RetryConfig
}

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

	logger.Info("HTTP sender initialized",
		zap.Int("server_count", loadBalancer.ServerCount()),
		zap.Strings("servers", loadBalancer.GetServers()))

	return &HTTPSender{
		client:       client,
		loadBalancer: loadBalancer,
		retryConfig:  &cfg.Retry,
	}
}

// SendResult contains the result of a send operation
type SendResult struct {
	Success      bool
	TargetServer string
	Attempt      int
	Error        error
}

// Send sends data to destination servers with retry logic and exponential backoff.
// Each retry rotates to the next server so a single unhealthy host doesn't block the worker.
func (s *HTTPSender) Send(ctx context.Context, data []byte) *SendResult {
	// Global throttle: wait 2 seconds before each send attempt for this message.
	select {
	case <-ctx.Done():
		return &SendResult{
			Success: false,
			Error:   fmt.Errorf("context cancelled before send: %w", ctx.Err()),
		}
	case <-time.After(2 * time.Second):
	}

	serverCount := s.loadBalancer.ServerCount()
	if serverCount == 0 {
		return &SendResult{
			Success: false,
			Error:   fmt.Errorf("no servers available"),
		}
	}

	var lastErr error
	var lastServer string

	for attempt := 1; attempt <= s.retryConfig.MaxAttempts; attempt++ {
		targetServer := s.loadBalancer.NextServer()
		lastServer = targetServer

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

			return &SendResult{
				Success:      true,
				TargetServer: targetServer,
				Attempt:      attempt,
			}
		}

		lastErr = err

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

// sendToServer sends data to a specific server
func (s *HTTPSender) sendToServer(ctx context.Context, serverURL string, data []byte) error {
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned non-success status: %d", resp.StatusCode)
	}

	return nil
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
