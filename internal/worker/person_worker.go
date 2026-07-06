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
	Progress  *TaskProgressBroadcaster
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

	// 2. If already enriched, skip — unless the new external_credits
	//    table is empty for this person. That's the backfill path for
	//    people who were enriched before the cast/crew/known-for columns
	//    started flowing; the next PersonFetch tick picks them up once
	//    and they don't re-enter the worker after.
	if existing.HeyaEnrichedAt.Valid {
		creds, _ := q.ListPersonExternalCredits(ctx, job.Args.PersonID)
		if len(creds) > 0 {
			return nil
		}
	}

	w.Progress.SetCurrentByKind(PersonFetchArgs{}.Kind(), existing.Name)

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
		// Retry transient upstream failures at the job level; a terminal 404
		// (person genuinely absent upstream) is a no-op, not a retry.
		if ctx.Err() != nil || heyamedia.IsRetryable(err) {
			return fmt.Errorf("person fetch tmdb:%d: %w", tmdbID, err)
		}
		log.Debug().Err(err).Int("tmdb_id", tmdbID).Msg("person fetch from heya failed (terminal)")
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

	// Guard idx_people_heya_slug — a *global* UNIQUE on heya_slug. Two
	// distinct person rows can resolve to the same upstream person (e.g. the
	// same actor scanned under name variants across titles). When the slug is
	// already owned by a different row, that row is the established canonical:
	// fold this person into it (reparenting its cast/crew links) and stop —
	// the owner already carries the enriched metadata. This both removes the
	// duplicate and avoids the UpdatePersonFull 23505 that would otherwise
	// throw away this person's entire enrichment (it writes heya_slug
	// alongside every other column in one statement).
	if resp.Slug != "" && resp.Slug != existing.HeyaSlug {
		if owner, err := q.GetPersonByHeyaSlug(ctx, resp.Slug); err == nil && owner.ID != job.Args.PersonID {
			if mergeErr := mergePersonInto(ctx, w.DB, q, owner.ID, job.Args.PersonID); mergeErr != nil {
				log.Warn().Err(mergeErr).
					Int64("person_id", job.Args.PersonID).
					Int64("canonical_id", owner.ID).
					Msg("merge duplicate person failed")
				return nil
			}
			log.Debug().
				Int64("merged_person_id", job.Args.PersonID).
				Int64("canonical_id", owner.ID).
				Str("heya_slug", resp.Slug).
				Msg("merged duplicate person into canonical row")
			return nil
		}
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
		// A concurrent person_fetch for the same upstream actor may have claimed
		// idx_people_heya_slug (a global UNIQUE) between our earlier owner check
		// and this write, throwing a 23505. Re-resolve the now-committed owner
		// and fold into it rather than dropping this person's enrichment —
		// mirrors the "race resolved" re-query in matcher/persistence.go. The
		// failed UpdatePersonFull is a single autocommit statement, so nothing
		// partial was written before we merge.
		if resp.Slug != "" {
			if owner, reErr := q.GetPersonByHeyaSlug(ctx, resp.Slug); reErr == nil && owner.ID != job.Args.PersonID {
				if mergeErr := mergePersonInto(ctx, w.DB, q, owner.ID, job.Args.PersonID); mergeErr == nil {
					log.Debug().
						Int64("merged_person_id", job.Args.PersonID).
						Int64("canonical_id", owner.ID).
						Str("heya_slug", resp.Slug).
						Msg("merged duplicate person into canonical row (slug race)")
					return nil
				}
			}
		}
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

	// 9b. Replace external credits (cast + crew + known_for_titles). We
	// nuke + reinsert rather than upsert one-by-one because upstream may
	// drop entries between enrichment runs, and stale rows would clutter
	// the "Known For" tab forever.
	_ = q.DeletePersonExternalCredits(ctx, job.Args.PersonID)
	storeExternalCredits(ctx, q, job.Args.PersonID, "cast", pay.Cast)
	storeExternalCredits(ctx, q, job.Args.PersonID, "crew", pay.Crew)
	storeExternalCredits(ctx, q, job.Args.PersonID, "known_for", pay.KnownForTitles)

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

// storeExternalCredits writes a person's cast/crew/known-for credits from
// the upstream payload. Each row carries the union of external IDs we
// receive so the FE LEFT JOIN against media_items can resolve the
// "already in library" link without a roundtrip per row.
func storeExternalCredits(ctx context.Context, q *sqlc.Queries, personID int64, kind string, credits []heyamedia.HeyaCredit) {
	for i, c := range credits {
		ids := map[string]string{}
		if c.TmdbID > 0 {
			ids["tmdb"] = fmt.Sprintf("%d", c.TmdbID)
		}
		if c.TvdbID > 0 {
			ids["tvdb"] = fmt.Sprintf("%d", c.TvdbID)
		}
		if c.ImdbID != "" {
			ids["imdb"] = c.ImdbID
		}
		idsJSON, _ := json.Marshal(ids)

		// `order` from upstream is the cast billing position when present;
		// fall back to source iteration order so the FE keeps a stable
		// rendering even when upstream order is zero.
		order := c.Order
		if order == 0 {
			order = i
		}

		_ = q.UpsertPersonExternalCredit(ctx, sqlc.UpsertPersonExternalCreditParams{
			PersonID:     personID,
			Kind:         kind,
			MediaKind:    c.Kind,
			Title:        c.Title,
			Year:         int32(c.Year),
			Character:    c.Character,
			Job:          c.Job,
			Department:   c.Department,
			EpisodeCount: int32(c.EpisodeCount),
			DisplayOrder: int32(order),
			Slug:         c.Slug,
			PosterUrl:    c.PosterURL,
			ExternalIds:  idsJSON,
			Source:       c.Source,
		})
	}
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
