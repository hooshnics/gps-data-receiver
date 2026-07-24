package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/device"
	"github.com/gps-data-receiver/internal/ingest"
	"github.com/gps-data-receiver/internal/queue"
	"github.com/gps-data-receiver/internal/replay"
	"github.com/gps-data-receiver/pkg/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

// Replay CLI — offline recovery / parser validation.
// Never ACKs or deletes live gps:raw_incoming consumer-group entries.
// Never writes into the live raw stream (enqueue requires a separate target).
func main() {
	_ = godotenv.Load()

	var (
		mode         = flag.String("mode", "dry-run", "dry-run | export | reprocess | enqueue")
		source       = flag.String("source", "redis", "redis | dlq | file")
		stream       = flag.String("stream", "", "Redis stream (default: REDIS_RAW_STREAM or gps:dead_letter for dlq)")
		file         = flag.String("file", "", "NDJSON input (source=file) or export output path")
		id           = flag.String("id", "", "exact Redis stream ID")
		imei         = flag.String("imei", "", "filter by IMEI")
		fromStr      = flag.String("from", "", "RFC3339 start (inclusive)")
		toStr        = flag.String("to", "", "RFC3339 end (inclusive)")
		limit        = flag.Int64("limit", 0, "max entries (0 = unbounded)")
		force        = flag.Bool("force", false, "reprocess: bypass telemetry dedupe")
		noExternal   = flag.Bool("no-external", true, "reprocess: skip PiStat/Hooshnics/DLQ (safe default)")
		noPostgres   = flag.Bool("no-postgres", true, "reprocess: skip Postgres writes (safe default)")
		targetStream = flag.String("target-stream", "gps:raw_replay", "enqueue destination (must NOT be live/protected stream)")
		tzOffset     = flag.Duration("tz-offset", 3*time.Hour+30*time.Minute, "device timestamp display offset")
		verbose      = flag.Bool("v", false, "print per-entry dry-run lines")
		confirmProd  = flag.Bool("confirm-production", false, "required with -no-external=false when ENVIRONMENT=production")
	)
	flag.Parse()

	filter, err := buildFilter(*id, *imei, *fromStr, *toStr, *limit)
	if err != nil {
		fatal(err)
	}

	if err := replay.ValidateReplayEnvironment(os.Getenv("ENVIRONMENT")); err != nil {
		fatal(err)
	}
	if err := replay.ProductionReprocessGuard(os.Getenv("ENVIRONMENT"), *mode, *noExternal, *confirmProd); err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	entries, q, err := loadEntries(ctx, *source, *stream, *file, filter)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("selected=%d source=%s mode=%s\n", len(entries), *source, *mode)

	cfg := replay.RunConfig{
		Mode:         replay.Mode(*mode),
		Force:        *force,
		NoExternal:   *noExternal,
		NoPostgres:   *noPostgres,
		ExportPath:   *file,
		TargetStream: *targetStream,
		TZOffset:     *tzOffset,
	}

	switch cfg.Mode {
	case replay.ModeDryRun:
		res, lines := replay.DryRunParse(entries, *tzOffset)
		printResult(res)
		if *verbose {
			for _, line := range lines {
				fmt.Println(line)
			}
		}
	case replay.ModeExport:
		if *file == "" {
			fatal(fmt.Errorf("-file required for export mode"))
		}
		res, err := replay.Export(*file, entries)
		if err != nil {
			fatal(err)
		}
		printResult(res)
		fmt.Printf("wrote %s\n", *file)
	case replay.ModeEnqueue:
		if q == nil {
			fatal(fmt.Errorf("enqueue requires redis source connectivity"))
		}
		n, err := replay.EnqueueToStream(ctx, q, *targetStream, entries)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("enqueued=%d target=%s\n", n, *targetStream)
	case replay.ModeReprocess:
		if !*noExternal {
			logger.Warn("reprocess with external dispatch enabled",
				zap.Bool("force", *force),
				zap.Bool("no_postgres", *noPostgres))
		}
		proc := &ingest.Processor{
			Queue:    q,
			TZOffset: *tzOffset,
			Drivers:  device.DefaultRegistry(*tzOffset),
		}
		res, err := replay.Reprocess(ctx, proc, entries, cfg)
		if err != nil {
			fatal(err)
		}
		printResult(res)
	default:
		fatal(fmt.Errorf("unknown mode %q", *mode))
	}
}

