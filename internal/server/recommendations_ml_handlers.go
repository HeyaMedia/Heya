package server

import (
	"context"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sonicanalysis"
)

// collectRecommendationsMLStatus assembles the open-shape status blob the
// Recommendations-Engine settings page polls: settings + env locks + model
// download state + embedding-backfill progress.
func collectRecommendationsMLStatus(ctx context.Context, app *service.App) map[string]any {
	settings := app.RecommendationsMLSettings(ctx)
	embedded, total := app.EmbeddedVideoCount(ctx)
	epEmbedded, epTotal := app.EmbeddedEpisodeCount(ctx)
	enabledLock, accelLock := app.RecommendationsMLEnvLock()

	out := map[string]any{
		"accelerators":      sonicanalysis.AvailableAccelerators(),
		"enabled":           settings.Enabled,
		"accelerator":       settings.Accelerator,
		"env_locks":         map[string]string{"enabled": enabledLock, "accelerator": accelLock},
		"embedded":          embedded,
		"total":             total,
		"embedded_episodes": epEmbedded,
		"total_episodes":    epTotal,
		"model":             "BGE-large-en-v1.5",
		"dimensions":        1024,
	}
	if f := app.RecFetcher(); f != nil {
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
			}
		}
		if err := f.LastError(); err != nil {
			fetcher["last_error"] = err.Error()
		}
		out["fetcher"] = fetcher
	}
	return out
}
