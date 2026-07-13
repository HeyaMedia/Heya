package cast

// Static device resolution for networks where multicast never reaches the
// server: containers behind a CNI (pod egress ≠ LAN), receivers on another
// VLAN, no mDNS reflector on the router. AirPlay receivers answer one-shot
// UNICAST mDNS queries (RFC 6762 §6.7 legacy unicast) sent straight to
// <ip>:5353 — validated against the house RX-V6A: a single PTR query for
// _airplay._tcp.local returns PTR + SRV + TXT + A in one response. So a
// bare IP in HEYA_CAST_DEVICES is enough to build the verbatim-TXT Device
// cliap2 needs, with zero infrastructure changes.
//
// Note this only solves DISCOVERY — the audio session still needs plain
// unicast routability to the receiver (RTSP :7000 + UDP), which is also
// exactly what these deployments must provide (hostNetwork or SNAT'd
// egress). See docs/deployment.md.

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/dns/dnsmessage"
)

const (
	// Same cadence as the multicast browse loop: static targets re-resolve
	// so renames and firmware TXT changes surface without a restart.
	staticResolveInterval = 60 * time.Second
	staticQueryTimeout    = 3 * time.Second
	mdnsPort              = "5353"
)

// StaticTargetStatus is the per-address outcome of the last unicast
// resolve pass — the Settings → Casting debug page renders these so "why
// is my receiver missing" answers itself.
type StaticTargetStatus struct {
	Addr      string    `json:"addr"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	DeviceID  string    `json:"device_id,omitempty"`
	Name      string    `json:"name,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// resolveStaticLoop periodically unicast-resolves the configured addresses
// and feeds the shared device cache. Failures are recorded per address and
// retried on the next tick — a powered-off receiver is normal. Note the
// hard physics: receivers enforce RFC 6762's source-address check, so
// unicast queries only get answers from the SAME subnet — a cross-VLAN
// target will sit here erroring until the server gets a leg on that L2.
func (m *Manager) resolveStaticLoop(ctx context.Context, addrs []string) {
	for {
		for _, addr := range addrs {
			dev, err := resolveStaticAirplay(ctx, addr)
			status := StaticTargetStatus{Addr: addr, CheckedAt: time.Now()}
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				status.Error = err.Error()
				log.Debug().Err(err).Str("addr", addr).Msg("cast: static device did not resolve")
			} else {
				status.OK = true
				status.DeviceID = dev.ID
				status.Name = dev.Name
				m.upsertDevice(dev)
			}
			m.setStaticStatus(status)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(staticResolveInterval):
		}
	}
}

// resolveStaticAirplay sends one unicast mDNS PTR query for _airplay._tcp
// to the address and assembles a Device from the answer + additionals.
func resolveStaticAirplay(ctx context.Context, addr string) (Device, error) {
	target := addr
	if _, _, err := net.SplitHostPort(target); err != nil {
		target = net.JoinHostPort(target, mdnsPort)
	}
	d := net.Dialer{Timeout: staticQueryTimeout}
	conn, err := d.DialContext(ctx, "udp", target)
	if err != nil {
		return Device{}, fmt.Errorf("dial %s: %w", target, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(staticQueryTimeout))

	query, err := packAirplayQuery()
	if err != nil {
		return Device{}, err
	}
	if _, err := conn.Write(query); err != nil {
		return Device{}, fmt.Errorf("query %s: %w", target, err)
	}

	buf := make([]byte, 9000)
	n, err := conn.Read(buf)
	if err != nil {
		return Device{}, fmt.Errorf("no mDNS answer from %s: %w", target, err)
	}

	host, _, _ := net.SplitHostPort(target)
	dev, ok := deviceFromDNSResponse(buf[:n], host)
	if !ok {
		return Device{}, fmt.Errorf("%s answered but no usable _airplay._tcp record (missing deviceid/SRV)", target)
	}
	return dev, nil
}

func packAirplayQuery() ([]byte, error) {
	name, err := dnsmessage.NewName(airplayServiceType + "." + mdnsDomain)
	if err != nil {
		return nil, err
	}
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{RecursionDesired: false},
		Questions: []dnsmessage.Question{{
			Name:  name,
			Type:  dnsmessage.TypePTR,
			Class: dnsmessage.ClassINET,
		}},
	}
	return msg.Pack()
}

// deviceFromDNSResponse walks a parsed mDNS response and builds a Device.
// queriedHost is used as the address — we just proved the receiver answers
// there, which beats trusting an A record that may point at another
// interface. Split from the network path so a captured packet can pin the
// parse in tests.
func deviceFromDNSResponse(packet []byte, queriedHost string) (Device, bool) {
	var msg dnsmessage.Message
	if err := msg.Unpack(packet); err != nil {
		return Device{}, false
	}

	// The instance we're assembling: first PTR answer wins.
	var instance string
	var port int
	var txt []string
	var srvHost string

	records := make([]dnsmessage.Resource, 0, len(msg.Answers)+len(msg.Additionals))
	records = append(records, msg.Answers...)
	records = append(records, msg.Additionals...)

	for _, r := range records {
		switch body := r.Body.(type) {
		case *dnsmessage.PTRResource:
			if instance == "" && r.Header.Name.String() == airplayServiceType+"."+mdnsDomain {
				instance = body.PTR.String()
			}
		case *dnsmessage.SRVResource:
			if instance == "" || r.Header.Name.String() == instance {
				port = int(body.Port)
				srvHost = body.Target.String()
			}
		case *dnsmessage.TXTResource:
			if instance == "" || r.Header.Name.String() == instance {
				txt = body.TXT
			}
		}
	}
	if instance == "" || port == 0 || len(txt) == 0 {
		return Device{}, false
	}
	deviceID := txtValue(txt, "deviceid")
	if deviceID == "" {
		// Same guard as deviceFromEntry: cliap2 plays into the void
		// without a deviceid TXT.
		return Device{}, false
	}

	// dnsmessage.Name.String() yields raw label bytes, so "Anlæg" arrives
	// as UTF-8 already; dnsUnescape no-ops there but still normalizes any
	// \DDD-escaped presentation form defensively (zeroconf's format).
	name := strings.TrimSuffix(instance, "."+airplayServiceType+"."+mdnsDomain)
	return Device{
		ID:           "airplay:" + strings.ToLower(deviceID),
		Provider:     "airplay",
		Capabilities: []string{"audio"},
		Name:         dnsUnescape(name),
		Model:        txtValue(txt, "model"),
		Manufacturer: txtValue(txt, "manufacturer"),
		Host:         srvHost,
		Addr:         queriedHost,
		Port:         port,
		LastSeen:     time.Now(),
		TXT:          txt,
	}, true
}
