//go:build !windows

package tcp

import "net"

// listenTCP uses the standard backlog (kernel SOMAXCONN).
// For production fleets, raise host sysctls:
//   net.core.somaxconn
//   net.ipv4.tcp_max_syn_backlog
// See docs/production-readiness.md.
func listenTCP(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}
