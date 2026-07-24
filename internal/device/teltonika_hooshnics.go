package device

import (
	"fmt"
	"time"

	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/internal/teltonika/codec"
)

const (
	NameTeltonika = "teltonika"
	NameHooshnics = "hooshnics"
)

// TeltonikaDriver wraps existing codec + parser conversions. Algorithms are not modified.
type TeltonikaDriver struct {
	TZOffset time.Duration
}

func (TeltonikaDriver) Name() string { return NameTeltonika }

func (TeltonikaDriver) Detect(raw []byte, _ Hints) bool {
	f := codec.DetectFormat(raw)
	return f == codec.FormatTeltonikaAVL || f == codec.FormatTeltonikaQueued
}

func (TeltonikaDriver) Validate(raw []byte) error {
	_, err := codec.CountAVLRecords(raw)
	return err
}

func (d TeltonikaDriver) Decode(raw []byte, hints Hints) (DecodeResult, error) {
	tz := d.TZOffset
	if tz == 0 {
		tz = 3*time.Hour + 30*time.Minute
	}
	_, records, err := codec.ParsePacket(raw)
	if err != nil {
		return DecodeResult{}, err
	}
	imei := hints.IMEI
	parsed := parser.TeltonikaRecordsToParsed(records, imei, tz)
	points := make([]PointView, 0, len(parsed))
	for i, p := range parsed {
		ign := p.Status != 0
		dir := 0
		var sat *int
		if i < len(records) {
			dir = int(records[i].Angle)
			s := int(records[i].Satellites)
			sat = &s
			if v, ok := records[i].IOs[66]; ok {
				volt := float64(v) / 1000.0
				points = append(points, PointView{
					IMEI: p.IMEI, Lat: p.Coordinate[0], Lng: p.Coordinate[1],
					Speed: p.Speed, Direction: dir, Ignition: ign, Voltage: &volt,
					Satellite: sat, Timestamp: p.DateTime, RawRef: p.RawData,
				})
				continue
			}
		}
		points = append(points, PointView{
			IMEI: p.IMEI, Lat: p.Coordinate[0], Lng: p.Coordinate[1],
			Speed: p.Speed, Direction: dir, Ignition: ign, Satellite: sat,
			Timestamp: p.DateTime, RawRef: p.RawData,
		})
	}
	proto := "codec8"
	if len(raw) > 8 {
		switch raw[8] {
		case 0x8E:
			proto = "codec8e"
		case 0x10:
			proto = "codec16"
		}
	}
	return DecodeResult{
		DeviceType:      NameTeltonika,
		Protocol:        proto,
		ProtocolVersion: proto,
		Points:          points,
	}, nil
}

// HooshnicsDriver wraps the legacy +Hooshnic JSON/text parser.
type HooshnicsDriver struct{}

func (HooshnicsDriver) Name() string { return NameHooshnics }

func (HooshnicsDriver) Detect(raw []byte, hints Hints) bool {
	if codec.DetectFormat(raw) == codec.FormatHooshnicJSON {
		return true
	}
	if len(raw) > 10 && (raw[0] == '+' || raw[0] == '[') {
		return true
	}
	_ = hints
	return false
}

func (HooshnicsDriver) Validate(raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty hooshnics payload")
	}
	return nil
}

func (HooshnicsDriver) Decode(raw []byte, hints Hints) (DecodeResult, error) {
	res, err := parser.ParseWithIMEI(raw, hints.IMEI, 3*time.Hour+30*time.Minute)
	if err != nil {
		return DecodeResult{}, err
	}
	points := make([]PointView, 0, len(res.Records))
	for _, p := range res.Records {
		points = append(points, PointView{
			IMEI:      p.IMEI,
			Lat:       p.Coordinate[0],
			Lng:       p.Coordinate[1],
			Speed:     p.Speed,
			Ignition:  p.Status != 0,
			Timestamp: p.DateTime,
			RawRef:    p.RawData,
		})
	}
	return DecodeResult{
		DeviceType:      NameHooshnics,
		Protocol:        "hooshnic",
		ProtocolVersion: "v1",
		Points:          points,
	}, nil
}
