package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/musicsemantic"
	"github.com/karbowiak/heya/internal/textembed"
	"github.com/pgvector/pgvector-go"
)

// RadioSeed is the union of accepted seed kinds for /api/music/radio.
// Exactly one of {TrackID, ArtistID, ArtistSlug, AlbumID, Text} should be set.
// Kind is the discriminator the handler reads to route resolution.
type RadioSeed struct {
	Kind       string `json:"kind" enum:"track,artist,album,text" doc:"Seed type — picks how Heya resolves the starting track"`
	TrackID    int64  `json:"track_id,omitempty"    doc:"Required when kind=track"`
	ArtistID   int64  `json:"artist_id,omitempty"   doc:"Required when kind=artist (or pass artist_slug)"`
	ArtistSlug string `json:"artist_slug,omitempty" doc:"Alternative to artist_id for kind=artist"`
	AlbumID    int64  `json:"album_id,omitempty"    doc:"Required when kind=album"`
	Text       string `json:"text,omitempty"        doc:"Required when kind=text (CLAP audio-vibe prompt)"`
}

// RadioRequest is the body of POST /api/music/radio. Either Seed (single) or
// Seeds (multi-seed centroid blend) — both populated means Seeds wins.
type RadioRequest struct {
	Seed            RadioSeed   `json:"seed"`
	Seeds           []RadioSeed `json:"seeds,omitempty"  doc:"Optional. When populated, every seed is resolved to a track and their sonic embeddings are averaged into a centroid for KNN. Use to mix multiple artists/albums/tracks/vibes into one cohesive queue."`
	Limit           int32       `json:"limit"            doc:"Number of tracks to return"`
	ExcludeTrackIDs []int64     `json:"exclude_track_ids,omitempty" doc:"Tracks to skip (typically the current queue)"`
	// GenreAffinity is additive: 0 (the default, and any value <= 0) is an
	// exact no-op — ranking is byte-identical to genre-blind radio. Values
	// toward 1 make candidates that share the seed's genre profile rank
	// higher, and at >=0.9 candidates with zero genre overlap are dropped
	// from the queue entirely once enough overlapping candidates remain to
	// still fill the requested limit. See genreAffinityScoreScale's doc
	// comment in music_recommendations.go for the exact formula.
	GenreAffinity float64 `json:"genre_affinity,omitempty" doc:"0..1 knob for how strongly candidates must share the seed's genre(s) to rank well. 0 (default) is a no-op; near 1 pushes zero-genre-overlap candidates to the bottom and, at >=0.9, drops them once enough overlapping candidates remain to fill the limit."`
}

// RadioResponse is the result of one radio build. SeedTrackID echoes the
// resolved seed so the FE can show "Radio from <track>" without bookkeeping.
type RadioResponse struct {
	SeedTrackID int64                              `json:"seed_track_id"`
	Tracks      []sqlc.SimilarTracksByTrackRichRow `json:"tracks"`
	Suggestions []MusicCatalogSuggestion           `json:"suggestions" doc:"Similar canonical recordings that are not currently playable in this library"`
}

// MusicCatalogSuggestion is deliberately separate from Tracks: it can be
// displayed or linked out, but the player must never mistake it for a local
// file. Its reason comes from shared focused facets, not artist biography,
// descriptions, titles, or lyrics.
type MusicCatalogSuggestion struct {
	RecordingEntityID string  `json:"recording_entity_id"`
	Title             string  `json:"title"`
	ArtistName        string  `json:"artist_name"`
	ProviderURL       string  `json:"provider_url,omitempty"`
	Score             float64 `json:"score"`
	Reason            string  `json:"reason"`
}

// ErrNoRadioSeed is returned when none of the seed fields resolve to a
// playable track, or when even the metadata/popularity fallback is empty.
var ErrNoRadioSeed = errors.New("no playable track available for the given seed")

