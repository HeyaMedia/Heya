package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/diagnostics"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/ingress"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/service"
)

type adminDiagnosticsBody struct {
	GeneratedAt   time.Time                   `json:"generated_at"`
	Status        string                      `json:"status" enum:"healthy,watching,degraded"`
	Findings      []adminDiagnosticFinding    `json:"findings"`
	System        adminSystemBody             `json:"system"`
	HTTPAvailable bool                        `json:"http_available"`
	HTTP          ingress.HTTPMetrics         `json:"http"`
	Database      adminDBBody                 `json:"database"`
	Queries       diagnostics.QuerySnapshot   `json:"queries"`
	Worker        service.WorkerRuntimeStatus `json:"worker"`
	WorkerOnline  bool                        `json:"worker_online"`
	Logs          adminLogSummary             `json:"logs"`
}

type adminDiagnosticFinding struct {
	Tone    string `json:"tone" enum:"good,warn,bad"`
	Title   string `json:"title"`
	Detail  string `json:"detail"`
	Section string `json:"section" enum:"runtime,traffic,database,queries,logs"`
}

type adminLogSummary struct {
	Buffered     int            `json:"buffered"`
	Capacity     int            `json:"capacity"`
	Counts       map[string]int `json:"counts"`
	Last5Minutes map[string]int `json:"last_5_minutes"`
	LatestAt     time.Time      `json:"latest_at,omitempty"`
	Recent       []logbuf.Entry `json:"recent"`
}

func registerAdminDiagnosticsRoutes(api huma.API, app *service.App, hub *eventhub.Hub, buf *logbuf.RingBuffer) {
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/diagnostics", "admin-diagnostics", "Aggregate runtime, traffic, query, database, and log diagnostics", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[adminDiagnosticsBody], error) {
			return noStoreJSON(collectAdminDiagnostics(ctx, app, hub, buf)), nil
		})
}

func collectAdminDiagnostics(ctx context.Context, app *service.App, hub *eventhub.Hub, buf *logbuf.RingBuffer) adminDiagnosticsBody {
	ctx = diagnostics.WithoutQueryTrace(ctx)
	body := adminDiagnosticsBody{
		GeneratedAt: time.Now().UTC(), Status: "healthy", Findings: []adminDiagnosticFinding{},
		System: collectAdminSystem(app, hub), Database: collectAdminDB(ctx, app),
		Logs: collectLogSummary(buf),
	}
	if app != nil && app.Diagnostics() != nil {
		body.Queries = app.Diagnostics().Snapshot()
	} else {
		body.Queries = diagnostics.QuerySnapshot{WindowSeconds: 60, TopStatements: []diagnostics.QueryStatement{}}
	}
	if app != nil {
		if manager := app.Ingress(); manager != nil {
			status := manager.Status()
			body.HTTPAvailable = status.Running
			body.HTTP = status.HTTP
		}
		if worker, err := app.WorkerRuntimeStatus(ctx); err == nil {
			body.Worker = worker
			body.WorkerOnline = worker.Online(time.Now())
		}
	}
	body.Findings = diagnosticFindings(body)
	for _, finding := range body.Findings {
		if finding.Tone == "bad" {
			body.Status = "degraded"
			break
		}
		if finding.Tone == "warn" {
			body.Status = "watching"
		}
	}
	return body
}

func collectLogSummary(buf *logbuf.RingBuffer) adminLogSummary {
	levels := []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}
	out := adminLogSummary{
		Capacity: 0, Counts: make(map[string]int, len(levels)), Last5Minutes: make(map[string]int, len(levels)),
		Recent: []logbuf.Entry{},
	}
	for _, level := range levels {
		out.Counts[level] = 0
		out.Last5Minutes[level] = 0
	}
	if buf == nil {
		return out
	}
	out.Capacity = buf.Capacity()
	entries := buf.Recent(out.Capacity)
	out.Buffered = len(entries)
	cutoff := time.Now().Add(-5 * time.Minute)
	for _, entry := range entries {
		level := strings.ToLower(entry.Level)
		out.Counts[level]++
		if !entry.Time.Before(cutoff) {
			out.Last5Minutes[level]++
		}
		if level == "warn" || level == "error" || level == "fatal" || level == "panic" {
			out.Recent = append(out.Recent, entry)
			if len(out.Recent) > 8 {
				out.Recent = out.Recent[1:]
			}
		}
	}
	if len(entries) > 0 {
		out.LatestAt = entries[len(entries)-1].Time
	}
	return out
}

func diagnosticFindings(body adminDiagnosticsBody) []adminDiagnosticFinding {
	findings := make([]adminDiagnosticFinding, 0, 8)
	add := func(tone, title, detail, section string) {
		findings = append(findings, adminDiagnosticFinding{Tone: tone, Title: title, Detail: detail, Section: section})
	}

	if body.Database.Error != "" {
		add("bad", "Database probe failed", body.Database.Error, "database")
	} else if body.Database.MaxConnections > 0 {
		used := float64(body.Database.AcquiredConnections) / float64(body.Database.MaxConnections) * 100
		if used >= 90 {
			add("bad", "Database pool nearly exhausted", "More than 90% of the connection pool is allocated.", "database")
		} else if used >= 70 {
			add("warn", "Database pool is busy", "More than 70% of the connection pool is allocated.", "database")
		}
	}
	if !body.WorkerOnline {
		add("warn", "Worker process is offline", "The worker heartbeat is missing, stopped, or more than 30 seconds old.", "runtime")
	}
	if body.Database.WaitingQueries > 0 {
		add("warn", "Database work is waiting", "One or more PostgreSQL sessions currently have a wait event.", "database")
	}
	if body.Database.Deadlocks > 0 {
		add("warn", "PostgreSQL has recorded deadlocks", "The lifetime deadlock counter is non-zero; inspect database activity and logs.", "database")
	}
	if body.Queries.P95MS >= 1000 {
		add("bad", "API queries are very slow", "The one-minute query p95 is above one second.", "queries")
	} else if body.Queries.P95MS >= 250 {
		add("warn", "API query latency is elevated", "The one-minute query p95 is above 250 ms.", "queries")
	}
	if body.HTTPAvailable && body.HTTP.P95LatencyMS >= 2000 {
		add("bad", "Request latency is very high", "Ingress p95 latency is above two seconds.", "traffic")
	} else if body.HTTPAvailable && body.HTTP.P95LatencyMS >= 750 {
		add("warn", "Request latency is elevated", "Ingress p95 latency is above 750 ms.", "traffic")
	}
	if body.Logs.Last5Minutes["error"]+body.Logs.Last5Minutes["fatal"]+body.Logs.Last5Minutes["panic"] > 0 {
		add("warn", "Recent errors in the log", "At least one error-level event was recorded in the last five minutes.", "logs")
	}
	if len(findings) == 0 {
		add("good", "No immediate pressure detected", "Runtime, database, queries, and recent logs are within the dashboard thresholds.", "runtime")
	}
	return findings
}
