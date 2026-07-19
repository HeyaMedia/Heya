package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/textembed"
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
	candidateMetadata
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
	// GenreOverlap is only populated when the caller opted into
	// genre_affinity (see buildMusicRecommendationPool) — zero value means
	// "not computed", which is indistinguishable from a real zero-overlap
	// candidate, but that's fine: rankMusicRecommendationPool only reads it
	// when genreAffinity > 0, and genreAffinity > 0 is exactly the condition
	// buildMusicRecommendationPool used to decide whether to compute it.
	GenreOverlap float64
	// GenreDataKnown distinguishes a REAL overlap value (seed and candidate
	// both have genre data) from the neutralGenreOverlap stand-in assigned
	// when the candidate has no genre signal anywhere — the strict-mode drop
	// logic treats those two very differently.
	GenreDataKnown bool
}

type musicTasteProfile struct {
	SonicCentroid    pgvector.Vector
	MetadataCentroid pgvector.Vector
	ArtistIDs        []int64
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
		profile.SonicCentroid = centroid
	}

	var metadataCentroid pgvector.Vector
	err = a.db.QueryRow(ctx, `WITH `+musicAffinityCTE+`
		SELECT AVG(mrf.text_embedding)
		FROM aff
		JOIN tracks t ON t.id = aff.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN metadata_entity_bindings binding
		  ON binding.local_kind = 'track' AND binding.local_id = t.id
		 AND binding.entity_kind = 'recording'
		JOIN music_recording_facets mrf ON mrf.recording_entity_id = binding.entity_id
		WHERE aff.score > 0
		  AND mrf.text_embedding IS NOT NULL
		  AND mrf.embedder_version >= $2
		  AND `+musicVetoFilter, userID, int32(textembed.Version)).Scan(&metadataCentroid)
	if err == nil && len(metadataCentroid.Slice()) > 0 {
		profile.MetadataCentroid = metadataCentroid
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
	// Library Radio / For You station callers don't expose the genre_affinity
	// knob (that's a seed-radio-only dial) — always pass the no-op values.
	return a.recommendMusicAround(ctx, userID, profile.SonicCentroid, profile.MetadataCentroid, profile.ArtistIDs, mode, limit, exclude, 0, nil)
}

type recommendationMixRule struct {
	slug        string
	kind        string
	name        string
	description string
	mode        musicRecommendationMode
}

