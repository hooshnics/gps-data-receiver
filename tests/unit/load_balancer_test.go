package unit

import (
	"sync"
	"testing"

	"github.com/gps-data-receiver/internal/sender"
	"github.com/stretchr/testify/assert"
)

func TestLoadBalancerRoundRobin(t *testing.T) {
	servers := []string{
		"http://server1.example.com",
		"http://server2.example.com",
		"http://server3.example.com",
	}

	lb := sender.NewLoadBalancer(servers)

	// Test round-robin distribution
	for i := 0; i < 9; i++ {
		server := lb.NextServer()
		expected := servers[i%3]
		assert.Equal(t, expected, server, "Round-robin should cycle through servers")
	}
}

func TestLoadBalancerConcurrentAccess(t *testing.T) {
	servers := []string{
		"http://server1.example.com",
		"http://server2.example.com",
		"http://server3.example.com",
	}

	lb := sender.NewLoadBalancer(servers)

	// Track server selection counts
	counts := make(map[string]int)
	var mu sync.Mutex

	// Concurrent access test
	var wg sync.WaitGroup
	goroutines := 100
	selectionsPerGoroutine := 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < selectionsPerGoroutine; j++ {
				server := lb.NextServer()
				mu.Lock()
				counts[server]++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Verify all servers were used
	totalSelections := goroutines * selectionsPerGoroutine
	assert.Equal(t, totalSelections, counts[servers[0]]+counts[servers[1]]+counts[servers[2]])

	// Each server should be selected roughly equally (allowing for some variance)
	expectedPerServer := totalSelections / len(servers)
	tolerance := expectedPerServer / 10 // 10% tolerance

	for _, server := range servers {
		count := counts[server]
		assert.InDelta(t, expectedPerServer, count, float64(tolerance),
			"Server %s should be selected roughly equally", server)
	}
}

func TestLoadBalancerEmptyServers(t *testing.T) {
	lb := sender.NewLoadBalancer([]string{})
	server := lb.NextServer()
	assert.Empty(t, server, "Should return empty string for no servers")
}

func TestLoadBalancerSingleServer(t *testing.T) {
	servers := []string{"http://server1.example.com"}
	lb := sender.NewLoadBalancer(servers)

	// All selections should return the same server
	for i := 0; i < 10; i++ {
		server := lb.NextServer()
		assert.Equal(t, servers[0], server)
	}
}

func TestLoadBalancerServerCount(t *testing.T) {
	servers := []string{
		"http://server1.example.com",
		"http://server2.example.com",
	}

	lb := sender.NewLoadBalancer(servers)
	assert.Equal(t, 2, lb.ServerCount())
}

func TestLoadBalancerGetServers(t *testing.T) {
	servers := []string{
		"http://server1.example.com",
		"http://server2.example.com",
	}

	lb := sender.NewLoadBalancer(servers)
	result := lb.GetServers()

	assert.Equal(t, len(servers), len(result))
	assert.ElementsMatch(t, servers, result)
}

