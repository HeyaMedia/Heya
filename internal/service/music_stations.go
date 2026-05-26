package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// StationTrack is the unified row shape returned by every Quick Station.
// Mirrors SimilarTracksByTrackRichRow so the FE renders Library Radio /
// Deep Cuts / Time Travel / Random Album with one row component.
type StationTrack struct {
	TrackID        int64  `json:"track_id"`
	TrackTitle     string `json:"track_title"`
	Duration       int32  `json:"duration"`
	DiscNumber     int32  `json:"disc_number"`
	TrackNumber    int32  `json:"track_number"`
	AlbumID        int64  `json:"album_id"`
	AlbumTitle     string `json:"album_title"`
	AlbumSlug      string `json:"album_slug"`
	AlbumCoverPath string `json:"album_cover_path"`
	AlbumYear      string `json:"album_year"`
	ArtistID       int64  `json:"artist_id"`
	ArtistName     string `json:"artist_name"`
	ArtistSlug     string `json:"artist_slug"`
}

// StationResponse is the envelope every Quick Station returns. `label` is a
// human-readable description ("Random Album: Brat", "1990s", "Deep Cuts")
// the FE renders as the page subtitle so each tap can advertise what it
// resolved to.
type StationResponse struct {
	Kind   string         `json:"kind"`
	Label  string         `json:"label"`
	Tracks []StationTrack `json:"tracks"`
}

// LibraryRadio: N random tracks from across the music library.
func (a *App) LibraryRadio(ctx context.Context, limit int32) (*StationResponse, error) {
	limit = clampStationLimit(limit)
	rows, err := sqlc.New(a.db).ListRandomMusicTracks(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("library radio: %w", err)
	}
	tracks := make([]StationTrack, len(rows))
	for i, r := range rows {
		tracks[i] = randomRowToStationTrack(r)
	}
	return &StationResponse{Kind: "library_radio", Label: "Library Radio", Tracks: tracks}, nil
}

// DeepCuts: tracks the user has never (or barely) played.
func (a *App) DeepCuts(ctx context.Context, userID int64, limit int32) (*StationResponse, error) {
	limit = clampStationLimit(limit)
	rows, err := sqlc.New(a.db).ListDeepCutsForUser(ctx, sqlc.ListDeepCutsForUserParams{
		UserID:     userID,
		TrackLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("deep cuts: %w", err)
	}
	tracks := make([]StationTrack, len(rows))
	for i, r := range rows {
		tracks[i] = deepCutsRowToStationTrack(r)
	}
	return &StationResponse{Kind: "deep_cuts", Label: "Deep Cuts", Tracks: tracks}, nil
}

// TimeTravel: random tracks from a year range. Min/max inclusive.
func (a *App) TimeTravel(ctx context.Context, minYear, maxYear int32, limit int32) (*StationResponse, error) {
	limit = clampStationLimit(limit)
	if minYear <= 0 {
		minYear = 1900
	}
	if maxYear <= 0 || maxYear < minYear {
		maxYear = minYear + 9
	}
	rows, err := sqlc.New(a.db).ListTracksByYearRange(ctx, sqlc.ListTracksByYearRangeParams{
		MinYear:    minYear,
		MaxYear:    maxYear,
		TrackLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("time travel: %w", err)
	}
	tracks := make([]StationTrack, len(rows))
	for i, r := range rows {
		tracks[i] = yearRangeRowToStationTrack(r)
	}
	label := fmt.Sprintf("%d–%d", minYear, maxYear)
	return &StationResponse{Kind: "time_travel", Label: label, Tracks: tracks}, nil
}

// RandomAlbum: one random album, end-to-end.
func (a *App) RandomAlbum(ctx context.Context) (*StationResponse, error) {
	rows, err := sqlc.New(a.db).PickRandomAlbumWithTracks(ctx)
	if err != nil {
		return nil, fmt.Errorf("random album: %w", err)
	}
	if len(rows) == 0 {
		return &StationResponse{Kind: "random_album", Label: "No albums yet", Tracks: nil}, nil
	}
	tracks := make([]StationTrack, len(rows))
	for i, r := range rows {
		tracks[i] = randomAlbumRowToStationTrack(r)
	}
	label := fmt.Sprintf("%s — %s", rows[0].ArtistName, rows[0].AlbumTitle)
	return &StationResponse{Kind: "random_album", Label: label, Tracks: tracks}, nil
}

func clampStationLimit(n int32) int32 {
	if n <= 0 {
		return 30
	}
	if n > 100 {
		return 100
	}
	return n
}

// The row→StationTrack mappers below are mechanical — sqlc generates a
// distinct struct per query even though every station query returns the
// same column set, so each one needs its own bridge.
func randomRowToStationTrack(r sqlc.ListRandomMusicTracksRow) StationTrack {
	return StationTrack{
		TrackID: r.TrackID, TrackTitle: r.TrackTitle, Duration: r.Duration,
		DiscNumber: r.DiscNumber, TrackNumber: r.TrackNumber,
		AlbumID: r.AlbumID, AlbumTitle: r.AlbumTitle, AlbumSlug: r.AlbumSlug,
		AlbumCoverPath: r.AlbumCoverPath, AlbumYear: r.AlbumYear,
		ArtistID: r.ArtistID, ArtistName: r.ArtistName, ArtistSlug: r.ArtistSlug,
	}
}

func deepCutsRowToStationTrack(r sqlc.ListDeepCutsForUserRow) StationTrack {
	return StationTrack{
		TrackID: r.TrackID, TrackTitle: r.TrackTitle, Duration: r.Duration,
		DiscNumber: r.DiscNumber, TrackNumber: r.TrackNumber,
		AlbumID: r.AlbumID, AlbumTitle: r.AlbumTitle, AlbumSlug: r.AlbumSlug,
		AlbumCoverPath: r.AlbumCoverPath, AlbumYear: r.AlbumYear,
		ArtistID: r.ArtistID, ArtistName: r.ArtistName, ArtistSlug: r.ArtistSlug,
	}
}

func yearRangeRowToStationTrack(r sqlc.ListTracksByYearRangeRow) StationTrack {
	return StationTrack{
		TrackID: r.TrackID, TrackTitle: r.TrackTitle, Duration: r.Duration,
		DiscNumber: r.DiscNumber, TrackNumber: r.TrackNumber,
		AlbumID: r.AlbumID, AlbumTitle: r.AlbumTitle, AlbumSlug: r.AlbumSlug,
		AlbumCoverPath: r.AlbumCoverPath, AlbumYear: r.AlbumYear,
		ArtistID: r.ArtistID, ArtistName: r.ArtistName, ArtistSlug: r.ArtistSlug,
	}
}

func randomAlbumRowToStationTrack(r sqlc.PickRandomAlbumWithTracksRow) StationTrack {
	return StationTrack{
		TrackID: r.TrackID, TrackTitle: r.TrackTitle, Duration: r.Duration,
		DiscNumber: r.DiscNumber, TrackNumber: r.TrackNumber,
		AlbumID: r.AlbumID, AlbumTitle: r.AlbumTitle, AlbumSlug: r.AlbumSlug,
		AlbumCoverPath: r.AlbumCoverPath, AlbumYear: r.AlbumYear,
		ArtistID: r.ArtistID, ArtistName: r.ArtistName, ArtistSlug: r.ArtistSlug,
	}
}
