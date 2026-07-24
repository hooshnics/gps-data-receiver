package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gps-data-receiver/internal/queue"
	"github.com/redis/go-redis/v9"
)

// ReadRedis reads raw entries via XRANGE (read-only — never XACK/XDEL/XGROUP).
func ReadRedis(ctx context.Context, q *queue.RedisQueue, stream string, f Filter) ([]Entry, error) {
	if q == nil {
		return nil, fmt.Errorf("redis queue is nil")
	}
	if stream == "" {
		stream = q.GetRawStreamName()
	}

	min, max := "-", "+"
	if f.StreamID != "" {
		min, max = f.StreamID, f.StreamID
	} else {
		if !f.From.IsZero() {
			min = strconv.FormatInt(f.From.UnixMilli(), 10) + "-0"
		}
		if !f.To.IsZero() {
			max = strconv.FormatInt(f.To.UnixMilli(), 10) + "-999999"
		}
	}

	count := int64(1000)
	if f.Limit > 0 && f.Limit < count {
		count = f.Limit
	}

	var out []Entry
	cursor := min
	for {
		msgs, err := q.GetClient().XRangeN(ctx, stream, cursor, max, count).Result()
		if err != nil {
			return nil, fmt.Errorf("XRANGE %s: %w", stream, err)
		}
		if len(msgs) == 0 {
			break
		}
		for _, msg := range msgs {
			e := entryFromXMessage(msg)
			if !f.Match(e) {
				continue
			}
			out = append(out, e)
			if f.Limit > 0 && int64(len(out)) >= f.Limit {
				return out, nil
			}
		}
		last := msgs[len(msgs)-1].ID
		// Exclusive next page (Redis 6.2+ '(' prefix).
		cursor = "(" + last
		if len(msgs) < int(count) {
			break
		}
	}
	return out, nil
}

func entryFromXMessage(msg redis.XMessage) Entry {
	e := Entry{ID: msg.ID}
	if v, ok := msg.Values["imei"].(string); ok {
		e.IMEI = v
	}
	if v, ok := msg.Values["frame_hex"].(string); ok {
		e.FrameHex = v
	}
	if v, ok := msg.Values["received_at"].(string); ok {
		e.ReceivedAt = v
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			e.ReceivedUTC = t.UTC()
		}
	}
	if v, ok := msg.Values["source"].(string); ok {
		e.Source = v
	}
	if v, ok := msg.Values["device_type"].(string); ok {
		e.DeviceType = v
	}
	if v, ok := msg.Values["protocol"].(string); ok {
		e.Protocol = v
	}
	if v, ok := msg.Values["protocol_version"].(string); ok {
		e.ProtocolVersion = v
	}
	if v, ok := msg.Values["source_ip"].(string); ok {
		e.SourceIP = v
	}
	if v, ok := msg.Values["error"].(string); ok {
		e.Error = v
	}
	if e.ReceivedUTC.IsZero() {
		e.ReceivedUTC = streamIDTime(msg.ID)
	}
	return e
}

// ReadNDJSON loads archive / export files (one Entry JSON per line).
func ReadNDJSON(path string, f Filter) ([]Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var out []Entry
	sc := bufio.NewScanner(file)
	// AVL hex can be large; raise buffer.
	buf := make([]byte, 0, 1024*1024)
	sc.Buffer(buf, 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("ndjson decode: %w", err)
		}
		if e.ReceivedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, e.ReceivedAt); err == nil {
				e.ReceivedUTC = t.UTC()
			}
		}
		if !f.Match(e) {
			continue
		}
		out = append(out, e)
		if f.Limit > 0 && int64(len(out)) >= f.Limit {
			break
		}
	}
	return out, sc.Err()
}

// WriteNDJSON exports entries for archival / test environments.
func WriteNDJSON(w io.Writer, entries []Entry) error {
	enc := json.NewEncoder(w)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// EnqueueToStream copies entries into a non-live stream (e.g. gps:raw_replay).
// Does not touch gps:raw_incoming consumer groups or TCP ACK path.
func EnqueueToStream(ctx context.Context, q *queue.RedisQueue, targetStream string, entries []Entry) (int, error) {
	if targetStream == "" {
		return 0, fmt.Errorf("target stream required")
	}
	if err := GuardReplayTarget(targetStream, q.GetRawStreamName()); err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		frame, err := e.Frame()
		if err != nil || len(frame) == 0 {
			continue
		}
		received := e.ReceivedAt
		if received == "" && !e.ReceivedUTC.IsZero() {
			received = e.ReceivedUTC.UTC().Format(time.RFC3339Nano)
		}
		if received == "" {
			received = time.Now().UTC().Format(time.RFC3339Nano)
		}
		source := e.Source
		if source == "" {
			source = "replay"
		}
		args := &redis.XAddArgs{
			Stream: targetStream,
			Values: map[string]interface{}{
				"imei":             e.IMEI,
				"frame_hex":        e.FrameHex,
				"received_at":      received,
				"source":           source,
				"replay_of":        e.ID,
				"device_type":      e.DeviceType,
				"protocol":         e.Protocol,
				"protocol_version": e.ProtocolVersion,
				"source_ip":        e.SourceIP,
			},
		}
		if _, err := q.GetClient().XAdd(ctx, args).Result(); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// GuardReplayTarget refuses writes into production/live operational streams.
func GuardReplayTarget(target, liveRawStream string) error {
	t := strings.TrimSpace(strings.ToLower(target))
	live := strings.TrimSpace(strings.ToLower(liveRawStream))
	if live == "" {
		live = "gps:raw_incoming"
	}
	if t == live || strings.Contains(t, "raw_incoming") {
		return fmt.Errorf("refusing to write replay into live raw stream %q — use -target-stream gps:raw_replay", target)
	}
	protected := []string{
		"gps:reports",
		"gps:dead_letter",
		"gps:pistat_retry",
		"gps:hooshnics_retry",
	}
	for _, p := range protected {
		if t == p {
			return fmt.Errorf("refusing to write replay into protected stream %q", target)
		}
	}
	return nil
}
