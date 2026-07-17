package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/pgvector/pgvector-go"
)

// The music recommender is deliberately separate from the movie/TV engine.
// Music has a much better domain-specific similarity model (CLAP), reactions
// at three catalog levels, completion events, provider charts, and an
// external similar-artist graph. One candidate pipeline combines those
// signals; mixes, discovery, quick radio, and on-demand radio are just
// different selection policies over the same pool.

type musicRecommendationMode string

const (
	recommendForYou     musicRecommendationMode = "for_you"
	recommendDiscovery  musicRecommendationMode = "discovery"
	recommendDeepCuts   musicRecommendationMode = "deep_cuts"
	recommendRediscover musicRecommendationMode = "rediscover"
	recommendRadio      musicRecommendationMode = "radio"
)

type musicCandidateSource uint8

const (
	candidateSonic musicCandidateSource = 1 << iota
	candidateExternal
	candidateProviderChart
	candidateAffinity
	candidatePopular
)

type musicCandidateState struct {
	Affinity    float64
	TrackKnown  bool
	ArtistKnown bool
	LastPlayed  time.Time
}

type musicRecommendationCandidate struct {
	Track   sqlc.ListArtistTopTracksForMixRow
	Score   float64
	Sources musicCandidateSource
	State   musicCandidateState
}

type musicTasteProfile struct {
	Centroid  pgvector.Vector
	ArtistIDs []int64
}

// musicTasteProfile combines every positive signal into a user centroid and
// a ranked artist list. Explicit reactions dominate in musicAffinityCTE;
// completed listens can only add a small bounded amount.
func (a *App) musicTasteProfile(ctx context.Context, userID int64, artistLimit int) (musicTasteProfile, error) {
	seeds, err := a.tasteMixSeeds(ctx, userID, artistLimit)
	if err != nil {
		return musicTasteProfile{}, err
	}
	profile := musicTasteProfile{ArtistIDs: make([]int64, 0, len(seeds))}
	for _, seed := range seeds {
		profile.ArtistIDs = append(profile.ArtistIDs, seed.ArtistID)
	}

	var centroid pgvector.Vector
	err = a.db.QueryRow(ctx, `WITH `+musicAffinityCTE+`
		SELECT AVG(tf.track_embedding)
		FROM aff
		JOIN tracks t ON t.id = aff.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN track_facets tf ON tf.track_id = t.id
		WHERE aff.score > 0
		  AND tf.track_embedding IS NOT NULL
		  AND `+musicVetoFilter, userID).Scan(&centroid)
	if err == nil && len(centroid.Slice()) > 0 {
		profile.Centroid = centroid
	}
	// A profile with only unanalysed signals is still useful: the provider
	// graph and chart candidates below do not need an embedding.
	return profile, nil
}

func (a *App) recommendMusicForUser(ctx context.Context, userID int64, mode musicRecommendationMode, limit int, exclude []int64) ([]sqlc.ListArtistTopTracksForMixRow, error) {
	profile, err := a.musicTasteProfile(ctx, userID, 16)
	if err != nil {
		return nil, fmt.Errorf("taste profile: %w", err)
	}
	return a.recommendMusicAround(ctx, userID, profile.Centroid, profile.ArtistIDs, mode, limit, exclude)
}

type recommendationMixRule struct {
	slug        string
	kind        string
	name        string
	description string
	mode        musicRecommendationMode
}

