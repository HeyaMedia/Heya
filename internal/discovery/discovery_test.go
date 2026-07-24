package discovery

import (
	"net"
	"strings"
	"testing"
)

func txtMap(t *testing.T, entries []string) map[string]string {
	t.Helper()
	return ParseTXT(entries)
}

func TestBuildTXTCarriesTheClientContract(t *testing.T) {
	txt := BuildTXT(Config{
		Instance:  "knas",
		HTTPS:     true,
		ServerID:  "0123456789abcdef0123456789abcdef",
		Version:   "0.4.96",
		RemoteURL: "https://wan.heya.example.com:41234",
	})
	got := txtMap(t, txt)

	want := map[string]string{
		"v":      TXTVersion,
		"name":   "knas",
		"scheme": "https",
		"path":   "/",
		"api":    "/api",
		"id":     "0123456789abcdef0123456789abcdef",
		"ver":    "0.4.96",
		"remote": "https://wan.heya.example.com:41234",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("TXT %q = %q, want %q", key, got[key], value)
		}
	}
}

func TestBuildTXTOmitsUnsetOptionalKeys(t *testing.T) {
	got := txtMap(t, BuildTXT(Config{Instance: "heya"}))

	if got["scheme"] != "http" {
		t.Errorf("scheme = %q, want http when HTTPS is false", got["scheme"])
	}
	for _, key := range []string{"id", "ver", "remote"} {
		if _, present := got[key]; present {
			t.Errorf("TXT should omit %q when unset, got %q", key, got[key])
		}
	}
}

// A TXT entry that lost its `=` would silently drop a key for every client,
// so the sanitizer must never emit a control character or a stray newline.
func TestBuildTXTStripsControlCharacters(t *testing.T) {
	txt := BuildTXT(Config{Instance: "ev\nil\tserver", ServerID: "abc\x00def"})
	for _, entry := range txt {
		if strings.ContainsAny(entry, "\n\r\t\x00") {
			t.Errorf("TXT entry %q contains a control character", entry)
		}
	}
	// DNS-SD caps a single TXT string at 255 bytes; ours must stay well under.
	for _, entry := range txt {
		if len(entry) > 255 {
			t.Errorf("TXT entry is %d bytes, over the 255-byte limit: %q", len(entry), entry)
		}
	}
}

func TestBuildTXTCapsOverlongValues(t *testing.T) {
	txt := BuildTXT(Config{Instance: strings.Repeat("x", 400), RemoteURL: "https://" + strings.Repeat("y", 400)})
	for _, entry := range txt {
		if len(entry) > 255 {
			t.Errorf("TXT entry is %d bytes, over the 255-byte limit", len(entry))
		}
	}
}

