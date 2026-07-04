package storage

import (
	"context"
	"sync"
	"time"

	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

const defaultWriteQueueSize = 10000

type writeOpKind int

const (
	writeOpSuccess writeOpKind = iota
	writeOpFailed
)

type writeOp struct {
	kind         writeOpKind
	records      []parser.ParsedGPSData
	targetServer string
	errorMessage string
}

// AsyncWriter queues PostgreSQL writes on a background goroutine so delivery
// workers are not blocked on database I/O.
type AsyncWriter struct {
	store  *PostgresStore
	ch     chan writeOp
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewAsyncWriter starts a background writer for the given store.
func NewAsyncWriter(store *PostgresStore, queueSize int) *AsyncWriter {
	if store == nil {
		return nil
	}
	if queueSize <= 0 {
		queueSize = defaultWriteQueueSize
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := &AsyncWriter{
		store:  store,
		ch:     make(chan writeOp, queueSize),
		ctx:    ctx,
		cancel: cancel,
	}

	w.wg.Add(1)
	go w.run()

	return w
}

func (w *AsyncWriter) run() {
	defer w.wg.Done()

	for op := range w.ch {
		w.process(op)
	}
}

func (w *AsyncWriter) process(op writeOp) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	switch op.kind {
	case writeOpSuccess:
		err = w.store.StoreRecords(ctx, op.records)
	case writeOpFailed:
		err = w.store.StoreFailedRecords(ctx, op.records, op.targetServer, op.errorMessage)
	}

	if err != nil {
		logger.Warn("Async PostgreSQL write failed",
			zap.Error(err),
			zap.Int("record_count", len(op.records)))
	}
}

func (w *AsyncWriter) enqueue(op writeOp) {
	if w == nil || len(op.records) == 0 {
		return
	}

	select {
	case w.ch <- op:
	case <-w.ctx.Done():
	default:
		go func() {
			select {
			case w.ch <- op:
			case <-w.ctx.Done():
			}
		}()
	}
}

// EnqueueSuccess schedules successfully delivered records for async storage.
func (w *AsyncWriter) EnqueueSuccess(records []parser.ParsedGPSData) {
	w.enqueue(writeOp{kind: writeOpSuccess, records: records})
}

// EnqueueFailed schedules failed delivery records for async storage.
func (w *AsyncWriter) EnqueueFailed(records []parser.ParsedGPSData, targetServer, errorMessage string) {
	w.enqueue(writeOp{
		kind:         writeOpFailed,
		records:      records,
		targetServer: targetServer,
		errorMessage: errorMessage,
	})
}

// Close stops the writer and waits for in-flight writes to finish.
func (w *AsyncWriter) Close() {
	if w == nil {
		return
	}

	w.cancel()
	close(w.ch)
	w.wg.Wait()
}