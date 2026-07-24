package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gps-data-receiver/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	fieldIMEI             = "imei"
	fieldFrameHex         = "frame_hex"
	fieldReceivedAt       = "received_at"
	fieldSource           = "source"
	fieldDeviceType       = "device_type"
	fieldProtocol         = "protocol"
	fieldProtocolVersion  = "protocol_version"
	fieldSourceIP         = "source_ip"
	fieldError            = "error"
	fieldKind             = "kind"
	fieldURL              = "url"
	fieldBody             = "body"
	fieldHeaders          = "headers"
	fieldPayload          = "payload"
	fieldAttempt          = "attempt"

	sourceTeltonikaTCP = "teltonika_tcp"
	dedupeKeyPrefix    = "gps:dedupe:"
	telemetryKeyPrefix = "gps:telem:"

	DeviceTypeTeltonika = "teltonika"
	DeviceTypeHooshnics = "hooshnics"
)

// RawFrame is the durable ingest envelope written to Redis BEFORE TCP ACK.
type RawFrame struct {
	IMEI            string
	Frame           []byte
	DeviceType      string
	Protocol        string
	ProtocolVersion string
	SourceIP        string
	Source          string
	ReceivedAt      time.Time
}

// EnqueueRawAVL durably writes a Teltonika AVL frame to gps:raw_incoming.
// This MUST succeed before the TCP ACK is sent to the device.
// sourceIP may be empty (legacy callers).
func (q *RedisQueue) EnqueueRawAVL(ctx context.Context, imei string, frame []byte, sourceIP string) (string, error) {
	return q.EnqueueRawFrame(ctx, RawFrame{
		IMEI:            imei,
		Frame:           frame,
		DeviceType:      DeviceTypeTeltonika,
		Protocol:        "codec8",
		ProtocolVersion: "codec8",
		SourceIP:        sourceIP,
		Source:          sourceTeltonikaTCP,
	})
}

// EnqueueRawFrame writes an enriched raw envelope (still on the Zero-Drop ACK path).
func (q *RedisQueue) EnqueueRawFrame(ctx context.Context, f RawFrame) (string, error) {
	if len(f.Frame) == 0 {
		return "", fmt.Errorf("empty AVL frame")
	}
	if f.IMEI == "" {
		return "", fmt.Errorf("empty IMEI")
	}
	received := f.ReceivedAt
	if received.IsZero() {
		received = time.Now().UTC()
	}
	deviceType := f.DeviceType
	if deviceType == "" {
		deviceType = DeviceTypeTeltonika
	}
	protocol := f.Protocol
	if protocol == "" {
		protocol = "codec8"
	}
	protoVer := f.ProtocolVersion
	if protoVer == "" {
		protoVer = protocol
	}
	source := f.Source
	if source == "" {
		source = sourceTeltonikaTCP
	}
	if len(f.Frame) > 8 {
		switch f.Frame[8] {
		case 0x8E:
			if protocol == "codec8" {
				protocol = "codec8e"
				protoVer = "codec8e"
			}
		case 0x10:
			if protocol == "codec8" {
				protocol = "codec16"
				protoVer = "codec16"
			}
		}
	}

	args := &redis.XAddArgs{
		Stream: q.rawStream,
		Values: map[string]interface{}{
			fieldIMEI:            f.IMEI,
			fieldFrameHex:        hex.EncodeToString(f.Frame),
			fieldReceivedAt:      received.UTC().Format(time.RFC3339Nano),
			fieldSource:          source,
			fieldDeviceType:      deviceType,
			fieldProtocol:        protocol,
			fieldProtocolVersion: protoVer,
			fieldSourceIP:        f.SourceIP,
		},
	}
	if q.rawMaxLen > 0 {
		args.MaxLen = q.rawMaxLen
		args.Approx = false
	}

	id, err := q.client.XAdd(ctx, args).Result()
	if err != nil {
		return "", fmt.Errorf("raw XADD failed: %w", err)
	}
	return id, nil
}

// TelemetryDedupeKey builds a logical uniqueness key from decoded telemetry.
func TelemetryDedupeKey(imei string, dateTime string, lat, lon float64, status int) string {
	return fmt.Sprintf("%s|%s|%.6f|%.6f|%d", imei, dateTime, lat, lon, status)
}