func loadEntries(ctx context.Context, source, stream, file string, f replay.Filter) ([]replay.Entry, *queue.RedisQueue, error) {
	switch strings.ToLower(source) {
	case "file":
		if file == "" {
			return nil, nil, fmt.Errorf("-file required for source=file")
		}
		entries, err := replay.ReadNDJSON(file, f)
		return entries, nil, err
	case "redis", "dlq":
		q, err := redisQueueFromEnv()
		if err != nil {
			return nil, nil, err
		}
		streamName := stream
		if streamName == "" {
			if source == "dlq" {
				streamName = envOr("REDIS_DEAD_LETTER_STREAM", "gps:dead_letter")
			} else {
				streamName = q.GetRawStreamName()
			}
		}
		entries, err := replay.ReadRedis(ctx, q, streamName, f)
		return entries, q, err
	default:
		return nil, nil, fmt.Errorf("unknown source %q", source)
	}
}

func redisQueueFromEnv() (*queue.RedisQueue, error) {
	cfg := &config.RedisConfig{
		Host:                 envOr("REDIS_HOST", "127.0.0.1"),
		Port:                 envOr("REDIS_PORT", "6379"),
		Password:             os.Getenv("REDIS_PASSWORD"),
		DB:                   0,
		PoolSize:             20,
		StreamName:           envOr("REDIS_STREAM_NAME", "gps:reports"),
		ConsumerGroup:        envOr("REDIS_CONSUMER_GROUP", "gps-workers"),
		RawStreamName:        envOr("REDIS_RAW_STREAM", "gps:raw_incoming"),
		RawConsumerGroup:     envOr("REDIS_RAW_CONSUMER_GROUP", "gps-raw-workers"),
		DeadLetterStream:     envOr("REDIS_DEAD_LETTER_STREAM", "gps:dead_letter"),
		HooshnicsRetryStream: envOr("REDIS_HOOSHNICS_RETRY_STREAM", "gps:hooshnics_retry"),
		PistatRetryStream:    envOr("REDIS_PISTAT_RETRY_STREAM", "gps:pistat_retry"),
		SentinelMaster:       envOr("REDIS_SENTINEL_MASTER", "mymaster"),
		DedupeTTL:            24 * time.Hour,
	}
	if v := os.Getenv("REDIS_SENTINEL_ADDRS"); v != "" {
		for _, p := range strings.Split(v, ",") {
			if t := strings.TrimSpace(p); t != "" {
				cfg.SentinelAddrs = append(cfg.SentinelAddrs, t)
			}
		}
	}
	return queue.NewRedisQueue(cfg)
}

func buildFilter(id, imei, fromStr, toStr string, limit int64) (replay.Filter, error) {
	f := replay.Filter{StreamID: id, IMEI: imei, Limit: limit}
	var err error
	if fromStr != "" {
		f.From, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			return f, fmt.Errorf("-from: %w", err)
		}
	}
	if toStr != "" {
		f.To, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			return f, fmt.Errorf("-to: %w", err)
		}
	}
	return f, nil
}

func printResult(res replay.Result) {
	fmt.Printf("parsed_ok=%d parse_fail=%d records=%d replayed=%d exported=%d\n",
		res.ParsedOK, res.ParseFail, res.RecordSum, res.Replayed, res.Exported)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "replay: %v\n", err)
	os.Exit(1)
}
