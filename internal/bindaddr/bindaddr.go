// Package bindaddr provides Phi-compatible safe LAN binding discovery.
package bindaddr

import (
	"net"
	"sort"
)

type Kind int

const (
	Loopback Kind = iota
	LAN
	Tailnet
)

type Addr struct {
	IP   net.IP
	Kind Kind
}

func (k Kind) String() string {
	switch k {
	case Loopback:
		return "local"
	case LAN:
		return "LAN"
	case Tailnet:
		return "Tailnet"
	default:
		return "other"
	}
}

var (
	lanCIDRs       = parseCIDRs("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")
	tailnetCIDR    = parseCIDR("100.64.0.0/10")
	interfaceAddrs = net.InterfaceAddrs
)

// Detect returns loopback plus every RFC 1918 and Tailscale IPv4 address.
// It deliberately matches Phi's default "lan" binding policy.
func Detect() []Addr {
	out := []Addr{{IP: net.IPv4(127, 0, 0, 1), Kind: Loopback}}
	addrs, err := interfaceAddrs()
	if err != nil {
		return out
	}

	seen := map[string]bool{}
	var lan, tailnet []Addr
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsLoopback() || seen[ip.String()] {
			continue
		}
		seen[ip.String()] = true
		switch {
		case tailnetCIDR.Contains(ip):
			tailnet = append(tailnet, Addr{IP: ip, Kind: Tailnet})
		case contains(lanCIDRs, ip):
			lan = append(lan, Addr{IP: ip, Kind: LAN})
		}
	}
	sortAddrs(lan)
	sortAddrs(tailnet)
	return append(append(out, lan...), tailnet...)
}

func contains(cidrs []*net.IPNet, ip net.IP) bool {
	for _, cidr := range cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func sortAddrs(addrs []Addr) {
	sort.Slice(addrs, func(i, j int) bool {
		return string(addrs[i].IP.To4()) < string(addrs[j].IP.To4())
	})
}

func parseCIDR(value string) *net.IPNet {
	_, cidr, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return cidr
}

func parseCIDRs(values ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		out = append(out, parseCIDR(value))
	}
	return out
}
