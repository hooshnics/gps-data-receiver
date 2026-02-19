package storage

import (
	"context"
	"sync"
	"time"

	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// FailedPacketJob is a single failed packet to be saved asynchronously
type FailedPacketJob struct {
	Payload      []byte
	RetryCount   int
	LastError    string
	TargetServer string
}

// FailedPacketSaver saves failed packets in a background goroutine so workers are not blocked.
type FailedPacketSaver struct {
	repo   *FailedPacketRepository
	jobs   chan FailedPacketJob
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewFailedPacketSaver creates a saver that processes jobs in the background. Buffer size should be
// large enough to absorb bursts (e.g. 500–2000). Dropped jobs are logged when buffer is full.
func NewFailedPacketSaver(repo *FailedPacketRepository, bufferSize int) *FailedPacketSaver {
	ctx, cancel := context.WithCancel(context.Background())
	s := &FailedPacketSaver{
		repo:   repo,
		jobs:   make(chan FailedPacketJob, bufferSize),
		ctx:    ctx,
		cancel: cancel,
	}
	s.wg.Add(1)
	go s.run()
	return s
}

// Submit enqueues a failed packet for saving. Non-blocking; drops and logs if buffer is full.
func (s *FailedPacketSaver) Submit(job FailedPacketJob) {
	select {
	case s.jobs <- job:
		// queued
	default:
		logger.Error("Failed packet saver buffer full, dropping packet",
			zap.String("target_server", job.TargetServer),
			zap.Int("retry_count", job.RetryCount))
	}
}

func (s *FailedPacketSaver) run() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case job, ok := <-s.jobs:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := s.repo.SaveFailedPacket(ctx, job.Payload, job.RetryCount, job.LastError, job.TargetServer)
			cancel()
			if err != nil {
				logger.Error("Failed to save failed packet (async)",
					zap.Error(err),
					zap.String("target_server", job.TargetServer))
			} else if metrics.AppMetrics != nil {
				metrics.AppMetrics.RecordFailedPacketStored()
			}
		}
	}
}

// Shutdown stops the saver and drains remaining jobs (with a timeout).
func (s *FailedPacketSaver) Shutdown(timeout time.Duration) {
	s.cancel()
	close(s.jobs)
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(timeout):
		logger.Warn("FailedPacketSaver shutdown timed out with jobs still pending")
	}
}
