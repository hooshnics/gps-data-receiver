package tcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/gps-data-receiver/internal/parser"
	"github.com/gps-data-receiver/internal/teltonika/codec"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

type session struct {
	conn     net.Conn
	server   *Server
	readTO   time.Duration
	tzOffset time.Duration
}

func (s *session) run(ctx context.Context) {
	remote := s.conn.RemoteAddr().String()
	imei, err := s.readIMEI()
	if err != nil {
		logger.Debug("Teltonika IMEI handshake failed",
			zap.String("remote", remote),
			zap.Error(err))
		return
	}

	if !s.server.isIMEIAllowed(imei) {
		_, _ = s.conn.Write([]byte{0x00})
		logger.Warn("Teltonika IMEI rejected",
			zap.String("remote", remote),
			zap.String("imei", imei))
		return
	}

	if _, err := s.conn.Write([]byte{0x01}); err != nil {
		logger.Debug("Teltonika IMEI accept write failed", zap.Error(err))
		return
	}

	logger.Info("Teltonika device connected",
		zap.String("remote", remote),
		zap.String("imei", imei))

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		frame, err := s.readFrame()
		if err != nil {
			if err != io.EOF {
				logger.Debug("Teltonika session read ended",
					zap.String("imei", imei),
					zap.Error(err))
			}
			return
		}

		accepted := s.handleFrame(ctx, imei, frame)
		if err := s.writeAck(accepted); err != nil {
			logger.Debug("Teltonika ack write failed",
				zap.String("imei", imei),
				zap.Error(err))
			return
		}
	}
}

func (s *session) readIMEI() (string, error) {
	if err := s.conn.SetReadDeadline(time.Now().Add(s.readTO)); err != nil {
		return "", err
	}

	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(s.conn, lenBuf); err != nil {
		return "", err
	}
	imeiLen := binary.BigEndian.Uint16(lenBuf)
	if imeiLen == 0 || imeiLen > 32 {
		return "", fmt.Errorf("invalid IMEI length: %d", imeiLen)
	}

	imeiBuf := make([]byte, imeiLen)
	if _, err := io.ReadFull(s.conn, imeiBuf); err != nil {
		return "", err
	}
	return string(imeiBuf), nil
}

func (s *session) readFrame() ([]byte, error) {
	if err := s.conn.SetReadDeadline(time.Now().Add(s.readTO)); err != nil {
		return nil, err
	}

	header := make([]byte, 8)
	if _, err := io.ReadFull(s.conn, header); err != nil {
		return nil, err
	}
	if header[0]|header[1]|header[2]|header[3] != 0 {
		return nil, fmt.Errorf("invalid preamble: % X", header[:4])
	}

	dataFieldLen := binary.BigEndian.Uint32(header[4:8])
	if dataFieldLen == 0 || dataFieldLen > 2048 {
		return nil, fmt.Errorf("invalid data field length: %d", dataFieldLen)
	}

	payload := make([]byte, dataFieldLen+4)
	if _, err := io.ReadFull(s.conn, payload); err != nil {
		return nil, err
	}

	frame := make([]byte, 8+len(payload))
	copy(frame, header)
	copy(frame[8:], payload)
	return frame, nil
}

func (s *session) handleFrame(ctx context.Context, imei string, frame []byte) uint32 {
	// Async Hooshnics mirror first (non-blocking). Does not affect local parse/enqueue/ACK for PiStat.
	if s.server.mirror != nil {
		s.server.mirror.ForwardTeltonikaAVL(imei, frame)
	}

	_, records, err := codec.ParsePacket(frame)
	if err != nil {
		logger.Warn("Teltonika packet parse failed",
			zap.String("imei", imei),
			zap.Error(err))
		return 0
	}

	parsed := parser.TeltonikaRecordsToParsed(records, imei, s.tzOffset)
	if len(parsed) == 0 {
		return 0
	}

	envelope, err := parser.ParseTeltonikaEnvelope(imei, parsed)
	if err != nil {
		logger.Error("Teltonika envelope marshal failed",
			zap.String("imei", imei),
			zap.Error(err))
		return 0
	}

	if _, err := s.server.queue.Enqueue(ctx, envelope); err != nil {
		logger.Error("Teltonika enqueue failed",
			zap.String("imei", imei),
			zap.Error(err))
		// ACK accepted records anyway; device should not resend valid data.
	}

	return uint32(len(parsed))
}

func (s *session) writeAck(count uint32) error {
	if err := s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	ack := make([]byte, 4)
	binary.BigEndian.PutUint32(ack, count)
	_, err := s.conn.Write(ack)
	return err
}
