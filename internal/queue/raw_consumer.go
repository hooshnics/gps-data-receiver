package queue

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RawHandler processes one durable raw AVL message. Return nil to ACK the stream entry.
type RawHandler func(ctx context.Context, imei string, frame []byte) error

// StreamArchiver optionally persists raw frames after successful processing (not on TCP ACK path).
type StreamArchiver interface {
	ArchiveEntry(ctx context.Context, e ArchiveEntry) error
	Strict() bool
}

// ArchiveEntry is the async raw persistence contract (never on ACK path).
type ArchiveEntry struct {
	ID              string
	IMEI            string
	FrameHex        string
	ReceivedAt      string
	Source          string
	DeviceType      string
	Protocol        string
	ProtocolVersion string
	SourceIP        string
}

// RawConsumer reads gps:raw_incoming via a consumer group, dedupes, then invokes handler.
type RawConsumer struct {
	queue       *RedisQueue
	workerCount int
	batchSize   int
	handler     RawHandler
	archiver    StreamArchiver
	prefix      string
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	active      atomic.Int32
}

// NewRawConsumer creates a consumer for the durable Teltonika raw stream.
func NewRawConsumer(q *RedisQueue, workerCount, batchSize int, handler RawHandler) *RawConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	if workerCount <= 0 {
		workerCount = 10
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	return &RawConsumer{
		queue:       q,
		workerCount: workerCount,
		batchSize:   batchSize,
		handler:     handler,
		prefix:      fmt.Sprintf("raw-%d", time.Now().Unix()),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetArchiver attaches an optional post-process archive sink (local disk NDJSON.gz).
func (c *RawConsumer) SetArchiver(a StreamArchiver) {
	if c != nil {
		c.archiver = a
	}
}

// Start launches worker goroutines and a pending reclaimer.
func (c *RawConsumer) Start() {
	logger.Info("Starting raw ingest consumers",
		zap.Int("workers", c.workerCount),
		zap.String("stream", c.queue.GetRawStreamName()),
		zap.String("group", c.queue.GetRawConsumerGroup()))
	for i := 0; i < c.workerCount; i++ {
		c.wg.Add(1)
		go c.worker(i)
	}
	c.wg.Add(1)
	go c.reclaimPending()
}

// Stop cancels workers and waits for drain.
func (c *RawConsumer) Stop() {
	c.cancel()
	c.wg.Wait()
}

func (c *RawConsumer) worker(id int) {
	defer c.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Raw consumer worker panic recovered",
				zap.Int("worker", id),
				zap.Any("panic", r))
		}
	}()
	name := fmt.Sprintf("%s-%d", c.prefix, id)
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("Panic in raw readOnce",
							zap.Int("worker", id),
							zap.Any("panic", r))
						time.Sleep(time.Second)
					}
				}()
				c.readOnce(id, name)
			}()
		}
	}
}

func (c *RawConsumer) readOnce(workerID int, consumerName string) {
	streams, err := c.queue.GetClient().XReadGroup(c.ctx, &redis.XReadGroupArgs{
		Group:    c.queue.GetRawConsumerGroup(),
		Consumer: consumerName,
		Streams:  []string{c.queue.GetRawStreamName(), ">"},
		Count:    int64(c.batchSize),
		Block:    2 * time.Second,
	}).Result()
	if err != nil {
		if err == context.Canceled || err == redis.Nil {
			return
		}
		if strings.Contains(err.Error(), "NOGROUP") {
			_ = c.queue.EnsureDurableStreams(c.ctx)
			time.Sleep(time.Second)
			return
		}
		logger.Error("Raw stream read failed", zap.Int("worker", workerID), zap.Error(err))
		time.Sleep(time.Second)
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			if c.handle(workerID, msg) {
				c.ack(msg.ID)
			}
		}
	}
}

