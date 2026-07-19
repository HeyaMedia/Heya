package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	heyaMetadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"
	"github.com/stretchr/testify/require"
)

type pagedMetadataChangeSource struct {
	pages  map[int64]heyaMetadata.ChangePage
	calls  []int64
	before func(int64) error
}

func (s *pagedMetadataChangeSource) Changes(_ context.Context, after, _ int64, _ string) (heyaMetadata.ChangePage, error) {
	s.calls = append(s.calls, after)
	if s.before != nil {
		if err := s.before(after); err != nil {
			return heyaMetadata.ChangePage{}, err
		}
	}
	page, ok := s.pages[after]
	if !ok {
		return heyaMetadata.ChangePage{}, fmt.Errorf("unexpected metadata change cursor %d", after)
	}
	return page, nil
}

func TestSyncMetadataChangesEnqueuesTrailingRefreshAcrossPages(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	unique := uuid.NewString()
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "metadata-change-pages-" + unique,
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/metadata-change-pages-" + unique},
		ScanInterval: pgtype.Interval{Microseconds: 3_600_000_000, Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool),
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType,
		ProviderKind: "heya", HeyaSlug: "metadata-change-pages-" + unique,
		Title: "Metadata change page target", SortTitle: "Metadata change page target",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)

	entityID := uuid.New()
	_, err = q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "media_item", LocalID: item.ID,
		EntityID: entityID, EntityKind: "movie", SchemaVersion: 1,
	})
	require.NoError(t, err)
	consumer := "metadata-change-pages-" + unique
	streamID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO metadata_change_consumers (consumer, next_cursor, stream_id)
		VALUES ($1, 0, $2)`, consumer, streamID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			DELETE FROM river_job
			WHERE kind = 'enrich_media_item'
			  AND NULLIF(args->>'item_id', '')::bigint = $1`, item.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM metadata_change_consumers WHERE consumer = $1`, consumer)
		_ = q.DeleteMediaItem(context.Background(), item.ID)
		testutil.CleanupLibrary(t, pool, lib.ID)
	})

	// A full first page makes the consumer request a second page. Repeating the
	// same entity is valid: the feed may publish many child/projection changes
	// for one canonical item, and the worker deliberately coalesces them within
	// a page.
	firstEntries := make([]heyaMetadata.Change, metadataChangePageSize)
	for i := range firstEntries {
		firstEntries[i] = heyaMetadata.Change{
			Sequence: int64(i + 1), EntityID: entityID.String(), EntityKind: "movie",
			ChangeType: "updated", ProjectionVersion: int64(i + 1),
		}
	}
	source := &pagedMetadataChangeSource{pages: map[int64]heyaMetadata.ChangePage{
		0: {
			Entries: firstEntries, StreamID: streamID.String(),
			HeadCursor: metadataChangePageSize + 1, NextCursor: metadataChangePageSize,
		},
		metadataChangePageSize: {
			Entries: []heyaMetadata.Change{{
				Sequence: metadataChangePageSize + 1, EntityID: entityID.String(), EntityKind: "movie",
				ChangeType: "updated", ProjectionVersion: metadataChangePageSize + 1,
			}},
			StreamID: streamID.String(), HeadCursor: metadataChangePageSize + 1, NextCursor: metadataChangePageSize + 1,
		},
	}, before: func(after int64) error {
		if after != metadataChangePageSize {
			return nil
		}
		// Model the precise race: the page-one refresh starts before the
		// consumer asks HeyaMetadata for page two. The later invalidation must
		// leave that running job alone and add one trailing refresh.
		_, err := pool.Exec(ctx, `
			UPDATE river_job
			SET state = 'running', attempted_at = now()
			WHERE id = (
				SELECT id FROM river_job
				WHERE kind = 'enrich_media_item'
				  AND NULLIF(args->>'item_id', '')::bigint = $1
				  AND args->>'source' = 'metadata_change'
				ORDER BY id DESC LIMIT 1
			)`, item.ID)
		return err
	}}

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)
	worker := &SyncMetadataChangesWorker{DB: pool, Source: source, Consumer: consumer}
	require.NoError(t, worker.Work(rivertest.WorkContext(ctx, rc), &river.Job[SyncMetadataChangesArgs]{}))
	require.Equal(t, []int64{0, metadataChangePageSize}, source.calls)

	var cursor int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT next_cursor FROM metadata_change_consumers WHERE consumer = $1`, consumer).Scan(&cursor))
	require.EqualValues(t, metadataChangePageSize+1, cursor)

	rows, err := pool.Query(ctx, `
		SELECT state::text
		FROM river_job
		WHERE kind = 'enrich_media_item'
		  AND NULLIF(args->>'item_id', '')::bigint = $1
		  AND args->>'source' = 'metadata_change'
		ORDER BY id`, item.ID)
	require.NoError(t, err)
	defer rows.Close()
	states := make([]string, 0, 2)
	for rows.Next() {
		var state string
		require.NoError(t, rows.Scan(&state))
		states = append(states, state)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"running", "available"}, states,
		"the later page must preserve the running refresh and enqueue a trailing one before its cursor commits")
}

