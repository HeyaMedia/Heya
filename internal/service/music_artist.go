package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/karbowiak/heya/internal/titlematch"
)

// ArtistURL is one chip in the "External Links" grid. heya.media exposes
// ArtistURL as {type, url} — we mirror that shape so the FE can render
// without re-mapping.
type ArtistURL struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// ArtistMember is one entry in the band-membership graph. heya.media's
// member rows carry begin/end dates when the person joined/left, which
// is enough for the FE to print "1990-1995" next to the chip. LocalSlug
// is filled by matchArtistMembersLocal when the member is themselves a
// library artist — the FE then links the chip and shows their portrait.
type ArtistMember struct {
	Name      string `json:"name"`
	MBID      string `json:"mbid,omitempty"`
	BeginYear int    `json:"begin_year,omitempty"`
	EndYear   int    `json:"end_year,omitempty"`
	LocalSlug string `json:"local_slug,omitempty"`
}

// artistRelationBlob is the shape actually stored in artists.members /
// artists.groups jsonb — it's metadata.ArtistRelationEntry marshaled
// verbatim at apply time. Reading it as ArtistMember silently dropped the
// tenure dates (begin/end vs begin_year/end_year key mismatch), which is
// why member year ranges never rendered.
type artistRelationBlob struct {
	Name  string `json:"name"`
	MBID  string `json:"mbid"`
	Begin string `json:"begin"`
	End   string `json:"end"`
}

func parseArtistMembers(raw []byte) []ArtistMember {
	var blobs []artistRelationBlob
	if err := json.Unmarshal(raw, &blobs); err != nil {
		return nil
	}
	out := make([]ArtistMember, 0, len(blobs))
	for _, b := range blobs {
		out = append(out, ArtistMember{
			Name:      b.Name,
			MBID:      b.MBID,
			BeginYear: yearOf(b.Begin),
			EndYear:   yearOf(b.End),
		})
	}
	return out
}

// yearOf extracts the year from a MusicBrainz partial date ("1993",
// "1993-01-03", ...). Zero when absent/garbled.
func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	var y int
	if _, err := fmt.Sscanf(date[:4], "%d", &y); err != nil {
		return 0
	}
	return y
}

// ArtistView is the JSON-clean envelope that ships to the FE in
// /api/media/{id}.artist. It mirrors sqlc.Artist but:
//
//   - drops search_vector (huge tsvector that has no FE use)
//   - parses the jsonb byte slices (urls/wikipedia/profiles/groups/members)
//     into typed Go shapes so they marshal as objects, not base64
//
// Fields are tagged with omitempty where empty values would just be noise.
type ArtistView struct {
	ID                    int64             `json:"id"`
	MediaItemID           int64             `json:"media_item_id"`
	MusicbrainzID         string            `json:"musicbrainz_id,omitempty"`
	Name                  string            `json:"name"`
	SortName              string            `json:"sort_name,omitempty"`
	Disambiguation        string            `json:"disambiguation,omitempty"`
	Biography             string            `json:"biography,omitempty"`
	Annotation            string            `json:"annotation,omitempty"`
	ArtistType            string            `json:"artist_type,omitempty"`
	BeginDate             string            `json:"begin_date,omitempty"`
	BeginYear             int32             `json:"begin_year,omitempty"`
	EndDate               string            `json:"end_date,omitempty"`
	Ended                 bool              `json:"ended,omitempty"`
	Deathday              string            `json:"deathday,omitempty"`
	Birthplace            string            `json:"birthplace,omitempty"`
	Listeners             int64             `json:"listeners,omitempty"`
	Playcount             int64             `json:"playcount,omitempty"`
	Popularity            int32             `json:"popularity,omitempty"`
	Genres                []string          `json:"genres,omitempty"`
	Tags                  []string          `json:"tags,omitempty"`
	Aliases               []string          `json:"aliases,omitempty"`
	URLs                  []ArtistURL       `json:"urls,omitempty"`
	WikipediaLinks        map[string]string `json:"wikipedia_links,omitempty"`
	Profiles              map[string]string `json:"profiles,omitempty"`
	Groups                []ArtistMember    `json:"groups,omitempty"`
	Members               []ArtistMember    `json:"members,omitempty"`
	DiscographyEnrichedAt *string           `json:"discography_enriched_at,omitempty"`
	CoverArtEnrichedAt    *string           `json:"cover_art_enriched_at,omitempty"`
}

