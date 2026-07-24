package discovery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

// Found is one server that answered a browse, with the TXT record already
// parsed. It is the shape a client implementor needs, which is why the CLI
// prints it verbatim: whatever `heya discovery browse` shows is exactly what
// HeyaClient/HeyaTV have to work with.
type Found struct {
	Instance string            `json:"instance"`
	Hostname string            `json:"hostname"`
	Port     int               `json:"port"`
	IPv4     []string          `json:"ipv4,omitempty"`
	IPv6     []string          `json:"ipv6,omitempty"`
	TXT      map[string]string `json:"txt,omitempty"`
	RawTXT   []string          `json:"raw_txt,omitempty"`
}

// URL is the origin a client should connect to: the first IPv4 answer when
// there is one (a literal address always resolves; `.local.` needs a working
// mDNS resolver on the client, which Android notably lacks), otherwise the
// advertised hostname. Scheme comes from TXT, defaulting to http.
func (f Found) URL() string {
	scheme := f.TXT["scheme"]
	if scheme != "http" && scheme != "https" {
		scheme = "http"
	}
	host := strings.TrimSuffix(f.Hostname, ".")
	if len(f.IPv4) > 0 {
		host = f.IPv4[0]
	} else if len(f.IPv6) > 0 {
		host = "[" + f.IPv6[0] + "]"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, f.Port)
}

// ID returns the stable server id from the TXT record, "" if absent.
func (f Found) ID() string { return f.TXT["id"] }

// Browse runs one multicast query window and returns everything that
// answered, deduplicated by instance name and sorted for stable output.
//
// The channel protocol below is dictated by conditional ownership inside
// zeroconf: its mainloop closes `entries` when it ran, but a setup failure
// (no multicast-capable interface — real in container/CNI deployments)
// returns from Browse before the mainloop ever starts, leaving the channel
// open and senderless, so a bare `for range` drainer would leak. We never
// close it (the mainloop may already have) and release the drainer via quit
// after Browse returns — at that point no sender can exist, since Browse
// joins its mainloop before returning. While Browse runs the drainer must
// keep receiving unconditionally: the mainloop's send is blocking, and
// abandoning the channel would wedge it. internal/cast's browseService
// carries the same reasoning for the receiver-discovery side.
func Browse(ctx context.Context, serviceType string, window time.Duration) ([]Found, error) {
	if strings.TrimSpace(serviceType) == "" {
		serviceType = ServiceType
	}

	entries := make(chan *zeroconf.ServiceEntry, 16)
	quit := make(chan struct{})
	drained := make(chan struct{})
	byInstance := map[string]Found{}

	go func() {
		defer close(drained)
		collect := func(e *zeroconf.ServiceEntry) {
			if e == nil {
				return
			}
			found := fromEntry(e)
			// A later answer carries at least as much detail as an earlier
			// one (address records often arrive after the PTR), so overwrite.
			byInstance[found.Instance] = found
		}
		for {
			select {
			case e, ok := <-entries:
				if !ok {
					return
				}
				collect(e)
			case <-quit:
				for {
					select {
					case e, ok := <-entries:
						if !ok {
							return
						}
						collect(e)
					default:
						return
					}
				}
			}
		}
	}()

	winCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	err := zeroconf.Browse(winCtx, serviceType, Domain, entries)
	close(quit)
	<-drained

	// A window that simply expired is the success path, not a failure.
	if err != nil && ctx.Err() == nil && winCtx.Err() == context.DeadlineExceeded && len(byInstance) > 0 {
		err = nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]Found, 0, len(byInstance))
	for _, found := range byInstance {
		out = append(out, found)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Instance < out[j].Instance })
	return out, nil
}

func fromEntry(e *zeroconf.ServiceEntry) Found {
	found := Found{
		Instance: e.Instance,
		Hostname: strings.TrimSuffix(e.HostName, "."),
		Port:     e.Port,
		TXT:      ParseTXT(e.Text),
		RawTXT:   e.Text,
	}
	for _, ip := range e.AddrIPv4 {
		found.IPv4 = append(found.IPv4, ip.String())
	}
	for _, ip := range e.AddrIPv6 {
		// Link-local answers need a zone to be dialable and are noise in a
		// picker; skip them rather than hand a client an address that fails.
		if ip.IsLinkLocalUnicast() {
			continue
		}
		found.IPv6 = append(found.IPv6, ip.String())
	}
	return found
}

// ParseTXT turns DNS-SD `key=value` strings into a map. Keys are lowercased
// (DNS-SD says they're case-insensitive); entries without `=` are skipped.
func ParseTXT(entries []string) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return out
}