func (a *App) generateRecommendationMixes(ctx context.Context, userID int64, maxMixes, tracksPerMix int) ([]MusicMix, error) {
	profile, err := a.musicTasteProfile(ctx, userID, 16)
	if err != nil {
		return nil, err
	}
	rules := []recommendationMixRule{
		{
			slug: "for-you", kind: "for_you", name: "For You", mode: recommendForYou,
			description: "Favorites, recent completions, sonic neighbors, and trusted catalog picks in one daily blend.",
		},
		{
			slug: "new-discoveries", kind: "discovery", name: "New Discoveries", mode: recommendDiscovery,
			description: "Unplayed tracks near your taste, with extra room for artists you have not heard yet.",
		},
		{
			slug: "time-capsule", kind: "rediscovery", name: "Time Capsule", mode: recommendRediscover,
			description: "Loved and completed tracks that have been out of rotation for a while.",
		},
		{
			slug: "deep-cuts", kind: "deep_cuts", name: "Deep Cuts", mode: recommendDeepCuts,
			description: "Unplayed corners of artists already in your taste profile.",
		},
	}

	mixes := make([]MusicMix, 0, min(maxMixes, len(rules)))
	used := make([]int64, 0, tracksPerMix*len(rules))
	pool, err := a.buildMusicRecommendationPool(ctx, userID, profile.Centroid, profile.ArtistIDs, max(120, tracksPerMix*10), 1.0)
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if len(mixes) >= maxMixes {
			break
		}
		tracks := rankMusicRecommendationPool(pool, userID, rule.mode, tracksPerMix, used)
		if len(tracks) < 5 {
			continue
		}
		mixes = append(mixes, MusicMix{
			Slug: rule.slug, Kind: rule.kind, Name: rule.name,
			Description: rule.description, Tracks: tracks,
		})
		for _, track := range tracks {
			used = append(used, track.TrackID)
		}
	}
	return mixes, nil
}

// recommendMusicAround is the shared candidate/scoring core. centroid and
// artistIDs may come from the user profile (For You/mixes) or the requested
// seed (instant radio). Either may be empty; provider popularity is the final
// cold-start fallback.
func (a *App) recommendMusicAround(
	ctx context.Context,
	userID int64,
	centroid pgvector.Vector,
	artistIDs []int64,
	mode musicRecommendationMode,
	limit int,
	exclude []int64,
) ([]sqlc.ListArtistTopTracksForMixRow, error) {
	if limit <= 0 {
		return []sqlc.ListArtistTopTracksForMixRow{}, nil
	}
	affinityWeight := 1.0
	if mode == recommendRadio {
		affinityWeight = 0.22
	}
	pool, err := a.buildMusicRecommendationPool(ctx, userID, centroid, artistIDs, max(120, limit*10), affinityWeight)
	if err != nil {
		return nil, err
	}
	return rankMusicRecommendationPool(pool, userID, mode, limit, exclude), nil
}

