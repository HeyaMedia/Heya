package transcoder

import "testing"

func TestHwAccelProviderConfigureAppliesToFutureResolution(t *testing.T) {
	provider := NewHwAccelProvider(t.TempDir(), string(HwAccelNone))
	if got := provider.Get().Type; got != HwAccelNone {
		t.Fatalf("initial hardware acceleration = %s, want %s", got, HwAccelNone)
	}

	provider.Configure(string(HwAccelVideoToolbox))
	if got := provider.Get().Type; got != HwAccelVideoToolbox {
		t.Fatalf("reconfigured hardware acceleration = %s, want %s", got, HwAccelVideoToolbox)
	}
}
