package server

import (
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
	if ts := app.TextSearcher(); ts != nil {
		out["text_searcher"] = map[string]any{
			"ready": ts.Ready(),
		}
	}
	return out
}
