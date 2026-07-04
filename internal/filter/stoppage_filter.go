package filter

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	redisKeyPrefix      = "stoppage_state:"
	defaultSyncInterval = 30 * time.Second
	shardCount          = 256
)

type stoppageShard struct {
	mu    sync.RWMutex
	state map[string]bool
}

// StoppageFilter filters redundant stoppage data to prevent sending duplicate
// stoppage records to destination servers. It maintains state per IMEI to track
// whether the last sent record was a stoppage or movement.
type StoppageFilter struct {
	redis        *redis.Client
	shards       [shardCount]stoppageShard
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	syncTicker   *time.Ticker
	enabled      bool
	syncInterval time.Duration
}

func shardIndex(imei string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(imei))
	return int(h.Sum32() % shardCount)
}

func (sf *StoppageFilter) shardForIMEI(imei string) *stoppageShard {
	return &sf.shards[shardIndex(imei)]
}

// NewStoppageFilter creates a new stoppage filter with Redis-backed state
func NewStoppageFilter(redisClient *redis.Client, enabled bool, syncInterval time.Duration) *StoppageFilter {
	if syncInterval == 0 {
		syncInterval = defaultSyncInterval
	}

	ctx, cancel := context.WithCancel(context.Background())

	filter := &StoppageFilter{
		redis:        redisClient,
		ctx:          ctx,
		cancel:       cancel,
		enabled:      enabled,
		syncInterval: syncInterval,
	}

	for i := range filter.shards {
		filter.shards[i].state = make(map[string]bool)
	}

	// Load existing state from Redis on startup
	filter.loadFromRedis()

	return filter
}

// Start starts the background goroutine that periodically syncs state to Redis
func (sf *StoppageFilter) Start() {
	if !sf.enabled {
		logger.Info("Stoppage filter is disabled")
		return
	}

	sf.syncTicker = time.NewTicker(sf.syncInterval)
	sf.wg.Add(1)

	go func() {
		defer sf.wg.Done()
		defer sf.syncTicker.Stop()

		logger.Info("Stoppage filter started",
			zap.Duration("sync_interval", sf.syncInterval))

		for {
			select {
			case <-sf.ctx.Done():
				logger.Info("Stoppage filter shutting down")
				// Final sync before shutdown
				sf.syncToRedis()
				return
			case <-sf.syncTicker.C:
				sf.syncToRedis()
			}
		}
	}()
}

// Close gracefully stops the filter and performs final state sync
func (sf *StoppageFilter) Close() {
	if !sf.enabled {
		return
	}

	logger.Info("Stopping stoppage filter...")
	sf.cancel()
	sf.wg.Wait()
	logger.Info("Stoppage filter stopped")
}

// FilterData filters GPS data records to remove redundant stoppage entries.
// For each IMEI, only the first stoppage after movement is kept.
// All movement records are always kept.
func (sf *StoppageFilter) FilterData(data []parser.ParsedGPSData) []parser.ParsedGPSData {
	if !sf.enabled {
		return data
	}

	if len(data) == 0 {
		return data
	}

	filtered := make([]parser.ParsedGPSData, 0, len(data))

	for _, record := range data {
		imei := record.IMEI
		shard := sf.shardForIMEI(imei)
		isCurrentStoppage := sf.isStoppage(record)

		shard.mu.Lock()
		lastWasStoppage, exists := shard.state[imei]

		if !isCurrentStoppage {
			filtered = append(filtered, record)
			shard.state[imei] = false
			logger.Debug("Movement record - sending",
				zap.String("imei", imei),
				zap.Int("speed", record.Speed))
		} else if !exists || !lastWasStoppage {
			filtered = append(filtered, record)
			shard.state[imei] = true
			logger.Debug("First stoppage after movement - sending",
				zap.String("imei", imei),
				zap.Int("speed", record.Speed))
		} else {
			logger.Debug("Redundant stoppage - filtering",
				zap.String("imei", imei),
				zap.Int("speed", record.Speed))
		}
		shard.mu.Unlock()
	}

	if len(filtered) < len(data) {
		logger.Info("Filtered redundant stoppage records",
			zap.Int("original_count", len(data)),
			zap.Int("filtered_count", len(filtered)),
			zap.Int("removed_count", len(data)-len(filtered)))
	}

	return filtered
}

// isStoppage checks if a GPS record represents a stoppage (speed == 0)
func (sf *StoppageFilter) isStoppage(record parser.ParsedGPSData) bool {
	return record.Speed == 0
}

// syncToRedis persists the current state to Redis for durability
func (sf *StoppageFilter) syncToRedis() {
	stateCopy := make(map[string]bool)
	for i := range sf.shards {
		shard := &sf.shards[i]
		shard.mu.RLock()
		for k, v := range shard.state {
			stateCopy[k] = v
		}
		shard.mu.RUnlock()
	}

	if len(stateCopy) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pipe := sf.redis.Pipeline()
	syncedCount := 0

	for imei, isStoppage := range stateCopy {
		key := redisKeyPrefix + imei
		var value string
		if isStoppage {
			value = "1"
		} else {
			value = "0"
		}
		pipe.Set(ctx, key, value, 24*time.Hour) // TTL: 24 hours
		syncedCount++
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		logger.Error("Failed to sync stoppage state to Redis",
			zap.Error(err),
			zap.Int("attempted_count", syncedCount))
		return
	}

	logger.Debug("Synced stoppage state to Redis",
		zap.Int("imei_count", syncedCount))
}

// loadFromRedis loads existing state from Redis on startup
func (sf *StoppageFilter) loadFromRedis() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Scan for all stoppage_state:* keys
	var cursor uint64
	loadedCount := 0

	for {
		keys, nextCursor, err := sf.redis.Scan(ctx, cursor, redisKeyPrefix+"*", 100).Result()
		if err != nil {
			logger.Error("Failed to scan Redis for stoppage state",
				zap.Error(err))
			return
		}

		if len(keys) > 0 {
			// Get values for all keys in batch
			values, err := sf.redis.MGet(ctx, keys...).Result()
			if err != nil {
				logger.Error("Failed to load stoppage state from Redis",
					zap.Error(err))
				return
			}

			for i, key := range keys {
				imei := key[len(redisKeyPrefix):]
				if values[i] == nil {
					continue
				}
				strVal, ok := values[i].(string)
				if !ok {
					continue
				}

				shard := sf.shardForIMEI(imei)
				shard.mu.Lock()
				shard.state[imei] = (strVal == "1")
				shard.mu.Unlock()
				loadedCount++
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if loadedCount > 0 {
		logger.Info("Loaded stoppage state from Redis",
			zap.Int("imei_count", loadedCount))
	} else {
		logger.Info("No existing stoppage state found in Redis")
	}
}

// GetStateCount returns the number of IMEIs being tracked (for monitoring)
func (sf *StoppageFilter) GetStateCount() int {
	total := 0
	for i := range sf.shards {
		shard := &sf.shards[i]
		shard.mu.RLock()
		total += len(shard.state)
		shard.mu.RUnlock()
	}
	return total
}

// ClearState clears all state (useful for testing)
func (sf *StoppageFilter) ClearState() {
	for i := range sf.shards {
		shard := &sf.shards[i]
		shard.mu.Lock()
		shard.state = make(map[string]bool)
		shard.mu.Unlock()
	}
	logger.Info("Cleared stoppage filter state")
}
