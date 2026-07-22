package service

import (
	"context"
	"fmt"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

// Music smart-home shelves. Each method returns one section of the landing
// page. The "rotating" shelves (MoreByArtists, LapsedArtists, MoreFromLabel,
// MoreInGenre) are seed-driven so the same query within a 5-minute window
// returns the same picks — that's what gives the page its "stable until it
// rotates" feel.
//
// Mixes for You is live-generated rather than batch-precomputed because the
// user wants it to surface within seconds of new listening behavior. The
// price is a one-second-ish render whenever the cache is cold. The 1h
// response cache makes it cheap on subsequent fetches.

// shelfBucketSize is the rotation period for the seed-driven shelves. Five
// minutes is the user's stated cadence; the bucket index becomes the seed.
const shelfBucketSize = 5 * time.Minute

// The four profile archetypes plus at most six artist spotlights keep the
// slate broad without turning one uncached request into dozens of sequential
// HNSW queries. Callers may request fewer; requesting 20 is still bounded to
// a useful ten-mix slate.
const maxArtistMixesInSlate = 6

// shelfSeed returns a string seed that's stable within the current 5-minute
// window. Combined with the user_id in the query so the same seed produces
// different picks for different users.
func shelfSeed(userID int64) string {
	bucket := time.Now().Unix() / int64(shelfBucketSize.Seconds())
	return fmt.Sprintf("%d:%d", userID, bucket)
}

// MusicMix is one generated music product. Artist mixes retain their seed
// fields; profile/discovery/rediscovery mixes leave them empty and describe
// themselves through Kind/Description. Slug is the stable route identity and
// is intentionally not coupled to an artist slug.
type MusicMix struct {
	Slug                        string `json:"slug"`
	Kind                        string `json:"kind"`
	Description                 string `json:"description"`
	SeedArtistID                int64  `json:"seed_artist_id"`
	SeedArtistName              string `json:"seed_artist_name"`
	SeedArtistSlug              string `json:"seed_artist_slug"`
	SeedArtistMediaItemID       int64  `json:"seed_artist_media_item_id"`
	SeedArtistMediaItemPublicID string `json:"seed_artist_media_item_public_id,omitempty"`
	// SeedGenre is set for Kind == "genre" mixes (generateGenreMixes) and
	// genre-seeded Kind == "library" sampler mixes (generateLibrarySampler-
	// Mixes) — the genre name backing the title, so the FE can render a
	// genre chip without parsing Name.
	SeedGenre string                              `json:"seed_genre,omitempty"`
	Name      string                              `json:"name"`
	Tracks    []sqlc.ListArtistTopTracksForMixRow `json:"tracks"`
}

// GenerateMixesForUser assembles one bounded slate from the shared music
// recommendation pool: four profile archetypes followed by up to six
// per-artist taste mixes. Provider/catalog popularity keeps cold-start users
// useful even with no explicit taste yet.
//
// variant is 0 for the normal cached daily slate (stable within a day) or a
// caller-supplied non-zero value (e.g. a regenerate request's timestamp) that
// folds into both generators' rotation entropy so the same day can still
// produce a visibly different slate on demand.
func (a *App) GenerateMixesForUser(ctx context.Context, userID int64, maxMixes, tracksPerMix int, variant int64) ([]MusicMix, error) {
	if maxMixes <= 0 {
		maxMixes = 10
	} else if maxMixes > 10 {
		maxMixes = 10
	}
	if tracksPerMix <= 0 || tracksPerMix > 100 {
		tracksPerMix = 30
	}

	// The shared recommendation core produces the non-artist archetypes first:
	// For You, discovery, rediscovery, and deep cuts. These blend sonic KNN,
	// explicit taste, completion signals, provider charts, and the external
	// similar-artist graph. Cold users still get a provider-popularity mix.
	mixes, err := a.generateRecommendationMixes(ctx, userID, maxMixes, tracksPerMix, variant)
	if err != nil {
		log.Warn().Err(err).Msg("recommendation mixes failed — trying artist mixes")
		mixes = nil
	}

	// usedTrackIDs accumulates across every generator below so a track never
	// appears in two mixes in the same slate (docs/mix-rules-plan.md layer-1:
	// "a track appears in at most one mix per slate").
	usedTrackIDs := make([]int64, 0, maxMixes*tracksPerMix)
	for _, m := range mixes {
		for _, t := range m.Tracks {
			usedTrackIDs = append(usedTrackIDs, t.TrackID)
		}
	}

	// Genre archetype (mix-rules-plan layer-1 #2): "<Genre> Mix" seeded from
	// the user's top 1-2 genres by recent affinity. Self-gates when the
	// affinity distribution is too sparse/flat to name a genre honestly —
	// see genreMixMinAffinityTracks/genreMixMinShare.
	if genreSlots := maxMixes - len(mixes); genreSlots > 0 {
		genreMixes, genreErr := a.generateGenreMixes(ctx, userID, genreSlots, tracksPerMix, variant, usedTrackIDs)
		if genreErr != nil {
			log.Warn().Err(genreErr).Msg("genre mixes failed")
		} else if len(genreMixes) > 0 {
			mixes = append(mixes, genreMixes...)
			for _, m := range genreMixes {
				for _, t := range m.Tracks {
					usedTrackIDs = append(usedTrackIDs, t.TrackID)
				}
			}
		}
	}

	artistSlots := maxMixes - len(mixes)
	artistSlots = min(artistSlots, maxArtistMixesInSlate)
	if artistSlots > 0 {
		artistMixes, artistErr := a.generateTasteMixes(ctx, userID, artistSlots, tracksPerMix, variant, usedTrackIDs)
		if artistErr != nil {
			log.Warn().Err(artistErr).Msg("taste mixes failed")
		} else if len(artistMixes) > 0 {
			mixes = append(mixes, artistMixes...)
			for _, m := range artistMixes {
				for _, t := range m.Tracks {
					usedTrackIDs = append(usedTrackIDs, t.TrackID)
				}
			}
		}
	}

	// Library Sampler — the cold-start floor (mix-rules-plan cold ladder):
	// genre tours seeded from the library's own composition rather than
	// listening history. Runs last on purpose and self-devalues via its
	// signal ladder, so it only carries the slate while the personal
	// archetypes above have nothing to work with — see music_mixes_sampler.go.
	if samplerSlots := maxMixes - len(mixes); samplerSlots > 0 {
		samplerMixes, samplerErr := a.generateLibrarySamplerMixes(ctx, userID, samplerSlots, tracksPerMix, variant, usedTrackIDs)
		if samplerErr != nil {
			log.Warn().Err(samplerErr).Msg("library sampler mixes failed")
		} else if len(samplerMixes) > 0 {
			mixes = append(mixes, samplerMixes...)
		}
	}

	// A cold user with no analyzed tracks, no explicit ratings, and no
	// provider popularity yet legitimately produces nothing from either
	// generator — an empty slate is fine, the FE hides the empty rail.
	if mixes == nil {
		mixes = []MusicMix{}
	}
	return mixes, nil
}

// diversifyMixByArtist — same idea as the music_radio version but for the
// mix row shape. Keeps no two adjacent tracks from the same artist when
// possible, preserving the play-count rank ordering otherwise.
func diversifyMixByArtist(rows []sqlc.ListArtistTopTracksForMixRow, limit int) []sqlc.ListArtistTopTracksForMixRow {
	if len(rows) <= 1 || limit <= 1 {
		if len(rows) > limit {
			return rows[:limit]
		}
		return rows
	}
	out := make([]sqlc.ListArtistTopTracksForMixRow, 0, limit)
	deferred := make([]sqlc.ListArtistTopTracksForMixRow, 0)
	seen := make(map[int64]bool, limit)
	prevArtist := int64(0)
	for _, r := range rows {
		if seen[r.TrackID] {
			continue
		}
		if r.ArtistID == prevArtist && len(out) > 0 {
			deferred = append(deferred, r)
			continue
		}
		out = append(out, r)
		seen[r.TrackID] = true
		prevArtist = r.ArtistID
		if len(out) >= limit {
			break
		}
	}
	for _, r := range deferred {
		if len(out) >= limit {
			break
		}
		if seen[r.TrackID] || (len(out) > 0 && out[len(out)-1].ArtistID == r.ArtistID) {
			continue
		}
		out = append(out, r)
		seen[r.TrackID] = true
	}
	if len(out) < limit {
		for _, r := range deferred {
			if len(out) >= limit {
				break
			}
			if !seen[r.TrackID] {
				out = append(out, r)
				seen[r.TrackID] = true
			}
		}
	}
	return out
}

// ArtistPlayQueue returns one artist's full catalog ordered by the user's
// play count desc (top hits first), then album year + disc/track. Powers
// the "play this artist" button on home tiles — the queue starts on the
// best-of and naturally rolls into the deeper cuts.
func (a *App) ArtistPlayQueue(ctx context.Context, userID int64, slug string, limit int32) ([]sqlc.ListArtistTracksTopPlayedFirstRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	return sqlc.New(a.db).ListArtistTracksTopPlayedFirst(ctx, sqlc.ListArtistTracksTopPlayedFirstParams{
		UserID:     userID,
		Slug:       slug,
		TrackLimit: limit,
	})
}

// RecentlyPlayedArtistsForUser — the "Recently Played" shelf at artist
// granularity (not track granularity). Distinct artists, newest-play first.
func (a *App) RecentlyPlayedArtistsForUser(ctx context.Context, userID int64, limit, offset int32) ([]sqlc.ListRecentlyPlayedArtistsRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return sqlc.New(a.db).ListRecentlyPlayedArtists(ctx, sqlc.ListRecentlyPlayedArtistsParams{
		UserID: userID,
		Lim:    limit,
		Off:    offset,
	})
}

// OnThisDayAlbums — anniversaries. Empty for libraries thin on release-date
// metadata; that's fine, the FE hides empty rails.
func (a *App) OnThisDayAlbums(ctx context.Context, limit, offset int32) ([]sqlc.ListOnThisDayAlbumsRow, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return sqlc.New(a.db).ListOnThisDayAlbums(ctx, sqlc.ListOnThisDayAlbumsParams{Lim: limit, Off: offset})
}

// RecentPlaylistsForUser — playlists ordered by "last played" (max played_at
// across the playlist's tracks for this user). Falls back to updated_at when
// the user hasn't actually played anything from it.
func (a *App) RecentPlaylistsForUser(ctx context.Context, userID int64, limit, offset int32) ([]sqlc.ListRecentUserPlaylistsRow, error) {
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	if offset < 0 {
		offset = 0
	}
	return sqlc.New(a.db).ListRecentUserPlaylists(ctx, sqlc.ListRecentUserPlaylistsParams{
		UserID: userID,
		Lim:    limit,
		Off:    offset,
	})
}

// MoreByArtist is the row shape for the "More by <Artist>" shelf — one
// seed artist + a small list of their albums. Service picks `picks` random
// artists from the user's history (stable per 5-min window) and fetches
// each artist's discography.
type MoreByArtist struct {
	ArtistID    int64                                  `json:"artist_id"`
	ArtistName  string                                 `json:"artist_name"`
	ArtistSlug  string                                 `json:"artist_slug"`
	MediaItemID int64                                  `json:"media_item_id"`
	Albums      []sqlc.ListAlbumsByArtistIDForShelfRow `json:"albums"`
}

// MoreByArtistsForUser returns `picks` artist-discography blocks. Picks are
// stable within a 5-minute window so navigating away and back doesn't churn
// the page.
func (a *App) MoreByArtistsForUser(ctx context.Context, userID int64, picks, albumsPerArtist int32) ([]MoreByArtist, error) {
	if picks <= 0 || picks > 10 {
		picks = 3
	}
	if albumsPerArtist <= 0 || albumsPerArtist > 20 {
		albumsPerArtist = 6
	}
	q := sqlc.New(a.db)
	seeds, err := q.PickRandomPlayedArtists(ctx, sqlc.PickRandomPlayedArtistsParams{
		UserID: userID,
		Seed:   shelfSeed(userID),
		Picks:  picks,
	})
	if err != nil {
		return nil, fmt.Errorf("more-by seeds: %w", err)
	}
	out := make([]MoreByArtist, 0, len(seeds))
	for _, s := range seeds {
		albums, err := q.ListAlbumsByArtistIDForShelf(ctx, sqlc.ListAlbumsByArtistIDForShelfParams{
			ArtistID: s.ArtistID,
			Limit:    albumsPerArtist,
		})
		if err != nil {
			return nil, fmt.Errorf("more-by albums: %w", err)
		}
		if len(albums) == 0 {
			continue
		}
		out = append(out, MoreByArtist{
			ArtistID:    s.ArtistID,
			ArtistName:  s.ArtistName,
			ArtistSlug:  s.ArtistSlug,
			MediaItemID: s.MediaItemID,
			Albums:      albums,
		})
	}
	return out, nil
}

// MoreInGenre is the row shape for the genre-driven shelf. Lists artists
// in a genre the user listens to — no album art, just a tight artist list
// (matches the hibiki UX of "names only" for this rail).
type MoreInGenre struct {
	Genre   string                       `json:"genre"`
	Artists []sqlc.ListArtistsByGenreRow `json:"artists"`
}

// MoreInGenreForUser picks ONE of the user's top genres (cycled via the
// 5-min seed) and returns artists matching it. We picks 1 because the
// hibiki UX devotes a full row to one genre at a time.
func (a *App) MoreInGenreForUser(ctx context.Context, userID int64, artistsLimit int32) (*MoreInGenre, error) {
	if artistsLimit <= 0 || artistsLimit > 50 {
		artistsLimit = 20
	}
	q := sqlc.New(a.db)
	// Pull the top 10 candidates; rotate by 5-min bucket index.
	top, err := q.PickTopGenresForUser(ctx, sqlc.PickTopGenresForUserParams{
		UserID: userID,
		Limit:  10,
	})
	if err != nil {
		return nil, fmt.Errorf("top genres: %w", err)
	}
	if len(top) == 0 {
		return nil, nil
	}
	bucket := time.Now().Unix() / int64(shelfBucketSize.Seconds())
	pick := top[int(bucket)%len(top)]
	artists, err := q.ListArtistsByGenre(ctx, sqlc.ListArtistsByGenreParams{
		Genre: pick.Genre,
		Limit: artistsLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("artists by genre %q: %w", pick.Genre, err)
	}
	if len(artists) == 0 {
		return nil, nil
	}
	return &MoreInGenre{Genre: pick.Genre, Artists: artists}, nil
}

// MostPlayedShelf — the "Most Played in <month>" shelf. Month is fixed at
// "previous calendar month" since that's the established Plex/Apple Music
// convention. Header text comes back from the server so the FE doesn't
// localize-cum-format the month name itself.
type MostPlayedShelf struct {
	WindowLabel string                            `json:"window_label"`
	StartAt     time.Time                         `json:"start_at"`
	EndAt       time.Time                         `json:"end_at"`
	Albums      []sqlc.MostPlayedAlbumsInRangeRow `json:"albums"`
}

// MostPlayedAlbumsLastMonth returns the heaviest-played albums in the
// previous calendar month. Falls back to "Most Played This Month" when
// the calendar just rolled and the previous month has no plays.
func (a *App) MostPlayedAlbumsLastMonth(ctx context.Context, userID int64, limit int32) (*MostPlayedShelf, error) {
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	q := sqlc.New(a.db)

	now := time.Now()
	thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	lastMonthStart := thisMonthStart.AddDate(0, -1, 0)

	rows, err := q.MostPlayedAlbumsInRange(ctx, sqlc.MostPlayedAlbumsInRangeParams{
		UserID:  userID,
		StartAt: pgTimestamptz(lastMonthStart),
		EndAt:   pgTimestamptz(thisMonthStart),
		Limit:   limit,
	})
	if err != nil {
		return nil, fmt.Errorf("most-played last month: %w", err)
	}
	label := "Most Played in " + lastMonthStart.Month().String()
	start := lastMonthStart
	end := thisMonthStart

	// Fallback to this-month when last-month is empty (fresh users / fresh
	// install). Keeps the rail meaningful instead of going blank for the
	// first 30 days.
	if len(rows) == 0 {
		rows, err = q.MostPlayedAlbumsInRange(ctx, sqlc.MostPlayedAlbumsInRangeParams{
			UserID:  userID,
			StartAt: pgTimestamptz(thisMonthStart),
			EndAt:   pgTimestamptz(thisMonthStart.AddDate(0, 1, 0)),
			Limit:   limit,
		})
		if err != nil {
			return nil, fmt.Errorf("most-played this month: %w", err)
		}
		label = "Most Played in " + thisMonthStart.Month().String()
		start = thisMonthStart
		end = thisMonthStart.AddDate(0, 1, 0)
	}
	return &MostPlayedShelf{
		WindowLabel: label,
		StartAt:     start,
		EndAt:       end,
		Albums:      rows,
	}, nil
}

// LapsedArtistShelf — the "Haven't Played in N Months" shelf. Picks artists
// the user used to play (>=3 plays) but hasn't touched in the past 6 months.
// Stable within the 5-min window.
type LapsedArtistShelf struct {
	SinceLabel string              `json:"since_label"`
	Artists    []LapsedArtistEntry `json:"artists"`
}

// LapsedArtistEntry pairs the lapsed-artist row with their albums so the
// FE can render an inline mini-discography card per artist.
type LapsedArtistEntry struct {
	ArtistID     int64                                  `json:"artist_id"`
	ArtistName   string                                 `json:"artist_name"`
	ArtistSlug   string                                 `json:"artist_slug"`
	MediaItemID  int64                                  `json:"media_item_id"`
	LastPlayedAt time.Time                              `json:"last_played_at"`
	PlayCount    int64                                  `json:"play_count"`
	MonthsLapsed int                                    `json:"months_lapsed"`
	Albums       []sqlc.ListAlbumsByArtistIDForShelfRow `json:"albums"`
}

// LapsedArtistsForUser returns 3 artists the user historically played but
// hasn't in ~6 months, each with a mini-discography card.
func (a *App) LapsedArtistsForUser(ctx context.Context, userID int64, picks, albumsPerArtist int32) (*LapsedArtistShelf, error) {
	if picks <= 0 || picks > 10 {
		picks = 3
	}
	if albumsPerArtist <= 0 || albumsPerArtist > 20 {
		albumsPerArtist = 6
	}
	q := sqlc.New(a.db)

	const lapsedDays = 180
	cutoff := time.Now().AddDate(0, 0, -lapsedDays)
	rows, err := q.ListLapsedArtists(ctx, sqlc.ListLapsedArtistsParams{
		UserID:   userID,
		CutoffAt: pgTimestamptz(cutoff),
		MinPlays: 3,
		Seed:     shelfSeed(userID),
		Picks:    picks,
	})
	if err != nil {
		return nil, fmt.Errorf("lapsed artists: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	entries := make([]LapsedArtistEntry, 0, len(rows))
	for _, r := range rows {
		albums, err := q.ListAlbumsByArtistIDForShelf(ctx, sqlc.ListAlbumsByArtistIDForShelfParams{
			ArtistID: r.ArtistID,
			Limit:    albumsPerArtist,
		})
		if err != nil {
			return nil, fmt.Errorf("lapsed album fetch: %w", err)
		}
		var lastPlayed time.Time
		months := 0
		if r.LastPlayedAt.Valid {
			lastPlayed = r.LastPlayedAt.Time
			months = int(time.Since(r.LastPlayedAt.Time).Hours() / 24 / 30)
		}
		entries = append(entries, LapsedArtistEntry{
			ArtistID:     r.ArtistID,
			ArtistName:   r.ArtistName,
			ArtistSlug:   r.ArtistSlug,
			MediaItemID:  r.MediaItemID,
			LastPlayedAt: lastPlayed,
			PlayCount:    r.PlayCount,
			MonthsLapsed: months,
			Albums:       albums,
		})
	}
	return &LapsedArtistShelf{
		SinceLabel: "Haven't played in a while",
		Artists:    entries,
	}, nil
}

// MoreFromLabel — picks ONE label from the user's listening and returns
// every album on it (across all artists). The "discovery" angle: the
// label-roster shelves are how you find a new band you'd actually like
// based on which label your favorites releases through.
type MoreFromLabel struct {
	Label  string                      `json:"label"`
	Albums []sqlc.ListAlbumsByLabelRow `json:"albums"`
}

// MoreFromLabelForUser samples one label the user listens to, returns
// every music-library album under that label.
func (a *App) MoreFromLabelForUser(ctx context.Context, userID int64, limit int32) (*MoreFromLabel, error) {
	if limit <= 0 || limit > 40 {
		limit = 20
	}
	q := sqlc.New(a.db)
	labels, err := q.PickLabelForUser(ctx, sqlc.PickLabelForUserParams{
		UserID: userID,
		Seed:   shelfSeed(userID),
		Picks:  1,
	})
	if err != nil {
		return nil, fmt.Errorf("pick label: %w", err)
	}
	if len(labels) == 0 {
		return nil, nil
	}
	pick := labels[0].Label
	rows, err := q.ListAlbumsByLabel(ctx, sqlc.ListAlbumsByLabelParams{
		Label: pick,
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("albums by label %q: %w", pick, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &MoreFromLabel{Label: pick, Albums: rows}, nil
}