// BuildRadio resolves seeds, averages every available sonic and focused
// metadata embedding independently, and asks the shared recommender for a
// diverse queue. Either vector path can work without the other.
func (a *App) BuildRadio(ctx context.Context, userID int64, req RadioRequest) (*RadioResponse, error) {
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 50
	}
	if req.GenreAffinity < 0 {
		req.GenreAffinity = 0
	} else if req.GenreAffinity > 1 {
		req.GenreAffinity = 1
	}

	q := sqlc.New(a.db)

	// Resolve seeds. Seeds (multi) takes precedence over Seed (single legacy).
	var seedIDs []int64
	if len(req.Seeds) > 0 {
		for _, s := range req.Seeds {
			id, err := a.resolveRadioSeed(ctx, s)
			if err != nil {
				// Skip a single bad seed rather than failing the whole build —
				// otherwise one cold-start track wipes out the entire mix.
				if errors.Is(err, ErrNoRadioSeed) {
					continue
				}
				return nil, err
			}
			seedIDs = append(seedIDs, id)
		}
		if len(seedIDs) == 0 {
			return nil, ErrNoRadioSeed
		}
	} else {
		id, err := a.resolveRadioSeed(ctx, req.Seed)
		if err != nil {
			return nil, err
		}
		seedIDs = []int64{id}
	}

	// Pull every available seed embedding. Missing facets no longer kill the
	// station: the external artist graph + provider chart path below can grow
	// an entirely metadata-driven radio queue.
	var embeddings []pgvector.Vector
	for _, id := range seedIDs {
		f, err := q.GetTrackFacets(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("seed facets: %w", err)
		}
		embeddings = append(embeddings, f.TrackEmbedding)
	}
	sonicCentroid := pgvector.Vector{}
	if len(embeddings) > 0 {
		sonicCentroid = averageEmbeddings(embeddings)
	}

	metadataCentroid, seedMetadata, seedRecordingIDs, err := a.musicMetadataForTracks(ctx, seedIDs)
	if err != nil {
		return nil, fmt.Errorf("seed metadata: %w", err)
	}

	exclude := append(append([]int64{}, seedIDs...), req.ExcludeTrackIDs...)
	// Dislikes are law: a thumbs-downed track never enters a generated queue.
	if vetoed, err := a.DislikedTrackIDs(ctx, userID); err == nil {
		exclude = append(exclude, vetoed...)
	}

	artistIDs, err := a.artistIDsForTracks(ctx, seedIDs)
	if err != nil {
		return nil, fmt.Errorf("seed artists: %w", err)
	}

	// Only pay for the extra genre-profile fetches when the caller actually
	// opted in — genre_affinity == 0 must stay a true no-op, cost included.
	var seedGenreProfile map[string]float64
	if req.GenreAffinity > 0 {
		seeds := req.Seeds
		if len(seeds) == 0 {
			seeds = []RadioSeed{req.Seed}
		}
		seedGenreProfile, err = a.radioSeedGenreProfile(ctx, seeds, seedIDs)
		if err != nil {
			return nil, fmt.Errorf("seed genre profile: %w", err)
		}
	}

	rows, err := a.recommendMusicAround(ctx, userID, sonicCentroid, metadataCentroid, artistIDs, recommendRadio, int(req.Limit), exclude, req.GenreAffinity, seedGenreProfile)
	if err != nil {
		return nil, fmt.Errorf("recommend radio: %w", err)
	}
	suggestions, err := a.unownedMetadataSuggestions(ctx, metadataCentroid, seedMetadata, seedRecordingIDs, min(12, max(6, int(req.Limit)/4)))
	if err != nil {
		return nil, fmt.Errorf("catalog suggestions: %w", err)
	}
	if len(rows) == 0 && len(suggestions) == 0 {
		return nil, ErrNoRadioSeed
	}
	tracks := make([]sqlc.SimilarTracksByTrackRichRow, len(rows))
	for i, row := range rows {
		tracks[i] = mixRowToRadioTrack(row)
	}
	return &RadioResponse{SeedTrackID: seedIDs[0], Tracks: tracks, Suggestions: suggestions}, nil
}

