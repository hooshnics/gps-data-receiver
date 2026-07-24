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

// RedisQueue handles Redis Stream operations for HTTP reports and durable Teltonika ingest.
type RedisQueue struct {
	client        *redis.Client
	streamName    string
	consumerGroup string
	maxLen        int64

	rawStream      string
	rawGroup       string
	rawMaxLen      int64
	deadLetterStream string
	hooshnicsRetryStream string
	hooshnicsRetryGroup  string
	pistatRetryStream    string
	pistatRetryGroup     string
	dedupeTTL            time.Duration
}

// NewRedisQueue creates a new Redis queue and ensures all durable streams/groups exist.
func NewRedisQueue(cfg *config.RedisConfig) (*RedisQueue, error) {
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 200
	}

	var client *redis.Client
	if cfg.UseSentinel() {
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    cfg.SentinelMaster,
			SentinelAddrs: cfg.SentinelAddrs,
			Password:      cfg.Password,
			DB:            cfg.DB,
			PoolSize:      poolSize,
			MinIdleConns:  poolSize / 4,
			PoolTimeout:   10 * time.Second,
			ReadTimeout:   5 * time.Second,
			WriteTimeout:  5 * time.Second,
			DialTimeout:   5 * time.Second,
		})
		logger.Info("Redis client using Sentinel failover",
			zap.String("master", cfg.SentinelMaster),
			zap.Strings("sentinels", cfg.SentinelAddrs))
	} else {
		client = redis.NewClient(&redis.Options{
			Addr:         cfg.GetAddr(),
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     poolSize,
			MinIdleConns: poolSize / 4,
			PoolTimeout:  10 * time.Second,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			DialTimeout:  5 * time.Second,
		})
		logger.Info("Redis client using standalone address", zap.String("addr", cfg.GetAddr()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	rawStream := cfg.RawStreamName
	if rawStream == "" {
		rawStream = "gps:raw_incoming"
	}
	rawGroup := cfg.RawConsumerGroup
	if rawGroup == "" {
		rawGroup = "gps-raw-workers"
	}
	deadLetter := cfg.DeadLetterStream
	if deadLetter == "" {
		deadLetter = "gps:dead_letter"
	}
	hooshRetry := cfg.HooshnicsRetryStream
	if hooshRetry == "" {
		hooshRetry = "gps:hooshnics_retry"
	}
	pistatRetry := cfg.PistatRetryStream
	if pistatRetry == "" {
		pistatRetry = "gps:pistat_retry"
	}
	dedupeTTL := cfg.DedupeTTL
	if dedupeTTL <= 0 {
		dedupeTTL = 24 * time.Hour
	}

	queue := &RedisQueue{
		client:               client,
		streamName:           cfg.StreamName,
		consumerGroup:        cfg.ConsumerGroup,
		maxLen:               cfg.MaxLen,
		rawStream:            rawStream,
		rawGroup:             rawGroup,
		rawMaxLen:            cfg.RawMaxLen,
		deadLetterStream:     deadLetter,
		hooshnicsRetryStream: hooshRetry,
		hooshnicsRetryGroup:  "gps-hooshnics-retry-workers",
		pistatRetryStream:    pistatRetry,
		pistatRetryGroup:     "gps-pistat-retry-workers",
		dedupeTTL:            dedupeTTL,
	}

	if err := queue.ensureConsumerGroupWithRetry(context.Background(), 3, 2*time.Second); err != nil {
		return nil, fmt.Errorf("consumer group init: %w", err)
	}
	if err := queue.EnsureDurableStreams(context.Background()); err != nil {
		return nil, fmt.Errorf("durable streams init: %w", err)
	}

	logger.Info("Redis queue initialized",
		zap.String("stream", queue.streamName),
		zap.String("consumer_group", queue.consumerGroup),
		zap.String("raw_stream", queue.rawStream),
		zap.String("raw_group", queue.rawGroup),
		zap.Int64("max_len", queue.maxLen),
		zap.Int64("raw_max_len", queue.rawMaxLen),
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

// Enqueue adds a message to the Redis Stream (HTTP / legacy parsed path).
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
