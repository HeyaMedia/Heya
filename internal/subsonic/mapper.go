package subsonic

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

// Row → DTO assembly. The JF* listers return tracks/albums without user
// state or file facts, so hydration batches the decorations per page:
// best file (suffix/bitrate/size), album year, added-at, star timestamps,
// and play counts. Everything is one query per concern, never per row.

// ignoredArticles is Subsonic's stock list; clients strip these when
// building their own indexes.
const ignoredArticles = "The El La Los Las Le Les"

// starState holds the user's loved timestamps per entity kind for one
// request. Built lazily — most requests never need it twice.
type starState struct {
	tracks  map[int64]time.Time
	albums  map[int64]time.Time
	artists map[int64]time.Time
}

// starStateFor pages through the hearted lists — ratings ≥ 9 in the unified
// rating store, the same band the web app's heart reaction writes — building
// the timestamp maps (500 per page, hard cap 20 pages). Errors degrade to
// "nothing starred" — a star icon is not worth failing a browse response.
func (s *Server) starStateFor(ctx context.Context, userID int64) starState {
	st := starState{
		tracks:  map[int64]time.Time{},
		albums:  map[int64]time.Time{},
		artists: map[int64]time.Time{},
	}
	const pageSize = 500
	const heart = 9
	for offset := int32(0); offset < 20*pageSize; offset += pageSize {
		page, err := s.app.ListUserRatedTracks(ctx, userID, heart, pageSize, offset)
		if err != nil || page == nil {
			break
		}
		for _, r := range page.Items {
			st.tracks[r.TrackID] = r.RatedAt.Time
		}
		if int64(offset)+int64(len(page.Items)) >= page.Total || len(page.Items) == 0 {
			break
		}
	}
	for offset := int32(0); offset < 20*pageSize; offset += pageSize {
		page, err := s.app.ListUserRatedAlbums(ctx, userID, heart, pageSize, offset)
		if err != nil || page == nil {
			break
		}
		for _, r := range page.Items {
			st.albums[r.ID] = r.RatedAt.Time
		}
		if int64(offset)+int64(len(page.Items)) >= page.Total || len(page.Items) == 0 {
			break
		}
	}
	for offset := int32(0); offset < 20*pageSize; offset += pageSize {
		page, err := s.app.ListUserRatedArtists(ctx, userID, heart, pageSize, offset)
		if err != nil || page == nil {
			break
		}
		for _, r := range page.Items {
			st.artists[r.ID] = r.RatedAt.Time
		}
		if int64(offset)+int64(len(page.Items)) >= page.Total || len(page.Items) == 0 {
			break
		}
	}
	return st
}

func starredAt(m map[int64]time.Time, id int64) *subTime {
	if t, ok := m[id]; ok {
		return subTimePtr(t)
	}
	return nil
}

// trackContext carries the batched per-page decorations for Child building.
type trackContext struct {
	files     map[int64]service.SubsonicTrackFileInfo
	albumYear map[int64]int32
	added     map[int64]time.Time
	stars     starState
	plays     map[int64]int64
	ratings   map[int64]int16
}

// trackContextFor batches every decoration the given track rows need.
func (s *Server) trackContextFor(ctx context.Context, userID int64, rows []sqlc.JFListTracksRow) trackContext {
	trackIDs := make([]int64, 0, len(rows))
	albumIDSet := map[int64]bool{}
	for _, r := range rows {
		trackIDs = append(trackIDs, r.ID)
		albumIDSet[r.AlbumID] = true
	}
	albumIDs := make([]int64, 0, len(albumIDSet))
	for id := range albumIDSet {
		albumIDs = append(albumIDs, id)
	}

	tc := trackContext{
		files:     map[int64]service.SubsonicTrackFileInfo{},
		albumYear: map[int64]int32{},
		added:     map[int64]time.Time{},
		plays:     map[int64]int64{},
		stars:     s.starStateFor(ctx, userID),
	}
	if files, err := s.app.SubsonicTrackBestFiles(ctx, trackIDs); err == nil {
		tc.files = files
	}
	if added, err := s.app.SubsonicAlbumAddedAt(ctx, albumIDs); err == nil {
		tc.added = added
	}
	if plays, err := s.app.SubsonicTrackPlayCounts(ctx, userID, trackIDs); err == nil {
		tc.plays = plays
	}
	if ratings, err := s.app.RatingsForTracks(ctx, userID, trackIDs); err == nil {
		tc.ratings = ratings
	}
	if len(albumIDs) > 0 {
		if albums, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{OnlyIds: albumIDs}); err == nil {
			for _, al := range albums {
				tc.albumYear[al.ID] = parseYear(al.Year)
			}
		}
	}
	return tc
}