func (a *App) musicMetadataForTracks(ctx context.Context, trackIDs []int64) (pgvector.Vector, musicsemantic.Facets, []uuid.UUID, error) {
	rows, err := a.db.Query(ctx, `
		SELECT recording.recording_entity_id,
		       recording.genres, recording.tags, recording.moods,
		       recording.instrumentation, recording.vocal_characteristics,
		       recording.recording_attributes,
		       COALESCE(facet.text_embedding::text, '')
		FROM metadata_entity_bindings binding
		JOIN music_catalog_recordings recording ON recording.recording_entity_id = binding.entity_id
		LEFT JOIN music_recording_facets facet
		  ON facet.recording_entity_id = recording.recording_entity_id
		 AND facet.embedder_version >= $2
		WHERE binding.local_kind = 'track' AND binding.entity_kind = 'recording'
		  AND binding.local_id = ANY($1::bigint[])`, trackIDs, int32(textembed.Version))
	if err != nil {
		return pgvector.Vector{}, musicsemantic.Facets{}, nil, err
	}
	defer rows.Close()

	var vectors []pgvector.Vector
	var recordingIDs []uuid.UUID
	type missingFacet struct {
		id     uuid.UUID
		facets musicsemantic.Facets
	}
	var missing []missingFacet
	combined := musicsemantic.Facets{}
	for rows.Next() {
		var id uuid.UUID
		var facets musicsemantic.Facets
		var vectorText string
		if err := rows.Scan(&id, &facets.Genres, &facets.Tags, &facets.Moods,
			&facets.Instrumentation, &facets.VocalCharacteristics,
			&facets.RecordingAttributes, &vectorText); err != nil {
			return pgvector.Vector{}, musicsemantic.Facets{}, nil, err
		}
		recordingIDs = append(recordingIDs, id)
		combined.Genres = append(combined.Genres, facets.Genres...)
		combined.Tags = append(combined.Tags, facets.Tags...)
		combined.Moods = append(combined.Moods, facets.Moods...)
		combined.Instrumentation = append(combined.Instrumentation, facets.Instrumentation...)
		combined.VocalCharacteristics = append(combined.VocalCharacteristics, facets.VocalCharacteristics...)
		combined.RecordingAttributes = append(combined.RecordingAttributes, facets.RecordingAttributes...)
		if vectorText != "" {
			var vector pgvector.Vector
			if err := vector.Parse(vectorText); err != nil {
				return pgvector.Vector{}, musicsemantic.Facets{}, nil, err
			}
			vectors = append(vectors, vector)
		} else if musicsemantic.Document(facets) != "" {
			missing = append(missing, missingFacet{id: id, facets: facets})
		}
	}
	if err := rows.Err(); err != nil {
		return pgvector.Vector{}, musicsemantic.Facets{}, nil, err
	}
	rows.Close()
	// Make a newly refreshed seed immediately usable instead of waiting for the
	// next scheduled catalog sweep. This remains best-effort: radio still has
	// provider/popularity fallbacks if the optional model is disabled or busy.
	if len(missing) > 0 {
		if lease, embedderErr := a.borrowRecEmbedder(ctx); embedderErr == nil && lease != nil {
			func() {
				defer lease.Close()
				embedder := lease.embedder
				for _, value := range missing {
					doc := musicsemantic.Document(value.facets)
					embedding, embedErr := embedder.Embed(doc)
					if embedErr != nil {
						continue
					}
					vector := pgvector.NewVector(embedding)
					vectors = append(vectors, vector)
					_, _ = a.db.Exec(ctx, `
					INSERT INTO music_recording_facets
					  (recording_entity_id, text_embedding, embedder_version, doc_hash, embedded_at)
					VALUES ($1, $2, $3, $4, now())
					ON CONFLICT (recording_entity_id) DO UPDATE SET
					  text_embedding = EXCLUDED.text_embedding,
					  embedder_version = EXCLUDED.embedder_version,
					  doc_hash = EXCLUDED.doc_hash,
					  embedded_at = now()`,
						value.id, vector, int32(textembed.Version), embedDocHash(doc))
				}
			}()
		}
	}
	return averageEmbeddings(vectors), combined, recordingIDs, nil
}

type musicCatalogSuggestionRow struct {
	id     uuid.UUID
	title  string
	artist string
	url    string
	score  float64
	facets musicsemantic.Facets
}

