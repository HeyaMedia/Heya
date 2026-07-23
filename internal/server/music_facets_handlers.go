package server

import (
	"context"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sonicanalysis"
)

// collectSonicAnalysisStatus snapshots the model fetcher + analyzer lifecycle
// for the admin UI ("downloading models", "ready", "analyzing N tracks", etc.).
// Invoked by the Huma operation registered in admin_huma.go.
func collectSonicAnalysisStatus(ctx context.Context, app *service.App) map[string]any {
	out := map[string]any{
		"accelerators":     sonicanalysis.AvailableAccelerators(),
		"analyzer_version": sonicanalysis.AnalyzerVersion,
	}
	if f := app.ModelFetcher(); f != nil {
		fetcher := map[string]any{
			"state":         f.State().String(),
			"all_present":   f.AllPresent(),
			"missing_count": f.MissingCount(),
			"total_count":   len(f.Manifest()),
			"total_size":    f.TotalSize(),
			"manifest":      f.Manifest(),
		}
		if p := f.Progress(); p != nil {
			fetcher["progress"] = map[string]any{
				"current_file": p.CurrentFile,
				"bytes_done":   p.BytesDone,
				"bytes_total":  p.BytesTotal,
				"files_done":   p.FilesDone,
				"files_total":  p.FilesTotal,
				"started_at":   p.StartedAt.UTC().Format("2006-01-02T15:04:05Z"),
			}
		}
		if err := f.LastError(); err != nil {
			fetcher["last_error"] = err.Error()
		}
		out["fetcher"] = fetcher
	}
	if a := app.SonicAnalyzer(); a != nil {
		out["analyzer"] = map[string]any{
			"state": a.State().String(),
		}
	}
	// Holder is the per-track-worker model lessor — separate object from
	// the standalone analyzer above. The model actually loads inside the
	// dedicated worker process, so prefer its published heartbeat
	// (runtime.sonic.status) over this process's own never-borrowed
	// Holder — otherwise the panel reports "cold" forever while the
	// worker's GPU is busy. Fall back to local state when the worker is
	// offline or the snapshot is stale.
	now := time.Now()
	var holderSt *sonicanalysis.Status
	holderSource := "local"
	if h := app.SonicHolder(); h != nil {
		st := h.Status()
		holderSt = &st
	}
	if rt, err := app.SonicRuntimeStatus(ctx); err == nil && rt.Fresh(now) {
		holderSt = &rt.Holder
		holderSource = "worker"
		if rt.CurrentItem != "" {
			out["current_item"] = rt.CurrentItem
		}
		if rt.CurrentStage != "" {
			out["current_stage"] = rt.CurrentStage
		}
	}
	if holderSt != nil {
		st := *holderSt
		holderInfo := map[string]any{
			"state":            st.State.String(),
			"accelerator":      string(st.Accelerator),
			"refs":             st.Refs,
			"idle_timeout_sec": st.IdleTimeoutSec,
			"total_borrows":    st.TotalBorrows,
			"preprocess_ahead": st.PreprocessAhead,
			"gpu_workers":      st.GPUWorkers,
			"pipeline_workers": st.PipelineWorkers,
			"source":           holderSource,
		}
		if st.LoadedAt != nil {
			holderInfo["loaded_at"] = st.LoadedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if st.IdleUnloadAt != nil {
			holderInfo["idle_unload_at"] = st.IdleUnloadAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if st.LastBorrowAt != nil {
			holderInfo["last_borrow_at"] = st.LastBorrowAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		out["holder"] = holderInfo
	}
	if ws, err := app.WorkerRuntimeStatus(ctx); err == nil {
		out["worker_online"] = ws.Online(now)
	}
	if ts := app.TextSearcher(); ts != nil {
		out["text_searcher"] = map[string]any{
			"ready": ts.Ready(),
		}
	}
	// Library coverage — tracks analyzed at the current AnalyzerVersion
	// vs tracks still pending. Mirrors the count surfaced on the Tasks
	// page so the two views agree.
	q := sqlc.New(app.DBPool())
	analyzed, _ := q.CountAnalyzedTracks(ctx, sonicanalysis.AnalyzerVersion)
	pending, _ := q.CountPendingAnalysis(ctx, sqlc.CountPendingAnalysisParams{
		MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
		AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
	})
	out["coverage"] = map[string]any{
		"analyzed": analyzed,
		"pending":  pending,
	}
	// Hourly throughput over the trailing 24h for the dashboard graph.
	// The query returns sparse buckets; fill the gaps so the frontend
	// can plot a fixed-width series without date math.
	if rows, err := q.SonicAnalysisThroughput(ctx, 24); err == nil {
		counts := make(map[time.Time]int32, len(rows))
		for _, r := range rows {
			if r.Bucket.Valid {
				counts[r.Bucket.Time.UTC()] = r.Analyzed
			}
		}
		start := now.UTC().Truncate(time.Hour).Add(-23 * time.Hour)
		buckets := make([]map[string]any, 0, 24)
		var lastHour, last24 int32
		for i := range 24 {
			h := start.Add(time.Duration(i) * time.Hour)
			c := counts[h]
			last24 += c
			if i == 23 {
				lastHour = c
			}
			buckets = append(buckets, map[string]any{
				"hour":  h.Format(time.RFC3339),
				"count": c,
			})
		}
		out["throughput"] = map[string]any{
			"last_hour": lastHour,
			"last_24h":  last24,
			"buckets":   buckets,
		}
	}
	return out
}
