package safedial

import "testing"

func TestControl(t *testing.T) {
	allowed := []string{
		"8.8.8.8:443",
		"1.1.1.1:80",
		"93.184.216.34:443", // example.com
		"[2606:4700:4700::1111]:443",
		"[64:ff9b::808:808]:443", // well-known NAT64 carrying 8.8.8.8
	}
	blocked := []string{
		"127.0.0.1:80",         // loopback
		"[::1]:80",             // loopback v6
		"10.1.2.3:80",          // private
		"172.16.5.4:80",        // private
		"192.168.1.1:80",       // private
		"169.254.169.254:80",   // link-local (cloud metadata)
		"100.64.1.1:80",        // CGNAT / tailnet
		"100.127.255.1:80",     // CGNAT upper bound
		"0.0.0.0:80",           // unspecified
		"192.0.2.1:80",         // documentation
		"198.18.0.1:80",        // benchmarking
		"224.0.0.1:80",         // multicast
		"240.0.0.1:80",         // reserved
		"[fc00::1]:80",         // unique-local v6
		"[fe80::1%lo0]:80",     // link-local v6 with zone
		"[2001:db8::1]:80",     // documentation v6
		"[64:ff9b::7f00:1]:80", // NAT64 carrying 127.0.0.1
		"notanip:80",           // unresolved host
	}
	for _, a := range allowed {
		if err := Control("tcp", a, nil); err != nil {
			t.Errorf("Control(%q) = %v, want allowed", a, err)
		}
	}
	for _, a := range blocked {
		if err := Control("tcp", a, nil); err == nil {
			t.Errorf("Control(%q) = nil, want rejected", a)
		}
	}
}
