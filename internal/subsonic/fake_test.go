package subsonic

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sessions"
)

// fakeBackend is an in-memory Backend for handler tests: one admin user
// with app password "sekret-app-pw", one artist / album / two tracks.
type fakeBackend struct {
	enabled  bool
	user     sqlc.User
	secret   string
	sessions *sessions.Store

	artists []service.SubsonicArtistRow
	albums  []sqlc.JFListAlbumsRow
	tracks  []sqlc.JFListTracksRow
	files   map[int64]service.SubsonicTrackFileInfo

	playQueue    *service.SubsonicPlayQueue
	scrobbles    []service.PlaybackEvent
	ratedTracks  map[int64]int16
	ratedAlbums  map[int64]int16
	ratedArtists map[int64]int16
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		enabled: true,
		user:    sqlc.User{ID: 1, Username: "admin", Email: "admin@localhost", IsAdmin: true},
		secret:  "sekret-app-pw",
		// Sort-name order, mirroring SubsonicListArtists' ORDER BY contract.
		artists: []service.SubsonicArtistRow{
			{ArtistID: 6, Name: "Aphex Twin", SortName: "Aphex Twin", MediaItemID: 60, Slug: "aphex-twin", AlbumCount: 0},
			{ArtistID: 5, Name: "The Prodigy", SortName: "Prodigy, The", MediaItemID: 50, Slug: "the-prodigy", AlbumCount: 1, MusicbrainzID: "mbid-prodigy"},
		},
		albums: []sqlc.JFListAlbumsRow{
			{ID: 10, ArtistID: 5, Title: "The Fat of the Land", Slug: "the-fat-of-the-land", Year: "1997",
				Genres: []string{"Electronic"}, TotalTracks: 2, DurationSeconds: 400,
				ArtistName: "The Prodigy", ArtistMediaItemID: 50, ArtistSlug: "the-prodigy", LibraryID: 1},
		},
		tracks: []sqlc.JFListTracksRow{
			{ID: 100, AlbumID: 10, DiscNumber: 1, TrackNumber: 1, Title: "Smack My Bitch Up", Duration: 342,
				AlbumTitle: "The Fat of the Land", AlbumSlug: "the-fat-of-the-land", AlbumGenres: []string{"Electronic"},
				ArtistID: 5, ArtistName: "The Prodigy", ArtistMediaItemID: 50, ArtistSlug: "the-prodigy", LibraryID: 1, BestFileID: 1000},
			{ID: 101, AlbumID: 10, DiscNumber: 1, TrackNumber: 2, Title: "Breathe", Duration: 336,
				AlbumTitle: "The Fat of the Land", AlbumSlug: "the-fat-of-the-land", AlbumGenres: []string{"Electronic"},
				ArtistID: 5, ArtistName: "The Prodigy", ArtistMediaItemID: 50, ArtistSlug: "the-prodigy", LibraryID: 1, BestFileID: 1001},
		},
		files: map[int64]service.SubsonicTrackFileInfo{
			100: {TrackID: 100, TrackFileID: 1000, LibraryFileID: 2000, Format: "flac", BitrateKbps: 1000, SizeBytes: 42_000_000, Duration: 342, Path: "/music/prodigy/01.flac"},
			101: {TrackID: 101, TrackFileID: 1001, LibraryFileID: 2001, Format: "mp3", BitrateKbps: 320, SizeBytes: 8_000_000, Duration: 336, Path: "/music/prodigy/02.mp3"},
		},
		ratedTracks:  map[int64]int16{},
		ratedAlbums:  map[int64]int16{},
		ratedArtists: map[int64]int16{},
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return NewMiddleware(newFakeBackend(), http.NotFoundHandler())
}

// --- gate + boot ---

func (f *fakeBackend) SubsonicEnabled() bool                { return f.enabled }
func (f *fakeBackend) LoadSubsonicFromDB(_ context.Context) {}

// --- auth ---

func (f *fakeBackend) SubsonicAuthByUsername(_ context.Context, username string) (sqlc.User, string, error) {
	if username != f.user.Username {
		return sqlc.User{}, "", service.ErrSubsonicNoCredential
	}
	return f.user, f.secret, nil
}

func (f *fakeBackend) SubsonicAuthBySecret(_ context.Context, secret string) (sqlc.User, error) {
	if secret != f.secret {
		return sqlc.User{}, service.ErrSubsonicNoCredential
	}
	return f.user, nil
}

func (f *fakeBackend) TouchSubsonicCredential(int64) {}

// --- browse ---

func (f *fakeBackend) ListLibraries(_ context.Context) ([]sqlc.Library, error) {
	return []sqlc.Library{{ID: 1, Name: "Music", MediaType: sqlc.MediaTypeMusic}}, nil
}

func (f *fakeBackend) SubsonicListArtists(_ context.Context, _ int64) ([]service.SubsonicArtistRow, error) {
	return f.artists, nil
}

