package tcp

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

const (
	defaultReadTimeout     = 180 * time.Second
	defaultWriteTimeout    = 15 * time.Second
	defaultIngestTimeout   = 15 * time.Second
	defaultKeepAlivePeriod = 30 * time.Second
	maxIngestAttempts      = 3
	listenBacklogHint      = 4096
)

// Server accepts Teltonika device TCP connections.
// Durable ingest (Redis XADD of raw AVL) happens on the session hot path before ACK.
type Server struct {
	cfg      config.TeltonikaConfig
	raw      RawIngester
	listener net.Listener
	wg       sync.WaitGroup
	cancel   context.CancelFunc
	imeiAllow map[string]struct{}
	active    atomic.Int64
}

// NewServer creates a Teltonika TCP server. Returns nil if TCP is disabled.
// raw must be a durable RawIngester (Redis); ACK is withheld if EnqueueRawAVL fails.
func NewServer(cfg config.TeltonikaConfig, raw RawIngester) *Server {
	if !cfg.TCPEnabled {
		return nil
	}
	if raw == nil {
		logger.Error("Teltonika TCP enabled but RawIngester is nil — refusing to start")
		return nil
	}

	allow := make(map[string]struct{}, len(cfg.IMEIWhitelist))
	for _, imei := range cfg.IMEIWhitelist {
		allow[imei] = struct{}{}
	}

	return &Server{
		cfg:       cfg,
		raw:       raw,
		imeiAllow: allow,
	}
}

func (s *Server) isIMEIAllowed(imei string) bool {
	if len(s.imeiAllow) == 0 {
		return true
	}
	_, ok := s.imeiAllow[imei]
	return ok
}

// Start listens for incoming device connections.
func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}

	addr := fmt.Sprintf("%s:%s", s.cfg.TCPHost, s.cfg.TCPPort)
	ln, err := listenTCP(addr)
	if err != nil {
		return fmt.Errorf("teltonika tcp listen: %w", err)
	}

	if len(s.imeiAllow) == 0 {
		logger.Warn("Teltonika TCP IMEI whitelist empty — accepting all IMEIs (set TELTONIKA_IMEI_WHITELIST in production)")
	} else {
		logger.Info("Teltonika TCP IMEI whitelist active", zap.Int("count", len(s.imeiAllow)))
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.listener = ln

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-runCtx.Done():
					return
				default:
					logger.Warn("Teltonika TCP accept failed", zap.Error(err))
					continue
				}
			}
			configureConn(conn)
			if max := s.maxConnections(); max > 0 && s.active.Load() >= max {
				logger.Warn("Teltonika TCP connection limit reached — rejecting",
					zap.Int64("active", s.active.Load()),
					zap.Int64("max", max))
				_ = conn.Close()
				continue
			}
			s.wg.Add(1)
			go func(c net.Conn) {
				defer s.wg.Done()
				s.handleConn(runCtx, c)
			}(conn)
		}
	}()

	logger.Info("Teltonika TCP server started (zero-loss durable ingest)",
		zap.String("address", addr),
		zap.Int64("max_connections", s.maxConnections()),
		zap.Duration("ingest_timeout", defaultIngestTimeout),
		zap.Int("listen_backlog_hint", listenBacklogHint))
	return nil
}

func (s *Server) maxConnections() int64 {
	if s.cfg.MaxConnections <= 0 {
		return 10000
	}
	return int64(s.cfg.MaxConnections)
}

func configureConn(conn net.Conn) {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tc.SetNoDelay(true)
	_ = tc.SetKeepAlive(true)
	_ = tc.SetKeepAlivePeriod(defaultKeepAlivePeriod)
}

// Stop closes the listener and waits for active sessions.
func (s *Server) Stop() {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.wg.Wait()
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	n := s.active.Add(1)
	if metrics.AppMetrics != nil {
		metrics.AppMetrics.SetTeltonikaActiveConnections(n)
	}
	defer func() {
		left := s.active.Add(-1)
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.SetTeltonikaActiveConnections(left)
		}
	}()

	sess := &session{
		conn:     conn,
		server:   s,
		readTO:   defaultReadTimeout,
		writeTO:  defaultWriteTimeout,
		ingestTO: defaultIngestTimeout,
	}
	sess.run(ctx)
}
