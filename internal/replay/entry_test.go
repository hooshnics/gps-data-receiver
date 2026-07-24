package replay_test

import (
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/replay"
	"github.com/stretchr/testify/require"
)

func TestFilterMatch_IMEIAndTime(t *testing.T) {
	e := replay.Entry{
		ID:          "1720000000000-0",
		IMEI:        "356890080000001",
		ReceivedAt:  "2024-07-03T12:00:00.000Z",
		ReceivedUTC: time.Date(2024, 7, 3, 12, 0, 0, 0, time.UTC),
	}

	require.True(t, (replay.Filter{IMEI: "356890080000001"}).Match(e))
	require.False(t, (replay.Filter{IMEI: "other"}).Match(e))
	require.True(t, (replay.Filter{
		From: time.Date(2024, 7, 3, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2024, 7, 3, 23, 0, 0, 0, time.UTC),
	}).Match(e))
	require.False(t, (replay.Filter{
		From: time.Date(2024, 7, 4, 0, 0, 0, 0, time.UTC),
	}).Match(e))
	require.True(t, (replay.Filter{StreamID: "1720000000000-0"}).Match(e))
	require.False(t, (replay.Filter{StreamID: "1-1"}).Match(e))
}

func TestGuardReplayTarget(t *testing.T) {
	require.Error(t, replay.GuardReplayTarget("gps:raw_incoming", "gps:raw_incoming"))
	require.Error(t, replay.GuardReplayTarget("gps:reports", "gps:raw_incoming"))
	require.Error(t, replay.GuardReplayTarget("gps:dead_letter", "gps:raw_incoming"))
	require.Error(t, replay.GuardReplayTarget("gps:pistat_retry", "gps:raw_incoming"))
	require.Error(t, replay.GuardReplayTarget("GPS:RAW_INCOMING", "gps:raw_incoming"))
	require.NoError(t, replay.GuardReplayTarget("gps:raw_replay", "gps:raw_incoming"))
}

func TestProductionReprocessGuard(t *testing.T) {
	require.NoError(t, replay.ProductionReprocessGuard("development", "reprocess", false, false))
	require.NoError(t, replay.ProductionReprocessGuard("production", "dry-run", false, false))
	require.NoError(t, replay.ProductionReprocessGuard("production", "reprocess", true, false))
	require.NoError(t, replay.ProductionReprocessGuard("production", "reprocess", false, true))
	require.Error(t, replay.ProductionReprocessGuard("production", "reprocess", false, false))
	require.Error(t, replay.ProductionReprocessGuard("PRODUCTION", "reprocess", false, false))
	require.Error(t, replay.ProductionReprocessGuard("prod", "reprocess", false, false))
}

func TestValidateReplayEnvironment(t *testing.T) {
	require.NoError(t, replay.ValidateReplayEnvironment(""))
	require.NoError(t, replay.ValidateReplayEnvironment("development"))
	require.NoError(t, replay.ValidateReplayEnvironment("production"))
	require.Error(t, replay.ValidateReplayEnvironment("prod-live-oops"))
}

func TestGuardReplayTarget_CustomLiveStream(t *testing.T) {
	require.Error(t, replay.GuardReplayTarget("custom:live", "custom:live"))
	require.NoError(t, replay.GuardReplayTarget("custom:replay", "custom:live"))
}
