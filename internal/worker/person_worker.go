package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type PersonFetchWorker struct {
	river.WorkerDefaults[PersonFetchArgs]
	DB        *pgxpool.Pool
	HeyaMedia *heyamedia.Client
}

func (w *PersonFetchWorker) Work(ctx context.Context, job *river.Job[PersonFetchArgs]) error {
	if w.HeyaMedia == nil {
		return nil
	}

	q := sqlc.New(w.DB)

	// 1. Look up existing person by ID.
	existing, err := q.GetPersonByID(ctx, job.Args.PersonID)
	if err != nil {
		return nil
	}

	// 2. If already enriched, skip.
	if existing.HeyaEnrichedAt.Valid {
		return nil
	}

	// 3. Get TMDB ID from job args (already provided).
	tmdbID := int(job.Args.TmdbID)
	if tmdbID == 0 {
		// Try to extract from the person's external_ids JSONB.
		var extIDs map[string]interface{}
		if err := json.Unmarshal(existing.ExternalIds, &extIDs); err == nil {
			if v, ok := extIDs["tmdb"]; ok {
				switch t := v.(type) {
				case string:
					fmt.Sscanf(t, "%d", &tmdbID)
				case float64:
					tmdbID = int(t)
				}
			}
		}
		if tmdbID == 0 {
			log.Debug().Int64("person_id", job.Args.PersonID).Msg("person has no tmdb_id, skipping")
			return nil
		}
	}

	// 4. Call HeyaMedia person lookup.
	resp, err := heyamedia.GetPersonFromHeya(ctx, w.HeyaMedia, tmdbID)
	if err != nil {
		log.Debug().Err(err).Int("tmdb_id", tmdbID).Msg("person fetch from heya failed")
		return nil
	}

	pay := &resp.Payload

	// 5-6. Parse payload, convert gender string to int.
	gender := personGenderToInt(pay.Gender)

	// Build external_ids JSON: merge tmdb + payload external_ids.
	mergedIDs := make(map[string]string)
	mergedIDs["tmdb"] = fmt.Sprintf("%d", tmdbID)
	for k, v := range pay.ExternalIDs {
		if v != "" {
			mergedIDs[k] = v
		}
	}
	extIDsJSON, _ := json.Marshal(mergedIDs)

	// Get profile_path: first (highest-scored) profile image URL.
	profilePath := ""
	if len(pay.Profiles) > 0 {
		profilePath = pay.Profiles[0].URL
	}

	// Also known as: ensure non-nil.
	aka := pay.AlsoKnownAs
	if aka == nil {
		aka = []string{}
	}

	// 7. Update person with full data.
	_, err = q.UpdatePersonFull(ctx, sqlc.UpdatePersonFullParams{
		ID:                 job.Args.PersonID,
		Name:               pay.Name,
		AlsoKnownAs:        aka,
		Biography:          pay.Biography,
		Birthday:           pay.Birthday,
		Deathday:           pay.Deathday,
		PlaceOfBirth:       pay.BirthPlace,
		Gender:             int32(gender),
		ProfilePath:        profilePath,
		Homepage:           pay.Homepage,
		Popularity:         numericFromFloat64(pay.Popularity),
		ExternalIds:        extIDsJSON,
		SortName:           pay.SortName,
		KnownForDepartment: pay.KnownForDepartment,
		BirthYear:          int32(pay.BirthYear),
		HeyaSlug:           resp.Slug,
	})
	if err != nil {
		log.Warn().Err(err).Int64("person_id", job.Args.PersonID).Msg("failed to update person")
		return nil
	}

	// 8. Store biographies.
	for lang, bio := range pay.Biographies {
		if bio == "" {
			continue
		}
		q.CreatePersonBiography(ctx, sqlc.CreatePersonBiographyParams{
			PersonID:  job.Args.PersonID,
			Language:  lang,
			Biography: bio,
		})
	}

	// 9. Store profiles.
	for i, prof := range pay.Profiles {
		q.CreatePersonProfile(ctx, sqlc.CreatePersonProfileParams{
			PersonID:  job.Args.PersonID,
			Url:       prof.URL,
			Source:    prof.Source,
			Aspect:    prof.Aspect,
			Width:     int32(prof.Width),
			Height:    int32(prof.Height),
			Score:     numericFromFloat64(prof.Score),
			SortOrder: int32(i),
		})
	}

	// 10. Generate slug if not set.
	if existing.Slug == "" {
		personSlug := slug.GenerateUnique(ctx, pay.Name, "", job.Args.PersonID,
			func(ctx context.Context, s string, excludeID int64) (bool, error) {
				r, err := q.PersonSlugExists(ctx, sqlc.PersonSlugExistsParams{Slug: s, ID: excludeID})
				if err != nil {
					return false, err
				}
				return r, nil
			})
		q.UpdatePersonSlug(ctx, sqlc.UpdatePersonSlugParams{ID: job.Args.PersonID, Slug: personSlug})
	}

	// 11. Mark enriched.
	q.MarkPersonEnriched(ctx, job.Args.PersonID)

	// 12. Queue profile image download for the best photo.
	if profilePath != "" {
		client := river.ClientFromContext[pgx.Tx](ctx)
		client.Insert(ctx, DownloadImageArgs{
			PersonID:   job.Args.PersonID,
			EntityType: "person",
			URL:        profilePath,
			AssetType:  "profile",
			MediaType:  "person",
		}, &river.InsertOpts{Priority: 4})
	}

	log.Debug().
		Str("name", pay.Name).
		Int("tmdb_id", tmdbID).
		Msg("person metadata fetched from heya")

	return nil
}

// personGenderToInt converts the string gender from the Heya API to an int:
// "male" -> 2, "female" -> 1, else -> 0.
func personGenderToInt(g string) int {
	switch g {
	case "male":
		return 2
	case "female":
		return 1
	default:
		return 0
	}
}

// numericFromFloat64 converts a float64 to pgtype.Numeric with 3 decimal places.
func numericFromFloat64(f float64) pgtype.Numeric {
	if f == 0 {
		return pgtype.Numeric{Valid: true}
	}
	intVal := int64(f * 1000)
	return pgtype.Numeric{
		Int:   big.NewInt(intVal),
		Exp:   -3,
		Valid: true,
	}
}
