package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
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
}

// RadioResponse is the result of one radio build. SeedTrackID echoes the
// resolved seed so the FE can show "Radio from <track>" without bookkeeping.
type RadioResponse struct {
	SeedTrackID int64                              `json:"seed_track_id"`
	Tracks      []sqlc.SimilarTracksByTrackRichRow `json:"tracks"`
}

// ErrNoRadioSeed is returned when none of the seed fields resolve to a
// playable track, or when even the metadata/popularity fallback is empty.
var ErrNoRadioSeed = errors.New("no playable track available for the given seed")

// BuildRadio resolves seeds, averages every available sonic embedding, and
// asks the shared recommender for a diverse queue. Missing ML data falls back
// to the seed artists' external graph/provider charts instead of failing.
func (a *App) BuildRadio(ctx context.Context, userID int64, req RadioRequest) (*RadioResponse, error) {
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 50
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
	centroid := pgvector.Vector{}
	if len(embeddings) > 0 {
		centroid = averageEmbeddings(embeddings)
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
	rows, err := a.recommendMusicAround(ctx, userID, centroid, artistIDs, recommendRadio, int(req.Limit), exclude)
	if err != nil {
		return nil, fmt.Errorf("recommend radio: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNoRadioSeed
	}
	tracks := make([]sqlc.SimilarTracksByTrackRichRow, len(rows))
	for i, row := range rows {
		tracks[i] = mixRowToRadioTrack(row)
	}
	return &RadioResponse{SeedTrackID: seedIDs[0], Tracks: tracks}, nil
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
// ensure all vectors have the same dimension; we assume they do because every
// track_embedding column is vector(512). Cosine distance is invariant to
// magnitude so we don't re-normalize — pgvector's HNSW handles that.
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
