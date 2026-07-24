package main

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type latencyStats struct {
	mu   sync.Mutex
	vals []time.Duration
}

func (s *latencyStats) add(d time.Duration) {
	s.mu.Lock()
	s.vals = append(s.vals, d)
	s.mu.Unlock()
}

func (s *latencyStats) snapshot() summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.vals) == 0 {
		return summary{}
	}
	cp := append([]time.Duration(nil), s.vals...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	pct := func(p float64) time.Duration {
		if len(cp) == 1 {
			return cp[0]
		}
		idx := int(float64(len(cp)-1) * p)
		return cp[idx]
	}
	var sum time.Duration
	for _, v := range cp {
		sum += v
	}
	return summary{
		count: len(cp),
		avg:   sum / time.Duration(len(cp)),
		p50:   pct(0.50),
		p95:   pct(0.95),
		p99:   pct(0.99),
		max:   cp[len(cp)-1],
		min:   cp[0],
	}
}

type summary struct {
	count      int
	avg, p50, p95, p99, min, max time.Duration
}

func (s summary) String() string {
	if s.count == 0 {
		return "no samples"
	}
	return fmt.Sprintf("n=%d min=%s p50=%s avg=%s p95=%s p99=%s max=%s",
		s.count, s.min, s.p50, s.avg, s.p95, s.p99, s.max)
}

type runCounters struct {
	sent      atomic.Int64
	acked     atomic.Int64
	failed    atomic.Int64
	reconnect atomic.Int64
	timeouts  atomic.Int64
}
