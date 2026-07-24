package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	var (
		host          = flag.String("host", "127.0.0.1", "TCP server host")
		port          = flag.Int("port", 5055, "TCP server port")
		devices       = flag.Int("devices", 1000, "number of virtual Teltonika devices")
		interval      = flag.Duration("interval", 5*time.Second, "packet interval per device")
		duration      = flag.Duration("duration", 2*time.Minute, "test duration")
		records       = flag.Int("records", 1, "AVL records per packet")
		codecName     = flag.String("codec", "8", "AVL codec: 8 | 8e (Codec8 Extended)")
		ackTimeout    = flag.Duration("ack-timeout", 30*time.Second, "ACK wait timeout")
		offlineEvery  = flag.Int("offline-every", 0, "simulate disconnect after N ACKs (0=off)")
		retransmit    = flag.Bool("retransmit", false, "on reconnect, retransmit last buffered packet twice")
		imeiOffset    = flag.Int("imei-offset", 0, "starting offset for synthetic IMEIs")
		lat           = flag.Float64("lat", 35.689000, "base latitude")
		lon           = flag.Float64("lon", 51.389000, "base longitude")
		ramp          = flag.Duration("ramp", 10*time.Second, "stagger device start over this window")
	)
	flag.Parse()

	fmt.Println("=== Teltonika Load Simulator ===")
	fmt.Printf("target=%s:%d devices=%d interval=%s duration=%s records/pkt=%d codec=%s\n",
		*host, *port, *devices, *interval, *duration, *records, *codecName)
	fmt.Printf("expected steady PPS ≈ %.1f\n", float64(*devices)/interval.Seconds())
	fmt.Printf("offline-every=%d retransmit=%v ramp=%s\n", *offlineEvery, *retransmit, *ramp)

	stop := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nstopping...")
		close(stop)
	}()

	counters := &runCounters{}
	ackLat := &latencyStats{}
	var wg sync.WaitGroup
	startAll := time.Now()

	for i := 0; i < *devices; i++ {
		wg.Add(1)
		idx := *imeiOffset + i
		cfg := deviceConfig{
			host:           *host,
			port:           *port,
			imei:           syntheticIMEI(idx),
			index:          idx,
			interval:       *interval,
			recordsPerPkt:  *records,
			codecName:      *codecName,
			ackTimeout:     *ackTimeout,
			duration:       *duration,
			offlineEvery:   *offlineEvery,
			retransmitOnce: *retransmit,
			lat:            *lat,
			lon:            *lon,
		}
		delay := time.Duration(0)
		if *devices > 1 && *ramp > 0 {
			delay = time.Duration(int64(*ramp) * int64(i) / int64(*devices))
		}
		go func(d time.Duration, c deviceConfig) {
			defer wg.Done()
			if d > 0 {
				select {
				case <-stop:
					return
				case <-time.After(d):
				}
			}
			_ = runDevice(c, counters, ackLat, stop)
		}(delay, cfg)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	for {
		select {
		case <-done:
			printReport(startAll, counters, ackLat)
			return
		case <-ticker.C:
			elapsed := time.Since(startAll).Seconds()
			acked := counters.acked.Load()
			pps := float64(0)
			if elapsed > 0 {
				pps = float64(acked) / elapsed
			}
			fmt.Printf("[live] sent=%d acked=%d failed=%d timeouts=%d reconnects=%d pps=%.1f ack{%s}\n",
				counters.sent.Load(), acked, counters.failed.Load(), counters.timeouts.Load(),
				counters.reconnect.Load(), pps, ackLat.snapshot())
		}
	}
}

func printReport(start time.Time, c *runCounters, ack *latencyStats) {
	elapsed := time.Since(start)
	acked := c.acked.Load()
	pps := float64(0)
	if elapsed.Seconds() > 0 {
		pps = float64(acked) / elapsed.Seconds()
	}
	fmt.Println()
	fmt.Println("=== Load Test Result ===")
	fmt.Printf("elapsed          : %s\n", elapsed)
	fmt.Printf("packets sent     : %d\n", c.sent.Load())
	fmt.Printf("packets ACKed    : %d\n", acked)
	fmt.Printf("failed           : %d\n", c.failed.Load())
	fmt.Printf("ACK timeouts     : %d\n", c.timeouts.Load())
	fmt.Printf("reconnects       : %d\n", c.reconnect.Load())
	fmt.Printf("throughput       : %.2f ACK/s\n", pps)
	fmt.Printf("ACK latency      : %s\n", ack.snapshot())
	fmt.Println()
	fmt.Println("Also capture from server:")
	fmt.Println("  curl -s localhost:8080/metrics | grep gps_teltonika_")
	fmt.Println("  curl -s localhost:8080/metrics | grep gps_raw_")
	fmt.Println("Fill results into docs/performance/load-test-report.md")
}
