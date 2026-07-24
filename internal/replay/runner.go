package replay

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gps-data-receiver/internal/ingest"
	"github.com/gps-data-receiver/internal/teltonika/codec"
)

// Mode selects replay behavior. Live ingest path is never modified.
type Mode string

const (
	ModeDryRun    Mode = "dry-run"    // parse only; no writes
	ModeExport    Mode = "export"     // write NDJSON archive
	ModeReprocess Mode = "reprocess"  // run parser pipeline offline
	ModeEnqueue   Mode = "enqueue"    // copy into a test/replay stream
)

// RunConfig configures a replay job.
type RunConfig struct {
	Mode         Mode
	Force        bool   // SkipDedupe on reprocess
	NoExternal   bool   // skip PiStat + Hooshnics + DLQ side effects
	NoPostgres   bool
	ExportPath   string
	TargetStream string // for ModeEnqueue
	TZOffset     time.Duration
}

// Result summarizes a replay run.
type Result struct {
	Selected   int
	ParsedOK   int
	ParseFail  int
	Replayed   int
	Exported   int
	Enqueued   int
	RecordSum  int
}

// DryRunParse validates CRC/decode and preserves device timestamps in the report.
func DryRunParse(entries []Entry, tzOffset time.Duration) (Result, []string) {
	var res Result
	var lines []string
	res.Selected = len(entries)
	for _, e := range entries {
		frame, err := e.Frame()
		if err != nil || len(frame) == 0 {
			res.ParseFail++
			lines = append(lines, fmt.Sprintf("FAIL id=%s imei=%s err=bad_frame_hex", e.ID, e.IMEI))
			continue
		}
		count, err := codec.CountAVLRecords(frame)
		if err != nil {
			res.ParseFail++
			lines = append(lines, fmt.Sprintf("FAIL id=%s imei=%s crc_err=%v", e.ID, e.IMEI, err))
			continue
		}
		_, records, err := codec.ParsePacket(frame)
		if err != nil {
			res.ParseFail++
			lines = append(lines, fmt.Sprintf("FAIL id=%s imei=%s parse_err=%v crc_count=%d", e.ID, e.IMEI, err, count))
			continue
		}
		res.ParsedOK++
		res.RecordSum += len(records)
		firstTS := ""
		if len(records) > 0 {
			t := time.UnixMilli(int64(records[0].TimestampMs)).UTC().Add(tzOffset)
			firstTS = t.Format(time.RFC3339)
		}
		lines = append(lines, fmt.Sprintf(
			"OK id=%s imei=%s received_at=%s records=%d first_device_ts=%s source=%s",
			e.ID, e.IMEI, e.ReceivedAt, len(records), firstTS, e.Source,
		))
	}
	return res, lines
}

// Reprocess runs the ingest processor offline (does not XACK/XDEL live stream entries).
func Reprocess(ctx context.Context, p *ingest.Processor, entries []Entry, cfg RunConfig) (Result, error) {
	res := Result{Selected: len(entries)}
	opts := ingest.ProcessOptions{
		SkipDedupe:   cfg.Force,
		SkipMirror:   cfg.NoExternal,
		SkipSender:   cfg.NoExternal,
		SkipPostgres: cfg.NoPostgres || cfg.NoExternal,
		SkipDLQ:      cfg.NoExternal,
	}
	for _, e := range entries {
		frame, err := e.Frame()
		if err != nil || len(frame) == 0 {
			res.ParseFail++
			continue
		}
		if _, err := codec.CountAVLRecords(frame); err != nil {
			res.ParseFail++
			continue
		}
		if err := p.ProcessRawAVLWithOptions(ctx, e.IMEI, frame, opts); err != nil {
			return res, fmt.Errorf("reprocess id=%s: %w", e.ID, err)
		}
		res.Replayed++
		res.ParsedOK++
	}
	return res, nil
}

// Export writes NDJSON preserving original metadata fields.
func Export(path string, entries []Entry) (Result, error) {
	res := Result{Selected: len(entries)}
	f, err := os.Create(path)
	if err != nil {
		return res, err
	}
	defer f.Close()
	if err := WriteNDJSON(f, entries); err != nil {
		return res, err
	}
	res.Exported = len(entries)
	return res, nil
}
