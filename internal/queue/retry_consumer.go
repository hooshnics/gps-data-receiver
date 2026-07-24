package queue

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	gojson "github.com/goccy/go-json"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/internal/sender"
	"github.com/gps-data-receiver/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	defaultRetryMaxAttempts = 20
	retryStreamSoftCap      = 50000
)

func retryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 5 {
		shift = 5
	}
	d := time.Duration(1<<uint(shift)) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// RetryConsumers drains gps:pistat_retry and gps:hooshnics_retry without touching raw_incoming.
type RetryConsumers struct {
	queue        *RedisQueue
	sender       *sender.HTTPSender
	client       *http.Client
	auth         string
	maxAttempts  int
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewRetryConsumers builds background retry workers for PiStat and Hooshnics.
func NewRetryConsumers(q *RedisQueue, s *sender.HTTPSender, httpClient *http.Client, authToken string) *RetryConsumers {
	ctx, cancel := context.WithCancel(context.Background())
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &RetryConsumers{
		queue:       q,
		sender:      s,
		client:      httpClient,
		auth:        authToken,
		maxAttempts: defaultRetryMaxAttempts,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start launches workers plus pending reclaimers per retry stream.
func (r *RetryConsumers) Start() {
	if r == nil {
		return
	}
	r.wg.Add(4)
	go r.loopPistat()
	go r.loopHooshnics()
	go r.reclaim(r.queue.GetPistatRetryStream(), r.queue.GetPistatRetryGroup(), r.handlePistat)
	go r.reclaim(r.queue.GetHooshnicsRetryStream(), r.queue.GetHooshnicsRetryGroup(), r.handleHooshnics)
	logger.Info("Retry consumers started",
		zap.String("pistat_stream", r.queue.GetPistatRetryStream()),
		zap.String("hooshnics_stream", r.queue.GetHooshnicsRetryStream()),
		zap.Int("max_attempts", r.maxAttempts))
}

// Stop cancels retry loops.
func (r *RetryConsumers) Stop() {
	if r == nil {
		return
	}
	r.cancel()
	r.wg.Wait()
}

func (r *RetryConsumers) loopPistat() {
	defer r.wg.Done()
	consumer := fmt.Sprintf("pistat-retry-%d", time.Now().Unix())
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
			r.drainOnce(
				r.queue.GetPistatRetryStream(),
				r.queue.GetPistatRetryGroup(),
				consumer,
				r.handlePistat,
			)
		}
	}
}

func (r *RetryConsumers) loopHooshnics() {
	defer r.wg.Done()
	consumer := fmt.Sprintf("hooshnics-retry-%d", time.Now().Unix())
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
			r.drainOnce(
				r.queue.GetHooshnicsRetryStream(),
				r.queue.GetHooshnicsRetryGroup(),
				consumer,
				r.handleHooshnics,
			)
		}
	}
}

type msgHandler func(ctx context.Context, msg redis.XMessage) bool

func (r *RetryConsumers) drainOnce(stream, group, consumer string, h msgHandler) {
	streams, err := r.queue.GetClient().XReadGroup(r.ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    20,
		Block:    3 * time.Second,
	}).Result()
	if err != nil {
		if err == context.Canceled || err == redis.Nil {
			return
		}
		if strings.Contains(err.Error(), "NOGROUP") {
			_ = r.queue.EnsureDurableStreams(r.ctx)
			time.Sleep(time.Second)
			return
		}
		time.Sleep(time.Second)
		return
	}
	for _, streamRes := range streams {
		for _, msg := range streamRes.Messages {
			ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
			ok := h(ctx, msg)
			cancel()
			if ok {
				r.ack(stream, group, msg.ID)
			}
		}
	}
}

func (r *RetryConsumers) reclaim(stream, group string, h msgHandler) {
	defer r.wg.Done()
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()
	name := fmt.Sprintf("retry-reclaimer-%d", time.Now().UnixNano())
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
			msgs, _, err := r.queue.GetClient().XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   stream,
				Group:    group,
				Consumer: name,
				MinIdle:  90 * time.Second,
				Start:    "0-0",
				Count:    50,
			}).Result()
			cancel()
			if err != nil || len(msgs) == 0 {
				continue
			}
			logger.Info("Reclaiming stale retry messages",
				zap.String("stream", stream),
				zap.Int("count", len(msgs)))
			for _, msg := range msgs {
				ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
				ok := h(ctx, msg)
				cancel()
				if ok {
					r.ack(stream, group, msg.ID)
				}
			}
		}
	}
}

func (r *RetryConsumers) ack(stream, group, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pipe := r.queue.GetClient().Pipeline()
	pipe.XAck(ctx, stream, group, id)
	pipe.XDel(ctx, stream, id)
	_, _ = pipe.Exec(ctx)
}

