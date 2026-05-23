package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
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

// CollectionResult holds a collection and its movies.
type CollectionResult struct {
	Collection sqlc.Collection  `json:"collection"`
	Movies     []sqlc.MediaItem `json:"movies"`
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

	return CollectionResult{
		Collection: col,
		Movies:     movies,
	}, nil
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
