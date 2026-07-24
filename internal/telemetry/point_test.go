package telemetry_test

import (
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/destination"
	"github.com/gps-data-receiver/internal/device"
	"github.com/gps-data-receiver/internal/telemetry"
	"github.com/stretchr/testify/require"
)

func TestFromDeviceViews_CanonicalThenPiStatAdapter(t *testing.T) {
	views := []device.PointView{{
		IMEI: "123", Lat: 35.7, Lng: 51.4, Speed: 12, Direction: 90,
		Ignition: true, Timestamp: "2026-07-23 12:00:00",
	}}
	points := telemetry.FromDeviceViews(views, "teltonika", "codec8")
	require.Len(t, points, 1)
	require.Equal(t, "teltonika", points[0].DeviceType)
	require.True(t, points[0].Ignition)
	require.Equal(t, "2026-07-23 12:00:00", points[0].DeviceClock)

	parsed := destination.ToPiStatRecords(points)
	require.Len(t, parsed, 1)
	require.Equal(t, "123", parsed[0].IMEI)
	require.Equal(t, 12, parsed[0].Speed)
	require.Equal(t, 1, parsed[0].Status)
	require.InDelta(t, 35.7, parsed[0].Coordinate[0], 1e-9)
}

func TestFromDeviceViews_RFC3339Timestamp(t *testing.T) {
	ts := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	views := []device.PointView{{
		IMEI: "1", Lat: 1, Lng: 2, Timestamp: ts.Format(time.RFC3339),
	}}
	points := telemetry.FromDeviceViews(views, "hooshnics", "hooshnic")
	require.True(t, points[0].Timestamp.Equal(ts))
}
