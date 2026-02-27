package filter

import (
	"context"
	"sync"
	"time"

	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// StoppageFilter filters redundant stoppage data to prevent sending duplicate
// stoppage records to destination servers. It maintains state per IMEI to track
// whether the last sent record was a stoppage or movement.
type StoppageFilter struct {
	redis        *redis.Client
	state        map[string]bool // IMEI -> last was stoppage
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	syncTicker   *time.Ticker
	enabled      bool
	syncInterval time.Duration
}

const (
	redisKeyPrefix      = "stoppage_state:"
	defaultSyncInterval = 30 * time.Second
)

// NewStoppageFilter creates a new stoppage filter with Redis-backed state
func NewStoppageFilter(redisClient *redis.Client, enabled bool, syncInterval time.Duration) *StoppageFilter {
	if syncInterval == 0 {
		syncInterval = defaultSyncInterval
	}

	ctx, cancel := context.WithCancel(context.Background())

	filter := &StoppageFilter{
		redis:        redisClient,
		state:        make(map[string]bool),
		ctx:          ctx,
		cancel:       cancel,
		enabled:      enabled,
		syncInterval: syncInterval,
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

	sf.mu.Lock()
	defer sf.mu.Unlock()

	for _, record := range data {
		imei := record.IMEI
		isCurrentStoppage := sf.isStoppage(record)
		lastWasStoppage, exists := sf.state[imei]

		// Decision logic:
		// - If movement: always send and update state
		// - If stoppage and last was movement (or no prior state): send and update state
		// - If stoppage and last was also stoppage: filter out (don't send)

		if !isCurrentStoppage {
			// Current is movement: always send
			filtered = append(filtered, record)
			sf.state[imei] = false // Update: last is now movement
			logger.Debug("Movement record - sending",
				zap.String("imei", imei),
				zap.Int("speed", record.Speed))
		} else {
			// Current is stoppage
			if !exists || !lastWasStoppage {
				// First stoppage or stoppage after movement: send
				filtered = append(filtered, record)
				sf.state[imei] = true // Update: last is now stoppage
				logger.Debug("First stoppage after movement - sending",
					zap.String("imei", imei),
					zap.Int("speed", record.Speed))
			} else {
				// Redundant stoppage: filter out
				logger.Debug("Redundant stoppage - filtering",
					zap.String("imei", imei),
					zap.Int("speed", record.Speed))
			}
		}
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
	sf.mu.RLock()
	stateCopy := make(map[string]bool, len(sf.state))
	for k, v := range sf.state {
		stateCopy[k] = v
	}
	sf.mu.RUnlock()

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

			sf.mu.Lock()
			for i, key := range keys {
				// Extract IMEI from key (remove prefix)
				imei := key[len(redisKeyPrefix):]

				if values[i] != nil {
					if strVal, ok := values[i].(string); ok {
						sf.state[imei] = (strVal == "1")
						loadedCount++
					}
				}
			}
			sf.mu.Unlock()
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
	sf.mu.RLock()
	defer sf.mu.RUnlock()
	return len(sf.state)
}

// ClearState clears all state (useful for testing)
func (sf *StoppageFilter) ClearState() {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	sf.state = make(map[string]bool)
	logger.Info("Cleared stoppage filter state")
}
