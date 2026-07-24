package ingest

import (
	"context"
	"encoding/hex"
	"time"

	gojson "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/gps-data-receiver/internal/device"
	"github.com/gps-data-receiver/internal/destination"
	"github.com/gps-data-receiver/internal/filter"
	"github.com/gps-data-receiver/internal/hooshnics"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/internal/queue"
	"github.com/gps-data-receiver/internal/sender"
	"github.com/gps-data-receiver/internal/storage"
	"github.com/gps-data-receiver/internal/telemetry"
	"github.com/gps-data-receiver/internal/teltonika/codec"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// DeliveryEmitter is optional (Socket.IO). Nil-safe.
type DeliveryEmitter interface {
	Emit(event string, payload map[string]interface{})
}

// BroadcasterAdapter adapts api.AsyncBroadcaster (variadic Emit) to DeliveryEmitter.
type BroadcasterAdapter struct {
	EmitFn func(event string, args ...interface{}) error
}

func (a BroadcasterAdapter) Emit(event string, payload map[string]interface{}) {
	if a.EmitFn == nil {
		return
	}
	_ = a.EmitFn(event, payload)
}

// Processor turns durable raw AVL frames into PiStat/Hooshnics/Postgres dispatches.
type Processor struct {
	Queue      *queue.RedisQueue
	Mirror     *hooshnics.Forwarder
	Sender     *sender.HTTPSender
	Filter     *filter.StoppageFilter
	Postgres   *storage.AsyncWriter
	Emitter    DeliveryEmitter
	TZOffset   time.Duration
	Drivers    *device.Registry
	ForwardRaw bool // legacy Hooshnics raw AVL mirror (default false)
}

// ProcessOptions controls side effects for live ingest vs offline replay.
// Live path uses zero-value options (full pipeline + dedupe).
type ProcessOptions struct {
	// SkipDedupe bypasses Redis telemetry SET NX (required after parser bug fixes).
	SkipDedupe bool
	// SkipMirror disables Hooshnics forward.
	SkipMirror bool
	// SkipSender disables PiStat HTTP dispatch (and retry park).
	SkipSender bool
	// SkipPostgres disables success/failed/invalid Postgres writes.
	SkipPostgres bool
	// SkipDLQ disables writing decode failures to the live DLQ stream.
	SkipDLQ bool
}

// ProcessRawAVL parses a durable raw frame and dispatches without blocking the raw stream
// on destination downtime (exhausted PiStat retries → gps:pistat_retry).
func (p *Processor) ProcessRawAVL(ctx context.Context, imei string, frame []byte) error {
	return p.ProcessRawAVLWithOptions(ctx, imei, frame, ProcessOptions{})
}

// ProcessRawAVLWithOptions is the replay-safe entry point (dry-run / force / no-external).
func (p *Processor) ProcessRawAVLWithOptions(ctx context.Context, imei string, frame []byte, opts ProcessOptions) error {
	if p == nil || len(frame) == 0 {
		return nil
	}

	parseStart := time.Now()
	var points []telemetry.Point
	if p.Drivers != nil {
		drv, derr := p.Drivers.Detect(frame, device.Hints{IMEI: imei})
		if derr != nil {
			drv, _ = p.Drivers.Get(device.NameTeltonika)
		}
		if drv != nil {
			dec, err := drv.Decode(frame, device.Hints{IMEI: imei})
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.ObserveParserLatency(time.Since(parseStart))
			}
			if err != nil {
				return p.handleDecodeFailure(ctx, imei, frame, err, opts)
			}
			points = telemetry.FromDeviceViews(dec.Points, dec.DeviceType, dec.Protocol)
		}
	}
	if points == nil {
		_, records, err := codec.ParsePacket(frame)
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.ObserveParserLatency(time.Since(parseStart))
		}
		if err != nil {
			return p.handleDecodeFailure(ctx, imei, frame, err, opts)
		}
		legacy := parser.TeltonikaRecordsToParsed(records, imei, p.TZOffset)
		points = destination.FromPiStatRecords(legacy, device.NameTeltonika, "codec8")
	}
	if len(points) == 0 {
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.IncTeltonikaDecodeFailure()
			metrics.AppMetrics.IncTeltonikaDeadLetter()
			metrics.AppMetrics.IncParseErrors()
		}
		logger.Warn("Teltonika decode produced zero records — DLQ",
			zap.String("imei", imei),
			zap.Int("frame_size", len(frame)))
		if p.Queue != nil && !opts.SkipDLQ {
			if dlErr := p.Queue.EnqueueDeadLetter(ctx, imei, frame, "zero parsed records"); dlErr != nil {
				logger.Error("DLQ enqueue failed — leaving raw message pending",
					zap.String("imei", imei), zap.Error(dlErr))
				return dlErr
			}
		}
		return nil
	}
	if metrics.AppMetrics != nil {
		metrics.AppMetrics.IncParsedPackets(len(points))
	}

	// Destination adapter boundary: canonical Point → PiStat wire (filter / HTTP still use ParsedGPSData).
	parsed := destination.ToPiStatRecords(points)

	// Logical idempotency (IMEI+datetime+lat+lon+status) — skips retransmission duplicates.
	unique := make([]parser.ParsedGPSData, 0, len(parsed))
	claimedKeys := make([]string, 0, len(parsed))
	for _, rec := range parsed {
		if p.Queue == nil || opts.SkipDedupe {
			unique = append(unique, rec)
			continue
		}
		key := queue.TelemetryDedupeKey(rec.IMEI, rec.DateTime, rec.Coordinate[0], rec.Coordinate[1], rec.Status)
		first, err := p.Queue.ClaimTelemetrySighting(ctx, key)
		if err != nil {
			logger.Warn("Telemetry dedupe check failed — processing record",
				zap.String("imei", imei),
				zap.Error(err))
			unique = append(unique, rec)
			continue
		}
		if !first {
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.IncTeltonikaDuplicate()
			}
			continue
		}
		claimedKeys = append(claimedKeys, key)
		unique = append(unique, rec)
	}
	releaseClaims := func() {
		if p.Queue == nil || opts.SkipDedupe {
			return
		}
		bg := context.Background()
		for _, key := range claimedKeys {
			if err := p.Queue.ReleaseTelemetrySighting(bg, key); err != nil {
				logger.Error("Failed to release telemetry dedupe claim",
					zap.String("imei", imei),
					zap.String("key", key),
					zap.Error(err))
			}
		}
	}
	if len(unique) == 0 {
		logger.Info("All records in frame were duplicates — skipping dispatch",
			zap.String("imei", imei),
			zap.Int("record_count", len(parsed)))
		return nil
	}
	parsed = unique

	if p.Mirror != nil && !opts.SkipMirror && p.ForwardRaw {
		p.Mirror.ForwardTeltonikaAVL(imei, frame)
	}

	filtered := parsed
	if p.Filter != nil {
		filtered = p.Filter.FilterData(parsed)
	}
	if len(filtered) == 0 {
		logger.Debug("All Teltonika records filtered (stoppage)",
			zap.String("imei", imei),
			zap.Int("original", len(parsed)))
		return nil
	}

	wrapped := map[string]interface{}{"data": filtered}
	jsonData, err := gojson.Marshal(wrapped)
	if err != nil {
		logger.Error("Failed to marshal PiStat payload",
			zap.String("imei", imei),
			zap.Error(err))
		if p.Queue != nil && !opts.SkipDLQ {
			if dlErr := p.Queue.EnqueueDeadLetter(ctx, imei, frame, "marshal: "+err.Error()); dlErr != nil {
				releaseClaims()
				return dlErr
			}
		}
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.IncTeltonikaDeadLetter()
		}
		return nil
	}

	if p.Mirror != nil && !opts.SkipMirror {
		// HooshnicsParsedForwarder contract: parsed JSON envelope (same as PiStat body).
		_ = destination.HooshnicsParsedForwarder{Mirror: p.Mirror}.SendParsed(ctx, jsonData)
	}

	if p.Sender == nil || opts.SkipSender {
		return nil
	}

	deliveryID := uuid.New().String()
	emit := func(status, target string) {
		if p.Emitter == nil {
			return
		}
		p.Emitter.Emit("gps-delivery", map[string]interface{}{
			"delivery_id":   deliveryID,
			"status":        status,
			"target_server": target,
			"payload":       string(jsonData),
			"payload_size":  len(jsonData),
			"record_count":  len(filtered),
			"updated_at":    time.Now().UTC().Format(time.RFC3339),
		})
	}

	emit("sending", "")
	// PiStatForwarder.SendParsed is the adapter surface; use HTTPSender.Send here to retain
	// attempt observers / delivery events without redesigning sender.
	result := p.Sender.Send(ctx, jsonData, sender.FuncSendObserver{
		OnAttemptFunc: func(server string, attempt int) {
			emit("sending", server)
		},
	})

	if result.Success {
		emit("delivered", result.TargetServer)
		if p.Postgres != nil && !opts.SkipPostgres {
			p.Postgres.EnqueueSuccess(filtered)
		}
		return nil
	}

	emit("failed", result.TargetServer)
	errMsg := ""
	if result.Error != nil {
		errMsg = result.Error.Error()
	}
	if p.Postgres != nil && !opts.SkipPostgres {
		p.Postgres.EnqueueFailed(filtered, result.TargetServer, errMsg)
	}
	if p.Queue != nil {
		if parkErr := p.Queue.EnqueuePistatRetry(ctx, jsonData, result.Attempt); parkErr != nil {
			logger.Error("Failed to park PiStat payload on retry stream — leaving raw message pending",
				zap.String("imei", imei),
				zap.Error(parkErr))
			releaseClaims()
			return parkErr
		}
		logger.Warn("PiStat down — payload moved to gps:pistat_retry",
			zap.String("imei", imei),
			zap.String("target", result.TargetServer),
			zap.Int("attempt", result.Attempt),
			zap.Error(result.Error))
	}
	return nil
}

func (p *Processor) handleDecodeFailure(ctx context.Context, imei string, frame []byte, err error, opts ProcessOptions) error {
	if metrics.AppMetrics != nil {
		metrics.AppMetrics.IncTeltonikaDecodeFailure()
		metrics.AppMetrics.IncTeltonikaDeadLetter()
		metrics.AppMetrics.IncParseErrors()
	}
	logger.Warn("Teltonika full decode failed after CRC — routing to DLQ",
		zap.String("imei", imei),
		zap.Int("frame_size", len(frame)),
		zap.Error(err))
	if p.Queue != nil && !opts.SkipDLQ {
		if dlErr := p.Queue.EnqueueDeadLetter(ctx, imei, frame, err.Error()); dlErr != nil {
			logger.Error("DLQ enqueue failed — leaving raw message pending",
				zap.String("imei", imei), zap.Error(dlErr))
			return dlErr
		}
	}
	if p.Postgres != nil && !opts.SkipPostgres {
		p.Postgres.EnqueueInvalid([]parser.InvalidRecord{{
			RawData: hex.EncodeToString(frame),
			Reason:  err.Error(),
		}})
	}
	return nil
}
