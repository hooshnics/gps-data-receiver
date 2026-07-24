package destination

import (
	"time"

	"github.com/gps-data-receiver/internal/device"
	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/internal/telemetry"
)

// ToPiStatRecords maps canonical telemetry into the PiStat wire shape.
// PiStat coupling lives here — not in telemetry.Point.
func ToPiStatRecords(points []telemetry.Point) []parser.ParsedGPSData {
	out := make([]parser.ParsedGPSData, 0, len(points))
	for _, p := range points {
		status := 0
		if p.Ignition {
			status = 1
		}
		ew, ns := 1, 1
		if p.Lng < 0 {
			ew = 0
		}
		if p.Lat < 0 {
			ns = 0
		}
		dt := p.DeviceClock
		if dt == "" {
			dt = p.Timestamp.UTC().Format("2006-01-02 15:04:05")
		}
		out = append(out, parser.ParsedGPSData{
			Coordinate: [2]float64{p.Lat, p.Lng},
			Speed:      p.Speed,
			Status:     status,
			Directions: parser.DirectionData{EW: ew, NS: ns},
			DateTime:   dt,
			IMEI:       p.IMEI,
		})
	}
	return out
}

// FromPiStatRecords lifts legacy PiStat rows into canonical Points (HTTP / filter bridges).
func FromPiStatRecords(rows []parser.ParsedGPSData, deviceType, protocol string) []telemetry.Point {
	views := make([]device.PointView, 0, len(rows))
	for _, r := range rows {
		views = append(views, device.PointView{
			IMEI:      r.IMEI,
			Lat:       r.Coordinate[0],
			Lng:       r.Coordinate[1],
			Speed:     r.Speed,
			Ignition:  r.Status != 0,
			Timestamp: r.DateTime,
			RawRef:    r.RawData,
		})
	}
	return telemetry.FromDeviceViews(views, deviceType, protocol)
}

// Ensure Timestamp is non-zero for adapters that require UTC instants.
func EnsureTimestamps(points []telemetry.Point) []telemetry.Point {
	now := time.Now().UTC()
	for i := range points {
		if points[i].Timestamp.IsZero() {
			points[i].Timestamp = now
		}
	}
	return points
}
