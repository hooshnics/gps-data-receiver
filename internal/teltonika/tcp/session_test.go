package tcp_test

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/teltonika/tcp"
	"github.com/stretchr/testify/require"
)

var hexLinePattern = regexp.MustCompile(`[0-9a-fA-F]{16,}`)

type stubEnqueuer struct {
	payloads [][]byte
}

func (s *stubEnqueuer) Enqueue(_ context.Context, data []byte) (string, error) {
	s.payloads = append(s.payloads, append([]byte(nil), data...))
	return "1-0", nil
}

func loadSampleHex(t *testing.T, filename string) []byte {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, filename))
	require.NoError(t, err)
	match := hexLinePattern.FindString(string(data))
	require.NotEmpty(t, match)
	raw, err := hex.DecodeString(strings.TrimSpace(match))
	require.NoError(t, err)
	return raw
}

func TestTCPServer_IMEIHandshakeAndAVL(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	_ = ln.Close()

	port := strings.Split(addr, ":")[1]
	enqueue := &stubEnqueuer{}
	srv := tcp.NewServer(config.TeltonikaConfig{
		TCPEnabled:     true,
		TCPHost:        "127.0.0.1",
		TCPPort:        port,
		TimezoneOffset: 3*time.Hour + 30*time.Minute,
	}, enqueue)
	require.NotNil(t, srv)
	require.NoError(t, srv.Start(context.Background()))
	defer srv.Stop()

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	imei := "3520930864036555"
	handshake := make([]byte, 2+len(imei))
	binary.BigEndian.PutUint16(handshake[:2], uint16(len(imei)))
	copy(handshake[2:], imei)
	_, err = conn.Write(handshake)
	require.NoError(t, err)

	acceptBuf := make([]byte, 1)
	_, err = io.ReadFull(conn, acceptBuf)
	require.NoError(t, err)
	require.Equal(t, byte(0x01), acceptBuf[0])

	avlPacket := loadSampleHex(t, "sample data.txt")
	_, err = conn.Write(avlPacket)
	require.NoError(t, err)

	ackBuf := make([]byte, 4)
	_, err = io.ReadFull(conn, ackBuf)
	require.NoError(t, err)
	require.Equal(t, uint32(3), binary.BigEndian.Uint32(ackBuf))

	require.Len(t, enqueue.payloads, 1)
	require.Contains(t, string(enqueue.payloads[0]), `"_teltonika":true`)
}
