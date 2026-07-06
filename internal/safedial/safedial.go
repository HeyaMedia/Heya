// Package safedial provides an SSRF guard for server-side fetches of
// semi-trusted URLs — e.g. artwork/CDN image URLs stored in DB metadata (which
// can be NFO-sourced) and reached from anonymous endpoints. The guard runs as a
// net.Dialer.Control hook so it vets the resolved IP post-DNS (DNS-rebinding
// safe) on every connection attempt, including redirect hops.
package safedial

import (
	"fmt"
	"net"
	"syscall"
)

// Control is a net.Dialer.Control hook that refuses connections to addresses an
// anonymous caller must never be able to make the server fetch from: loopback,
// RFC1918 private, link-local (uni/multicast), unspecified, and CGNAT
// (100.64.0.0/10, which tailnets squat on). Transports using it MUST disable
// Proxy — an HTTP(S)_PROXY would tunnel the request through the proxy, so the
// guard would vet only the proxy's address while the proxy reaches the private
// target, defeating the protection.
func Control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("dial to unresolved host %q refused", host)
	}
	inCGNAT := false
	if v4 := ip.To4(); v4 != nil {
		inCGNAT = v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || inCGNAT {
		return fmt.Errorf("dial to non-public address %s refused", ip)
	}
	return nil
}
