package rawlog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	gojson "github.com/goccy/go-json"
	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

var imeiPattern = regexp.MustCompile(`^\d{15}$`)

type dataItem struct {
	Data string `json:"data"`
}

type logEntry struct {
	imei string
	data string
	at   time.Time
}

// Logger asynchronously appends raw device payloads to per-IMEI daily text files.
type Logger struct {
	baseDir   string
	ch        chan logEntry
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
	locks     sync.Map
}

// New creates a raw data logger. Returns nil when disabled.
func New(enabled bool, baseDir string, bufferSize int) *Logger {
	if !enabled {
		return nil
	}
	if bufferSize <= 0 {
		bufferSize = 10000
	}
	l := &Logger{
		baseDir: baseDir,
		ch:      make(chan logEntry, bufferSize),
		done:    make(chan struct{}),
	}
	l.wg.Add(1)
	go l.worker()
	return l
}

// LogRecords queues raw payloads grouped by IMEI, preserving the original JSON array structure.
func (l *Logger) LogRecords(records []parser.ParsedGPSData) {
	if l == nil || len(records) == 0 {
		return
	}

	byIMEI := make(map[string][]string)
	for _, record := range records {
		if record.RawData == "" || !imeiPattern.MatchString(record.IMEI) {
			continue
		}
		byIMEI[record.IMEI] = append(byIMEI[record.IMEI], record.RawData)
	}

	now := time.Now()
	for imei, rawItems := range byIMEI {
		payload, err := formatPayload(rawItems)
		if err != nil {
			logger.Warn("Failed to format raw log payload",
				zap.String("imei", imei),
				zap.Error(err))
			continue
		}

		select {
		case <-l.done:
			return
		case l.ch <- logEntry{imei: imei, data: payload, at: now}:
		default:
			logger.Warn("Raw log buffer full, dropping entry",
				zap.String("imei", imei))
		}
	}
}

func formatPayload(rawItems []string) (string, error) {
	items := make([]dataItem, len(rawItems))
	for i, raw := range rawItems {
		items[i] = dataItem{Data: raw}
	}
	encoded, err := gojson.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("marshal raw payload: %w", err)
	}
	return string(encoded), nil
}

// Close stops the background worker after draining queued entries.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.closeOnce.Do(func() {
		close(l.done)
	})
	l.wg.Wait()
}

func (l *Logger) worker() {
	defer l.wg.Done()
	for {
		select {
		case <-l.done:
			l.drain()
			return
		case entry := <-l.ch:
			l.writeEntry(entry)
		}
	}
}

func (l *Logger) drain() {
	for {
		select {
		case entry := <-l.ch:
			l.writeEntry(entry)
		default:
			return
		}
	}
}

func (l *Logger) writeEntry(entry logEntry) {
	filePath, err := l.filePath(entry.imei, entry.at)
	if err != nil {
		logger.Warn("Skipping raw log entry",
			zap.String("imei", entry.imei),
			zap.Error(err))
		return
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		logger.Warn("Failed to create raw log directory",
			zap.String("path", filepath.Dir(filePath)),
			zap.Error(err))
		return
	}

	mu := l.fileLock(filePath)
	mu.Lock()
	defer mu.Unlock()

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Warn("Failed to open raw log file",
			zap.String("path", filePath),
			zap.Error(err))
		return
	}
	defer f.Close()

	if _, err := f.WriteString(entry.data + "\n"); err != nil {
		logger.Warn("Failed to write raw log entry",
			zap.String("path", filePath),
			zap.Error(err))
	}
}

func (l *Logger) filePath(imei string, at time.Time) (string, error) {
	if !imeiPattern.MatchString(imei) {
		return "", fmt.Errorf("invalid imei: %s", imei)
	}
	fileName := at.Format("2006-01-02") + ".txt"
	return filepath.Join(l.baseDir, imei, fileName), nil
}

func (l *Logger) fileLock(path string) *sync.Mutex {
	value, _ := l.locks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}
