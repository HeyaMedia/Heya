package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/rs/zerolog/log"
)

func (m *Matcher) createOrLinkMediaItem(ctx context.Context, detail *metadata.MediaDetail, kind metadata.MediaKind, libraryID int64, filePath string) (int64, bool, error) {
	extJSON, _ := json.Marshal(detail.ExternalIDs)

	// Only link by external IDs when we actually HAVE some. `external_ids @> '{}'`
	// matches every row, so an empty-ID stub (NFO-less / filename-only local)
	// would otherwise link onto an arbitrary existing media_item. Empty-ID
	// dedup is handled by natural identity (FindMediaItemByIdentity), not containment.
	hasExternalIDs := len(detail.ExternalIDs) > 0 && len(extJSON) > 0 &&
		string(extJSON) != "{}" && string(extJSON) != "null"

	if hasExternalIDs {
		existing, err := m.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
			LibraryID: libraryID,
			ExtFilter: extJSON,
		})
		if err == nil {
			log.Debug().Int64("id", existing.ID).Str("title", existing.Title).Msg("linked to existing media item")
			return existing.ID, false, nil
		}
	}

	mediaType := kindToMediaType(kind)
	sortTitle := strings.ToLower(detail.Title)

	item, err := m.q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:        libraryID,
		MediaType:        mediaType,
		Title:            detail.Title,
		SortTitle:        sortTitle,
		Year:             detail.Year,
		Description:      detail.Description,
		PosterPath:       detail.PosterURL,
		BackdropPath:     detail.BackdropURL,
		ExternalIds:      extJSON,
		Tagline:          detail.Tagline,
		OriginalTitle:    detail.OriginalTitle,
		OriginalLanguage: detail.OriginalLanguage,
		Status:           detail.Status,
		ProviderKind:     detail.ProviderKind,
		HeyaSlug:         detail.HeyaSlug,
	})
	if err != nil {
		if hasExternalIDs {
			existing, retryErr := m.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
				LibraryID: libraryID,
				ExtFilter: extJSON,
			})
			if retryErr == nil {
				log.Debug().Int64("id", existing.ID).Str("title", existing.Title).Msg("linked to existing media item (race resolved)")
				return existing.ID, false, nil
			}
		}
		return 0, false, fmt.Errorf("creating media item: %w", err)
	}

	itemSlug := slug.GenerateUnique(ctx, detail.Title, detail.Year, item.ID,
		func(ctx context.Context, s string, excludeID int64) (bool, error) {
			r, err := m.q.MediaItemSlugExists(ctx, sqlc.MediaItemSlugExistsParams{Slug: s, ID: excludeID})
			if err != nil {
				return false, err
			}
			return r, nil
		})
	m.q.UpdateMediaItemSlug(ctx, sqlc.UpdateMediaItemSlugParams{ID: item.ID, Slug: itemSlug})

	_ = m.q.MarkMatched(ctx, item.ID)

	return item.ID, true, nil
}

// createTypeSpecificRow inserts the type-specific row (movies / tv_series /
// books) for a media item. Called by the enrich path (after a successful
// GetDetail) — not by the match step, which writes only the media_items
// stub. Music is intentionally absent: the music enrich path goes through
// matcher.RefreshMusicArtist (album + track upsert from the artist payload)
// instead of a MediaDetail → CreateMusic shape, so this function no-ops for
// music. If a future feature needs a music candidate write here, route it
// through RefreshMusicArtist instead of resurrecting a separate path.
func (m *Matcher) createTypeSpecificRow(ctx context.Context, mediaItemID int64, kind metadata.MediaKind, detail *metadata.MediaDetail, filePath string) error {
	switch kind {
	case metadata.KindMovie:
		return m.createMovie(ctx, mediaItemID, detail)
	case metadata.KindTV:
		return m.createTVSeries(ctx, mediaItemID, detail)
	case metadata.KindBook:
		return m.createBook(ctx, mediaItemID, detail, filePath)
	}
	return nil
}