// childFromTrack renders one Child (song) row.
func childFromTrack(tr sqlc.JFListTracksRow, tc trackContext) Child {
	c := Child{
		ID:       EncodeID(KindTrack, tr.ID),
		Parent:   EncodeID(KindAlbum, tr.AlbumID),
		IsDir:    false,
		Title:    tr.Title,
		Album:    tr.AlbumTitle,
		Artist:   tr.ArtistName,
		CoverArt: EncodeID(KindTrack, tr.ID),
		Duration: tr.Duration,
		AlbumID:  EncodeID(KindAlbum, tr.AlbumID),
		ArtistID: EncodeID(KindArtist, tr.ArtistID),
		Type:     "music",
		IsVideo:  false,
	}
	if tr.TrackNumber > 0 {
		n := tr.TrackNumber
		c.Track = &n
	}
	if tr.DiscNumber > 0 {
		d := tr.DiscNumber
		c.DiscNumber = &d
	}
	if y := tc.albumYear[tr.AlbumID]; y > 0 {
		c.Year = &y
	}
	if len(tr.AlbumGenres) > 0 {
		c.Genre = tr.AlbumGenres[0]
		for _, g := range tr.AlbumGenres {
			c.Genres = append(c.Genres, ItemGenre{Name: g})
		}
	}
	if f, ok := tc.files[tr.ID]; ok {
		c.Suffix = suffixOf(f)
		c.ContentType = contentTypeForSuffix(c.Suffix)
		c.Size = f.SizeBytes
		c.BitRate = f.BitrateKbps
		if c.Duration == 0 {
			c.Duration = f.Duration
		}
		c.Path = virtualPath(tr.ArtistName, tr.AlbumTitle, tr.TrackNumber, tr.Title, c.Suffix)
	}
	if t, ok := tc.added[tr.AlbumID]; ok {
		c.Created = subTimePtr(t)
	}
	c.Starred = starredAt(tc.stars.tracks, tr.ID)
	if n := tc.plays[tr.ID]; n > 0 {
		c.PlayCount = &n
	}
	// Heya ratings are 1..10; Subsonic speaks 1..5 stars (setRating writes
	// stars*2, so this halves round-half-up for odd native ratings).
	if r := tc.ratings[tr.ID]; r > 0 {
		stars := int32(r+1) / 2
		c.UserRating = &stars
	}
	return c
}

// childrenFromTracks hydrates a page of track rows into Child DTOs.
func (s *Server) childrenFromTracks(ctx context.Context, userID int64, rows []sqlc.JFListTracksRow) []Child {
	tc := s.trackContextFor(ctx, userID, rows)
	out := make([]Child, 0, len(rows))
	for _, r := range rows {
		out = append(out, childFromTrack(r, tc))
	}
	return out
}

// tracksByIDs fetches + hydrates tracks preserving the given id order —
// list queries return ranking-ordered ids, JFListTracks does not preserve
// them.
func (s *Server) tracksByIDs(ctx context.Context, userID int64, ids []int64) []Child {
	if len(ids) == 0 {
		return []Child{}
	}
	rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: ids})
	if err != nil {
		return []Child{}
	}
	children := s.childrenFromTracks(ctx, userID, rows)
	byID := make(map[int64]Child, len(children))
	for i, r := range rows {
		byID[r.ID] = children[i]
	}
	out := make([]Child, 0, len(ids))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out
}

// albumID3From renders one AlbumID3 row.
func albumID3From(al sqlc.JFListAlbumsRow, added map[int64]time.Time, stars starState) AlbumID3 {
	a := AlbumID3{
		ID:        EncodeID(KindAlbum, al.ID),
		Name:      al.Title,
		Artist:    al.ArtistName,
		ArtistID:  EncodeID(KindArtist, al.ArtistID),
		CoverArt:  EncodeID(KindAlbum, al.ID),
		SongCount: al.TotalTracks,
		Duration:  al.DurationSeconds,
	}
	if y := parseYear(al.Year); y > 0 {
		a.Year = &y
	}
	if len(al.Genres) > 0 {
		a.Genre = al.Genres[0]
		for _, g := range al.Genres {
			a.Genres = append(a.Genres, ItemGenre{Name: g})
		}
	}
	if t, ok := added[al.ID]; ok {
		a.Created = subTimePtr(t)
	}
	a.Starred = starredAt(stars.albums, al.ID)
	return a
}

// albumsByIDs fetches + hydrates albums preserving id order.
func (s *Server) albumsByIDs(ctx context.Context, userID int64, ids []int64) []AlbumID3 {
	if len(ids) == 0 {
		return []AlbumID3{}
	}
	rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{OnlyIds: ids})
	if err != nil {
		return []AlbumID3{}
	}
	albums := s.albumID3sFrom(ctx, userID, rows)
	byID := make(map[int64]AlbumID3, len(albums))
	for i, r := range rows {
		byID[r.ID] = albums[i]
	}
	out := make([]AlbumID3, 0, len(ids))
	for _, id := range ids {
		if a, ok := byID[id]; ok {
			out = append(out, a)
		}
	}
	return out
}