// BuildArtistView converts a sqlc.Artist row into the FE-shaped envelope.
// JSONB parse failures fall back to nil rather than erroring — a broken
// blob shouldn't take down the artist page; the FE simply hides empty
// sections.
func BuildArtistView(a sqlc.Artist) ArtistView {
	v := ArtistView{
		ID:             a.ID,
		MediaItemID:    a.MediaItemID,
		MusicbrainzID:  a.MusicbrainzID,
		Name:           a.Name,
		SortName:       a.SortName,
		Disambiguation: a.Disambiguation,
		Biography:      a.Biography,
		Annotation:     a.Annotation,
		ArtistType:     a.ArtistType,
		BeginDate:      a.BeginDate,
		BeginYear:      a.BeginYear,
		EndDate:        a.EndDate,
		Ended:          a.Ended,
		Deathday:       a.Deathday,
		Birthplace:     a.Birthplace,
		Listeners:      a.Listeners,
		Playcount:      a.Playcount,
		Popularity:     a.Popularity,
		Genres:         nonNilStrings(a.Genres),
		Tags:           nonNilStrings(a.Tags),
		Aliases:        nonNilStrings(a.Aliases),
	}
	if len(a.Urls) > 0 {
		var urls []ArtistURL
		if err := json.Unmarshal(a.Urls, &urls); err == nil {
			v.URLs = urls
		}
	}
	if len(a.WikipediaLinks) > 0 {
		var wiki map[string]string
		if err := json.Unmarshal(a.WikipediaLinks, &wiki); err == nil {
			v.WikipediaLinks = wiki
		}
	}
	if len(a.Profiles) > 0 {
		var profiles map[string]string
		if err := json.Unmarshal(a.Profiles, &profiles); err == nil {
			v.Profiles = profiles
		}
	}
	if len(a.Groups) > 0 {
		v.Groups = parseArtistMembers(a.Groups)
	}
	if len(a.Members) > 0 {
		v.Members = parseArtistMembers(a.Members)
	}
	if a.DiscographyEnrichedAt.Valid {
		ts := a.DiscographyEnrichedAt.Time.Format("2006-01-02T15:04:05Z07:00")
		v.DiscographyEnrichedAt = &ts
	}
	if a.CoverArtEnrichedAt.Valid {
		ts := a.CoverArtEnrichedAt.Time.Format("2006-01-02T15:04:05Z07:00")
		v.CoverArtEnrichedAt = &ts
	}
	return v
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return nil
	}
	return s
}

// localArtistRef points an external artist mention (similar-artist hit,
// band member) at the library artist it matches.
type localArtistRef struct {
	artistID int64
	slug     string
}

// localArtistIndex loads the whole music-artist catalog into MBID + name
// lookup maps. The pool is small (hundreds at most) so one query + two
// maps beats N per-mention queries. Errors degrade to empty maps — a
// failed index just means no local linking, not a failed page.
func (a *App) localArtistIndex(ctx context.Context) (byMBID, byName map[string]localArtistRef) {
	byMBID = map[string]localArtistRef{}
	byName = map[string]localArtistRef{}
	rows, err := a.db.Query(ctx, `
		SELECT a.id, a.name, a.musicbrainz_id, mi.slug
		FROM artists a
		JOIN media_item_cards mi ON mi.id = a.media_item_id
		JOIN libraries   l  ON l.id  = mi.library_id
		WHERE l.media_type = 'music'
	`)
	if err != nil {
		return byMBID, byName
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, mbid, slug string
		if err := rows.Scan(&id, &name, &mbid, &slug); err != nil {
			continue
		}
		ref := localArtistRef{artistID: id, slug: slug}
		if mbid != "" {
			byMBID[mbid] = ref
		}
		byName[strings.ToLower(strings.TrimSpace(name))] = ref
	}
	return byMBID, byName
}

// matchArtistMembersLocal fills Members/Groups LocalSlug where the person
// (or band) is themselves a library artist — MBID first, case-fold name
// as fallback, same policy as GetSimilarArtists.
func (a *App) matchArtistMembersLocal(ctx context.Context, v *ArtistView) {
	if len(v.Members) == 0 && len(v.Groups) == 0 {
		return
	}
	byMBID, byName := a.localArtistIndex(ctx)
	if len(byMBID) == 0 && len(byName) == 0 {
		return
	}
	link := func(list []ArtistMember) {
		for i := range list {
			m := &list[i]
			if m.MBID != "" {
				if ref, ok := byMBID[m.MBID]; ok {
					m.LocalSlug = ref.slug
					continue
				}
			}
			if ref, ok := byName[strings.ToLower(strings.TrimSpace(m.Name))]; ok {
				m.LocalSlug = ref.slug
			}
		}
	}
	link(v.Members)
	link(v.Groups)
}

// ArtistTopTrackRow is one row of the artist's "Popular" rail. When
// LocalTrackID is non-zero the FE renders a play button that streams the
// owned recording; otherwise the chip falls back to a Last.fm link.
type ArtistTopTrackRow struct {
	Rank             int32  `json:"rank"`
	Provider         string `json:"provider,omitempty"`
	Title            string `json:"title"`
	MBID             string `json:"mbid,omitempty"`
	Playcount        int64  `json:"playcount"`
	Listeners        int64  `json:"listeners"`
	URL              string `json:"url,omitempty"`
	LocalTrackID     int64  `json:"local_track_id,omitempty"`
	LocalAlbumID     int64  `json:"local_album_id,omitempty"`
	LocalAlbumTitle  string `json:"local_album_title,omitempty"`
	LocalAlbumSlug   string `json:"local_album_slug,omitempty"`
	LocalAlbumYear   string `json:"local_album_year,omitempty"`
	LocalDurationSec int32  `json:"local_duration,omitempty"`
	LocalCoverPath   string `json:"local_cover_path,omitempty"`
}

