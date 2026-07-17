package remote

import (
	"errors"
	"testing"
)

type mappingCall struct {
	protocol string
	lease    uint32
}

type fakeIGD struct {
	add       func(protocol string, lease uint32) error
	adds      []mappingCall
	deletions []string
}

func (f *fakeIGD) GetExternalIPAddress() (string, error) { return "203.0.113.7", nil }

func (f *fakeIGD) AddPortMapping(_ string, _ uint16, protocol string, _ uint16, _ string, _ bool, _ string, lease uint32) error {
	f.adds = append(f.adds, mappingCall{protocol: protocol, lease: lease})
	if f.add != nil {
		return f.add(protocol, lease)
	}
	return nil
}

func (f *fakeIGD) DeletePortMapping(_ string, _ uint16, protocol string) error {
	f.deletions = append(f.deletions, protocol)
	return nil
}

func TestUPnPMapsTCPAndUDPWithIndependentLeaseFallback(t *testing.T) {
	client := &fakeIGD{add: func(protocol string, lease uint32) error {
		if protocol == "UDP" && lease != 0 {
			return errors.New("timed UDP lease rejected")
		}
		return nil
	}}
	gateway := newUPnPGateway(client, "http://router.test/igd")
	mappings, err := gateway.addMappings(44321, "192.168.1.12")
	if err != nil {
		t.Fatalf("addMappings: %v", err)
	}
	if len(mappings) != 2 || !mappings[0].Active || !mappings[1].Active {
		t.Fatalf("mappings = %+v", mappings)
	}
	if mappings[0].Protocol != "TCP" || mappings[0].LeaseSeconds != 7200 {
		t.Fatalf("TCP mapping = %+v", mappings[0])
	}
	if mappings[1].Protocol != "UDP" || mappings[1].LeaseSeconds != 0 {
		t.Fatalf("UDP mapping = %+v", mappings[1])
	}
	if len(client.adds) != 3 || client.adds[1] != (mappingCall{protocol: "UDP", lease: 7200}) || client.adds[2] != (mappingCall{protocol: "UDP", lease: 0}) {
		t.Fatalf("mapping calls = %+v", client.adds)
	}

	client.adds = nil
	if _, err := gateway.addMappings(44321, "192.168.1.12"); err != nil {
		t.Fatalf("renew mappings: %v", err)
	}
	if len(client.adds) != 2 || client.adds[1].lease != 0 {
		t.Fatalf("renewal did not retain UDP permanent lease: %+v", client.adds)
	}
}

func TestUPnPReportsOneTransportFailureWithoutHidingTheOther(t *testing.T) {
	client := &fakeIGD{add: func(protocol string, _ uint32) error {
		if protocol == "TCP" {
			return errors.New("TCP blocked")
		}
		return nil
	}}
	gateway := newUPnPGateway(client, "router")
	mappings, err := gateway.addMappings(44444, "192.168.1.20")
	if err == nil {
		t.Fatal("expected aggregate mapping error")
	}
	if mappings[0].Active || mappings[0].Error == "" {
		t.Fatalf("TCP failure not surfaced: %+v", mappings[0])
	}
	if !mappings[1].Active || mappings[1].Error != "" {
		t.Fatalf("UDP success was lost: %+v", mappings[1])
	}
	if err := gateway.unmapMappings(44444); err != nil {
		t.Fatalf("unmapMappings: %v", err)
	}
	if len(client.deletions) != 2 || client.deletions[0] != "TCP" || client.deletions[1] != "UDP" {
		t.Fatalf("deletions = %v", client.deletions)
	}
}
