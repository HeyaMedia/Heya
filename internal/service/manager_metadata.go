package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The manager metadata surface: EVERYTHING known about an item, rendered as
// generic labeled fields so the UI stays one component and new fields are a
// backend-only change. Field keys line up with the metadata editor's
// field_provenance stamps, so manually-edited fields wear their provenance.

type ManagerMetadataField struct {
	Key        string   `json:"key"`
	Label      string   `json:"label"`
	Value      string   `json:"value,omitempty"`
	Values     []string `json:"values,omitempty" doc:"Chip lists (genres, countries, aliases)"`
	Provenance string   `json:"provenance,omitempty" doc:"'user' when manually edited (locked against enrichment)"`
	Mono       bool     `json:"mono,omitempty"`
	Long       bool     `json:"long,omitempty" doc:"Full-width prose (overview, biography)"`
	Href       string   `json:"href,omitempty"`
	Tone       string   `json:"tone,omitempty" doc:"'bad' flags a problem value (enrich error)"`
}

type ManagerMetadataSection struct {
	Title  string                 `json:"title"`
	Fields []ManagerMetadataField `json:"fields"`
}

type ManagerAltTitleView struct {
	Title    string `json:"title"`
	Language string `json:"language,omitempty"`
	Country  string `json:"country,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Source   string `json:"source,omitempty"`
}

type ManagerMetadataView struct {
	Sections  []ManagerMetadataSection `json:"sections"`
	AltTitles []ManagerAltTitleView    `json:"alt_titles,omitempty"`
}

// externalIDLinks mirrors the FE's provider link map so external ids are
// clickable wherever the metadata surface renders.
func externalIDLink(provider, id, mediaType string) string {
	switch provider {
	case "imdb":
		return "https://www.imdb.com/title/" + id + "/"
	case "tmdb":
		kind := "tv"
		if mediaType == "movie" {
			kind = "movie"
		}
		return "https://www.themoviedb.org/" + kind + "/" + id
	case "tvdb":
		return "https://thetvdb.com/dereferrer/series/" + id
	case "musicbrainz":
		return "https://musicbrainz.org/artist/" + id
	case "anidb":
		return "https://anidb.net/anime/" + id
	case "anilist":
		return "https://anilist.co/anime/" + id
	case "openlibrary":
		return "https://openlibrary.org/works/" + id
	}
	return ""
}

// ManagerMetadata assembles the full metadata dump for one media item.
func (a *App) ManagerMetadata(ctx context.Context, id int64) (*ManagerMetadataView, error) {
	var (
		mediaType, slug, heyaSlug, providerKind, enrichmentStatus string
		slugLocked                                                bool
		createdAt, updatedAt                                      time.Time
		matchedAt, baseAt, peopleAt, extrasAt, imagesAt           pgtype.Timestamptz
		structureAt, lastAttemptAt                                pgtype.Timestamptz
		lastEnrichError, addedSource                              string
		provenanceRaw, externalRaw                                []byte
		matchConfidence                                           float32
		title, sortTitle, year, description, tagline              string
		originalTitle, originalLanguage, status, homepage         string
	)
	err := a.db.QueryRow(ctx, `
		SELECT mi.media_type::text, mi.slug, mi.slug_locked, mi.heya_slug, mi.provider_kind,
		       mi.created_at, mi.updated_at, mi.matched_at, mi.enrichment_status,
		       mi.base_enriched_at, mi.people_enriched_at, mi.extras_enriched_at,
		       mi.images_enriched_at, mi.structure_enriched_at, mi.last_enrich_attempt_at,
		       mi.last_enrich_error, mi.field_provenance, mi.match_confidence,
		       COALESCE(mi.added_source, ''),
		       p.title, p.sort_title, p.year, p.description, p.tagline,
		       p.original_title, p.original_language, p.status, p.homepage,
		       (SELECT COALESCE(jsonb_object_agg(ei.provider, ei.external_id), '{}'::jsonb)
		          FROM media_item_external_ids ei WHERE ei.media_item_id = mi.id)
		FROM media_items mi
		JOIN media_item_profiles p ON p.media_item_id = mi.id
		WHERE mi.id = $1`, id).Scan(
		&mediaType, &slug, &slugLocked, &heyaSlug, &providerKind,
		&createdAt, &updatedAt, &matchedAt, &enrichmentStatus,
		&baseAt, &peopleAt, &extrasAt, &imagesAt, &structureAt, &lastAttemptAt,
		&lastEnrichError, &provenanceRaw, &matchConfidence, &addedSource,
		&title, &sortTitle, &year, &description, &tagline,
		&originalTitle, &originalLanguage, &status, &homepage,
		&externalRaw,
	)
	if err != nil {
		return nil, fmt.Errorf("manager metadata %d: %w", id, err)
	}

	prov := map[string]string{}
	_ = json.Unmarshal(provenanceRaw, &prov)
	externalIDs := map[string]string{}
	_ = json.Unmarshal(externalRaw, &externalIDs)

	field := func(key, label, value string) ManagerMetadataField {
		return ManagerMetadataField{Key: key, Label: label, Value: value, Provenance: prov[key]}
	}

	view := &ManagerMetadataView{}

	identity := ManagerMetadataSection{Title: "Identity"}
	identity.Fields = append(identity.Fields,
		field("title", "Title", title),
		field("sort_title", "Sort title", sortTitle),
		field("original_title", "Original title", originalTitle),
		field("year", "Year", year),
		field("status", "Status", status),
		field("original_language", "Original language", originalLanguage),
	)
	slugField := field("slug", "Slug", slug)
	slugField.Mono = true
	if slugLocked {
		slugField.Provenance = "locked"
	}
	identity.Fields = append(identity.Fields, slugField)
	if homepage != "" {
		home := field("homepage", "Homepage", homepage)
		home.Href = homepage
		home.Mono = true
		identity.Fields = append(identity.Fields, home)
	}
	view.Sections = append(view.Sections, identity)

	story := ManagerMetadataSection{Title: "Story"}
	overview := field("description", "Overview", description)
	overview.Long = true
	tag := field("tagline", "Tagline", tagline)
	tag.Long = true
	story.Fields = append(story.Fields, overview, tag)

	var details *ManagerMetadataSection
	switch mediaType {
	case "movie":
		details, err = a.movieMetadataSection(ctx, id, field)
	case "tv", "anime":
		details, err = a.tvMetadataSection(ctx, id, field)
	case "music":
		details, err = a.artistMetadataSection(ctx, id, field, &story)
	case "book":
		details, err = a.bookMetadataSection(ctx, id, field)
	}
	if err != nil {
		return nil, err
	}
	// Details pairs with Identity on the first grid row; the full-width
	// Story block follows.
	if details != nil && len(details.Fields) > 0 {
		view.Sections = append(view.Sections, *details)
	}
	view.Sections = append(view.Sections, story)

	// External ids — stable order, clickable where the provider is known.
	if len(externalIDs) > 0 || heyaSlug != "" {
		ids := ManagerMetadataSection{Title: "External IDs"}
		providers := make([]string, 0, len(externalIDs))
		for p := range externalIDs {
			providers = append(providers, p)
		}
		sort.Strings(providers)
		for _, p := range providers {
			f := ManagerMetadataField{
				Key: "external_ids", Label: p, Value: externalIDs[p],
				Provenance: prov["external_ids"], Mono: true,
				Href: externalIDLink(p, externalIDs[p], mediaType),
			}
			ids.Fields = append(ids.Fields, f)
		}
		if heyaSlug != "" {
			ids.Fields = append(ids.Fields, ManagerMetadataField{
				Key: "heya_slug", Label: "heya.media", Value: heyaSlug, Mono: true,
			})
		}
		view.Sections = append(view.Sections, ids)
	}

	// Enrichment + record keeping — when each pass last ran, and how the
	// item entered the library.
	fmtTS := func(ts pgtype.Timestamptz) string {
		if !ts.Valid {
			return ""
		}
		return ts.Time.UTC().Format(time.RFC3339)
	}
	enrich := ManagerMetadataSection{Title: "Enrichment"}
	enrich.Fields = append(enrich.Fields,
		ManagerMetadataField{Key: "enrichment_status", Label: "Status", Value: enrichmentStatus},
	)
	if matchedAt.Valid {
		enrich.Fields = append(enrich.Fields, ManagerMetadataField{
			Key: "matched_at", Label: "Matched",
			Value: fmt.Sprintf("%s · %.0f%% confidence", matchedAt.Time.UTC().Format(time.RFC3339), matchConfidence*100),
			Mono:  true,
		})
	}
	for _, pass := range []struct {
		key, label string
		ts         pgtype.Timestamptz
	}{
		{"base_enriched_at", "Base pass", baseAt},
		{"structure_enriched_at", "Structure pass", structureAt},
		{"people_enriched_at", "People pass", peopleAt},
		{"images_enriched_at", "Images pass", imagesAt},
		{"extras_enriched_at", "Extras pass", extrasAt},
		{"last_enrich_attempt_at", "Last attempt", lastAttemptAt},
	} {
		enrich.Fields = append(enrich.Fields, ManagerMetadataField{
			Key: pass.key, Label: pass.label, Value: fmtTS(pass.ts), Mono: true,
		})
	}
	if lastEnrichError != "" {
		enrich.Fields = append(enrich.Fields, ManagerMetadataField{
			Key: "last_enrich_error", Label: "Last error", Value: lastEnrichError, Tone: "bad", Long: true,
		})
	}
	view.Sections = append(view.Sections, enrich)

	record := ManagerMetadataSection{Title: "Record"}
	added := createdAt.UTC().Format(time.RFC3339)
	if addedSource != "" {
		added += " · via " + addedSource
	}
	record.Fields = append(record.Fields,
		ManagerMetadataField{Key: "created_at", Label: "Added", Value: added, Mono: true},
		ManagerMetadataField{Key: "updated_at", Label: "Updated", Value: updatedAt.UTC().Format(time.RFC3339), Mono: true},
		ManagerMetadataField{Key: "id", Label: "Item id", Value: strconv.FormatInt(id, 10), Mono: true},
	)
	if providerKind != "" {
		record.Fields = append(record.Fields, ManagerMetadataField{Key: "provider_kind", Label: "Provider kind", Value: providerKind, Mono: true})
	}
	view.Sections = append(view.Sections, record)

	// Alternate / localized titles.
	titleRows, err := a.db.Query(ctx, `
		SELECT title, language, country, title_type, source
		FROM media_titles WHERE media_item_id = $1
		ORDER BY language, title`, id)
	if err != nil {
		return nil, fmt.Errorf("manager metadata titles: %w", err)
	}
	defer titleRows.Close()
	for titleRows.Next() {
		var t ManagerAltTitleView
		if err := titleRows.Scan(&t.Title, &t.Language, &t.Country, &t.Kind, &t.Source); err != nil {
			return nil, err
		}
		view.AltTitles = append(view.AltTitles, t)
	}
	return view, titleRows.Err()
}

func commaInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func (a *App) movieMetadataSection(ctx context.Context, id int64, field func(key, label, value string) ManagerMetadataField) (*ManagerMetadataSection, error) {
	var (
		runtime                int32
		genres, spoken, origin []string
		rating, popularity     float64
		releaseDate            pgtype.Date
		budget, revenue        int64
	)
	err := a.db.QueryRow(ctx, `
		SELECT runtime_minutes, genres, COALESCE(rating, 0), release_date,
		       COALESCE(budget, 0), COALESCE(revenue, 0), COALESCE(popularity, 0),
		       COALESCE(spoken_languages, '{}'), COALESCE(origin_country, '{}')
		FROM movies WHERE media_item_id = $1`, id).Scan(
		&runtime, &genres, &rating, &releaseDate, &budget, &revenue, &popularity, &spoken, &origin)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // metadata row not written yet (fresh manager add)
		}
		return nil, fmt.Errorf("movie metadata %d: %w", id, err)
	}
	s := &ManagerMetadataSection{Title: "Movie"}
	s.Fields = append(s.Fields, field("release_date", "Release date", dateToString(releaseDate)))
	if runtime > 0 {
		s.Fields = append(s.Fields, field("runtime_minutes", "Runtime", fmt.Sprintf("%d min", runtime)))
	} else {
		s.Fields = append(s.Fields, field("runtime_minutes", "Runtime", ""))
	}
	genresField := field("genres", "Genres", "")
	genresField.Values = genres
	s.Fields = append(s.Fields, genresField)
	if rating > 0 {
		s.Fields = append(s.Fields, field("rating", "Rating", fmt.Sprintf("%.1f", rating)))
	}
	if popularity > 0 {
		s.Fields = append(s.Fields, field("popularity", "Popularity", fmt.Sprintf("%.1f", popularity)))
	}
	if budget > 0 {
		s.Fields = append(s.Fields, field("budget", "Budget", "$"+commaInt(budget)))
	}
	if revenue > 0 {
		s.Fields = append(s.Fields, field("revenue", "Revenue", "$"+commaInt(revenue)))
	}
	spokenField := field("spoken_languages", "Spoken languages", "")
	spokenField.Values = spoken
	originField := field("origin_country", "Origin country", "")
	originField.Values = origin
	s.Fields = append(s.Fields, spokenField, originField)
	return s, nil
}

func (a *App) tvMetadataSection(ctx context.Context, id int64, field func(key, label, value string) ManagerMetadataField) (*ManagerMetadataSection, error) {
	var (
		seriesID               int64
		originalName           string
		genres, spoken, origin []string
		rating, popularity     float64
		firstAir, lastAir      pgtype.Date
		seasons, episodes      int32
	)
	err := a.db.QueryRow(ctx, `
		SELECT id, original_name, genres, COALESCE(rating, 0), first_air_date, last_air_date,
		       number_of_seasons, number_of_episodes, COALESCE(popularity, 0),
		       COALESCE(spoken_languages, '{}'), COALESCE(origin_country, '{}')
		FROM tv_series WHERE media_item_id = $1`, id).Scan(
		&seriesID, &originalName, &genres, &rating, &firstAir, &lastAir,
		&seasons, &episodes, &popularity, &spoken, &origin)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("series metadata %d: %w", id, err)
	}
	s := &ManagerMetadataSection{Title: "Series"}
	s.Fields = append(s.Fields,
		field("original_name", "Original name", originalName),
		field("first_air_date", "First aired", dateToString(firstAir)),
		field("last_air_date", "Last aired", dateToString(lastAir)),
		field("number_of_seasons", "Seasons", strconv.Itoa(int(seasons))),
		field("number_of_episodes", "Episodes", strconv.Itoa(int(episodes))),
	)
	genresField := field("genres", "Genres", "")
	genresField.Values = genres
	s.Fields = append(s.Fields, genresField)
	if rating > 0 {
		s.Fields = append(s.Fields, field("rating", "Rating", fmt.Sprintf("%.1f", rating)))
	}
	if popularity > 0 {
		s.Fields = append(s.Fields, field("popularity", "Popularity", fmt.Sprintf("%.1f", popularity)))
	}

	networkRows, err := a.db.Query(ctx, `
		SELECT n.name FROM tv_series_networks sn
		JOIN networks n ON n.id = sn.network_id
		WHERE sn.series_id = $1 ORDER BY sn.sort_order`, seriesID)
	if err == nil {
		var networks []string
		for networkRows.Next() {
			var name string
			if networkRows.Scan(&name) == nil {
				networks = append(networks, name)
			}
		}
		networkRows.Close()
		networksField := field("networks", "Networks", "")
		networksField.Values = networks
		s.Fields = append(s.Fields, networksField)
	}
	spokenField := field("spoken_languages", "Spoken languages", "")
	spokenField.Values = spoken
	originField := field("origin_country", "Origin country", "")
	originField.Values = origin
	s.Fields = append(s.Fields, spokenField, originField)
	return s, nil
}

func (a *App) artistMetadataSection(ctx context.Context, id int64, field func(key, label, value string) ManagerMetadataField, story *ManagerMetadataSection) (*ManagerMetadataSection, error) {
	var (
		name, sortName, disambiguation, biography, artistType string
		beginDate, endDate, birthplace, deathday, mbid        string
		aliases, tags, sources                                []string
		listeners, playcount                                  int64
		popularity                                            int32
		ended                                                 bool
	)
	err := a.db.QueryRow(ctx, `
		SELECT name, sort_name, disambiguation, biography, artist_type,
		       begin_date, end_date, ended, birthplace, deathday, musicbrainz_id,
		       aliases, tags, COALESCE(metadata_sources, '{}'),
		       listeners, playcount, popularity
		FROM artists WHERE media_item_id = $1`, id).Scan(
		&name, &sortName, &disambiguation, &biography, &artistType,
		&beginDate, &endDate, &ended, &birthplace, &deathday, &mbid,
		&aliases, &tags, &sources, &listeners, &playcount, &popularity)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("artist metadata %d: %w", id, err)
	}
	if biography != "" {
		bio := field("biography", "Biography", biography)
		bio.Long = true
		story.Fields = append(story.Fields, bio)
	}
	s := &ManagerMetadataSection{Title: "Artist"}
	s.Fields = append(s.Fields,
		field("name", "Name", name),
		field("sort_name", "Sort name", sortName),
		field("disambiguation", "Disambiguation", disambiguation),
		field("artist_type", "Type", artistType),
		field("begin_date", "Began", beginDate),
	)
	if ended || endDate != "" {
		s.Fields = append(s.Fields, field("end_date", "Ended", endDate))
	}
	s.Fields = append(s.Fields,
		field("birthplace", "Birthplace", birthplace),
	)
	if deathday != "" {
		s.Fields = append(s.Fields, field("deathday", "Died", deathday))
	}
	if mbid != "" {
		f := field("musicbrainz_id", "MusicBrainz id", mbid)
		f.Mono = true
		f.Href = "https://musicbrainz.org/artist/" + mbid
		s.Fields = append(s.Fields, f)
	}
	aliasField := field("aliases", "Aliases", "")
	aliasField.Values = aliases
	tagField := field("tags", "Tags", "")
	tagField.Values = tags
	sourceField := field("metadata_sources", "Metadata sources", "")
	sourceField.Values = sources
	s.Fields = append(s.Fields, aliasField, tagField, sourceField)
	if listeners > 0 {
		s.Fields = append(s.Fields, field("listeners", "Listeners", commaInt(listeners)))
	}
	if playcount > 0 {
		s.Fields = append(s.Fields, field("playcount", "Play count", commaInt(playcount)))
	}
	if popularity > 0 {
		s.Fields = append(s.Fields, field("popularity", "Popularity", strconv.Itoa(int(popularity))))
	}
	return s, nil
}

func (a *App) bookMetadataSection(ctx context.Context, id int64, field func(key, label, value string) ManagerMetadataField) (*ManagerMetadataSection, error) {
	var (
		isbn, publisher, language, seriesName, format, author string
		publishDate                                           pgtype.Date
		pageCount, seriesNumber                               int32
		subjects                                              []string
	)
	err := a.db.QueryRow(ctx, `
		SELECT b.isbn, b.publisher, b.publish_date, b.page_count,
		       COALESCE(b.subjects, '{}'), b.language, b.series_name,
		       COALESCE(b.series_number, 0), b.format, COALESCE(au.name, '')
		FROM books b
		LEFT JOIN authors au ON au.id = b.author_id
		WHERE b.media_item_id = $1`, id).Scan(
		&isbn, &publisher, &publishDate, &pageCount, &subjects,
		&language, &seriesName, &seriesNumber, &format, &author)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("book metadata %d: %w", id, err)
	}
	s := &ManagerMetadataSection{Title: "Book"}
	s.Fields = append(s.Fields,
		field("author", "Author", author),
		field("isbn", "ISBN", isbn),
		field("publisher", "Publisher", publisher),
		field("publish_date", "Published", dateToString(publishDate)),
	)
	if pageCount > 0 {
		s.Fields = append(s.Fields, field("page_count", "Pages", strconv.Itoa(int(pageCount))))
	}
	s.Fields = append(s.Fields,
		field("language", "Language", language),
		field("series_name", "Series", seriesName),
	)
	if seriesNumber > 0 {
		s.Fields = append(s.Fields, field("series_number", "Series #", strconv.Itoa(int(seriesNumber))))
	}
	s.Fields = append(s.Fields, field("format", "Format", format))
	subjectField := field("subjects", "Subjects", "")
	subjectField.Values = subjects
	s.Fields = append(s.Fields, subjectField)
	return s, nil
}
