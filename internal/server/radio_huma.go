package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/radiobrowser"
	"github.com/karbowiak/heya/internal/service"
)

// registerRadioRoutes mounts the internet-radio surface:
//
//   - /api/radio/* — proxy + cache for the community radio-browser API
//   - /api/me/radio/* — per-user favorites + recents
//   - /api/radio/stream — streaming proxy with ICY metadata extraction
//     (mounted via binary_huma.go since it returns raw audio bytes)
func registerRadioRoutes(api huma.API, app *service.App) {
	// Top-N curated lists. Three categories matching radio-browser's: vote
	// (community-voted), click (most-played), lastchange (newly-registered).
	huma.Register(api, secured(op(http.MethodGet, "/api/radio/top", "radio-top", "Top stations (votes / clicks / lastchange)", "Radio")),
		func(ctx context.Context, in *struct {
			Category string `query:"category" enum:"topvote,topclick,lastchange" default:"topvote"`
			Count    int    `query:"count"    minimum:"1" maximum:"100" default:"30"`
		}) (*JSONOutput[stationsBody], error) {
			rows, err := app.TopRadioStations(ctx, radiobrowser.TopCategory(in.Category), in.Count)
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return cachedJSON(stationsBody{Items: rows}, 300), nil
		})

	// Search across the radio-browser catalog. All filters are optional —
	// the FE passes whatever the user typed; upstream handles fuzzy match.
	huma.Register(api, secured(op(http.MethodGet, "/api/radio/search", "radio-search", "Search radio stations", "Radio")),
		func(ctx context.Context, in *struct {
			Name        string `query:"name"        maxLength:"200"`
			Tag         string `query:"tag"         maxLength:"100"`
			Country     string `query:"country"     maxLength:"100"`
			CountryCode string `query:"countrycode" maxLength:"4"`
			Limit       int    `query:"limit"       minimum:"1" maximum:"200" default:"30"`
			Offset      int    `query:"offset"      minimum:"0" default:"0"`
		}) (*JSONOutput[stationsBody], error) {
			rows, err := app.SearchRadioStations(ctx, radiobrowser.SearchParams{
				Name: in.Name, Tag: in.Tag, Country: in.Country, CountryCode: in.CountryCode,
				Limit: in.Limit, Offset: in.Offset,
			})
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return cachedJSON(stationsBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/radio/countries", "radio-countries", "All countries with at least one station", "Radio")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[radioCountriesBody], error) {
			rows, err := app.RadioCountries(ctx)
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return cachedJSON(radioCountriesBody{Items: rows}, 3600), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/radio/tags", "radio-tags", "Most popular station tags", "Radio")),
		func(ctx context.Context, in *struct {
			Limit int `query:"limit" minimum:"1" maximum:"500" default:"100"`
		}) (*JSONOutput[radioTagsBody], error) {
			rows, err := app.RadioTags(ctx, in.Limit)
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return cachedJSON(radioTagsBody{Items: rows}, 3600), nil
		})

	// --- Per-user favorites + recents ---
	huma.Register(api, secured(op(http.MethodGet, "/api/me/radio/favorites", "list-radio-favorites", "User's favorited stations", "Radio")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[radioFavoritesBody], error) {
			rows, err := app.ListRadioFavorites(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(radioFavoritesBody{Items: rows}), nil
		})

	// Add favorite — body carries the station snapshot. Use a relaxed local
	// type rather than radiobrowser.Station so the FE can post partial rows
	// (e.g. when re-favoriting from the recents rail where we don't have
	// every upstream field). Only stationuuid + name are strictly required.
	huma.Register(api, secured(op(http.MethodPost, "/api/me/radio/favorites", "add-radio-favorite", "Save a station to favorites", "Radio")),
		func(ctx context.Context, in *struct {
			Body stationInput
		}) (*JSONOutput[sqlc.UserRadioFavorite], error) {
			row, err := app.AddRadioFavorite(ctx, userFrom(ctx).ID, in.Body.toStation())
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(row), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/radio/favorites/{uuid}", "remove-radio-favorite", "Unfavorite a station", "Radio")),
		func(ctx context.Context, in *struct {
			UUID string `path:"uuid" pattern:"^[a-f0-9-]+$" maxLength:"64"`
		}) (*JSONOutput[okBody], error) {
			if err := app.RemoveRadioFavorite(ctx, userFrom(ctx).ID, in.UUID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(okBody{Ok: true}), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/radio/recents", "list-radio-recents", "User's recently-played stations (deduped)", "Radio")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"30"`
		}) (*JSONOutput[radioRecentsBody], error) {
			rows, err := app.ListRecentRadio(ctx, userFrom(ctx).ID, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(radioRecentsBody{Items: rows}), nil
		})

	// Click / play event — the FE fires this when a station starts playing
	// so radio-browser sees the play AND we land it in the recents log.
	huma.Register(api, secured(op(http.MethodPost, "/api/me/radio/play", "record-radio-play", "Record a station play (recents + upstream click)", "Radio")),
		func(ctx context.Context, in *struct {
			Body stationInput
		}) (*JSONOutput[okBody], error) {
			if err := app.RecordRadioPlay(ctx, userFrom(ctx).ID, in.Body.toStation()); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(okBody{Ok: true}), nil
		})
}

// stationInput is the request-body shape for favorite + play. Mirrors the
// radiobrowser.Station JSON tags but with every field except `stationuuid`
// and `name` optional so the FE can POST partial rows (favoriting from the
// recents rail, which only stores a subset of metadata, is a real case).
type stationInput struct {
	StationUUID string `json:"stationuuid" minLength:"1" maxLength:"64"`
	Name        string `json:"name"        minLength:"1" maxLength:"500"`
	URL         string `json:"url,omitempty"`
	URLResolved string `json:"url_resolved,omitempty"`
	Favicon     string `json:"favicon,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"countrycode,omitempty"`
	Language    string `json:"language,omitempty"`
	Tags        string `json:"tags,omitempty"`
	Codec       string `json:"codec,omitempty"`
	Bitrate     int    `json:"bitrate,omitempty"`
	Votes       int    `json:"votes,omitempty"`
	ClickCount  int    `json:"clickcount,omitempty"`
}

func (in stationInput) toStation() *radiobrowser.Station {
	return &radiobrowser.Station{
		StationUUID: in.StationUUID, Name: in.Name, URL: in.URL, URLResolved: in.URLResolved,
		Favicon: in.Favicon, Homepage: in.Homepage, Country: in.Country, CountryCode: in.CountryCode,
		Language: in.Language, Tags: in.Tags, Codec: in.Codec, Bitrate: in.Bitrate,
		Votes: in.Votes, ClickCount: in.ClickCount,
	}
}

// Typed envelopes — same pattern as the music similarity endpoints.
type stationsBody struct {
	Items []radiobrowser.Station `json:"items"`
}

type radioCountriesBody struct {
	Items []radiobrowser.Country `json:"items"`
}

type radioTagsBody struct {
	Items []radiobrowser.Tag `json:"items"`
}

type radioFavoritesBody struct {
	Items []sqlc.UserRadioFavorite `json:"items"`
}

type radioRecentsBody struct {
	Items []sqlc.ListRadioRecentsRow `json:"items"`
}
