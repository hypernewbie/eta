package mdns

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
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

	addrs, err := Lookup(context.Background(), "jupiter.local")
	if err != nil {
		t.Skipf("no multicast on this host: %v", err)
	}
	found := false
	for _, addr := range addrs {
		if addr.String() == "192.168.1.42" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the responder's address, got %v", addrs)
	}
}

// The answer is cached: browsing a peer is thousands of requests, and
// re-querying the link for each one would be slow and antisocial.
func TestLookupCachesTheAnswer(t *testing.T) {
	var queries int
	stop := startCountingResponder(t, "cached.local.", "10.0.0.9", &queries)
	defer stop()

	if _, err := Lookup(context.Background(), "cached.local"); err != nil {
		t.Skipf("no multicast on this host: %v", err)
	}
	after := queries
	for i := 0; i < 5; i++ {
		if _, err := Lookup(context.Background(), "cached.local"); err != nil {
			t.Fatal(err)
		}
	}
	if queries != after {
		t.Errorf("expected cached lookups to send no further queries, sent %d", queries-after)
	}

	// And Forget must actually re-query, or a peer that moves stays
	// unreachable until the TTL runs out.
	Forget("cached.local")
	if _, err := Lookup(context.Background(), "cached.local"); err != nil {
		t.Fatal(err)
	}
	if queries <= after {
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
	var ignored int
	return startCountingResponder(t, fqdn, ip, &ignored)
}

func startCountingResponder(t *testing.T, fqdn, ip string, queries *int) func() {
	t.Helper()
	group := &net.UDPAddr{IP: net.ParseIP(ipv4Group), Port: port}
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
			*queries++
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

var _ = netip.Addr{}
