package cast

import (
	"context"
	"strings"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

// airplayProvider discovers AirPlay 2 receivers over mDNS and creates
// cliap2-backed transports for them. First (and currently only) entry
// in the provider table; yamaha.go / sony.go / nad.go etc. join it as
// vendor backchannels and URL-push providers get built.
type airplayProvider struct {
	binPath string // extracted cliap2, resolved once at Manager start
}

const (
	airplayServiceType = "_airplay._tcp"
	mdnsDomain         = "local."

	// browseWindow/browseIdle: zeroconf.Browse listens for the window,
	// then we idle before re-querying. Receivers also gratuitously
	// announce on state change, which a live window picks up; the idle
	// keeps steady-state multicast traffic negligible.
	browseWindow = 25 * time.Second
	browseIdle   = 35 * time.Second
)

func (p *airplayProvider) Name() string { return "airplay" }

func (p *airplayProvider) Browse(ctx context.Context, found func(Device)) error {
	for {
		if err := p.browseOnce(ctx, found); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(browseIdle):
		}
	}
}

func (p *airplayProvider) browseOnce(ctx context.Context, found func(Device)) error {
	winCtx, cancel := context.WithTimeout(ctx, browseWindow)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry, 8)
	go func() {
		for e := range entries {
			if dev, ok := deviceFromEntry(e); ok {
				found(dev)
			}
		}
	}()
	return zeroconf.Browse(winCtx, airplayServiceType, mdnsDomain, entries)
}

func deviceFromEntry(e *zeroconf.ServiceEntry) (Device, bool) {
	if e == nil || len(e.AddrIPv4) == 0 || e.Port == 0 {
		return Device{}, false
	}
	txt := txtValue(e.Text, "deviceid")
	if txt == "" {
		// cliap2 refuses devices without a deviceid — and then plays PCM
		// into the void while reporting "playing". Filter here so that
		// failure mode is unreachable.
		return Device{}, false
	}
	return Device{
		ID:           "airplay:" + strings.ToLower(txt),
		Provider:     "airplay",
		Capabilities: []string{"audio"},
		Name:         dnsUnescape(e.Instance),
		Model:        txtValue(e.Text, "model"),
		Manufacturer: txtValue(e.Text, "manufacturer"),
		Host:         e.HostName,
		Addr:         e.AddrIPv4[0].String(),
		Port:         e.Port,
		LastSeen:     time.Now(),
		TXT:          e.Text,
	}, true
}

// dnsUnescape decodes DNS presentation format as delivered by the
// zeroconf library's instance names: `\DDD` decimal byte escapes (UTF-8
// bytes of e.g. æ arrive as `\195\166`) and `\X` character escapes.
func dnsUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		if i+3 < len(s) && isDigit(s[i+1]) && isDigit(s[i+2]) && isDigit(s[i+3]) {
			b.WriteByte((s[i+1]-'0')*100 + (s[i+2]-'0')*10 + (s[i+3] - '0'))
			i += 3
			continue
		}
		i++
		b.WriteByte(s[i])
	}
	return b.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func txtValue(txt []string, key string) string {
	for _, kv := range txt {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

func (p *airplayProvider) NewTransport(dev Device) (Transport, error) {
	return newAirplayTransport(dev, p.binPath), nil
}
