//go:build darwin

package diagnostics

import (
	"encoding/binary"
	"runtime"

	"golang.org/x/sys/unix"
)

func readHostCPU() hostCPUSample {
	// macOS does not expose Linux-style aggregate CPU tick counters through a
	// stable sysctl. vm.loadavg is stable and directly represents whole-system
	// runnable pressure, so report its one-minute value normalized by logical
	// CPU count. This is deliberately labelled load_average_1m in the API.
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil || len(raw) < 20 {
		return hostCPUSample{}
	}
	load := binary.NativeEndian.Uint32(raw[:4])
	var scale uint64
	if len(raw) >= 24 {
		scale = binary.NativeEndian.Uint64(raw[len(raw)-8:])
	} else {
		scale = uint64(binary.NativeEndian.Uint32(raw[len(raw)-4:]))
	}
	if scale == 0 {
		return hostCPUSample{}
	}
	capacity := runtime.NumCPU()
	if capacity < 1 {
		capacity = 1
	}
	return hostCPUSample{
		busy: float64(load) / float64(scale), total: float64(capacity),
		available: true, instantaneous: true, metric: "load_average_1m",
	}
}
