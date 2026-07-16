package matcher

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

// albumWriteTitleYear decides the (title, year) to persist for a single album
// during enrichment, guarding the uq_albums_artist_title_year unique index
// — (artist_id, lower(title), year).
//
// Enrichment can collapse two distinct local albums onto the same tuple:
//   - title drift: a standard and a deluxe folder both fuzzy-match one
//     upstream release, so both want the same canonical title; or
//   - year drift: a row with no local year gets the upstream year backfilled,
//     landing it on a same-titled sibling.
//
// Either rewrite trips the constraint and fails that album's UPDATE outright.
// When the normalized (title, year) is already owned by a *different* album of
// the same artist — reported by collides — both fields fall back to the local
// values so the row keeps its existing index slot and the remaining enriched
// columns (MBID, label, cover, …) still land.
//
// An empty embeddedTitle preserves the local title (upstream had nothing to
// say); the candidate year is the caller's already-resolved target.
func albumWriteTitleYear(embeddedTitle, localTitle, candidateYear, localYear string, collides func(title, year string) bool) (title, year string) {
	title = embeddedTitle
	if title == "" {
		title = localTitle
	}
	year = candidateYear
	if (!strings.EqualFold(title, localTitle) || year != localYear) && collides(title, year) {
		return localTitle, localYear
	}
	return title, year
}

// writeArtistExtendedMetadata fills the post-00019 columns on the artists
// row. Separated from the main RefreshMusicArtist body so its JSON encoding
// noise doesn't drown out the matching logic.
//
// Empty / zero values are written verbatim — heya.media is authoritative
// for this slice of the row, so a refresh that doesn't see a value should
// clear what was there (unlike the bio/name fields where NFOs may carry
// richer data than the upstream).
func (m *Matcher) writeArtistExtendedMetadata(ctx context.Context, artistID int64, d *metadata.MediaDetail) error {
	urlsJSON, _ := json.Marshal(safeURLs(d.ArtistURLs))
	wikipediaJSON, _ := json.Marshal(safeStringMap(d.ArtistWikipedia))
	profilesJSON, _ := json.Marshal(safeStringMap(d.ArtistProfiles))
	groupsJSON, _ := json.Marshal(safeRelations(d.ArtistGroups))
	membersJSON, _ := json.Marshal(safeRelations(d.ArtistMembers))

	return m.q.UpdateArtistExtendedMetadata(ctx, sqlc.UpdateArtistExtendedMetadataParams{
		ID:              artistID,
		Listeners:       d.ArtistListeners,
		Playcount:       d.ArtistPlaycount,
		Popularity:      int32(d.ArtistPopularity),
		Annotation:      d.ArtistAnnotation,
		Urls:            urlsJSON,
		WikipediaLinks:  wikipediaJSON,
		Profiles:        profilesJSON,
		Aliases:         nonNilStrings(d.ArtistAliases),
		Groups:          groupsJSON,
		Members:         membersJSON,
		ArtistType:      d.ArtistType,
		BeginDate:       d.ArtistBeginDate,
		BeginYear:       int32(d.ArtistBeginYear),
		EndDate:         d.ArtistEndDate,
		Ended:           d.ArtistEnded,
		Deathday:        d.ArtistDeathday,
		Birthplace:      d.ArtistBirthplace,
		Tags:            nonNilStrings(d.ArtistTags),
		Genres:          nonNilStrings(d.Genres),
		MetadataSources: nonNilStrings(d.ArtistMetadataSources),
	})
}

