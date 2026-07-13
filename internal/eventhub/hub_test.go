package eventhub

import "testing"

func TestClientDevicesAreUserScopedAndUpserted(t *testing.T) {
	h := New()
	h.UpsertDevice(1, ClientDevice{ID: "client:desktop", Name: "Desktop"})
	h.UpsertDevice(2, ClientDevice{ID: "client:phone", Name: "Phone"})
	h.UpsertDevice(1, ClientDevice{ID: "client:desktop", Name: "Office Desktop", State: map[string]any{"playing": true}})

	one := h.ClientDevices(1)
	if len(one) != 1 || one[0].Name != "Office Desktop" || one[0].State["playing"] != true {
		t.Fatalf("unexpected user 1 devices: %#v", one)
	}
	two := h.ClientDevices(2)
	if len(two) != 1 || two[0].ID != "client:phone" {
		t.Fatalf("unexpected user 2 devices: %#v", two)
	}
}