// buildMusicRecommendationPool performs candidate retrieval once. Generated
// mix slates reuse the resulting pool across every archetype; instant radio
// builds one seed-specific pool. This keeps the model genuinely shared and
// avoids repeating the same HNSW/provider queries four times per slate.
func (a *App) buildMusicRecommendationPool(
	ctx context.Context,
	userID int64,
	centroid pgvector.Vector,
	artistIDs []int64,
	poolLimit int,
	affinityWeight float64,
) ([]musicRecommendationCandidate, error) {
	candidates := map[int64]*musicRecommendationCandidate{}
	add := func(row sqlc.ListArtistTopTracksForMixRow, source musicCandidateSource, score float64) {
		candidate := candidates[row.TrackID]
		if candidate == nil {
			candidate = &musicRecommendationCandidate{Track: row}
			candidates[row.TrackID] = candidate
		}
		candidate.Sources |= source
		candidate.Score += score
	}

	if len(centroid.Slice()) > 0 {
		rows, err := a.tasteNeighborTracks(ctx, userID, centroid, poolLimit)
		if err != nil {
			return nil, fmt.Errorf("sonic candidates: %w", err)
		}
		denom := math.Max(1, float64(len(rows)-1))
		for i, row := range rows {
			// Rank is used instead of the raw distance because it is stable
			// across analyzer/model versions and still preserves KNN order.
			strength := 4.5 - 3.6*(float64(i)/denom)
			add(row, candidateSonic, strength)
		}
	}

	if len(artistIDs) > 0 {
		rows, err := a.externalMusicCandidates(ctx, userID, artistIDs, poolLimit)
		if err != nil {
			return nil, fmt.Errorf("provider candidates: %w", err)
		}
		for _, row := range rows {
			source := candidateExternal
			if row.chartRank < 10000 {
				source |= candidateProviderChart
			}
			score := row.relevance * 2.7
			if row.chartRank < 10000 {
				score += 1.8 / math.Sqrt(float64(max(1, row.chartRank)))
			}
			add(row.track, source, score)
		}
	}

	// Always retrieve known positives. Discovery/deep-cuts filter them during
	// selection; rediscovery needs them; radio applies a deliberately smaller
	// affinity weight so the seed remains in charge.
	rows, err := a.positiveMusicCandidates(ctx, userID, poolLimit)
	if err != nil {
		return nil, fmt.Errorf("affinity candidates: %w", err)
	}
	for _, row := range rows {
		add(row.track, candidateAffinity, math.Min(8, row.affinity)*affinityWeight)
	}

	// A brand-new user, an unanalysed seed, or a server whose external graph
	// has not localized yet still receives a useful queue from provider chart
	// matches and enriched album/artist popularity.
	if len(candidates) < max(40, poolLimit/5) {
		rows, err := a.popularMusicCandidates(ctx, userID, poolLimit)
		if err != nil {
			return nil, fmt.Errorf("popular candidates: %w", err)
		}
		for i, row := range rows {
			strength := 1.6 - 0.8*(float64(i)/math.Max(1, float64(len(rows))))
			add(row, candidatePopular|candidateProviderChart, strength)
		}
	}

	ids := make([]int64, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	states, err := a.musicCandidateStates(ctx, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("candidate state: %w", err)
	}

	flat := make([]musicRecommendationCandidate, 0, len(candidates))
	for id, ptr := range candidates {
		ptr.State = states[id]
		flat = append(flat, *ptr)
	}
	return flat, nil
}

func rankMusicRecommendationPool(pool []musicRecommendationCandidate, userID int64, mode musicRecommendationMode, limit int, exclude []int64) []sqlc.ListArtistTopTracksForMixRow {
	excluded := make(map[int64]bool, len(exclude))
	for _, id := range exclude {
		excluded[id] = true
	}
	now := time.Now()
	dayBucket := now.Unix() / 86400
	ranked := make([]musicRecommendationCandidate, 0, len(pool))
	for _, base := range pool {
		if excluded[base.Track.TrackID] || !musicCandidateEligible(base, mode, now) {
			continue
		}
		candidate := base
		if candidate.State.TrackKnown {
			candidate.Score += 0.8
		} else {
			candidate.Score += 0.45
		}
		if !candidate.State.ArtistKnown {
			candidate.Score += 0.35
			if mode == recommendDiscovery {
				candidate.Score += 1.0
			}
		}
		if mode == recommendRediscover && !candidate.State.LastPlayed.IsZero() {
			ageMonths := now.Sub(candidate.State.LastPlayed).Hours() / (24 * 30)
			candidate.Score += math.Min(2.5, ageMonths/6)
		}
		// Small deterministic exploration jitter rotates equal candidates
		// daily without making a refresh reshuffle the queue underneath users.
		candidate.Score += musicRotationJitter(userID, candidate.Track.TrackID, string(mode), dayBucket) * 0.35
		ranked = append(ranked, candidate)
	}
	return selectMusicRecommendations(ranked, limit, mode)
}

func musicCandidateEligible(candidate musicRecommendationCandidate, mode musicRecommendationMode, now time.Time) bool {
	switch mode {
	case recommendDiscovery:
		return !candidate.State.TrackKnown && candidate.State.Affinity <= 0
	case recommendDeepCuts:
		return candidate.State.ArtistKnown && !candidate.State.TrackKnown && candidate.State.Affinity <= 0
	case recommendRediscover:
		if candidate.State.Affinity <= 0 {
			return false
		}
		return candidate.State.LastPlayed.IsZero() || now.Sub(candidate.State.LastPlayed) >= 45*24*time.Hour
	default:
		return true
	}
}

// selectMusicRecommendations applies product-independent safety rails:
// recording/version dedupe, a soft artist cap, artist adjacency avoidance,
// and a familiar/discovery blend for the main For You stream.
func selectMusicRecommendations(candidates []musicRecommendationCandidate, limit int, mode musicRecommendationMode) []sqlc.ListArtistTopTracksForMixRow {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Track.TrackID < candidates[j].Track.TrackID
		}
		return candidates[i].Score > candidates[j].Score
	})

	if mode == recommendForYou {
		known := make([]musicRecommendationCandidate, 0, len(candidates))
		fresh := make([]musicRecommendationCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.State.TrackKnown || candidate.State.Affinity > 0 {
				known = append(known, candidate)
			} else {
				fresh = append(fresh, candidate)
			}
		}
		// Roughly 60% familiar / 40% fresh when both pools can support it.
		blended := make([]musicRecommendationCandidate, 0, len(candidates))
		ki, fi := 0, 0
		for ki < len(known) || fi < len(fresh) {
			for n := 0; n < 3 && ki < len(known); n++ {
				blended = append(blended, known[ki])
				ki++
			}
			for n := 0; n < 2 && fi < len(fresh); n++ {
				blended = append(blended, fresh[fi])
				fi++
			}
		}
		candidates = blended
	}

	seenTrack := map[int64]bool{}
	seenSong := map[string]bool{}
	artistCounts := map[int64]int{}
	artistCap := max(2, limit/7)
	selected := make([]sqlc.ListArtistTopTracksForMixRow, 0, limit)
	deferred := make([]musicRecommendationCandidate, 0)
	previousArtist := int64(0)

	push := func(candidate musicRecommendationCandidate, enforceCap bool) bool {
		row := candidate.Track
		if seenTrack[row.TrackID] || seenSong[mixSongKey(row)] {
			return false
		}
		if enforceCap && artistCounts[row.ArtistID] >= artistCap {
			return false
		}
		if len(selected) > 0 && row.ArtistID == previousArtist {
			return false
		}
		seenTrack[row.TrackID] = true
		seenSong[mixSongKey(row)] = true
		artistCounts[row.ArtistID]++
		selected = append(selected, row)
		previousArtist = row.ArtistID
		return true
	}

	for _, candidate := range candidates {
		if len(selected) >= limit {
			break
		}
		if !push(candidate, true) {
			deferred = append(deferred, candidate)
		}
	}
	// Narrow libraries should still fill. First relax adjacency while keeping
	// the artist cap, then relax the cap as a last resort.
	for pass := 0; pass < 2 && len(selected) < limit; pass++ {
		for _, candidate := range deferred {
			if len(selected) >= limit {
				break
			}
			row := candidate.Track
			if seenTrack[row.TrackID] || seenSong[mixSongKey(row)] {
				continue
			}
			if pass == 0 && artistCounts[row.ArtistID] >= artistCap {
				continue
			}
			seenTrack[row.TrackID] = true
			seenSong[mixSongKey(row)] = true
			artistCounts[row.ArtistID]++
			selected = append(selected, row)
			previousArtist = row.ArtistID
		}
	}
	return selected
}

