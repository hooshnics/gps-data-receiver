package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gps-data-receiver/internal/config"
	"github.com/gps-data-receiver/internal/queue"
	"github.com/gps-data-receiver/internal/teltonika/codec"
	"github.com/joho/godotenv"
)

// ACK latency benchmark for the Zero-Drop critical path:
//   TCP receive → CRC (CountAVLRecords) → Redis XADD → TCP ACK
//
// Modes:
//   -tcp   : end-to-end against a running GPS receiver (:5055)
//   -local : microbench CRC + XADD only (no TCP), requires Redis
func main() {
	_ = godotenv.Load()

	var (
		mode       = flag.String("mode", "tcp", "tcp | local")
		host       = flag.String("host", "127.0.0.1", "TCP host (tcp mode)")
		port       = flag.Int("port", 5055, "TCP port (tcp mode)")
		sessions   = flag.Int("sessions", 50, "concurrent TCP sessions / workers")
		packets    = flag.Int("packets", 200, "packets per session")
		records    = flag.Int("records", 1, "records per Codec8 packet")
		ackTimeout = flag.Duration("ack-timeout", 15*time.Second, "ACK timeout")
		imeiBase   = flag.Int("imei-base", 100000, "IMEI numeric base")
	)
	flag.Parse()

	switch *mode {
	case "tcp":
		runTCPBench(*host, *port, *sessions, *packets, *records, *ackTimeout, *imeiBase)
	case "local":
		runLocalXADDBench(*sessions, *packets, *records)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", *mode)
		os.Exit(2)
	}
}