// albumID3sFrom hydrates a page of album rows (added-at + stars batched).
func (s *Server) albumID3sFrom(ctx context.Context, userID int64, rows []sqlc.JFListAlbumsRow) []AlbumID3 {
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	added, _ := s.app.SubsonicAlbumAddedAt(ctx, ids)
	stars := s.starStateFor(ctx, userID)
	out := make([]AlbumID3, 0, len(rows))
	for _, r := range rows {
		out = append(out, albumID3From(r, added, stars))
	}
	return out
}

// artistID3From renders one ArtistID3 row.
func artistID3From(ar service.SubsonicArtistRow, stars starState) ArtistID3 {
	return ArtistID3{
		ID:            EncodeID(KindArtist, ar.ArtistID),
		Name:          ar.Name,
		CoverArt:      EncodeID(KindArtist, ar.ArtistID),
		AlbumCount:    ar.AlbumCount,
		MusicBrainzID: ar.MusicbrainzID,
		SortName:      ar.SortName,
		Starred:       starredAt(stars.artists, ar.ArtistID),
	}
}

// indexArtists groups artists into Subsonic index buckets (A-Z, # for
// everything else) on the article-stripped sort name.
func indexArtists(artists []ArtistID3, sortKeys []string) []IndexID3 {
	buckets := map[string][]ArtistID3{}
	var order []string
	for i, a := range artists {
		key := indexBucket(sortKeys[i])
		if _, seen := buckets[key]; !seen {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], a)
	}
	// The artist list arrives sort-ordered, so first-seen bucket order is
	// already alphabetical (with # possibly interleaved — pull it last).
	out := make([]IndexID3, 0, len(order))
	var hash *IndexID3
	for _, key := range order {
		idx := IndexID3{Name: key, Artists: buckets[key]}
		if key == "#" {
			hash = &idx
			continue
		}
		out = append(out, idx)
	}
	if hash != nil {
		out = append(out, *hash)
	}
	return out
}

func indexBucket(name string) string {
	n := strings.TrimSpace(strings.ToUpper(name))
	for _, article := range strings.Fields(strings.ToUpper(ignoredArticles)) {
		if strings.HasPrefix(n, article+" ") {
			n = strings.TrimSpace(strings.TrimPrefix(n, article+" "))
			break
		}
	}
	if n == "" {
		return "#"
	}
	c := rune(n[0])
	if c >= 'A' && c <= 'Z' {
		return string(c)
	}
	return "#"
}

func parseYear(s string) int32 {
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil && y > 0 && y <= 9999 {
			return int32(y) //nolint:gosec // G109: bounded 1..9999 by the guard above
		}
	}
	return 0
}

func suffixOf(f service.SubsonicTrackFileInfo) string {
	if f.Format != "" {
		return strings.ToLower(f.Format)
	}
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(f.Path), "."))
}

var suffixContentTypes = map[string]string{
	"mp3":  "audio/mpeg",
	"flac": "audio/flac",
	"m4a":  "audio/mp4",
	"mp4":  "audio/mp4",
	"aac":  "audio/aac",
	"alac": "audio/mp4",
	"ogg":  "audio/ogg",
	"oga":  "audio/ogg",
	"opus": "audio/opus",
	"wav":  "audio/wav",
	"aif":  "audio/aiff",
	"aiff": "audio/aiff",
	"wma":  "audio/x-ms-wma",
	"ape":  "audio/x-ape",
	"dsf":  "audio/x-dsf",
	"wv":   "audio/x-wavpack",
}

func contentTypeForSuffix(suffix string) string {
	if ct, ok := suffixContentTypes[suffix]; ok {
		return ct
	}
	return "application/octet-stream"
}

// Tiny JF-param builders — the generic listers take a wide param struct;
// these name the two shapes this package uses constantly.
func jfTracksByIDs(ids ...int64) sqlc.JFListTracksParams {
	return sqlc.JFListTracksParams{OnlyIds: ids}
}

func jfAlbumsByIDs(ids ...int64) sqlc.JFListAlbumsParams {
	return sqlc.JFListAlbumsParams{OnlyIds: ids}
}

func jfTracksBySearch(query string, limit int32) sqlc.JFListTracksParams {
	return sqlc.JFListTracksParams{Search: query, Lim: limit}
}

// virtualPath fabricates the stable pseudo-path clients use for offline
// file naming. Heya's real on-disk layout is none of the client's business.
func virtualPath(artist, album string, trackNo int32, title, suffix string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch r {
			case '/', '\\', ':':
				return '_'
			}
			return r
		}, s)
	}
	name := clean(title)
	if trackNo > 0 {
		name = fmt.Sprintf("%02d - %s", trackNo, name)
	}
	if suffix != "" {
		name += "." + suffix
	}
	return clean(artist) + "/" + clean(album) + "/" + name
}
