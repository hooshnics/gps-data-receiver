package filter

import (
	"context"
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/parser"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRedis creates a test Redis client (can use miniredis for unit tests)
func setupTestRedis(t *testing.T) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15, // Use a different DB for tests
	})

	// Clean up test keys before test
	ctx := context.Background()
	keys, _ := client.Keys(ctx, redisKeyPrefix+"*").Result()
	if len(keys) > 0 {
		client.Del(ctx, keys...)
	}

	t.Cleanup(func() {
		// Clean up after test
		keys, _ := client.Keys(ctx, redisKeyPrefix+"*").Result()
		if len(keys) > 0 {
			client.Del(ctx, keys...)
		}
		client.Close()
	})

	return client
}

// TestNewStoppageFilter tests filter initialization
func TestNewStoppageFilter(t *testing.T) {
	client := setupTestRedis(t)

	filter := NewStoppageFilter(client, true, 1*time.Second)
	require.NotNil(t, filter)
	assert.True(t, filter.enabled)
	assert.Equal(t, 1*time.Second, filter.syncInterval)
	assert.NotNil(t, filter.state)
	assert.Equal(t, 0, len(filter.state))
}

// TestFilterData_MovementOnly tests that all movement records pass through
func TestFilterData_MovementOnly(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	data := []parser.ParsedGPSData{
		{IMEI: "123456789012345", Speed: 10, Status: 1},
		{IMEI: "123456789012345", Speed: 20, Status: 1},
		{IMEI: "123456789012345", Speed: 15, Status: 1},
	}

	filtered := filter.FilterData(data)

	assert.Equal(t, 3, len(filtered), "All movement records should pass through")
	assert.Equal(t, data, filtered)
}

// TestFilterData_StoppageAfterMovement tests first stoppage is sent
func TestFilterData_StoppageAfterMovement(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	// First send movement
	movement := []parser.ParsedGPSData{
		{IMEI: "123456789012345", Speed: 10, Status: 1},
	}
	filtered := filter.FilterData(movement)
	assert.Equal(t, 1, len(filtered))

	// Then send stoppage - should be sent (first stoppage after movement)
	stoppage := []parser.ParsedGPSData{
		{IMEI: "123456789012345", Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(stoppage)
	assert.Equal(t, 1, len(filtered), "First stoppage after movement should be sent")
}

// TestFilterData_RedundantStoppages tests redundant stoppages are filtered
func TestFilterData_RedundantStoppages(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	imei := "123456789012345"

	// Send movement
	movement := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 10, Status: 1},
	}
	filtered := filter.FilterData(movement)
	assert.Equal(t, 1, len(filtered))

	// Send first stoppage - should pass
	stoppage1 := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(stoppage1)
	assert.Equal(t, 1, len(filtered), "First stoppage should be sent")

	// Send second stoppage - should be filtered
	stoppage2 := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(stoppage2)
	assert.Equal(t, 0, len(filtered), "Second consecutive stoppage should be filtered")

	// Send third stoppage - should also be filtered
	stoppage3 := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(stoppage3)
	assert.Equal(t, 0, len(filtered), "Third consecutive stoppage should be filtered")
}

// TestFilterData_StoppageMovementCycle tests full cycle
func TestFilterData_StoppageMovementCycle(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	imei := "123456789012345"

	testCases := []struct {
		name          string
		data          []parser.ParsedGPSData
		expectedCount int
		description   string
	}{
		{
			name:          "movement1",
			data:          []parser.ParsedGPSData{{IMEI: imei, Speed: 10, Status: 1}},
			expectedCount: 1,
			description:   "Movement should be sent",
		},
		{
			name:          "movement2",
			data:          []parser.ParsedGPSData{{IMEI: imei, Speed: 5, Status: 1}},
			expectedCount: 1,
			description:   "Movement should be sent",
		},
		{
			name:          "stoppage1",
			data:          []parser.ParsedGPSData{{IMEI: imei, Speed: 0, Status: 1}},
			expectedCount: 1,
			description:   "First stoppage should be sent",
		},
		{
			name:          "stoppage2",
			data:          []parser.ParsedGPSData{{IMEI: imei, Speed: 0, Status: 1}},
			expectedCount: 0,
			description:   "Redundant stoppage should be filtered",
		},
		{
			name:          "stoppage3",
			data:          []parser.ParsedGPSData{{IMEI: imei, Speed: 0, Status: 1}},
			expectedCount: 0,
			description:   "Redundant stoppage should be filtered",
		},
		{
			name:          "movement3",
			data:          []parser.ParsedGPSData{{IMEI: imei, Speed: 8, Status: 1}},
			expectedCount: 1,
			description:   "Movement after stoppage should be sent",
		},
		{
			name:          "movement4",
			data:          []parser.ParsedGPSData{{IMEI: imei, Speed: 12, Status: 1}},
			expectedCount: 1,
			description:   "Movement should be sent",
		},
		{
			name:          "stoppage4",
			data:          []parser.ParsedGPSData{{IMEI: imei, Speed: 0, Status: 1}},
			expectedCount: 1,
			description:   "First stoppage after movement should be sent",
		},
		{
			name:          "stoppage5",
			data:          []parser.ParsedGPSData{{IMEI: imei, Speed: 0, Status: 1}},
			expectedCount: 0,
			description:   "Redundant stoppage should be filtered",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filtered := filter.FilterData(tc.data)
			assert.Equal(t, tc.expectedCount, len(filtered), tc.description)
		})
	}
}

