// Command loadtest generates sustained HTTP load against the GPS receiver API.
// Optimized for 10K+ req/s using connection pooling and parallel workers.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

const defaultPayload = `{"device_id":"GPS001","lat":37.7749,"lon":-122.4194,"timestamp":1234567890,"speed":45.5,"altitude":100,"heading":180}`

type result struct {
	statusCode int
	latency    time.Duration
	err        error
}

func main() {
	targetURL := flag.String("url", envOr("TARGET_URL", "http://localhost:8080/api/gps/reports"), "Target URL")
	duration := flag.Duration("duration", parseDuration(envOr("DURATION", "30s")), "Test duration")
	warmup := flag.Duration("warmup", parseDuration(envOr("WARMUP", "5s")), "Warmup duration (excluded from report)")
	rateLimit := flag.Int("rate", envInt("RATE", 10000), "Target requests per second")
	workers := flag.Int("workers", envInt("WORKERS", 200), "Concurrent worker goroutines")
	timeout := flag.Duration("timeout", parseDuration(envOr("TIMEOUT", "5s")), "Per-request timeout")
	payloadPath := flag.String("payload", envOr("PAYLOAD_FILE", ""), "Path to JSON payload file (optional)")
	flag.Parse()

	payload := []byte(defaultPayload)
	if *payloadPath != "" {
		data, err := os.ReadFile(*payloadPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read payload file: %v\n", err)
			os.Exit(1)
		}
		payload = data
	}

	client := &http.Client{
		Timeout: *timeout,
		Transport: &http.Transport{
			MaxIdleConns:        *workers * 2,
			MaxIdleConnsPerHost: *workers * 2,
			MaxConnsPerHost:     *workers * 2,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
		},
	}

	fmt.Println("GPS Data Receiver — Load Test")
	fmt.Println("=============================")
	fmt.Printf("Target URL:   %s\n", *targetURL)
	fmt.Printf("Target rate:  %d req/s\n", *rateLimit)
	fmt.Printf("Workers:      %d\n", *workers)
	fmt.Printf("Warmup:       %s\n", *warmup)
	fmt.Printf("Duration:     %s\n", *duration)
	fmt.Printf("Timeout:      %s\n", *timeout)
	fmt.Printf("Payload size: %d bytes\n\n", len(payload))

	if *warmup > 0 {
		fmt.Printf("Warming up for %s...\n", *warmup)
		runPhase(client, *targetURL, payload, *workers, *rateLimit, *warmup)
	}

	fmt.Printf("Running load test for %s...\n\n", *duration)
	stats := runPhase(client, *targetURL, payload, *workers, *rateLimit, *duration)
	printReport(stats)
}

type phaseStats struct {
	total      int64
	success    int64
	errors     int64
	status     map[int]int64
	latencies  []time.Duration
	start, end time.Time
}

func runPhase(client *http.Client, url string, payload []byte, workers, rps int, duration time.Duration) *phaseStats {
	stats := &phaseStats{
		status: make(map[int]int64),
		start:  time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	limiter := rate.NewLimiter(rate.Limit(rps), rps)
	if rps <= 0 {
		limiter = rate.NewLimiter(rate.Inf, 1)
	}

	results := make(chan result, workers*4)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if err := limiter.Wait(ctx); err != nil {
					return
				}

				start := time.Now()
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
				if err != nil {
					results <- result{err: err, latency: time.Since(start)}
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Connection", "keep-alive")

				resp, err := client.Do(req)
				latency := time.Since(start)
				if err != nil {
					results <- result{err: err, latency: latency}
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				results <- result{statusCode: resp.StatusCode, latency: latency}
			}
		}()
	}

	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for r := range results {
			atomic.AddInt64(&stats.total, 1)
			stats.latencies = append(stats.latencies, r.latency)
			if r.err != nil {
				atomic.AddInt64(&stats.errors, 1)
				continue
			}
			stats.status[r.statusCode]++
			if r.statusCode >= 200 && r.statusCode < 300 {
				atomic.AddInt64(&stats.success, 1)
			}
		}
	}()

	wg.Wait()
	close(results)
	<-collectorDone
	stats.end = time.Now()
	return stats
}

func printReport(stats *phaseStats) {
	elapsed := stats.end.Sub(stats.start)
	actualRPS := float64(stats.total) / elapsed.Seconds()

	fmt.Println("Results")
	fmt.Println("-------")
	fmt.Printf("Total requests:   %d\n", stats.total)
	fmt.Printf("Successful (2xx): %d (%.2f%%)\n", stats.success, pct(stats.success, stats.total))
	fmt.Printf("Errors:           %d (%.2f%%)\n", stats.errors, pct(stats.errors, stats.total))
	fmt.Printf("Elapsed:          %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Actual rate:      %.0f req/s\n", actualRPS)

	if len(stats.status) > 0 {
		fmt.Println("\nStatus codes:")
		codes := make([]int, 0, len(stats.status))
		for code := range stats.status {
			codes = append(codes, code)
		}
		sort.Ints(codes)
		for _, code := range codes {
			count := stats.status[code]
			fmt.Printf("  %d: %d (%.2f%%)\n", code, count, pct(count, stats.total))
		}
	}

	non2xx := stats.total - stats.success - stats.errors
	if non2xx > 0 {
		fmt.Printf("\nNon-2xx responses: %d (%.2f%%)\n", non2xx, pct(non2xx, stats.total))
	}

	if len(stats.latencies) == 0 {
		return
	}

	sort.Slice(stats.latencies, func(i, j int) bool {
		return stats.latencies[i] < stats.latencies[j]
	})

	fmt.Println("\nLatency:")
	fmt.Printf("  min:  %s\n", stats.latencies[0].Round(time.Microsecond))
	fmt.Printf("  p50:  %s\n", percentile(stats.latencies, 0.50).Round(time.Microsecond))
	fmt.Printf("  p90:  %s\n", percentile(stats.latencies, 0.90).Round(time.Microsecond))
	fmt.Printf("  p95:  %s\n", percentile(stats.latencies, 0.95).Round(time.Microsecond))
	fmt.Printf("  p99:  %s\n", percentile(stats.latencies, 0.99).Round(time.Microsecond))
	fmt.Printf("  max:  %s\n", stats.latencies[len(stats.latencies)-1].Round(time.Microsecond))
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
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

func parseDuration(s string) time.Duration {
	s = strings.TrimSpace(s)
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	var secs int
	if _, err := fmt.Sscanf(s, "%d", &secs); err == nil {
		return time.Duration(secs) * time.Second
	}
	return 30 * time.Second
}
