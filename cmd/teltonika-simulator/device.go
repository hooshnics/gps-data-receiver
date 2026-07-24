package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

type deviceConfig struct {
	host           string
	port           int
	imei           string
	index          int
	interval       time.Duration
	recordsPerPkt  int
	codecName      string // "8" or "8e"
	ackTimeout     time.Duration
	duration       time.Duration
	offlineEvery   int // after N successful ACKs, simulate disconnect+buffer
	retransmitOnce bool
	lat, lon       float64
}

type deviceResult struct {
	imei string
	err  error
}

func runDevice(cfg deviceConfig, counters *runCounters, ackLat *latencyStats, stop <-chan struct{}) deviceResult {
	deadline := time.Now().Add(cfg.duration)
	var offlineBuf [][]byte
	successSinceOffline := 0

	for time.Now().Before(deadline) {
		select {
		case <-stop:
			return deviceResult{imei: cfg.imei}
		default:
		}

		conn, err := dialAndLogin(cfg)
		if err != nil {
			counters.failed.Add(1)
			counters.reconnect.Add(1)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Flush offline buffer (retransmission simulation)
		for len(offlineBuf) > 0 {
			pkt := offlineBuf[0]
			if err := sendAwaitACK(conn, pkt, cfg.ackTimeout, counters, ackLat); err != nil {
				_ = conn.Close()
				counters.reconnect.Add(1)
				goto reconnect
			}
			offlineBuf = offlineBuf[1:]
		}

		for time.Now().Before(deadline) {
			select {
			case <-stop:
				_ = conn.Close()
				return deviceResult{imei: cfg.imei}
			default:
			}

			pkt := buildAVLPacket(cfg.codecName, cfg.index, cfg.recordsPerPkt, time.Now().UTC(), cfg.lat, cfg.lon)
			counters.sent.Add(1)

			if cfg.offlineEvery > 0 && successSinceOffline >= cfg.offlineEvery {
				// Simulate radio drop: keep packet in device buffer, reconnect later.
				offlineBuf = append(offlineBuf, pkt)
				if cfg.retransmitOnce {
					offlineBuf = append(offlineBuf, append([]byte(nil), pkt...)) // duplicate retransmit
				}
				successSinceOffline = 0
				_ = conn.Close()
				counters.reconnect.Add(1)
				time.Sleep(cfg.interval)
				goto reconnect
			}

			if err := sendAwaitACK(conn, pkt, cfg.ackTimeout, counters, ackLat); err != nil {
				offlineBuf = append(offlineBuf, pkt)
				_ = conn.Close()
				counters.reconnect.Add(1)
				time.Sleep(200 * time.Millisecond)
				goto reconnect
			}
			successSinceOffline++
			time.Sleep(cfg.interval)
		}
		_ = conn.Close()
		return deviceResult{imei: cfg.imei}
	reconnect:
		continue
	}
	return deviceResult{imei: cfg.imei}
}

func dialAndLogin(cfg deviceConfig) (net.Conn, error) {
	addr := fmt.Sprintf("%s:%d", cfg.host, cfg.port)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	imei := []byte(cfg.imei)
	hs := make([]byte, 2+len(imei))
	binary.BigEndian.PutUint16(hs[:2], uint16(len(imei)))
	copy(hs[2:], imei)
	_ = conn.SetDeadline(time.Now().Add(cfg.ackTimeout))
	if _, err := conn.Write(hs); err != nil {
		_ = conn.Close()
		return nil, err
	}
	ack := make([]byte, 1)
	if _, err := io.ReadFull(conn, ack); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if ack[0] != 0x01 {
		_ = conn.Close()
		return nil, fmt.Errorf("IMEI rejected: 0x%02x", ack[0])
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func sendAwaitACK(conn net.Conn, pkt []byte, timeout time.Duration, counters *runCounters, ackLat *latencyStats) error {
	_ = conn.SetDeadline(time.Now().Add(timeout))
	start := time.Now()
	if _, err := conn.Write(pkt); err != nil {
		counters.failed.Add(1)
		return err
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		counters.timeouts.Add(1)
		counters.failed.Add(1)
		return err
	}
	elapsed := time.Since(start)
	ackLat.add(elapsed)
	n := binary.BigEndian.Uint32(buf)
	if n == 0 {
		counters.failed.Add(1)
		return fmt.Errorf("ACK=0")
	}
	counters.acked.Add(1)
	_ = conn.SetDeadline(time.Time{})
	return nil
}
