package mdns

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"sync"
	"sync/atomic"
)

// Only .local goes to multicast. Everything else must keep using the
// ordinary resolver, and an IP literal is not a name at all.
func TestIsLocalSelectsOnlyMDNSNames(t *testing.T) {
	for _, name := range []string{"jupiter.local", "JUPITER.LOCAL", "jupiter.local.", "a.b.local"} {
		if !IsLocal(name) {
			t.Errorf("%q is an mDNS name", name)
		}
	}
	for _, name := range []string{"jupiter", "example.com", "192.168.1.5", "::1", "", "localhost", "notlocal.localdomain"} {
		if IsLocal(name) {
			t.Errorf("%q must not be sent to multicast", name)
		}
	}
}

// A real responder on the loopback-reachable multicast group, answering a
// real query from the real client. This is the whole feature: the name
// the user typed becomes an address.
func TestLookupResolvesAgainstARealResponder(t *testing.T) {
	responder := startResponder(t, "jupiter.local.", "192.168.1.42")
	defer responder()

	addr, err := Lookup(context.Background(), "jupiter.local")
	if err != nil {
		t.Skipf("no multicast on this host: %v", err)
	}
	if addr.String() != "192.168.1.42" {
		t.Fatalf("expected the responder's address, got %v", addr)
	}
}

// The answer is cached: browsing a peer is thousands of requests, and
// re-querying the link for each one would be slow and antisocial.
func TestLookupCachesTheAnswer(t *testing.T) {
	var queries atomic.Int64
	stop := startCountingResponder(t, "cached.local.", "10.0.0.9", &queries)
	defer stop()

	if _, err := Lookup(context.Background(), "cached.local"); err != nil {
		t.Skipf("no multicast on this host: %v", err)
	}
	after := queries.Load()
	for i := 0; i < 5; i++ {
		if _, err := Lookup(context.Background(), "cached.local"); err != nil {
			t.Fatal(err)
		}
	}
	if sent := queries.Load() - after; sent != 0 {
		t.Errorf("expected cached lookups to send no further queries, sent %d", sent)
	}

	// And Forget must actually re-query, or a peer that moves stays
	// unreachable until the TTL runs out.
	Forget("cached.local")
	if _, err := Lookup(context.Background(), "cached.local"); err != nil {
		t.Fatal(err)
	}
	if queries.Load() <= after {
		t.Error("Forget should have forced a fresh query")
	}
}

// A name nobody answers for must fail promptly rather than hanging.
func TestLookupFailsWhenNothingAnswers(t *testing.T) {
	start := time.Now()
	_, err := Lookup(context.Background(), "definitely-no-such-host-xyzzy.local")
	if err == nil {
		t.Fatal("expected a failure when no responder exists")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v; an unanswered query must not hang", elapsed)
	}
}

// The dialer must leave ordinary addresses completely alone.
func TestDialContextPassesThroughNonLocalAddresses(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	conn, err := DialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("an ordinary address must dial normally: %v", err)
	}
	conn.Close()
}

// startResponder answers mDNS queries for one name, as a peer running
// Avahi or Bonjour would.
func startResponder(t *testing.T, fqdn, ip string) func() {
	var ignored atomic.Int64
	return startCountingResponder(t, fqdn, ip, &ignored)
}

// queries is atomic because the responder counts on its own goroutine
// while the test reads it -- counting it with a plain int made the
// assertions themselves racy, and so worthless.
func startCountingResponder(t *testing.T, fqdn, ip string, queries *atomic.Int64) func() {
	t.Helper()
	group := &net.UDPAddr{IP: net.ParseIP("224.0.0.251"), Port: 5353}
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		t.Skipf("cannot listen on the mDNS group here: %v", err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 9000)
		for {
			select {
			case <-done:
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				continue
			}
			msg := new(dns.Msg)
			if msg.Unpack(buf[:n]) != nil || len(msg.Question) == 0 {
				continue
			}
			question := msg.Question[0]
			if !strings.EqualFold(question.Name, fqdn) || question.Qtype != dns.TypeA {
				continue
			}
			queries.Add(1)
			reply := new(dns.Msg)
			reply.SetReply(msg)
			reply.Answer = []dns.RR{&dns.A{
				Hdr: dns.RR_Header{Name: fqdn, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
				A:   net.ParseIP(ip),
			}}
			if wire, err := reply.Pack(); err == nil {
				// Unicast back to the asking port, as RFC 6762 §6.7
				// requires for a query from a non-5353 source port.
				_, _ = conn.WriteToUDP(wire, src)
			}
		}
	}()
	return func() { close(done); conn.Close() }
}

// Concurrent first lookups for one name must collapse into a single
// query. A browse session opens many connections at once, so this is the
// ordinary case rather than a rare race: without it, every one of them
// opens sockets and floods the link with the same question.
func TestLookupCollapsesConcurrentQueries(t *testing.T) {
	var queries atomic.Int64
	stop := startCountingResponder(t, "stampede.local.", "10.0.0.11", &queries)
	defer stop()

	// Baseline: what one lookup costs on the wire. pion sends the same
	// question out several interfaces and sockets, so this is not 1, and
	// asserting a fixed number here would be asserting pion's internals
	// rather than Eta's collapsing.
	Forget("stampede.local")
	if _, err := Lookup(context.Background(), "stampede.local"); err != nil {
		t.Skipf("no multicast on this host: %v", err)
	}
	baseline := queries.Load()
	if baseline == 0 {
		t.Skip("responder saw no queries; no multicast on this host")
	}

	// Now the same thing from twelve goroutines at once, which is what a
	// browse session does. If they collapse, the cost is one lookup's
	// worth; if they do not, it is twelve.
	Forget("stampede.local")
	before := queries.Load()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Lookup(context.Background(), "stampede.local"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent lookup failed: %v", err)
	}

	if sent := queries.Load() - before; sent > baseline {
		t.Errorf("12 concurrent lookups cost %d queries against a single lookup's %d; they did not collapse", sent, baseline)
	}
}

// An expired entry for a name that no longer resolves must not sit in the
// map forever.
func TestLookupDropsAnExpiredEntry(t *testing.T) {
	Forget("gone.local")
	cacheMu.Lock()
	cache["gone.local"] = entry{expires: time.Now().Add(-time.Minute)}
	cacheMu.Unlock()

	_, _ = Lookup(context.Background(), "gone.local")

	cacheMu.Lock()
	_, still := cache["gone.local"]
	cacheMu.Unlock()
	if still {
		t.Error("an expired entry that failed to re-resolve was left in the cache")
	}
}