// createMovie writes the movies row from a remote detail. Get-or-create-or-fill:
//   - no row yet (normal first enrich, or a freshly materialized local stub) →
//     INSERT the full detail.
//   - row exists (local stub being upgraded by enrich, a re-identify, or a
//     forced refresh) → UPDATE with remote values, EXCEPT fields the user has
//     edited (field_provenance == "user"), which are preserved (edits win).
//
// Non-forced re-enrich never reaches here: the enrich worker gates the base
// component on BaseEnrichedAt. Rich metadata (cast/crew/…) is written by the
// caller's separate StoreRichMetadata, not here.
func (m *Matcher) createMovie(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	existing, gerr := m.q.GetMovieByMediaItemID(ctx, mediaItemID)
	switch {
	case errors.Is(gerr, pgx.ErrNoRows):
		_, err := m.q.CreateMovie(ctx, sqlc.CreateMovieParams{
			MediaItemID:      mediaItemID,
			RuntimeMinutes:   int32(d.RuntimeMinutes),
			Tagline:          d.Tagline,
			Genres:           emptyIfNil(d.Genres),
			Rating:           numericFromFloat(d.Rating),
			ReleaseDate:      pgDateFromString(d.ReleaseDate),
			OriginalTitle:    d.OriginalTitle,
			OriginalLanguage: d.OriginalLanguage,
			Budget:           d.Budget,
			Revenue:          d.Revenue,
			Popularity:       numericFromFloat(d.Popularity),
			SpokenLanguages:  emptyIfNil(d.SpokenLanguages),
			OriginCountry:    emptyIfNil(d.OriginCountry),
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	case gerr == nil:
		locked := m.lockedFields(ctx, mediaItemID)
		p := sqlc.UpdateMovieParams{
			ID:               existing.ID,
			RuntimeMinutes:   int32(d.RuntimeMinutes),
			Tagline:          d.Tagline,
			Genres:           emptyIfNil(d.Genres),
			Rating:           numericFromFloat(d.Rating),
			ReleaseDate:      pgDateFromString(d.ReleaseDate),
			OriginalTitle:    d.OriginalTitle,
			OriginalLanguage: d.OriginalLanguage,
			Budget:           d.Budget,
			Revenue:          d.Revenue,
			Popularity:       numericFromFloat(d.Popularity),
			SpokenLanguages:  emptyIfNil(d.SpokenLanguages),
			OriginCountry:    emptyIfNil(d.OriginCountry),
		}
		// Remote wins EXCEPT any field the user has edited (provenance == "user").
		if locked["genres"] {
			p.Genres = existing.Genres
		}
		if locked["runtime_minutes"] {
			p.RuntimeMinutes = existing.RuntimeMinutes
		}
		if locked["tagline"] {
			p.Tagline = existing.Tagline
		}
		if locked["rating"] {
			p.Rating = existing.Rating
		}
		if locked["release_date"] {
			p.ReleaseDate = existing.ReleaseDate
		}
		if locked["original_title"] {
			p.OriginalTitle = existing.OriginalTitle
		}
		if locked["original_language"] {
			p.OriginalLanguage = existing.OriginalLanguage
		}
		if locked["budget"] {
			p.Budget = existing.Budget
		}
		if locked["revenue"] {
			p.Revenue = existing.Revenue
		}
		if locked["popularity"] {
			p.Popularity = existing.Popularity
		}
		if locked["spoken_languages"] {
			p.SpokenLanguages = existing.SpokenLanguages
		}
		if locked["origin_country"] {
			p.OriginCountry = existing.OriginCountry
		}
		if _, err := m.q.UpdateMovie(ctx, p); err != nil {
			return err
		}
	default:
		return gerr
	}
	return nil
}

// lockedFields returns the set of base/type-specific field names a user has
// manually edited (field_provenance == "user") on the media_item — the enrich
// writers must not overwrite these.
func (m *Matcher) lockedFields(ctx context.Context, mediaItemID int64) map[string]bool {
	item, err := m.q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return nil
	}
	fp := ParseFieldProvenance(item.FieldProvenance)
	if len(fp) == 0 {
		return nil
	}
	locked := make(map[string]bool, len(fp))
	for field, src := range fp {
		if src == ProvUser {
			locked[field] = true
		}
	}
	return locked
}

// richErrs accumulates non-benign failures across storeRichMetadata's fan-out.
// The fan-out keeps going on individual failures (partial rich metadata beats
// none, and every insert is idempotent via ON CONFLICT), but the joined summary
// stops the caller from stamping the enrich component done — so a degraded
// cast/crew set gets retried instead of frozen. Capped: a dead connection
// would otherwise collect thousands of identical lines.
type richErrs struct {
	errs   []error
	total  int
	misses int // people/keywords/companies that could not be resolved or created
}

func (r *richErrs) add(err error) {
	r.total++
	if len(r.errs) < 8 {
		r.errs = append(r.errs, err)
	}
}

func (r *richErrs) result() error {
	out := append([]error{}, r.errs...)
	if r.total > len(r.errs) {
		out = append(out, fmt.Errorf("… and %d more failures", r.total-len(r.errs)))
	}
	if r.misses > 0 {
		out = append(out, fmt.Errorf("%d referenced people/keywords/companies could not be resolved", r.misses))
	}
	return errors.Join(out...)
}

func (r *richErrs) stopIfDone(ctx context.Context) bool {
	if err := ctx.Err(); err != nil {
		r.add(err)
		return true
	}
	return false
}

// richFailure records a fan-out error. A matcher scoped to an explicit
// PostgreSQL transaction must stop immediately: any statement error aborts the
// transaction, so continuing only hides the useful error behind a cascade of
// SQLSTATE 25P02 failures. Pool-backed fan-out retains the existing best-effort
// behaviour and continues writing independent components.
func (m *Matcher) richFailure(re *richErrs, err error) bool {
	re.add(err)
	return m.inTx
}

type richPersonCredit struct {
	isCast      bool
	canonicalID string
	externalIDs map[string]string
	name        string
	character   string
	job         string
	department  string
	order       int
	gender      int
	profilePath string
	profiles    []metadata.ProfileImage
	popularity  float64
	source      string
}

type resolvedPersonCredit struct {
	credit richPersonCredit
	person sqlc.Person
}

type richPersonProfile struct {
	image     metadata.ProfileImage
	sortOrder int
}

type richPersonWrite struct {
	person       sqlc.Person
	canonicalIDs map[string]struct{}
	profiles     map[string]richPersonProfile
}

func collectRichPersonCredits(d *metadata.MediaDetail) []richPersonCredit {
	credits := make([]richPersonCredit, 0, len(d.Cast)+len(d.Crew))
	seenCast := map[string]bool{}
	for _, c := range d.Cast {
		dedup := c.Name + "|" + c.Character
		if seenCast[dedup] {
			continue
		}
		seenCast[dedup] = true
		credits = append(credits, richPersonCredit{
			isCast: true, canonicalID: c.CanonicalID, externalIDs: c.ExternalIDs,
			name: c.Name, character: c.Character, order: c.Order, gender: c.Gender,
			profilePath: c.ProfilePath, profiles: c.Profiles, popularity: c.Popularity,
			source: c.Source,
		})
	}

	seenCrew := map[string]bool{}
	for _, c := range d.Crew {
		dedup := c.Name + "|" + c.Job
		if seenCrew[dedup] {
			continue
		}
		seenCrew[dedup] = true
		credits = append(credits, richPersonCredit{
			canonicalID: c.CanonicalID, externalIDs: c.ExternalIDs, name: c.Name,
			job: c.Job, department: c.Department, gender: c.Gender,
			profilePath: c.ProfilePath, profiles: c.Profiles, source: c.Source,
		})
	}

	// Cast and crew are deliberately one ordering domain. A person can be cast
	// in one title and crew in another; ordering the two lists separately still
	// permits opposite row-lock orders between concurrent applies.
	sort.SliceStable(credits, func(i, j int) bool {
		return richPersonCreditKey(credits[i]) < richPersonCreditKey(credits[j])
	})
	return credits
}

func richPersonCreditKey(c richPersonCredit) string {
	identity := strings.ToLower(strings.TrimSpace(c.canonicalID))
	if identity == "" {
		parts := make([]string, 0, len(c.externalIDs))
		for _, key := range sortedNonEmptyExternalIDKeys(c.externalIDs) {
			parts = append(parts, strings.ToLower(key)+"="+strings.ToLower(c.externalIDs[key]))
		}
		identity = strings.Join(parts, "|")
	}
	if identity == "" {
		identity = strings.ToLower(strings.TrimSpace(c.name))
	}
	role := "crew|" + c.department + "|" + c.job
	if c.isCast {
		role = "cast|" + c.character
	}
	return identity + "|" + role + "|" + c.name
}

func sortedNonEmptyExternalIDKeys(ids map[string]string) []string {
	keys := make([]string, 0, len(ids))
	for key, value := range ids {
		if value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func (m *Matcher) storeRichMetadata(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	var re richErrs
	credits := collectRichPersonCredits(d)
	resolved := make([]resolvedPersonCredit, 0, len(credits))
	for _, credit := range credits {
		if re.stopIfDone(ctx) {
			return re.result()
		}
		person, err := m.findOrCreatePerson(ctx, credit.name, credit.externalIDs, credit.gender, credit.profilePath, credit.popularity)
		if err != nil {
			if m.richFailure(&re, fmt.Errorf("resolve person %q: %w", credit.name, err)) {
				return re.result()
			}
			continue
		}
		if person.ID == 0 {
			re.misses++
			continue
		}
		resolved = append(resolved, resolvedPersonCredit{credit: credit, person: person})
	}

	// Resolve first, then acquire every shared person/profile/binding row in
	// ascending local person ID order. Local IDs are the actual PostgreSQL lock
	// keys, making this robust even when two payloads describe the same person
	// with different roles or identifier subsets.
	sortResolvedPersonCredits(resolved)
	writes := make([]*richPersonWrite, 0, len(resolved))
	for _, item := range resolved {
		var write *richPersonWrite
		if len(writes) == 0 || writes[len(writes)-1].person.ID != item.person.ID {
			write = &richPersonWrite{
				person: item.person, canonicalIDs: map[string]struct{}{},
				profiles: map[string]richPersonProfile{},
			}
			writes = append(writes, write)
		} else {
			write = writes[len(writes)-1]
		}
		if item.credit.canonicalID != "" {
			write.canonicalIDs[item.credit.canonicalID] = struct{}{}
		}
		for index, profile := range item.credit.profiles {
			if profile.URL == "" {
				continue
			}
			if existing, ok := write.profiles[profile.URL]; !ok || index < existing.sortOrder {
				write.profiles[profile.URL] = richPersonProfile{image: profile, sortOrder: index}
			}
		}
	}

	for _, write := range writes {
		profileURLs := make([]string, 0, len(write.profiles))
		for url := range write.profiles {
			profileURLs = append(profileURLs, url)
		}
		sort.Strings(profileURLs)
		for _, url := range profileURLs {
			profile := write.profiles[url]
			if err := m.q.CreatePersonProfile(ctx, sqlc.CreatePersonProfileParams{
				PersonID: write.person.ID, Url: profile.image.URL, Source: profile.image.Source,
				Aspect: fallbackAspect(profile.image.Aspect), Width: int32(profile.image.Width),
				Height: int32(profile.image.Height), Score: numericFromFloat(profile.image.Score),
				SortOrder: int32(profile.sortOrder),
			}); err != nil {
				if m.richFailure(&re, fmt.Errorf("profile for person %q: %w", write.person.Name, err)) {
					return re.result()
				}
			}
		}

		canonicalIDs := make([]string, 0, len(write.canonicalIDs))
		for id := range write.canonicalIDs {
			canonicalIDs = append(canonicalIDs, id)
		}
		sort.Strings(canonicalIDs)
		for _, canonicalID := range canonicalIDs {
			if err := m.bindCanonical(ctx, "person", write.person.ID, canonicalID, "person", 1, 0); err != nil {
				if m.richFailure(&re, fmt.Errorf("bind person %q: %w", write.person.Name, err)) {
					return re.result()
				}
			}
		}
	}

	for _, item := range resolved {
		credit, person := item.credit, item.person
		if credit.isCast {
			if err := m.q.CreateMediaCast(ctx, sqlc.CreateMediaCastParams{
				MediaItemID:  mediaItemID,
				PersonID:     person.ID,
				Character:    credit.character,
				DisplayOrder: int32(credit.order),
				Gender:       int32(credit.gender),
				Source:       credit.source,
			}); err != nil {
				if m.richFailure(&re, fmt.Errorf("cast %q: %w", credit.name, err)) {
					return re.result()
				}
			}
			continue
		}
		if err := m.q.CreateMediaCrew(ctx, sqlc.CreateMediaCrewParams{
			MediaItemID: mediaItemID,
			PersonID:    person.ID,
			Job:         credit.job,
			Department:  credit.department,
			Gender:      int32(credit.gender),
			Source:      credit.source,
		}); err != nil {
			if m.richFailure(&re, fmt.Errorf("crew %q: %w", credit.name, err)) {
				return re.result()
			}
		}
	}

	seenKeywords := map[string]bool{}
	for _, k := range d.Keywords {
		if re.stopIfDone(ctx) {
			return re.result()
		}
		if seenKeywords[k.Name] {
			continue
		}
		seenKeywords[k.Name] = true

		kw := m.findOrCreateKeyword(ctx, k.Name, k.ExternalIDs)
		if kw.ID == 0 {
			re.misses++
			continue
		}
		if err := m.q.LinkMediaKeyword(ctx, sqlc.LinkMediaKeywordParams{
			MediaItemID: mediaItemID,
			KeywordID:   kw.ID,
		}); err != nil {
			re.add(fmt.Errorf("keyword %q: %w", k.Name, err))
			if ctx.Err() != nil {
				return re.result()
			}
		}
	}

	seenCompanies := map[string]bool{}
	for _, pc := range d.ProductionCompanies {
		if re.stopIfDone(ctx) {
			return re.result()
		}
		if seenCompanies[pc.Name] {
			continue
		}
		seenCompanies[pc.Name] = true

		co := m.findOrCreateCompany(ctx, pc.Name, pc.ExternalIDs, pc.LogoPath, pc.OriginCountry)
		if co.ID == 0 {
			re.misses++
			continue
		}
		if err := m.q.LinkMediaProductionCompany(ctx, sqlc.LinkMediaProductionCompanyParams{
			MediaItemID: mediaItemID,
			CompanyID:   co.ID,
		}); err != nil {
			re.add(fmt.Errorf("company %q: %w", pc.Name, err))
			if ctx.Err() != nil {
				return re.result()
			}
		}
	}

	for _, v := range d.Videos {
		if re.stopIfDone(ctx) {
			return re.result()
		}
		if err := m.q.CreateMediaVideo(ctx, sqlc.CreateMediaVideoParams{
			MediaItemID: mediaItemID,
			ProviderKey: v.ProviderKey,
			Name:        v.Name,
			Site:        v.Site,
			VideoKey:    v.Key,
			VideoType:   v.Type,
			Language:    v.Language,
			Official:    v.Official,
		}); err != nil {
			re.add(fmt.Errorf("video %q: %w", v.Name, err))
			if ctx.Err() != nil {
				return re.result()
			}
		}
	}

	for _, c := range d.Certifications {
		if re.stopIfDone(ctx) {
			return re.result()
		}
		if err := m.q.CreateMediaCertification(ctx, sqlc.CreateMediaCertificationParams{
			MediaItemID:   mediaItemID,
			Country:       c.Country,
			Certification: c.Certification,
			ReleaseDate:   pgDateFromString(c.ReleaseDate),
			ReleaseType:   int32(c.ReleaseType),
			Source:        c.Source,
		}); err != nil {
			re.add(fmt.Errorf("certification %s: %w", c.Country, err))
			if ctx.Err() != nil {
				return re.result()
			}
		}
	}

	for _, r := range d.Recommendations {
		if re.stopIfDone(ctx) {
			return re.result()
		}
		if err := m.q.CreateMediaRecommendation(ctx, sqlc.CreateMediaRecommendationParams{
			MediaItemID:   mediaItemID,
			ExternalIds:   mustJSON(r.ExternalIDs),
			Title:         r.Title,
			PosterPath:    r.PosterPath,
			MediaType:     r.MediaType,
			VoteAverage:   numericFromFloat(r.VoteAverage),
			ProviderScore: r.ProviderScore,
			ReleaseDate:   r.ReleaseDate,
		}); err != nil {
			re.add(fmt.Errorf("recommendation %q: %w", r.Title, err))
			if ctx.Err() != nil {
				return re.result()
			}
		}
	}

	if re.stopIfDone(ctx) {
		return re.result()
	}
	if d.Collection != nil && d.Collection.Name != "" && m.shouldAutoCollect(ctx, mediaItemID) {
		m.linkCollection(ctx, mediaItemID, d.Collection)
	}

	// Merge the social/link IDs the enrich payload carries beyond the core
	// provider IDs into media_items.external_ids. (This block used to write
	// the row back unchanged — the merge below is what it always meant to do.)
	if d.ExternalIDs["wikidata"] != "" || d.ExternalIDs["facebook"] != "" || d.ExternalIDs["instagram"] != "" || d.ExternalIDs["twitter"] != "" || d.Homepage != "" {
		item, err := m.q.GetMediaItemByID(ctx, mediaItemID)
		if err == nil {
			ids := map[string]string{}
			_ = json.Unmarshal(item.ExternalIds, &ids)
			changed := false
			for _, k := range []string{"wikidata", "facebook", "instagram", "twitter"} {
				if v := d.ExternalIDs[k]; v != "" && ids[k] != v {
					ids[k] = v
					changed = true
				}
			}
			if d.Homepage != "" && ids["homepage"] != d.Homepage {
				ids["homepage"] = d.Homepage
				changed = true
			}
			if changed {
				if _, err := m.q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
					ID:               item.ID,
					Title:            item.Title,
					SortTitle:        item.SortTitle,
					Year:             item.Year,
					Description:      item.Description,
					PosterPath:       item.PosterPath,
					BackdropPath:     item.BackdropPath,
					ExternalIds:      mustJSON(ids),
					Tagline:          item.Tagline,
					OriginalTitle:    item.OriginalTitle,
					OriginalLanguage: item.OriginalLanguage,
					Status:           item.Status,
					ProviderKind:     item.ProviderKind,
					HeyaSlug:         item.HeyaSlug,
				}); err != nil {
					re.add(fmt.Errorf("external ids: %w", err))
				}
			}
		}
	}

	for _, t := range d.Titles {
		if re.stopIfDone(ctx) {
			return re.result()
		}
		if err := m.q.CreateMediaTitle(ctx, sqlc.CreateMediaTitleParams{
			MediaItemID: mediaItemID,
			Title:       t.Title,
			Language:    t.Language,
			Country:     t.Country,
			TitleType:   t.TitleType,
			Source:      t.Source,
		}); err != nil {
			re.add(fmt.Errorf("title %q: %w", t.Title, err))
			if ctx.Err() != nil {
				return re.result()
			}
		}
	}

	for lang, text := range d.Overviews {
		if re.stopIfDone(ctx) {
			return re.result()
		}
		if err := m.q.CreateMediaOverview(ctx, sqlc.CreateMediaOverviewParams{
			MediaItemID: mediaItemID,
			Language:    lang,
			Overview:    text,
		}); err != nil {
			re.add(fmt.Errorf("overview %s: %w", lang, err))
			if ctx.Err() != nil {
				return re.result()
			}
		}
	}

	log.Info().Int64("media_id", mediaItemID).
		Int("cast", len(d.Cast)).
		Int("crew", len(d.Crew)).
		Int("keywords", len(d.Keywords)).
		Int("videos", len(d.Videos)).
		Int("recs", len(d.Recommendations)).
		Int("titles", len(d.Titles)).
		Int("overviews", len(d.Overviews)).
		Msg("stored rich metadata")

	return re.result()
}

func (m *Matcher) findOrCreatePerson(ctx context.Context, name string, externalIDs map[string]string, gender int, profilePath string, popularity float64) (sqlc.Person, error) {
	for _, key := range sortedNonEmptyExternalIDKeys(externalIDs) {
		probe := mustJSON(map[string]string{key: externalIDs[key]})
		existing, err := m.q.FindPersonByExternalID(ctx, probe)
		switch {
		case err == nil:
			return existing, nil
		case errors.Is(err, pgx.ErrNoRows):
			continue
		default:
			return sqlc.Person{}, err
		}
	}

	created, err := m.q.CreatePerson(ctx, sqlc.CreatePersonParams{
		ExternalIds: mustJSON(externalIDs),
		Name:        name,
		AlsoKnownAs: []string{},
		Gender:      int32(gender),
		ProfilePath: profilePath,
		Popularity:  numericFromFloat(popularity),
	})
	if err != nil {
		return sqlc.Person{}, err
	}
	return created, nil
}

func sortResolvedPersonCredits(resolved []resolvedPersonCredit) {
	sort.SliceStable(resolved, func(i, j int) bool {
		if resolved[i].person.ID != resolved[j].person.ID {
			return resolved[i].person.ID < resolved[j].person.ID
		}
		return richPersonCreditKey(resolved[i].credit) < richPersonCreditKey(resolved[j].credit)
	})
}

func fallbackAspect(a string) string {
	if a == "" {
		return "profile"
	}
	return a
}

func (m *Matcher) findOrCreateKeyword(ctx context.Context, name string, externalIDs map[string]string) sqlc.Keyword {
	if existing, err := m.q.FindKeywordByName(ctx, name); err == nil {
		return existing
	}
	kw, err := m.q.CreateKeyword(ctx, sqlc.CreateKeywordParams{
		ExternalIds: mustJSON(externalIDs),
		Name:        name,
	})
	if err != nil {
		return sqlc.Keyword{}
	}
	return kw
}

func (m *Matcher) findOrCreateCompany(ctx context.Context, name string, externalIDs map[string]string, logoPath, country string) sqlc.ProductionCompany {
	for k, v := range externalIDs {
		if v == "" {
			continue
		}
		if existing, err := m.q.FindProductionCompanyByExternalID(ctx, mustJSON(map[string]string{k: v})); err == nil {
			return existing
		}
	}
	if existing, err := m.q.FindProductionCompanyByName(ctx, name); err == nil {
		return existing
	}
	co, err := m.q.CreateProductionCompany(ctx, sqlc.CreateProductionCompanyParams{
		ExternalIds:   mustJSON(externalIDs),
		Name:          name,
		LogoPath:      logoPath,
		OriginCountry: country,
	})
	if err != nil {
		return sqlc.ProductionCompany{}
	}
	return co
}

// linkCollection find-or-creates the collection (franchise) row and points
// the movie's collection_id at it. Collections carry no unique constraint on
// name, so a blind insert per enrich would pile up duplicate rows — an
// existing row is refreshed with the latest artwork/overview instead.
func (m *Matcher) linkCollection(ctx context.Context, mediaItemID int64, c *metadata.CollectionDetail) {
	// Always a valid JSON array — an empty/nil slice must be "[]", not "null",
	// or UpdateCollection's jsonb_array_length guard errors on a scalar.
	partsJSON := []byte("[]")
	if len(c.Parts) > 0 {
		partsJSON = mustJSON(c.Parts)
	}
	col, err := m.q.FindCollectionByName(ctx, c.Name)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		col, err = m.q.CreateCollection(ctx, sqlc.CreateCollectionParams{
			ExternalIds:  mustJSON(c.ExternalIDs),
			Name:         c.Name,
			Overview:     c.Overview,
			PosterPath:   c.PosterPath,
			BackdropPath: c.BackdropPath,
			Parts:        partsJSON,
		})
		if err != nil {
			log.Debug().Err(err).Str("collection", c.Name).Msg("failed to create collection")
			return
		}
	case err != nil:
		log.Debug().Err(err).Str("collection", c.Name).Msg("failed to look up collection")
		return
	default:
		if err := m.q.UpdateCollection(ctx, sqlc.UpdateCollectionParams{
			ID:           col.ID,
			ExternalIds:  mustJSON(c.ExternalIDs),
			Overview:     c.Overview,
			PosterPath:   c.PosterPath,
			BackdropPath: c.BackdropPath,
			Column6:      partsJSON,
		}); err != nil {
			log.Debug().Err(err).Str("collection", c.Name).Msg("failed to refresh collection")
		}
	}
	if err := m.q.SetMovieCollection(ctx, sqlc.SetMovieCollectionParams{
		MediaItemID:  mediaItemID,
		CollectionID: pgtype.Int8{Int64: col.ID, Valid: true},
	}); err != nil {
		log.Debug().Err(err).Int64("media_id", mediaItemID).Str("collection", c.Name).Msg("failed to link movie to collection")
	}
}

