package service

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog/log"
)

// Taste-driven Mixes for You (docs/mix-rules-plan.md phases 1+2). The old
// generator seeded from whole-artist catalog centroids and filled with
// globally top-played tracks — rigid and blind to the user. This path:
//
//   - scores every track the user touched with an AFFINITY (completed plays
//     are a weak, decaying positive; explicit track/album/artist reactions
//     dominate; dislikes are hard exclusions),
//   - seeds each mix from the user's top-affinity artists,
//   - builds the seed vector as the centroid of the user's POSITIVE tracks
//     by that artist (their taste within the artist, not the catalog blur),
//   - expands by track-level KNN over track_facets.track_embedding,
//   - reserves an exploration share drawn from deeper KNN ranks with a
//     day-seeded shuffle, so mixes rotate daily without losing the core,
//   - never includes a disliked track.
//
// Falls back to the legacy generator when the user has no usable affinity
// yet (cold start) — callers see one contract either way.

// musicAffinityCTE scores (track_id, score) for one user ($1). Kept as a
// shared WITH-clause so seed selection and per-artist queries stay in sync.
const musicAffinityCTE = `
	play_aff AS (
		SELECT pe.track_id,
		       LEAST(2.0, SUM(0.25
		           * POWER(0.5, EXTRACT(EPOCH FROM (now() - pe.played_at)) / 2592000.0))) AS score
		FROM play_events pe
		WHERE pe.user_id = $1 AND pe.completed
		GROUP BY pe.track_id
	),
	rate_aff AS (
		SELECT utr.track_id,
		       CASE WHEN utr.rating >= 9 THEN 8.0
		            WHEN utr.rating >= 6 THEN 4.0
		            WHEN utr.rating <= 3 THEN 0.0
		            ELSE 0.75 END::float8 AS score
		FROM user_track_ratings utr
		WHERE utr.user_id = $1
	),
	album_aff AS (
		SELECT t.id AS track_id,
		       CASE WHEN uar.rating >= 9 THEN 3.0
		            WHEN uar.rating >= 6 THEN 1.5
		            WHEN uar.rating <= 3 THEN 0.0
		            ELSE 0.25 END::float8 AS score
		FROM user_album_ratings uar
		JOIN tracks t ON t.album_id = uar.album_id
		WHERE uar.user_id = $1
	),
	artist_aff AS (
		SELECT uar.artist_id,
		       CASE WHEN uar.rating >= 9 THEN 2.0
		            WHEN uar.rating >= 6 THEN 1.0
		            WHEN uar.rating <= 3 THEN 0.0
		            ELSE 0.15 END::float8 AS score
		FROM user_artist_ratings uar
		WHERE uar.user_id = $1
	),
	aff AS (
		SELECT track_id, SUM(score)::float8 AS score
		FROM (
			SELECT * FROM play_aff
			UNION ALL SELECT * FROM rate_aff
			UNION ALL SELECT * FROM album_aff
		) u
		GROUP BY track_id
	)`

// musicVetoFilter excludes every explicit downvote, including inherited
// album/artist downvotes. It is usable in queries with user id $1 and the
// conventional tracks/albums aliases t + al.
const musicVetoFilter = `NOT EXISTS (
		SELECT 1 FROM user_track_ratings veto
		WHERE veto.user_id = $1 AND veto.track_id = t.id AND veto.rating <= 3)
	AND NOT EXISTS (
		SELECT 1 FROM user_album_ratings veto
		WHERE veto.user_id = $1 AND veto.album_id = al.id AND veto.rating <= 3)
	AND NOT EXISTS (
		SELECT 1 FROM user_artist_ratings veto
		WHERE veto.user_id = $1 AND veto.artist_id = al.artist_id AND veto.rating <= 3)`

type tasteMixSeed struct {
	ArtistID          int64
	ArtistName        string
	MediaItemID       int64
	MediaItemPublicID uuid.UUID
	ArtistSlug        string
	Affinity          float64
}

