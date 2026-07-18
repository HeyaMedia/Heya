package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type PersonFetchWorker struct {
	river.WorkerDefaults[PersonFetchArgs]
	DB           *pgxpool.Pool
	HeyaMetadata *heyametadata.Client
	Progress     *TaskProgressBroadcaster
}

func (w *PersonFetchWorker) Work(ctx context.Context, job *river.Job[PersonFetchArgs]) error {
	if w.HeyaMetadata == nil {
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
	if existing.HeyaEnrichedAt.Valid && !job.Args.Force {
		creds, _ := q.ListPersonExternalCredits(ctx, job.Args.PersonID)
		if len(creds) > 0 {
			return nil
		}
	}

	w.Progress.SetCurrentByKind(PersonFetchArgs{}.Kind(), existing.Name)

	// 3. Resolve the local person to its canonical Heya UUID. New jobs always
	//    carry this value or have a metadata binding created with the credit.
	entityID := job.Args.EntityID
	if entityID == "" {
		binding, bindErr := q.GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{LocalKind: "person", LocalID: job.Args.PersonID})
		if bindErr == nil {
			entityID = binding.EntityID.String()
		} else if !errors.Is(bindErr, pgx.ErrNoRows) {
			return fmt.Errorf("read canonical person binding: %w", bindErr)
		}
	}

	// Keep the old argument only to drain jobs queued before the V2 cutover and
	// to preserve the value in the local external-ID display.
	tmdbID := int(job.Args.TmdbID)
	if tmdbID == 0 {
		// Try to extract from the person's external_ids JSONB.
		var extIDs map[string]interface{}
		if err := json.Unmarshal(existing.ExternalIds, &extIDs); err == nil {
			if v, ok := extIDs["tmdb"]; ok {
				switch t := v.(type) {
				case string:
					if parsedID, parseErr := strconv.Atoi(t); parseErr == nil {
						tmdbID = parsedID
					}
				case float64:
					tmdbID = int(t)
				}
			}
		}
	}
	if entityID == "" {
		log.Debug().Int64("person_id", job.Args.PersonID).Msg("person has no canonical metadata binding, skipping")
		return nil
	}

	// 4. Read person detail and reverse credits by canonical UUID.
	resp, err := heyametadata.GetPersonByEntityFromHeya(ctx, w.HeyaMetadata, entityID)
	if err != nil {
		// Retry transient upstream failures at the job level; a terminal 404
		// (person genuinely absent upstream) is a no-op, not a retry.
		if ctx.Err() != nil || heyametadata.IsRetryable(err) {
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
	if tmdbID > 0 {
		mergedIDs["tmdb"] = fmt.Sprintf("%d", tmdbID)
	}
	for k, v := range pay.ExternalIDs {
		if v != "" {
			mergedIDs[k] = v
		}
	}
	if tmdbID == 0 {
		_, _ = fmt.Sscanf(mergedIDs["tmdb"], "%d", &tmdbID)
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
	canonicalUUID, parseErr := uuid.Parse(resp.ID)
	if parseErr != nil {
		return fmt.Errorf("canonical person returned invalid UUID %q: %w", resp.ID, parseErr)
	}
	schemaVersion := resp.SchemaVersion
	if schemaVersion <= 0 {
		schemaVersion = 1
	}
	if _, bindErr := q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "person", LocalID: job.Args.PersonID, EntityID: canonicalUUID, EntityKind: "person",
		SchemaVersion: int32(schemaVersion), ProjectionVersion: resp.ProjectionVersion,
	}); bindErr != nil {
		return fmt.Errorf("bind person %d to canonical metadata: %w", job.Args.PersonID, bindErr)
	}

	// 8. Store biographies.
	for lang, bio := range pay.Biographies {
		if bio == "" {
			continue
		}
		if err := q.CreatePersonBiography(ctx, sqlc.CreatePersonBiographyParams{
			PersonID:  job.Args.PersonID,
			Language:  lang,
			Biography: bio,
		}); err != nil {
			return fmt.Errorf("store %s biography for person %d: %w", lang, job.Args.PersonID, err)
		}
	}

	// 9. Store profiles.
	for i, prof := range pay.Profiles {
		if err := q.CreatePersonProfile(ctx, sqlc.CreatePersonProfileParams{
			PersonID:  job.Args.PersonID,
			Url:       prof.URL,
			Source:    prof.Source,
			Aspect:    prof.Aspect,
			Width:     int32(prof.Width),
			Height:    int32(prof.Height),
			Score:     numericFromFloat64(prof.Score),
			SortOrder: int32(i),
		}); err != nil {
			return fmt.Errorf("store profile for person %d: %w", job.Args.PersonID, err)
		}
	}

	// 9b. Replace external credits (cast + crew + known_for_titles). We
	// nuke + reinsert rather than upsert one-by-one because upstream may
	// drop entries between enrichment runs, and stale rows would clutter
	// the "Known For" tab forever.
	if err := q.DeletePersonExternalCredits(ctx, job.Args.PersonID); err != nil {
		return fmt.Errorf("delete stale credits for person %d: %w", job.Args.PersonID, err)
	}
	if err := storeExternalCredits(ctx, q, job.Args.PersonID, "cast", pay.Cast); err != nil {
		return err
	}
	if err := storeExternalCredits(ctx, q, job.Args.PersonID, "crew", pay.Crew); err != nil {
		return err
	}
	if err := storeExternalCredits(ctx, q, job.Args.PersonID, "known_for", pay.KnownForTitles); err != nil {
		return err
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
		if err := q.UpdatePersonSlug(ctx, sqlc.UpdatePersonSlugParams{ID: job.Args.PersonID, Slug: personSlug}); err != nil {
			return fmt.Errorf("update slug for person %d: %w", job.Args.PersonID, err)
		}
	}

	// 11. Queue profile image download before marking enrichment complete. If
	// enqueueing fails, the retry must still enter this worker and try again.
	if profilePath != "" {
		client := river.ClientFromContext[pgx.Tx](ctx)
		if _, err := client.Insert(ctx, DownloadImageArgs{
			PersonID:   job.Args.PersonID,
			EntityType: "person",
			URL:        profilePath,
			AssetType:  "profile",
			MediaType:  "person",
		}, &river.InsertOpts{Priority: 4}); err != nil {
			return fmt.Errorf("enqueue profile image for person %d: %w", job.Args.PersonID, err)
		}
	}

	// 12. Mark enriched only after every durable write and follow-up enqueue.
	if err := q.MarkPersonEnriched(ctx, job.Args.PersonID); err != nil {
		return fmt.Errorf("mark person %d enriched: %w", job.Args.PersonID, err)
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
func storeExternalCredits(ctx context.Context, q *sqlc.Queries, personID int64, kind string, credits []heyametadata.HeyaCredit) error {
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

		if err := q.UpsertPersonExternalCredit(ctx, sqlc.UpsertPersonExternalCreditParams{
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
		}); err != nil {
			return fmt.Errorf("store %s credit for person %d: %w", kind, personID, err)
		}
	}
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