func (a *App) unownedMetadataSuggestions(
	ctx context.Context,
	centroid pgvector.Vector,
	seed musicsemantic.Facets,
	seedRecordingIDs []uuid.UUID,
	limit int,
) ([]MusicCatalogSuggestion, error) {
	if len(centroid.Slice()) == 0 || limit <= 0 {
		return []MusicCatalogSuggestion{}, nil
	}
	rows, err := a.db.Query(ctx, `
		SELECT recording.recording_entity_id, recording.title, recording.artist_name,
		       recording.provider_url, recording.genres, recording.tags, recording.moods,
		       recording.instrumentation, recording.vocal_characteristics,
		       recording.recording_attributes,
		       (facet.text_embedding <=> $1)::float8 AS distance
		FROM music_recording_facets facet
		JOIN music_catalog_recordings recording USING (recording_entity_id)
		WHERE facet.text_embedding IS NOT NULL
		  AND facet.embedder_version >= $2
		  AND NOT (recording.recording_entity_id = ANY($3::uuid[]))
		  AND NOT EXISTS (
		      SELECT 1
		      FROM metadata_entity_bindings binding
		      JOIN track_files tf ON tf.track_id = binding.local_id
		      JOIN library_files lf ON lf.id = tf.library_file_id AND lf.deleted_at IS NULL
		      WHERE binding.local_kind = 'track' AND binding.entity_kind = 'recording'
		        AND binding.entity_id = recording.recording_entity_id)
		ORDER BY facet.text_embedding <=> $1
		LIMIT $4`, centroid, int32(textembed.Version), seedRecordingIDs, limit*4)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	artistCounts := map[string]int{}
	result := make([]MusicCatalogSuggestion, 0, limit)
	for rows.Next() {
		var row musicCatalogSuggestionRow
		var distance float64
		if err := rows.Scan(&row.id, &row.title, &row.artist, &row.url,
			&row.facets.Genres, &row.facets.Tags, &row.facets.Moods,
			&row.facets.Instrumentation, &row.facets.VocalCharacteristics,
			&row.facets.RecordingAttributes, &distance); err != nil {
			return nil, err
		}
		if strings.TrimSpace(row.title) == "" || strings.TrimSpace(row.artist) == "" {
			continue
		}
		artistKey := strings.ToLower(strings.TrimSpace(row.artist))
		if artistCounts[artistKey] >= 2 {
			continue
		}
		artistCounts[artistKey]++
		row.score = math.Max(-1, math.Min(1, 1-distance))
		shared := musicsemantic.SharedTerms(seed, row.facets, 3)
		reason := "Similar musical metadata"
		if len(shared) > 0 {
			reason = "Shared: " + strings.Join(shared, ", ")
		}
		result = append(result, MusicCatalogSuggestion{
			RecordingEntityID: row.id.String(), Title: row.title,
			ArtistName: row.artist, ProviderURL: row.url,
			Score: math.Round(row.score*1000) / 1000, Reason: reason,
		})
		if len(result) == limit {
			break
		}
	}
	return result, rows.Err()
}

