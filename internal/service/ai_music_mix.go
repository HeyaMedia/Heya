package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/llm"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/rs/zerolog/log"
)

// AIMusicMixRequest is a free-form brief for the AI music director. The LLM
// interprets the brief, while CLAP and deterministic code keep every pick
// grounded in tracks that actually exist in the library.
type AIMusicMixRequest struct {
	Query string `json:"query" minLength:"2" maxLength:"2000" doc:"Narrative description of the desired mix"`
	Limit int    `json:"limit,omitempty" minimum:"5" maximum:"60" doc:"Number of tracks (default 30)"`
}

// AIMusicMixTrack is the rich playable track row returned to Mix Builder.
// Reason is the AI's short explanation; Distance is the best CLAP cosine
// distance observed across all generated probes (lower is closer).
type AIMusicMixTrack struct {
	TrackID        int64   `json:"track_id"`
	TrackTitle     string  `json:"track_title"`
	Duration       int32   `json:"duration"`
	DiscNumber     int32   `json:"disc_number"`
	TrackNumber    int32   `json:"track_number"`
	AlbumID        int64   `json:"album_id"`
	AlbumTitle     string  `json:"album_title"`
	AlbumSlug      string  `json:"album_slug"`
	AlbumCoverPath string  `json:"album_cover_path"`
	AlbumYear      string  `json:"album_year"`
	ArtistID       int64   `json:"artist_id"`
	ArtistName     string  `json:"artist_name"`
	ArtistSlug     string  `json:"artist_slug"`
	Distance       float32 `json:"distance"`
	Reason         string  `json:"reason,omitempty"`
}

type AIMusicMixResult struct {
	Title      string            `json:"title"`
	Summary    string            `json:"summary"`
	Probes     []string          `json:"probes" doc:"Acoustic CLAP searches derived from the brief"`
	Tracks     []AIMusicMixTrack `json:"tracks"`
	Model      string            `json:"model,omitempty"`
	Mode       string            `json:"mode"`
	DurationMs int64             `json:"duration_ms"`
}

const (
	aiMusicDefaultLimit = 30
	aiMusicMaxLimit     = 60
	aiMusicPerProbe     = 60
	aiMusicMaxPool      = 90
)

var aiMusicTemp = 0.25

var aiMusicPlanSchema = []byte(`{
	"type": "object",
	"properties": {
		"title": { "type": "string", "minLength": 2, "maxLength": 80 },
		"summary": { "type": "string", "minLength": 2, "maxLength": 240 },
		"arc": { "type": "string", "enum": ["steady", "rising", "waves", "cinematic"] },
		"probes": {
			"type": "array", "minItems": 2, "maxItems": 5,
			"items": { "type": "string", "minLength": 3, "maxLength": 120 }
		}
	},
	"required": ["title", "summary", "arc", "probes"],
	"additionalProperties": false
}`)

var aiMusicPicksSchema = []byte(`{
	"type": "object",
	"properties": {
		"picks": {
			"type": "array", "minItems": 1, "maxItems": 60,
			"items": {
				"type": "object",
				"properties": {
					"id": { "type": "integer" },
					"reason": { "type": "string", "maxLength": 80 }
				},
				"required": ["id", "reason"],
				"additionalProperties": false
			}
		}
	},
	"required": ["picks"],
	"additionalProperties": false
}`)

type aiMusicPlan struct {
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Arc     string   `json:"arc"`
	Probes  []string `json:"probes"`
}

type aiMusicPick struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

type aiMusicCandidate struct {
	Row          sqlc.SimilarTracksByTextRichRow
	BestDistance float32
	RankScore    float64
	ProbeHits    int
	BPM          *float32
	Genres       []string
	Moods        []string
}

