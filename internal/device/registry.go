package device

import (
	"fmt"
	"sync"
	"time"
)

// Registry looks up drivers by name and runs Detect in registration order.
type Registry struct {
	mu      sync.RWMutex
	drivers []Driver
	byName  map[string]Driver
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Driver)}
}

// Register adds a driver. Later Detect walks in registration order.
func (r *Registry) Register(d Driver) {
	if d == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drivers = append(r.drivers, d)
	r.byName[d.Name()] = d
}

// Get returns a driver by name.
func (r *Registry) Get(name string) (Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.byName[name]
	return d, ok
}

// Detect returns the first matching driver, or an error if none match.
func (r *Registry) Detect(raw []byte, hints Hints) (Driver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, d := range r.drivers {
		if d.Detect(raw, hints) {
			return d, nil
		}
	}
	return nil, fmt.Errorf("no device driver matched payload (%d bytes)", len(raw))
}

// DefaultRegistry registers built-in Teltonika + Hooshnics drivers.
func DefaultRegistry(tzOffset time.Duration) *Registry {
	r := NewRegistry()
	r.Register(TeltonikaDriver{TZOffset: tzOffset})
	r.Register(HooshnicsDriver{})
	return r
}
