package telemetry

import (
	"time"

	"github.com/gps-data-receiver/internal/device"
)

// Point is the canonical Hoosh IoT Gateway telemetry model.
// Destination-specific wire formats (PiStat, legacy envelopes) must map at adapter
// boundaries — this type must not import destination or PiStat packages.
type Point struct {
	IMEI       string    `json:"imei"`
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	Speed      int       `json:"speed"`
	Direction  int       `json:"direction"`
	Ignition   bool      `json:"ignition"`
	Battery    *float64  `json:"battery,omitempty"`
	Voltage    *float64  `json:"voltage,omitempty"`
	GSM        *int      `json:"gsm,omitempty"`
	Satellite  *int      `json:"satellite,omitempty"`
	Timestamp  time.Time `json:"timestamp"` // UTC instant (canonical)
	DeviceClock string   `json:"device_clock,omitempty"` // optional device wall-clock string as reported
	DeviceType string    `json:"device_type,omitempty"`
	Protocol   string    `json:"protocol,omitempty"`
}

// FromDeviceViews maps driver decode output into canonical Points.
func FromDeviceViews(views []device.PointView, deviceType, protocol string) []Point {
	out := make([]Point, 0, len(views))
	for _, v := range views {
		p := Point{
			IMEI:        v.IMEI,
			Lat:         v.Lat,
			Lng:         v.Lng,
			Speed:       v.Speed,
			Direction:   v.Direction,
			Ignition:    v.Ignition,
			Battery:     v.Battery,
			Voltage:     v.Voltage,
			GSM:         v.GSM,
			Satellite:   v.Satellite,
			DeviceClock: v.Timestamp,
			DeviceType:  deviceType,
			Protocol:    protocol,
		}
		if t, err := time.Parse(time.RFC3339, v.Timestamp); err == nil {
			p.Timestamp = t.UTC()
		} else if t, err := time.ParseInLocation("2006-01-02 15:04:05", v.Timestamp, time.UTC); err == nil {
			p.Timestamp = t
		} else {
			p.Timestamp = time.Now().UTC()
		}
		out = append(out, p)
	}
	return out
}
