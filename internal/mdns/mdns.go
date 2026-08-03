// Package mdns resolves .local hostnames over multicast DNS.
//
// This exists because Go programs cannot rely on the operating system to
// do it. RFC 6762 §3 reserves .local for mDNS and says names in it "are
// link-local" and resolved by multicast, so ordinary unicast DNS servers
// are expected to refuse them -- which is exactly what a user hitting
// this sees: systemd-resolved answering SERVFAIL, surfaced by Go as
// "server misbehaving".
//
// On Linux the resolution normally happens in NSS, via Avahi's
// mdns4_minimal module, and Go's pure-Go resolver does not consult NSS
// modules at all -- it reads /etc/hosts and asks the nameservers in
// resolv.conf. So `ping jupiter.local` succeeds while the same lookup
// inside a Go program fails, on a machine that is working correctly. A
// box without the Avahi NSS module (hosts: files dns) cannot resolve it
// by any means.
//
// Eta is a LAN filesystem browser and .local is how LAN machines are
// named, so it does the multicast query itself rather than depending on
// each user's resolver being configured for it. The DNS wire format is
// handled by miekg/dns, the library CoreDNS is built on, rather than
// hand-rolled here.
package mdns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// The mDNS group addresses and port (RFC 6762 §3).
	ipv4Group = "224.0.0.251"
	ipv6Group = "ff02::fb"
	port      = 5353

	// A one-shot query is answered quickly on a healthy LAN; this bounds
	// the wait when nothing is there to answer at all.
	queryTimeout = 2 * time.Second

	// mDNS A records commonly carry a 120s TTL. Clamped so a peer that
	// moves is not pinned to a stale address for long, and so a
	// zero-TTL record does not defeat caching entirely.
	minTTL = 10 * time.Second
	maxTTL = 2 * time.Minute
)

// IsLocal reports whether a hostname is in the mDNS .local domain, and so
// must be resolved by multicast rather than by unicast DNS.
func IsLocal(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || net.ParseIP(host) != nil {
		return false
	}
	return strings.HasSuffix(host, ".local")
}

type entry struct {
	addrs   []netip.Addr
	expires time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]entry{}
)

// Lookup resolves a .local name to its addresses, preferring a cached
// answer. Results are cached for the record's own TTL: a browse session
// makes many requests to one peer, and re-querying the whole link for
// every one of them would be both slow and antisocial.
func Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	key := strings.ToLower(strings.TrimSuffix(host, "."))

	cacheMu.Lock()
	if hit, ok := cache[key]; ok && time.Now().Before(hit.expires) {
		addrs := append([]netip.Addr(nil), hit.addrs...)
		cacheMu.Unlock()
		return addrs, nil
	}
	cacheMu.Unlock()

	addrs, ttl, err := query(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no mDNS response for %q", host)
	}

	cacheMu.Lock()
	cache[key] = entry{addrs: addrs, expires: time.Now().Add(ttl)}
	cacheMu.Unlock()
	return addrs, nil
}

// Forget drops any cached answer for a name. Used when a cached address
// stops working, so a peer that changed address is re-resolved rather
// than failing until the TTL runs out.
func Forget(host string) {
	cacheMu.Lock()
	delete(cache, strings.ToLower(strings.TrimSuffix(host, ".")))
	cacheMu.Unlock()
}