// variant is 0 for the normal day-stable rotation, or a non-zero value
// (folded into rankMusicRecommendationPool's dayBucket entropy) that lets an
// explicit regenerate request break same-day determinism.
func (a *App) generateRecommendationMixes(ctx context.Context, userID int64, maxMixes, tracksPerMix int, variant int64) ([]MusicMix, error) {
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
	// Profile archetypes don't use the genre_affinity knob (that's seed-radio
	// only) — 0/nil keeps buildMusicRecommendationPool's genre fetch skipped.
	pool, err := a.buildMusicRecommendationPool(ctx, userID, profile.SonicCentroid, profile.MetadataCentroid, profile.ArtistIDs, max(120, tracksPerMix*10), 1.0, 0, nil)
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if len(mixes) >= maxMixes {
			break
		}
		tracks := rankMusicRecommendationPool(pool, userID, rule.mode, tracksPerMix, used, variant, 0)
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

// recommendMusicAround is the shared candidate/scoring core. The independent
// sonic/metadata centroids and artist IDs may come from the user profile or an
// instant-radio seed. Any source may be empty; provider popularity is the final
// cold-start fallback.
// genreAffinity/seedGenreProfile are the RadioRequest.GenreAffinity knob's
// plumbing — see genreAffinityScoreScale's doc comment below for the
// formula. Every non-radio caller passes 0/nil, which is a guaranteed no-op.
func (a *App) recommendMusicAround(
	ctx context.Context,
	userID int64,
	sonicCentroid pgvector.Vector,
	metadataCentroid pgvector.Vector,
	artistIDs []int64,
	mode musicRecommendationMode,
	limit int,
	exclude []int64,
	genreAffinity float64,
	seedGenreProfile map[string]float64,
) ([]sqlc.ListArtistTopTracksForMixRow, error) {
	if limit <= 0 {
		return []sqlc.ListArtistTopTracksForMixRow{}, nil
	}
	affinityWeight := 1.0
	if mode == recommendRadio {
		affinityWeight = 0.22
	}
	pool, err := a.buildMusicRecommendationPool(ctx, userID, sonicCentroid, metadataCentroid, artistIDs, max(120, limit*10), affinityWeight, genreAffinity, seedGenreProfile)
	if err != nil {
		return nil, err
	}
	// Radio/station callers don't have a regenerate concept, so variant is
	// always 0 here — the head/tail exploration split still rotates day to
	// day off dayBucket alone.
	return rankMusicRecommendationPool(pool, userID, mode, limit, exclude, 0, genreAffinity), nil
}

// buildMusicRecommendationPool performs candidate retrieval once. Generated
// mix slates reuse the resulting pool across every archetype; instant radio
// builds one seed-specific pool. This keeps the model genuinely shared and
// avoids repeating the same HNSW/provider queries four times per slate.
// genreAffinity/seedGenreProfile: when genreAffinity > 0 and the seed
// profile is non-empty, every candidate gets a batched GenreOverlap score
// against seedGenreProfile (see genreHistogramOverlap). genreAffinity <= 0
// or an empty seed profile skips the extra fetch entirely — the additive
// no-op at genre_affinity=0 costs nothing, not just "scores nothing".
func (a *App) buildMusicRecommendationPool(
	ctx context.Context,
	userID int64,
	sonicCentroid pgvector.Vector,
	metadataCentroid pgvector.Vector,
	artistIDs []int64,
	poolLimit int,
	affinityWeight float64,
	genreAffinity float64,
	seedGenreProfile map[string]float64,
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

	if len(sonicCentroid.Slice()) > 0 {
		rows, err := a.tasteNeighborTracks(ctx, userID, sonicCentroid, poolLimit)
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

	if len(metadataCentroid.Slice()) > 0 {
		rows, err := a.metadataNeighborTracks(ctx, userID, metadataCentroid, poolLimit)
		if err != nil {
			return nil, fmt.Errorf("metadata candidates: %w", err)
		}
		denom := math.Max(1, float64(len(rows)-1))
		for i, row := range rows {
			strength := 4.0 - 3.1*(float64(i)/denom)
			add(row, candidateMetadata, strength)
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

	return a.finalizeMusicCandidates(ctx, userID, candidates, genreAffinity, seedGenreProfile)
}

// finalizeMusicCandidates attaches per-user candidate state and (when the
// genre knob is active) genre overlap to a built candidate map, flattening it
// for ranking. Shared by the blended profile pool above and the sonic-first
// seeded-radio pool below.
func (a *App) finalizeMusicCandidates(ctx context.Context, userID int64, candidates map[int64]*musicRecommendationCandidate, genreAffinity float64, seedGenreProfile map[string]float64) ([]musicRecommendationCandidate, error) {
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

	if genreAffinity > 0 && len(seedGenreProfile) > 0 {
		genreProfiles, err := a.candidateGenreProfiles(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("candidate genre profiles: %w", err)
		}
		for i := range flat {
			if cp, ok := genreProfiles[flat[i].Track.TrackID]; ok {
				flat[i].GenreOverlap = genreHistogramOverlap(seedGenreProfile, cp)
				flat[i].GenreDataKnown = true
			} else {
				// No genre signal anywhere for this track (neither
				// album.genres nor track_facets.top_genres) — neutralGenreOverlap's
				// doc comment explains why this must not be 0.
				flat[i].GenreOverlap = neutralGenreOverlap
			}
		}
	}
	return flat, nil
}

// buildSeededRadioPool is the sonic-first candidate retrieval for explicit
// seed radio (BuildRadio / the Mix Builder's manual tab). It deliberately
// diverges from buildMusicRecommendationPool because a seeded build answers a
// different question — "more that sounds like THESE" — where the blended
// profile pool answers "what would this user enjoy today":
//
//   - Per-seed KNN, never an averaged centroid. Averaging six different
//     bands' embeddings lands on a point that sounds like none of them (the
//     exact whole-artist-mush failure mix-rules-plan documents); instead each
//     seed contributes its own neighborhood and a track near several seeds
//     accumulates score across them.
//   - No affinity candidate source. Injecting the user's own unrelated
//     favorites into an explicit seed radio is taste bleed, not relevance.
//   - Metadata text-embedding neighbors are a FALLBACK only (unanalysed
//     seeds), not a peer source — text-tag adjacency is far noisier than
//     acoustic adjacency and was polluting seeded queues.
//   - The external similar-artist graph stays: it is anchored to the seed
//     artists themselves and supplies legitimate variety.
//   - Provider popularity remains the cold-library floor, unchanged.
func (a *App) buildSeededRadioPool(
	ctx context.Context,
	userID int64,
	seedEmbeddings []pgvector.Vector,
	metadataCentroid pgvector.Vector,
	artistIDs []int64,
	poolLimit int,
	genreAffinity float64,
	seedGenreProfile map[string]float64,
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

	perSeed := max(80, poolLimit/max(1, len(seedEmbeddings)))
	sonicCount := 0
	for _, emb := range seedEmbeddings {
		if len(emb.Slice()) == 0 {
			continue
		}
		rows, err := a.tasteNeighborTracks(ctx, userID, emb, perSeed)
		if err != nil {
			return nil, fmt.Errorf("seed sonic candidates: %w", err)
		}
		denom := math.Max(1, float64(len(rows)-1))
		for i, row := range rows {
			strength := 4.5 - 3.6*(float64(i)/denom)
			add(row, candidateSonic, strength)
			sonicCount++
		}
	}

	if len(artistIDs) > 0 {
		rows, err := a.externalMusicCandidates(ctx, userID, artistIDs, poolLimit)
		if err != nil {
			return nil, fmt.Errorf("provider candidates: %w", err)
		}
		for _, row := range rows {
			source := candidateExternal
			if row.chartRank > 0 {
				source |= candidateProviderChart
			}
			score := row.relevance * 2.7
			if row.chartRank > 0 {
				score += 1.8 / math.Sqrt(float64(max(1, row.chartRank)))
			}
			add(row.track, source, score)
		}
	}

	// Text-embedding fallback: only when the seeds' own acoustic neighborhood
	// couldn't fill a meaningful pool (unanalysed seeds).
	if sonicCount < poolLimit/3 && len(metadataCentroid.Slice()) > 0 {
		rows, err := a.metadataNeighborTracks(ctx, userID, metadataCentroid, poolLimit)
		if err != nil {
			return nil, fmt.Errorf("metadata candidates: %w", err)
		}
		denom := math.Max(1, float64(len(rows)-1))
		for i, row := range rows {
			strength := 4.0 - 3.1*(float64(i)/denom)
			add(row, candidateMetadata, strength)
		}
	}

	if len(candidates) < 40 {
		rows, err := a.popularMusicCandidates(ctx, userID, poolLimit)
		if err != nil {
			return nil, fmt.Errorf("popular candidates: %w", err)
		}
		for i, row := range rows {
			strength := 1.6 - 0.8*(float64(i)/math.Max(1, float64(len(rows))))
			add(row, candidatePopular|candidateProviderChart, strength)
		}
	}

	return a.finalizeMusicCandidates(ctx, userID, candidates, genreAffinity, seedGenreProfile)
}

// recommendSeededRadio ranks a seeded-radio pool. Radio has no regenerate
// concept, so variant stays 0 — rotation comes from the day bucket alone.
func (a *App) recommendSeededRadio(ctx context.Context, userID int64, seedEmbeddings []pgvector.Vector, metadataCentroid pgvector.Vector, artistIDs []int64, limit int, exclude []int64, genreAffinity float64, seedGenreProfile map[string]float64) ([]sqlc.ListArtistTopTracksForMixRow, error) {
	if limit <= 0 {
		return []sqlc.ListArtistTopTracksForMixRow{}, nil
	}
	pool, err := a.buildSeededRadioPool(ctx, userID, seedEmbeddings, metadataCentroid, artistIDs, max(120, limit*10), genreAffinity, seedGenreProfile)
	if err != nil {
		return nil, err
	}
	return rankMusicRecommendationPool(pool, userID, recommendRadio, limit, exclude, 0, genreAffinity), nil
}

func rankMusicRecommendationPool(pool []musicRecommendationCandidate, userID int64, mode musicRecommendationMode, limit int, exclude []int64, variant int64, genreAffinity float64) []sqlc.ListArtistTopTracksForMixRow {
	excluded := make(map[int64]bool, len(exclude))
	for _, id := range exclude {
		excluded[id] = true
	}
	now := time.Now()
	// variant folds into the day bucket so an explicit regenerate (non-zero
	// variant) can break same-day determinism; normal callers pass 0 and get
	// the existing stable-until-tomorrow behavior.
	dayBucket := (now.Unix() / 86400) ^ variant

	eligible := make([]musicRecommendationCandidate, 0, len(pool))
	for _, base := range pool {
		if excluded[base.Track.TrackID] || !musicCandidateEligible(base, mode, now) {
			continue
		}
		eligible = append(eligible, base)
	}

	// genreAffinityScoreScale's doc comment has the full formula and
	// invariants. genreAffinity <= 0 leaves dropZeroOverlap false and the
	// score term below inert, so this is a true no-op for every caller that
	// doesn't pass a positive genreAffinity (i.e. everyone except seed radio
	// with the knob turned up).
	dropZeroOverlap := false
	dropNeutral := false
	if genreAffinity >= genreAffinityDropThreshold {
		overlapping := 0
		realMatches := 0
		for _, c := range eligible {
			if c.GenreOverlap > 0 {
				overlapping++
			}
			if c.GenreDataKnown && c.GenreOverlap > 0 {
				realMatches++
			}
		}
		dropZeroOverlap = overlapping >= limit
		// When enough candidates GENUINELY share the seed's genres, strict
		// mode also sheds the no-genre-data crowd — their neutral stand-in
		// overlap exists to avoid emptying sparse pools, not to let unknown
		// tracks ride into a queue that has real matches to spare.
		dropNeutral = realMatches >= limit
	}

	ranked := make([]musicRecommendationCandidate, 0, len(eligible))
	for _, base := range eligible {
		if dropZeroOverlap && base.GenreDataKnown && base.GenreOverlap == 0 {
			continue
		}
		if dropNeutral && !base.GenreDataKnown {
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
		if genreAffinity > 0 {
			candidate.Score += genreAffinity * genreAffinityScoreScale * candidate.GenreOverlap
		}
		// Small deterministic scoring jitter still breaks ties between
		// near-identical candidates. It's intentionally too small to be the
		// rotation mechanism on its own (score spreads of several points
		// swamp it) — selectMusicRecommendations' head/tail exploration
		// sampling below is what actually makes the rail visibly rotate.
		candidate.Score += musicRotationJitter(userID, candidate.Track.TrackID, string(mode), dayBucket) * 0.35
		ranked = append(ranked, candidate)
	}
	explorationRng := rand.New(rand.NewSource(musicExplorationSeed(userID, mode, dayBucket))) //nolint:gosec // rotation, not crypto
	return selectMusicRecommendations(ranked, limit, mode, explorationRng)
}

// musicExplorationSeed derives a stable rng seed from (userID, mode,
// dayBucket) — the same triple named in the mix-rules-plan doc — so the
// exploration-tail shuffle in selectMusicRecommendations rotates once a day
// per user/mode and immediately on an explicit regenerate (dayBucket already
// carries the folded-in variant).
func musicExplorationSeed(userID int64, mode musicRecommendationMode, dayBucket int64) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%s:%d", userID, mode, dayBucket)
	return int64(h.Sum64()) //nolint:gosec // rotation seed, not crypto
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

// explorationTailWindow bounds how far past the deterministic head
// selectMusicRecommendations will look for exploration-tail picks — roughly
// ranks 20-60 for a typical ~30-track mix (head ends at `limit`, tail runs
// `limit`..`limit+explorationTailWindow`). Deep enough to feel like a real
// alternate pick, shallow enough that everything sampled is still a
// plausible recommendation rather than pool dregs.
const explorationTailWindow = 40

// selectMusicRecommendations applies product-independent safety rails:
// recording/version dedupe, a soft artist cap, artist adjacency avoidance,
// and a familiar/discovery blend for the main For You stream. It then fills
// slots from a head/tail split — ~80% from the deterministic score-ranked
// head, ~20% sampled from the deeper exploration tail via rng — the same
// pattern proven in assembleTasteMix, so For You/Discovery/Rediscover/Deep
// Cuts/Radio all visibly rotate day to day instead of re-surfacing the exact
// same score order (the jitter in rankMusicRecommendationPool alone is too
// small to do that once score spreads exceed a couple points). rng may be
// nil (tests exercising a fixed order) — the tail is simply left unshuffled.
func selectMusicRecommendations(candidates []musicRecommendationCandidate, limit int, mode musicRecommendationMode, rng *rand.Rand) []sqlc.ListArtistTopTracksForMixRow {
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

	// Split into the deterministic head (top `limit`) and the exploration
	// tail (the next explorationTailWindow candidates beyond it), shuffled
	// with the caller's seeded rng. Anything deeper than that is `rest` —
	// untouched extra fill material for narrow libraries, same as before
	// this change.
	headEnd := min(len(candidates), limit)
	head := candidates[:headEnd]
	tailEnd := min(len(candidates), headEnd+explorationTailWindow)
	tail := append([]musicRecommendationCandidate{}, candidates[headEnd:tailEnd]...)
	if mode == recommendRadio {
		// Explicit seed radio: exploration may only branch within the
		// seed-anchored sources (per-seed sonic KNN, similar-artist graph).
		// The deep pool is where fallback text/popularity candidates live,
		// and sampling those into a "sounds like these seeds" queue is how
		// unrelated tracks were leaking in.
		anchored := tail[:0]
		for _, c := range tail {
			if c.Sources&(candidateSonic|candidateExternal) != 0 {
				anchored = append(anchored, c)
			}
		}
		tail = anchored
	}
	if rng != nil && len(tail) > 1 {
		rng.Shuffle(len(tail), func(i, j int) { tail[i], tail[j] = tail[j], tail[i] })
	}
	rest := candidates[tailEnd:]

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

	// Interleave head and tail: 1 in 5 output slots (~20%) draws from the
	// shuffled exploration tail, the rest come from the deterministic head
	// in score order — mirrors assembleTasteMix's "every Nth slot branches
	// out" pattern.
	hi, ti := 0, 0
	for len(selected) < limit && (hi < len(head) || ti < len(tail)) {
		takeExploration := ti < len(tail) && len(selected)%5 == 4
		if takeExploration {
			if !push(tail[ti], true) {
				deferred = append(deferred, tail[ti])
			}
			ti++
			continue
		}
		if hi < len(head) {
			if !push(head[hi], true) {
				deferred = append(deferred, head[hi])
			}
			hi++
			continue
		}
		if ti < len(tail) {
			if !push(tail[ti], true) {
				deferred = append(deferred, tail[ti])
			}
			ti++
		}
	}
	// Still short (narrow head/tail pool)? Fall through to the untouched
	// deeper pool before relaxing any constraints.
	for _, candidate := range rest {
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
