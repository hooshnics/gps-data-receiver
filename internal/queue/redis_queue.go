package queue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RedisQueue handles Redis Stream operations
type RedisQueue struct {
	client        *redis.Client
	streamName    string
	consumerGroup string
	maxLen        int64
}

// NewRedisQueue creates a new Redis queue
func NewRedisQueue(cfg *config.RedisConfig) (*RedisQueue, error) {
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 200
	}

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.GetAddr(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     poolSize,
		MinIdleConns: poolSize / 4,
		PoolTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	queue := &RedisQueue{
		client:        client,
		streamName:    cfg.StreamName,
		consumerGroup: cfg.ConsumerGroup,
		maxLen:        cfg.MaxLen,
	}

	// Ensure stream and consumer group exist (retry on transient failure)
	if err := queue.ensureConsumerGroupWithRetry(context.Background(), 3, 2*time.Second); err != nil {
		return nil, fmt.Errorf("consumer group init: %w", err)
	}

	logger.Info("Redis queue initialized",
		zap.String("stream", queue.streamName),
		zap.String("consumer_group", queue.consumerGroup),
		zap.Int64("max_len", queue.maxLen),
		zap.Int("pool_size", poolSize))

	return queue, nil
}

// EnqueueWithDepth atomically enqueues a message and returns the current stream length
// using a Redis pipeline (single round-trip).
func (q *RedisQueue) EnqueueWithDepth(ctx context.Context, data []byte) (messageID string, depth int64, err error) {
	pipe := q.client.Pipeline()

	addCmd := pipe.XAdd(ctx, &redis.XAddArgs{
		Stream: q.streamName,
		MaxLen: q.maxLen,
		Approx: true,
		Values: map[string]interface{}{
			"data":      data,
			"timestamp": time.Now().Unix(),
		},
	})

	lenCmd := pipe.XLen(ctx, q.streamName)

	_, err = pipe.Exec(ctx)
	if err != nil {
		logger.Error("Failed to enqueue message (pipeline)", zap.Error(err))
		return "", 0, fmt.Errorf("failed to enqueue: %w", err)
	}

	messageID = addCmd.Val()
	depth = lenCmd.Val()

	logger.Debug("Message enqueued", zap.String("message_id", messageID), zap.Int64("depth", depth))
	return messageID, depth, nil
}

// Enqueue adds a message to the Redis Stream
func (q *RedisQueue) Enqueue(ctx context.Context, data []byte) (string, error) {
	result, err := q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.streamName,
		MaxLen: q.maxLen,
		Approx: true,
		Values: map[string]interface{}{
			"data":      data,
			"timestamp": time.Now().Unix(),
		},
	}).Result()

	if err != nil {
		logger.Error("Failed to enqueue message", zap.Error(err))
		return "", fmt.Errorf("failed to enqueue: %w", err)
	}

	logger.Debug("Message enqueued", zap.String("message_id", result))
	return result, nil
}

// Close closes the Redis connection
func (q *RedisQueue) Close() error {
	return q.client.Close()
}

// GetClient returns the Redis client (needed by consumer)
func (q *RedisQueue) GetClient() *redis.Client {
	return q.client
}

// GetStreamName returns the stream name
func (q *RedisQueue) GetStreamName() string {
	return q.streamName
}

// GetConsumerGroup returns the consumer group name
func (q *RedisQueue) GetConsumerGroup() string {
	return q.consumerGroup
}

// GetMaxLen returns the maximum stream length (for backpressure)
func (q *RedisQueue) GetMaxLen() int64 {
	return q.maxLen
}

// Len returns the current number of entries in the stream (queue depth)
func (q *RedisQueue) Len(ctx context.Context) (int64, error) {
	return q.client.XLen(ctx, q.streamName).Result()
}

// EnsureConsumerGroup creates the stream and consumer group if they do not exist.
// Idempotent: safe to call on every NOGROUP. Returns nil on success or if group already exists (BUSYGROUP).
func (q *RedisQueue) EnsureConsumerGroup(ctx context.Context) error {
	err := q.client.XGroupCreateMkStream(ctx, q.streamName, q.consumerGroup, "0").Err()
	if err == nil {
		logger.Info("Consumer group created",
			zap.String("stream", q.streamName),
			zap.String("consumer_group", q.consumerGroup))
		return nil
	}
	if strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

// ensureConsumerGroupWithRetry creates the consumer group with retries (for startup).
func (q *RedisQueue) ensureConsumerGroupWithRetry(ctx context.Context, attempts int, backoff time.Duration) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(backoff)
		}
		lastErr = q.EnsureConsumerGroup(ctx)
		if lastErr == nil {
			if i == 0 {
				logger.Info("Consumer group ready",
					zap.String("stream", q.streamName),
					zap.String("consumer_group", q.consumerGroup))
			}
			return nil
		}
		logger.Warn("Consumer group create attempt failed, will retry",
			zap.Int("attempt", i+1),
			zap.Int("max_attempts", attempts),
			zap.Error(lastErr))
	}
	return lastErr
}
