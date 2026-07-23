package tcp

import (
	"context"
	"time"

	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

const (
	defaultPipelineWorkers = 64
	defaultPipelineBuffer  = 100_000
	defaultReadTimeout     = 120 * time.Second
	defaultWriteTimeout    = 10 * time.Second
	defaultKeepAlivePeriod = 30 * time.Second
)

// postAckJob is work that must NOT run on the TCP hot path (after ACK is written).
type postAckJob struct {
	imei     string
	frame    []byte // raw AVL for Hooshnics mirror
	envelope []byte // parsed queue payload for Redis / PiStat workers
}

func (s *Server) startPipeline() {
	s.jobs = make(chan postAckJob, defaultPipelineBuffer)
	for i := 0; i < defaultPipelineWorkers; i++ {
		s.pipelineWG.Add(1)
		go s.pipelineWorker()
	}
	logger.Info("Teltonika post-ACK pipeline started",
		zap.Int("workers", defaultPipelineWorkers),
		zap.Int("buffer", defaultPipelineBuffer))
}

func (s *Server) stopPipeline() {
	if s.jobs == nil {
		return
	}
	close(s.jobs)
	s.pipelineWG.Wait()
	s.jobs = nil
	logger.Info("Teltonika post-ACK pipeline stopped")
}

// submitPostAck queues Redis enqueue + Hooshnics mirror without blocking the TCP session.
// If the buffer is full under extreme load, the job is dropped and a warning is logged.
func (s *Server) submitPostAck(job postAckJob) {
	if s == nil || s.jobs == nil {
		return
	}
	select {
	case s.jobs <- job:
	default:
		logger.Warn("Teltonika post-ACK pipeline full — dropping enqueue/mirror job",
			zap.String("imei", job.imei),
			zap.Int("envelope_size", len(job.envelope)),
			zap.Int("frame_size", len(job.frame)))
	}
}

func (s *Server) pipelineWorker() {
	defer s.pipelineWG.Done()
	for job := range s.jobs {
		s.processPostAck(job)
	}
}

func (s *Server) processPostAck(job postAckJob) {
	// Hooshnics mirror is itself async/non-blocking; call from worker so TCP never waits on HTTP.
	if s.mirror != nil && len(job.frame) > 0 {
		s.mirror.ForwardTeltonikaAVL(job.imei, job.frame)
	}

	if len(job.envelope) == 0 || s.queue == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := s.queue.Enqueue(ctx, job.envelope); err != nil {
		logger.Error("Teltonika async enqueue failed",
			zap.String("imei", job.imei),
			zap.Error(err))
	}
}
