package mdns

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DialContext dials like a normal dialer, except that a .local name is
// resolved by multicast first.
//
// This is deliberately the dial path rather than the probe: adding a PC
// is one request, and browsing it is thousands. Fixing only the probe
// would let a peer be added and then fail on every file listing, which
// is a worse outcome than refusing it up front.
//
// The system resolver is still tried if multicast finds nothing, so a
// machine that does have working .local resolution (macOS, or Linux with
// Avahi wired into NSS) keeps whatever it already had. Multicast goes
// first because it is the mechanism the name is defined by, and because
// the system path is the one already known to fail slowly with SERVFAIL.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}

	host, port, err := net.SplitHostPort(address)
	if err != nil || !IsLocal(host) {
		return dialer.DialContext(ctx, network, address)
	}

	addrs, err := Lookup(ctx, host)
	if err != nil {
		// Fall back rather than failing: this is the path a correctly
		// configured macOS or Avahi box was already using.
		conn, sysErr := dialer.DialContext(ctx, network, address)
		if sysErr == nil {
			return conn, nil
		}
		return nil, fmt.Errorf("%s could not be found on the local network: %w", host, err)
	}

	// Try every address the peer answered with. A multi-homed machine
	// advertises all of them and only some may be reachable from here,
	// so the first answer is not necessarily the working one.
	var lastErr error
	for _, addr := range addrs {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	// Every advertised address failed, so the cached answer is suspect:
	// drop it rather than serve it again until the TTL expires.
	Forget(host)
	return nil, fmt.Errorf("%s answered on the local network but could not be reached: %w", host, lastErr)
}