// TestFilterData_MultipleIMEIs tests independent filtering per IMEI
func TestFilterData_MultipleIMEIs(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	imei1 := "111111111111111"
	imei2 := "222222222222222"

	// Both IMEIs start with movement
	data := []parser.ParsedGPSData{
		{IMEI: imei1, Speed: 10, Status: 1},
		{IMEI: imei2, Speed: 15, Status: 1},
	}
	filtered := filter.FilterData(data)
	assert.Equal(t, 2, len(filtered))

	// Both stop
	data = []parser.ParsedGPSData{
		{IMEI: imei1, Speed: 0, Status: 1},
		{IMEI: imei2, Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(data)
	assert.Equal(t, 2, len(filtered), "First stoppage for both IMEIs should be sent")

	// Both still stopped - should be filtered
	data = []parser.ParsedGPSData{
		{IMEI: imei1, Speed: 0, Status: 1},
		{IMEI: imei2, Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(data)
	assert.Equal(t, 0, len(filtered), "Redundant stoppages for both IMEIs should be filtered")

	// Only IMEI1 moves
	data = []parser.ParsedGPSData{
		{IMEI: imei1, Speed: 5, Status: 1},
		{IMEI: imei2, Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(data)
	assert.Equal(t, 1, len(filtered), "Only IMEI1 movement should be sent")
	assert.Equal(t, imei1, filtered[0].IMEI)

	// IMEI1 stops again, IMEI2 still stopped
	data = []parser.ParsedGPSData{
		{IMEI: imei1, Speed: 0, Status: 1},
		{IMEI: imei2, Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(data)
	assert.Equal(t, 1, len(filtered), "Only IMEI1 first stoppage should be sent")
	assert.Equal(t, imei1, filtered[0].IMEI)
}

// TestFilterData_BatchWithMixedRecords tests batch filtering
func TestFilterData_BatchWithMixedRecords(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	imei := "123456789012345"

	// Send a batch with movement -> stoppage -> stoppage -> movement
	data := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 10, Status: 1, DateTime: "2026-02-27 10:00:01"},
		{IMEI: imei, Speed: 5, Status: 1, DateTime: "2026-02-27 10:00:02"},
		{IMEI: imei, Speed: 0, Status: 1, DateTime: "2026-02-27 10:00:03"},
		{IMEI: imei, Speed: 0, Status: 1, DateTime: "2026-02-27 10:00:04"},
		{IMEI: imei, Speed: 0, Status: 1, DateTime: "2026-02-27 10:00:05"},
		{IMEI: imei, Speed: 8, Status: 1, DateTime: "2026-02-27 10:00:06"},
	}

	filtered := filter.FilterData(data)

	// Expected: movement1, movement2, stoppage1 (first), movement
	// Filtered: stoppage2, stoppage3
	assert.Equal(t, 4, len(filtered), "Should have 4 records (2 movements, 1 stoppage, 1 movement)")

	assert.Equal(t, 10, filtered[0].Speed, "First record should be movement1")
	assert.Equal(t, 5, filtered[1].Speed, "Second record should be movement2")
	assert.Equal(t, 0, filtered[2].Speed, "Third record should be stoppage1")
	assert.Equal(t, 8, filtered[3].Speed, "Fourth record should be movement")
}

// TestFilterData_FirstRecordIsStoppage tests device starting with stoppage
func TestFilterData_FirstRecordIsStoppage(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	imei := "123456789012345"

	// First record ever is a stoppage
	stoppage := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 0, Status: 1},
	}
	filtered := filter.FilterData(stoppage)
	assert.Equal(t, 1, len(filtered), "First record should be sent even if it's stoppage")

	// Second stoppage should be filtered
	stoppage2 := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(stoppage2)
	assert.Equal(t, 0, len(filtered), "Second stoppage should be filtered")
}

// TestFilterData_Disabled tests that disabled filter passes all data
func TestFilterData_Disabled(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, false, 30*time.Second)

	imei := "123456789012345"

	// Send multiple stoppages
	data := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 0, Status: 1},
		{IMEI: imei, Speed: 0, Status: 1},
		{IMEI: imei, Speed: 0, Status: 1},
	}

	filtered := filter.FilterData(data)
	assert.Equal(t, 3, len(filtered), "All records should pass when filter is disabled")
}

// TestFilterData_EmptyData tests handling of empty data
func TestFilterData_EmptyData(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	data := []parser.ParsedGPSData{}
	filtered := filter.FilterData(data)

	assert.Equal(t, 0, len(filtered), "Empty data should return empty result")
}

// TestSyncToRedis tests Redis synchronization
func TestSyncToRedis(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	imei := "123456789012345"

	// Add some state
	data := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 10, Status: 1},
	}
	filter.FilterData(data)

	// Sync to Redis
	filter.syncToRedis()

	// Verify data in Redis
	ctx := context.Background()
	val, err := client.Get(ctx, redisKeyPrefix+imei).Result()
	require.NoError(t, err)
	assert.Equal(t, "0", val, "Movement state should be saved as '0'")

	// Now send stoppage
	stoppage := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 0, Status: 1},
	}
	filter.FilterData(stoppage)
	filter.syncToRedis()

	// Verify stoppage state in Redis
	val, err = client.Get(ctx, redisKeyPrefix+imei).Result()
	require.NoError(t, err)
	assert.Equal(t, "1", val, "Stoppage state should be saved as '1'")
}

