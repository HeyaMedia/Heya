package matcher

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

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
		ID:             artistID,
		Listeners:      d.ArtistListeners,
		Playcount:      d.ArtistPlaycount,
		Popularity:     int32(d.ArtistPopularity),
		Annotation:     d.ArtistAnnotation,
		Urls:           urlsJSON,
		WikipediaLinks: wikipediaJSON,
		Profiles:       profilesJSON,
		Aliases:        nonNilStrings(d.ArtistAliases),
		Groups:         groupsJSON,
		Members:        membersJSON,
		ArtistType:     d.ArtistType,
		BeginDate:      d.ArtistBeginDate,
		BeginYear:      int32(d.ArtistBeginYear),
		EndDate:        d.ArtistEndDate,
		Ended:          d.ArtistEnded,
		Deathday:       d.ArtistDeathday,
		Birthplace:     d.ArtistBirthplace,
		Tags:           nonNilStrings(d.ArtistTags),
	})
}

func (m *Matcher) writeArtistTopTracks(ctx context.Context, artistID int64, tops []metadata.TopTrackEntry) error {
	if err := m.q.ReplaceArtistTopTracks(ctx, artistID); err != nil {
		return err
	}
	for i, t := range tops {
		if err := m.q.CreateArtistTopTrack(ctx, sqlc.CreateArtistTopTrackParams{
			ArtistID:  artistID,
			Rank:      int32(i),
			Title:     t.Title,
			Mbid:      t.MBID,
			Playcount: t.Playcount,
			Listeners: t.Listeners,
			Url:       t.URL,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Matcher) writeArtistSimilarArtists(ctx context.Context, artistID int64, sims []metadata.SimilarArtistEntry) error {
	if err := m.q.ReplaceArtistSimilarArtists(ctx, artistID); err != nil {
		return err
	}
	for i, s := range sims {
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
			Rank:          int32(i),
			Name:          s.Name,
			Mbid:          s.MBID,
			MatchScore:    pgNumericFromFloat(s.Match),
			Url:           s.URL,
			LocalArtistID: localID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Matcher) writeAlbumExtendedMetadata(ctx context.Context, albumID int64, e *metadata.AlbumEntry) error {
	externalIDsJSON, _ := json.Marshal(safeStringMap(e.ExternalIDs))
	creditsJSON, _ := json.Marshal(safeCredits(e.ArtistCredits))

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
	})
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