type scoredMixRow struct {
	track    sqlc.ListArtistTopTracksForMixRow
	affinity float64
}

func (a *App) positiveMusicCandidates(ctx context.Context, userID int64, limit int) ([]scoredMixRow, error) {
	rows, err := a.db.Query(ctx, `WITH `+musicAffinityCTE+`
		SELECT t.id, t.title, t.duration, t.disc_number, t.track_number,
		       al.id, al.title, al.slug, al.cover_path, al.year,
		       ar.id, ar.name, mi.slug,
		       (SELECT count(*) FROM play_events pe WHERE pe.track_id = t.id AND pe.completed),
		       aff.score
		FROM aff
		JOIN tracks t ON t.id = aff.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		JOIN media_item_cards mi ON mi.id = ar.media_item_id
		WHERE aff.score > 0
		  AND `+musicVetoFilter+`
		  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
		              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
		ORDER BY aff.score DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]scoredMixRow, 0)
	for rows.Next() {
		var row scoredMixRow
		if err := scanScoredMixRow(rows, &row.track, &row.affinity); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

type externalMixRow struct {
	track     sqlc.ListArtistTopTracksForMixRow
	relevance float64
	chartRank int
}

func (a *App) externalMusicCandidates(ctx context.Context, userID int64, artistIDs []int64, limit int) ([]externalMixRow, error) {
	rows, err := a.db.Query(ctx, `WITH
		seed AS (
			SELECT artist_id,
			       GREATEST(0.45, 1.05 - (ord - 1)::float8 * 0.04) AS relevance,
			       false AS is_related
			FROM unnest($2::bigint[]) WITH ORDINALITY AS s(artist_id, ord)
		),
		related_raw AS (
			SELECT artist_id, relevance, is_related FROM seed
			UNION ALL
			SELECT asa.local_artist_id,
			       (CASE asa.provider WHEN 'lastfm' THEN 1.0 WHEN 'deezer' THEN 0.95
			             WHEN 'apple' THEN 0.95 WHEN 'tidal' THEN 0.92 ELSE 0.85 END)
			       * (1.0 / (1.0 + asa.rank::float8 * 0.08))
			       + LEAST(1.0, asa.match_score::float8) * 0.2,
			       true
			FROM seed s
			JOIN artist_similar_artists asa ON asa.artist_id = s.artist_id
			WHERE asa.local_artist_id IS NOT NULL
		),
		related AS (
			SELECT artist_id, MAX(relevance)::float8 AS relevance,
			       bool_or(is_related) AS is_related
			FROM related_raw
			GROUP BY artist_id
		)
		SELECT picked.track_id, picked.track_title, picked.duration,
		       picked.disc_number, picked.track_number,
		       picked.album_id, picked.album_title, picked.album_slug,
		       picked.album_cover_path, picked.album_year,
		       picked.artist_id, picked.artist_name, picked.artist_slug,
		       (SELECT count(*) FROM play_events pe WHERE pe.track_id = picked.track_id AND pe.completed),
		       related.relevance, picked.chart_rank
		FROM related
		CROSS JOIN LATERAL (
			SELECT t.id AS track_id, t.title AS track_title, t.duration,
			       t.disc_number, t.track_number,
			       al.id AS album_id, al.title AS album_title, al.slug AS album_slug,
			       al.cover_path AS album_cover_path, al.year AS album_year,
			       ar.id AS artist_id, ar.name AS artist_name, mi.slug AS artist_slug,
			       COALESCE((
					SELECT MIN(CASE WHEN att.provider_rank > 0 THEN att.provider_rank ELSE att.rank END)
					FROM artist_top_tracks att
					WHERE att.artist_id = ar.id
					  AND ((att.mbid <> '' AND att.mbid = t.recording_mbid)
					       OR lower(att.title) = lower(t.title))
			       ), 10000)::int AS chart_rank
			FROM tracks t
			JOIN albums al ON al.id = t.album_id
			JOIN artists ar ON ar.id = al.artist_id
			JOIN media_item_cards mi ON mi.id = ar.media_item_id
			WHERE al.artist_id = related.artist_id
			  AND `+musicVetoFilter+`
			  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
			              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
			ORDER BY chart_rank ASC, al.popularity DESC, al.playcount DESC, t.id ASC
			LIMIT 10
		) picked
		ORDER BY related.relevance DESC, picked.chart_rank ASC
		LIMIT $3`, userID, artistIDs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]externalMixRow, 0)
	for rows.Next() {
		var row externalMixRow
		if err := scanScoredMixRow(rows, &row.track, &row.relevance, &row.chartRank); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (a *App) popularMusicCandidates(ctx context.Context, userID int64, limit int) ([]sqlc.ListArtistTopTracksForMixRow, error) {
	return a.scanMixRows(ctx, `WITH popular_albums AS (
		SELECT al.id
		FROM albums al
		JOIN artists ar ON ar.id = al.artist_id
		ORDER BY (al.popularity + ar.popularity) DESC,
		         (al.playcount + ar.playcount) DESC
		LIMIT 250
	)
	SELECT t.id, t.title, t.duration, t.disc_number, t.track_number,
	       al.id, al.title, al.slug, al.cover_path, al.year,
	       ar.id, ar.name, mi.slug,
	       (SELECT count(*) FROM play_events pe WHERE pe.track_id = t.id AND pe.completed)
	FROM popular_albums pa
	JOIN albums al ON al.id = pa.id
	JOIN tracks t ON t.album_id = al.id
	JOIN artists ar ON ar.id = al.artist_id
	JOIN media_item_cards mi ON mi.id = ar.media_item_id
	WHERE `+musicVetoFilter+`
	  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
	              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
	ORDER BY COALESCE((
		SELECT MIN(CASE WHEN att.provider_rank > 0 THEN att.provider_rank ELSE att.rank END)
		FROM artist_top_tracks att
		WHERE att.artist_id = ar.id
		  AND ((att.mbid <> '' AND att.mbid = t.recording_mbid)
		       OR lower(att.title) = lower(t.title))
	), 10000), (al.popularity + ar.popularity) DESC, (al.playcount + ar.playcount) DESC
	LIMIT $2`, userID, limit)
}

func (a *App) musicCandidateStates(ctx context.Context, userID int64, ids []int64) (map[int64]musicCandidateState, error) {
	out := make(map[int64]musicCandidateState, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := a.db.Query(ctx, `WITH `+musicAffinityCTE+`,
		known_artists AS (
			SELECT DISTINCT al.artist_id
			FROM aff
			JOIN tracks t ON t.id = aff.track_id
			JOIN albums al ON al.id = t.album_id
			WHERE aff.score > 0
			UNION
			SELECT artist_id FROM user_artist_ratings WHERE user_id = $1 AND rating > 3
			UNION
			SELECT DISTINCT al.artist_id
			FROM user_album_ratings uar
			JOIN albums al ON al.id = uar.album_id
			WHERE uar.user_id = $1 AND uar.rating > 3
		)
		SELECT t.id, COALESCE(aff.score, 0)::float8,
		       EXISTS (SELECT 1 FROM play_events pe WHERE pe.user_id = $1 AND pe.track_id = t.id AND pe.completed),
		       EXISTS (SELECT 1 FROM known_artists ka WHERE ka.artist_id = al.artist_id),
		       (SELECT max(pe.played_at) FROM play_events pe WHERE pe.user_id = $1 AND pe.track_id = t.id AND pe.completed)
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		LEFT JOIN aff ON aff.track_id = t.id
		WHERE t.id = ANY($2::bigint[])`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var state musicCandidateState
		var last pgtype.Timestamptz
		if err := rows.Scan(&id, &state.Affinity, &state.TrackKnown, &state.ArtistKnown, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			state.LastPlayed = last.Time
		}
		out[id] = state
	}
	return out, rows.Err()
}

func (a *App) vetoedMusicTrackSet(ctx context.Context, userID int64, ids []int64) (map[int64]bool, error) {
	vetoed := make(map[int64]bool)
	if len(ids) == 0 {
		return vetoed, nil
	}
	rows, err := a.db.Query(ctx, `
		SELECT t.id
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		WHERE t.id = ANY($2::bigint[])
		  AND NOT (`+musicVetoFilter+`)`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		vetoed[id] = true
	}
	return vetoed, rows.Err()
}

// scanScoredMixRow scans the common 14-column playable track projection and
// then any caller-specific score columns.
func scanScoredMixRow(rows interface{ Scan(...any) error }, track *sqlc.ListArtistTopTracksForMixRow, extras ...any) error {
	dest := []any{
		&track.TrackID, &track.TrackTitle, &track.Duration, &track.DiscNumber, &track.TrackNumber,
		&track.AlbumID, &track.AlbumTitle, &track.AlbumSlug, &track.AlbumCoverPath, &track.AlbumYear,
		&track.ArtistID, &track.ArtistName, &track.ArtistSlug, &track.PlayCount,
	}
	dest = append(dest, extras...)
	return rows.Scan(dest...)
}

func musicRotationJitter(userID, trackID int64, mode string, dayBucket int64) float64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%d:%s:%d", userID, trackID, mode, dayBucket)
	return float64(h.Sum64()%10000) / 10000
}
