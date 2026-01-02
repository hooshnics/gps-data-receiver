package tests

import (
	"sync/atomic"
	"testing"

	"github.com/gps-data-receiver/internal/sender"
)

// BenchmarkLoadBalancer tests the performance of the load balancer
func BenchmarkLoadBalancer(b *testing.B) {
	servers := []string{
		"http://server1.example.com",
		"http://server2.example.com",
		"http://server3.example.com",
	}

	lb := sender.NewLoadBalancer(servers)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = lb.NextServer()
		}
	})
}

// BenchmarkLoadBalancerConcurrent tests concurrent access
func BenchmarkLoadBalancerConcurrent(b *testing.B) {
	servers := []string{
		"http://server1.example.com",
		"http://server2.example.com",
		"http://server3.example.com",
		"http://server4.example.com",
		"http://server5.example.com",
	}

	lb := sender.NewLoadBalancer(servers)
	var counter uint64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = lb.NextServer()
			atomic.AddUint64(&counter, 1)
		}
	})

	b.ReportMetric(float64(counter)/b.Elapsed().Seconds(), "ops/sec")
}

// BenchmarkAtomicCounter benchmarks atomic operations
func BenchmarkAtomicCounter(b *testing.B) {
	var counter uint64

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			atomic.AddUint64(&counter, 1)
		}
	})
}

