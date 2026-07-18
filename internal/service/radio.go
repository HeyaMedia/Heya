package service

import (
	"context"
	"fmt"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/radiobrowser"
)

// SearchRadioStations is a thin pass-through to the cached radio-browser
// client. Lives in the service layer (not in the handler) so future
// rate-limiting / source-of-truth replacement can hook here.
func (a *App) SearchRadioStations(ctx context.Context, in radiobrowser.SearchParams) ([]radiobrowser.Station, error) {
	return a.radioBrowser.Search(ctx, in)
}

func (a *App) TopRadioStations(ctx context.Context, category radiobrowser.TopCategory, count int) ([]radiobrowser.Station, error) {
	return a.radioBrowser.Top(ctx, category, count)
}

func (a *App) RadioCountries(ctx context.Context) ([]radiobrowser.Country, error) {
	return a.radioBrowser.Countries(ctx)
}

func (a *App) RadioTags(ctx context.Context, limit int) ([]radiobrowser.Tag, error) {
	return a.radioBrowser.Tags(ctx, limit)
}

// ListRadioFavorites returns the user's saved stations, newest first.
func (a *App) ListRadioFavorites(ctx context.Context, userID int64) ([]sqlc.UserRadioFavorite, error) {
	return sqlc.New(a.db).ListRadioFavorites(ctx, userID)
}

// AddRadioFavorite upserts a station into favorites. Caller passes in the
// full radio-browser Station so we can persist a snapshot — the upstream
// can change a station's tags / bitrate but the favorite reflects what the
// user saw when they added it (refreshed transparently on re-add).
func (a *App) AddRadioFavorite(ctx context.Context, userID int64, s *radiobrowser.Station) (sqlc.UserRadioFavorite, error) {
	if s == nil || s.StationUUID == "" {
		return sqlc.UserRadioFavorite{}, fmt.Errorf("station missing stationuuid")
	}
	return sqlc.New(a.db).AddRadioFavorite(ctx, sqlc.AddRadioFavoriteParams{
		UserID:      userID,
		Stationuuid: s.StationUUID,
		Name:        s.Name,
		Url:         resolveStationURL(s),
		Favicon:     s.Favicon,
		Homepage:    s.Homepage,
		Country:     s.Country,
		Countrycode: s.CountryCode,
		Language:    s.Language,
		Tags:        s.Tags,
		Codec:       s.Codec,
		Bitrate:     int32(s.Bitrate),
	})
}

// RemoveRadioFavorite drops the favorite for the given UUID. No-op if not
// favorited (database constraint already covers the join).
func (a *App) RemoveRadioFavorite(ctx context.Context, userID int64, uuid string) error {
	return sqlc.New(a.db).RemoveRadioFavorite(ctx, sqlc.RemoveRadioFavoriteParams{
		UserID:      userID,
		Stationuuid: uuid,
	})
}

// ListRecentRadio returns the user's recently-played stations, deduped by
// stationuuid so a station looped all morning shows up once. Hard-capped
// here to keep the response light; the underlying table is pruned by
// PruneRadioRecents in the recents-vacuum job.
func (a *App) ListRecentRadio(ctx context.Context, userID int64, limit int32) ([]sqlc.ListRadioRecentsRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	return sqlc.New(a.db).ListRadioRecents(ctx, sqlc.ListRadioRecentsParams{
		UserID:     userID,
		TrackLimit: limit,
	})
}

// RecordRadioPlay appends to the recents log AND fires the upstream click
// so radio-browser's stats see the play. Called when the FE starts a stream.
func (a *App) RecordRadioPlay(ctx context.Context, userID int64, s *radiobrowser.Station) error {
	if s == nil || s.StationUUID == "" {
		return fmt.Errorf("station missing stationuuid")
	}
	_, err := sqlc.New(a.db).RecordRadioPlay(ctx, sqlc.RecordRadioPlayParams{
		UserID:      userID,
		Stationuuid: s.StationUUID,
		Name:        s.Name,
		Url:         resolveStationURL(s),
		Favicon:     s.Favicon,
		Country:     s.Country,
		Tags:        s.Tags,
		Codec:       s.Codec,
		Bitrate:     int32(s.Bitrate),
	})
	if err == nil {
		// Notify the upstream without blocking playback, but keep the detached
		// request App-owned so shutdown cancels and joins it before returning.
		stationUUID := s.StationUUID
		a.startBackground(func() {
			clickCtx, cancel := context.WithTimeout(a.LifetimeContext(), 15*time.Second)
			defer cancel()
			a.radioBrowser.PostClick(clickCtx, stationUUID)
		})
	}
	return err
}

// resolveStationURL prefers url_resolved (radio-browser's playable URL
// after PLS/M3U resolution) over the raw url field. Both are present in
// search results; FE callers can just use whatever we return.
func resolveStationURL(s *radiobrowser.Station) string {
	if s.URLResolved != "" {
		return s.URLResolved
	}
	return s.URL
}
