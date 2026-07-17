package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

// igdClient is the subset of the WANIPConnection SOAP surface we drive,
// satisfied by all three generated goupnp client generations. Only the *Ctx
// variants are acceptable here: the legacy methods hardcode
// context.Background() over a zero-timeout http.Client, so a router whose
// UPnP endpoint stops responding mid-call would hang the caller — and the
// opMu-serialized Enable/Disable/Recheck surface with it — forever.
type igdClient interface {
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
	AddPortMappingCtx(ctx context.Context, remoteHost string, extPort uint16, protocol string, intPort uint16, intClient string, enabled bool, desc string, lease uint32) error
	DeletePortMappingCtx(ctx context.Context, remoteHost string, extPort uint16, protocol string) error
}

// soapCallTimeout bounds a single SOAP round trip against the router. A LAN
// router answers these in well under a second; anything slower is a wedged
// UPnP stack we must not wait on.
const soapCallTimeout = 10 * time.Second

func soapCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, soapCallTimeout)
}

// upnpGateway wraps a discovered IGD. Discovery is slow (multicast SSDP,
// ~2-3s) so the client is cached for the lifetime of an Enable; the
// maintenance loop reuses it for lease renewal.
type upnpGateway struct {
	client igdClient
	loc    string
	mu     sync.Mutex
	// leaseSeconds records what the router accepted for each transport:
	// preferred 7200, falling back to 0 (permanent) for routers that reject
	// timed leases. Routers are allowed to behave differently for TCP/UDP.
	leaseSeconds map[string]uint32
}

const mappingDescription = "Heya remote access"

var mappingProtocols = []string{"TCP", "UDP"}

// discoverGateway finds the first WANIPConnection service on the LAN,
// preferring IGDv2. Bounded as a whole: each generation is one multicast
// SSDP search (fixed ~2s window inside goupnp) plus a description fetch per
// responder, and a device that accepts the TCP connection but never serves
// its description document must not stall Enable indefinitely.
func discoverGateway(ctx context.Context) (*upnpGateway, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if clients, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx); err == nil && len(clients) > 0 {
		return newUPnPGateway(clients[0], clients[0].Location.String()), nil
	}
	if clients, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx); err == nil && len(clients) > 0 {
		return newUPnPGateway(clients[0], clients[0].Location.String()), nil
	}
	if clients, _, err := internetgateway1.NewWANIPConnection1ClientsCtx(ctx); err == nil && len(clients) > 0 {
		return newUPnPGateway(clients[0], clients[0].Location.String()), nil
	}
	return nil, errors.New("no UPnP internet gateway found on this network (is UPnP enabled on the router?)")
}

func newUPnPGateway(client igdClient, location string) *upnpGateway {
	return &upnpGateway{
		client: client,
		loc:    location,
		leaseSeconds: map[string]uint32{
			"TCP": 7200,
			"UDP": 7200,
		},
	}
}

func (g *upnpGateway) location() string { return g.loc }

func (g *upnpGateway) externalIP(ctx context.Context) (string, error) {
	ctx, cancel := soapCtx(ctx)
	defer cancel()
	return g.client.GetExternalIPAddressCtx(ctx)
}

// addMappings asserts extPort→lanIP:extPort for TCP (HTTP/1.1 + HTTP/2) and
// UDP (HTTP/3). Same-port inside and out keeps the URL story simple. Each
// transport is attempted independently so an UDP-hostile router cannot take
// otherwise-working remote HTTPS down with it.
func (g *upnpGateway) addMappings(ctx context.Context, port int, lanIP string) ([]PortMappingStatus, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if lanIP == "" {
		err := errors.New("no LAN IP detected")
		return failedMappings(port, lanIP, err), err
	}

	mappings := make([]PortMappingStatus, 0, len(mappingProtocols))
	var errs []error
	for _, protocol := range mappingProtocols {
		lease := g.leaseSeconds[protocol]
		err := g.addMappingLocked(ctx, port, lanIP, protocol, lease)
		if err != nil && lease != 0 {
			if permanentErr := g.addMappingLocked(ctx, port, lanIP, protocol, 0); permanentErr == nil {
				lease = 0
				g.leaseSeconds[protocol] = 0
				err = nil
			} else {
				err = fmt.Errorf("timed lease failed: %v; permanent fallback failed: %w", err, permanentErr)
			}
		}

		mapping := PortMappingStatus{
			Protocol: protocol, ExternalPort: port, InternalIP: lanIP,
			InternalPort: port, LeaseSeconds: lease, Active: err == nil,
		}
		if err != nil {
			mapping.Error = err.Error()
			errs = append(errs, fmt.Errorf("%s: %w", strings.ToLower(protocol), err))
		} else {
			mapping.MappedAt = time.Now().UTC().Format(time.RFC3339)
		}
		mappings = append(mappings, mapping)
	}
	return mappings, errors.Join(errs...)
}

func (g *upnpGateway) addMappingLocked(ctx context.Context, port int, lanIP, protocol string, lease uint32) error {
	callCtx, cancel := soapCtx(ctx)
	defer cancel()
	p := uint16(port)
	if err := g.client.AddPortMappingCtx(callCtx, "", p, protocol, p, lanIP, true, mappingDescription, lease); err != nil {
		return fmt.Errorf("router rejected port mapping: %w", err)
	}
	return nil
}

func failedMappings(port int, lanIP string, err error) []PortMappingStatus {
	mappings := make([]PortMappingStatus, 0, len(mappingProtocols))
	for _, protocol := range mappingProtocols {
		mappings = append(mappings, PortMappingStatus{
			Protocol: protocol, ExternalPort: port, InternalIP: lanIP,
			InternalPort: port, Error: err.Error(),
		})
	}
	return mappings
}

func (g *upnpGateway) unmapMappings(ctx context.Context, port int) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	var errs []error
	for _, protocol := range mappingProtocols {
		callCtx, cancel := soapCtx(ctx)
		err := g.client.DeletePortMappingCtx(callCtx, "", uint16(port), protocol)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", strings.ToLower(protocol), err))
		}
	}
	return errors.Join(errs...)
}
