package sender

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	// Create optimized HTTP client with connection pooling
	transport := &http.Transport{
		MaxIdleConns:        cfg.HTTP.MaxIdleConns,
		MaxConnsPerHost:     cfg.HTTP.MaxConnsPerHost,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableCompression:  false,
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

// Send sends data to a destination server with retry logic
func (s *HTTPSender) Send(ctx context.Context, data []byte) *SendResult {
	for attempt := 1; attempt <= s.retryConfig.MaxAttempts; attempt++ {
		// Get next server using round-robin
		targetServer := s.loadBalancer.NextServer()
		if targetServer == "" {
			return &SendResult{
				Success:      false,
				TargetServer: "",
				Attempt:      attempt,
				Error:        fmt.Errorf("no servers available"),
			}
		}

		// Attempt to send
		sendStart := time.Now()
		err := s.sendToServer(ctx, targetServer, data)
		sendDuration := time.Since(sendStart)
		
		if err == nil {
			// Success!
			logger.Info("Message sent successfully",
				zap.String("server", targetServer),
				zap.Int("attempt", attempt),
				zap.Int("payload_size", len(data)))
			
			// Record metrics
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordSenderRequest(targetServer, "success", sendDuration)
			}

			return &SendResult{
				Success:      true,
				TargetServer: targetServer,
				Attempt:      attempt,
				Error:        nil,
			}
		}
		
		// Record failed attempt
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.RecordSenderRequest(targetServer, "error", sendDuration)
			if attempt > 1 {
				metrics.AppMetrics.RecordSenderRetry(targetServer, attempt)
			}
		}

		// Log failure
		logger.Warn("Failed to send message",
			zap.String("server", targetServer),
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", s.retryConfig.MaxAttempts),
			zap.Error(err))

		// If this was the last attempt, return failure
		if attempt >= s.retryConfig.MaxAttempts {
			// Record permanent failure
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordSenderFailure(targetServer)
			}
			
			return &SendResult{
				Success:      false,
				TargetServer: targetServer,
				Attempt:      attempt,
				Error:        fmt.Errorf("max retry attempts reached: %w", err),
			}
		}

		// Wait before retry
		delay := s.getRetryDelay(attempt)
		logger.Debug("Waiting before retry",
			zap.Int("attempt", attempt),
			zap.Duration("delay", delay))

		select {
		case <-ctx.Done():
			return &SendResult{
				Success:      false,
				TargetServer: targetServer,
				Attempt:      attempt,
				Error:        fmt.Errorf("context cancelled: %w", ctx.Err()),
			}
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	// Should not reach here
	return &SendResult{
		Success:      false,
		TargetServer: "",
		Attempt:      s.retryConfig.MaxAttempts,
		Error:        fmt.Errorf("unexpected error in retry loop"),
	}
}

// sendToServer sends data to a specific server
func (s *HTTPSender) sendToServer(ctx context.Context, serverURL string, data []byte) error {
	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", serverURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GPS-Data-Receiver/1.0")

	// Send request
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body (for logging, limited)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned non-success status: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// getRetryDelay calculates the delay before the next retry
func (s *HTTPSender) getRetryDelay(attempt int) time.Duration {
	if attempt == 1 {
		return s.retryConfig.DelayFirst
	}
	return s.retryConfig.DelaySubsequent
}

// Close closes the HTTP client
func (s *HTTPSender) Close() {
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
}

