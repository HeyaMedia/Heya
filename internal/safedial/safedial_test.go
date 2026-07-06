package safedial

import "testing"

func TestControl(t *testing.T) {
	allowed := []string{
		"8.8.8.8:443",
		"1.1.1.1:80",
		"93.184.216.34:443", // example.com
	}
	blocked := []string{
		"127.0.0.1:80",       // loopback
		"[::1]:80",           // loopback v6
		"10.1.2.3:80",        // private
		"172.16.5.4:80",      // private
		"192.168.1.1:80",     // private
		"169.254.169.254:80", // link-local (cloud metadata)
		"100.64.1.1:80",      // CGNAT / tailnet
		"100.127.255.1:80",   // CGNAT upper bound
		"0.0.0.0:80",         // unspecified
		"notanip:80",         // unresolved host
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