// query sends a one-shot mDNS question and collects the answers.
//
// The query goes out of every multicast-capable interface rather than
// only the default route. That matters here specifically: a machine
// running Eta typically has several (a LAN interface, a Tailscale
// interface, one or more bridges from container runtimes), and the
// kernel's default multicast route is frequently not the LAN one.
//
// Sending from an ephemeral source port rather than 5353 makes this a
// "legacy" query in RFC 6762 §6.7 terms, which responders answer by
// unicast straight back to that port. So no group membership is needed
// to hear the reply, and Eta never joins the group or answers queries --
// it asks, it does not advertise.
func query(ctx context.Context, name string) ([]netip.Addr, time.Duration, error) {
	fqdn := dns.Fqdn(name)

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, 0, fmt.Errorf("mDNS socket: %w", err)
	}
	defer conn.Close()

	conn6, err6 := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6unspecified, Port: 0})
	if err6 == nil {
		defer conn6.Close()
	}

	deadline := time.Now().Add(queryTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	if conn6 != nil {
		_ = conn6.SetDeadline(deadline)
	}

	var questions []*dns.Msg
	for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
		msg := new(dns.Msg)
		msg.SetQuestion(fqdn, qtype)
		// mDNS queries are not recursive; there is no upstream to recurse
		// to on a link.
		msg.RecursionDesired = false
		questions = append(questions, msg)
	}

	sent := 0
	for _, msg := range questions {
		wire, err := msg.Pack()
		if err != nil {
			continue
		}
		sent += broadcast4(conn, wire)
		if conn6 != nil {
			sent += broadcast6(conn6, wire)
		}
	}
	if sent == 0 {
		return nil, 0, errors.New("no multicast-capable network interface")
	}

	type result struct {
		addrs []netip.Addr
		ttl   time.Duration
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	collect := func(c *net.UDPConn) {
		defer wg.Done()
		addrs, ttl := receive(c, fqdn)
		results <- result{addrs, ttl}
	}
	wg.Add(1)
	go collect(conn)
	if conn6 != nil {
		wg.Add(1)
		go collect(conn6)
	}
	wg.Wait()
	close(results)

	var addrs []netip.Addr
	ttl := maxTTL
	seen := map[netip.Addr]bool{}
	for r := range results {
		for _, addr := range r.addrs {
			if !seen[addr] {
				seen[addr] = true
				addrs = append(addrs, addr)
			}
		}
		if r.ttl > 0 && r.ttl < ttl {
			ttl = r.ttl
		}
	}
	return addrs, ttl, nil
}

// receive reads replies until the deadline, keeping the address records
// that answer the name asked about. It does not stop at the first
// response: a multi-homed peer answers with several addresses, and the
// reachable one is not necessarily first.
func receive(conn *net.UDPConn, fqdn string) ([]netip.Addr, time.Duration) {
	var addrs []netip.Addr
	ttl := time.Duration(0)
	buf := make([]byte, 9000)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return addrs, ttl
		}
		msg := new(dns.Msg)
		if err := msg.Unpack(buf[:n]); err != nil {
			continue
		}
		for _, rr := range append(msg.Answer, msg.Extra...) {
			if !strings.EqualFold(rr.Header().Name, fqdn) {
				continue
			}
			var addr netip.Addr
			switch record := rr.(type) {
			case *dns.A:
				addr, _ = netip.AddrFromSlice(record.A.To4())
			case *dns.AAAA:
				addr, _ = netip.AddrFromSlice(record.AAAA.To16())
			default:
				continue
			}
			if !addr.IsValid() {
				continue
			}
			addrs = append(addrs, addr)
			if recordTTL := clampTTL(rr.Header().Ttl); ttl == 0 || recordTTL < ttl {
				ttl = recordTTL
			}
		}
	}
}

func clampTTL(seconds uint32) time.Duration {
	ttl := time.Duration(seconds) * time.Second
	if ttl < minTTL {
		return minTTL
	}
	if ttl > maxTTL {
		return maxTTL
	}
	return ttl
}

// broadcast4 sends one query out of every interface that can carry
// multicast, returning how many it reached.
func broadcast4(conn *net.UDPConn, wire []byte) int {
	packet := ipv4.NewPacketConn(conn)
	target := &net.UDPAddr{IP: net.ParseIP(ipv4Group), Port: port}
	sent := 0
	for _, iface := range multicastInterfaces() {
		if err := packet.SetMulticastInterface(&iface); err != nil {
			continue
		}
		if _, err := packet.WriteTo(wire, nil, target); err == nil {
			sent++
		}
	}
	return sent
}

func broadcast6(conn *net.UDPConn, wire []byte) int {
	packet := ipv6.NewPacketConn(conn)
	target := &net.UDPAddr{IP: net.ParseIP(ipv6Group), Port: port}
	sent := 0
	for _, iface := range multicastInterfaces() {
		if err := packet.SetMulticastInterface(&iface); err != nil {
			continue
		}
		if _, err := packet.WriteTo(wire, nil, target); err == nil {
			sent++
		}
	}
	return sent
}

func multicastInterfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var usable []net.Interface
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		usable = append(usable, iface)
	}
	return usable
}
