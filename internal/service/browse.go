package service

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

// GenreResult holds a genre name, its matching media items, and a total count.
type GenreResult struct {
	Genre string           `json:"genre"`
	Items []sqlc.MediaItem `json:"items"`
	Total int64            `json:"total"`
}

// KeywordResult holds a keyword name, its matching media items, and a total count.
type KeywordResult struct {
	Keyword string           `json:"keyword"`
	Items   []sqlc.MediaItem `json:"items"`
	Total   int64            `json:"total"`
}

// CollectionResult holds a collection, its local movies, and the full
// franchise membership resolved to owned-vs-missing.
type CollectionResult struct {
	Collection sqlc.Collection  `json:"collection"`
	Movies     []sqlc.MediaItem `json:"movies"`
	// Parts is every film in the franchise (from heya.media), each tagged with
	// its local movie when owned. Empty until a member movie is enriched.
	Parts      []CollectionPartView `json:"parts"`
	OwnedCount int                  `json:"owned_count"`
	// Genres aggregated across the collection's owned movies, most-common
	// first. TMDB collections have no genres of their own, so we surface the
	// members' — see ListCollectionGenres.
	Genres []string `json:"genres"`
	// Keywords is the finer folksonomy aggregated across the owned movies
	// (capped, most-common first) — see ListCollectionKeywords.
	Keywords []string `json:"keywords"`
}

// CollectionPartView is one franchise film plus its local resolution: when
// LocalMediaItemID is set the user owns it (link to the movie); otherwise it's
// a gap in the collection the UI renders as missing.
type CollectionPartView struct {
	metadata.CollectionPart
	LocalMediaItemID *int64  `json:"local_media_item_id,omitempty"`
	LocalSlug        *string `json:"local_slug,omitempty"`
}

// CollectionListResult holds a paginated list of collections and the total count.
type CollectionListResult struct {
	Items []sqlc.ListAllCollectionsRow `json:"items"`
	Total int64                        `json:"total"`
}

// ListGenres returns all genres with their media item counts.
func (a *App) ListGenres(ctx context.Context) ([]sqlc.ListAllGenresRow, error) {
	q := sqlc.New(a.db)
	return q.ListAllGenres(ctx)
}

// GetGenre returns media items matching a genre name, paginated.
func (a *App) GetGenre(ctx context.Context, name string, limit, offset int32) (GenreResult, error) {
	q := sqlc.New(a.db)

	items, err := q.ListMediaByGenre(ctx, sqlc.ListMediaByGenreParams{
		Column1: name,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return GenreResult{}, err
	}

	total, _ := q.CountMediaByGenre(ctx, name)

	return GenreResult{
		Genre: name,
		Items: items,
		Total: total,
	}, nil
}

// GetKeyword returns media items matching a keyword name, paginated.
func (a *App) GetKeyword(ctx context.Context, name string, limit, offset int32) (KeywordResult, error) {
	q := sqlc.New(a.db)

	items, err := q.ListMediaByKeyword(ctx, sqlc.ListMediaByKeywordParams{
		Column1: name,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return KeywordResult{}, err
	}

	total, _ := q.CountMediaByKeyword(ctx, name)

	return KeywordResult{
		Keyword: name,
		Items:   items,
		Total:   total,
	}, nil
}

// ListCollections returns a paginated list of all collections with movie counts.
func (a *App) ListCollections(ctx context.Context, limit, offset int32) (CollectionListResult, error) {
	q := sqlc.New(a.db)

	items, err := q.ListAllCollections(ctx, sqlc.ListAllCollectionsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return CollectionListResult{}, err
	}

	total, _ := q.CountAllCollections(ctx)

	return CollectionListResult{
		Items: items,
		Total: total,
	}, nil
}

// GetCollection returns a collection by ID together with its movies.
func (a *App) GetCollection(ctx context.Context, id int64) (CollectionResult, error) {
	q := sqlc.New(a.db)

	col, err := q.GetCollectionByID(ctx, id)
	if err != nil {
		return CollectionResult{}, err
	}

	movies, _ := q.ListCollectionMovies(ctx, pgtype.Int8{Int64: col.ID, Valid: true})

	parts, owned := a.resolveCollectionParts(ctx, q, col.Parts)

	genreRows, _ := q.ListCollectionGenres(ctx, pgtype.Int8{Int64: col.ID, Valid: true})
	genres := make([]string, 0, len(genreRows))
	for _, g := range genreRows {
		genres = append(genres, g.Genre)
	}

	keywordRows, _ := q.ListCollectionKeywords(ctx, pgtype.Int8{Int64: col.ID, Valid: true})
	keywords := make([]string, 0, len(keywordRows))
	for _, k := range keywordRows {
		keywords = append(keywords, k.Name)
	}

	return CollectionResult{
		Collection: col,
		Movies:     movies,
		Parts:      parts,
		OwnedCount: owned,
		Genres:     genres,
		Keywords:   keywords,
	}, nil
}

// resolveCollectionParts unmarshals the persisted franchise membership and tags
// each film with the local movie that satisfies it (matched by tmdb id),
// returning the views in the stored release order plus the owned count. A part
// with no local match is a gap (missing from the library).
func (a *App) resolveCollectionParts(ctx context.Context, q *sqlc.Queries, raw []byte) ([]CollectionPartView, int) {
	var parts []metadata.CollectionPart
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &parts)
	}
	if len(parts) == 0 {
		return nil, 0
	}

	// Resolve every tmdb id present in one query, then index tmdb -> local ref.
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.TmdbID > 0 {
			ids = append(ids, strconv.FormatInt(p.TmdbID, 10))
		}
	}
	local := make(map[int64]collectionLocalRef, len(ids))
	if len(ids) > 0 {
		if rows, err := q.ListMoviesByTmdbIDs(ctx, ids); err == nil {
			for _, r := range rows {
				var ext map[string]string
				if json.Unmarshal(r.ExternalIds, &ext) == nil {
					if tmdb, convErr := strconv.ParseInt(ext["tmdb"], 10, 64); convErr == nil {
						local[tmdb] = collectionLocalRef{ID: r.ID, Slug: r.Slug}
					}
				}
			}
		}
	}
	return buildCollectionPartViews(parts, local)
}

