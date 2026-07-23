package tcp

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// Enqueuer enqueues parsed payloads for downstream processing.
type Enqueuer interface {
	Enqueue(ctx context.Context, data []byte) (string, error)
}

// HooshnicsRawMirror is implemented by internal/hooshnics.Forwarder.
// Interface keeps tcp free of a hard import on hooshnics (no circular deps).
type HooshnicsRawMirror interface {
	ForwardTeltonikaAVL(imei string, frame []byte)
}

// Server accepts Teltonika device TCP connections.
type Server struct {
	cfg       config.TeltonikaConfig
	queue     Enqueuer
	mirror    HooshnicsRawMirror
	listener  net.Listener
	wg        sync.WaitGroup
	cancel    context.CancelFunc
	imeiAllow map[string]struct{}
}

// NewServer creates a Teltonika TCP server. Returns nil if TCP is disabled.
// mirror may be nil to disable Hooshnics forwarding.
func NewServer(cfg config.TeltonikaConfig, q Enqueuer, mirror HooshnicsRawMirror) *Server {
	if !cfg.TCPEnabled {
		return nil
	}

	allow := make(map[string]struct{}, len(cfg.IMEIWhitelist))
	for _, imei := range cfg.IMEIWhitelist {
		allow[imei] = struct{}{}
	}

	return &Server{
		cfg:       cfg,
		queue:     q,
		mirror:    mirror,
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
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("teltonika tcp listen: %w", err)
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
			s.wg.Add(1)
			go func(c net.Conn) {
				defer s.wg.Done()
				s.handleConn(runCtx, c)
			}(conn)
		}
	}()

	logger.Info("Teltonika TCP server started", zap.String("address", addr))
	return nil
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
	_ = conn.SetDeadline(time.Time{})

	sess := &session{
		conn:     conn,
		server:   s,
		readTO:   60 * time.Second,
		tzOffset: s.cfg.TimezoneOffset,
	}
	sess.run(ctx)
}
