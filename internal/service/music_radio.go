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
// track with analyzed facets — typically because the user picked an
// artist/album whose tracks haven't been analyzed yet.
var ErrNoRadioSeed = errors.New("no analyzed track available for the given seed")

// BuildRadio resolves seeds → starting track(s) → KNN expansion. In
// multi-seed mode (req.Seeds populated) we average the sonic embeddings of
// every resolved seed into one centroid and KNN around it — the queue then
// reflects the joint space of every input. Diversifies the result so we
// don't slam back-to-back tracks from the same artist.
func (a *App) BuildRadio(ctx context.Context, userID int64, req RadioRequest) (*RadioResponse, error) {
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 50
	}

	q := sqlc.New(a.db)

	// Resolve seeds. Seeds (multi) takes precedence over Seed (single legacy).
	var seedIDs []int64
	if len(req.Seeds) > 0 {
		for _, s := range req.Seeds {
			id, err := a.resolveRadioSeed(ctx, q, s)
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
		id, err := a.resolveRadioSeed(ctx, q, req.Seed)
		if err != nil {
			return nil, err
		}
		seedIDs = []int64{id}
	}

	// Pull facets for every seed, average their track_embeddings into the
	// query centroid. With a single seed this reduces to that seed's vector.
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
	if len(embeddings) == 0 {
		return nil, ErrNoRadioSeed
	}
	centroid := averageEmbeddings(embeddings)

	exclude := append(append([]int64{}, seedIDs...), req.ExcludeTrackIDs...)
	// Dislikes are law: a thumbs-downed track never enters a generated queue.
	if vetoed, err := a.DislikedTrackIDs(ctx, userID); err == nil {
		exclude = append(exclude, vetoed...)
	}

	// Over-fetch so diversification has room to drop same-artist runs.
	// 2.5× is a heuristic; sonic-similar pools tend to cluster by artist.
	fetch := req.Limit * 5 / 2
	if fetch < req.Limit {
		fetch = req.Limit
	}

	rows, err := q.SimilarTracksByTrackRich(ctx, sqlc.SimilarTracksByTrackRichParams{
		TrackEmbedding: centroid,
		ExcludeIds:     exclude,
		TrackLimit:     fetch,
	})
	if err != nil {
		return nil, fmt.Errorf("knn: %w", err)
	}

	tracks := diversifyByArtist(rows, int(req.Limit))
	return &RadioResponse{SeedTrackID: seedIDs[0], Tracks: tracks}, nil
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

func (a *App) resolveRadioSeed(ctx context.Context, q *sqlc.Queries, seed RadioSeed) (int64, error) {
	switch seed.Kind {
	case "track":
		if seed.TrackID <= 0 {
			return 0, fmt.Errorf("seed.track_id required for kind=track")
		}
		return seed.TrackID, nil

	case "artist":
		switch {
		case seed.ArtistID > 0:
			id, err := q.PickTrackWithFacetsByArtistID(ctx, seed.ArtistID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, ErrNoRadioSeed
				}
				return 0, fmt.Errorf("artist seed: %w", err)
			}
			return id, nil
		case seed.ArtistSlug != "":
			id, err := q.PickTrackWithFacetsByArtistSlug(ctx, seed.ArtistSlug)
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
		id, err := q.PickTrackWithFacetsByAlbumID(ctx, seed.AlbumID)
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

// diversifyByArtist re-orders the KNN result so no two adjacent tracks share
// an artist. Walks the list and defers same-artist runs to the back. Keeps
// the cosine-ranked ordering otherwise — first slot is still the closest hit.
func diversifyByArtist(rows []sqlc.SimilarTracksByTrackRichRow, limit int) []sqlc.SimilarTracksByTrackRichRow {
	if len(rows) <= 1 {
		return rows
	}
	out := make([]sqlc.SimilarTracksByTrackRichRow, 0, limit)
	deferred := make([]sqlc.SimilarTracksByTrackRichRow, 0)
	prevArtist := int64(0)
	for _, r := range rows {
		if r.ArtistID == prevArtist && len(out) > 0 {
			deferred = append(deferred, r)
			continue
		}
		out = append(out, r)
		prevArtist = r.ArtistID
		if len(out) >= limit {
			break
		}
	}
	// Fill the tail from the deferred bucket, still avoiding back-to-back
	// where possible. prevArtist isn't read after this loop — the
	// last-ditch pass below intentionally allows duplicates.
	for _, r := range deferred {
		if len(out) >= limit {
			break
		}
		if len(out) > 0 && out[len(out)-1].ArtistID == r.ArtistID {
			continue
		}
		out = append(out, r)
	}
	_ = prevArtist
	// Last-ditch fill (if the library is dominated by one artist there's no
	// way to keep the no-repeat invariant — better to deliver tracks than
	// pad with nothing).
	if len(out) < limit {
		for _, r := range deferred {
			if len(out) >= limit {
				break
			}
			// Skip ones already added (cheap O(n) — limit is small).
			seen := false
			for _, o := range out {
				if o.TrackID == r.TrackID {
					seen = true
					break
				}
			}
			if !seen {
				out = append(out, r)
			}
		}
	}
	return out
}