func (a *App) artistIDsForTracks(ctx context.Context, trackIDs []int64) ([]int64, error) {
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT al.artist_id
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		WHERE t.id = ANY($1::bigint[])`, trackIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func mixRowToRadioTrack(row sqlc.ListArtistTopTracksForMixRow) sqlc.SimilarTracksByTrackRichRow {
	return sqlc.SimilarTracksByTrackRichRow{
		TrackID: row.TrackID, TrackTitle: row.TrackTitle, Duration: row.Duration,
		DiscNumber: row.DiscNumber, TrackNumber: row.TrackNumber,
		AlbumID: row.AlbumID, AlbumTitle: row.AlbumTitle, AlbumSlug: row.AlbumSlug,
		AlbumCoverPath: row.AlbumCoverPath, AlbumYear: row.AlbumYear,
		ArtistID: row.ArtistID, ArtistName: row.ArtistName, ArtistSlug: row.ArtistSlug,
	}
}

// averageEmbeddings returns the element-wise mean of `vecs`. Caller must
// ensure all vectors in one call have the same dimension. Sonic and metadata
// centroids call this separately, so their 512-d and 1024-d spaces never mix.
// Cosine distance is invariant to magnitude, so no re-normalization is needed.
func averageEmbeddings(vecs []pgvector.Vector) pgvector.Vector {
	if len(vecs) == 0 {
		return pgvector.Vector{}
	}
	if len(vecs) == 1 {
		return vecs[0]
	}
	first := vecs[0].Slice()
	out := make([]float32, len(first))
	copy(out, first)
	for _, v := range vecs[1:] {
		s := v.Slice()
		for i := range out {
			if i < len(s) {
				out[i] += s[i]
			}
		}
	}
	inv := 1.0 / float32(len(vecs))
	for i := range out {
		out[i] *= inv
	}
	return pgvector.NewVector(out)
}

func (a *App) resolveRadioSeed(ctx context.Context, seed RadioSeed) (int64, error) {
	switch seed.Kind {
	case "track":
		if seed.TrackID <= 0 {
			return 0, fmt.Errorf("seed.track_id required for kind=track")
		}
		var id int64
		err := a.db.QueryRow(ctx, `SELECT t.id FROM tracks t
			WHERE t.id = $1
			  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
			              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)`, seed.TrackID).Scan(&id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, ErrNoRadioSeed
			}
			return 0, fmt.Errorf("track seed: %w", err)
		}
		return id, nil

	case "artist":
		switch {
		case seed.ArtistID > 0:
			var id int64
			err := a.db.QueryRow(ctx, `SELECT t.id
				FROM tracks t
				JOIN albums al ON al.id = t.album_id
				LEFT JOIN track_facets tf ON tf.track_id = t.id AND tf.track_embedding IS NOT NULL
				WHERE al.artist_id = $1
				  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
				              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
				ORDER BY (tf.track_id IS NOT NULL) DESC, random()
				LIMIT 1`, seed.ArtistID).Scan(&id)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, ErrNoRadioSeed
				}
				return 0, fmt.Errorf("artist seed: %w", err)
			}
			return id, nil
		case seed.ArtistSlug != "":
			var id int64
			err := a.db.QueryRow(ctx, `SELECT t.id
				FROM tracks t
				JOIN albums al ON al.id = t.album_id
				JOIN artists ar ON ar.id = al.artist_id
				JOIN media_item_cards mi ON mi.id = ar.media_item_id
				LEFT JOIN track_facets tf ON tf.track_id = t.id AND tf.track_embedding IS NOT NULL
				WHERE mi.slug = $1
				  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
				              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
				ORDER BY (tf.track_id IS NOT NULL) DESC, random()
				LIMIT 1`, seed.ArtistSlug).Scan(&id)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, ErrNoRadioSeed
				}
				return 0, fmt.Errorf("artist seed: %w", err)
			}
			return id, nil
		default:
			return 0, fmt.Errorf("seed.artist_id or seed.artist_slug required for kind=artist")
		}

	case "album":
		if seed.AlbumID <= 0 {
			return 0, fmt.Errorf("seed.album_id required for kind=album")
		}
		var id int64
		err := a.db.QueryRow(ctx, `SELECT t.id
			FROM tracks t
			LEFT JOIN track_facets tf ON tf.track_id = t.id AND tf.track_embedding IS NOT NULL
			WHERE t.album_id = $1
			  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id = atf.library_file_id
			              WHERE atf.track_id = t.id AND alf.deleted_at IS NULL)
			ORDER BY (tf.track_id IS NOT NULL) DESC, random()
			LIMIT 1`, seed.AlbumID).Scan(&id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, ErrNoRadioSeed
			}
			return 0, fmt.Errorf("album seed: %w", err)
		}
		return id, nil

	case "text":
		if seed.Text == "" {
			return 0, fmt.Errorf("seed.text required for kind=text")
		}
		hits, err := a.SearchMusicByText(ctx, seed.Text, 1)
		if err != nil {
			return 0, fmt.Errorf("text seed: %w", err)
		}
		if len(hits) == 0 {
			return 0, ErrNoRadioSeed
		}
		return hits[0].TrackID, nil

	default:
		return 0, fmt.Errorf("unknown seed.kind %q (want track|artist|album|text)", seed.Kind)
	}
}
