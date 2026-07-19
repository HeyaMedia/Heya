package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"
	"time"
	"unicode"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/pgvector/pgvector-go"
)

// Genre mix archetype — docs/mix-rules-plan.md layer-1 #2: a "<Genre> Mix"
// seeded from the user's top 1-2 genres by RECENT genre affinity (the same
// musicAffinityCTE signal every other archetype uses, aggregated against
// track/album genre membership instead of artist/track identity). Fill is
// high-affinity in-genre tracks + sonically-adjacent in-genre KNN neighbors,
// with a discovery quota of never-played in-genre tracks. This file hand-
// rolls its own SQL (like music_mixes_taste.go) rather than using sqlc,
// because every query here needs to interpolate the shared musicAffinityCTE
// Go string constant — sqlc has no way to splice a Go-side constant into a
// generated query.

const (
	// genreMixMinAffinityTracks is the sparse gate: fewer than this many
	// distinct genre-tagged affinity tracks (summed across ALL genres) means
	// the user's history is too thin to name a genre honestly yet — a brand
	// new account with 3 plays shouldn't get a confident "Jazz Mix".
	genreMixMinAffinityTracks = 8

	// genreMixMinShare is the flat gate: a genre must carry at least this
	// fraction of the user's total genre-affinity mass to be named the seed
	// of a mix. Below it the distribution is too spread out (a dozen
	// near-equal genres) to single one out as "what you're into lately".
	// 0.12 was picked so a user split fairly evenly across up to ~8 genres
	// still clears it for their top pick(s), but someone smeared across 15+
	// tags with no real lean does not get a mix at all.
	genreMixMinShare = 0.12
)

// userGenreAffinity is one genre's aggregated affinity score for a user —
// see the doc comment on the method of the same purpose below.
type userGenreAffinity struct {
	Genre string
	Score float64
}

// genreMixTrackRow is a KNN neighbor candidate carrying the two bits of
// per-user state assembleGenreMix needs beyond the base track: whether the
// user already has any affinity for it (irrelevant to bucket choice, kept
// for potential future tie-breaking) and whether they have ever played it
// at all (the discovery-quota split).
type genreMixTrackRow struct {
	Track       sqlc.ListArtistTopTracksForMixRow
	Affinity    float64
	NeverPlayed bool
}

// genreMembershipSQL is the "does track t (with joined album al) belong to
// genre $N" predicate shared by every query in this file: al.genres array
// membership (broad coverage) OR a track_facets.top_genres entry with that
// name (~18% coverage, adds precision where analysis exists). paramIndex
// lets each caller place it at whatever positional-argument slot its own
// query needs. Uses a correlated EXISTS against a fresh track_facets alias
// so it is self-contained whether or not the caller's FROM clause already
// joins track_facets.
func genreMembershipSQL(paramIndex int) string {
	return fmt.Sprintf(`(al.genres @> ARRAY[$%d::text]
		OR EXISTS (
			SELECT 1 FROM track_facets tf_genre, jsonb_array_elements(COALESCE(tf_genre.top_genres, '[]'::jsonb)) elem
			WHERE tf_genre.track_id = t.id AND (elem->>'name') = $%d::text
		))`, paramIndex, paramIndex)
}