// AIMusicMix turns a narrative brief into a grounded, ordered playlist:
//
//  1. LLM translates story/lore/reference language into acoustic CLAP probes.
//  2. CLAP text→audio KNN builds a real-library candidate pool.
//  3. LLM selects and sequences only ids from that pool.
//  4. Code validates, de-duplicates, diversifies, and fills to the requested
//     length if the small model returns too few picks.
func (a *App) AIMusicMix(ctx context.Context, userID int64, in AIMusicMixRequest) (AIMusicMixResult, error) {
	query := strings.TrimSpace(in.Query)
	if len(query) < 2 {
		return AIMusicMixResult{}, fmt.Errorf("mix brief is empty")
	}
	limit := in.Limit
	if limit < 5 {
		limit = aiMusicDefaultLimit
	}
	if limit > aiMusicMaxLimit {
		limit = aiMusicMaxLimit
	}
	if a.textSearcher == nil {
		return AIMusicMixResult{}, sonicanalysis.ErrTextSearcherUnavailable
	}

	settings := a.AISettings(ctx)
	client, model, err := a.aiClient(ctx, settings)
	if err != nil {
		return AIMusicMixResult{}, err
	}
	start := time.Now()

	planStarted := time.Now()
	plan := a.aiMusicMakePlan(ctx, client, model, query)
	planDuration := time.Since(planStarted)
	retrievalStarted := time.Now()
	candidates, err := a.aiMusicCandidatePool(ctx, plan.Probes, limit)
	if err != nil {
		return AIMusicMixResult{}, err
	}
	// Dislikes are law: drop thumbs-downed tracks before anything selects.
	if vetoed, err := a.DislikedTrackIDs(ctx, userID); err == nil && len(vetoed) > 0 {
		vetoSet := make(map[int64]bool, len(vetoed))
		for _, id := range vetoed {
			vetoSet[id] = true
		}
		kept := candidates[:0]
		for _, c := range candidates {
			if !vetoSet[c.Row.TrackID] {
				kept = append(kept, c)
			}
		}
		candidates = kept
	}
	retrievalDuration := time.Since(retrievalStarted)

	result := AIMusicMixResult{
		Title:   plan.Title,
		Summary: plan.Summary,
		Probes:  plan.Probes,
		Tracks:  []AIMusicMixTrack{},
		Mode:    settings.Mode,
		Model:   model,
	}
	if settings.Mode == "local" {
		result.Model = settings.LocalModel
	}
	if len(candidates) == 0 {
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// The plan is the model's whole job in local mode: a 4B generating 30
	// picks with reasons is ~40s of tokens on low-power hardware, while its
	// per-track judgment adds little over CLAP's ranking — and code sequences
	// an arc better than a small model does (it has the actual BPMs). Big
	// external models keep the curator turn: their tokens are fast and their
	// track sense is real.
	var curationDuration time.Duration
	if settings.Mode == "local" {
		result.Tracks = aiMusicDeterministicMix(candidates, plan.Arc, limit)
	} else {
		var selected struct {
			Picks []aiMusicPick `json:"picks"`
		}
		curationStarted := time.Now()
		err = client.CompleteJSON(ctx, llm.Request{
			Model:       model,
			Temperature: &aiMusicTemp,
			MaxTokens:   max(700, limit*24),
			Messages: []llm.Message{
				{Role: "system", Content: aiMusicCuratorSystem(limit)},
				{Role: "user", Content: aiMusicCuratorUser(query, plan, candidates, limit)},
			},
		}, "music_mix_picks", aiMusicPicksSchema, &selected)
		curationDuration = time.Since(curationStarted)
		if err != nil {
			// Retrieval already did the expensive semantic work. A weak JSON turn
			// should degrade to the ranked CLAP pool, not throw the whole mix away.
			log.Warn().Err(err).
				Int("candidates", len(candidates)).
				Dur("plan", planDuration).
				Dur("retrieval", retrievalDuration).
				Dur("curation", curationDuration).
				Msg("ai music mix: curation failed — using CLAP-ranked fallback")
		}
		result.Tracks = disposeAIMusicPicks(candidates, selected.Picks, limit)
	}

	result.DurationMs = time.Since(start).Milliseconds()
	log.Info().
		Str("mode", settings.Mode).
		Int("candidates", len(candidates)).
		Int("tracks", len(result.Tracks)).
		Dur("plan", planDuration).
		Dur("retrieval", retrievalDuration).
		Dur("curation", curationDuration).
		Int64("total_ms", result.DurationMs).
		Msg("ai music mix: complete")
	return result, nil
}

// aiMusicDeterministicMix is the local-mode DJ: select the strongest diverse
// candidates from the CLAP ranking, sequence them to the requested energy arc
// by BPM, and derive each reason from the track's own moods/genres. Zero
// model tokens.
func aiMusicDeterministicMix(candidates []aiMusicCandidate, arc string, limit int) []AIMusicMixTrack {
	selected := selectAIMusicCandidates(candidates, limit)
	sequenced := aiMusicSequenceByArc(selected, arc)
	tracks := make([]AIMusicMixTrack, 0, len(sequenced))
	for _, candidate := range sequenced {
		tracks = append(tracks, aiMusicTrackFromCandidate(candidate, aiMusicDerivedReason(candidate)))
	}
	return tracks
}

// aiMusicDistanceMargin is the junk-tail cut for the deterministic path: a
// candidate whose best CLAP distance is this far past the pool's best is a
// weak sonic match (the tail the LLM curator used to filter). Soft — the
// fill passes relax it rather than under-fill the mix.
const aiMusicDistanceMargin = 0.12

// selectAIMusicCandidates applies the dispose diversity rules (dedup, artist
// cap) plus a relative distance cutoff to the ranked pool without an LLM pick
// list. Same-artist adjacency is NOT enforced here — the arc sequencer
// reorders, so adjacency is fixed after.
func selectAIMusicCandidates(candidates []aiMusicCandidate, limit int) []aiMusicCandidate {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}
	best := candidates[0].BestDistance
	for _, candidate := range candidates {
		if candidate.BestDistance < best {
			best = candidate.BestDistance
		}
	}
	cutoff := best + aiMusicDistanceMargin
	artistCap := max(2, int(math.Ceil(float64(limit)/8)))
	seenRecording := map[string]bool{}
	seenSong := map[string]bool{}
	artistCounts := map[int64]int{}
	out := make([]aiMusicCandidate, 0, limit)
	passes := []struct{ enforceCap, enforceCutoff bool }{
		{true, true},   // ideal: strong matches, diverse artists
		{true, false},  // narrow sonic slice — admit weaker matches
		{false, false}, // narrow library — relax the artist cap too
	}
	for _, pass := range passes {
		for _, candidate := range candidates {
			r := candidate.Row
			if seenRecording[aiMusicRecordingKey(r)] {
				continue
			}
			// One version per song: "X (Original Mix)" and "X (Club Remix)"
			// share a base title — the LLM curator used to filter these.
			if seenSong[aiMusicSongKey(r)] {
				continue
			}
			if pass.enforceCutoff && candidate.BestDistance > cutoff {
				continue
			}
			if pass.enforceCap && artistCounts[r.ArtistID] >= artistCap {
				continue
			}
			seenRecording[aiMusicRecordingKey(r)] = true
			seenSong[aiMusicSongKey(r)] = true
			artistCounts[r.ArtistID]++
			out = append(out, candidate)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// aiMusicSongKey identifies a song across versions: artist + the title before
// any parenthetical/bracketed mix suffix.
func aiMusicSongKey(row sqlc.SimilarTracksByTextRichRow) string {
	title := strings.ToLower(strings.TrimSpace(row.TrackTitle))
	for _, sep := range []string{" (", " ["} {
		if i := strings.Index(title, sep); i > 0 {
			title = title[:i]
		}
	}
	return fmt.Sprintf("%d|%s", row.ArtistID, strings.TrimSpace(title))
}

// aiMusicSequenceByArc orders the selected tracks into the plan's energy arc
// using BPM as the energy proxy (missing BPM sorts to the middle), then
// breaks up same-artist adjacency with forward swaps.
func aiMusicSequenceByArc(tracks []aiMusicCandidate, arc string) []aiMusicCandidate {
	if len(tracks) < 3 {
		return tracks
	}
	byBPM := func(items []aiMusicCandidate) {
		median := aiMusicMedianBPM(items)
		sort.SliceStable(items, func(i, j int) bool {
			return aiMusicEnergy(items[i], median) < aiMusicEnergy(items[j], median)
		})
	}
	out := append([]aiMusicCandidate{}, tracks...)
	switch arc {
	case "rising":
		byBPM(out)
	case "waves":
		// Three passes: up, down, up — each roughly a third of the set.
		byBPM(out)
		third := len(out) / 3
		reverseMusicChunk(out[third : 2*third])
	case "cinematic":
		// Rise to a peak around two-thirds in, then wind down: even indices
		// ascend to the peak, odd indices descend after it.
		byBPM(out)
		asc := make([]aiMusicCandidate, 0, (len(out)+1)/2)
		desc := make([]aiMusicCandidate, 0, len(out)/2)
		for i, candidate := range out {
			if i%2 == 0 {
				asc = append(asc, candidate)
			} else {
				desc = append(desc, candidate)
			}
		}
		reverseMusicChunk(desc)
		seq := make([]aiMusicCandidate, 0, len(out))
		seq = append(seq, asc...)
		seq = append(seq, desc...)
		out = seq
	default: // "steady" — keep the CLAP-affinity ranking as the flow
	}
	// Same-artist adjacency fix-up: swap the offender forward with the next
	// track by a different artist.
	for i := 1; i < len(out); i++ {
		if out[i].Row.ArtistID != out[i-1].Row.ArtistID {
			continue
		}
		for j := i + 1; j < len(out); j++ {
			if out[j].Row.ArtistID != out[i-1].Row.ArtistID {
				out[i], out[j] = out[j], out[i]
				break
			}
		}
	}
	return out
}

func aiMusicEnergy(candidate aiMusicCandidate, median float64) float64 {
	if candidate.BPM != nil {
		return float64(*candidate.BPM)
	}
	return median
}

func aiMusicMedianBPM(tracks []aiMusicCandidate) float64 {
	bpms := make([]float64, 0, len(tracks))
	for _, candidate := range tracks {
		if candidate.BPM != nil {
			bpms = append(bpms, float64(*candidate.BPM))
		}
	}
	if len(bpms) == 0 {
		return 0
	}
	sort.Float64s(bpms)
	return bpms[len(bpms)/2]
}

func reverseMusicChunk(chunk []aiMusicCandidate) {
	for i, j := 0, len(chunk)-1; i < j; i, j = i+1, j-1 {
		chunk[i], chunk[j] = chunk[j], chunk[i]
	}
}

// aiMusicDerivedReason builds the per-track line from the track's own sonic
// tags — free, honest, and no model tokens spent on 30 reason strings.
func aiMusicDerivedReason(candidate aiMusicCandidate) string {
	parts := make([]string, 0, 3)
	seen := map[string]bool{}
	push := func(tag string) {
		if tag == "" || seen[strings.ToLower(tag)] || len(parts) >= 3 {
			return
		}
		seen[strings.ToLower(tag)] = true
		parts = append(parts, tag)
	}
	for _, mood := range candidate.Moods {
		push(cleanMusicMoodTag(mood))
	}
	for _, genre := range candidate.Genres {
		push(cleanMusicGenreTag(genre))
	}
	if len(parts) == 0 {
		return "Strong sonic match"
	}
	return strings.Join(parts, " · ")
}

// cleanMusicMoodTag turns classifier tag slugs into display words:
// "mood_happy" → "happy", "danceability" → "danceable".
func cleanMusicMoodTag(tag string) string {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "mood_")
	switch tag {
	case "danceability":
		return "danceable"
	case "voice":
		return "vocal"
	}
	return tag
}

// cleanMusicGenreTag keeps the most specific segment of a hierarchical
// Discogs genre: "Electronic---Trance" → "Trance".
func cleanMusicGenreTag(tag string) string {
	if i := strings.LastIndex(tag, "---"); i >= 0 {
		tag = tag[i+3:]
	}
	return strings.TrimSpace(tag)
}

func (a *App) aiMusicMakePlan(ctx context.Context, client llm.Completer, model, query string) aiMusicPlan {
	var plan aiMusicPlan
	err := client.CompleteJSON(ctx, llm.Request{
		Model:       model,
		Temperature: &aiMusicTemp,
		MaxTokens:   256,
		Messages: []llm.Message{
			{Role: "system", Content: aiMusicPlannerSystem()},
			{Role: "user", Content: "Mix brief:\n" + query},
		},
	}, "music_mix_plan", aiMusicPlanSchema, &plan)
	if a.AISettings(ctx).Mode == "local" {
		a.llmLocal.Touch()
	}
	if err != nil {
		log.Warn().Err(err).Msg("ai music mix: planning failed — searching the raw brief")
		return aiMusicPlan{
			Title:   "AI Mix",
			Summary: "A mix shaped directly from your brief.",
			Arc:     "waves",
			Probes:  []string{query},
		}
	}
	plan.Title = strings.TrimSpace(plan.Title)
	plan.Summary = strings.TrimSpace(plan.Summary)
	plan.Probes = normalizeMusicProbes(plan.Probes, query)
	return plan
}

func aiMusicPlannerSystem() string {
	return "You are a music supervisor translating an imaginative scene into acoustic search language for a CLAP text-to-audio index. " +
		"The index understands how music sounds: instrumentation, genre, intensity, rhythm, production, vocals, atmosphere, and emotional energy. " +
		"Translate lore and narrative into sound. Example: a Starfleet crew fighting the Borg with a Doom reference means punishing industrial metal, djent riffs, martial percussion, ominous sci-fi synths, and escalating battle energy — not songs whose titles literally mention space. " +
		"Write 3-5 distinct probes of 6-14 words each that cover the core sound plus useful adjacent angles. Reference a known soundtrack, artist, or game only when it clarifies the sound, and always include descriptive acoustic terms. " +
		"The title should feel like a real mixtape title. The summary is one short sentence. Choose an arc: steady, rising, waves, or cinematic."
}

func aiMusicCuratorSystem(limit int) string {
	return fmt.Sprintf("You are sequencing a %d-track mixtape from candidates retrieved by CLAP audio similarity. ", limit) +
		"Use only candidate ids. Select exactly the requested number when enough candidates exist. " +
		"Honor the brief as a sonic and emotional direction, not as a literal title-matching exercise. " +
		"Prefer strong fits, but make the set feel authored: varied artists, no duplicate versions of the same song, and no same artist back-to-back. " +
		"Order the tracks to follow the requested energy arc; BPM is context, not a rigid DJ constraint. " +
		"Each reason is at most eight words and describes why the track belongs, without mentioning CLAP, ids, scores, candidates, or the listener."
}

func aiMusicCuratorUser(query string, plan aiMusicPlan, candidates []aiMusicCandidate, limit int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Original brief: %s\n", query)
	fmt.Fprintf(&b, "Mix title: %s\nDirection: %s\nEnergy arc: %s\n", plan.Title, plan.Summary, plan.Arc)
	fmt.Fprintf(&b, "Choose and order %d tracks. Candidates:\n", min(limit, len(candidates)))
	for _, candidate := range candidates {
		r := candidate.Row
		fmt.Fprintf(&b, "id=%d | %s — %s", r.TrackID, r.ArtistName, r.TrackTitle)
		if candidate.BPM != nil {
			fmt.Fprintf(&b, " | bpm=%.0f", *candidate.BPM)
		}
		if len(candidate.Genres) > 0 {
			fmt.Fprintf(&b, " | genres=%s", strings.Join(candidate.Genres, ", "))
		}
		if len(candidate.Moods) > 0 {
			fmt.Fprintf(&b, " | moods=%s", strings.Join(candidate.Moods, ", "))
		}
		fmt.Fprintf(&b, " | affinity=%.3f | probe_hits=%d\n", 1-float64(candidate.BestDistance), candidate.ProbeHits)
	}
	return b.String()
}

func normalizeMusicProbes(probes []string, fallback string) []string {
	out := make([]string, 0, 5)
	seen := map[string]bool{}
	for _, probe := range probes {
		probe = strings.TrimSpace(probe)
		key := strings.ToLower(probe)
		if len(probe) < 3 || seen[key] || len(out) >= 5 {
			continue
		}
		seen[key] = true
		out = append(out, probe)
	}
	if len(out) == 0 {
		out = append(out, strings.TrimSpace(fallback))
	}
	return out
}

func (a *App) aiMusicCandidatePool(ctx context.Context, probes []string, limit int) ([]aiMusicCandidate, error) {
	byID := make(map[int64]*aiMusicCandidate)
	for probeIndex, probe := range probes {
		hits, err := a.SearchMusicByText(ctx, probe, aiMusicPerProbe)
		if err != nil {
			return nil, fmt.Errorf("CLAP search %q: %w", probe, err)
		}
		probeWeight := 1.0
		if probeIndex == 0 {
			probeWeight = 1.15
		}
		for rank, hit := range hits {
			candidate, ok := byID[hit.TrackID]
			if !ok {
				candidate = &aiMusicCandidate{Row: hit, BestDistance: hit.Distance}
				byID[hit.TrackID] = candidate
			}
			candidate.ProbeHits++
			candidate.RankScore += probeWeight / float64(rank+6)
			if hit.Distance < candidate.BestDistance {
				candidate.BestDistance = hit.Distance
				candidate.Row = hit
			}
		}
	}

	if err := a.aiMusicHydrateCandidates(ctx, byID); err != nil {
		return nil, err
	}
	candidates := make([]aiMusicCandidate, 0, len(byID))
	for _, candidate := range byID {
		if candidate.BPM == nil && candidate.Genres == nil && candidate.Moods == nil {
			// Hydration only visits playable current-library tracks. An entirely
			// untouched candidate is stale/deleted and must not reach the model.
			continue
		}
		candidates = append(candidates, *candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].RankScore == candidates[j].RankScore {
			return candidates[i].BestDistance < candidates[j].BestDistance
		}
		return candidates[i].RankScore > candidates[j].RankScore
	})

	// Collapse reissues/duplicate recordings after ranking so the best CLAP
	// match survives. The schema already uses this identity elsewhere.
	deduped := make([]aiMusicCandidate, 0, len(candidates))
	seenRecording := map[string]bool{}
	for _, candidate := range candidates {
		key := aiMusicRecordingKey(candidate.Row)
		if seenRecording[key] {
			continue
		}
		seenRecording[key] = true
		deduped = append(deduped, candidate)
	}
	poolLimit := aiMusicCandidatePoolLimit(limit)
	if len(deduped) > poolLimit {
		deduped = deduped[:poolLimit]
	}
	return deduped, nil
}

// A local 4B model does not benefit from reading four candidates per desired
// track. A tighter, higher-ranked pool leaves enough room for diversity while
// roughly halving prompt evaluation time for the default 30-track mix.
func aiMusicCandidatePoolLimit(limit int) int {
	return min(aiMusicMaxPool, max(48, limit*2))
}

func (a *App) aiMusicHydrateCandidates(ctx context.Context, candidates map[int64]*aiMusicCandidate) error {
	ids := make([]int64, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := a.db.Query(ctx, `
		SELECT tf.track_id, tf.bpm, COALESCE(tf.top_genres, '[]'::jsonb), COALESCE(tf.mood_tags, '{}'::jsonb)
		FROM track_facets tf
		JOIN tracks t ON t.id=tf.track_id
		WHERE tf.track_id=ANY($1)
		  AND EXISTS (
			SELECT 1 FROM track_files f JOIN library_files lf ON lf.id=f.library_file_id
			WHERE f.track_id=t.id AND lf.deleted_at IS NULL
		  )`, ids)
	if err != nil {
		return fmt.Errorf("hydrate music candidates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var bpm pgtype.Float4
		var genresRaw, moodsRaw []byte
		if err := rows.Scan(&id, &bpm, &genresRaw, &moodsRaw); err != nil {
			return err
		}
		candidate := candidates[id]
		if candidate == nil {
			continue
		}
		if bpm.Valid && !float32Invalid(bpm.Float32) {
			value := bpm.Float32
			candidate.BPM = &value
		}
		candidate.Genres = topMusicGenres(genresRaw, 2)
		candidate.Moods = topMusicMoods(moodsRaw, 2)
		// Non-nil empty slices mark this as a playable hydrated row.
		if candidate.Genres == nil {
			candidate.Genres = []string{}
		}
		if candidate.Moods == nil {
			candidate.Moods = []string{}
		}
	}
	return rows.Err()
}

func float32Invalid(v float32) bool {
	f := float64(v)
	return math.IsNaN(f) || math.IsInf(f, 0)
}

func topMusicGenres(raw []byte, limit int) []string {
	var values []struct {
		Name  string  `json:"name"`
		Score float64 `json:"score"`
	}
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	sort.SliceStable(values, func(i, j int) bool { return values[i].Score > values[j].Score })
	out := make([]string, 0, min(limit, len(values)))
	for _, value := range values {
		if value.Name == "" || len(out) >= limit {
			continue
		}
		out = append(out, value.Name)
	}
	return out
}

func topMusicMoods(raw []byte, limit int) []string {
	values := map[string]float64{}
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	type moodScore struct {
		name  string
		score float64
	}
	ranked := make([]moodScore, 0, len(values))
	for name, score := range values {
		if score >= 0.25 {
			ranked = append(ranked, moodScore{name: name, score: score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	out := make([]string, 0, min(limit, len(ranked)))
	for _, mood := range ranked {
		if len(out) >= limit {
			break
		}
		out = append(out, mood.name)
	}
	return out
}

func disposeAIMusicPicks(candidates []aiMusicCandidate, picks []aiMusicPick, limit int) []AIMusicMixTrack {
	if limit <= 0 || len(candidates) == 0 {
		return []AIMusicMixTrack{}
	}
	byID := make(map[int64]aiMusicCandidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.Row.TrackID] = candidate
	}

	result := make([]AIMusicMixTrack, 0, min(limit, len(candidates)))
	seenID := map[int64]bool{}
	seenRecording := map[string]bool{}
	artistCounts := map[int64]int{}
	artistCap := max(2, int(math.Ceil(float64(limit)/8)))

	add := func(candidate aiMusicCandidate, reason string, enforceCap bool) bool {
		r := candidate.Row
		if seenID[r.TrackID] || seenRecording[aiMusicRecordingKey(r)] {
			return false
		}
		if enforceCap && artistCounts[r.ArtistID] >= artistCap {
			return false
		}
		if len(result) > 0 && result[len(result)-1].ArtistID == r.ArtistID {
			return false
		}
		seenID[r.TrackID] = true
		seenRecording[aiMusicRecordingKey(r)] = true
		artistCounts[r.ArtistID]++
		result = append(result, aiMusicTrackFromCandidate(candidate, reason))
		return true
	}

	for _, pick := range picks {
		candidate, ok := byID[pick.ID]
		if !ok {
			continue // hallucinated or stale id
		}
		add(candidate, strings.TrimSpace(pick.Reason), true)
		if len(result) >= limit {
			return result
		}
	}

	// Fill a short model reply from the ranked CLAP pool while maintaining
	// diversity. First honor the artist cap; then relax it if the library slice
	// is narrow. Same-artist adjacency and duplicate recordings stay forbidden.
	for _, enforceCap := range []bool{true, false} {
		for _, candidate := range candidates {
			add(candidate, "Strong sonic match", enforceCap)
			if len(result) >= limit {
				return result
			}
		}
	}
	return result
}

func aiMusicTrackFromCandidate(candidate aiMusicCandidate, reason string) AIMusicMixTrack {
	r := candidate.Row
	return AIMusicMixTrack{
		TrackID: r.TrackID, TrackTitle: r.TrackTitle, Duration: r.Duration,
		DiscNumber: r.DiscNumber, TrackNumber: r.TrackNumber,
		AlbumID: r.AlbumID, AlbumTitle: r.AlbumTitle, AlbumSlug: r.AlbumSlug,
		AlbumCoverPath: r.AlbumCoverPath, AlbumYear: r.AlbumYear,
		ArtistID: r.ArtistID, ArtistName: r.ArtistName, ArtistSlug: r.ArtistSlug,
		Distance: candidate.BestDistance, Reason: reason,
	}
}

func aiMusicRecordingKey(row sqlc.SimilarTracksByTextRichRow) string {
	return fmt.Sprintf("%d|%s|%d", row.ArtistID, strings.ToLower(strings.TrimSpace(row.TrackTitle)), row.Duration/15)
}