func (c *RawConsumer) handle(workerID int, msg redis.XMessage) bool {
	c.active.Add(1)
	defer c.active.Add(-1)

	imei, _ := msg.Values[fieldIMEI].(string)
	frameHex, _ := msg.Values[fieldFrameHex].(string)
	frame, err := DecodeRawFrameHex(frameHex)
	if err != nil || len(frame) == 0 {
		logger.Error("Raw message invalid frame_hex — DLQ required before ACK",
			zap.String("id", msg.ID),
			zap.Error(err))
		if dlErr := c.queue.EnqueueDeadLetter(c.ctx, imei, nil, "invalid frame_hex"); dlErr != nil {
			logger.Error("DLQ enqueue failed — leaving raw message pending",
				zap.String("id", msg.ID),
				zap.Error(dlErr))
			return false
		}
		return true
	}

	// Idempotency: device may resend after lost TCP ACK.
	first, hash, err := c.queue.ClaimFirstSighting(c.ctx, frame)
	claimed := false
	if err != nil {
		logger.Warn("Dedupe check failed — processing anyway",
			zap.String("id", msg.ID),
			zap.Error(err))
	} else if !first {
		logger.Info("Duplicate raw AVL skipped (idempotent)",
			zap.String("imei", imei),
			zap.String("hash", hash),
			zap.String("id", msg.ID))
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.IncTeltonikaDuplicate()
		}
		return true
	} else {
		claimed = true
	}

	ctx, cancel := context.WithTimeout(c.ctx, 45*time.Second)
	defer cancel()

	if err := c.handler(ctx, imei, frame); err != nil {
		logger.Warn("Raw handler failed — leaving pending for reclaim",
			zap.Int("worker", workerID),
			zap.String("id", msg.ID),
			zap.Error(err))
		if claimed {
			if relErr := c.queue.ReleaseFirstSighting(context.Background(), frame); relErr != nil {
				logger.Error("Failed to release frame dedupe claim after handler error",
					zap.String("id", msg.ID),
					zap.String("hash", hash),
					zap.Error(relErr))
			}
		}
		return false
	}

	if c.archiver != nil {
		receivedAt, _ := msg.Values[fieldReceivedAt].(string)
		source, _ := msg.Values[fieldSource].(string)
		deviceType, _ := msg.Values[fieldDeviceType].(string)
		protocol, _ := msg.Values[fieldProtocol].(string)
		protoVer, _ := msg.Values[fieldProtocolVersion].(string)
		sourceIP, _ := msg.Values[fieldSourceIP].(string)
		if aerr := c.archiver.ArchiveEntry(ctx, ArchiveEntry{
			ID: msg.ID, IMEI: imei, FrameHex: frameHex,
			ReceivedAt: receivedAt, Source: source,
			DeviceType: deviceType, Protocol: protocol,
			ProtocolVersion: protoVer, SourceIP: sourceIP,
		}); aerr != nil {
			logger.Error("Raw archive write failed",
				zap.String("id", msg.ID),
				zap.String("imei", imei),
				zap.Error(aerr))
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.IncRawStorageFailure()
			}
			// Strict: leave pending for reclaim (retry). Non-strict: park a DLQ copy so the
			// frame remains recoverable after XACK, without blocking TCP ACK path.
			if c.archiver.Strict() {
				if claimed {
					_ = c.queue.ReleaseFirstSighting(context.Background(), frame)
				}
				return false
			}
			if dlErr := c.queue.EnqueueDeadLetter(ctx, imei, frame, "archive write failed: "+aerr.Error()); dlErr != nil {
				logger.Error("Archive failure DLQ park failed — leaving raw message pending",
					zap.String("id", msg.ID),
					zap.Error(dlErr))
				if claimed {
					_ = c.queue.ReleaseFirstSighting(context.Background(), frame)
				}
				return false
			}
		}
	}
	return true
}

func (c *RawConsumer) ack(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pipe := c.queue.GetClient().Pipeline()
	pipe.XAck(ctx, c.queue.GetRawStreamName(), c.queue.GetRawConsumerGroup(), id)
	pipe.XDel(ctx, c.queue.GetRawStreamName(), id)
	if _, err := pipe.Exec(ctx); err != nil {
		logger.Error("Raw stream ACK failed", zap.String("id", id), zap.Error(err))
	}
}

func (c *RawConsumer) reclaimPending() {
	defer c.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	name := c.prefix + "-reclaimer"
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
			msgs, _, err := c.queue.GetClient().XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   c.queue.GetRawStreamName(),
				Group:    c.queue.GetRawConsumerGroup(),
				Consumer: name,
				MinIdle:  60 * time.Second,
				Start:    "0-0",
				Count:    int64(c.batchSize),
			}).Result()
			cancel()
			if err != nil || len(msgs) == 0 {
				continue
			}
			logger.Info("Reclaiming stale raw messages", zap.Int("count", len(msgs)))
			for _, msg := range msgs {
				if c.handle(-1, msg) {
					c.ack(msg.ID)
				}
			}
		}
	}
}