func (m *Matcher) shouldAutoCollect(ctx context.Context, mediaItemID int64) bool {
	item, err := m.q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return true
	}
	lib, err := m.q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return true
	}
	settings := metadata.ParseSettings(lib.Settings)
	if settings.IsEmpty() {
		return true
	}
	return settings.AutoCollections
}

func (m *Matcher) createTVSeries(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	series, err := m.upsertTVSeriesRow(ctx, mediaItemID, d)
	if err != nil {
		return err
	}

	if !m.lockedFields(ctx, mediaItemID)["networks"] {
		m.linkNetworks(ctx, series.ID, d.Networks)
	}
	m.linkCreators(ctx, series.ID, d.CreatedBy)

	for _, sd := range d.Seasons {
		seasonExtIDs := map[string]string{}
		if sd.TmdbSeasonID != 0 {
			seasonExtIDs["tmdb"] = fmt.Sprintf("%d", sd.TmdbSeasonID)
		}
		if sd.TvdbSeasonID != 0 {
			seasonExtIDs["tvdb"] = fmt.Sprintf("%d", sd.TvdbSeasonID)
		}
		if sd.AnidbID != 0 {
			seasonExtIDs["anidb"] = fmt.Sprintf("%d", sd.AnidbID)
		}

		season, err := m.q.CreateTVSeason(ctx, sqlc.CreateTVSeasonParams{
			SeriesID:      series.ID,
			SeasonNumber:  int32(sd.Number),
			Title:         sd.Title,
			Overview:      sd.Overview,
			PosterPath:    sd.PosterURL,
			AirDate:       pgDateFromString(sd.AirDate),
			EndDate:       pgDateFromString(sd.EndDate),
			Status:        sd.Status,
			AiredEpisodes: int32(sd.AiredEpisodes),
			ExternalIds:   mustJSON(seasonExtIDs),
		})
		// DO NOTHING → no row when the season already exists; recover its id so
		// new episodes can still be attached under it.
		if errors.Is(err, pgx.ErrNoRows) {
			season, err = m.q.GetTVSeason(ctx, sqlc.GetTVSeasonParams{SeriesID: series.ID, SeasonNumber: int32(sd.Number)})
		}
		if err != nil {
			log.Warn().Err(err).Int("season", sd.Number).Msg("error creating season")
			continue
		}
		if err := m.bindCanonical(ctx, "tv_season", season.ID, sd.CanonicalID, "season", d.SchemaVersion, d.ProjectionVersion); err != nil {
			log.Warn().Err(err).Int64("season_id", season.ID).Msg("bind canonical TV season")
		}

		for _, ep := range sd.Episodes {
			epExtIDs := map[string]string{}
			if ep.TmdbID != 0 {
				epExtIDs["tmdb"] = fmt.Sprintf("%d", ep.TmdbID)
			}
			if ep.TvdbID != 0 {
				epExtIDs["tvdb"] = fmt.Sprintf("%d", ep.TvdbID)
			}

			tvEp, err := m.q.CreateTVEpisode(ctx, sqlc.CreateTVEpisodeParams{
				SeasonID:       season.ID,
				EpisodeNumber:  int32(ep.Number),
				Title:          ep.Title,
				Overview:       ep.Overview,
				StillPath:      ep.StillURL,
				RuntimeMinutes: int32(ep.RuntimeMinutes),
				AirDate:        pgDateFromString(ep.AirDate),
				Rating:         numericFromFloat(ep.Rating),
				AbsoluteNumber: int32(ep.AbsoluteNumber),
				IsSpecial:      ep.IsSpecial,
				EpisodeType:    int32(ep.EpisodeType),
				ExternalIds:    mustJSON(epExtIDs),
				Source:         ep.Source,
			})
			// DO NOTHING → episode already exists; preserve it (incl. user edits)
			// and skip its title/overview re-insert below.
			if errors.Is(err, pgx.ErrNoRows) {
				tvEp, err = m.q.GetTVEpisode(ctx, sqlc.GetTVEpisodeParams{SeasonID: season.ID, EpisodeNumber: int32(ep.Number)})
			}
			if err != nil {
				log.Warn().Err(err).Int("episode", ep.Number).Msg("error creating episode")
				continue
			}
			if err := m.bindCanonical(ctx, "tv_episode", tvEp.ID, ep.CanonicalID, "episode", d.SchemaVersion, d.ProjectionVersion); err != nil {
				log.Warn().Err(err).Int64("episode_id", tvEp.ID).Msg("bind canonical TV episode")
			}
			for _, t := range ep.Titles {
				m.q.CreateEpisodeTitle(ctx, sqlc.CreateEpisodeTitleParams{
					EpisodeID: tvEp.ID,
					Title:     t.Title,
					Language:  t.Language,
					Source:    t.Source,
				})
			}
			for lang, text := range ep.Overviews {
				m.q.CreateEpisodeOverview(ctx, sqlc.CreateEpisodeOverviewParams{
					EpisodeID: tvEp.ID,
					Language:  lang,
					Overview:  text,
				})
			}
		}
	}

	return nil
}

