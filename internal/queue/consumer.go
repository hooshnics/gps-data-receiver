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

// MessageHandler is a function type that processes a message
type MessageHandler func(ctx context.Context, data []byte) error

// Consumer handles consuming messages from Redis Stream
type Consumer struct {
	queue          *RedisQueue
	workerCount    int
	batchSize      int
	maxRetries     int
	handler        MessageHandler
	consumerPrefix string
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
	activeWorkers  atomic.Int32
}

// NewConsumer creates a new consumer. maxRetries controls how many total
// delivery attempts a message gets before being permanently dropped.
func NewConsumer(queue *RedisQueue, workerCount, batchSize, maxRetries int, handler MessageHandler) *Consumer {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Consumer{
		queue:          queue,
		workerCount:    workerCount,
		batchSize:      batchSize,
		maxRetries:     maxRetries,
		handler:        handler,
		consumerPrefix: fmt.Sprintf("worker-%d", time.Now().Unix()),
		ctx:            ctx,
		cancel:         cancel,
	}
	c.activeWorkers.Store(0)

	return c
}

// Start starts the consumer workers and the pending message reclaimer.
func (c *Consumer) Start() {
	logger.Info("Starting consumer workers",
		zap.Int("worker_count", c.workerCount),
		zap.Int("batch_size", c.batchSize),
		zap.String("consumer_prefix", c.consumerPrefix))

	for i := 0; i < c.workerCount; i++ {
		c.wg.Add(1)
		go c.worker(i)
	}

	c.wg.Add(1)
	go c.reclaimPending()

	logger.Info("All consumer workers started successfully",
		zap.Int("worker_count", c.workerCount))
}

// Stop gracefully stops all workers
func (c *Consumer) Stop() {
	logger.Info("Stopping consumer workers...")
	c.cancel()
	c.wg.Wait()
	logger.Info("All consumer workers stopped")
}

// worker is the main worker loop
func (c *Consumer) worker(workerID int) {
	defer c.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			logger.Error("Worker panic recovered",
				zap.Int("worker_id", workerID),
				zap.Any("panic", r))
		}
	}()

	consumerName := fmt.Sprintf("%s-%d", c.consumerPrefix, workerID)
	logger.Info("Worker started", zap.Int("worker_id", workerID), zap.String("consumer", consumerName))

	for {
		select {
		case <-c.ctx.Done():
			logger.Info("Worker shutting down", zap.Int("worker_id", workerID))
			return
		default:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("Panic in processMessages",
							zap.Int("worker_id", workerID),
							zap.Any("panic", r))
						time.Sleep(1 * time.Second)
					}
				}()
				c.processMessages(workerID, consumerName)
			}()
		}
	}
}

// processMessages reads a batch and processes all messages concurrently, then batch-ACKs.
func (c *Consumer) processMessages(workerID int, consumerName string) {
	streams, err := c.queue.GetClient().XReadGroup(c.ctx, &redis.XReadGroupArgs{
		Group:    c.queue.GetConsumerGroup(),
		Consumer: consumerName,
		Streams:  []string{c.queue.GetStreamName(), ">"},
		Count:    int64(c.batchSize),
		Block:    2 * time.Second,
	}).Result()

	if err != nil {
		if err == context.Canceled || err == context.DeadlineExceeded || err == redis.Nil {
			return
		}
		// Stream or consumer group was deleted (e.g. Redis restart/FLUSHDB). Recreate and retry next iteration.
		if strings.Contains(err.Error(), "NOGROUP") {
			if ensureErr := c.queue.EnsureConsumerGroup(c.ctx); ensureErr != nil {
				logger.Error("Failed to recreate consumer group after NOGROUP",
					zap.Int("worker_id", workerID),
					zap.Error(ensureErr))
			} else {
				logger.Info("Recreated consumer group after NOGROUP",
					zap.Int("worker_id", workerID),
					zap.String("stream", c.queue.GetStreamName()),
					zap.String("group", c.queue.GetConsumerGroup()))
			}
			time.Sleep(1 * time.Second)
			return
		}
		logger.Error("Failed to read from stream",
			zap.Int("worker_id", workerID),
			zap.String("consumer", consumerName),
			zap.Error(err))
		time.Sleep(1 * time.Second)
		return
	}

	for _, stream := range streams {
		if len(stream.Messages) == 0 {
			continue
		}
		c.processBatchConcurrently(workerID, stream.Messages)
	}
}

