package integration

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/sender"
	"github.com/stretchr/testify/assert"
)

func TestSenderIntegration_SuccessfulSend(t *testing.T) {
	// Create mock server
	requestCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		assert.NotEmpty(t, body)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"received"}`))
	}))
	defer server.Close()

	// Create sender
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Timeout:            30 * time.Second,
			MaxIdleConns:       10,
			MaxConnsPerHost:    5,
			DestinationServers: []string{server.URL},
		},
		Retry: config.RetryConfig{
			MaxAttempts:     5,
			DelayFirst:      100 * time.Millisecond,
			DelaySubsequent: 200 * time.Millisecond,
		},
	}

	httpSender := sender.NewHTTPSender(cfg)
	defer httpSender.Close()

	// Send data
	data := []byte(`{"device_id":"GPS001","lat":37.7749,"lon":-122.4194}`)
	ctx := context.Background()

	result := httpSender.Send(ctx, data)

	assert.True(t, result.Success)
	assert.Equal(t, 1, result.Attempt)
	assert.Nil(t, result.Error)
	assert.Equal(t, server.URL, result.TargetServer)
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount))
}

func TestSenderIntegration_RetryOnFailure(t *testing.T) {
	// Create mock server that fails first 2 times, then succeeds
	requestCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"server error"}`))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"received"}`))
		}
	}))
	defer server.Close()

	// Create sender
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Timeout:            30 * time.Second,
			MaxIdleConns:       10,
			MaxConnsPerHost:    5,
			DestinationServers: []string{server.URL},
		},
		Retry: config.RetryConfig{
			MaxAttempts:     5,
			DelayFirst:      100 * time.Millisecond,
			DelaySubsequent: 200 * time.Millisecond,
		},
	}

	httpSender := sender.NewHTTPSender(cfg)
	defer httpSender.Close()

	// Send data
	data := []byte(`{"device_id":"GPS001"}`)
	ctx := context.Background()

	result := httpSender.Send(ctx, data)

	assert.True(t, result.Success)
	assert.Equal(t, 3, result.Attempt)
	assert.Nil(t, result.Error)
	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
}

func TestSenderIntegration_MaxRetriesExceeded(t *testing.T) {
	// Create mock server that always fails
	requestCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	defer server.Close()

	// Create sender
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Timeout:            30 * time.Second,
			MaxIdleConns:       10,
			MaxConnsPerHost:    5,
			DestinationServers: []string{server.URL},
		},
		Retry: config.RetryConfig{
			MaxAttempts:     3,
			DelayFirst:      50 * time.Millisecond,
			DelaySubsequent: 50 * time.Millisecond,
		},
	}

	httpSender := sender.NewHTTPSender(cfg)
	defer httpSender.Close()

	// Send data
	data := []byte(`{"device_id":"GPS001"}`)
	ctx := context.Background()

	result := httpSender.Send(ctx, data)

	assert.False(t, result.Success)
	assert.Equal(t, 3, result.Attempt)
	assert.NotNil(t, result.Error)
	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
}

func TestSenderIntegration_RoundRobinMultipleServers(t *testing.T) {
	// Create three mock servers
	server1Count := int32(0)
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&server1Count, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2Count := int32(0)
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&server2Count, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	server3Count := int32(0)
	server3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&server3Count, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server3.Close()

	// Create sender with all three servers
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Timeout:            30 * time.Second,
			MaxIdleConns:       10,
			MaxConnsPerHost:    5,
			DestinationServers: []string{server1.URL, server2.URL, server3.URL},
		},
		Retry: config.RetryConfig{
			MaxAttempts:     1,
			DelayFirst:      100 * time.Millisecond,
			DelaySubsequent: 100 * time.Millisecond,
		},
	}

	httpSender := sender.NewHTTPSender(cfg)
	defer httpSender.Close()

	// Send 9 requests
	data := []byte(`{"device_id":"GPS001"}`)
	ctx := context.Background()

	for i := 0; i < 9; i++ {
		result := httpSender.Send(ctx, data)
		assert.True(t, result.Success)
	}

	// Each server should receive 3 requests (round-robin)
	assert.Equal(t, int32(3), atomic.LoadInt32(&server1Count))
	assert.Equal(t, int32(3), atomic.LoadInt32(&server2Count))
	assert.Equal(t, int32(3), atomic.LoadInt32(&server3Count))
}

func TestSenderIntegration_Timeout(t *testing.T) {
	// Create mock server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create sender with short timeout
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Timeout:            500 * time.Millisecond,
			MaxIdleConns:       10,
			MaxConnsPerHost:    5,
			DestinationServers: []string{server.URL},
		},
		Retry: config.RetryConfig{
			MaxAttempts:     2,
			DelayFirst:      100 * time.Millisecond,
			DelaySubsequent: 100 * time.Millisecond,
		},
	}

	httpSender := sender.NewHTTPSender(cfg)
	defer httpSender.Close()

	// Send data
	data := []byte(`{"device_id":"GPS001"}`)
	ctx := context.Background()

	result := httpSender.Send(ctx, data)

	assert.False(t, result.Success)
	assert.NotNil(t, result.Error)
	assert.Contains(t, result.Error.Error(), "context deadline exceeded")
}
