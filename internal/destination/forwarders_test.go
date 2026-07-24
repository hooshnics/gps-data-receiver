package destination_test

import (
	"context"
	"testing"

	"github.com/gps-data-receiver/internal/destination"
	"github.com/gps-data-receiver/internal/telemetry"
	"github.com/stretchr/testify/require"
)

func TestForwarderNames(t *testing.T) {
	require.Equal(t, "pistat", destination.PiStatForwarder{}.Name())
	require.Equal(t, "hooshnics", destination.HooshnicsParsedForwarder{}.Name())
}

func TestNilSafeSendParsed(t *testing.T) {
	require.NoError(t, destination.PiStatForwarder{}.SendParsed(context.Background(), []byte(`{}`)))
	require.NoError(t, destination.HooshnicsParsedForwarder{}.SendParsed(context.Background(), []byte(`{}`)))
}

func TestToPiStatRecords(t *testing.T) {
	points := []telemetry.Point{{
		IMEI: "1", Lat: 10, Lng: -20, Speed: 5, Ignition: true,
		DeviceClock: "2026-01-01 00:00:00",
	}}
	rows := destination.ToPiStatRecords(points)
	require.Len(t, rows, 1)
	require.Equal(t, 1, rows[0].Status)
	require.Equal(t, 0, rows[0].Directions.EW)
}