func (m *Matcher) writeArtistTopTracks(ctx context.Context, artistID int64, tops []metadata.TopTrackEntry) error {
	type topTrackRow struct {
		Rank              int    `json:"rank"`
		Provider          string `json:"provider"`
		ProviderRank      int    `json:"provider_rank"`
		Title             string `json:"title"`
		MBID              string `json:"mbid"`
		RecordingEntityID string `json:"recording_entity_id"`
		Playcount         int64  `json:"playcount"`
		Listeners         int64  `json:"listeners"`
		URL               string `json:"url"`
	}
	rows := make([]topTrackRow, 0, len(tops))
	for i, t := range tops {
		rows = append(rows, topTrackRow{Rank: i + 1, Provider: t.Provider, ProviderRank: t.Rank, Title: t.Title, MBID: t.MBID,
			RecordingEntityID: t.RecordingEntityID, Playcount: t.Playcount,
			Listeners: t.Listeners, URL: t.URL})
	}
	body, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	if err := m.q.DeleteArtistTopTracks(ctx, artistID); err != nil {
		return err
	}
	return m.q.InsertArtistTopTracks(ctx, sqlc.InsertArtistTopTracksParams{ArtistID: artistID, Tracks: body})
}

func (m *Matcher) writeArtistSimilarArtists(ctx context.Context, artistID int64, sims []metadata.SimilarArtistEntry) error {
	if err := m.q.ReplaceArtistSimilarArtists(ctx, artistID); err != nil {
		return err
	}
	// Three providers now feed this list and often suggest the same artist;
	// dedupe by folded name, keeping the first hit (upstream orders Last.fm
	// — the only provider with MBIDs — ahead of Deezer/Tidal).
	seen := make(map[string]struct{}, len(sims))
	rank := 0
	for _, s := range sims {
		nameKey := strings.ToLower(strings.TrimSpace(s.Name))
		if nameKey == "" {
			continue
		}
		if _, dup := seen[nameKey]; dup {
			continue
		}
		seen[nameKey] = struct{}{}
		// Best-effort local-artist resolution: when the similar artist's
		// MBID matches a row in our artists table, link to it so the
		// frontend can render an internal /music/artist/{slug} link
		// instead of just an external Last.fm badge.
		var localID pgtype.Int8
		if s.MBID != "" {
			if local, err := m.q.GetArtistByMusicBrainzID(ctx, s.MBID); err == nil {
				localID = pgtype.Int8{Int64: local.ID, Valid: true}
			}
		}
		if err := m.q.CreateArtistSimilarArtist(ctx, sqlc.CreateArtistSimilarArtistParams{
			ArtistID:      artistID,
			Rank:          int32(rank),
			Name:          s.Name,
			Mbid:          s.MBID,
			MatchScore:    pgNumericFromFloat(s.Match),
			Url:           s.URL,
			LocalArtistID: localID,
			Provider:      s.Provider,
		}); err != nil {
			return err
		}
		rank++
	}
	return nil
}

// writeArtistMusicVideos replaces the artist's media_videos rows (YouTube
// music videos, TheAudioDB via heya.media). Same replace-on-refresh story
// as similar artists: the entity document is authoritative, including an
// empty list. Artists only ever carry music videos, so the by-item delete
// cannot collide with another video kind.
func (m *Matcher) writeArtistMusicVideos(ctx context.Context, mediaItemID int64, videos []metadata.VideoDetail) error {
	if err := m.q.DeleteMediaVideosByItem(ctx, mediaItemID); err != nil {
		return err
	}
	for _, v := range videos {
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
			return err
		}
	}
	return nil
}

func (m *Matcher) writeAlbumExtendedMetadata(ctx context.Context, albumID int64, e *metadata.AlbumEntry) error {
	externalIDsJSON, _ := json.Marshal(safeStringMap(e.ExternalIDs))
	creditsJSON, _ := json.Marshal(safeCredits(e.ArtistCredits))
	ratingsJSON, _ := json.Marshal(nonNilRatings(e.Ratings))
	editionsJSON, _ := json.Marshal(nonNilEditions(e.Editions))

	return m.q.UpdateAlbumExtendedMetadata(ctx, sqlc.UpdateAlbumExtendedMetadataParams{
		ID:             albumID,
		Column2:        e.CatalogNo,
		Column3:        e.OriginalTitle,
		Column4:        e.Language,
		Explicit:       e.Explicit,
		Column6:        int32(e.Duration),
		Column7:        pgNumericFromFloat(e.Rating),
		Popularity:     int32(e.Popularity),
		Listeners:      e.Listeners,
		Playcount:      e.Playcount,
		SecondaryTypes: nonNilStrings(e.SecondaryTypes),
		Styles:         nonNilStrings(e.Styles),
		Isrcs:          nonNilStrings(e.ISRCs),
		ExternalIds:    externalIDsJSON,
		ArtistCredits:  creditsJSON,
		Column16:       e.Description,
		Column17:       e.Review,
		Ratings:        ratingsJSON,
		Editions:       editionsJSON,
		Column20:       e.Sales,
	})
}

