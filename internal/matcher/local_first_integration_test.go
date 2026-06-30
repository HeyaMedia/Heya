package matcher

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/stretchr/testify/require"
)

// Phase-0 local-first guards, against a real Postgres in a rolled-back tx:
//   - NFO-less stubs (empty external_ids) must NOT fuse via `external_ids @> '{}'`.
//   - The type-specific upserts fill empty/default columns on first enrich but
//     preserve already-set / user-edited values (fill-only-empty).
//   - TV re-enrich is no longer a silent no-op: it adds new episodes while
//     preserving an edited one.
func TestLocalFirstUpserts(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)
	m := &Matcher{q: qtx}

	user, err := qtx.CreateUser(ctx, sqlc.CreateUserParams{
		Username: "lftest", Email: "lftest@example.com", PasswordHash: "x", IsAdmin: true,
	})
	require.NoError(t, err)
	newLib := func(name string, mt sqlc.MediaType) int64 {
		lib, err := qtx.CreateLibrary(ctx, sqlc.CreateLibraryParams{
			Name: name, MediaType: mt, Paths: []string{"/x"},
			ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
			CreatedBy:    user.ID, Settings: []byte("{}"),
		})
		require.NoError(t, err)
		return lib.ID
	}

	t.Run("materializeLocal creates a visible local movie and dedups on re-scan", func(t *testing.T) {
		lib := newLib("LocalMovies", sqlc.MediaTypeMovie)
		file := sqlc.LibraryFile{ID: 0, Path: "/x/Unknown Film (2020)/Unknown Film (2020).mkv"}
		parsed := parser.ParsedStorageEntry{Release: &parser.SceneReleaseParse{Title: "Unknown Film", Year: "2020"}}

		info, ok := m.materializeLocal(ctx, file, parsed, nil, metadata.KindMovie, sqlc.MediaTypeMovie, lib)
		require.True(t, ok)
		require.True(t, info.IsNew)

		key := localIdentityKey("Unknown Film", "2020", sqlc.MediaTypeMovie)
		mi, err := qtx.GetMediaItemByLocalIdentityKey(ctx, sqlc.GetMediaItemByLocalIdentityKeyParams{LibraryID: lib, LocalIdentityKey: key})
		require.NoError(t, err)
		require.Equal(t, "local", mi.EnrichmentStatus)
		require.Equal(t, "local", mi.ProviderKind)
		require.NotEmpty(t, mi.Slug, "a local item still needs a slug for routing")

		// Visible: the movies row exists, so the library's INNER JOIN includes it.
		_, err = qtx.GetMovieByMediaItemID(ctx, mi.ID)
		require.NoError(t, err, "type-specific row must exist so the local item is visible")

		// Re-scan of the same local entity dedups instead of creating a duplicate.
		info2, ok2 := m.materializeLocal(ctx, file, parsed, nil, metadata.KindMovie, sqlc.MediaTypeMovie, lib)
		require.True(t, ok2)
		require.False(t, info2.IsNew, "re-scan must link to the existing local entity")
	})

	t.Run("NFO-less stubs do not mislink", func(t *testing.T) {
		lib := newLib("Movies1", sqlc.MediaTypeMovie)
		id1, new1, err := m.createOrLinkMediaItem(ctx, &metadata.MediaDetail{Title: "Alpha", Year: "2001"}, metadata.KindMovie, lib, "")
		require.NoError(t, err)
		require.True(t, new1)
		id2, new2, err := m.createOrLinkMediaItem(ctx, &metadata.MediaDetail{Title: "Beta", Year: "2002"}, metadata.KindMovie, lib, "")
		require.NoError(t, err)
		require.True(t, new2)
		require.NotEqual(t, id1, id2, "two NFO-less movies must be distinct rows, not fused via external_ids @> '{}'")
	})

	t.Run("movie: enrich fills stub; provenance protects user edits (incl. cleared)", func(t *testing.T) {
		lib := newLib("Movies2", sqlc.MediaTypeMovie)
		id, _, err := m.createOrLinkMediaItem(ctx, &metadata.MediaDetail{Title: "Gamma", Year: "2003", ExternalIDs: map[string]string{"tmdb": "111"}}, metadata.KindMovie, lib, "")
		require.NoError(t, err)

		// First enrich inserts the movie row (none existed).
		m.StoreEntityMetadata(ctx, id, metadata.KindMovie, &metadata.MediaDetail{Genres: []string{"Action"}, RuntimeMinutes: 120, Tagline: "boom"})
		mv, err := qtx.GetMovieByMediaItemID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, []string{"Action"}, mv.Genres)

		// User edits genres, CLEARS tagline, and stamps provenance (what the
		// metadata editor will do for edited fields).
		_, err = qtx.UpdateMovie(ctx, sqlc.UpdateMovieParams{
			ID: mv.ID, RuntimeMinutes: mv.RuntimeMinutes, Tagline: "", Genres: []string{"UserPick"},
			Rating: mv.Rating, ReleaseDate: mv.ReleaseDate, OriginalTitle: mv.OriginalTitle,
			OriginalLanguage: mv.OriginalLanguage, Budget: mv.Budget, Revenue: mv.Revenue,
			Popularity: mv.Popularity, SpokenLanguages: mv.SpokenLanguages, OriginCountry: mv.OriginCountry,
		})
		require.NoError(t, err)
		require.NoError(t, qtx.SetMediaItemFieldProvenance(ctx, sqlc.SetMediaItemFieldProvenanceParams{
			ID: id, FieldProvenance: []byte(`{"genres":"user","tagline":"user"}`),
		}))

		// Re-enrich: user-locked fields survive (incl. the cleared tagline);
		// unlocked fields are refreshed from remote.
		m.StoreEntityMetadata(ctx, id, metadata.KindMovie, &metadata.MediaDetail{Genres: []string{"Drama", "Thriller"}, RuntimeMinutes: 999, Tagline: "refilled tagline"})
		mv2, err := qtx.GetMovieByMediaItemID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, []string{"UserPick"}, mv2.Genres, "user-locked genres must survive re-enrich")
		require.Equal(t, "", mv2.Tagline, "a user-cleared (locked) field must survive re-enrich")
		require.EqualValues(t, 999, mv2.RuntimeMinutes, "an unlocked field is refreshed from remote")
	})

	t.Run("tv enrich fill: adds a new episode, preserves an edited one", func(t *testing.T) {
		lib := newLib("TV1", sqlc.MediaTypeTv)
		id, _, err := m.createOrLinkMediaItem(ctx, &metadata.MediaDetail{Title: "Show", Year: "2004", ExternalIDs: map[string]string{"tmdb": "222"}}, metadata.KindTV, lib, "")
		require.NoError(t, err)

		m.StoreEntityMetadata(ctx, id, metadata.KindTV, &metadata.MediaDetail{
			Status:  "Returning",
			Seasons: []metadata.SeasonDetail{{Number: 1, Title: "Season 1", Episodes: []metadata.EpisodeDetail{{Number: 1, Title: "Pilot"}}}},
		})
		series, err := qtx.GetTVSeriesByMediaItemID(ctx, id)
		require.NoError(t, err)
		seasons, err := qtx.ListTVSeasonsBySeries(ctx, series.ID)
		require.NoError(t, err)
		require.Len(t, seasons, 1)
		eps, err := qtx.ListTVEpisodesBySeason(ctx, seasons[0].ID)
		require.NoError(t, err)
		require.Len(t, eps, 1)
		require.Equal(t, "Pilot", eps[0].Title)

		// User edits the episode title.
		_, err = qtx.UpdateTVEpisode(ctx, sqlc.UpdateTVEpisodeParams{
			ID: eps[0].ID, Title: "Edited Pilot", Overview: eps[0].Overview, StillPath: eps[0].StillPath,
			RuntimeMinutes: eps[0].RuntimeMinutes, AirDate: eps[0].AirDate, Rating: eps[0].Rating,
			AbsoluteNumber: eps[0].AbsoluteNumber, IsSpecial: eps[0].IsSpecial, EpisodeType: eps[0].EpisodeType,
			ExternalIds: eps[0].ExternalIds, Source: eps[0].Source,
		})
		require.NoError(t, err)

		// Re-enrich (the fill path — a forced refresh / re-identify; non-forced
		// re-enrich is gated out at the worker) fills the existing series and adds
		// the new E2 under the existing season, while the edited E1 is preserved
		// (CreateTVEpisode is insert-or-skip).
		m.StoreEntityMetadata(ctx, id, metadata.KindTV, &metadata.MediaDetail{
			Status: "Returning",
			Seasons: []metadata.SeasonDetail{{Number: 1, Title: "Season 1", Episodes: []metadata.EpisodeDetail{
				{Number: 1, Title: "Pilot"}, {Number: 2, Title: "Second"},
			}}},
		})
		eps2, err := qtx.ListTVEpisodesBySeason(ctx, seasons[0].ID)
		require.NoError(t, err)
		require.Len(t, eps2, 2, "re-enrich adds the new episode under the existing season")
		var e1 sqlc.TvEpisode
		for _, e := range eps2 {
			if e.EpisodeNumber == 1 {
				e1 = e
			}
		}
		require.Equal(t, "Edited Pilot", e1.Title, "the edited episode title is preserved")
	})
}