// ClaimTelemetrySighting marks a telemetry key as seen (SET NX EX).
func (q *RedisQueue) ClaimTelemetrySighting(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return true, nil
	}
	ttl := q.dedupeTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	ok, err := q.client.SetNX(ctx, telemetryKeyPrefix+key, "1", ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// EnqueueDeadLetter stores poison payloads that passed CRC but failed full decode,
// or exhausted retry payloads. Always includes timestamp + reason; attempt when >0.
func (q *RedisQueue) EnqueueDeadLetter(ctx context.Context, imei string, frame []byte, reason string) error {
	return q.EnqueueDeadLetterAttempt(ctx, imei, frame, reason, 0)
}

// EnqueueDeadLetterAttempt is EnqueueDeadLetter with an explicit retry-count metadata field.
func (q *RedisQueue) EnqueueDeadLetterAttempt(ctx context.Context, imei string, frame []byte, reason string, attempt int) error {
	values := map[string]interface{}{
		fieldIMEI:       imei,
		fieldReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		fieldError:      reason,
		fieldSource:     sourceTeltonikaTCP,
	}
	if attempt > 0 {
		values[fieldAttempt] = attempt
	}
	if len(frame) > 0 {
		values[fieldFrameHex] = hex.EncodeToString(frame)
	}
	args := &redis.XAddArgs{Stream: q.deadLetterStream, Values: values}
	// Bound DLQ growth; poison packets are already durable-failed for device purposes.
	if q.rawMaxLen > 0 {
		args.MaxLen = q.rawMaxLen
		args.Approx = true
	} else {
		args.MaxLen = 100000
		args.Approx = true
	}
	_, err := q.client.XAdd(ctx, args).Result()
	return err
}

// EnqueuePistatRetry parks a fully-formed PiStat JSON payload for later delivery.
func (q *RedisQueue) EnqueuePistatRetry(ctx context.Context, payload []byte, attempt int) error {
	if attempt < 1 {
		attempt = 1
	}
	if n, err := q.client.XLen(ctx, q.pistatRetryStream).Result(); err == nil && n >= retryStreamSoftCap {
		logger.Error("PiStat retry stream at soft cap — routing to DLQ",
			zap.Int64("depth", n), zap.Int("cap", retryStreamSoftCap))
		return q.EnqueueDeadLetterAttempt(ctx, "", payload, "pistat retry stream full", attempt)
	}
	args := &redis.XAddArgs{
		Stream: q.pistatRetryStream,
		MaxLen: int64(retryStreamSoftCap),
		Approx: true,
		Values: map[string]interface{}{
			fieldPayload:    string(payload),
			fieldAttempt:    attempt,
			fieldReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	_, err := q.client.XAdd(ctx, args).Result()
	return err
}

// EnqueueHooshnicsRetry parks a failed Hooshnics HTTP dispatch for later retry.
func (q *RedisQueue) EnqueueHooshnicsRetry(ctx context.Context, kind, url string, body []byte, headersJSON string) error {
	return q.EnqueueHooshnicsRetryAttempt(ctx, kind, url, body, headersJSON, 1)
}

// EnqueueHooshnicsRetryAttempt parks a Hooshnics retry with an explicit attempt counter.
func (q *RedisQueue) EnqueueHooshnicsRetryAttempt(ctx context.Context, kind, url string, body []byte, headersJSON string, attempt int) error {
	if attempt < 1 {
		attempt = 1
	}
	if n, err := q.client.XLen(ctx, q.hooshnicsRetryStream).Result(); err == nil && n >= retryStreamSoftCap {
		logger.Error("Hooshnics retry stream at soft cap — routing to DLQ",
			zap.Int64("depth", n), zap.Int("cap", retryStreamSoftCap))
		return q.EnqueueDeadLetterAttempt(ctx, "", body, "hooshnics retry stream full", attempt)
	}
	args := &redis.XAddArgs{
		Stream: q.hooshnicsRetryStream,
		MaxLen: int64(retryStreamSoftCap),
		Approx: true,
		Values: map[string]interface{}{
			fieldKind:       kind,
			fieldURL:        url,
			fieldBody:       string(body),
			fieldHeaders:    headersJSON,
			fieldAttempt:    attempt,
			fieldReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	_, err := q.client.XAdd(ctx, args).Result()
	return err
}

// ClaimFirstSighting returns true if this raw frame hash was not seen in the last dedupe TTL.
// Uses SET NX EX — safe under concurrent workers.
// IMPORTANT: call ReleaseFirstSighting if processing fails after a successful claim,
// otherwise XAUTOCLAIM reclaim will skip and silently drop the message.
func (q *RedisQueue) ClaimFirstSighting(ctx context.Context, frame []byte) (bool, string, error) {
	sum := sha256.Sum256(frame)
	hash := hex.EncodeToString(sum[:])
	key := dedupeKeyPrefix + hash

	ttl := q.dedupeTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	ok, err := q.client.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, hash, err
	}
	return ok, hash, nil
}

// ReleaseFirstSighting clears a frame-hash claim so a failed handler can be retried via PEL/XAUTOCLAIM.
func (q *RedisQueue) ReleaseFirstSighting(ctx context.Context, frame []byte) error {
	if len(frame) == 0 {
		return nil
	}
	sum := sha256.Sum256(frame)
	key := dedupeKeyPrefix + hex.EncodeToString(sum[:])
	return q.client.Del(ctx, key).Err()
}

// ReleaseTelemetrySighting clears a telemetry dedupe key (used when dispatch/park fails after claim).
func (q *RedisQueue) ReleaseTelemetrySighting(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	return q.client.Del(ctx, telemetryKeyPrefix+key).Err()
}

// EnsureDurableStreams creates consumer groups for raw / retry / DLQ streams.
func (q *RedisQueue) EnsureDurableStreams(ctx context.Context) error {
	groups := []struct {
		stream string
		group  string
	}{
		{q.rawStream, q.rawGroup},
		{q.pistatRetryStream, q.pistatRetryGroup},
		{q.hooshnicsRetryStream, q.hooshnicsRetryGroup},
	}
	for _, g := range groups {
		if g.stream == "" || g.group == "" {
			continue
		}
		err := q.client.XGroupCreateMkStream(ctx, g.stream, g.group, "0").Err()
		if err == nil {
			logger.Info("Durable stream consumer group ready",
				zap.String("stream", g.stream),
				zap.String("group", g.group))
			continue
		}
		if strings.Contains(err.Error(), "BUSYGROUP") {
			continue
		}
		return fmt.Errorf("ensure group %s/%s: %w", g.stream, g.group, err)
	}
	// DLQ is write-only (no consumer group required), but create the stream.
	if q.deadLetterStream != "" {
		_ = q.client.XGroupCreateMkStream(ctx, q.deadLetterStream, "gps-dlq-observers", "0").Err()
	}
	return nil
}

// GetRawStreamName returns the durable raw ingest stream.
func (q *RedisQueue) GetRawStreamName() string { return q.rawStream }

// GetRawConsumerGroup returns the raw ingest consumer group.
func (q *RedisQueue) GetRawConsumerGroup() string { return q.rawGroup }

// GetPistatRetryStream returns the PiStat retry stream name.
func (q *RedisQueue) GetPistatRetryStream() string { return q.pistatRetryStream }

// GetPistatRetryGroup returns the PiStat retry consumer group.
func (q *RedisQueue) GetPistatRetryGroup() string { return q.pistatRetryGroup }

// GetHooshnicsRetryStream returns the Hooshnics retry stream name.
func (q *RedisQueue) GetHooshnicsRetryStream() string { return q.hooshnicsRetryStream }

// GetHooshnicsRetryGroup returns the Hooshnics retry consumer group.
func (q *RedisQueue) GetHooshnicsRetryGroup() string { return q.hooshnicsRetryGroup }

// StreamDepths returns XLEN for durable streams (observability).
func (q *RedisQueue) StreamDepths(ctx context.Context) (raw, pistat, hoosh, dlq int64, err error) {
	pipe := q.client.Pipeline()
	cRaw := pipe.XLen(ctx, q.rawStream)
	cPistat := pipe.XLen(ctx, q.pistatRetryStream)
	cHoosh := pipe.XLen(ctx, q.hooshnicsRetryStream)
	cDLQ := pipe.XLen(ctx, q.deadLetterStream)
	if _, err = pipe.Exec(ctx); err != nil {
		return 0, 0, 0, 0, err
	}
	return cRaw.Val(), cPistat.Val(), cHoosh.Val(), cDLQ.Val(), nil
}

// RawPendingCount returns approximate pending entries for the raw consumer group.
func (q *RedisQueue) RawPendingCount(ctx context.Context) (int64, error) {
	pending, err := q.client.XPending(ctx, q.rawStream, q.rawGroup).Result()
	if err != nil {
		return 0, err
	}
	return pending.Count, nil
}

// DecodeRawFrameHex decodes a hex-encoded AVL frame from a stream entry.
func DecodeRawFrameHex(frameHex string) ([]byte, error) {
	return hex.DecodeString(strings.TrimSpace(frameHex))
}
