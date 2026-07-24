package archive

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gps-data-receiver/internal/queue"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// Record is one archived raw AVL entry (matches replay NDJSON shape).
type Record struct {
	ID              string `json:"id"`
	IMEI            string `json:"imei"`
	FrameHex        string `json:"frame_hex"`
	ReceivedAt      string `json:"received_at,omitempty"`
	Source          string `json:"source,omitempty"`
	DeviceType      string `json:"device_type,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	ProtocolVersion string `json:"protocol_version,omitempty"`
	SourceIP        string `json:"source_ip,omitempty"`
	ArchivedAt      string `json:"archived_at"`
}

// Writer appends NDJSON lines into daily gzip files under Dir/YYYY/MM/DD/.
// Intended for the raw consumer path AFTER Redis durability (never on TCP ACK hot path).
type Writer struct {
	dir    string
	strict bool

	mu     sync.Mutex
	day    string
	file   *os.File
	gz     *gzip.Writer
	enc    *json.Encoder
}

// NewWriter creates an archive writer after validating the directory is writable.
func NewWriter(dir string, strict bool) (*Writer, error) {
	if dir == "" {
		dir = "archive"
	}
	if err := EnsureWritable(dir); err != nil {
		return nil, err
	}
	logger.Info("Raw archive writer ready",
		zap.String("dir", dir),
		zap.Bool("strict", strict))
	return &Writer{dir: dir, strict: strict}, nil
}

// EnsureWritable verifies dir exists and the process can create YYYY/MM/DD/ and write a probe file.
// Call at startup when ARCHIVE_ENABLED so permission issues fail fast (not mid-ingest).
func EnsureWritable(dir string) error {
	if dir == "" {
		return fmt.Errorf("archive dir is empty")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("archive dir mkdir %q: %w (fix volume ownership for uid running the process)", dir, err)
	}

	now := time.Now().UTC()
	dayDir := filepath.Join(dir, now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dayDir, 0o750); err != nil {
		return fmt.Errorf("archive day dir mkdir %q: %w (permission denied usually means Docker volume owned by root; see docs/recovery/archive-strategy.md)", dayDir, err)
	}

	probe, err := os.CreateTemp(dayDir, ".write-probe-*")
	if err != nil {
		return fmt.Errorf("archive dir not writable %q: %w", dayDir, err)
	}
	name := probe.Name()
	_, _ = probe.Write([]byte("ok"))
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// Strict reports whether archive failures should block stream ACK.
func (w *Writer) Strict() bool {
	return w != nil && w.strict
}

// Archive persists one stream entry (legacy helpers / tests).
func (w *Writer) Archive(ctx context.Context, id, imei, frameHex, receivedAt, source string) error {
	return w.ArchiveEntry(ctx, queue.ArchiveEntry{
		ID: id, IMEI: imei, FrameHex: frameHex, ReceivedAt: receivedAt, Source: source,
	})
}

// ArchiveEntry implements queue.StreamArchiver (async; never on TCP ACK path).
func (w *Writer) ArchiveEntry(ctx context.Context, e queue.ArchiveEntry) error {
	return w.archiveMeta(ctx, Record{
		ID: e.ID, IMEI: e.IMEI, FrameHex: e.FrameHex, ReceivedAt: e.ReceivedAt, Source: e.Source,
		DeviceType: e.DeviceType, Protocol: e.Protocol, ProtocolVersion: e.ProtocolVersion, SourceIP: e.SourceIP,
	})
}

func (w *Writer) archiveMeta(ctx context.Context, rec Record) error {
	if w == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if rec.ArchivedAt == "" {
		rec.ArchivedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ensureDayLocked(time.Now().UTC()); err != nil {
		return err
	}
	if err := w.enc.Encode(rec); err != nil {
		return fmt.Errorf("archive encode: %w", err)
	}
	return w.gz.Flush()
}

func (w *Writer) ensureDayLocked(now time.Time) error {
	day := now.Format("2006/01/02")
	if w.day == day && w.enc != nil {
		return nil
	}
	_ = w.closeLocked()

	dir := filepath.Join(w.dir, now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	name := filepath.Join(dir, fmt.Sprintf("raw-%s.ndjson.gz", now.Format("20060102")))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	w.file = f
	w.gz = gz
	w.enc = json.NewEncoder(gz)
	w.day = day
	return nil
}

func (w *Writer) closeLocked() error {
	var err error
	if w.gz != nil {
		err = w.gz.Close()
		w.gz = nil
	}
	if w.file != nil {
		if e := w.file.Close(); e != nil && err == nil {
			err = e
		}
		w.file = nil
	}
	w.enc = nil
	w.day = ""
	return err
}

// Close flushes and closes the current daily file.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeLocked()
}
