package tcp_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/teltonika/codec"
	"github.com/gps-data-receiver/internal/teltonika/tcp"
	"github.com/stretchr/testify/require"
)

type stubRawIngester struct {
	mu       sync.Mutex
	frames   [][]byte
	imeis    []string
	failNext atomic.Bool
	calls    atomic.Int32
}

func (s *stubRawIngester) EnqueueRawAVL(_ context.Context, imei string, frame []byte, _ string) (string, error) {
	s.calls.Add(1)
	if s.failNext.Load() {
		return "", context.DeadlineExceeded
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.imeis = append(s.imeis, imei)
	s.frames = append(s.frames, append([]byte(nil), frame...))
	return "1-0", nil
}

func (s *stubRawIngester) Count() int {
	return int(s.calls.Load())
}

func buildMinimalCodec8Packet(t *testing.T) []byte {
	t.Helper()
	record := make([]byte, 0, 32)
	record = append(record, make([]byte, 8)...)  // timestamp
	record = append(record, 0x00)                // priority
	record = append(record, make([]byte, 15)...) // GPS
	record = append(record, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)

	dataField := []byte{0x08, 0x01}
	dataField = append(dataField, record...)
	dataField = append(dataField, 0x01)

	crc := codec.CRC16IBM(dataField)
	frame := make([]byte, 0, 8+len(dataField)+4)
	frame = append(frame, 0, 0, 0, 0)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(dataField)))
	frame = append(frame, lenBuf...)
	frame = append(frame, dataField...)
	crcBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBuf, uint32(crc))
	frame = append(frame, crcBuf...)

	n, err := codec.CountAVLRecords(frame)
	require.NoError(t, err)
	require.Equal(t, uint32(1), n)
	return frame
}

func startTestServer(t *testing.T, raw tcp.RawIngester) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr = ln.Addr().String()
	_ = ln.Close()

	port := strings.Split(addr, ":")[1]
	srv := tcp.NewServer(config.TeltonikaConfig{
		TCPEnabled: true,
		TCPHost:    "127.0.0.1",
		TCPPort:    port,
	}, raw)
	require.NotNil(t, srv)
	require.NoError(t, srv.Start(context.Background()))
	return addr, srv.Stop
}

func imeiHandshake(t *testing.T, conn net.Conn, imei string) {
	t.Helper()
	handshake := make([]byte, 2+len(imei))
	binary.BigEndian.PutUint16(handshake[:2], uint16(len(imei)))
	copy(handshake[2:], imei)
	_, err := conn.Write(handshake)
	require.NoError(t, err)

	acceptBuf := make([]byte, 1)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = io.ReadFull(conn, acceptBuf)
	require.NoError(t, err)
	require.Equal(t, byte(0x01), acceptBuf[0])
}

func TestTCPServer_ACKOnlyAfterDurableIngest(t *testing.T) {
	raw := &stubRawIngester{}
	addr, stop := startTestServer(t, raw)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	imeiHandshake(t, conn, "356890085205101")
	packet := buildMinimalCodec8Packet(t)
	_, err = conn.Write(packet)
	require.NoError(t, err)

	ackBuf := make([]byte, 4)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = io.ReadFull(conn, ackBuf)
	require.NoError(t, err)
	require.Equal(t, uint32(1), binary.BigEndian.Uint32(ackBuf))
	require.Equal(t, 1, raw.Count())
}

func TestTCPServer_NoACKWhenDurableIngestFails(t *testing.T) {
	raw := &stubRawIngester{}
	raw.failNext.Store(true)
	addr, stop := startTestServer(t, raw)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	imeiHandshake(t, conn, "356890085205101")
	packet := buildMinimalCodec8Packet(t)
	_, err = conn.Write(packet)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	ackBuf := make([]byte, 4)
	_, err = io.ReadFull(conn, ackBuf)
	require.Error(t, err, "ACK must be withheld when Redis ingest fails")

	// Session must stay open: recover and ACK the next packet.
	raw.failNext.Store(false)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write(packet)
	require.NoError(t, err)
	_, err = io.ReadFull(conn, ackBuf)
	require.NoError(t, err, "session should survive ingest failure and ACK later packets")
	require.Equal(t, uint32(1), binary.BigEndian.Uint32(ackBuf))
}

func TestTCPServer_MultiplePacketsDurable(t *testing.T) {
	raw := &stubRawIngester{}
	addr, stop := startTestServer(t, raw)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	imeiHandshake(t, conn, "356890085205101")
	packet := buildMinimalCodec8Packet(t)

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < 5; i++ {
		_, err = conn.Write(packet)
		require.NoError(t, err)
		ackBuf := make([]byte, 4)
		_, err = io.ReadFull(conn, ackBuf)
		require.NoError(t, err, "packet %d", i+1)
		require.Equal(t, uint32(1), binary.BigEndian.Uint32(ackBuf))
	}
	require.Equal(t, 5, raw.Count())
}