func runTCPBench(host string, port, sessions, packets, records int, ackTimeout time.Duration, imeiBase int) {
	fmt.Println("=== ACK Latency Benchmark (TCP end-to-end) ===")
	fmt.Printf("target=%s:%d sessions=%d packets/session=%d records=%d\n", host, port, sessions, packets, records)
	fmt.Println("path: TCP → CRC → Redis XADD → ACK  (Zero-Drop unchanged)")

	var (
		latMu sync.Mutex
		lats  []time.Duration
		okN   atomic.Int64
		failN atomic.Int64
	)

	var wg sync.WaitGroup
	startAll := time.Now()
	for s := 0; s < sessions; s++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			imei := fmt.Sprintf("35689008%07d", imeiBase+idx)
			addr := fmt.Sprintf("%s:%d", host, port)
			conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
			if err != nil {
				failN.Add(int64(packets))
				return
			}
			defer conn.Close()
			if tc, ok := conn.(*net.TCPConn); ok {
				_ = tc.SetNoDelay(true)
			}
			if err := imeiLogin(conn, imei, ackTimeout); err != nil {
				failN.Add(int64(packets))
				return
			}
			for p := 0; p < packets; p++ {
				pkt := buildMinimalPacket(idx, records, time.Now().UTC())
				_ = conn.SetDeadline(time.Now().Add(ackTimeout))
				t0 := time.Now()
				if _, err := conn.Write(pkt); err != nil {
					failN.Add(1)
					return
				}
				buf := make([]byte, 4)
				if _, err := io.ReadFull(conn, buf); err != nil {
					failN.Add(1)
					return
				}
				d := time.Since(t0)
				if binary.BigEndian.Uint32(buf) == 0 {
					failN.Add(1)
					continue
				}
				okN.Add(1)
				latMu.Lock()
				lats = append(lats, d)
				latMu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	printLatencyReport("TCP ACK RTT", time.Since(startAll), okN.Load(), failN.Load(), lats)
}

func runLocalXADDBench(workers, packets, records int) {
	fmt.Println("=== ACK Path Microbench (CRC + Redis XADD only) ===")
	fmt.Println("NOTE: excludes TCP stack; isolates durable write cost with AOF everysec")

	redisCfg := redisConfigFromEnv()
	q, err := queue.NewRedisQueue(redisCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "redis: %v\n", err)
		os.Exit(1)
	}
	defer q.Close()

	var (
		latMu sync.Mutex
		lats  []time.Duration
		okN   atomic.Int64
		failN atomic.Int64
	)
	var wg sync.WaitGroup
	startAll := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			imei := fmt.Sprintf("35689008%07d", 200000+idx)
			for p := 0; p < packets; p++ {
				frame := buildMinimalPacket(idx, records, time.Now().UTC())
				t0 := time.Now()
				if _, err := codec.CountAVLRecords(frame); err != nil {
					failN.Add(1)
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := q.EnqueueRawAVL(ctx, imei, frame, "")
				cancel()
				d := time.Since(t0)
				if err != nil {
					failN.Add(1)
					continue
				}
				okN.Add(1)
				latMu.Lock()
				lats = append(lats, d)
				latMu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	printLatencyReport("CRC+XADD", time.Since(startAll), okN.Load(), failN.Load(), lats)
}

func redisConfigFromEnv() *config.RedisConfig {
	cfg := &config.RedisConfig{
		Host:                 envOr("REDIS_HOST", "127.0.0.1"),
		Port:                 envOr("REDIS_PORT", "6379"),
		Password:             os.Getenv("REDIS_PASSWORD"),
		DB:                   0,
		PoolSize:             100,
		StreamName:           envOr("REDIS_STREAM_NAME", "gps:reports"),
		ConsumerGroup:        envOr("REDIS_CONSUMER_GROUP", "gps-workers"),
		RawStreamName:        envOr("REDIS_RAW_STREAM", "gps:raw_incoming"),
		RawConsumerGroup:     envOr("REDIS_RAW_CONSUMER_GROUP", "gps-raw-workers"),
		DeadLetterStream:     envOr("REDIS_DEAD_LETTER_STREAM", "gps:dead_letter"),
		HooshnicsRetryStream: envOr("REDIS_HOOSHNICS_RETRY_STREAM", "gps:hooshnics_retry"),
		PistatRetryStream:    envOr("REDIS_PISTAT_RETRY_STREAM", "gps:pistat_retry"),
		SentinelMaster:       envOr("REDIS_SENTINEL_MASTER", "mymaster"),
		DedupeTTL:            24 * time.Hour,
	}
	if v := os.Getenv("REDIS_SENTINEL_ADDRS"); v != "" {
		cfg.SentinelAddrs = splitCSV(v)
	}
	return cfg
}

func imeiLogin(conn net.Conn, imei string, timeout time.Duration) error {
	b := []byte(imei)
	hs := make([]byte, 2+len(b))
	binary.BigEndian.PutUint16(hs[:2], uint16(len(b)))
	copy(hs[2:], b)
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(hs); err != nil {
		return err
	}
	ack := make([]byte, 1)
	if _, err := io.ReadFull(conn, ack); err != nil {
		return err
	}
	if ack[0] != 0x01 {
		return fmt.Errorf("IMEI rejected")
	}
	return nil
}

func buildMinimalPacket(seed, records int, t time.Time) []byte {
	// reuse simulator packet builder logic inline (same CRC rules)
	n := records
	if n < 1 {
		n = 1
	}
	if n > 40 {
		n = 40
	}
	var body []byte
	for i := 0; i < n; i++ {
		ts := uint64(t.Add(time.Duration(i) * time.Second).UnixMilli())
		rec := make([]byte, 0, 40)
		tb := make([]byte, 8)
		binary.BigEndian.PutUint64(tb, ts)
		rec = append(rec, tb...)
		rec = append(rec, 0x00)
		lon := int32(513890000 + seed + i)
		lat := int32(356890000 + seed + i)
		lb := make([]byte, 4)
		ab := make([]byte, 4)
		binary.BigEndian.PutUint32(lb, uint32(lon))
		binary.BigEndian.PutUint32(ab, uint32(lat))
		rec = append(rec, lb...)
		rec = append(rec, ab...)
		alt := make([]byte, 2)
		binary.BigEndian.PutUint16(alt, 1200)
		rec = append(rec, alt...)
		ang := make([]byte, 2)
		binary.BigEndian.PutUint16(ang, 0)
		rec = append(rec, ang...)
		rec = append(rec, 12)
		spd := make([]byte, 2)
		binary.BigEndian.PutUint16(spd, 4)
		rec = append(rec, spd...)
		rec = append(rec, 0x00, 0x01, 0x01, 239, 0x01, 0x00, 0x00, 0x00)
		body = append(body, rec...)
	}
	dataField := append([]byte{0x08, byte(n)}, body...)
	dataField = append(dataField, byte(n))
	crc := codec.CRC16IBM(dataField)
	frame := make([]byte, 0, 8+len(dataField)+4)
	frame = append(frame, 0, 0, 0, 0)
	lb := make([]byte, 4)
	binary.BigEndian.PutUint32(lb, uint32(len(dataField)))
	frame = append(frame, lb...)
	frame = append(frame, dataField...)
	cb := make([]byte, 4)
	binary.BigEndian.PutUint32(cb, uint32(crc))
	frame = append(frame, cb...)
	return frame
}

func printLatencyReport(title string, elapsed time.Duration, ok, fail int64, lats []time.Duration) {
	fmt.Println()
	fmt.Printf("=== %s ===\n", title)
	fmt.Printf("elapsed=%s ok=%d fail=%d\n", elapsed, ok, fail)
	if len(lats) == 0 {
		fmt.Println("no latency samples")
		return
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	pct := func(p float64) time.Duration {
		idx := int(float64(len(lats)-1) * p)
		return lats[idx]
	}
	var sum time.Duration
	for _, d := range lats {
		sum += d
	}
	avg := sum / time.Duration(len(lats))
	fmt.Printf("p50=%s  p95=%s  p99=%s  avg=%s  min=%s  max=%s\n",
		pct(0.50), pct(0.95), pct(0.99), avg, lats[0], lats[len(lats)-1])
	fmt.Println()
	fmt.Println("Targets (guidance): p99 ACK << device timeout; with AOF everysec expect ms–tens of ms on LAN.")
	fmt.Println("If p99 regresses: profile Redis fsync / pool / network before changing Zero-Drop order.")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := s[start:i]
			for len(part) > 0 && (part[0] == ' ' || part[0] == '\t') {
				part = part[1:]
			}
			for len(part) > 0 && (part[len(part)-1] == ' ' || part[len(part)-1] == '\t') {
				part = part[:len(part)-1]
			}
			if part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}