func TestSyncMetadataChangesKeepsProjectionVersionPerChangedScope(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	unique := uuid.NewString()
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "metadata-scope-versions-" + unique, MediaType: sqlc.MediaTypeMusic,
		Paths:        []string{"/media/metadata-scope-versions-" + unique},
		ScanInterval: pgtype.Interval{Microseconds: 3_600_000_000, Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Scope Artist " + unique,
		SortTitle: "scope artist " + unique, ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID: item.ID, Name: item.Title, SortName: item.SortTitle,
	})
	require.NoError(t, err)
	entityID := uuid.New()
	_, err = q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "artist", LocalID: artist.ID, EntityID: entityID,
		EntityKind: "artist", SchemaVersion: 1, ProjectionVersion: 10,
	})
	require.NoError(t, err)
	consumer := "metadata-scope-versions-" + unique
	streamID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO metadata_change_consumers(consumer,next_cursor,stream_id)
		VALUES($1,0,$2)`, consumer, streamID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE kind IN ('enrich_media_item','reconcile_metadata_scope') AND (NULLIF(args->>'local_id','')::bigint=$1 OR NULLIF(args->>'item_id','')::bigint=$2)`, artist.ID, item.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM metadata_change_consumers WHERE consumer=$1`, consumer)
		_, _ = pool.Exec(context.Background(), `DELETE FROM metadata_entity_bindings WHERE local_kind='artist' AND local_id=$1`, artist.ID)
		testutil.CleanupLibrary(t, pool, lib.ID)
	})

	source := &pagedMetadataChangeSource{pages: map[int64]heyaMetadata.ChangePage{
		0: {
			Entries: []heyaMetadata.Change{
				{Sequence: 1, EntityID: entityID.String(), EntityKind: "artist", ChangeType: "updated", ChangedScopes: []string{"top_tracks"}, ProjectionVersion: 8},
				{Sequence: 2, EntityID: entityID.String(), EntityKind: "artist", ChangeType: "updated", ChangedScopes: []string{"biography"}, ProjectionVersion: 10},
			},
			StreamID: streamID.String(), HeadCursor: 2, NextCursor: 2,
		},
	}}
	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)
	worker := &SyncMetadataChangesWorker{DB: pool, Source: source, Consumer: consumer}
	require.NoError(t, worker.Work(rivertest.WorkContext(ctx, rc), &river.Job[SyncMetadataChangesArgs]{}))

	var versions []int64
	rows, err := pool.Query(ctx, `
		SELECT (args->>'projection_version')::bigint
		FROM river_job
		WHERE kind='reconcile_metadata_scope'
		  AND NULLIF(args->>'local_id','')::bigint=$1
		  AND args->>'scope'='top_tracks'
		ORDER BY id`, artist.ID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var version int64
		require.NoError(t, rows.Scan(&version))
		versions = append(versions, version)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int64{8}, versions,
		"an unrelated v10 biography change must not promote a v8 top-tracks snapshot request")
}
