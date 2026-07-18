package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

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
	if err := m.q.UpdateMediaItemSlug(ctx, sqlc.UpdateMediaItemSlugParams{ID: item.ID, Slug: itemSlug}); err != nil {
		// The media item already exists at this point. Keep it usable and let a
		// later maintenance pass repair the optional display slug.
		log.Warn().Err(err).Int64("media_item_id", item.ID).Msg("update media item slug failed")
	}

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
// The fan-out keeps going on individual failures where doing so is safe, but
// the joined summary stops the caller from stamping the enrich component done.
// Cast and crew are handled specially: their current projection is replaced
// atomically only after every referenced person has been resolved. Capped: a
// dead connection would otherwise collect thousands of identical lines.
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

type mediaPersonCreditProjection struct {
	PersonID     int64  `json:"person_id"`
	IsCast       bool   `json:"is_cast"`
	Character    string `json:"character"`
	DisplayOrder int    `json:"display_order"`
	Gender       int    `json:"gender"`
	Source       string `json:"source"`
	Job          string `json:"job"`
	Department   string `json:"department"`
}

type metadataEntityBindingProjection struct {
	LocalKind         string `json:"local_kind"`
	LocalID           int64  `json:"local_id"`
	EntityID          string `json:"entity_id"`
	EntityKind        string `json:"entity_kind"`
	SchemaVersion     int    `json:"schema_version"`
	ProjectionVersion int64  `json:"projection_version"`
}

type personIdentifierProbe struct {
	IdentityKey string `json:"identity_key"`
	Priority    int    `json:"priority"`
	Provider    string `json:"provider"`
	ProviderID  string `json:"provider_id"`
}

type personCreateProjection struct {
	IdentityKey string            `json:"identity_key"`
	ExternalIDs map[string]string `json:"external_ids"`
	Name        string            `json:"name"`
	Gender      int               `json:"gender"`
	ProfilePath string            `json:"profile_path"`
	Popularity  float64           `json:"popularity"`
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
	identity := richPersonIdentityKey(c)
	role := "crew|" + c.department + "|" + c.job
	if c.isCast {
		role = "cast|" + c.character
	}
	return identity + "|" + role + "|" + c.name
}

