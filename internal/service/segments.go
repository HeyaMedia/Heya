package service

import (
	"context"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// FileSegment is one stored skip marker for a playable file. end_ms is
// always materialized at ingest (open-ended community markers get the
// file runtime), so consumers never handle a missing end bound.
type FileSegment struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Source  string `json:"source"`
}

// ListFileSegments returns the stored skip segments for a library file,
// ordered by start time. Empty slice (not nil) when none exist — the
// player treats that as "nothing to skip", not an error.
func (a *App) ListFileSegments(ctx context.Context, fileID int64) ([]FileSegment, error) {
	rows, err := sqlc.New(a.db).ListMediaSegmentsForFile(ctx, fileID)
	if err != nil {
		return nil, err
	}
	out := make([]FileSegment, 0, len(rows))
	for _, r := range rows {
		out = append(out, FileSegment{
			ID:      r.ID,
			Type:    r.SegmentType,
			StartMs: r.StartMs,
			EndMs:   r.EndMs,
			Source:  r.Source,
		})
	}
	return out, nil
}
