package codec

import "bytes"

// Format identifies the payload type for routing parsers.
type Format int

const (
	FormatUnknown Format = iota
	FormatHooshnicJSON
	FormatTeltonikaAVL
	FormatTeltonikaQueued
)

const teltonikaQueuedPrefix = `{"_teltonika":`

// DetectFormat inspects raw bytes and returns the best-matching format.
func DetectFormat(data []byte) Format {
	if len(data) >= len(teltonikaQueuedPrefix) && bytes.HasPrefix(data, []byte(teltonikaQueuedPrefix)) {
		return FormatTeltonikaQueued
	}
	if len(data) >= 9 &&
		data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 0 {
		switch data[8] {
		case 0x08, 0x8E, 0x10:
			return FormatTeltonikaAVL
		}
	}
	if len(data) > 0 && (data[0] == '[' || data[0] == '{') {
		return FormatHooshnicJSON
	}
	return FormatUnknown
}
