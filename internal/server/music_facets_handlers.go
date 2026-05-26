package server

import (
	"context"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sonicanalysis"
)

// collectSonicAnalysisStatus snapshots the model fetcher + analyzer lifecycle
// for the admin UI ("downloading models", "ready", "analyzing N tracks", etc.).
// Invoked by the Huma operation registered in admin_huma.go.
func collectSonicAnalysisStatus(app *service.App) map[string]any {
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
	// the standalone analyzer above. Its state covers refcount and
	// idle-unload scheduling, which is what the diagnostic panel cares
	// about ("model loaded for N seconds, will unload at HH:MM").
	if h := app.SonicHolder(); h != nil {
		st := h.Status()
		holderInfo := map[string]any{
			"state":            st.State.String(),
			"accelerator":      string(st.Accelerator),
			"refs":             st.Refs,
			"idle_timeout_sec": st.IdleTimeoutSec,
			"total_borrows":    st.TotalBorrows,
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
	if ts := app.TextSearcher(); ts != nil {
		out["text_searcher"] = map[string]any{
			"ready": ts.Ready(),
		}
	}
	// Library coverage — tracks analyzed at the current AnalyzerVersion
	// vs tracks still pending. Mirrors the count surfaced on the Tasks
	// page so the two views agree.
	q := sqlc.New(app.DBPool())
	ctx := context.Background()
	analyzed, _ := q.CountAnalyzedTracks(ctx, sonicanalysis.AnalyzerVersion)
	pending, _ := q.CountPendingAnalysis(ctx, sqlc.CountPendingAnalysisParams{
		MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
		AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
	})
	out["coverage"] = map[string]any{
		"analyzed": analyzed,
		"pending":  pending,
	}
	return out
}