func richPersonIdentityKey(c richPersonCredit) string {
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
	return identity
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

func resolvedPersonIDs(resolved []resolvedPersonCredit) []int64 {
	ids := make([]int64, 0, len(resolved))
	seen := make(map[int64]struct{}, len(resolved))
	for _, item := range resolved {
		if item.person.ID == 0 {
			continue
		}
		if _, ok := seen[item.person.ID]; ok {
			continue
		}
		seen[item.person.ID] = struct{}{}
		ids = append(ids, item.person.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// replaceMediaPersonCredits treats one MediaDetail as the authoritative current
// cast and crew projection. Older provider-shaped person rows may legitimately
// remain in people, but their stale links to this title must not survive a
// canonical HeyaMetadata refresh. The narrow transaction prevents readers from
// observing an empty or partially rebuilt list when StoreRichMetadata is called
// through the pool-backed worker matcher. Callers already inside a transaction
// reuse it so the replacement stays part of the enclosing scanner apply.
func (m *Matcher) replaceMediaPersonCredits(ctx context.Context, mediaItemID int64, resolved []resolvedPersonCredit) error {
	replace := func(q *sqlc.Queries) error {
		personIDs := resolvedPersonIDs(resolved)
		if len(personIDs) > 0 {
			locked, err := q.LockPeopleForCreditWrite(ctx, personIDs)
			if err != nil {
				return fmt.Errorf("lock resolved people: %w", err)
			}
			if len(locked) != len(personIDs) {
				return fmt.Errorf("lock resolved people: %d of %d people still exist; retry metadata apply", len(locked), len(personIDs))
			}
		}

		credits := make([]mediaPersonCreditProjection, 0, len(resolved))
		for _, item := range resolved {
			credit, person := item.credit, item.person
			credits = append(credits, mediaPersonCreditProjection{
				PersonID: person.ID, IsCast: credit.isCast, Character: credit.character,
				DisplayOrder: credit.order, Gender: credit.gender, Source: credit.source,
				Job: credit.job, Department: credit.department,
			})
		}
		if _, err := q.ReplaceMediaPersonCredits(ctx, sqlc.ReplaceMediaPersonCreditsParams{
			TargetMediaItemID: mediaItemID,
			Credits:           mustJSON(credits),
		}); err != nil {
			return fmt.Errorf("write cast and crew projection: %w", err)
		}
		return nil
	}

	if m.inTx {
		return replace(m.q)
	}
	if m.db == nil {
		return errors.New("replace cast and crew: matcher has no database pool")
	}

	tx, err := m.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cast and crew replacement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := replace(m.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cast and crew replacement: %w", err)
	}
	return nil
}

func (m *Matcher) storeRichMetadata(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	started := time.Now()
	identityStarted := time.Now()
	var re richErrs
	credits := collectRichPersonCredits(d)
	canonicalIDs := make([]uuid.UUID, 0, len(credits))
	seenCanonicalIDs := make(map[uuid.UUID]struct{}, len(credits))
	for _, credit := range credits {
		if credit.canonicalID == "" {
			continue
		}
		canonicalID, err := uuid.Parse(credit.canonicalID)
		if err != nil {
			re.add(fmt.Errorf("person %q has invalid canonical ID %q: %w", credit.name, credit.canonicalID, err))
			continue
		}
		if _, exists := seenCanonicalIDs[canonicalID]; exists {
			continue
		}
		seenCanonicalIDs[canonicalID] = struct{}{}
		canonicalIDs = append(canonicalIDs, canonicalID)
	}
	peopleByCanonicalID := make(map[uuid.UUID]sqlc.Person, len(canonicalIDs))
	if len(canonicalIDs) > 0 {
		people, err := m.q.ListPeopleByCanonicalEntityIDs(ctx, canonicalIDs)
		if err != nil {
			return fmt.Errorf("resolve canonical people: %w", err)
		}
		for _, person := range people {
			peopleByCanonicalID[person.EntityID] = sqlc.Person{ID: person.ID, Name: person.Name}
		}
	}

	peopleByIdentity := make(map[string]sqlc.Person, len(credits))
	representatives := make(map[string]richPersonCredit, len(credits))
	identityKeys := make([]string, 0, len(credits))
	for _, credit := range credits {
		identityKey := richPersonIdentityKey(credit)
		if _, exists := representatives[identityKey]; !exists {
			representatives[identityKey] = credit
			identityKeys = append(identityKeys, identityKey)
		}
		if canonicalID, err := uuid.Parse(credit.canonicalID); err == nil {
			if person := peopleByCanonicalID[canonicalID]; person.ID != 0 {
				peopleByIdentity[identityKey] = person
			}
		}
	}
	sort.Strings(identityKeys)

	probes := make([]personIdentifierProbe, 0)
	for _, identityKey := range identityKeys {
		if peopleByIdentity[identityKey].ID != 0 {
			continue
		}
		for priority, provider := range sortedNonEmptyExternalIDKeys(representatives[identityKey].externalIDs) {
			probes = append(probes, personIdentifierProbe{
				IdentityKey: identityKey, Priority: priority, Provider: provider,
				ProviderID: representatives[identityKey].externalIDs[provider],
			})
		}
	}
	if len(probes) > 0 {
		people, err := m.q.ListPeopleByExternalIdentifierProbes(ctx, mustJSON(probes))
		if err != nil {
			return fmt.Errorf("resolve people by provider identifiers: %w", err)
		}
		for _, person := range people {
			peopleByIdentity[person.IdentityKey] = sqlc.Person{ID: person.ID, Name: person.Name}
		}
	}

	creates := make([]personCreateProjection, 0)
	for _, identityKey := range identityKeys {
		if peopleByIdentity[identityKey].ID != 0 {
			continue
		}
		credit := representatives[identityKey]
		creates = append(creates, personCreateProjection{
			IdentityKey: identityKey, ExternalIDs: credit.externalIDs,
			Name: credit.name, Gender: credit.gender, ProfilePath: credit.profilePath,
			Popularity: credit.popularity,
		})
	}
	if len(creates) > 0 {
		people, err := m.q.CreatePeopleBulk(ctx, mustJSON(creates))
		if err != nil {
			return fmt.Errorf("create canonical people: %w", err)
		}
		for _, person := range people {
			peopleByIdentity[person.IdentityKey] = sqlc.Person{ID: person.ID, Name: person.Name}
		}
	}

	resolved := make([]resolvedPersonCredit, 0, len(credits))
	for _, credit := range credits {
		if re.stopIfDone(ctx) {
			return re.result()
		}
		person := peopleByIdentity[richPersonIdentityKey(credit)]
		if person.ID == 0 {
			re.misses++
			continue
		}
		resolved = append(resolved, resolvedPersonCredit{credit: credit, person: person})
	}
	identityDuration := time.Since(identityStarted)

	// Resolve first, then acquire every shared person/profile/binding row in
	// ascending local person ID order. Local IDs are the actual PostgreSQL lock
	// keys, making this robust even when two payloads describe the same person
	// with different roles or identifier subsets.
	personProjectionStarted := time.Now()
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
		if _, err := uuid.Parse(item.credit.canonicalID); err == nil {
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

	bindings := make([]metadataEntityBindingProjection, 0, len(writes))
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
		if len(canonicalIDs) > 0 {
			// A local person has one canonical identity. Sorting above preserves
			// the legacy deterministic winner if malformed input supplies more.
			bindings = append(bindings, metadataEntityBindingProjection{
				LocalKind: "person", LocalID: write.person.ID,
				EntityID: canonicalIDs[len(canonicalIDs)-1], EntityKind: "person",
				SchemaVersion: 1,
			})
		}
	}
	if len(bindings) > 0 {
		if err := m.q.UpsertMetadataEntityBindings(ctx, mustJSON(bindings)); err != nil {
			if m.richFailure(&re, fmt.Errorf("bind canonical people: %w", err)) {
				return re.result()
			}
		}
	}
	personProjectionDuration := time.Since(personProjectionStarted)

	// Do not destroy the prior complete projection when even one current person
	// failed to resolve, profile, or bind. The caller will retry this component.
	if err := re.result(); err != nil {
		return err
	}
	creditStarted := time.Now()
	if err := m.replaceMediaPersonCredits(ctx, mediaItemID, resolved); err != nil {
		return fmt.Errorf("replace cast and crew: %w", err)
	}
	creditDuration := time.Since(creditStarted)
	extrasStarted := time.Now()

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
		Dur("identity_duration", identityDuration).
		Dur("person_projection_duration", personProjectionDuration).
		Dur("credit_duration", creditDuration).
		Dur("extras_duration", time.Since(extrasStarted)).
		Dur("duration", time.Since(started)).
		Msg("stored rich metadata")

	return re.result()
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
		if err := m.linkNetworks(ctx, series.ID, d.Networks); err != nil {
			return err
		}
	}
	if err := m.linkCreators(ctx, series.ID, d.CreatedBy); err != nil {
		return err
	}

	type seasonProjection struct {
		SeasonNumber  int               `json:"season_number"`
		Title         string            `json:"title"`
		Overview      string            `json:"overview"`
		PosterPath    string            `json:"poster_path"`
		AirDate       string            `json:"air_date"`
		EndDate       string            `json:"end_date"`
		Status        string            `json:"status"`
		AiredEpisodes int               `json:"aired_episodes"`
		ExternalIDs   map[string]string `json:"external_ids"`
		CanonicalID   string            `json:"canonical_id"`
	}
	type episodeProjection struct {
		SeasonNumber   int               `json:"season_number"`
		EpisodeNumber  int               `json:"episode_number"`
		Title          string            `json:"title"`
		Overview       string            `json:"overview"`
		StillPath      string            `json:"still_path"`
		RuntimeMinutes int               `json:"runtime_minutes"`
		AirDate        string            `json:"air_date"`
		Rating         float64           `json:"rating"`
		AbsoluteNumber int               `json:"absolute_number"`
		IsSpecial      bool              `json:"is_special"`
		EpisodeType    int               `json:"episode_type"`
		ExternalIDs    map[string]string `json:"external_ids"`
		Source         string            `json:"source"`
		CanonicalID    string            `json:"canonical_id"`
	}
	type episodeTitleProjection struct {
		SeasonNumber  int    `json:"season_number"`
		EpisodeNumber int    `json:"episode_number"`
		Title         string `json:"title"`
		Language      string `json:"language"`
		Source        string `json:"source"`
	}
	type episodeOverviewProjection struct {
		SeasonNumber  int    `json:"season_number"`
		EpisodeNumber int    `json:"episode_number"`
		Language      string `json:"language"`
		Overview      string `json:"overview"`
	}

	seasons := make([]seasonProjection, 0, len(d.Seasons))
	episodes := make([]episodeProjection, 0, d.NumberOfEpisodes)
	titles := make([]episodeTitleProjection, 0)
	overviews := make([]episodeOverviewProjection, 0)
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
		seasons = append(seasons, seasonProjection{
			SeasonNumber: sd.Number, Title: sd.Title, Overview: sd.Overview,
			PosterPath: sd.PosterURL, AirDate: sd.AirDate, EndDate: sd.EndDate,
			Status: sd.Status, AiredEpisodes: sd.AiredEpisodes,
			ExternalIDs: seasonExtIDs, CanonicalID: sd.CanonicalID,
		})

		for _, ep := range sd.Episodes {
			epExtIDs := map[string]string{}
			if ep.TmdbID != 0 {
				epExtIDs["tmdb"] = fmt.Sprintf("%d", ep.TmdbID)
			}
			if ep.TvdbID != 0 {
				epExtIDs["tvdb"] = fmt.Sprintf("%d", ep.TvdbID)
			}
			episodes = append(episodes, episodeProjection{
				SeasonNumber: sd.Number, EpisodeNumber: ep.Number, Title: ep.Title,
				Overview: ep.Overview, StillPath: ep.StillURL,
				RuntimeMinutes: ep.RuntimeMinutes, AirDate: ep.AirDate, Rating: ep.Rating,
				AbsoluteNumber: ep.AbsoluteNumber, IsSpecial: ep.IsSpecial,
				EpisodeType: ep.EpisodeType, ExternalIDs: epExtIDs, Source: ep.Source,
				CanonicalID: ep.CanonicalID,
			})
			for _, t := range ep.Titles {
				titles = append(titles, episodeTitleProjection{
					SeasonNumber: sd.Number, EpisodeNumber: ep.Number,
					Title: t.Title, Language: t.Language, Source: t.Source,
				})
			}
			for lang, text := range ep.Overviews {
				overviews = append(overviews, episodeOverviewProjection{
					SeasonNumber: sd.Number, EpisodeNumber: ep.Number,
					Language: lang, Overview: text,
				})
			}
		}
	}
	if _, err := m.q.PersistTVStructure(ctx, sqlc.PersistTVStructureParams{
		SeriesID: series.ID, SchemaVersion: int32(d.SchemaVersion),
		ProjectionVersion: d.ProjectionVersion, Seasons: mustJSON(seasons),
		Episodes: mustJSON(episodes), Titles: mustJSON(titles), Overviews: mustJSON(overviews),
	}); err != nil {
		return fmt.Errorf("persist TV structure: %w", err)
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

func (m *Matcher) linkNetworks(ctx context.Context, seriesID int64, nets []metadata.NetworkDetail) error {
	if err := m.q.DeleteNetworksForSeries(ctx, seriesID); err != nil {
		return fmt.Errorf("deleting networks for series %d: %w", seriesID, err)
	}
	for i, n := range nets {
		if n.Name == "" {
			continue
		}
		net, err := m.upsertNetwork(ctx, n)
		if err != nil || net.ID == 0 {
			continue
		}
		if err := m.q.AttachNetworkToSeries(ctx, sqlc.AttachNetworkToSeriesParams{
			SeriesID:  seriesID,
			NetworkID: net.ID,
			SortOrder: int32(i),
		}); err != nil {
			return fmt.Errorf("attaching network %d to series %d: %w", net.ID, seriesID, err)
		}
	}
	return nil
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

func (m *Matcher) linkCreators(ctx context.Context, seriesID int64, creators []metadata.CreatorDetail) error {
	if err := m.q.DeleteCreatorsForSeries(ctx, seriesID); err != nil {
		return fmt.Errorf("deleting creators for series %d: %w", seriesID, err)
	}
	for i, c := range creators {
		if c.Name == "" {
			continue
		}
		cr, err := m.upsertCreator(ctx, c)
		if err != nil || cr.ID == 0 {
			continue
		}
		if err := m.q.AttachCreatorToSeries(ctx, sqlc.AttachCreatorToSeriesParams{
			SeriesID:  seriesID,
			CreatorID: cr.ID,
			SortOrder: int32(i),
		}); err != nil {
			return fmt.Errorf("attaching creator %d to series %d: %w", cr.ID, seriesID, err)
		}
	}
	return nil
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
// stamp the enrich component done. Cast and crew are replaced atomically; the
// remaining fan-out is idempotent, so a retry heals a partial extras write.
func (m *Matcher) StoreRichMetadata(ctx context.Context, mediaItemID int64, detail *metadata.MediaDetail) error {
	return m.storeRichMetadata(ctx, mediaItemID, detail)
}