// tasteMixSeeds ranks the user's artists by summed positive track affinity.
func (a *App) tasteMixSeeds(ctx context.Context, userID int64, picks int) ([]tasteMixSeed, error) {
	rows, err := a.db.Query(ctx, `WITH `+musicAffinityCTE+`,
		track_artist_aff AS (
			SELECT al.artist_id, SUM(aff.score)::float8 AS score
			FROM aff
			JOIN tracks t ON t.id = aff.track_id
			JOIN albums al ON al.id = t.album_id
			WHERE aff.score > 0 AND `+musicVetoFilter+`
			GROUP BY al.artist_id
		),
		artist_scores AS (
			SELECT artist_id, score FROM track_artist_aff
			UNION ALL
			SELECT artist_id, score * 6.0 FROM artist_aff WHERE score > 0
		),
		ranked AS (
			SELECT artist_id, SUM(score)::float8 AS total
			FROM artist_scores
			GROUP BY artist_id
		)
		SELECT ar.id, ar.name, mi.id, mi.public_id, mi.slug, ranked.total
		FROM ranked
		JOIN artists ar ON ar.id = ranked.artist_id
		JOIN media_item_cards mi ON mi.id = ar.media_item_id
		WHERE ranked.total > 0
		  AND EXISTS (SELECT 1 FROM library_files lf WHERE lf.media_item_id = ar.media_item_id AND lf.deleted_at IS NULL)
		ORDER BY ranked.total DESC
		LIMIT $2`, userID, picks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tasteMixSeed
	for rows.Next() {
		var s tasteMixSeed
		if err := rows.Scan(&s.ArtistID, &s.ArtistName, &s.MediaItemID, &s.MediaItemPublicID, &s.ArtistSlug, &s.Affinity); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// tasteCentroids batches the per-artist taste-vector lookup so a slate does
// not pay one query merely to discover that each cold artist lacks facets.
func (a *App) tasteCentroids(ctx context.Context, userID int64, artistIDs []int64) (map[int64]pgvector.Vector, error) {
	out := make(map[int64]pgvector.Vector)
	if len(artistIDs) == 0 {
		return out, nil
	}
	rows, err := a.db.Query(ctx, `WITH `+musicAffinityCTE+`
		SELECT al.artist_id, AVG(tf.track_embedding)
		FROM aff
		JOIN tracks t ON t.id = aff.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN track_facets tf ON tf.track_id = t.id
		WHERE al.artist_id = ANY($2::bigint[])
		  AND aff.score > 0
		  AND tf.track_embedding IS NOT NULL
		  AND `+musicVetoFilter+`
		GROUP BY al.artist_id`, userID, artistIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var artistID int64
		var centroid pgvector.Vector
		if err := rows.Scan(&artistID, &centroid); err != nil {
			return nil, err
		}
		out[artistID] = centroid
	}
	return out, rows.Err()
}

// tasteSeedTracks returns the user's own positive tracks for the seed artist,
// strongest affinity first — the personal core of the mix.
func (a *App) tasteSeedTracks(ctx context.Context, userID, artistID int64, limit int) ([]sqlc.ListArtistTopTracksForMixRow, error) {
	return a.scanMixRows(ctx, `WITH `+musicAffinityCTE+`
		SELECT t.id, t.title, t.duration, t.disc_number, t.track_number,
		       al.id, al.title, al.slug, al.cover_path, al.year,
		       ar.id, ar.name, mi.slug,
		       (SELECT count(*) FROM play_events pe WHERE pe.track_id = t.id AND pe.completed) AS play_count
		FROM aff
		JOIN tracks t ON t.id = aff.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		JOIN media_item_cards mi ON mi.id = ar.media_item_id
		WHERE al.artist_id = $2 AND aff.score > 0
		  AND `+musicVetoFilter+`
		  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
		              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
		ORDER BY aff.score DESC
		LIMIT $3`, userID, artistID, limit)
}

// tasteNeighborTracks KNNs the taste centroid over per-track embeddings,
// skipping the user's dislikes and unavailable files. fetch should exceed the
// slots wanted — the caller keeps a top core and samples deeper ranks.
func (a *App) tasteNeighborTracks(ctx context.Context, userID int64, centroid pgvector.Vector, fetch int) ([]sqlc.ListArtistTopTracksForMixRow, error) {
	return a.scanMixRows(ctx, `
		SELECT t.id, t.title, t.duration, t.disc_number, t.track_number,
		       al.id, al.title, al.slug, al.cover_path, al.year,
		       ar.id, ar.name, mi.slug,
		       (SELECT count(*) FROM play_events pe WHERE pe.track_id = t.id AND pe.completed) AS play_count
		FROM track_facets tf
		JOIN tracks t ON t.id = tf.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		JOIN media_item_cards mi ON mi.id = ar.media_item_id
		WHERE tf.track_embedding IS NOT NULL
		  AND `+musicVetoFilter+`
		  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
		              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
		ORDER BY tf.track_embedding <=> $2
		LIMIT $3`, userID, centroid, fetch)
}

func (a *App) scanMixRows(ctx context.Context, query string, args ...any) ([]sqlc.ListArtistTopTracksForMixRow, error) {
	rows, err := a.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sqlc.ListArtistTopTracksForMixRow
	for rows.Next() {
		var r sqlc.ListArtistTopTracksForMixRow
		if err := rows.Scan(&r.TrackID, &r.TrackTitle, &r.Duration, &r.DiscNumber, &r.TrackNumber,
			&r.AlbumID, &r.AlbumTitle, &r.AlbumSlug, &r.AlbumCoverPath, &r.AlbumYear,
			&r.ArtistID, &r.ArtistName, &r.ArtistSlug, &r.PlayCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// generateTasteMixes is the affinity-driven path. Empty result (not an
// error) means "no usable taste yet" — the caller falls back to legacy.
func (a *App) generateTasteMixes(ctx context.Context, userID int64, maxMixes, tracksPerMix int) ([]MusicMix, error) {
	// Over-fetch seeds because a top artist can legitimately have no analyzed
	// tracks yet. Keep walking the ranked taste list until the requested mix
	// slots are filled instead of letting one cold artist erase the rail.
	seeds, err := a.tasteMixSeeds(ctx, userID, max(64, maxMixes*8))
	if err != nil {
		return nil, fmt.Errorf("taste seeds: %w", err)
	}
	if len(seeds) == 0 {
		return nil, nil
	}
	artistIDs := make([]int64, len(seeds))
	for i, seed := range seeds {
		artistIDs[i] = seed.ArtistID
	}
	centroids, err := a.tasteCentroids(ctx, userID, artistIDs)
	if err != nil {
		return nil, fmt.Errorf("taste centroids: %w", err)
	}

	// Day-bucketed exploration seed: stable across a day per user, fresh
	// tomorrow — mixes rotate without churning on every request.
	dayBucket := time.Now().Unix() / 86400

	mixes := make([]MusicMix, 0, len(seeds))
	for _, seed := range seeds {
		if len(mixes) >= maxMixes {
			break
		}
		centroid, ok := centroids[seed.ArtistID]
		if !ok || len(centroid.Slice()) == 0 {
			continue // nothing analyzed for this artist yet
		}

		// Personal core: the user's own favorites from the seed artist
		// (~1/3 of the mix), ranked by THEIR affinity — not global counts.
		coreQuota := tracksPerMix / 3
		if coreQuota < 3 {
			coreQuota = 3
		}
		core, err := a.tasteSeedTracks(ctx, userID, seed.ArtistID, coreQuota)
		if err != nil {
			return nil, fmt.Errorf("seed tracks for %d: %w", seed.ArtistID, err)
		}

		// Neighborhood: track-level KNN around the taste centroid. Fetch
		// deep so the exploration share has real range to sample from.
		neighbors, err := a.tasteNeighborTracks(ctx, userID, centroid, tracksPerMix*3)
		if err != nil {
			return nil, fmt.Errorf("neighbors for %d: %w", seed.ArtistID, err)
		}

		rng := rand.New(rand.NewSource(userID ^ seed.ArtistID<<20 ^ dayBucket)) //nolint:gosec // rotation, not crypto
		tracks := assembleTasteMix(core, neighbors, tracksPerMix, rng)
		if len(tracks) < 5 {
			continue // too thin to feel like a mix
		}

		mixes = append(mixes, MusicMix{
			Slug:                        "artist-" + seed.ArtistSlug,
			Kind:                        "artist",
			Description:                 "Built from the tracks you like by this artist, then widened with sonic and catalog neighbors.",
			SeedArtistID:                seed.ArtistID,
			SeedArtistName:              seed.ArtistName,
			SeedArtistSlug:              seed.ArtistSlug,
			SeedArtistMediaItemID:       seed.MediaItemID,
			SeedArtistMediaItemPublicID: seed.MediaItemPublicID.String(),
			Name:                        "Inspired by " + seed.ArtistName,
			Tracks:                      tracks,
		})
	}
	if len(mixes) > 0 {
		log.Debug().Int64("user", userID).Int("mixes", len(mixes)).Msg("mixes: taste-driven path")
	}
	return mixes, nil
}

// assembleTasteMix merges the personal core with KNN neighbors: dedup by
// track and by song version, a per-artist cap for variety, and ~25% of the
// neighbor slots drawn from deeper ranks (the exploration share) so the mix
// branches out instead of hugging its seed.
func assembleTasteMix(core, neighbors []sqlc.ListArtistTopTracksForMixRow, tracksPerMix int, rng *rand.Rand) []sqlc.ListArtistTopTracksForMixRow {
	seenTrack := map[int64]bool{}
	seenSong := map[string]bool{}
	artistCounts := map[int64]int{}
	artistCap := max(3, tracksPerMix/6)
	out := make([]sqlc.ListArtistTopTracksForMixRow, 0, tracksPerMix)

	push := func(r sqlc.ListArtistTopTracksForMixRow, capped bool) bool {
		if len(out) >= tracksPerMix || seenTrack[r.TrackID] || seenSong[mixSongKey(r)] {
			return false
		}
		if capped && artistCounts[r.ArtistID] >= artistCap {
			return false
		}
		seenTrack[r.TrackID] = true
		seenSong[mixSongKey(r)] = true
		artistCounts[r.ArtistID]++
		out = append(out, r)
		return true
	}

	for _, r := range core {
		push(r, false) // the personal core is why this mix exists — never cap it
	}

	// Split the neighbor ranking into a trusted head and an exploration
	// tail; shuffle the tail day-stably and interleave ~1 exploration pick
	// per 3 head picks.
	headEnd := min(len(neighbors), tracksPerMix)
	head := neighbors[:headEnd]
	tail := append([]sqlc.ListArtistTopTracksForMixRow{}, neighbors[headEnd:]...)
	rng.Shuffle(len(tail), func(i, j int) { tail[i], tail[j] = tail[j], tail[i] })

	hi, ti := 0, 0
	for len(out) < tracksPerMix && (hi < len(head) || ti < len(tail)) {
		takeExploration := len(out)%4 == 3 // every 4th slot branches out
		if takeExploration && ti < len(tail) {
			if !push(tail[ti], true) {
				ti++
				continue
			}
			ti++
			continue
		}
		if hi < len(head) {
			if !push(head[hi], true) {
				hi++
				continue
			}
			hi++
			continue
		}
		if ti < len(tail) {
			if !push(tail[ti], true) {
				ti++
				continue
			}
			ti++
		}
	}
	return diversifyMixByArtist(out, tracksPerMix)
}

// mixSongKey collapses versions of the same song (artist + base title before
// any parenthetical/bracket suffix) so a mix never plays two remixes of one
// track.
func mixSongKey(r sqlc.ListArtistTopTracksForMixRow) string {
	title := strings.ToLower(strings.TrimSpace(r.TrackTitle))
	for _, sep := range []string{" (", " ["} {
		if i := strings.Index(title, sep); i > 0 {
			title = title[:i]
		}
	}
	return fmt.Sprintf("%d|%s", r.ArtistID, strings.TrimSpace(title))
}
