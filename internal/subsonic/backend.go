package subsonic

import (
	"context"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sessions"
)

// Backend is the slice of *service.App this package consumes — the same
// "handlers go through the service layer only" rule the Jellyfin surface
// follows, expressed as a consumer-side interface so unit tests can fake it
// without a database. *service.App satisfies it as-is.
type Backend interface {
	// Gate + boot.
	SubsonicEnabled() bool
	LoadSubsonicFromDB(ctx context.Context)

	// Auth (see internal/service/subsonic_credentials.go).
	SubsonicAuthByUsername(ctx context.Context, username string) (sqlc.User, string, error)
	SubsonicAuthBySecret(ctx context.Context, secret string) (sqlc.User, error)
	TouchSubsonicCredential(userID int64)

	// Browse.
	ListLibraries(ctx context.Context) ([]sqlc.Library, error)
	SubsonicListArtists(ctx context.Context, libraryID int64) ([]service.SubsonicArtistRow, error)
	SubsonicArtistByID(ctx context.Context, artistID int64) (service.SubsonicArtistRow, error)
	SubsonicArtistByName(ctx context.Context, name string) (service.SubsonicArtistRow, error)
	SubsonicListGenres(ctx context.Context) ([]service.SubsonicGenreRow, error)
	JFListAlbums(ctx context.Context, p sqlc.JFListAlbumsParams) ([]sqlc.JFListAlbumsRow, int64, error)
	JFListTracks(ctx context.Context, p sqlc.JFListTracksParams) ([]sqlc.JFListTracksRow, int64, error)
	SubsonicAlbumAddedAt(ctx context.Context, albumIDs []int64) (map[int64]time.Time, error)
	SubsonicTrackBestFiles(ctx context.Context, trackIDs []int64) (map[int64]service.SubsonicTrackFileInfo, error)
	SubsonicTrackPlayCounts(ctx context.Context, userID int64, trackIDs []int64) (map[int64]int64, error)
	GetMusicCounts(ctx context.Context) (*service.MusicCounts, error)

	// Lists.
	SubsonicAlbumIDsByList(ctx context.Context, listType string, userID int64, size, offset int32, genre string, fromYear, toYear int32) ([]int64, error)
	SubsonicRandomTrackIDs(ctx context.Context, size int32, genre string, fromYear, toYear int32) ([]int64, error)
	SubsonicTrackIDsByGenre(ctx context.Context, genre string, limit, offset int32) ([]int64, error)

	// Per-user state.
	ListUserLovedTrackIDs(ctx context.Context, userID int64) ([]int64, error)
	ListUserLovedArtistIDs(ctx context.Context, userID int64) ([]int64, error)
	ListUserLovedAlbumIDs(ctx context.Context, userID int64) ([]int64, error)
	ListUserLovedTracks(ctx context.Context, userID int64, limit, offset int32) (*service.MusicListPage[sqlc.ListUserLovedTracksRow], error)
	ListUserLovedArtists(ctx context.Context, userID int64, limit, offset int32) (*service.MusicListPage[sqlc.ListUserLovedArtistsRow], error)
	ListUserLovedAlbums(ctx context.Context, userID int64, limit, offset int32) (*service.MusicListPage[sqlc.ListUserLovedAlbumsRow], error)
	SetEntityLoved(ctx context.Context, userID int64, entityType string, entityID int64, loved bool) (bool, error)
	SetUserTrackRating(ctx context.Context, userID, trackID int64, rating int16) error
	SetUserAlbumRating(ctx context.Context, userID, albumID int64, rating int16) error
	SetUserArtistRating(ctx context.Context, userID, artistID int64, rating int16) error
	RatingsForTracks(ctx context.Context, userID int64, trackIDs []int64) (map[int64]int16, error)
	RecordPlayback(ctx context.Context, userID int64, ev service.PlaybackEvent) error
	GetSubsonicPlayQueue(ctx context.Context, userID int64) (service.SubsonicPlayQueue, bool, error)
	SaveSubsonicPlayQueue(ctx context.Context, userID int64, q service.SubsonicPlayQueue) error

	// Playlists.
	CreateUserPlaylist(ctx context.Context, userID int64, name, description, cover string) (sqlc.UserPlaylist, error)
	ListUserPlaylists(ctx context.Context, userID int64) ([]sqlc.ListUserPlaylistsRow, error)
	GetUserPlaylistDetail(ctx context.Context, userID, playlistID int64) (*service.PlaylistDetail, error)
	AddTrackToPlaylist(ctx context.Context, userID, playlistID, trackID int64) error
	RemoveTrackFromPlaylist(ctx context.Context, userID, playlistID, trackID int64) error
	DeleteUserPlaylist(ctx context.Context, userID, playlistID int64) error
	UpdateUserPlaylist(ctx context.Context, userID, playlistID int64, name, description, cover string) error

	// Discovery.
	GetSimilarArtists(ctx context.Context, artistID int64) ([]service.SimilarArtistRow, error)
	SimilarMusicTracks(ctx context.Context, seedTrackID int64, limit int32) ([]sqlc.SimilarTracksByTrackRichRow, error)
	ListArtistTopTracksBySlug(ctx context.Context, artistSlug string, limit int32) ([]service.ArtistTopTrackRow, error)

	// Media + misc.
	ListTrackFiles(ctx context.Context, trackID int64) ([]sqlc.TrackFile, error)
	ListUsers(ctx context.Context) ([]sqlc.User, error)
	Sessions() *sessions.Store
	EnqueueScanLibrary(id int64, force bool)
}
