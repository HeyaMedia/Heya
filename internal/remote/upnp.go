package remote

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

// igdClient is the subset of the WANIPConnection SOAP surface we drive,
// satisfied by all three generated goupnp client generations.
type igdClient interface {
	GetExternalIPAddress() (string, error)
	AddPortMapping(remoteHost string, extPort uint16, protocol string, intPort uint16, intClient string, enabled bool, desc string, lease uint32) error
	DeletePortMapping(remoteHost string, extPort uint16, protocol string) error
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
// preferring IGDv2.
func discoverGateway() (*upnpGateway, error) {
	if clients, _, err := internetgateway2.NewWANIPConnection2Clients(); err == nil && len(clients) > 0 {
		return newUPnPGateway(clients[0], clients[0].Location.String()), nil
	}
	if clients, _, err := internetgateway2.NewWANIPConnection1Clients(); err == nil && len(clients) > 0 {
		return newUPnPGateway(clients[0], clients[0].Location.String()), nil
	}
	if clients, _, err := internetgateway1.NewWANIPConnection1Clients(); err == nil && len(clients) > 0 {
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

func (g *upnpGateway) externalIP() (string, error) {
	return g.client.GetExternalIPAddress()
}

// addMappings asserts extPort→lanIP:extPort for TCP (HTTP/1.1 + HTTP/2) and
// UDP (HTTP/3). Same-port inside and out keeps the URL story simple. Each
// transport is attempted independently so an UDP-hostile router cannot take
// otherwise-working remote HTTPS down with it.
func (g *upnpGateway) addMappings(port int, lanIP string) ([]PortMappingStatus, error) {
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
		err := g.addMappingLocked(port, lanIP, protocol, lease)
		if err != nil && lease != 0 {
			if permanentErr := g.addMappingLocked(port, lanIP, protocol, 0); permanentErr == nil {
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

func (g *upnpGateway) addMappingLocked(port int, lanIP, protocol string, lease uint32) error {
	p := uint16(port)
	if err := g.client.AddPortMapping("", p, protocol, p, lanIP, true, mappingDescription, lease); err != nil {
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

func (g *upnpGateway) unmapMappings(port int) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	var errs []error
	for _, protocol := range mappingProtocols {
		if err := g.client.DeletePortMapping("", uint16(port), protocol); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", strings.ToLower(protocol), err))
		}
	}
	return errors.Join(errs...)
}