func nonNilRatings(values []metadata.AlbumRating) []metadata.AlbumRating {
	if values == nil {
		return []metadata.AlbumRating{}
	}
	return values
}

func nonNilEditions(values []metadata.AlbumEdition) []metadata.AlbumEdition {
	if values == nil {
		return []metadata.AlbumEdition{}
	}
	return values
}

func (m *Matcher) writeTrackExtendedMetadata(ctx context.Context, trackID int64, t *metadata.TrackDetail) error {
	externalIDsJSON, _ := json.Marshal(safeStringMap(t.ExternalIDs))
	creditsJSON, _ := json.Marshal(safeCredits(t.ArtistCredits))
	return m.q.UpdateTrackExtendedMetadata(ctx, sqlc.UpdateTrackExtendedMetadataParams{
		ID:            trackID,
		ExternalIds:   externalIDsJSON,
		Column3:       t.ISRC,
		Column4:       t.RecordingMBID,
		Column5:       t.PreviewURL,
		Explicit:      t.Explicit,
		ArtistCredits: creditsJSON,
	})
}

// --- JSONB encoding safeguards ------------------------------------------

// JSONB columns are NOT NULL with default '{}' / '[]'. Marshalling a nil
// map produces "null" which violates that constraint and confuses the
// frontend's typed client. Helpers below substitute empty-but-non-nil
// values so encoders always produce a real JSON object / array.

func safeStringMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func safeURLs(urls []metadata.URLEntry) []metadata.URLEntry {
	if urls == nil {
		return []metadata.URLEntry{}
	}
	return urls
}

func safeRelations(rels []metadata.ArtistRelationEntry) []metadata.ArtistRelationEntry {
	if rels == nil {
		return []metadata.ArtistRelationEntry{}
	}
	return rels
}

func safeCredits(credits []metadata.ArtistCreditEntry) []metadata.ArtistCreditEntry {
	if credits == nil {
		return []metadata.ArtistCreditEntry{}
	}
	return credits
}

// nonNilStrings replaces a nil slice with an empty one so the generated
// query gets a real `text[]` instead of NULL (the columns are NOT NULL
// DEFAULT '{}' and pgx-typed nil-vs-empty distinction trips the constraint).
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// pgNumericFromFloat converts a Go float64 into pgtype.Numeric for the
// NUMERIC(N,M) columns. We don't need precision beyond what float64 gives
// here — the upstream values are mostly two-decimal ratings / scores.
func pgNumericFromFloat(f float64) pgtype.Numeric {
	if f == 0 {
		return pgtype.Numeric{Valid: true, Int: nil, Exp: 0}
	}
	var n pgtype.Numeric
	// Scan handles the string formatting + Int128 decomposition that
	// matches pgx's wire format for numerics. Cheaper than rolling our
	// own decimal math.
	_ = n.Scan(formatFloatForNumeric(f))
	return n
}

// formatFloatForNumeric renders the float without scientific notation so
// pgtype.Numeric.Scan accepts it. strconv.FormatFloat with 'f' format is
// the cleanest path; 4 digits of precision matches the column scales
// (NUMERIC(4,2) for rating, NUMERIC(6,4) for match_score).
func formatFloatForNumeric(f float64) string {
	return strconv.FormatFloat(f, 'f', 4, 64)
}
