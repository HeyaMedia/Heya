package diagnostics

import (
	"runtime"
	"time"
)

// CPUUsage reports conventional process CPU (one fully occupied logical core
// is 100%, so a multithreaded process may exceed 100%) and the whole-host
// signal exposed by the platform. Linux reports CPU utilization; macOS reports
// one-minute load normalized by logical CPU count.
type CPUUsage struct {
	ProcessPercent float64 `json:"process_percent"`
	HostPercent    float64 `json:"host_percent"`
	HostAvailable  bool    `json:"host_available"`
	HostMetric     string  `json:"host_metric"`
}

type hostCPUSample struct {
	busy          float64
	total         float64
	available     bool
	instantaneous bool
	metric        string
}

type cpuSample struct {
	at             time.Time
	processSeconds float64
	hostBusy       float64
	hostTotal      float64
	hostAvailable  bool
}

// CPUUsage samples monotonic process and host counters. The first call only
// establishes a baseline, so callers should retain the previous published
// value until the next poll or heartbeat.
func (c *Collector) CPUUsage() CPUUsage {
	if c == nil {
		return CPUUsage{}
	}
	now := time.Now()
	processSeconds, processAvailable := readProcessCPUSeconds()
	host := readHostCPU()

	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.cpuPrevious
	wallSeconds := now.Sub(previous.at).Seconds()
	if !previous.at.IsZero() && wallSeconds > 0 {
		if processAvailable && processSeconds >= previous.processSeconds {
			c.cpuUsage.ProcessPercent = clampProcessPercent((processSeconds-previous.processSeconds)/wallSeconds*100, runtime.NumCPU())
		}
		if host.available && !host.instantaneous && previous.hostAvailable && host.total > previous.hostTotal && host.busy >= previous.hostBusy {
			c.cpuUsage.HostPercent = clampPercent((host.busy - previous.hostBusy) / (host.total - previous.hostTotal) * 100)
			c.cpuUsage.HostAvailable = true
			c.cpuUsage.HostMetric = host.metric
		}
	}
	if host.available && host.instantaneous && host.total > 0 {
		c.cpuUsage.HostPercent = clampPercent(host.busy / host.total * 100)
		c.cpuUsage.HostAvailable = true
		c.cpuUsage.HostMetric = host.metric
	}
	c.cpuPrevious = cpuSample{
		at: now, processSeconds: processSeconds, hostBusy: host.busy,
		hostTotal: host.total, hostAvailable: host.available,
	}
	return c.cpuUsage
}

// CPUPercent remains as the process-only compatibility accessor.
func (c *Collector) CPUPercent() float64 { return c.CPUUsage().ProcessPercent }

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func clampProcessPercent(value float64, numCPU int) float64 {
	if value < 0 {
		return 0
	}
	if numCPU < 1 {
		numCPU = 1
	}
	max := float64(numCPU) * 100
	if value > max {
		return max
	}
	return value
}