// ListArtistTopTracksBySlug returns the Last.fm-derived top-tracks rail for
// an artist, joined to local tracks where we own them. The join lives in Go
// (not SQL) so the title matcher can use the kagome romanizer — Last.fm
// often returns "Usseewa" while our tags carry "うっせぇわ" or vice versa,
// and SQL can't bridge that gap on its own.
//
// Match order, best to fallback:
//  1. recording_mbid equality (when both sides have one)
//  2. case-fold exact equality on the raw title
//  3. case-fold equality after stripping (...) / [...] parentheticals
//  4. case-fold equality after romanizing both sides (collapses kana/kanji)
//  5. case-fold substring containment (either direction, romanized) — only
//     when the shorter side is ≥ 3 chars so "Show" doesn't catch "Show me"
func (a *App) ListArtistTopTracksBySlug(ctx context.Context, artistSlug string, limit int32) ([]ArtistTopTrackRow, error) {
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	q := sqlc.New(a.db)
	artist, err := q.GetMusicArtistBySlug(ctx, artistSlug)
	if err != nil {
		return nil, fmt.Errorf("artist not found: %w", err)
	}
	tops, err := q.ListArtistTopTracksRawByArtistID(ctx, sqlc.ListArtistTopTracksRawByArtistIDParams{
		ArtistID:   artist.ID,
		TrackLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list top tracks: %w", err)
	}
	locals, err := q.ListTracksForArtistMatching(ctx, artist.ID)
	if err != nil {
		return nil, fmt.Errorf("list local tracks: %w", err)
	}

	idx := buildTrackMatchIndex(locals)

	out := make([]ArtistTopTrackRow, 0, len(tops))
	for _, t := range tops {
		row := ArtistTopTrackRow{
			Rank:      t.Rank,
			Provider:  t.Provider,
			Title:     t.Title,
			MBID:      t.Mbid,
			Playcount: t.Playcount,
			Listeners: t.Listeners,
			URL:       t.Url,
		}
		if m, ok := idx.findMatch(t.Title, t.Mbid); ok {
			row.LocalTrackID = m.TrackID
			row.LocalAlbumID = m.AlbumID
			row.LocalAlbumTitle = m.AlbumTitle
			row.LocalAlbumSlug = m.AlbumSlug
			row.LocalAlbumYear = m.AlbumYear
			row.LocalDurationSec = m.EffectiveDuration
			row.LocalCoverPath = m.CoverPath
		}
		out = append(out, row)
	}
	return out, nil
}

// trackMatchIndex is a per-artist in-memory index that bridges Last.fm
// titles to our local tracks. It pre-normalizes each local title four
// ways (raw, parens-stripped, romanized, parens-stripped+romanized) so
// the per-row lookups are constant-time map hits rather than O(N) scans.
type trackMatchIndex struct {
	rows []sqlc.ListTracksForArtistMatchingRow
	// byMBID: recording_mbid → row index, when populated
	byMBID map[string]int
	// byKey: normalized title (lowercase + romaji + parens stripped)
	//        → row index. Multiple normalizations of one local row all
	//        point back to the same row.
	byKey map[string]int
}

func buildTrackMatchIndex(rows []sqlc.ListTracksForArtistMatchingRow) *trackMatchIndex {
	idx := &trackMatchIndex{
		rows:   rows,
		byMBID: make(map[string]int, len(rows)),
		byKey:  make(map[string]int, len(rows)*4),
	}
	for i, r := range rows {
		if r.RecordingMbid != "" {
			idx.byMBID[r.RecordingMbid] = i
		}
		for _, key := range titlematch.Normalizations(r.Title) {
			if _, exists := idx.byKey[key]; !exists {
				idx.byKey[key] = i
			}
		}
	}
	return idx
}

func (idx *trackMatchIndex) findMatch(externalTitle, externalMBID string) (sqlc.ListTracksForArtistMatchingRow, bool) {
	if externalMBID != "" {
		if i, ok := idx.byMBID[externalMBID]; ok {
			return idx.rows[i], true
		}
	}
	for _, key := range titlematch.Normalizations(externalTitle) {
		if i, ok := idx.byKey[key]; ok {
			return idx.rows[i], true
		}
	}
	// Substring fallback — word-boundary scoped so "Odo" doesn't catch
	// "Odoru Ponpokorin" via prefix overlap. O(N) but N is tiny (one
	// artist's catalog) and only runs for the artist-detail page.
	ext := strings.ToLower(strings.TrimSpace(slug.Transliterate(externalTitle)))
	if len(ext) < 3 {
		return sqlc.ListTracksForArtistMatchingRow{}, false
	}
	extWords := titlematch.Tokenize(ext)
	for i, r := range idx.rows {
		loc := strings.ToLower(strings.TrimSpace(slug.Transliterate(r.Title)))
		if loc == "" {
			continue
		}
		locWords := titlematch.Tokenize(loc)
		if titlematch.ContainsWordSequence(locWords, extWords) || titlematch.ContainsWordSequence(extWords, locWords) {
			return idx.rows[i], true
		}
	}
	return sqlc.ListTracksForArtistMatchingRow{}, false
}
