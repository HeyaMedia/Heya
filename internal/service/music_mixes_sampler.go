package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/slug"
)

// Library Sampler archetype — docs/mix-rules-plan.md cold ladder: when a user
// has little or no listening history, every other archetype has nothing to
// seed from (affinity picks the artists, genre affinity picks the genres,
// taste centroids anchor the KNN — all of it derives from plays/reactions).
// The sampler treats the library itself as the taste prior instead: someone
// curated this collection, so its genre composition is a legitimate signal.
// It seeds "<Genre> Sampler" tours from the catalog's dominant genres, filled
// by provider/catalog popularity with a deep-cut quota, and needs neither
// play history nor embeddings — which usually don't exist yet on a fresh
// install either (sonic analysis defaults off and takes time to pump).
//
// Devaluation is structural, in two stacked ways: the allowance ladder below
// shrinks the sampler's mix budget as the user's affinity signal grows, and
// the sampler runs LAST in GenerateMixesForUser, so it only ever fills slate
// slots the signal-driven archetypes left empty. Once listening data has
// trickled in, the personal archetypes claim the slate and the sampler
// retires without any explicit switch-off.

const (
	// samplerColdSignal / samplerSparseSignal ladder the sampler's mix budget
	// by the count of distinct positive-affinity tracks (the same signal that
	// gates the personal archetypes — when it's low, they can't fire, so the
	// sampler carries the slate; as it grows they take over):
	//   < samplerColdSignal    → cold:   up to 3 sampler mixes
	//   < samplerSparseSignal  → sparse: at most 1
	//   ≥ samplerSparseSignal  → rich:   0, fully retired
	samplerColdSignal   = 20
	samplerSparseSignal = 100

	// samplerMinGenreTracks is the bucket floor: a genre must have at least
	// this many eligible tracks in the library to seed a coherent tour —
	// below it a "Sampler" would just replay the whole bucket.
	samplerMinGenreTracks = 12
)

// samplerAllowance maps listening-signal strength (distinct tracks with
// positive affinity) to the sampler's mix budget. Split out for unit tests.
func samplerAllowance(signalTracks int) int {
	switch {
	case signalTracks < samplerColdSignal:
		return 3
	case signalTracks < samplerSparseSignal:
		return 1
	default:
		return 0
	}
}

// musicListeningSignal counts the user's distinct positive-affinity tracks —
// the sampler's devaluation input.
func (a *App) musicListeningSignal(ctx context.Context, userID int64) (int, error) {
	var n int
	err := a.db.QueryRow(ctx, `WITH `+musicAffinityCTE+`
		SELECT count(*) FROM aff WHERE aff.score > 0`, userID).Scan(&n)
	return n, err
}

// librarySamplerGenres returns the library's dominant genres by eligible
// track count (veto-filtered per user, live files only), largest first.
func (a *App) librarySamplerGenres(ctx context.Context, userID int64, limit int) ([]string, error) {
	rows, err := a.db.Query(ctx, `SELECT genre_name::text AS genre, count(DISTINCT t.id) AS n
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		CROSS JOIN LATERAL unnest(al.genres) AS genre_name
		WHERE genre_name <> ''
		  AND `+musicVetoFilter+`
		  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
		              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
		GROUP BY genre_name
		HAVING count(DISTINCT t.id) >= $3
		ORDER BY n DESC, genre ASC
		LIMIT $2`, userID, limit, samplerMinGenreTracks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var genre string
		var n int64
		if err := rows.Scan(&genre, &n); err != nil {
			return nil, err
		}
		out = append(out, genre)
	}
	return out, rows.Err()
}

// librarySamplerPool fetches in-genre eligible tracks ranked exactly like
// popularMusicCandidates (provider top-track rank first, then album+artist
// popularity/playcount) — the "what would anyone start with" ordering that
// needs no user signal.
func (a *App) librarySamplerPool(ctx context.Context, userID int64, genre string, fetch int) ([]sqlc.ListArtistTopTracksForMixRow, error) {
	return a.scanMixRows(ctx, `SELECT t.id, t.title, t.duration, t.disc_number, t.track_number,
		       al.id, al.title, al.slug, al.cover_path, al.year,
		       ar.id, ar.name, mi.slug,
		       (SELECT count(*) FROM play_events pe WHERE pe.track_id = t.id AND pe.completed)
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		JOIN media_item_cards mi ON mi.id = ar.media_item_id
		WHERE `+genreMembershipSQL(2)+`
		  AND `+musicVetoFilter+`
		  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
		              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
		ORDER BY COALESCE((
			SELECT MIN(CASE WHEN att.provider_rank > 0 THEN att.provider_rank ELSE att.rank END)
			FROM artist_top_tracks att
			WHERE att.artist_id = ar.id
			  AND ((att.mbid <> '' AND att.mbid = t.recording_mbid)
			       OR lower(att.title) = lower(t.title))
		), 10000), (al.popularity + ar.popularity) DESC, (al.playcount + ar.playcount) DESC
		LIMIT $3`, userID, genre, fetch)
}

