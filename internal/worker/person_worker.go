package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata/tmdb"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type PersonFetchWorker struct {
	river.WorkerDefaults[PersonFetchArgs]
	DB        *pgxpool.Pool
	TMDBToken string
}

func (w *PersonFetchWorker) Work(ctx context.Context, job *river.Job[PersonFetchArgs]) error {
	if w.TMDBToken == "" {
		return nil
	}

	q := sqlc.New(w.DB)

	existing, err := q.GetPersonByID(ctx, job.Args.PersonID)
	if err != nil {
		return nil
	}
	if existing.Biography != "" {
		return nil
	}

	provider := tmdb.NewProvider(w.TMDBToken)
	detail, err := provider.GetPersonDetail(ctx, int(job.Args.TmdbID))
	if err != nil {
		log.Debug().Err(err).Int32("tmdb_id", job.Args.TmdbID).Msg("person fetch failed")
		return nil
	}

	aka := detail.AlsoKnownAs
	if aka == nil {
		aka = []string{}
	}

	q.CreatePerson(ctx, sqlc.CreatePersonParams{
		TmdbID:       pgtype.Int4{Int32: job.Args.TmdbID, Valid: true},
		Name:         detail.Name,
		AlsoKnownAs:  aka,
		Biography:    detail.Biography,
		Birthday:     detail.Birthday,
		Deathday:     detail.Deathday,
		PlaceOfBirth: detail.PlaceOfBirth,
		Gender:       int32(detail.Gender),
		ProfilePath:  imageURL(detail.ProfilePath),
		Homepage:     detail.Homepage,
		ImdbID:       detail.ImdbID,
		Popularity:   pgtype.Numeric{Valid: true},
	})

	personSlug := slug.GenerateUnique(ctx, detail.Name, "", job.Args.PersonID,
		func(ctx context.Context, s string, excludeID int64) (bool, error) {
			r, err := q.PersonSlugExists(ctx, sqlc.PersonSlugExistsParams{Slug: s, ID: excludeID})
			if err != nil {
				return false, err
			}
			return r, nil
		})
	q.UpdatePersonSlug(ctx, sqlc.UpdatePersonSlugParams{ID: job.Args.PersonID, Slug: personSlug})

	if detail.ProfilePath != "" {
		client := river.ClientFromContext[pgx.Tx](ctx)
		client.Insert(ctx, DownloadImageArgs{
			PersonID:   job.Args.PersonID,
			EntityType: "person",
			URL:        imageURL(detail.ProfilePath),
			AssetType:  "profile",
			MediaType:  "person",
		}, &river.InsertOpts{Priority: 4})
	}

	log.Debug().
		Str("name", detail.Name).
		Int32("tmdb_id", job.Args.TmdbID).
		Msg("person metadata fetched")

	return nil
}

func imageURL(path string) string {
	if path == "" {
		return ""
	}
	if len(path) > 4 && path[:4] == "http" {
		return path
	}
	return fmt.Sprintf("https://image.tmdb.org/t/p/original%s", path)
}
