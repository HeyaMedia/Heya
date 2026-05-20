package server

import (
	"net/http"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/vfs"
)

type scheduleEntry struct {
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name"`
	MediaType   string `json:"media_type"`
	Type        string `json:"type"`
	Interval    string `json:"interval"`
	IntervalSec int    `json:"interval_sec"`
}

func handleListSchedules(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		q := sqlc.New(app.DB)
		libs, err := q.ListLibraries(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		entries := []scheduleEntry{}

		for _, lib := range libs {
			settings := metadata.ParseSettings(lib.Settings)

			if settings.Watch {
				hasSMB := false
				for _, p := range lib.Paths {
					if vfs.IsSMBPath(p) {
						hasSMB = true
						break
					}
				}
				if hasSMB {
					interval := time.Hour
					if lib.ScanInterval.Valid {
						interval = time.Duration(lib.ScanInterval.Microseconds) * time.Microsecond
					}
					entries = append(entries, scheduleEntry{
						LibraryID:   lib.ID,
						LibraryName: lib.Name,
						MediaType:   string(lib.MediaType),
						Type:        "scan",
						Interval:    formatDuration(interval),
						IntervalSec: int(interval.Seconds()),
					})
				}
			}

			if settings.MetadataRefreshDays > 0 {
				interval := time.Duration(settings.MetadataRefreshDays) * 24 * time.Hour
				entries = append(entries, scheduleEntry{
					LibraryID:   lib.ID,
					LibraryName: lib.Name,
					MediaType:   string(lib.MediaType),
					Type:        "metadata_refresh",
					Interval:    formatDuration(interval),
					IntervalSec: int(interval.Seconds()),
				})
			}
		}

		writeJSON(w, http.StatusOK, entries)
	}
}

func formatDuration(d time.Duration) string {
	if d >= 24*time.Hour {
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day"
		}
		return time.Duration(days).String()[:0] + formatInt(days) + " days"
	}
	if d >= time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1 hour"
		}
		return formatInt(h) + " hours"
	}
	m := int(d.Minutes())
	if m == 1 {
		return "1 minute"
	}
	return formatInt(m) + " minutes"
}

func formatInt(n int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}
