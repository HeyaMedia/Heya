// Package metadatasync contains the local, transactional writers for
// independently refreshed HeyaMetadata projections.
package metadatasync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

const ArtistTopTracksScope = "top_tracks"

type topTrackRow struct {
	Rank              int    `json:"rank"`
	Provider          string `json:"provider"`
	ProviderRank      int    `json:"provider_rank"`
	Title             string `json:"title"`
	MBID              string `json:"mbid"`
	RecordingEntityID string `json:"recording_entity_id"`
	Playcount         int64  `json:"playcount"`
	Listeners         int64  `json:"listeners"`
	URL               string `json:"url"`
}

// ReplaceArtistTopTracks atomically replaces the ranked projection and
// advances its checkpoint. Callers must pass transaction-scoped queries.
// An authoritative empty slice therefore deletes the old rows and records a
// successful empty projection, while any error rolls both operations back.
func ReplaceArtistTopTracks(
	ctx context.Context,
	q *sqlc.Queries,
	artistID int64,
	entityID uuid.UUID,
	entityKind string,
	projectionVersion int64,
	tracks []metadata.TopTrackEntry,
) error {
	// Serialize every writer for this local binding. In particular, an older
	// full-document refresh must not land after a newer scope job and replace
	// its rows while GREATEST keeps the newer checkpoint.
	binding, err := q.GetMetadataEntityBindingForUpdate(ctx, sqlc.GetMetadataEntityBindingForUpdateParams{
		LocalKind: "artist", LocalID: artistID,
	})
	if err != nil {
		return fmt.Errorf("lock artist metadata binding: %w", err)
	}
	if binding.EntityID != entityID || binding.EntityKind != entityKind {
		return fmt.Errorf("artist metadata binding changed while applying top tracks")
	}
	if binding.ProjectionVersion > projectionVersion {
		return nil
	}
	state, err := q.GetMetadataProjectionState(ctx, sqlc.GetMetadataProjectionStateParams{
		LocalKind: "artist", LocalID: artistID, Scope: ArtistTopTracksScope,
	})
	if err == nil && state.EntityID == entityID && state.ProjectionVersion > projectionVersion {
		return nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read artist top-tracks checkpoint: %w", err)
	}

	rows := make([]topTrackRow, 0, len(tracks))
	for i, track := range tracks {
		rows = append(rows, topTrackRow{
			Rank: i + 1, Provider: track.Provider, ProviderRank: track.Rank,
			Title: track.Title, MBID: track.MBID,
			RecordingEntityID: track.RecordingEntityID,
			Playcount:         track.Playcount, Listeners: track.Listeners, URL: track.URL,
		})
	}
	body, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("encode artist top tracks: %w", err)
	}
	if err := q.DeleteArtistTopTracks(ctx, artistID); err != nil {
		return fmt.Errorf("delete artist top tracks: %w", err)
	}
	if err := q.InsertArtistTopTracks(ctx, sqlc.InsertArtistTopTracksParams{ArtistID: artistID, Tracks: body}); err != nil {
		return fmt.Errorf("insert artist top tracks: %w", err)
	}
	if _, err := q.UpsertMetadataProjectionState(ctx, sqlc.UpsertMetadataProjectionStateParams{
		LocalKind: "artist", LocalID: artistID, Scope: ArtistTopTracksScope,
		EntityID: entityID, EntityKind: entityKind, ProjectionVersion: projectionVersion,
	}); err != nil {
		return fmt.Errorf("checkpoint artist top tracks: %w", err)
	}
	return nil
}
