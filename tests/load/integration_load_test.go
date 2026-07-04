//go:build load

package load

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	loadURL      = flag.String("load-url", envOr("TARGET_URL", "http://localhost:8080/api/gps/reports"), "load test target URL")
	loadRate     = flag.Int("load-rate", envInt("RATE", 10000), "target requests per second")
	loadDuration = flag.Duration("load-duration", 10*time.Second, "load test duration")
	loadWorkers  = flag.Int("load-workers", envInt("WORKERS", 200), "concurrent workers")
)

// TestIntenseTrafficLoad hammers a running server at high RPS.
// Run with: go test -tags=load -v ./tests/load/... -timeout 5m
// Requires the app and Redis to be running (e.g. make docker-up).
func TestIntenseTrafficLoad(t *testing.T) {
	if os.Getenv("RUN_LOAD_TEST") != "1" {
		t.Skip("Set RUN_LOAD_TEST=1 to run high-traffic load test against a live server")
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"device_id": "LOADTEST",
		"lat":       37.7749,
		"lon":       -122.4194,
		"timestamp": time.Now().Unix(),
		"speed":     45.5,
	})

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: *loadWorkers * 2,
			MaxConnsPerHost:     *loadWorkers * 2,
			DisableCompression:  true,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), *loadDuration)
	defer cancel()

	interval := time.Second / time.Duration(*loadRate)
	if interval <= 0 {
		interval = time.Microsecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var (
		total   int64
		success int64
		errors  int64
		status  sync.Map
	)

	var wg sync.WaitGroup
workerLoop:
	for {
		select {
		case <-ctx.Done():
			break workerLoop
		case <-ticker.C:
			wg.Add(1)
			go func() {
				defer wg.Done()
				atomic.AddInt64(&total, 1)

				req, err := http.NewRequestWithContext(ctx, http.MethodPost, *loadURL, bytes.NewReader(payload))
				if err != nil {
					atomic.AddInt64(&errors, 1)
					return
				}
				req.Header.Set("Content-Type", "application/json")

				resp, err := client.Do(req)
				if err != nil {
					atomic.AddInt64(&errors, 1)
					return
				}
				resp.Body.Close()

				code := resp.StatusCode
				v, _ := status.LoadOrStore(code, new(int64))
				atomic.AddInt64(v.(*int64), 1)

				if code >= 200 && code < 300 {
					atomic.AddInt64(&success, 1)
				}
			}()
		}
	}
	wg.Wait()

	elapsed := *loadDuration
	actualRPS := float64(total) / elapsed.Seconds()

	t.Logf("Target URL: %s", *loadURL)
	t.Logf("Target rate: %d req/s", *loadRate)
	t.Logf("Duration: %s", *loadDuration)
	t.Logf("Total requests: %d", total)
	t.Logf("Successful (2xx): %d (%.1f%%)", success, pct(success, total))
	t.Logf("Errors: %d (%.1f%%)", errors, pct(errors, total))
	t.Logf("Actual rate: %.0f req/s", actualRPS)

	status.Range(func(key, value any) bool {
		code := key.(int)
		count := atomic.LoadInt64(value.(*int64))
		t.Logf("  HTTP %d: %d (%.1f%%)", code, count, pct(count, total))
		return true
	})

	if total == 0 {
		t.Fatal("no requests completed")
	}

	successRate := float64(success) / float64(total) * 100
	if successRate < 95 {
		t.Errorf("success rate %.1f%% is below 95%% threshold", successRate)
	}
}

func pct(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}
