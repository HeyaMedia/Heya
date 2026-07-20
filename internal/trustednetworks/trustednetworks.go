// Package trustednetworks validates and matches the direct-peer CIDRs that
// Heya administrators explicitly trust at the public boundary.
package trustednetworks

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

const (
	EnvVar       = "HEYA_TRUSTED_NETWORKS"
	SettingKey   = "security.trusted_networks"
	DefaultValue = "100.64.0.0/10,192.168.0.0/16"
	MaxEntries   = 64
)

// Parse accepts comma- or whitespace-separated CIDRs. Individual addresses
// are convenient shorthand for /32 (IPv4) or /128 (IPv6). The returned list
// is canonical, de-duplicated, and stable for persistence and Caddy reloads.
func Parse(value string) ([]netip.Prefix, error) {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	if len(parts) > MaxEntries {
		return nil, fmt.Errorf("trusted network list has %d entries; maximum is %d", len(parts), MaxEntries)
	}

	unique := make(map[netip.Prefix]struct{}, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(part)
		if err != nil {
			addr, addrErr := netip.ParseAddr(part)
			if addrErr != nil {
				return nil, fmt.Errorf("invalid trusted network %q: use an IP address or CIDR", part)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		prefix = prefix.Masked()
		unique[prefix] = struct{}{}
	}

	prefixes := make([]netip.Prefix, 0, len(unique))
	for prefix := range unique {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool {
		a, b := prefixes[i].Addr(), prefixes[j].Addr()
		if a.BitLen() != b.BitLen() {
			return a.BitLen() < b.BitLen()
		}
		if a != b {
			return a.Less(b)
		}
		return prefixes[i].Bits() < prefixes[j].Bits()
	})
	return prefixes, nil
}

func Strings(prefixes []netip.Prefix) []string {
	out := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		out[i] = prefix.String()
	}
	return out
}

func Canonical(value string) (string, []string, error) {
	prefixes, err := Parse(value)
	if err != nil {
		return "", nil, err
	}
	values := Strings(prefixes)
	return strings.Join(values, ","), values, nil
}

func CanonicalList(values []string) (string, []string, error) {
	return Canonical(strings.Join(values, ","))
}

func Contains(prefixes []netip.Prefix, value string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
