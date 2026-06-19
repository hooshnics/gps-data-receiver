package sender

import (
	"sync"
	"sync/atomic"
)

// LoadBalancer implements round-robin load balancing
type LoadBalancer struct {
	servers []string
	counter uint64
	mu      sync.RWMutex
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer(servers []string) *LoadBalancer {
	return &LoadBalancer{
		servers: servers,
		counter: 0,
	}
}

// NextServer returns the next server in round-robin fashion
func (lb *LoadBalancer) NextServer() string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if len(lb.servers) == 0 {
		return ""
	}

	// Atomic increment and get index
	index := atomic.AddUint64(&lb.counter, 1) - 1
	return lb.servers[index%uint64(len(lb.servers))]
}

// GetServers returns all servers
func (lb *LoadBalancer) GetServers() []string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	
	result := make([]string, len(lb.servers))
	copy(result, lb.servers)
	return result
}

// ServerCount returns the number of servers
func (lb *LoadBalancer) ServerCount() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return len(lb.servers)
}

