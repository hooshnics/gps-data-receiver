package parser

import (
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"time"

	gojson "github.com/goccy/go-json"
	"github.com/gps-data-receiver/internal/teltonika/codec"
)

const defaultTimezoneOffset = 3*time.Hour + 30*time.Minute

type teltonikaEnvelope struct {
	Teltonika bool            `json:"_teltonika"`
	IMEI      string          `json:"imei"`
	Records   []ParsedGPSData `json:"records"`
}

// ParseTeltonikaEnvelope serializes parsed records for the Redis queue (TCP path).
func ParseTeltonikaEnvelope(imei string, records []ParsedGPSData) ([]byte, error) {
	env := teltonikaEnvelope{
		Teltonika: true,
		IMEI:      imei,
		Records:   records,
	}
	return gojson.Marshal(env)
}

func unmarshalTeltonikaEnvelope(data []byte) (ParseResult, error) {
	var env teltonikaEnvelope
	if err := gojson.Unmarshal(data, &env); err != nil {
		return ParseResult{}, fmt.Errorf("invalid teltonika envelope: %w", err)
	}
	if !env.Teltonika {
		return ParseResult{}, fmt.Errorf("missing teltonika envelope marker")
	}
	records := env.Records
	for i := range records {
		if records[i].IMEI == "" {
			records[i].IMEI = env.IMEI
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].DateTime < records[j].DateTime
	})
	return ParseResult{Records: records}, nil
}

func parseTeltonikaAVL(data []byte, imei string, tzOffset time.Duration) (ParseResult, error) {
	_, records, err := codec.ParsePacket(data)
	if err != nil {
		return ParseResult{}, err
	}
	parsed := teltonikaRecordsToParsed(records, imei, tzOffset)
	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].DateTime < parsed[j].DateTime
	})
	return ParseResult{Records: parsed}, nil
}

func teltonikaRecordsToParsed(records []codec.Record, imei string, tzOffset time.Duration) []ParsedGPSData {
	out := make([]ParsedGPSData, 0, len(records))
	for _, rec := range records {
		out = append(out, teltonikaRecordToParsed(rec, imei, tzOffset))
	}
	return out
}

func teltonikaRecordToParsed(rec codec.Record, imei string, tzOffset time.Duration) ParsedGPSData {
	t := time.UnixMilli(int64(rec.TimestampMs)).UTC().Add(tzOffset)
	ew, ns := 1, 1
	if rec.Lon < 0 {
		ew = 0
	}
	if rec.Lat < 0 {
		ns = 0
	}

	status := 0
	if v, ok := rec.IOs[239]; ok && v == 1 {
		status = 1
	}

	rawData := ""
	if len(rec.RawBytes) > 0 {
		rawData = hex.EncodeToString(rec.RawBytes)
	}

	lat := math.Round(rec.Lat*1_000_000) / 1_000_000
	lon := math.Round(rec.Lon*1_000_000) / 1_000_000

	return ParsedGPSData{
		Coordinate: [2]float64{lat, lon},
		Speed:      int(rec.Speed),
		Status:     status,
		Directions: DirectionData{EW: ew, NS: ns},
		DateTime:   t.Format("2006-01-02 15:04:05"),
		IMEI:       imei,
		RawData:    rawData,
	}
}

// TeltonikaRecordsToParsed converts decoded Teltonika records for external callers (e.g. TCP server).
func TeltonikaRecordsToParsed(records []codec.Record, imei string, tzOffset time.Duration) []ParsedGPSData {
	if tzOffset == 0 {
		tzOffset = defaultTimezoneOffset
	}
	return teltonikaRecordsToParsed(records, imei, tzOffset)
}

// ParseWithIMEI routes parsing based on payload format.
// imei is required for standalone binary AVL packets (HTTP octet-stream path).
func ParseWithIMEI(data []byte, imei string, tzOffset time.Duration) (ParseResult, error) {
	if len(data) == 0 {
		return ParseResult{}, fmt.Errorf("input data cannot be empty")
	}
	if tzOffset == 0 {
		tzOffset = defaultTimezoneOffset
	}

	switch codec.DetectFormat(data) {
	case codec.FormatTeltonikaQueued:
		return unmarshalTeltonikaEnvelope(data)
	case codec.FormatTeltonikaAVL:
		return parseTeltonikaAVL(data, imei, tzOffset)
	case codec.FormatHooshnicJSON:
		return parseHooshnicJSON(data)
	default:
		return ParseResult{}, fmt.Errorf("unknown payload format (first byte: 0x%02X)", data[0])
	}
}