func TestSanitizeInstance(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"trims and collapses whitespace", "  living   room  ", "living room"},
		{"drops control characters", "he\x07ya", "heya"},
		{"dots become spaces so clients cannot read them as labels", "knas.local", "knas local"},
		{"empty stays empty for the caller's fallback", "   ", ""},
		{"caps at the 63-byte DNS label limit", strings.Repeat("a", 100), strings.Repeat("a", 63)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeInstance(tc.in); got != tc.want {
				t.Errorf("sanitizeInstance(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFoundURLPrefersLiteralAddress(t *testing.T) {
	// Android has no mDNS resolver for `.local.` names, so a client handed a
	// hostname when an A record was available would simply fail to connect.
	found := Found{
		Hostname: "knas.local",
		Port:     8080,
		IPv4:     []string{"192.168.1.10"},
		TXT:      map[string]string{"scheme": "https"},
	}
	if got, want := found.URL(), "https://192.168.1.10:8080"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

func TestFoundURLFallsBackToHostnameAndHTTP(t *testing.T) {
	found := Found{Hostname: "knas.local.", Port: 8096}
	if got, want := found.URL(), "http://knas.local:8096"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

func TestFoundURLBracketsIPv6(t *testing.T) {
	found := Found{Hostname: "knas.local", Port: 8080, IPv6: []string{"2001:db8::1"}}
	if got, want := found.URL(), "http://[2001:db8::1]:8080"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

func TestParseTXTSkipsMalformedEntries(t *testing.T) {
	got := ParseTXT([]string{"Scheme=https", "novalue", "empty=", "id=abc=def"})
	if got["scheme"] != "https" {
		t.Errorf("keys should be lowercased: %v", got)
	}
	if _, present := got["novalue"]; present {
		t.Error("entries without '=' should be skipped")
	}
	if got["empty"] != "" {
		t.Errorf("empty value = %q, want empty string", got["empty"])
	}
	if got["id"] != "abc=def" {
		t.Errorf("only the first '=' separates: got %q", got["id"])
	}
}

func TestSplitList(t *testing.T) {
	if got := SplitList(" eth0 , , br0 "); len(got) != 2 || got[0] != "eth0" || got[1] != "br0" {
		t.Errorf("SplitList = %#v, want [eth0 br0]", got)
	}
	if got := SplitList("   "); got != nil {
		t.Errorf("SplitList(blank) = %#v, want nil", got)
	}
}

// An unresolvable interface name must fail loudly rather than silently
// advertising on every NIC — the whole point of pinning one is to keep the
// announcement off the interfaces the operator excluded.
func TestResolveInterfacesRejectsUnknownName(t *testing.T) {
	_, _, pinned, err := resolveInterfaces([]string{"definitely-not-a-nic0"})
	if err == nil {
		t.Fatal("expected an error for an unmatched interface name")
	}
	if !pinned {
		t.Error("a named selection must report as pinned even when it matches nothing")
	}
}

func TestResolveInterfacesEmptyIsUnpinned(t *testing.T) {
	_, _, pinned, err := resolveInterfaces(nil)
	if err != nil {
		t.Fatalf("resolveInterfaces(nil): %v", err)
	}
	if pinned {
		t.Error("an empty selection must report as unpinned so zeroconf keeps its own default set")
	}
}

// The advertised host name must be a single label: zeroconf appends
// `.local.` to whatever it is handed, so passing an already-qualified name
// produced `mac.local.local.` — and a name it thinks is qualified but that
// lacks a trailing dot fails to pack at all, silently killing every answer.
func TestHostLabel(t *testing.T) {
	cases := map[string]string{
		"mac.local":   "mac",
		"mac.local.":  "mac",
		"box.lan":     "box",
		"knas":        "knas",
		"  knas  ":    "knas",
		".":           "",
		"":            "",
		"a.b.c.d.e.f": "a",
	}
	for in, want := range cases {
		if got := hostLabel(in); got != want {
			t.Errorf("hostLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := hostLabel(strings.Repeat("h", 80)); len(got) != 63 {
		t.Errorf("hostLabel capped at %d bytes, want 63 (the DNS label limit)", len(got))
	}
}

func TestInterfaceAddressesSkipsLoopbackAndDeduplicates(t *testing.T) {
	// Uses the real host: the only invariant that holds everywhere is that
	// nothing loopback and nothing duplicated comes back.
	ifaces, _, _, err := resolveInterfaces(nil)
	if err != nil {
		t.Fatalf("resolveInterfaces: %v", err)
	}
	addresses := interfaceAddresses(ifaces)
	seen := map[string]bool{}
	for _, text := range addresses {
		ip := net.ParseIP(text)
		if ip == nil {
			t.Errorf("advertised address %q does not parse", text)
			continue
		}
		if ip.IsLoopback() {
			t.Errorf("advertised address %q is loopback; clients would dial themselves", text)
		}
		if seen[text] {
			t.Errorf("advertised address %q is duplicated", text)
		}
		seen[text] = true
	}
}

func TestAddressRankPutsTheDefaultRouteFirstAndCGNATLast(t *testing.T) {
	primary := net.ParseIP("192.168.1.100")
	ranks := map[string]int{
		"192.168.1.100": 0, // the default-route address
		"10.8.0.4":      1, // another RFC1918 address
		"203.0.113.5":   2, // public
		"100.76.110.94": 3, // tailnet CGNAT — unroutable from a plain LAN client
	}
	for text, want := range ranks {
		if got := addressRank(net.ParseIP(text), primary); got != want {
			t.Errorf("addressRank(%s) = %d, want %d", text, got, want)
		}
	}
}

func TestInterfaceAddressesLeadsWithTheDefaultRoute(t *testing.T) {
	primary := defaultRouteIP()
	if primary == nil {
		t.Skip("no default route on this host")
	}
	ifaces, _, _, err := resolveInterfaces(nil)
	if err != nil {
		t.Fatalf("resolveInterfaces: %v", err)
	}
	addresses := interfaceAddresses(ifaces)
	if len(addresses) == 0 {
		t.Skip("no advertisable address on this host")
	}
	if addresses[0] != primary.String() {
		t.Errorf("first advertised address = %s, want the default-route address %s (clients try them in order)",
			addresses[0], primary)
	}
}

// Publishing A records for the machine's OWN `<hostname>.local` makes the
// system responder (mDNSResponder / avahi) treat us as a conflicting claim
// and defend its name by RENAMING the machine — kWorkBookPro → kWorkBookPro-2
// → -3, once per start. Starting a media server must not rename the user's
// computer, so the advertised name has to be one nothing else owns.
func TestAdvertisedHostLabelNeverClaimsTheMachinesOwnName(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		osHost     string
		want       string
	}{
		{"prefixes the OS hostname", "", "kWorkBookPro.local", "heya-kWorkBookPro"},
		{"prefixes a bare hostname", "", "knas", "heya-knas"},
		{"explicit override is used verbatim", "media-box", "knas", "media-box"},
		{"override is reduced to one label", "media-box.lan", "knas", "media-box"},
		{"does not stack prefixes", "", "heya-knas", "heya-knas"},
		{"prefix match is case-insensitive", "", "HEYA-knas", "HEYA-knas"},
		{"falls back when the OS says nothing", "", "", "heya"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := advertisedHostLabel(tc.configured, tc.osHost)
			if got != tc.want {
				t.Fatalf("advertisedHostLabel(%q, %q) = %q, want %q", tc.configured, tc.osHost, got, tc.want)
			}
			if tc.configured == "" && tc.osHost != "" && got == hostLabel(tc.osHost) &&
				!strings.HasPrefix(strings.ToLower(tc.osHost), hostPrefix) {
				t.Errorf("advertised name %q equals the machine's own name — the OS will rename the machine", got)
			}
		})
	}
}

func TestAdvertisedHostLabelStaysWithinTheDNSLabelLimit(t *testing.T) {
	got := advertisedHostLabel("", strings.Repeat("h", 200))
	if len(got) != 63 {
		t.Errorf("label is %d bytes, want 63 — an over-long name fails to pack and kills every answer", len(got))
	}
}