// TestLoadFromRedis tests state recovery from Redis
func TestLoadFromRedis(t *testing.T) {
	client := setupTestRedis(t)

	imei1 := "111111111111111"
	imei2 := "222222222222222"

	// Manually set some state in Redis
	ctx := context.Background()
	client.Set(ctx, redisKeyPrefix+imei1, "1", 24*time.Hour) // stoppage
	client.Set(ctx, redisKeyPrefix+imei2, "0", 24*time.Hour) // movement

	// Create new filter - should load state
	filter := NewStoppageFilter(client, true, 30*time.Second)

	// Verify state was loaded
	assert.Equal(t, 2, filter.GetStateCount())

	// IMEI1 should filter next stoppage (last was stoppage)
	stoppage := []parser.ParsedGPSData{
		{IMEI: imei1, Speed: 0, Status: 1},
	}
	filtered := filter.FilterData(stoppage)
	assert.Equal(t, 0, len(filtered), "IMEI1 stoppage should be filtered (last state was stoppage)")

	// IMEI2 should send next stoppage (last was movement)
	stoppage2 := []parser.ParsedGPSData{
		{IMEI: imei2, Speed: 0, Status: 1},
	}
	filtered = filter.FilterData(stoppage2)
	assert.Equal(t, 1, len(filtered), "IMEI2 stoppage should be sent (last state was movement)")
}

// TestGetStateCount tests state count retrieval
func TestGetStateCount(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	assert.Equal(t, 0, filter.GetStateCount())

	// Add state for multiple IMEIs
	data := []parser.ParsedGPSData{
		{IMEI: "111111111111111", Speed: 10, Status: 1},
		{IMEI: "222222222222222", Speed: 0, Status: 1},
		{IMEI: "333333333333333", Speed: 5, Status: 1},
	}
	filter.FilterData(data)

	assert.Equal(t, 3, filter.GetStateCount())
}

// TestClearState tests state clearing
func TestClearState(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 30*time.Second)

	// Add some state
	data := []parser.ParsedGPSData{
		{IMEI: "111111111111111", Speed: 10, Status: 1},
		{IMEI: "222222222222222", Speed: 0, Status: 1},
	}
	filter.FilterData(data)

	assert.Equal(t, 2, filter.GetStateCount())

	// Clear state
	filter.ClearState()

	assert.Equal(t, 0, filter.GetStateCount())
}

// TestStartAndClose tests filter lifecycle
func TestStartAndClose(t *testing.T) {
	client := setupTestRedis(t)
	filter := NewStoppageFilter(client, true, 100*time.Millisecond)

	imei := "123456789012345"

	// Add state
	data := []parser.ParsedGPSData{
		{IMEI: imei, Speed: 0, Status: 1},
	}
	filter.FilterData(data)

	// Start background sync
	filter.Start()

	// Wait for at least one sync cycle
	time.Sleep(200 * time.Millisecond)

	// Verify data was synced to Redis
	ctx := context.Background()
	val, err := client.Get(ctx, redisKeyPrefix+imei).Result()
	require.NoError(t, err)
	assert.Equal(t, "1", val)

	// Close filter
	filter.Close()

	// Verify final sync happened
	val, err = client.Get(ctx, redisKeyPrefix+imei).Result()
	require.NoError(t, err)
	assert.Equal(t, "1", val)
}
