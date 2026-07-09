package service

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestAnimeMediaWatchedAndFavoritedUseSeriesState(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	suffix := time.Now().UnixNano()
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		Username:     fmt.Sprintf("anime-state-%d", suffix),
		Email:        fmt.Sprintf("anime-state-%d@test.local", suffix),
		PasswordHash: "$2a$10$fakehash",
		IsAdmin:      true,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM user_watch_progress WHERE user_id = $1`, user.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM user_favorites WHERE user_id = $1`, user.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
	})

	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         fmt.Sprintf("Anime State %d", suffix),
		MediaType:    sqlc.MediaTypeAnime,
		Paths:        []string{"/media/anime"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	itemID, err := q.CreateMediaItemRaw(ctx, sqlc.CreateMediaItemRawParams{
		LibraryID:        lib.ID,
		MediaType:        sqlc.MediaTypeAnime,
		ProviderKind:     "tv",
		Title:            "Bocchi the Rock!",
		SortTitle:        "bocchi the rock",
		Year:             "2022",
		Description:      "",
		PosterPath:       "",
		BackdropPath:     "",
		Tagline:          "",
		OriginalTitle:    "",
		OriginalLanguage: "ja",
		Status:           "",
		ExternalIds:      []byte(`{"tmdb":"119100"}`),
	})
	require.NoError(t, err)
	series, err := q.CreateTVSeries(ctx, sqlc.CreateTVSeriesParams{
		MediaItemID:      itemID,
		Status:           "ended",
		Genres:           []string{"Animation"},
		Rating:           pgtype.Numeric{Int: big.NewInt(0), Valid: true},
		OriginalName:     "ぼっち・ざ・ろっく！",
		OriginalLanguage: "ja",
		NumberOfSeasons:  1,
		NumberOfEpisodes: 1,
		Popularity:       pgtype.Numeric{Int: big.NewInt(0), Valid: true},
		SpokenLanguages:  []string{"ja"},
		OriginCountry:    []string{"JP"},
	})
	require.NoError(t, err)
	season, err := q.CreateTVSeason(ctx, sqlc.CreateTVSeasonParams{
		SeriesID:      series.ID,
		SeasonNumber:  1,
		Title:         "Season 1",
		AiredEpisodes: 1,
		ExternalIds:   []byte(`{}`),
	})
	require.NoError(t, err)
	episode, err := q.CreateTVEpisode(ctx, sqlc.CreateTVEpisodeParams{
		SeasonID:      season.ID,
		EpisodeNumber: 1,
		Title:         "Lonely Rolling Bocchi",
		Rating:        pgtype.Numeric{Int: big.NewInt(0), Valid: true},
		ExternalIds:   []byte(`{}`),
		Source:        "test",
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO library_files (library_id, path, size, parse_result, status, media_item_id)
		VALUES ($1, $2, 123, '{"parsed":{"release":{"seasons":[1],"episodes":[1]}}}'::jsonb, 'matched', $3)
	`, lib.ID, "/media/anime/Bocchi the Rock!/Bocchi.S01E01.mkv", itemID)
	require.NoError(t, err)

	app := &App{db: pool}
	require.NoError(t, app.MarkMediaWatched(ctx, user.ID, itemID, true))
	watchedIDs, err := q.ListWatchedEpisodeIDsForSeries(ctx, sqlc.ListWatchedEpisodeIDsForSeriesParams{
		UserID:   user.ID,
		SeriesID: series.ID,
	})
	require.NoError(t, err)
	require.Contains(t, watchedIDs, episode.ID)

	_, err = q.ToggleFavorite(ctx, sqlc.ToggleFavoriteParams{
		UserID:     user.ID,
		EntityType: "media_item",
		EntityID:   itemID,
	})
	require.NoError(t, err)
	state, err := app.GetUserState(ctx, user.ID, "series", 0)
	require.NoError(t, err)
	require.Contains(t, state["favorited"].([]int64), itemID)
}
