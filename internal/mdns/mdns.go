// Package mdns resolves .local hostnames over multicast DNS.
//
// This exists because Go programs cannot rely on the operating system to
// do it. RFC 6762 §3 reserves .local for multicast DNS, so ordinary
// unicast DNS servers are expected to refuse those names -- which is
// exactly what a user hitting this sees: systemd-resolved answering
// SERVFAIL, surfaced by Go as "server misbehaving".
//
// On Linux the lookup normally succeeds inside NSS, via Avahi's
// mdns4_minimal module. Go's pure-Go resolver does not consult NSS
// modules at all: it reads /etc/hosts and queries the nameservers in
// resolv.conf (see the net package's "Name Resolution" documentation).
// So `ping jupiter.local` succeeds while the identical lookup inside a
// Go program fails on a correctly working machine, and a box with no
// Avahi NSS module (hosts: files dns) cannot resolve it at all.
//
// Eta is a LAN filesystem browser and .local is how LAN machines are
// named, so it queries multicast itself rather than depending on every
// user's resolver being configured for it.
//
// The multicast protocol is github.com/pion/mdns/v2, the resolver
// pion/webrtc uses for ICE .local candidates -- not hand-rolled here.
// What this package adds is only what is specific to Eta: deciding which
// names are multicast names, caching answers across a browse session,
// and a dialer that applies it to peer traffic.
package mdns

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	pion "github.com/pion/mdns/v2"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sync/singleflight"
)

const (
	// A one-shot query is answered promptly on a healthy link; this
	// bounds the wait when nothing is there to answer at all.
	queryTimeout = 2 * time.Second

	// How long an answer is trusted. pion reports the record header, but
	// a fixed modest lifetime is simpler and safer than honouring a TTL
	// a peer chose: long enough that browsing does not re-query the link
	// constantly, short enough that a PC which moves is picked up again.
	cacheFor = 60 * time.Second
)

// IsLocal reports whether a hostname is in the mDNS .local domain, and so
// must be resolved by multicast rather than unicast DNS.
func IsLocal(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false // an address literal is not a name
	}
	return strings.HasSuffix(host, ".local")
}

type entry struct {
	addr    netip.Addr
	expires time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]entry{}

	// Concurrent first requests for one name would otherwise each open
	// sockets and query the whole link -- the stampede the cache exists
	// to prevent, in the window before the first answer lands. A browse
	// session opens many connections at once, so this is the common case
	// rather than a rare one.
	inflight singleflight.Group
)

// Lookup resolves a .local name to an address, preferring a cached
// answer. Browsing one peer is thousands of requests, and re-querying
// the entire link for each of them would be both slow and antisocial.
func Lookup(ctx context.Context, host string) (netip.Addr, error) {
	key := strings.ToLower(strings.TrimSuffix(host, "."))

	cacheMu.Lock()
	hit, ok := cache[key]
	if ok && time.Now().Before(hit.expires) {
		cacheMu.Unlock()
		return hit.addr, nil
	}
	if ok {
		// Expired, and about to be re-queried: drop it now so a name that
		// stops resolving does not sit in the map forever.
		delete(cache, key)
	}
	cacheMu.Unlock()

	resolved, err, _ := inflight.Do(key, func() (any, error) {
		addr, err := query(ctx, key)
		if err != nil {
			return nil, err
		}
		cacheMu.Lock()
		cache[key] = entry{addr: addr, expires: time.Now().Add(cacheFor)}
		cacheMu.Unlock()
		return addr, nil
	})
	if err != nil {
		return netip.Addr{}, err
	}
	return resolved.(netip.Addr), nil
}

// Forget drops any cached answer for a name, so a PC that changed
// address is re-resolved rather than failing until the entry expires.
func Forget(host string) {
	cacheMu.Lock()
	delete(cache, strings.ToLower(strings.TrimSuffix(host, ".")))
	cacheMu.Unlock()
}

// query asks the link, using pion/mdns for the protocol itself.
//
// The sockets are opened per lookup and closed again rather than held
// open for the process's lifetime. Caching makes lookups rare, and Eta
// has no reason to sit on the mDNS port or to receive the link's
// multicast traffic while it is not asking anything.
//
// No LocalNames are configured, which is pion's condition for answering
// questions: Eta asks, and never advertises itself or responds on behalf
// of any name.
func query(ctx context.Context, name string) (netip.Addr, error) {
	// net.ListenUDP on a multicast address does not bind the group: the
	// stdlib rewrites it to the wildcard and sets SO_REUSEADDR (and
	// SO_REUSEPORT on Darwin/BSD), which is why this coexists with an
	// avahi-daemon or mDNSResponder already holding 5353. No reuse
	// options are needed here.
	var bindErr error
	var packet4 *ipv4.PacketConn
	if addr, err := net.ResolveUDPAddr("udp4", pion.DefaultAddressIPv4); err == nil {
		if conn, err := net.ListenUDP("udp4", addr); err == nil {
			packet4 = ipv4.NewPacketConn(conn)
		} else {
			bindErr = err
		}
	}
	var packet6 *ipv6.PacketConn
	if addr, err := net.ResolveUDPAddr("udp6", pion.DefaultAddressIPv6); err == nil {
		if conn, err := net.ListenUDP("udp6", addr); err == nil {
			packet6 = ipv6.NewPacketConn(conn)
		} else if bindErr == nil {
			bindErr = err
		}
	}
	if packet4 == nil && packet6 == nil {
		// Kept rather than swallowed: on a platform where a resident
		// responder holds the port exclusively, this is the only thing
		// that says why, and the caller falls back to the system
		// resolver on the strength of it.
		return netip.Addr{}, fmt.Errorf("no multicast socket available for %q: %w", name, bindErr)
	}

	// IncludeLoopback so that an Eta on this same machine, reached by its
	// own .local name, resolves like any other.
	server, err := pion.Server(packet4, packet6, &pion.Config{IncludeLoopback: true})
	if err != nil {
		return netip.Addr{}, fmt.Errorf("mDNS query for %q: %w", name, err)
	}
	defer server.Close() //nolint:errcheck

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	_, addr, err := server.QueryAddr(queryCtx, strings.TrimSuffix(name, "."))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("no mDNS response for %q: %w", name, err)
	}
	return addr, nil
}