// userGenreAffinity aggregates the shared per-user affinity signal
// (musicAffinityCTE — recency-decayed completions + explicit reactions)
// against genre membership: every genre a scored track carries earns that
// track's affinity score (album.genres, flat) or affinity*confidence
// (track_facets.top_genres). Returns genres sorted by score desc, plus the
// count of distinct affinity-scored tracks that carry ANY genre tag at all
// — genreMixCandidates' sparse gate reads that count directly instead of
// re-deriving it from the per-genre rows (a 3-genre track would otherwise
// be triple-counted).
func (a *App) userGenreAffinity(ctx context.Context, userID int64) ([]userGenreAffinity, int, error) {
	rows, err := a.db.Query(ctx, `WITH `+musicAffinityCTE+`,
		track_genre AS (
			SELECT t.id AS track_id, genre_name::text AS genre, 1.0::float8 AS weight
			FROM tracks t
			JOIN albums al ON al.id = t.album_id
			CROSS JOIN LATERAL unnest(al.genres) AS genre_name
			WHERE genre_name <> ''
			UNION ALL
			SELECT tf.track_id, (elem->>'name')::text AS genre, COALESCE((elem->>'score')::float8, 0) AS weight
			FROM track_facets tf
			CROSS JOIN LATERAL jsonb_array_elements(COALESCE(tf.top_genres, '[]'::jsonb)) AS elem
			WHERE COALESCE((elem->>'name')::text, '') <> ''
		),
		genre_track_count AS (
			SELECT count(DISTINCT aff.track_id) AS n
			FROM aff
			JOIN track_genre tg ON tg.track_id = aff.track_id
			WHERE aff.score > 0
		)
		SELECT tg.genre, SUM(tg.weight * aff.score)::float8 AS score,
		       (SELECT n FROM genre_track_count) AS total_tracks
		FROM aff
		JOIN track_genre tg ON tg.track_id = aff.track_id
		WHERE aff.score > 0
		GROUP BY tg.genre
		ORDER BY score DESC`, userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []userGenreAffinity
	var totalTracks int
	for rows.Next() {
		var g userGenreAffinity
		if err := rows.Scan(&g.Genre, &g.Score, &totalTracks); err != nil {
			return nil, 0, err
		}
		out = append(out, g)
	}
	return out, totalTracks, rows.Err()
}

// genreMixCandidates picks up to 2 genres (already sorted desc by score)
// that clear both the sparse and flat gates. Split out from
// generateGenreMixes so the gating math is unit-testable without a DB.
func genreMixCandidates(affinities []userGenreAffinity, totalTracks int) []userGenreAffinity {
	if totalTracks < genreMixMinAffinityTracks || len(affinities) == 0 {
		return nil
	}
	var total float64
	for _, g := range affinities {
		total += g.Score
	}
	if total <= 0 {
		return nil
	}
	var out []userGenreAffinity
	for _, g := range affinities {
		if len(out) >= 2 {
			break
		}
		if g.Score/total < genreMixMinShare {
			break // sorted desc — once one genre misses the bar, so does everything after it
		}
		out = append(out, g)
	}
	return out
}

// generateGenreMixes is Mix Rules Plan layer-1 archetype #2: a "<Genre>
// Mix" per genre returned by genreMixCandidates. exclude is the accumulated
// track-id set from every mix already assembled earlier in this slate
// (GenerateMixesForUser) — extended after each genre mix too, so two genre
// mixes in the same slate can't repeat a track either.
func (a *App) generateGenreMixes(ctx context.Context, userID int64, maxMixes, tracksPerMix int, variant int64, exclude []int64) ([]MusicMix, error) {
	if maxMixes <= 0 {
		return nil, nil
	}
	affinities, totalTracks, err := a.userGenreAffinity(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("genre affinity: %w", err)
	}
	picks := genreMixCandidates(affinities, totalTracks)
	if len(picks) == 0 {
		return nil, nil
	}

	// Day-bucketed exploration seed, variant-foldable exactly like every
	// other archetype (generateRecommendationMixes, generateTasteMixes).
	dayBucket := (time.Now().Unix() / 86400) ^ variant

	excludeSet := make(map[int64]bool, len(exclude))
	for _, id := range exclude {
		excludeSet[id] = true
	}

	mixes := make([]MusicMix, 0, min(maxMixes, len(picks)))
	for _, pick := range picks {
		if len(mixes) >= maxMixes {
			break
		}
		mix, err := a.buildGenreMix(ctx, userID, pick.Genre, tracksPerMix, dayBucket, excludeSet)
		if err != nil {
			return nil, fmt.Errorf("genre mix %q: %w", pick.Genre, err)
		}
		if mix == nil {
			continue
		}
		mixes = append(mixes, *mix)
		for _, track := range mix.Tracks {
			excludeSet[track.TrackID] = true
		}
	}
	return mixes, nil
}

// buildGenreMix assembles one genre's mix: personal core (the user's own
// high-affinity in-genre tracks) + KNN neighbors around the centroid of
// those same tracks, filtered to the genre and split into known/never-
// played pools by assembleGenreMix.
func (a *App) buildGenreMix(ctx context.Context, userID int64, genre string, tracksPerMix int, dayBucket int64, exclude map[int64]bool) (*MusicMix, error) {
	coreQuota := max(3, tracksPerMix/2)
	core, err := a.genreAffinityTracks(ctx, userID, genre, coreQuota*2)
	if err != nil {
		return nil, fmt.Errorf("core tracks: %w", err)
	}

	centroid, err := a.genreAffinityCentroid(ctx, userID, genre)
	if err != nil {
		return nil, fmt.Errorf("centroid: %w", err)
	}
	var neighbors []genreMixTrackRow
	if len(centroid.Slice()) > 0 {
		neighbors, err = a.genreNeighborTracks(ctx, userID, genre, centroid, tracksPerMix*4)
		if err != nil {
			return nil, fmt.Errorf("neighbors: %w", err)
		}
	}

	h := fnv.New64a()
	_, _ = h.Write([]byte(genre))
	rng := rand.New(rand.NewSource(int64(h.Sum64()) ^ userID ^ dayBucket)) //nolint:gosec // rotation, not crypto
	tracks := assembleGenreMix(core, neighbors, tracksPerMix, exclude, rng)
	if len(tracks) < 5 {
		return nil, nil // too thin to feel like a mix
	}

	title := titleCaseGenre(genre)
	return &MusicMix{
		Slug:        "genre-" + slug.Generate(genre, ""),
		Kind:        "genre",
		Description: "Built from your recent " + title + " listening, widened with sonic neighbors and a few new picks in the genre.",
		SeedGenre:   genre,
		Name:        title + " Mix",
		Tracks:      tracks,
	}, nil
}

// genreAffinityTracks returns the user's own in-genre tracks with positive
// affinity, strongest first — the personal core of the mix, mirroring
// tasteSeedTracks' artist-scoped counterpart in music_mixes_taste.go.
func (a *App) genreAffinityTracks(ctx context.Context, userID int64, genre string, limit int) ([]sqlc.ListArtistTopTracksForMixRow, error) {
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
		WHERE aff.score > 0
		  AND `+genreMembershipSQL(2)+`
		  AND `+musicVetoFilter+`
		  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
		              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
		ORDER BY aff.score DESC
		LIMIT $3`, userID, genre, limit)
}

// genreAffinityCentroid averages track_embedding over the user's positive
// in-genre tracks — the seed vector for genreNeighborTracks' KNN, mirroring
// tasteCentroids' artist-scoped counterpart.
func (a *App) genreAffinityCentroid(ctx context.Context, userID int64, genre string) (pgvector.Vector, error) {
	var centroid pgvector.Vector
	err := a.db.QueryRow(ctx, `WITH `+musicAffinityCTE+`
		SELECT AVG(tf.track_embedding)
		FROM aff
		JOIN tracks t ON t.id = aff.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN track_facets tf ON tf.track_id = t.id
		WHERE aff.score > 0
		  AND tf.track_embedding IS NOT NULL
		  AND `+genreMembershipSQL(2)+`
		  AND `+musicVetoFilter, userID, genre).Scan(&centroid)
	if err != nil {
		return pgvector.Vector{}, err
	}
	return centroid, nil
}

// genreNeighborTracks KNNs the genre centroid over per-track embeddings,
// restricted to tracks that also carry the genre, and reports per-row
// affinity + never-played so assembleGenreMix can split known-sonic-
// neighbor fill from the never-played discovery quota.
func (a *App) genreNeighborTracks(ctx context.Context, userID int64, genre string, centroid pgvector.Vector, fetch int) ([]genreMixTrackRow, error) {
	rows, err := a.db.Query(ctx, `WITH `+musicAffinityCTE+`
		SELECT t.id, t.title, t.duration, t.disc_number, t.track_number,
		       al.id, al.title, al.slug, al.cover_path, al.year,
		       ar.id, ar.name, mi.slug,
		       (SELECT count(*) FROM play_events pe WHERE pe.track_id = t.id AND pe.completed) AS play_count,
		       COALESCE(aff.score, 0)::float8 AS affinity,
		       NOT EXISTS (SELECT 1 FROM play_events pe2 WHERE pe2.user_id = $1 AND pe2.track_id = t.id) AS never_played
		FROM track_facets tf
		JOIN tracks t ON t.id = tf.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		JOIN media_item_cards mi ON mi.id = ar.media_item_id
		LEFT JOIN aff ON aff.track_id = t.id
		WHERE tf.track_embedding IS NOT NULL
		  AND `+genreMembershipSQL(4)+`
		  AND `+musicVetoFilter+`
		  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
		              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
		ORDER BY tf.track_embedding <=> $2
		LIMIT $3`, userID, centroid, fetch, genre)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []genreMixTrackRow
	for rows.Next() {
		var row genreMixTrackRow
		if err := rows.Scan(&row.Track.TrackID, &row.Track.TrackTitle, &row.Track.Duration,
			&row.Track.DiscNumber, &row.Track.TrackNumber,
			&row.Track.AlbumID, &row.Track.AlbumTitle, &row.Track.AlbumSlug, &row.Track.AlbumCoverPath, &row.Track.AlbumYear,
			&row.Track.ArtistID, &row.Track.ArtistName, &row.Track.ArtistSlug, &row.Track.PlayCount,
			&row.Affinity, &row.NeverPlayed); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// assembleGenreMix fills one genre mix from three pools: the user's own
// high-affinity in-genre tracks (core, never capped — same rationale as
// assembleTasteMix's core), sonically-adjacent in-genre KNN neighbors the
// user has already heard (fills the remaining non-discovery slots), and
// never-played in-genre neighbors (the discovery quota, ~1/4 of slots).
// exclude is every track already placed in an earlier mix this slate so
// nothing repeats across the payload; artistCap keeps this mix's own
// variety in check the same way every other archetype does.
func assembleGenreMix(core []sqlc.ListArtistTopTracksForMixRow, neighbors []genreMixTrackRow, tracksPerMix int, exclude map[int64]bool, rng *rand.Rand) []sqlc.ListArtistTopTracksForMixRow {
	discoveryQuota := max(1, tracksPerMix/4)
	seenTrack := map[int64]bool{}
	seenSong := map[string]bool{}
	artistCounts := map[int64]int{}
	artistCap := max(3, tracksPerMix/6)
	out := make([]sqlc.ListArtistTopTracksForMixRow, 0, tracksPerMix)
	discoveryCount := 0

	push := func(r sqlc.ListArtistTopTracksForMixRow, isDiscovery, capped bool) bool {
		if len(out) >= tracksPerMix || exclude[r.TrackID] || seenTrack[r.TrackID] || seenSong[mixSongKey(r)] {
			return false
		}
		if capped && artistCounts[r.ArtistID] >= artistCap {
			return false
		}
		seenTrack[r.TrackID] = true
		seenSong[mixSongKey(r)] = true
		artistCounts[r.ArtistID]++
		out = append(out, r)
		if isDiscovery {
			discoveryCount++
		}
		return true
	}

	// Personal core first, reserving room for the discovery quota — never
	// capped, it's why this mix exists.
	coreBudget := tracksPerMix - discoveryQuota
	for _, c := range core {
		if len(out) >= coreBudget {
			break
		}
		push(c, false, false)
	}

	var known, discovery []genreMixTrackRow
	for _, n := range neighbors {
		if n.NeverPlayed {
			discovery = append(discovery, n)
		} else {
			known = append(known, n)
		}
	}
	if len(discovery) > 1 {
		rng.Shuffle(len(discovery), func(i, j int) { discovery[i], discovery[j] = discovery[j], discovery[i] })
	}

	// Interleave: once the core is placed, every 4th slot (and anything once
	// the known pool runs dry) pulls from the discovery pool until its quota
	// is met — same ~25% exploration cadence as assembleTasteMix/
	// rankMusicRecommendationPool.
	ki, di := 0, 0
	for len(out) < tracksPerMix && (ki < len(known) || di < len(discovery)) {
		takeDiscovery := discoveryCount < discoveryQuota && di < len(discovery) &&
			(len(out)%4 == 3 || ki >= len(known))
		if takeDiscovery {
			push(discovery[di].Track, true, true)
			di++
			continue
		}
		if ki < len(known) {
			push(known[ki].Track, false, true)
			ki++
			continue
		}
		if di < len(discovery) {
			push(discovery[di].Track, true, true)
			di++
		}
	}
	return diversifyMixByArtist(out, tracksPerMix)
}

// titleCaseGenre turns a raw genre string ("hip hop", "ROCK") into display
// form ("Hip Hop", "Rock") for the mix title ("<Genre> Mix"). Deliberately
// simple word-capitalization — genre strings in this catalog are short,
// space-separated tags from album metadata or the sonic classifier, not
// prose that would need smarter title-casing rules (small-word lowercasing,
// etc). Classifier names using the Discogs "Parent---Child" separator are
// not special-cased; they title-case as one hyphenated-looking phrase.
func titleCaseGenre(genre string) string {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(genre)))
	for i, w := range words {
		r := []rune(w)
		if len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
