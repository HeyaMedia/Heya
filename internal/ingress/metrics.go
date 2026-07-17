package ingress

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	dto "github.com/prometheus/client_model/go"
)

type metricSample struct {
	at         time.Time
	generation uint64
	requests   uint64
	errors     uint64
	byIngress  map[string]sampleCounters
}

type sampleCounters struct {
	requests uint64
	errors   uint64
}

type metricAggregate struct {
	requests uint64
	inFlight float64
	errors   uint64
	bytesIn  float64
	bytesOut float64
	buckets  map[float64]uint64
	count    uint64
}

func (m *Manager) observeProtocol(ingress string, major int) {
	if ingress == "" {
		ingress = "unknown"
	}
	m.protocolMu.Lock()
	stats := m.protocols[ingress]
	switch major {
	case 1:
		stats.HTTP1++
	case 2:
		stats.HTTP2++
	case 3:
		stats.HTTP3++
	}
	m.protocols[ingress] = stats
	m.protocolMu.Unlock()
}

// Status returns a coherent topology snapshot and a freshly gathered metrics
// view. Gathering is local and lock-free inside Prometheus; no Caddy admin or
// HTTP metrics endpoint is exposed.
func (m *Manager) Status() IngressStatus {
	m.mu.RLock()
	status := IngressStatus{
		Running:         m.running,
		StartedAt:       m.startedAt,
		Generation:      m.generation,
		LastReloadAt:    m.lastReload,
		LastReloadError: m.lastError,
		Listeners:       append([]ListenerStatus(nil), m.listeners...),
		RecentEvents:    append([]Event(nil), m.events...),
		UpdatedAt:       time.Now().UTC(),
	}
	host := m.host
	remote := m.remote
	tailnet := m.tailnet
	registry := m.registries[m.generation]
	m.mu.RUnlock()

	status.Version, _ = caddy.Version()
	if !status.StartedAt.IsZero() {
		status.UptimeSeconds = int64(time.Since(status.StartedAt).Seconds())
	}
	root := filepath.Join(host.DataDir, "caddy", "pki", "authorities", "local", "root.crt")
	if _, err := os.Stat(root); err == nil {
		status.LocalCARoot = root
	}
	if host.HTTPS {
		_, defaultSNI := localCertificateSubjects(host)
		status.Certificates = append(status.Certificates, CertificateStatus{
			Name: "local", Source: "caddy-internal", Subject: defaultSNI,
		})
	}
	if remote != nil {
		subject := remote.DefaultSNI
		if len(remote.Names) > 0 {
			subject = remote.Names[0]
		}
		status.Certificates = append(status.Certificates, CertificateStatus{
			Name: "remote", Source: remote.CertificateMode, Subject: subject,
		})
	}
	if tailnet != nil && tailnet.HTTPS {
		status.Certificates = append(status.Certificates, CertificateStatus{
			Name: "tailnet", Source: "tailscale", Subject: tailnet.CertDomain,
		})
	}

	status.HTTP, status.ByIngress = m.gatherHTTPMetrics(registry, status.Generation)
	return status
}

func (m *Manager) gatherHTTPMetrics(registry prometheusGatherer, generation uint64) (HTTPMetrics, []IngressMetrics) {
	if registry == nil {
		return HTTPMetrics{Protocols: m.protocolTotals()}, nil
	}
	families, err := registry.Gather()
	if err != nil {
		return HTTPMetrics{Protocols: m.protocolTotals()}, nil
	}

	total := newMetricAggregate()
	byIngress := map[string]*metricAggregate{}
	for _, family := range families {
		name := family.GetName()
		switch name {
		case "caddy_http_requests_total", "caddy_http_requests_in_flight",
			"caddy_http_request_duration_seconds", "caddy_http_request_size_bytes", "caddy_http_response_size_bytes":
		default:
			continue
		}
		for _, metric := range family.Metric {
			labels := metricLabels(metric)
			server := labels["server"]
			if server == "" {
				server = "unknown"
			}
			agg := byIngress[server]
			if agg == nil {
				agg = newMetricAggregate()
				byIngress[server] = agg
			}
			applyMetric(name, metric, total)
			applyMetric(name, metric, agg)
		}
	}

	protocols := m.protocolSnapshot()
	now := time.Now()
	m.metricMu.Lock()
	previous := m.metricSample
	current := metricSample{
		at: now, generation: generation, requests: total.requests, errors: total.errors,
		byIngress: make(map[string]sampleCounters, len(byIngress)),
	}
	for name, agg := range byIngress {
		current.byIngress[name] = sampleCounters{requests: agg.requests, errors: agg.errors}
	}
	m.metricSample = current
	m.metricMu.Unlock()

	elapsed := now.Sub(previous.at).Seconds()
	requestRate, errorRate := 0.0, 0.0
	// The settings page can issue an automatic fetch and an immediate manual
	// refresh during mount. Ignore sub-second deltas so a handful of bootstrap
	// requests do not briefly render as hundreds of requests per second.
	if elapsed >= 1 && previous.generation == generation {
		requestRate = counterRate(total.requests, previous.requests, elapsed)
		errorRate = counterRate(total.errors, previous.errors, elapsed)
	}

	httpMetrics := HTTPMetrics{
		RequestsTotal: total.requests, RequestsPerSecond: requestRate, RequestsInFlight: total.inFlight,
		ErrorsTotal: total.errors, ErrorsPerSecond: errorRate,
		P50LatencyMS:  histogramQuantile(total, 0.50) * 1000,
		P95LatencyMS:  histogramQuantile(total, 0.95) * 1000,
		BytesReceived: uint64(math.Max(0, total.bytesIn)), BytesSent: uint64(math.Max(0, total.bytesOut)),
		Protocols: m.protocolTotalsFrom(protocols),
	}

	names := make([]string, 0, len(byIngress))
	for name := range byIngress {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]IngressMetrics, 0, len(names))
	for _, name := range names {
		agg := byIngress[name]
		rate := 0.0
		if elapsed >= 1 && previous.generation == generation {
			prev := previous.byIngress[name]
			rate = counterRate(agg.requests, prev.requests, elapsed)
		}
		result = append(result, IngressMetrics{
			Name: name, RequestsTotal: agg.requests, RequestsPerSecond: rate,
			RequestsInFlight: agg.inFlight, ErrorsTotal: agg.errors,
			P95LatencyMS: histogramQuantile(agg, 0.95) * 1000,
			BytesSent:    uint64(math.Max(0, agg.bytesOut)), Protocols: protocols[name],
		})
	}
	return httpMetrics, result
}

