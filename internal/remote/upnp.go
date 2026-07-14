package remote

import (
	"errors"
	"fmt"

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
	// leaseSeconds is what the router accepted: preferred 7200, falling back
	// to 0 (permanent) for routers that reject timed leases.
	leaseSeconds uint32
}

const mappingDescription = "Heya remote access"

// discoverGateway finds the first WANIPConnection service on the LAN,
// preferring IGDv2.
func discoverGateway() (*upnpGateway, error) {
	if clients, _, err := internetgateway2.NewWANIPConnection2Clients(); err == nil && len(clients) > 0 {
		return &upnpGateway{client: clients[0], loc: clients[0].Location.String(), leaseSeconds: 7200}, nil
	}
	if clients, _, err := internetgateway2.NewWANIPConnection1Clients(); err == nil && len(clients) > 0 {
		return &upnpGateway{client: clients[0], loc: clients[0].Location.String(), leaseSeconds: 7200}, nil
	}
	if clients, _, err := internetgateway1.NewWANIPConnection1Clients(); err == nil && len(clients) > 0 {
		return &upnpGateway{client: clients[0], loc: clients[0].Location.String(), leaseSeconds: 7200}, nil
	}
	return nil, errors.New("no UPnP internet gateway found on this network (is UPnP enabled on the router?)")
}

func (g *upnpGateway) location() string { return g.loc }

func (g *upnpGateway) externalIP() (string, error) {
	return g.client.GetExternalIPAddress()
}

// addMapping asserts extPort→lanIP:extPort TCP. Same-port inside and out keeps the
// URL story simple (one port in bookmarks, one listener). Timed lease first;
// some routers (older FRITZ!Box, some TP-Link) only accept lease 0.
func (g *upnpGateway) addMapping(port int, lanIP string) error {
	if lanIP == "" {
		return errors.New("no LAN IP detected")
	}
	p := uint16(port)
	err := g.client.AddPortMapping("", p, "TCP", p, lanIP, true, mappingDescription, g.leaseSeconds)
	if err != nil && g.leaseSeconds != 0 {
		if permErr := g.client.AddPortMapping("", p, "TCP", p, lanIP, true, mappingDescription, 0); permErr == nil {
			g.leaseSeconds = 0
			return nil
		}
		return fmt.Errorf("router rejected port mapping: %w", err)
	}
	if err != nil {
		return fmt.Errorf("router rejected port mapping: %w", err)
	}
	return nil
}

func (g *upnpGateway) unmap(port int) error {
	return g.client.DeletePortMapping("", uint16(port), "TCP")
}
