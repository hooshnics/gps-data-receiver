package replay

import (
	"encoding/hex"
	"strings"
	"time"
)

// Entry is one durable raw AVL message suitable for offline replay.
// Fields mirror Redis stream / archive values written by EnqueueRawFrame.
type Entry struct {
	ID              string    `json:"id,omitempty"`
	IMEI            string    `json:"imei"`
	FrameHex        string    `json:"frame_hex"`
	ReceivedAt      string    `json:"received_at,omitempty"`
	Source          string    `json:"source,omitempty"`
	DeviceType      string    `json:"device_type,omitempty"`
	Protocol        string    `json:"protocol,omitempty"`
	ProtocolVersion string    `json:"protocol_version,omitempty"`
	SourceIP        string    `json:"source_ip,omitempty"`
	Error           string    `json:"error,omitempty"` // DLQ only
	ReceivedUTC     time.Time `json:"-"`
}

// Frame returns the binary AVL payload.
func (e Entry) Frame() ([]byte, error) {
	return hex.DecodeString(strings.TrimSpace(e.FrameHex))
}

// Filter selects entries for replay without mutating the live stream.
type Filter struct {
	IMEI    string
	From    time.Time // inclusive, zero = unbound
	To      time.Time // inclusive, zero = unbound
	StreamID string  // exact Redis stream ID
	Limit   int64
}

// Match reports whether the entry passes the filter.
func (f Filter) Match(e Entry) bool {
	if f.StreamID != "" && e.ID != f.StreamID {
		return false
	}
	if f.IMEI != "" && e.IMEI != f.IMEI {
		return false
	}
	if !f.From.IsZero() || !f.To.IsZero() {
		ts := e.ReceivedUTC
		if ts.IsZero() && e.ReceivedAt != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, e.ReceivedAt); err == nil {
				ts = parsed
			}
		}
		if ts.IsZero() {
			// Fall back to Redis stream ID millisecond prefix when received_at missing.
			ts = streamIDTime(e.ID)
		}
		if !f.From.IsZero() && ts.Before(f.From) {
			return false
		}
		if !f.To.IsZero() && ts.After(f.To) {
			return false
		}
	}
	return true
}

func streamIDTime(id string) time.Time {
	if id == "" {
		return time.Time{}
	}
	parts := strings.SplitN(id, "-", 2)
	if len(parts) == 0 {
		return time.Time{}
	}
	var ms int64
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			return time.Time{}
		}
		ms = ms*10 + int64(c-'0')
	}
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
