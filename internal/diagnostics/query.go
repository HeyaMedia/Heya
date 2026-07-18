// Package diagnostics contains the bounded, in-process telemetry used by the
// admin diagnostics dashboard. It deliberately keeps only aggregate query
// data: arguments are never captured and SQL text is normalized/redacted.
package diagnostics

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	defaultMaxStatements = 256
	defaultRecentSamples = 1024
	recentWindow         = time.Minute
)

var (
	quotedSQLLiteral  = regexp.MustCompile(`'(?:''|[^'])*'`)
	numericSQLLiteral = regexp.MustCompile(`(^|[^$[:alnum:]_])(\d+(?:\.\d+)?)\b`)
)

// QueryStatement is one normalized query aggregate since this process
// started. Rows is pgx's affected-row count and may be zero for SELECTs whose
// command tag does not report a useful value.
type QueryStatement struct {
	Statement       string    `json:"statement"`
	Calls           uint64    `json:"calls"`
	Errors          uint64    `json:"errors"`
	Rows            int64     `json:"rows"`
	TotalDurationMS float64   `json:"total_duration_ms"`
	AverageMS       float64   `json:"average_ms"`
	MaxMS           float64   `json:"max_ms"`
	RecentCalls     uint64    `json:"recent_calls"`
	RecentErrors    uint64    `json:"recent_errors"`
	RecentAverageMS float64   `json:"recent_average_ms"`
	RecentP95MS     float64   `json:"recent_p95_ms"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	LastErrorAt     time.Time `json:"last_error_at,omitempty"`
	LastErrorCode   string    `json:"last_error_code,omitempty"`
}

// QuerySnapshot is a cheap copy of the API process's query telemetry. The
// one-minute percentiles are calculated from a bounded sample ring, while top
// statements are lifetime aggregates and remain useful during quiet periods.
type QuerySnapshot struct {
	StartedAt         time.Time        `json:"started_at"`
	WindowSeconds     int              `json:"window_seconds"`
	TotalQueries      uint64           `json:"total_queries"`
	TotalErrors       uint64           `json:"total_errors"`
	InFlight          int64            `json:"in_flight"`
	QueriesPerSecond  float64          `json:"queries_per_second"`
	AverageMS         float64          `json:"average_ms"`
	P50MS             float64          `json:"p50_ms"`
	P95MS             float64          `json:"p95_ms"`
	MaxMS             float64          `json:"max_ms"`
	RecentErrors      uint64           `json:"recent_errors"`
	TrackedStatements int              `json:"tracked_statements"`
	TopStatements     []QueryStatement `json:"top_statements"`
}

type queryAggregate struct {
	calls         uint64
	errors        uint64
	rows          int64
	total         time.Duration
	max           time.Duration
	lastSeenAt    time.Time
	lastErrorAt   time.Time
	lastErrorCode string
}

type querySample struct {
	at        time.Time
	duration  time.Duration
	err       bool
	statement string
}

type queryTraceContext struct {
	started   time.Time
	statement string
}

// Collector implements pgx.QueryTracer and also owns the tiny amount of
// state required to calculate process CPU utilization between dashboard
// samples. The query map and sample ring are hard-bounded.
type Collector struct {
	mu            sync.Mutex
	startedAt     time.Time
	maxStatements int
	maxSamples    int
	statements    map[string]*queryAggregate
	samples       []querySample
	samplePos     int
	sampleFull    bool
	totalQueries  uint64
	totalErrors   uint64
	inFlight      int64
	cpuPrevious   cpuSample
	cpuUsage      CPUUsage
}

func NewCollector() *Collector {
	return newCollector(defaultMaxStatements, defaultRecentSamples)
}

func newCollector(maxStatements, maxSamples int) *Collector {
	if maxStatements < 1 {
		maxStatements = 1
	}
	if maxSamples < 1 {
		maxSamples = 1
	}
	return &Collector{
		startedAt:     time.Now(),
		maxStatements: maxStatements,
		maxSamples:    maxSamples,
		statements:    make(map[string]*queryAggregate, maxStatements),
		samples:       make([]querySample, maxSamples),
	}
}

func (c *Collector) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if c == nil || queryTraceSuppressed(ctx) {
		return ctx
	}
	statement := SanitizeStatement(data.SQL)
	c.mu.Lock()
	c.inFlight++
	c.mu.Unlock()
	return context.WithValue(ctx, queryTraceKey{}, queryTraceContext{started: time.Now(), statement: statement})
}

func (c *Collector) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if c == nil {
		return
	}
	trace, ok := ctx.Value(queryTraceKey{}).(queryTraceContext)
	if !ok {
		return
	}
	duration := time.Since(trace.started)
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inFlight > 0 {
		c.inFlight--
	}
	c.totalQueries++
	if data.Err != nil {
		c.totalErrors++
	}

	statement := trace.statement
	if _, exists := c.statements[statement]; !exists && len(c.statements) >= c.maxStatements-1 {
		statement = "other statements"
	}
	agg := c.statements[statement]
	if agg == nil {
		agg = &queryAggregate{}
		c.statements[statement] = agg
	}
	agg.calls++
	agg.total += duration
	agg.lastSeenAt = now
	if duration > agg.max {
		agg.max = duration
	}
	if data.Err != nil {
		agg.errors++
		agg.lastErrorAt = now
		agg.lastErrorCode = queryErrorCode(data.Err)
	}
	if data.CommandTag.RowsAffected() > 0 {
		agg.rows += data.CommandTag.RowsAffected()
	}

	c.samples[c.samplePos] = querySample{at: now, duration: duration, err: data.Err != nil, statement: statement}
	c.samplePos = (c.samplePos + 1) % c.maxSamples
	if c.samplePos == 0 {
		c.sampleFull = true
	}
}

type queryTraceKey struct{}
type suppressQueryTraceKey struct{}

// WithoutQueryTrace marks observability probes so opening Diagnostics does not
// make its own polling queries look like application hot spots.
func WithoutQueryTrace(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressQueryTraceKey{}, true)
}

func queryTraceSuppressed(ctx context.Context) bool {
	suppressed, _ := ctx.Value(suppressQueryTraceKey{}).(bool)
	return suppressed
}

// Snapshot returns the one-minute query summary plus the statements consuming
// the most cumulative database time. The SQL is already sanitized on insert.
func (c *Collector) Snapshot() QuerySnapshot {
	if c == nil {
		return QuerySnapshot{WindowSeconds: int(recentWindow.Seconds()), TopStatements: []QueryStatement{}}
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	out := QuerySnapshot{
		StartedAt:         c.startedAt,
		WindowSeconds:     int(recentWindow.Seconds()),
		TotalQueries:      c.totalQueries,
		TotalErrors:       c.totalErrors,
		InFlight:          c.inFlight,
		TrackedStatements: len(c.statements),
		TopStatements:     make([]QueryStatement, 0, len(c.statements)),
	}
	recentByStatement := make(map[string]*queryRecentAggregate, len(c.statements))
	cutoff := now.Add(-recentWindow)
	durations := make([]float64, 0, c.maxSamples)
	var total float64
	for _, sample := range c.orderedSamplesLocked() {
		if sample.at.IsZero() || sample.at.Before(cutoff) {
			continue
		}
		ms := durationMS(sample.duration)
		durations = append(durations, ms)
		total += ms
		if sample.err {
			out.RecentErrors++
		}
		recent := recentByStatement[sample.statement]
		if recent == nil {
			recent = &queryRecentAggregate{}
			recentByStatement[sample.statement] = recent
		}
		recent.calls++
		recent.totalMS += ms
		recent.durations = append(recent.durations, ms)
		if sample.err {
			recent.errors++
		}
	}
	for statement, agg := range c.statements {
		average := 0.0
		if agg.calls > 0 {
			average = durationMS(agg.total) / float64(agg.calls)
		}
		row := QueryStatement{
			Statement: statement, Calls: agg.calls, Errors: agg.errors, Rows: agg.rows,
			TotalDurationMS: durationMS(agg.total), AverageMS: average, MaxMS: durationMS(agg.max),
			LastSeenAt: agg.lastSeenAt, LastErrorAt: agg.lastErrorAt, LastErrorCode: agg.lastErrorCode,
		}
		if recent := recentByStatement[statement]; recent != nil {
			row.RecentCalls = recent.calls
			row.RecentErrors = recent.errors
			row.RecentAverageMS = recent.totalMS / float64(recent.calls)
			sort.Float64s(recent.durations)
			row.RecentP95MS = percentile(recent.durations, 0.95)
		}
		out.TopStatements = append(out.TopStatements, row)
	}
	sort.Slice(out.TopStatements, func(i, j int) bool {
		return out.TopStatements[i].TotalDurationMS > out.TopStatements[j].TotalDurationMS
	})
	if len(out.TopStatements) > 12 {
		out.TopStatements = out.TopStatements[:12]
	}

	if len(durations) > 0 {
		elapsed := now.Sub(c.startedAt)
		if elapsed > recentWindow {
			elapsed = recentWindow
		}
		if elapsed < time.Second {
			elapsed = time.Second
		}
		out.QueriesPerSecond = float64(len(durations)) / elapsed.Seconds()
		out.AverageMS = total / float64(len(durations))
		sort.Float64s(durations)
		out.P50MS = percentile(durations, 0.50)
		out.P95MS = percentile(durations, 0.95)
		out.MaxMS = durations[len(durations)-1]
	}
	return out
}

type queryRecentAggregate struct {
	calls     uint64
	errors    uint64
	totalMS   float64
	durations []float64
}

func (c *Collector) orderedSamplesLocked() []querySample {
	if !c.sampleFull {
		return c.samples[:c.samplePos]
	}
	out := make([]querySample, 0, len(c.samples))
	out = append(out, c.samples[c.samplePos:]...)
	out = append(out, c.samples[:c.samplePos]...)
	return out
}

// SanitizeStatement collapses whitespace and replaces literal strings and
// numbers. pgx arguments are never part of SQL text, but dynamic statements
// occasionally contain literals; redacting them keeps diagnostics paste-safe.
func SanitizeStatement(sql string) string {
	sql = strings.Join(strings.Fields(sql), " ")
	sql = quotedSQLLiteral.ReplaceAllString(sql, "?")
	sql = numericSQLLiteral.ReplaceAllString(sql, "${1}?")
	if len(sql) > 280 {
		sql = sql[:277] + "..."
	}
	if sql == "" {
		return "unknown statement"
	}
	return sql
}

func queryErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code != "" {
		return pgErr.Code
	}
	return "query_error"
}

func percentile(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	idx := int(float64(len(values)-1) * quantile)
	if idx < 0 {
		idx = 0
	} else if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}

func durationMS(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}