func msgAttempt(msg redis.XMessage) int {
	switch v := msg.Values[fieldAttempt].(type) {
	case string:
		n, _ := strconv.Atoi(v)
		if n > 0 {
			return n
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	}
	return 1
}

func (r *RetryConsumers) handlePistat(ctx context.Context, msg redis.XMessage) bool {
	payload, _ := msg.Values[fieldPayload].(string)
	if payload == "" || r.sender == nil {
		return true
	}
	attempt := msgAttempt(msg)
	if attempt > r.maxAttempts {
		logger.Error("PiStat retry exhausted — moving to DLQ",
			zap.String("id", msg.ID),
			zap.Int("attempt", attempt))
		if err := r.queue.EnqueueDeadLetterAttempt(ctx, "", []byte(payload), fmt.Sprintf("pistat retry exhausted after %d", attempt), attempt); err != nil {
			logger.Error("DLQ enqueue failed — leaving retry message pending",
				zap.String("id", msg.ID), zap.Error(err))
			return false
		}
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.IncTeltonikaDeadLetter()
			metrics.AppMetrics.IncIntegrationRetryExhausted("pistat")
		}
		return true
	}

	result := r.sender.Send(ctx, []byte(payload))
	if result.Success {
		logger.Info("PiStat retry delivered",
			zap.String("id", msg.ID),
			zap.Int("attempt", attempt))
		return true
	}

	select {
	case <-r.ctx.Done():
		return false
	case <-time.After(retryBackoff(attempt)):
	}

	next := attempt + 1
	if err := r.queue.EnqueuePistatRetry(ctx, []byte(payload), next); err != nil {
		logger.Warn("PiStat retry requeue failed — leaving pending",
			zap.String("id", msg.ID),
			zap.Error(err))
		return false
	}
	if metrics.AppMetrics != nil {
		metrics.AppMetrics.IncIntegrationRetryRequeued("pistat")
	}
	logger.Warn("PiStat retry still failing — requeued after backoff",
		zap.String("id", msg.ID),
		zap.Int("next_attempt", next),
		zap.Duration("backoff", retryBackoff(attempt)),
		zap.Error(result.Error))
	return true
}

func (r *RetryConsumers) handleHooshnics(ctx context.Context, msg redis.XMessage) bool {
	url, _ := msg.Values[fieldURL].(string)
	body, _ := msg.Values[fieldBody].(string)
	headersJSON, _ := msg.Values[fieldHeaders].(string)
	kind, _ := msg.Values[fieldKind].(string)
	if url == "" || body == "" {
		return true
	}
	attempt := msgAttempt(msg)
	if attempt > r.maxAttempts {
		logger.Error("Hooshnics retry exhausted — moving to DLQ",
			zap.String("id", msg.ID),
			zap.Int("attempt", attempt))
		if err := r.queue.EnqueueDeadLetterAttempt(ctx, "", []byte(body), fmt.Sprintf("hooshnics retry exhausted after %d", attempt), attempt); err != nil {
			logger.Error("DLQ enqueue failed — leaving retry message pending",
				zap.String("id", msg.ID), zap.Error(err))
			return false
		}
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.IncTeltonikaDeadLetter()
			metrics.AppMetrics.IncIntegrationRetryExhausted("hooshnics")
		}
		return true
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(body)))
	if err != nil {
		return true
	}
	req.Header.Set("X-Ingest-Token", r.auth)
	req.Header.Set("X-Gps-Mirror", "gps-receiver-retry")
	req.Header.Set("Content-Type", "application/json")
	if headersJSON != "" {
		var headers map[string]string
		if gojson.Unmarshal([]byte(headersJSON), &headers) == nil {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}
	}

	resp, err := r.client.Do(req)
	if err != nil {
		r.requeueHooshnics(ctx, msg.ID, kind, url, body, headersJSON, attempt)
		return true
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		logger.Info("Hooshnics retry delivered",
			zap.String("id", msg.ID),
			zap.Int("status", resp.StatusCode),
			zap.Int("attempt", attempt))
		return true
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
		logger.Warn("Hooshnics retry permanent reject — ACK",
			zap.String("id", msg.ID),
			zap.Int("status", resp.StatusCode))
		return true
	}
	r.requeueHooshnics(ctx, msg.ID, kind, url, body, headersJSON, attempt)
	return true
}

func (r *RetryConsumers) requeueHooshnics(ctx context.Context, id, kind, url, body, headersJSON string, attempt int) {
	select {
	case <-r.ctx.Done():
		return
	case <-time.After(retryBackoff(attempt)):
	}
	next := attempt + 1
	if err := r.queue.EnqueueHooshnicsRetryAttempt(ctx, kind, url, []byte(body), headersJSON, next); err != nil {
		logger.Warn("Hooshnics retry requeue failed",
			zap.String("id", id),
			zap.Error(err))
		return
	}
	if metrics.AppMetrics != nil {
		metrics.AppMetrics.IncIntegrationRetryRequeued("hooshnics")
	}
	logger.Warn("Hooshnics retry still failing — requeued after backoff",
		zap.String("id", id),
		zap.Int("next_attempt", next),
		zap.Duration("backoff", retryBackoff(attempt)))
}
