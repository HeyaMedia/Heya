package cast

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestResolveStaticAirplayLive unicast-queries a real receiver. Discovery
// only — nothing plays. Needs a reachable AirPlay device:
//
//	HEYA_CAST_STATIC_TEST_ADDR=192.168.1.216 go test ./internal/cast/ -run StaticAirplayLive -v
func TestResolveStaticAirplayLive(t *testing.T) {
	addr := os.Getenv("HEYA_CAST_STATIC_TEST_ADDR")
	if addr == "" {
		t.Skip("set HEYA_CAST_STATIC_TEST_ADDR to a receiver IP to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev, err := resolveStaticAirplay(ctx, addr)
	if err != nil {
		t.Fatalf("resolve %s: %v", addr, err)
	}
	if dev.ID == "" || dev.Port == 0 || dev.Name == "" {
		t.Fatalf("incomplete device: %+v", dev)
	}
	if dev.Addr != addr {
		t.Errorf("Addr = %q, want the queried host %q", dev.Addr, addr)
	}
	if txtValue(dev.TXT, "deviceid") == "" {
		t.Error("TXT lost the deviceid cliap2 requires")
	}
	t.Logf("resolved: %s (%s %s) %s:%d deviceid=%s txt=%d entries",
		dev.Name, dev.Manufacturer, dev.Model, dev.Addr, dev.Port,
		txtValue(dev.TXT, "deviceid"), len(dev.TXT))
}
