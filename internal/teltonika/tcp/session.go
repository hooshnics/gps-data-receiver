package tcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gps-data-receiver/internal/metrics"
	"github.com/gps-data-receiver/internal/teltonika/codec"
	"github.com/gps-data-receiver/pkg/logger"
	"go.uber.org/zap"
)

// RawIngester durably persists a raw Teltonika AVL frame before TCP ACK.
// sourceIP may be empty. Implemented by queue.RedisQueue.EnqueueRawAVL.
type RawIngester interface {
	EnqueueRawAVL(ctx context.Context, imei string, frame []byte, sourceIP string) (string, error)
}

type session struct {
	conn     net.Conn
	server   *Server
	readTO   time.Duration
	writeTO  time.Duration
	ingestTO time.Duration
}

var headerPool = sync.Pool{New: func() any { return make([]byte, 8) }}

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
		_ = s.writeBytes([]byte{0x00})
		logger.Warn("Teltonika IMEI rejected",
			zap.String("remote", remote),
			zap.String("imei", imei))
		return
	}

	if err := s.writeBytes([]byte{0x01}); err != nil {
		logger.Debug("Teltonika IMEI accept write failed", zap.Error(err))
		return
	}

	logger.Debug("Teltonika device connected",
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
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.IncTeltonikaReceived()
		}

		count, err := codec.CountAVLRecords(frame)
		if err != nil {
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.IncTeltonikaCRCFailure()
			}
			logger.Warn("Teltonika packet validate failed",
				zap.String("imei", imei),
				zap.Int("frame_size", len(frame)),
				zap.Error(err))
			if werr := s.writeAck(0); werr != nil {
				return
			}
			continue
		}

		msgID, err := s.durableIngest(ctx, imei, frame)
		if err != nil {
			if metrics.AppMetrics != nil {
				metrics.AppMetrics.IncTeltonikaIngestError()
			}
			logger.Error("Teltonika durable ingest failed — withholding ACK (session kept for device retransmit)",
				zap.String("imei", imei),
				zap.Int("frame_size", len(frame)),
				zap.Error(err))
			// Zero-Drop: never ACK. Keep TCP session open so the device can
			// retransmit without a reconnect storm under Redis blips.
			continue
		}

		if err := s.writeAck(count); err != nil {
			logger.Debug("Teltonika ack write failed",
				zap.String("imei", imei),
				zap.String("redis_id", msgID),
				zap.Error(err))
			return
		}
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.IncTeltonikaAcked()
		}
		logger.Debug("Teltonika ACK after durable XADD",
			zap.String("imei", imei),
			zap.String("redis_id", msgID),
			zap.Uint32("ack_count", count),
			zap.Int("frame_size", len(frame)))
	}
}

// durableIngest retries Redis XADD briefly; ACK is still only issued by the caller on success.
func (s *session) durableIngest(ctx context.Context, imei string, frame []byte) (string, error) {
	var (
		msgID string
		err   error
	)
	for attempt := 1; attempt <= maxIngestAttempts; attempt++ {
		ingestCtx, cancel := context.WithTimeout(ctx, s.ingestTO)
		ingestStart := time.Now()
		msgID, err = s.server.raw.EnqueueRawAVL(ingestCtx, imei, frame, remoteIP(s.conn))
		if metrics.AppMetrics != nil {
			metrics.AppMetrics.ObserveTeltonikaRedisWrite(time.Since(ingestStart))
		}
		cancel()
		if err == nil {
			return msgID, nil
		}
		if attempt == maxIngestAttempts {
			break
		}
		backoff := time.Duration(attempt) * 50 * time.Millisecond
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(backoff):
		}
	}
	return "", err
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

	header := headerPool.Get().([]byte)
	defer headerPool.Put(header)

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

func remoteIP(conn net.Conn) string {
	if conn == nil || conn.RemoteAddr() == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

func (s *session) writeAck(count uint32) error {
	ack := [4]byte{}
	binary.BigEndian.PutUint32(ack[:], count)
	return s.writeBytes(ack[:])
}

func (s *session) writeBytes(b []byte) error {
	if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeTO)); err != nil {
		return err
	}
	_, err := s.conn.Write(b)
	_ = s.conn.SetWriteDeadline(time.Time{})
	return err
}
