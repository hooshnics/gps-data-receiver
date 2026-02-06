package queue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

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
	handler        MessageHandler
	consumerPrefix string
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
	activeWorkers  atomic.Int32 // Track active workers count
}

// NewConsumer creates a new consumer
func NewConsumer(queue *RedisQueue, workerCount, batchSize int, handler MessageHandler) *Consumer {
	ctx, cancel := context.WithCancel(context.Background())
	
	c := &Consumer{
		queue:          queue,
		workerCount:    workerCount,
		batchSize:      batchSize,
		handler:        handler,
		consumerPrefix: fmt.Sprintf("worker-%d", time.Now().Unix()),
		ctx:            ctx,
		cancel:         cancel,
	}
	c.activeWorkers.Store(0)
	
	return c
}

// Start starts the consumer workers
func (c *Consumer) Start() {
	logger.Info("Starting consumer workers",
		zap.Int("worker_count", c.workerCount),
		zap.Int("batch_size", c.batchSize),
		zap.String("consumer_prefix", c.consumerPrefix))

	for i := 0; i < c.workerCount; i++ {
		c.wg.Add(1)
		go c.worker(i)
	}
	
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
	
	// Panic recovery to prevent worker crashes
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
			// Wrap processMessages in a recovery block to handle any panics
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("Panic in processMessages",
							zap.Int("worker_id", workerID),
							zap.Any("panic", r))
						time.Sleep(1 * time.Second) // Back off before retrying
					}
				}()
				c.processMessages(workerID, consumerName)
			}()
		}
	}
}

// processMessages reads and processes messages from the stream
func (c *Consumer) processMessages(workerID int, consumerName string) {
	// Use a timeout for the read operation
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	// Read messages from the stream
	streams, err := c.queue.GetClient().XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.queue.GetConsumerGroup(),
		Consumer: consumerName,
		Streams:  []string{c.queue.GetStreamName(), ">"},
		Count:    int64(c.batchSize),
		Block:    2 * time.Second, // Block for 2 seconds if no messages
	}).Result()

	if err != nil {
		// Context cancelled or deadline exceeded is normal during shutdown
		if err == context.Canceled || err == context.DeadlineExceeded {
			return
		}
		// Redis nil error means no messages
		if err == redis.Nil {
			return
		}
		logger.Error("Failed to read from stream",
			zap.Int("worker_id", workerID),
			zap.String("consumer", consumerName),
			zap.Error(err))
		time.Sleep(1 * time.Second) // Back off on error
		return
	}

	// Process each message
	for _, stream := range streams {
		for _, message := range stream.Messages {
			// Check if context is cancelled before processing
			select {
			case <-c.ctx.Done():
				logger.Info("Worker stopping, skipping remaining messages",
					zap.Int("worker_id", workerID))
				return
			default:
				c.handleMessage(workerID, message)
			}
		}
	}
}

// handleMessage processes a single message
func (c *Consumer) handleMessage(workerID int, message redis.XMessage) {
	// Mark worker as active
	c.activeWorkers.Add(1)
	defer c.activeWorkers.Add(-1)
	
	// Extract data from message
	dataInterface, ok := message.Values["data"]
	if !ok {
		logger.Error("Message missing data field",
			zap.Int("worker_id", workerID),
			zap.String("message_id", message.ID))
		c.ackMessage(message.ID)
		return
	}

	// Convert to bytes
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
		c.ackMessage(message.ID)
		return
	}

	// Process the message with handler
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := c.handler(ctx, data)
	if err != nil {
		logger.Error("Failed to process message",
			zap.Int("worker_id", workerID),
			zap.String("message_id", message.ID),
			zap.Error(err))
		// Still ACK to avoid reprocessing - handler should have retry logic
		c.ackMessage(message.ID)
		return
	}

	// ACK the message
	c.ackMessage(message.ID)
	logger.Debug("Message processed successfully",
		zap.Int("worker_id", workerID),
		zap.String("message_id", message.ID))
}

// ackMessage acknowledges a message
func (c *Consumer) ackMessage(messageID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.queue.GetClient().XAck(ctx,
		c.queue.GetStreamName(),
		c.queue.GetConsumerGroup(),
		messageID).Err()

	if err != nil {
		logger.Error("Failed to ACK message",
			zap.String("message_id", messageID),
			zap.Error(err))
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

