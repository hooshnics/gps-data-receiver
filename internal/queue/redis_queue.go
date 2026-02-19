package queue

import (
	"context"
	"fmt"
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
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.GetAddr(),
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: 100, // High pool size for performance
	})

	// Test connection
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

	// Create consumer group (ignore error if already exists)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	
	err := client.XGroupCreateMkStream(ctx2, queue.streamName, queue.consumerGroup, "0").Err()
	if err != nil {
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			logger.Info("Consumer group already exists (this is normal)", 
				zap.String("consumer_group", queue.consumerGroup))
		} else {
			logger.Warn("Failed to create consumer group", zap.Error(err))
		}
	} else {
		logger.Info("Consumer group created successfully",
			zap.String("consumer_group", queue.consumerGroup))
	}

	logger.Info("Redis queue initialized",
		zap.String("stream", queue.streamName),
		zap.String("consumer_group", queue.consumerGroup),
		zap.Int64("max_len", queue.maxLen))

	return queue, nil
}

// Enqueue adds a message to the Redis Stream
func (q *RedisQueue) Enqueue(ctx context.Context, data []byte) (string, error) {
	// Add to stream with MAXLEN for memory management
	result, err := q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.streamName,
		MaxLen: q.maxLen,
		Approx: true, // Use approximate trimming for better performance
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