// prometheusGatherer is the small registry surface needed by Status and makes
// metric aggregation directly unit-testable.
type prometheusGatherer interface {
	Gather() ([]*dto.MetricFamily, error)
}

func newMetricAggregate() *metricAggregate {
	return &metricAggregate{buckets: make(map[float64]uint64)}
}

func applyMetric(name string, metric *dto.Metric, agg *metricAggregate) {
	switch name {
	case "caddy_http_requests_total":
		agg.requests += uint64(math.Max(0, metric.GetCounter().GetValue()))
	case "caddy_http_requests_in_flight":
		agg.inFlight += metric.GetGauge().GetValue()
	case "caddy_http_request_duration_seconds":
		h := metric.GetHistogram()
		agg.count += h.GetSampleCount()
		if strings.HasPrefix(metricLabel(metric, "code"), "5") {
			agg.errors += h.GetSampleCount()
		}
		for _, bucket := range h.Bucket {
			agg.buckets[bucket.GetUpperBound()] += bucket.GetCumulativeCount()
		}
	case "caddy_http_request_size_bytes":
		agg.bytesIn += metric.GetHistogram().GetSampleSum()
	case "caddy_http_response_size_bytes":
		agg.bytesOut += metric.GetHistogram().GetSampleSum()
	}
}

func metricLabel(metric *dto.Metric, name string) string {
	for _, pair := range metric.Label {
		if pair.GetName() == name {
			return pair.GetValue()
		}
	}
	return ""
}

func metricLabels(metric *dto.Metric) map[string]string {
	out := make(map[string]string, len(metric.Label))
	for _, pair := range metric.Label {
		out[pair.GetName()] = pair.GetValue()
	}
	return out
}

func histogramQuantile(agg *metricAggregate, quantile float64) float64 {
	if agg == nil || agg.count == 0 || len(agg.buckets) == 0 {
		return 0
	}
	bounds := make([]float64, 0, len(agg.buckets))
	for bound := range agg.buckets {
		bounds = append(bounds, bound)
	}
	sort.Float64s(bounds)
	target := uint64(math.Ceil(float64(agg.count) * quantile))
	for _, bound := range bounds {
		if agg.buckets[bound] >= target {
			if math.IsInf(bound, 1) {
				return 0
			}
			return bound
		}
	}
	return 0
}

func counterRate(current, previous uint64, elapsed float64) float64 {
	if elapsed <= 0 || current < previous {
		return 0
	}
	return float64(current-previous) / elapsed
}

func (m *Manager) protocolSnapshot() map[string]ProtocolStats {
	m.protocolMu.Lock()
	defer m.protocolMu.Unlock()
	out := make(map[string]ProtocolStats, len(m.protocols))
	for name, stats := range m.protocols {
		out[name] = stats
	}
	return out
}

func (m *Manager) protocolTotals() ProtocolStats {
	return m.protocolTotalsFrom(m.protocolSnapshot())
}

func (m *Manager) protocolTotalsFrom(values map[string]ProtocolStats) ProtocolStats {
	var total ProtocolStats
	for _, stats := range values {
		total.HTTP1 += stats.HTTP1
		total.HTTP2 += stats.HTTP2
		total.HTTP3 += stats.HTTP3
	}
	return total
}
