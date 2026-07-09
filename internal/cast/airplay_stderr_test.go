package cast

import "testing"

// Every input below is a verbatim line captured from cliap2 v1.5 runs
// against a Yamaha RX-V6A (2026-07-09/10, see docs/casting-research.md).
// If an upstream bump changes these strings, playback state detection
// breaks — this test is the tripwire.
func TestClassifyStderrLine(t *testing.T) {
	cases := []struct {
		name string
		line string
		want stderrEvent
	}{
		{
			"connected",
			"[2026-07-09 23:11:43] [DEBUG] [          (7408)]   player: Callback from AirPlay 2 device Anlæg to device_activate_cb (status 2)",
			evConnected,
		},
		{
			"play start",
			"[2026-07-09 23:11:46] [DEBUG] [          (7408)]   player: event_play_start()",
			evPlayStart,
		},
		{
			"end of stream",
			"[2026-07-09 23:11:56] [ INFO] [          (5200)]     fifo: play:Anlæg:end of stream reached",
			evEndOfStream,
		},
		{
			"rtsp closed",
			"[2026-07-09 23:22:16] [  LOG] [           (208)]  airplay: Device 'Anlæg' closed RTSP connection",
			evRTSPClosed,
		},
		{
			"device failed",
			"[2026-07-09 23:22:16] [ WARN] [           (208)]   player: The AirPlay 2 device 'Anlæg' failed",
			evDeviceFailed,
		},
		{
			"ntp too soon",
			"[2026-07-09 22:57:34] [ WARN] [         (-6048)]     main: get_start_ts:Anlæg:ntpstart time too soon. Adjust session_establishment_latency to align with device capability or increase ntpstart by at least 837 ms to prevent loss of audio.",
			evNTPTooSoon,
		},
		{
			// The commence-wedge trap: cliap2's internal player reports
			// status "playing" while sending nothing. Status lines must
			// classify as noise — only event_play_start() means playing.
			"internal playing status is noise",
			"[2026-07-09 23:21:47] [DEBUG] [           (208)]   player: Player status: playing",
			evNone,
		},
		{
			"pre-roll countdown is noise",
			"[2026-07-09 23:21:45] [ SPAM] [          (6768)]     fifo: play:Anlæg delta_ms = 4656 ms, latency_ms=0 ms, delta_ts=4.656265999",
			evNone,
		},
		{
			"metadata parse is noise",
			"[2026-07-09 23:21:57] [ SPAM] [          (3328)]     fifo: parse_mass_item:Anlæg:Parsed Music Assistant metadata key='TITLE' value='Usseewa'",
			evNone,
		},
		{
			"buffer underrun warning",
			"[2026-07-09 23:15:50] [ WARN] [          (1440)]     fifo: play:Anlæg output buffer low: put delay detected",
			evBufferLow,
		},
		{
			"keepalive is noise",
			"[2026-07-09 23:16:12] [DEBUG] [          (4880)]  airplay: keep_alive: Sending POST /feedback to 'Anlæg'",
			evNone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyStderrLine(tc.line); got != tc.want {
				t.Errorf("classifyStderrLine(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestTxtArg(t *testing.T) {
	got := txtArg([]string{"deviceid=18:58:80:46:9E:BB", "manufacturer=Yamaha Corporation"})
	want := `"deviceid=18:58:80:46:9E:BB" "manufacturer=Yamaha Corporation"`
	if got != want {
		t.Errorf("txtArg = %s, want %s", got, want)
	}
}

func TestDeviceFromEntryRequiresDeviceID(t *testing.T) {
	// Entries without deviceid= must be dropped: cliap2 rejects them and
	// then silently plays to nowhere (the run-1 failure mode).
	if _, ok := deviceFromEntry(nil); ok {
		t.Fatal("nil entry accepted")
	}
}

func TestClampVolume(t *testing.T) {
	for in, want := range map[int]int{-5: 0, 0: 0, 30: 30, 100: 100, 130: 100} {
		if got := clampVolume(in); got != want {
			t.Errorf("clampVolume(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestDNSUnescape(t *testing.T) {
	for in, want := range map[string]string{
		`Anl\195\166g`:       "Anlæg",
		`Sovev\195\166relse`: "Soveværelse",
		"JBL":                "JBL",
		`with\ space`:        "with space",
		`trailing\`:          `trailing\`,
	} {
		if got := dnsUnescape(in); got != want {
			t.Errorf("dnsUnescape(%q) = %q, want %q", in, got, want)
		}
	}
}
