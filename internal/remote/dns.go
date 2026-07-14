package remote

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/libdns/cloudflare"
	"github.com/libdns/desec"
	"github.com/libdns/duckdns"
	"github.com/libdns/libdns"
)

// fullProvider is what all three supported providers actually implement.
type fullProvider interface {
	libdns.RecordAppender
	libdns.RecordDeleter
	libdns.RecordSetter
	libdns.RecordGetter
}

// buildProvider constructs the libdns provider for cfg. The zone handed to
// libdns calls is always cfg.Domain — see zonePinned for why.
func buildProvider(cfg Config) (fullProvider, error) {
	switch cfg.DNSProvider {
	case "desec":
		return &desec.Provider{Token: cfg.DNSToken}, nil
	case "duckdns":
		return &duckdns.Provider{APIToken: cfg.DNSToken}, nil
	case "cloudflare":
		return &cloudflare.Provider{APIToken: cfg.DNSToken}, nil
	default:
		return nil, fmt.Errorf("unknown DNS provider %q", cfg.DNSProvider)
	}
}

// propagationDelay is how long certmagic waits before starting DNS-01
// propagation checks, tuned per provider: DuckDNS's anycast lags well behind
// its API accepting the update; deSEC and Cloudflare converge fast.
func propagationDelay(provider string) time.Duration {
	switch provider {
	case "duckdns":
		return 60 * time.Second
	case "desec":
		return 15 * time.Second
	default:
		return 10 * time.Second
	}
}

// zonePinned rebases every record onto a fixed zone before handing it to the
// wrapped provider. certmagic discovers the DNS zone by SOA-walking, which
// is wrong for DuckDNS: myname.duckdns.org has no SOA of its own, so the
// walk lands on duckdns.org and the provider would try to manage the domain
// "duckdns" — but the libdns duckdns provider needs the zone to be the
// user's own myname.duckdns.org. Pinning is a no-op for deSEC (per-domain
// SOA) and Cloudflare (cfg.Domain is the CF zone), so it's applied
// uniformly rather than special-cased.
type zonePinned struct {
	inner fullProvider
	zone  string // FQDN with trailing dot
}

func pinZone(p fullProvider, domain string) *zonePinned {
	return &zonePinned{inner: p, zone: domain + "."}
}

// rebase converts records that are relative to fromZone into records
// relative to the pinned zone, going through the fully-qualified name.
func (z *zonePinned) rebase(fromZone string, recs []libdns.Record) []libdns.Record {
	out := make([]libdns.Record, 0, len(recs))
	for _, rec := range recs {
		rr := rec.RR()
		fqdn := libdns.AbsoluteName(rr.Name, fromZone)
		rr.Name = libdns.RelativeName(fqdn, z.zone)
		out = append(out, rr)
	}
	return out
}

func (z *zonePinned) AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	return z.inner.AppendRecords(ctx, z.zone, z.rebase(zone, recs))
}

func (z *zonePinned) DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	return z.inner.DeleteRecords(ctx, z.zone, z.rebase(zone, recs))
}

func (z *zonePinned) SetRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	return z.inner.SetRecords(ctx, z.zone, z.rebase(zone, recs))
}

func (z *zonePinned) GetRecords(ctx context.Context, zone string) ([]libdns.Record, error) {
	return z.inner.GetRecords(ctx, z.zone)
}

// recordSyncer keeps the lan./wan. A records pointed at the right IPs.
type recordSyncer struct {
	provider *zonePinned
	names    dnsNames
}

func newRecordSyncer(cfg Config, names dnsNames) (*recordSyncer, error) {
	p, err := buildProvider(cfg)
	if err != nil {
		return nil, err
	}
	return &recordSyncer{provider: pinZone(p, cfg.Domain), names: names}, nil
}

const recordTTL = 5 * time.Minute

func (r *recordSyncer) set(ctx context.Context, name string, ip netip.Addr) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := r.provider.SetRecords(ctx, r.names.zone+".", []libdns.Record{
		libdns.Address{Name: name, TTL: recordTTL, IP: ip},
	})
	return err
}

// syncLAN points lan.<base> at the server's LAN IP. Providers without
// multi-record support (DuckDNS) have no lanRel and skip silently.
func (r *recordSyncer) syncLAN(ctx context.Context, ip netip.Addr) error {
	if r.names.lanRel == "" {
		return nil
	}
	return r.set(ctx, r.names.lanRel, ip)
}

// syncWAN points wan.<base> (or the DuckDNS apex) at the internet-facing IP.
func (r *recordSyncer) syncWAN(ctx context.Context, ip netip.Addr) error {
	return r.set(ctx, r.names.wanRel, ip)
}