func (f *fakeBackend) SubsonicArtistByID(_ context.Context, id int64) (service.SubsonicArtistRow, error) {
	for _, a := range f.artists {
		if a.ArtistID == id {
			return a, nil
		}
	}
	return service.SubsonicArtistRow{}, service.ErrSubsonicNoCredential // any error works
}

func (f *fakeBackend) SubsonicArtistByName(_ context.Context, name string) (service.SubsonicArtistRow, error) {
	for _, a := range f.artists {
		if strings.EqualFold(a.Name, name) {
			return a, nil
		}
	}
	return service.SubsonicArtistRow{}, service.ErrSubsonicNoCredential
}

func (f *fakeBackend) SubsonicListGenres(_ context.Context) ([]service.SubsonicGenreRow, error) {
	return []service.SubsonicGenreRow{{Name: "Electronic", AlbumCount: 1, SongCount: 2}}, nil
}

func (f *fakeBackend) JFListAlbums(_ context.Context, p sqlc.JFListAlbumsParams) ([]sqlc.JFListAlbumsRow, int64, error) {
	var out []sqlc.JFListAlbumsRow
	for _, al := range f.albums {
		if len(p.OnlyIds) > 0 && !containsID(p.OnlyIds, al.ID) {
			continue
		}
		if p.ArtistMediaItemID != 0 && al.ArtistMediaItemID != p.ArtistMediaItemID {
			continue
		}
		if p.Search != "" && !containsFold(al.Title, p.Search) {
			continue
		}
		out = append(out, al)
	}
	return out, int64(len(out)), nil
}

func (f *fakeBackend) JFListTracks(_ context.Context, p sqlc.JFListTracksParams) ([]sqlc.JFListTracksRow, int64, error) {
	var out []sqlc.JFListTracksRow
	for _, tr := range f.tracks {
		if len(p.OnlyIds) > 0 && !containsID(p.OnlyIds, tr.ID) {
			continue
		}
		if p.AlbumID != 0 && tr.AlbumID != p.AlbumID {
			continue
		}
		if p.ArtistMediaItemID != 0 && tr.ArtistMediaItemID != p.ArtistMediaItemID {
			continue
		}
		if p.Search != "" && !containsFold(tr.Title, p.Search) {
			continue
		}
		out = append(out, tr)
		if p.Lim > 0 && int32(len(out)) >= p.Lim {
			break
		}
	}
	return out, int64(len(out)), nil
}

func (f *fakeBackend) SubsonicAlbumAddedAt(_ context.Context, ids []int64) (map[int64]time.Time, error) {
	out := map[int64]time.Time{}
	for _, id := range ids {
		out[id] = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	}
	return out, nil
}

func (f *fakeBackend) SubsonicTrackBestFiles(_ context.Context, ids []int64) (map[int64]service.SubsonicTrackFileInfo, error) {
	out := map[int64]service.SubsonicTrackFileInfo{}
	for _, id := range ids {
		if fi, ok := f.files[id]; ok {
			out[id] = fi
		}
	}
	return out, nil
}

func (f *fakeBackend) SubsonicTrackPlayCounts(_ context.Context, _ int64, _ []int64) (map[int64]int64, error) {
	return map[int64]int64{}, nil
}

func (f *fakeBackend) GetMusicCounts(_ context.Context) (*service.MusicCounts, error) {
	return &service.MusicCounts{Artists: 2, Albums: 1, Tracks: 2}, nil
}

// --- lists ---

func (f *fakeBackend) SubsonicAlbumIDsByList(_ context.Context, _ string, _ int64, _, _ int32, _ string, _, _ int32) ([]int64, error) {
	return []int64{10}, nil
}

func (f *fakeBackend) SubsonicRandomTrackIDs(_ context.Context, _ int32, _ string, _, _ int32) ([]int64, error) {
	return []int64{101, 100}, nil
}

func (f *fakeBackend) SubsonicTrackIDsByGenre(_ context.Context, _ string, _, _ int32) ([]int64, error) {
	return []int64{100, 101}, nil
}

// --- per-user state ---

func emptyPage[T any]() (*service.MusicListPage[T], error) {
	return &service.MusicListPage[T]{Items: []T{}}, nil
}

func (f *fakeBackend) ListUserRatedTracks(_ context.Context, _ int64, minRating, _ int16, _, _ int32) (*service.MusicListPage[sqlc.ListUserRatedTracksRow], error) {
	page, _ := emptyPage[sqlc.ListUserRatedTracksRow]()
	for id, r := range f.ratedTracks {
		if r >= minRating {
			page.Items = append(page.Items, sqlc.ListUserRatedTracksRow{TrackID: id, Rating: r})
		}
	}
	page.Total = int64(len(page.Items))
	return page, nil
}

func (f *fakeBackend) ListUserRatedArtists(_ context.Context, _ int64, minRating, _ int16, _, _ int32) (*service.MusicListPage[sqlc.ListUserRatedArtistsRow], error) {
	page, _ := emptyPage[sqlc.ListUserRatedArtistsRow]()
	for id, r := range f.ratedArtists {
		if r >= minRating {
			page.Items = append(page.Items, sqlc.ListUserRatedArtistsRow{ID: id, Rating: r})
		}
	}
	page.Total = int64(len(page.Items))
	return page, nil
}

