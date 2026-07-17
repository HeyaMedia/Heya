package server

import (
	"context"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/ingress"
	"github.com/karbowiak/heya/internal/remote"
	"github.com/karbowiak/heya/internal/service"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"
)

// adminNetworkStatusBody is Heya's stable observability contract. Caddy's
// native metric names and tsnet's raw LocalAPI remain implementation details;
// the settings UI receives one coherent, cheap snapshot.
type adminNetworkStatusBody struct {
	Ingress   ingress.IngressStatus `json:"ingress"`
	Remote    *remote.RemoteStatus  `json:"remote,omitempty"`
	Tailscale *tsnetwrap.Status     `json:"tailscale,omitempty"`
	General   adminNetworkGeneral   `json:"general"`
	UpdatedAt time.Time             `json:"updated_at"`
}

type adminNetworkGeneral struct {
	Hostname      string                  `json:"hostname"`
	BindAddress   string                  `json:"bind_address"`
	LANIP         string                  `json:"lan_ip,omitempty"`
	HTTPSRequired bool                    `json:"https_required"`
	WSSubscribers int                     `json:"ws_subscribers"`
	Interfaces    []adminNetworkInterface `json:"interfaces"`
}

type adminNetworkInterface struct {
	Name         string   `json:"name"`
	MTU          int      `json:"mtu"`
	Flags        []string `json:"flags,omitempty"`
	HardwareAddr string   `json:"hardware_address,omitempty"`
	Addresses    []string `json:"addresses,omitempty"`
	Error        string   `json:"error,omitempty"`
}

func registerAdminNetworkRoutes(api huma.API, app *service.App, hub *eventhub.Hub) {
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/network/status", "admin-network-status", "Unified Caddy, UPnP, Tailscale and host network status", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[adminNetworkStatusBody], error) {
			return noStoreJSON(collectAdminNetworkStatus(app, hub)), nil
		})
}

func collectAdminNetworkStatus(app *service.App, hub *eventhub.Hub) adminNetworkStatusBody {
	now := time.Now().UTC()
	body := adminNetworkStatusBody{UpdatedAt: now}
	if manager := app.Ingress(); manager != nil {
		body.Ingress = manager.Status()
	}
	if manager := app.Remote(); manager != nil {
		status := manager.Status()
		body.Remote = &status
	}
	if manager := app.Tailscale(); manager != nil {
		status := manager.Status()
		body.Tailscale = &status
	}

	cfg := app.ConfigSnapshot()
	if cfg != nil {
		body.General.BindAddress = cfg.Addr()
	}
	body.General.Hostname, _ = os.Hostname()
	if hub != nil {
		body.General.WSSubscribers = hub.SubscriberCount()
	}
	body.General.LANIP = ingress.DetectLANIP()
	body.General.Interfaces = collectNetworkInterfaces()
	for _, listener := range body.Ingress.Listeners {
		if listener.Kind == "host" {
			body.General.HTTPSRequired = listener.TLS
			break
		}
	}
	return body
}

func collectNetworkInterfaces() []adminNetworkInterface {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []adminNetworkInterface{{Name: "unavailable", Error: err.Error()}}
	}
	result := make([]adminNetworkInterface, 0, len(interfaces))
	for _, iface := range interfaces {
		item := adminNetworkInterface{
			Name: iface.Name, MTU: iface.MTU, HardwareAddr: iface.HardwareAddr.String(),
			Flags: strings.Split(iface.Flags.String(), "|"),
		}
		addresses, addressErr := iface.Addrs()
		if addressErr != nil {
			item.Error = addressErr.Error()
		} else {
			for _, address := range addresses {
				item.Addresses = append(item.Addresses, address.String())
			}
			sort.Strings(item.Addresses)
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
