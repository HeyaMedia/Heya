package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/diagnostics"
	"github.com/karbowiak/heya/internal/service"
)

type adminWorkersBody struct {
	GeneratedAt  time.Time                   `json:"generated_at"`
	Online       bool                        `json:"online"`
	Status       service.WorkerRuntimeStatus `json:"status"`
	QueueSummary []service.JobSummaryRow     `json:"queue_summary"`
	ActiveJobs   []service.JobRow            `json:"active_jobs"`
	RecentJobs   []service.JobRow            `json:"recent_jobs"`
	Error        string                      `json:"error,omitempty"`
}

func registerAdminWorkerRoutes(api huma.API, app *service.App) {
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/workers", "admin-workers", "Worker process, queue, and recent activity diagnostics", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[adminWorkersBody], error) {
			return noStoreJSON(collectAdminWorkers(ctx, app)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/admin/processes/restart", "admin-restart-processes", "Gracefully restart the server, worker, or both", "Admin")),
		func(ctx context.Context, in *struct {
			Body struct {
				Target string `json:"target" enum:"server,worker,all" doc:"Process target supervised by Kubernetes, Compose, or AIO supervisord"`
			}
		}) (*JSONOutput[service.ProcessRestartResult], error) {
			result, err := app.RequestProcessRestart(ctx, in.Body.Target)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusServiceUnavailable)
			}
			return noStoreJSON(result), nil
		})
}

func collectAdminWorkers(ctx context.Context, app *service.App) adminWorkersBody {
	ctx = diagnostics.WithoutQueryTrace(ctx)
	body := adminWorkersBody{
		GeneratedAt: time.Now().UTC(), QueueSummary: []service.JobSummaryRow{},
		ActiveJobs: []service.JobRow{}, RecentJobs: []service.JobRow{},
	}
	if app == nil || app.DBPool() == nil {
		body.Error = "database unavailable"
		return body
	}

	status, err := app.WorkerRuntimeStatus(ctx)
	if err != nil {
		body.Error = err.Error()
	} else {
		body.Status = status
		body.Online = status.Online(time.Now())
	}
	if summary, summaryErr := app.JobSummary(ctx); summaryErr == nil {
		body.QueueSummary = summary
	} else if body.Error == "" {
		body.Error = summaryErr.Error()
	}
	if active, activeErr := app.ListJobs(ctx, "running", "", 100, 0); activeErr == nil {
		body.ActiveJobs = active.Jobs
	} else if body.Error == "" {
		body.Error = activeErr.Error()
	}
	if recent, recentErr := app.ListJobs(ctx, "", "", 30, 0); recentErr == nil {
		body.RecentJobs = recent.Jobs
	} else if body.Error == "" {
		body.Error = recentErr.Error()
	}
	return body
}