func (f *fakeBackend) ListUserRatedAlbums(_ context.Context, _ int64, minRating, _ int16, _, _ int32) (*service.MusicListPage[sqlc.ListUserRatedAlbumsRow], error) {
	page, _ := emptyPage[sqlc.ListUserRatedAlbumsRow]()
	for id, r := range f.ratedAlbums {
		if r >= minRating {
			page.Items = append(page.Items, sqlc.ListUserRatedAlbumsRow{ID: id, Rating_2: r})
		}
	}
	page.Total = int64(len(page.Items))
	return page, nil
}

func (f *fakeBackend) RatingsForTracks(_ context.Context, _ int64, ids []int64) (map[int64]int16, error) {
	out := map[int64]int16{}
	for _, id := range ids {
		if r, ok := f.ratedTracks[id]; ok && r > 0 {
			out[id] = r
		}
	}
	return out, nil
}

func (f *fakeBackend) SetUserTrackRating(_ context.Context, _, id int64, rating int16) error {
	f.ratedTracks[id] = rating
	return nil
}

func (f *fakeBackend) SetUserAlbumRating(_ context.Context, _, id int64, rating int16) error {
	f.ratedAlbums[id] = rating
	return nil
}

func (f *fakeBackend) SetUserArtistRating(_ context.Context, _, id int64, rating int16) error {
	f.ratedArtists[id] = rating
	return nil
}

func (f *fakeBackend) RecordPlayback(_ context.Context, _ int64, ev service.PlaybackEvent) error {
	f.scrobbles = append(f.scrobbles, ev)
	return nil
}

func (f *fakeBackend) GetSubsonicPlayQueue(_ context.Context, _ int64) (service.SubsonicPlayQueue, bool, error) {
	if f.playQueue == nil {
		return service.SubsonicPlayQueue{}, false, nil
	}
	return *f.playQueue, true, nil
}

func (f *fakeBackend) SaveSubsonicPlayQueue(_ context.Context, _ int64, q service.SubsonicPlayQueue) error {
	if len(q.TrackIDs) == 0 {
		f.playQueue = nil
		return nil
	}
	q.ChangedAt = time.Now()
	f.playQueue = &q
	return nil
}

// --- playlists (unused by current tests; explicit not-implemented) ---

func (f *fakeBackend) CreateUserPlaylist(_ context.Context, _ int64, name, _, _ string) (sqlc.UserPlaylist, error) {
	return sqlc.UserPlaylist{ID: 1, Name: name}, nil
}

func (f *fakeBackend) ListUserPlaylists(_ context.Context, _ int64) ([]sqlc.ListUserPlaylistsRow, error) {
	return []sqlc.ListUserPlaylistsRow{}, nil
}

func (f *fakeBackend) GetUserPlaylistDetail(_ context.Context, _, _ int64) (*service.PlaylistDetail, error) {
	return &service.PlaylistDetail{Tracks: []sqlc.ListPlaylistTracksRow{}}, nil
}

func (f *fakeBackend) AddTrackToPlaylist(_ context.Context, _, _, _ int64) error      { return nil }
func (f *fakeBackend) RemoveTrackFromPlaylist(_ context.Context, _, _, _ int64) error { return nil }
func (f *fakeBackend) DeleteUserPlaylist(_ context.Context, _, _ int64) error         { return nil }
func (f *fakeBackend) UpdateUserPlaylist(_ context.Context, _, _ int64, _, _, _ string, _ []string) error {
	return nil
}

// --- discovery ---

func (f *fakeBackend) GetSimilarArtists(_ context.Context, _ int64) ([]service.SimilarArtistRow, error) {
	return []service.SimilarArtistRow{{Name: "Aphex Twin", LocalArtistID: 6, LocalSlug: "aphex-twin"}}, nil
}

func (f *fakeBackend) SimilarMusicTracks(_ context.Context, _ int64, _ int32) ([]sqlc.SimilarTracksByTrackRichRow, error) {
	return []sqlc.SimilarTracksByTrackRichRow{}, nil
}

func (f *fakeBackend) ListArtistTopTracksBySlug(_ context.Context, _ string, _ int32) ([]service.ArtistTopTrackRow, error) {
	return []service.ArtistTopTrackRow{{Rank: 1, Title: "Breathe", LocalTrackID: 101}}, nil
}

// --- media + misc ---

func (f *fakeBackend) ListTrackFiles(_ context.Context, _ int64) ([]sqlc.TrackFile, error) {
	return []sqlc.TrackFile{}, nil
}

func (f *fakeBackend) ListUsers(_ context.Context) ([]sqlc.User, error) {
	return []sqlc.User{f.user}, nil
}

func (f *fakeBackend) Sessions() *sessions.Store      { return f.sessions }
func (f *fakeBackend) EnqueueScanLibrary(int64, bool) {}

func containsID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