// upsertTVSeriesRow writes the tv_series row from a remote detail, mirroring
// createMovie's get-or-create-or-fill + provenance rules. Returns the row so
// the caller can attach networks/creators/seasons to series.ID.
func (m *Matcher) upsertTVSeriesRow(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) (sqlc.TvSeries, error) {
	existing, gerr := m.q.GetTVSeriesByMediaItemID(ctx, mediaItemID)
	switch {
	case errors.Is(gerr, pgx.ErrNoRows):
		s, err := m.q.CreateTVSeries(ctx, sqlc.CreateTVSeriesParams{
			MediaItemID:      mediaItemID,
			Status:           d.Status,
			Genres:           emptyIfNil(d.Genres),
			Rating:           numericFromFloat(d.Rating),
			FirstAirDate:     pgDateFromString(d.FirstAirDate),
			LastAirDate:      pgDateFromString(d.LastAirDate),
			OriginalName:     d.OriginalName,
			OriginalLanguage: d.OriginalLanguage,
			NumberOfSeasons:  int32(d.NumberOfSeasons),
			NumberOfEpisodes: int32(d.NumberOfEpisodes),
			Popularity:       numericFromFloat(d.Popularity),
			SpokenLanguages:  emptyIfNil(d.SpokenLanguages),
			OriginCountry:    emptyIfNil(d.OriginCountry),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Concurrent insert won the conflict — fetch the existing row.
			return m.q.GetTVSeriesByMediaItemID(ctx, mediaItemID)
		}
		if err != nil {
			return sqlc.TvSeries{}, fmt.Errorf("creating tv series: %w", err)
		}
		return s, nil
	case gerr == nil:
		locked := m.lockedFields(ctx, mediaItemID)
		p := sqlc.UpdateTVSeriesParams{
			ID:               existing.ID,
			Status:           d.Status,
			Genres:           emptyIfNil(d.Genres),
			Rating:           numericFromFloat(d.Rating),
			FirstAirDate:     pgDateFromString(d.FirstAirDate),
			LastAirDate:      pgDateFromString(d.LastAirDate),
			OriginalName:     d.OriginalName,
			OriginalLanguage: d.OriginalLanguage,
			NumberOfSeasons:  int32(d.NumberOfSeasons),
			NumberOfEpisodes: int32(d.NumberOfEpisodes),
			Popularity:       numericFromFloat(d.Popularity),
			SpokenLanguages:  emptyIfNil(d.SpokenLanguages),
			OriginCountry:    emptyIfNil(d.OriginCountry),
		}
		// Remote wins EXCEPT any user-edited field.
		if locked["genres"] {
			p.Genres = existing.Genres
		}
		if locked["status"] {
			p.Status = existing.Status
		}
		if locked["rating"] {
			p.Rating = existing.Rating
		}
		if locked["first_air_date"] {
			p.FirstAirDate = existing.FirstAirDate
		}
		if locked["last_air_date"] {
			p.LastAirDate = existing.LastAirDate
		}
		if locked["original_name"] {
			p.OriginalName = existing.OriginalName
		}
		if locked["original_language"] {
			p.OriginalLanguage = existing.OriginalLanguage
		}
		if locked["popularity"] {
			p.Popularity = existing.Popularity
		}
		if locked["spoken_languages"] {
			p.SpokenLanguages = existing.SpokenLanguages
		}
		if locked["origin_country"] {
			p.OriginCountry = existing.OriginCountry
		}
		s, err := m.q.UpdateTVSeries(ctx, p)
		if err != nil {
			return sqlc.TvSeries{}, fmt.Errorf("updating tv series: %w", err)
		}
		return s, nil
	default:
		return sqlc.TvSeries{}, gerr
	}
}

func (m *Matcher) linkNetworks(ctx context.Context, seriesID int64, nets []metadata.NetworkDetail) {
	m.q.DeleteNetworksForSeries(ctx, seriesID)
	for i, n := range nets {
		if n.Name == "" {
			continue
		}
		net, err := m.upsertNetwork(ctx, n)
		if err != nil || net.ID == 0 {
			continue
		}
		m.q.AttachNetworkToSeries(ctx, sqlc.AttachNetworkToSeriesParams{
			SeriesID:  seriesID,
			NetworkID: net.ID,
			SortOrder: int32(i),
		})
	}
}

func (m *Matcher) upsertNetwork(ctx context.Context, n metadata.NetworkDetail) (sqlc.Network, error) {
	if len(n.ExternalIDs) > 0 {
		if existing, err := m.q.FindNetworkByExternalID(ctx, mustJSON(n.ExternalIDs)); err == nil {
			return existing, nil
		}
	}
	return m.q.UpsertNetworkByExternalIDs(ctx, sqlc.UpsertNetworkByExternalIDsParams{
		Name:        n.Name,
		ExternalIds: mustJSON(n.ExternalIDs),
		LogoPath:    n.LogoPath,
		Country:     n.Country,
	})
}

func (m *Matcher) linkCreators(ctx context.Context, seriesID int64, creators []metadata.CreatorDetail) {
	m.q.DeleteCreatorsForSeries(ctx, seriesID)
	for i, c := range creators {
		if c.Name == "" {
			continue
		}
		cr, err := m.upsertCreator(ctx, c)
		if err != nil || cr.ID == 0 {
			continue
		}
		m.q.AttachCreatorToSeries(ctx, sqlc.AttachCreatorToSeriesParams{
			SeriesID:  seriesID,
			CreatorID: cr.ID,
			SortOrder: int32(i),
		})
	}
}

func (m *Matcher) upsertCreator(ctx context.Context, c metadata.CreatorDetail) (sqlc.Creator, error) {
	if len(c.ExternalIDs) > 0 {
		if existing, err := m.q.FindCreatorByExternalID(ctx, mustJSON(c.ExternalIDs)); err == nil {
			return existing, nil
		}
	}
	return m.q.UpsertCreatorByExternalIDs(ctx, sqlc.UpsertCreatorByExternalIDsParams{
		Name:        c.Name,
		ExternalIds: mustJSON(c.ExternalIDs),
	})
}

func (m *Matcher) createBook(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail, filePath string) error {
	var authorID pgtype.Int8

	if d.AuthorName != "" {
		existing, err := m.q.GetAuthorByName(ctx, d.AuthorName)
		if err == nil {
			authorID = pgtype.Int8{Int64: existing.ID, Valid: true}
		} else {
			author, err := m.q.CreateAuthor(ctx, sqlc.CreateAuthorParams{
				Name:          d.AuthorName,
				OpenlibraryID: firstNonEmptyString(d.AuthorExternalIDs["openlibrary"], d.AuthorExternalIDs["ol_author_id"], d.ExternalIDs["openlibrary_author"]),
				Biography:     d.AuthorBio,
				BirthDate:     d.AuthorBirthDate,
				DeathDate:     d.AuthorDeathDate,
			})
			if err != nil {
				log.Warn().Err(err).Str("author", d.AuthorName).Msg("error creating author")
			} else {
				authorID = pgtype.Int8{Int64: author.ID, Valid: true}
			}
		}
		if authorID.Valid {
			if err := m.bindCanonical(ctx, "author", authorID.Int64, d.AuthorCanonicalID, "author", 1, 0); err != nil {
				return fmt.Errorf("bind author %d to canonical metadata: %w", authorID.Int64, err)
			}
		}
	}

	ext := ""
	if idx := strings.LastIndex(filePath, "."); idx >= 0 {
		ext = filePath[idx+1:]
	}

	// Get-or-create-or-fill, like createMovie/createTVSeries. The plain
	// INSERT-only version unique-violated books.media_item_id on any re-write
	// (forced refresh / re-identify / re-scan), which — now that the error is no
	// longer swallowed — marked the book failed. On an existing row we UPDATE
	// instead. Enrich reaches here with filePath=="" (it carries metadata, not
	// the local file), so preserve the stored path/format rather than blanking
	// them; only the local materialize/scan paths supply a real filePath.
	existing, gerr := m.q.GetBookByMediaItemID(ctx, mediaItemID)
	switch {
	case errors.Is(gerr, pgx.ErrNoRows):
		if _, err := m.q.CreateBook(ctx, sqlc.CreateBookParams{
			MediaItemID:   mediaItemID,
			AuthorID:      authorID,
			Isbn:          d.ISBN,
			OpenlibraryID: firstNonEmptyString(d.ExternalIDs["openlibrary"], d.ExternalIDs["ol_work_id"]),
			PageCount:     int32(d.PageCount),
			Publisher:     d.Publisher,
			PublishDate:   pgDateFromString(d.PublishDate),
			FilePath:      filePath,
			Subjects:      emptyIfNil(d.Subjects),
			Language:      d.Language,
			SeriesName:    d.SeriesName,
			SeriesNumber:  int32(d.SeriesNum),
			Format:        ext,
			Description:   d.Description,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	case gerr == nil:
		locked := m.lockedFields(ctx, mediaItemID)
		filePathToStore, formatToStore := filePath, ext
		if filePath == "" {
			filePathToStore, formatToStore = existing.FilePath, existing.Format
		}
		p := sqlc.UpdateBookParams{
			ID:            existing.ID,
			AuthorID:      authorID,
			Isbn:          d.ISBN,
			OpenlibraryID: firstNonEmptyString(d.ExternalIDs["openlibrary"], d.ExternalIDs["ol_work_id"]),
			PageCount:     int32(d.PageCount),
			Publisher:     d.Publisher,
			PublishDate:   pgDateFromString(d.PublishDate),
			FilePath:      filePathToStore,
			Subjects:      emptyIfNil(d.Subjects),
			Language:      d.Language,
			SeriesName:    d.SeriesName,
			SeriesNumber:  int32(d.SeriesNum),
			Format:        formatToStore,
			Description:   d.Description,
		}
		if locked["author"] {
			p.AuthorID = existing.AuthorID
		}
		if locked["isbn"] {
			p.Isbn = existing.Isbn
		}
		if locked["page_count"] {
			p.PageCount = existing.PageCount
		}
		if locked["publisher"] {
			p.Publisher = existing.Publisher
		}
		if locked["publish_date"] {
			p.PublishDate = existing.PublishDate
		}
		if locked["subjects"] {
			p.Subjects = existing.Subjects
		}
		if locked["language"] {
			p.Language = existing.Language
		}
		if locked["series_name"] {
			p.SeriesName = existing.SeriesName
		}
		if locked["series_number"] {
			p.SeriesNumber = existing.SeriesNumber
		}
		if locked["format"] {
			p.Format = existing.Format
		}
		if locked["description"] {
			p.Description = existing.Description
		}
		if _, err := m.q.UpdateBook(ctx, p); err != nil {
			return err
		}
	default:
		return gerr
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func kindToMediaType(kind metadata.MediaKind) sqlc.MediaType {
	switch kind {
	case metadata.KindMovie:
		return sqlc.MediaTypeMovie
	case metadata.KindTV:
		return sqlc.MediaTypeTv
	case metadata.KindMusic:
		return sqlc.MediaTypeMusic
	case metadata.KindBook:
		return sqlc.MediaTypeBook
	default:
		return sqlc.MediaTypeMovie
	}
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	if b == nil {
		return []byte("{}")
	}
	return b
}

// StoreEntityMetadata persists type-specific metadata (movie, TV, music, book)
// for a media item. Called by the worker pipeline (EnrichMediaItemWorker) and
// the metadata editor after a manual metadata refresh. The match step does
// NOT call this — it writes only the media_items stub via
// createOrLinkMediaItem, and the enrich worker fills in the type-specific
// row here.
func (m *Matcher) StoreEntityMetadata(ctx context.Context, mediaItemID int64, kind metadata.MediaKind, detail *metadata.MediaDetail) error {
	// pgx.ErrNoRows here is the benign "row already exists" no-op (ON CONFLICT DO
	// NOTHING on a re-enrich) — not a failure. Any OTHER error means the
	// type-specific row (movies / tv_series / books) was NOT written; the library
	// grid INNER JOINs on it, so a missing row makes the item invisible. Return
	// the error so the caller doesn't stamp the item enrichment_status='complete'
	// (which would strand it invisible-and-never-retried).
	if err := m.createTypeSpecificRow(ctx, mediaItemID, kind, detail, ""); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("store type-specific metadata (media %d, %s): %w", mediaItemID, kind, err)
	}
	if err := m.bindCanonical(ctx, "media_item", mediaItemID, detail.CanonicalID, detail.CanonicalKind, detail.SchemaVersion, detail.ProjectionVersion); err != nil {
		return fmt.Errorf("store canonical metadata binding (media %d): %w", mediaItemID, err)
	}
	if detail.CanonicalID != "" {
		if err := m.q.PromoteCanonicalMetadataProviderID(ctx, sqlc.PromoteCanonicalMetadataProviderIDParams{
			MediaItemID:        pgInt8(mediaItemID),
			MetadataProviderID: heyametadata.EncodeEntityProviderID(detail.CanonicalID),
		}); err != nil {
			return fmt.Errorf("promote local metadata identity %d to canonical UUID: %w", mediaItemID, err)
		}
	}
	return nil
}

func (m *Matcher) bindCanonical(ctx context.Context, localKind string, localID int64, entityID, entityKind string, schemaVersion int, projectionVersion int64) error {
	if entityID == "" {
		return nil
	}
	id, err := uuid.Parse(entityID)
	if err != nil {
		return fmt.Errorf("invalid canonical entity UUID %q: %w", entityID, err)
	}
	if entityKind == "" {
		return fmt.Errorf("canonical entity %s has no kind", entityID)
	}
	if schemaVersion <= 0 {
		schemaVersion = 1
	}
	_, err = m.q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: localKind, LocalID: localID, EntityID: id, EntityKind: entityKind,
		SchemaVersion: int32(schemaVersion), ProjectionVersion: projectionVersion,
	})
	return err
}

// StoreRichMetadata persists cast, crew, keywords, production companies, videos,
// certifications, recommendations, and collections for a media item. Called by
// the worker pipeline (EnrichMediaItemWorker) and the metadata editor. A non-nil
// error means at least one component failed to persist — the caller must not
// stamp the enrich component done (the fan-out is idempotent, so a retry heals).
func (m *Matcher) StoreRichMetadata(ctx context.Context, mediaItemID int64, detail *metadata.MediaDetail) error {
	return m.storeRichMetadata(ctx, mediaItemID, detail)
}