// processBatchConcurrently fans out message handling across goroutines, collects
// completed IDs, and batch-ACKs them in a single Redis pipeline call.
func (c *Consumer) processBatchConcurrently(workerID int, messages []redis.XMessage) {
	var mu sync.Mutex
	ackIDs := make([]string, 0, len(messages))

	var batchWg sync.WaitGroup
	for _, message := range messages {
		if c.ctx.Err() != nil {
			break
		}

		batchWg.Add(1)
		go func(msg redis.XMessage) {
			defer batchWg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Panic handling message",
						zap.Int("worker_id", workerID),
						zap.String("message_id", msg.ID),
						zap.Any("panic", r))
				}
			}()

			if c.handleMessage(workerID, msg) {
				mu.Lock()
				ackIDs = append(ackIDs, msg.ID)
				mu.Unlock()
			}
		}(message)
	}

	batchWg.Wait()
	c.batchAck(ackIDs)
}

// handleMessage processes a single message and returns true if it should be ACKed.
func (c *Consumer) handleMessage(workerID int, message redis.XMessage) bool {
	c.activeWorkers.Add(1)
	defer c.activeWorkers.Add(-1)

	if metrics.AppMetrics != nil {
		metrics.AppMetrics.IncActiveWorker()
		defer metrics.AppMetrics.DecActiveWorker()
	}

	dataInterface, ok := message.Values["data"]
	if !ok {
		logger.Error("Message missing data field",
			zap.Int("worker_id", workerID),
			zap.String("message_id", message.ID))
		return true
	}

	var data []byte
	switch v := dataInterface.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		logger.Error("Invalid data type in message",
			zap.Int("worker_id", workerID),
			zap.String("message_id", message.ID))
		return true
	}

	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	err := c.handler(ctx, data)
	if err != nil {
		logger.Warn("Message processing failed, leaving in queue for redelivery",
			zap.Int("worker_id", workerID),
			zap.String("message_id", message.ID),
			zap.Error(err))
		return false
	}

	return true
}

// batchAck ACKs multiple messages in a single Redis pipeline call.
func (c *Consumer) batchAck(ids []string) {
	if len(ids) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pipe := c.queue.GetClient().Pipeline()
	pipe.XAck(ctx, c.queue.GetStreamName(), c.queue.GetConsumerGroup(), ids...)
	pipe.XDel(ctx, c.queue.GetStreamName(), ids...)

	_, err := pipe.Exec(ctx)
	if err != nil {
		logger.Error("Failed to batch ACK messages",
			zap.Int("count", len(ids)),
			zap.Error(err))
	}
}

// reclaimPending periodically claims messages that were delivered but never ACKed (e.g. crashed workers).
func (c *Consumer) reclaimPending() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	consumerName := fmt.Sprintf("%s-reclaimer", c.consumerPrefix)

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.claimStaleMessages(consumerName)
		}
	}
}

// claimStaleMessages drops messages that exceeded the max delivery count,
// then claims remaining stale messages for reprocessing.
func (c *Consumer) claimStaleMessages(consumerName string) {
	c.dropExcessiveRetries()

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	messages, _, err := c.queue.GetClient().XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   c.queue.GetStreamName(),
		Group:    c.queue.GetConsumerGroup(),
		Consumer: consumerName,
		MinIdle:  60 * time.Second,
		Start:    "0-0",
		Count:    int64(c.batchSize),
	}).Result()

	if err != nil {
		if err != context.Canceled && err != context.DeadlineExceeded {
			logger.Debug("Failed to auto-claim pending messages", zap.Error(err))
		}
		return
	}

	if len(messages) == 0 {
		return
	}

	logger.Info("Reclaiming stale pending messages", zap.Int("count", len(messages)))
	c.processBatchConcurrently(-1, messages)
}

// dropExcessiveRetries finds pending messages whose delivery count has reached
// maxRetries and permanently removes them from the stream.
func (c *Consumer) dropExcessiveRetries() {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	pending, err := c.queue.GetClient().XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: c.queue.GetStreamName(),
		Group:  c.queue.GetConsumerGroup(),
		Start:  "-",
		End:    "+",
		Count:  200,
	}).Result()

	if err != nil {
		if err != context.Canceled && err != context.DeadlineExceeded {
			logger.Debug("Failed to inspect pending entries", zap.Error(err))
		}
		return
	}

	var deadIDs []string
	for _, p := range pending {
		if p.RetryCount >= int64(c.maxRetries) {
			deadIDs = append(deadIDs, p.ID)
		}
	}

	if len(deadIDs) > 0 {
		logger.Warn("Dropping messages that exceeded max delivery attempts",
			zap.Int("count", len(deadIDs)),
			zap.Int("max_retries", c.maxRetries))
		c.batchAck(deadIDs)
	}
}

// GetActiveWorkerCount returns the current number of active workers
func (c *Consumer) GetActiveWorkerCount() int {
	return int(c.activeWorkers.Load())
}

// GetWorkerPoolSize returns the total number of workers in the pool
func (c *Consumer) GetWorkerPoolSize() int {
	return c.workerCount
}
