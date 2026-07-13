package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/eventhub"
)

func registerClientDeviceRoutes(api huma.API, hub *eventhub.Hub) {
	type listBody struct {
		Items []eventhub.ClientDevice `json:"items"`
	}
	huma.Register(api, secured(op(http.MethodGet, "/api/me/devices", "client-devices", "List online Heya clients", "Devices")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[listBody], error) {
			return noStoreJSON(listBody{Items: hub.ClientDevices(userFrom(ctx).ID)}), nil
		})
	huma.Register(api, secured(op(http.MethodPost, "/api/me/devices/{id}/command", "client-device-command", "Control another Heya client", "Devices")),
		func(ctx context.Context, in *struct {
			ID   string `path:"id"`
			Body struct {
				Action string         `json:"action" minLength:"1"`
				Args   map[string]any `json:"args,omitempty"`
			}
		}) (*struct{}, error) {
			found := false
			for _, d := range hub.ClientDevices(userFrom(ctx).ID) {
				if d.ID == in.ID {
					found = true
					break
				}
			}
			if !found {
				return nil, huma.Error404NotFound("device is offline")
			}
			var b [8]byte
			_, _ = rand.Read(b[:])
			hub.EmitToUser(userFrom(ctx).ID, eventhub.EventDeviceCommand, eventhub.DeviceCommandPayload{
				TargetDeviceID: in.ID, CommandID: hex.EncodeToString(b[:]), Action: in.Body.Action, Args: in.Body.Args,
			})
			return &struct{}{}, nil
		})
}