// collectionLocalRef is a local movie a franchise part resolved to.
type collectionLocalRef struct {
	ID   int64
	Slug string
}

// buildCollectionPartViews is the pure resolution step: given the franchise
// membership (stored release order) and the tmdb->local index, it tags each
// part as owned or missing and counts the owned. Kept DB-free so it's unit
// testable without a database.
func buildCollectionPartViews(parts []metadata.CollectionPart, localByTmdb map[int64]collectionLocalRef) ([]CollectionPartView, int) {
	views := make([]CollectionPartView, 0, len(parts))
	owned := 0
	for _, p := range parts {
		v := CollectionPartView{CollectionPart: p}
		if p.TmdbID > 0 {
			if r, ok := localByTmdb[p.TmdbID]; ok {
				id, slug := r.ID, r.Slug
				v.LocalMediaItemID = &id
				v.LocalSlug = &slug
				owned++
			}
		}
		views = append(views, v)
	}
	return views, owned
}

// BrowseCollections returns collections that have at least one local movie.
func (a *App) BrowseCollections(ctx context.Context) ([]sqlc.ListCollectionsWithLocalMediaRow, error) {
	q := sqlc.New(a.db)
	return q.ListCollectionsWithLocalMedia(ctx)
}

// SearchPeople searches people by name prefix, ordered by popularity.
func (a *App) SearchPeople(ctx context.Context, query string, limit int32) ([]sqlc.SearchPeopleByNameRow, error) {
	q := sqlc.New(a.db)
	return q.SearchPeopleByName(ctx, sqlc.SearchPeopleByNameParams{
		Query:      query,
		MaxResults: limit,
	})
}

// SearchStudios searches production companies by name prefix.
func (a *App) SearchStudios(ctx context.Context, query string, limit int32) ([]sqlc.SearchProductionCompaniesByNameRow, error) {
	q := sqlc.New(a.db)
	return q.SearchProductionCompaniesByName(ctx, sqlc.SearchProductionCompaniesByNameParams{
		Query:      query,
		MaxResults: limit,
	})
}

// ListMediaIDsByPeople returns the union of cast and crew media item IDs for the given person IDs.
func (a *App) ListMediaIDsByPeople(ctx context.Context, personIDs []int64) ([]int64, error) {
	q := sqlc.New(a.db)

	castIDs, err := q.ListCastMediaItemIDs(ctx, personIDs)
	if err != nil {
		return nil, err
	}

	crewIDs, err := q.ListCrewMediaItemIDs(ctx, personIDs)
	if err != nil {
		return nil, err
	}

	seen := make(map[int64]struct{}, len(castIDs)+len(crewIDs))
	for _, id := range castIDs {
		seen[id] = struct{}{}
	}
	for _, id := range crewIDs {
		seen[id] = struct{}{}
	}

	result := make([]int64, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	return result, nil
}

// ListMediaIDsByStudio returns media item IDs associated with the given production company IDs.
func (a *App) ListMediaIDsByStudio(ctx context.Context, studioIDs []int64) ([]int64, error) {
	q := sqlc.New(a.db)
	return q.ListStudioMediaItemIDs(ctx, studioIDs)
}

// ListTopRecommendations returns the most-recommended items across the library,
// weighted by source count and vote average.
func (a *App) ListTopRecommendations(ctx context.Context, limit int32) ([]sqlc.ListTopRecommendationsRow, error) {
	q := sqlc.New(a.db)
	return q.ListTopRecommendations(ctx, limit)
}
