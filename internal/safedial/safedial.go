// Package safedial provides an SSRF guard for server-side fetches of
// semi-trusted URLs — e.g. artwork/CDN image URLs stored in DB metadata (which
// can be NFO-sourced) and reached from anonymous endpoints. The guard runs as a
// net.Dialer.Control hook so it vets the resolved IP post-DNS (DNS-rebinding
// safe) on every connection attempt, including redirect hops.
package safedial

import (
	"fmt"
	"net"
	"net/netip"
	"syscall"
)

var nonPublicPrefixes = []netip.Prefix{
	// IPv4 special-use space that must never be an SSRF destination.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // shared/CGNAT (incl. tailnets)
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), // documentation
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"), // multicast
	netip.MustParsePrefix("240.0.0.0/4"), // reserved + limited broadcast

	// IPv6 local, transition, documentation, benchmarking, and multicast
	// ranges. IPv4-mapped addresses are unmapped and checked above.
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
	netip.MustParsePrefix("100::/64"),       // discard-only
	netip.MustParsePrefix("2001::/32"),      // Teredo
	netip.MustParsePrefix("2001:2::/48"),    // benchmarking
	netip.MustParsePrefix("2001:10::/28"),   // ORCHID
	netip.MustParsePrefix("2001:20::/28"),   // ORCHIDv2
	netip.MustParsePrefix("2001:db8::/32"),  // documentation
	netip.MustParsePrefix("2002::/16"),      // 6to4
	netip.MustParsePrefix("3fff::/20"),      // documentation
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fec0::/10"), // deprecated site-local
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

var wellKnownNAT64 = netip.MustParsePrefix("64:ff9b::/96")

// Control is a net.Dialer.Control hook that refuses connections to addresses a
// semi-trusted URL must never make the server fetch: loopback, private,
// link-local, shared/CGNAT, multicast, documentation, benchmarking, and other
// special-use ranges. Transports using it MUST disable Proxy — an
// HTTP(S)_PROXY would tunnel the request through the proxy, so the guard would
// vet only the proxy's address while the proxy reaches the private target,
// defeating the protection.
func Control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("dial to unresolved host %q refused", host)
	}
	if !isPublicAddress(addr) {
		return fmt.Errorf("dial to non-public address %s refused", addr)
	}
	return nil
}

func isPublicAddress(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	if addr.Zone() != "" {
		addr = addr.WithZone("")
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() {
		return false
	}

	// The well-known NAT64 prefix embeds an IPv4 destination in the final
	// four bytes. Apply the IPv4 rules to prevent a synthesized NAT64 address
	// from smuggling a private/loopback target past the IPv6 check.
	if wellKnownNAT64.Contains(addr) {
		bytes := addr.As16()
		return isPublicAddress(netip.AddrFrom4([4]byte{bytes[12], bytes[13], bytes[14], bytes[15]}))
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}