// assembleSamplerMix fills one sampler mix from a popularity-ordered pool:
// the popular half of the pool carries ~3 of every 4 slots and the deep half
// (the library's hidden corners) the remaining quota, both day-shuffled so
// the tour rotates. Everything is artist-capped — unlike the personal
// archetypes there is no affinity core that earns an exemption.
func assembleSamplerMix(pool []sqlc.ListArtistTopTracksForMixRow, tracksPerMix int, exclude map[int64]bool, rng *rand.Rand) []sqlc.ListArtistTopTracksForMixRow {
	if len(pool) == 0 || tracksPerMix <= 0 {
		return nil
	}
	head := append([]sqlc.ListArtistTopTracksForMixRow(nil), pool[:(len(pool)+1)/2]...)
	tail := append([]sqlc.ListArtistTopTracksForMixRow(nil), pool[(len(pool)+1)/2:]...)
	rng.Shuffle(len(head), func(i, j int) { head[i], head[j] = head[j], head[i] })
	rng.Shuffle(len(tail), func(i, j int) { tail[i], tail[j] = tail[j], tail[i] })

	seenTrack := map[int64]bool{}
	seenSong := map[string]bool{}
	artistCounts := map[int64]int{}
	artistCap := max(3, tracksPerMix/6)
	out := make([]sqlc.ListArtistTopTracksForMixRow, 0, tracksPerMix)

	push := func(r sqlc.ListArtistTopTracksForMixRow) bool {
		if len(out) >= tracksPerMix || exclude[r.TrackID] || seenTrack[r.TrackID] || seenSong[mixSongKey(r)] {
			return false
		}
		if artistCounts[r.ArtistID] >= artistCap {
			return false
		}
		seenTrack[r.TrackID] = true
		seenSong[mixSongKey(r)] = true
		artistCounts[r.ArtistID]++
		out = append(out, r)
		return true
	}

	hi, ti := 0, 0
	for len(out) < tracksPerMix && (hi < len(head) || ti < len(tail)) {
		takeDeep := ti < len(tail) && (len(out)%4 == 3 || hi >= len(head))
		if takeDeep {
			push(tail[ti])
			ti++
			continue
		}
		if hi < len(head) {
			push(head[hi])
			hi++
			continue
		}
	}
	return diversifyMixByArtist(out, tracksPerMix)
}

// generateLibrarySamplerMixes is the cold-start floor of the slate. Returns
// nil without touching the DB further once the allowance ladder says the
// user's own signal is rich enough to carry the slate.
func (a *App) generateLibrarySamplerMixes(ctx context.Context, userID int64, maxMixes, tracksPerMix int, variant int64, exclude []int64) ([]MusicMix, error) {
	if maxMixes <= 0 {
		return nil, nil
	}
	signal, err := a.musicListeningSignal(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("listening signal: %w", err)
	}
	budget := min(samplerAllowance(signal), maxMixes)
	if budget <= 0 {
		return nil, nil
	}

	genres, err := a.librarySamplerGenres(ctx, userID, budget)
	if err != nil {
		return nil, fmt.Errorf("library genres: %w", err)
	}

	dayBucket := (time.Now().Unix() / 86400) ^ variant
	excludeSet := make(map[int64]bool, len(exclude))
	for _, id := range exclude {
		excludeSet[id] = true
	}

	mixes := make([]MusicMix, 0, budget)
	for _, genre := range genres {
		pool, err := a.librarySamplerPool(ctx, userID, genre, tracksPerMix*4)
		if err != nil {
			return nil, fmt.Errorf("sampler pool %q: %w", genre, err)
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte(genre))
		rng := rand.New(rand.NewSource(int64(h.Sum64()) ^ userID ^ dayBucket)) //nolint:gosec // rotation, not crypto
		tracks := assembleSamplerMix(pool, tracksPerMix, excludeSet, rng)
		if len(tracks) < 5 {
			continue
		}
		title := titleCaseGenre(genre)
		mixes = append(mixes, MusicMix{
			Slug:        "library-" + slug.Generate(genre, ""),
			Kind:        "library",
			SeedGenre:   genre,
			Name:        title + " Sampler",
			Description: "A tour through the " + title + " in your library — the obvious starting points plus a few hidden corners, rotating daily.",
			Tracks:      tracks,
		})
		for _, track := range tracks {
			excludeSet[track.TrackID] = true
		}
	}

	// A library with no usable genre metadata still deserves a first-day mix:
	// one whole-library tour over the same popularity ordering.
	if len(mixes) == 0 {
		pool, err := a.popularMusicCandidates(ctx, userID, tracksPerMix*4)
		if err != nil {
			return nil, fmt.Errorf("sampler fallback pool: %w", err)
		}
		rng := rand.New(rand.NewSource(userID ^ dayBucket)) //nolint:gosec // rotation, not crypto
		tracks := assembleSamplerMix(pool, tracksPerMix, excludeSet, rng)
		if len(tracks) >= 5 {
			mixes = append(mixes, MusicMix{
				Slug:        "library-sampler",
				Kind:        "library",
				Name:        "Library Sampler",
				Description: "A first tour through your library while Heya learns what you like.",
				Tracks:      tracks,
			})
		}
	}
	return mixes, nil
}
