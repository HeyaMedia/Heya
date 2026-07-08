package ingestv2

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

type MovieDetailProvider interface {
	GetDetail(context.Context, string, *metadata.FetchOptions) (*metadata.MediaDetail, error)
}

type TVDetailProvider interface {
	GetDetail(context.Context, string, *metadata.FetchOptions) (*metadata.MediaDetail, error)
}

type MovieFetchPreview struct {
	Key            string                `json:"key"`
	ProviderID     string                `json:"provider_id"`
	Title          string                `json:"title,omitempty"`
	Year           string                `json:"year,omitempty"`
	ExternalIDs    map[string]string     `json:"external_ids,omitempty"`
	HeyaSlug       string                `json:"heya_slug,omitempty"`
	RuntimeMinutes int                   `json:"runtime_minutes,omitempty"`
	Genres         []string              `json:"genres,omitempty"`
	Collection     string                `json:"collection,omitempty"`
	Artwork        int                   `json:"artwork,omitempty"`
	Cast           int                   `json:"cast,omitempty"`
	Crew           int                   `json:"crew,omitempty"`
	WouldApply     []string              `json:"would_apply,omitempty"`
	Error          string                `json:"error,omitempty"`
	Detail         *metadata.MediaDetail `json:"-"`
}

type TVFetchPreview struct {
	Key             string                `json:"key"`
	Keys            []string              `json:"keys,omitempty"`
	LocalIdentities int                   `json:"local_identities,omitempty"`
	ProviderID      string                `json:"provider_id"`
	Title           string                `json:"title,omitempty"`
	Year            string                `json:"year,omitempty"`
	ExternalIDs     map[string]string     `json:"external_ids,omitempty"`
	HeyaSlug        string                `json:"heya_slug,omitempty"`
	Status          string                `json:"status,omitempty"`
	FirstAirDate    string                `json:"first_air_date,omitempty"`
	LastAirDate     string                `json:"last_air_date,omitempty"`
	Genres          []string              `json:"genres,omitempty"`
	Networks        []string              `json:"networks,omitempty"`
	Seasons         int                   `json:"seasons,omitempty"`
	RemoteEpisodes  int                   `json:"remote_episodes,omitempty"`
	PlannedEpisodes int                   `json:"planned_episodes,omitempty"`
	MappedEpisodes  int                   `json:"mapped_episodes,omitempty"`
	PlannedFiles    int                   `json:"planned_files,omitempty"`
	MissingEpisodes []TVEpisodeRef        `json:"missing_episodes,omitempty"`
	Artwork         int                   `json:"artwork,omitempty"`
	Cast            int                   `json:"cast,omitempty"`
	Crew            int                   `json:"crew,omitempty"`
	WouldApply      []string              `json:"would_apply,omitempty"`
	Error           string                `json:"error,omitempty"`
	Detail          *metadata.MediaDetail `json:"-"`
}

type tvFetchGroup struct {
	search TVSearchMatch
	keys   []string
	local  []TVMatch
}

func FetchMovieMetadataPreviews(ctx context.Context, search []MovieSearchMatch, provider MovieDetailProvider, emit Emitter) ([]MovieFetchPreview, error) {
	if provider == nil {
		return nil, fmt.Errorf("movie detail provider is required")
	}

	var previews []MovieFetchPreview
	for _, match := range search {
		if err := ctx.Err(); err != nil {
			return previews, err
		}
		if !match.Accepted {
			continue
		}
		preview := MovieFetchPreview{Key: match.Key, ProviderID: match.ProviderID}
		emit.Emit(Event{
			Event: "metadata.fetch",
			Kind:  "movie",
			Data: map[string]any{
				"key":         match.Key,
				"provider_id": match.ProviderID,
				"title":       match.Title,
				"year":        match.Year,
			},
		})

		detail, err := provider.GetDetail(ctx, match.ProviderID, nil)
		if err != nil {
			preview.Error = err.Error()
			emit.Emit(Event{
				Event:    "metadata.fetch_failed",
				Severity: SeverityWarn,
				Kind:     "movie",
				Reason:   "detail_fetch_failed",
				Message:  err.Error(),
				Data: map[string]any{
					"key":         match.Key,
					"provider_id": match.ProviderID,
				},
			})
			previews = append(previews, preview)
			continue
		}

		preview = movieFetchPreview(match, detail)
		previews = append(previews, preview)
		emit.Emit(Event{
			Event: "metadata.preview",
			Kind:  "movie",
			Data: map[string]any{
				"key":          preview.Key,
				"provider_id":  preview.ProviderID,
				"title":        preview.Title,
				"year":         preview.Year,
				"external_ids": preview.ExternalIDs,
				"would_apply":  preview.WouldApply,
			},
		})
	}

	failures := 0
	for _, preview := range previews {
		if preview.Error != "" {
			failures++
		}
	}
	emit.Emit(Event{Event: "metadata.preview_summary", Data: map[string]any{"domain": "movie", "previews": len(previews), "failures": failures}})
	return previews, nil
}

func FetchTVMetadataPreviews(ctx context.Context, search []TVSearchMatch, matches []TVMatch, provider TVDetailProvider, emit Emitter) ([]TVFetchPreview, error) {
	if provider == nil {
		return nil, fmt.Errorf("TV detail provider is required")
	}

	matchByKey := map[string]TVMatch{}
	for _, match := range matches {
		matchByKey[match.Key] = match
	}

	var previews []TVFetchPreview
	for _, group := range tvFetchGroups(search, matchByKey) {
		if err := ctx.Err(); err != nil {
			return previews, err
		}
		searchMatch := group.search
		localMatch := combineTVFetchMatches(group.local)
		preview := TVFetchPreview{
			Key:             strings.Join(group.keys, ","),
			Keys:            group.keys,
			LocalIdentities: len(group.keys),
			ProviderID:      searchMatch.ProviderID,
			PlannedEpisodes: len(localMatch.Episodes),
			PlannedFiles:    len(localMatch.Files),
		}
		emit.Emit(Event{
			Event: "metadata.fetch",
			Kind:  "tv",
			Data: map[string]any{
				"key":         searchMatch.Key,
				"keys":        group.keys,
				"provider_id": searchMatch.ProviderID,
				"title":       searchMatch.Title,
				"year":        searchMatch.Year,
			},
		})

		detail, err := provider.GetDetail(ctx, searchMatch.ProviderID, nil)
		if err != nil {
			preview.Error = err.Error()
			emit.Emit(Event{
				Event:    "metadata.fetch_failed",
				Severity: SeverityWarn,
				Kind:     "tv",
				Reason:   "detail_fetch_failed",
				Message:  err.Error(),
				Data: map[string]any{
					"key":         searchMatch.Key,
					"provider_id": searchMatch.ProviderID,
				},
			})
			previews = append(previews, preview)
			continue
		}

		preview = tvFetchPreview(searchMatch, localMatch, detail)
		preview.Key = strings.Join(group.keys, ",")
		preview.Keys = group.keys
		preview.LocalIdentities = len(group.keys)
		previews = append(previews, preview)
		emit.Emit(Event{
			Event: "metadata.preview",
			Kind:  "tv",
			Data: map[string]any{
				"key":              preview.Key,
				"provider_id":      preview.ProviderID,
				"title":            preview.Title,
				"year":             preview.Year,
				"external_ids":     preview.ExternalIDs,
				"would_apply":      preview.WouldApply,
				"planned_episodes": preview.PlannedEpisodes,
				"mapped_episodes":  preview.MappedEpisodes,
				"missing_episodes": len(preview.MissingEpisodes),
			},
		})
	}

	failures := 0
	for _, preview := range previews {
		if preview.Error != "" {
			failures++
		}
	}
	emit.Emit(Event{Event: "metadata.preview_summary", Data: map[string]any{"domain": "tv", "previews": len(previews), "failures": failures}})
	return previews, nil
}

func tvFetchGroups(search []TVSearchMatch, matchByKey map[string]TVMatch) []tvFetchGroup {
	byProvider := map[string]*tvFetchGroup{}
	var order []string
	for _, searchMatch := range search {
		if !searchMatch.Accepted {
			continue
		}
		providerID := strings.TrimSpace(searchMatch.ProviderID)
		if providerID == "" {
			providerID = searchMatch.Key
		}
		group := byProvider[providerID]
		if group == nil {
			group = &tvFetchGroup{search: searchMatch}
			byProvider[providerID] = group
			order = append(order, providerID)
		}
		group.keys = append(group.keys, searchMatch.Key)
		group.local = append(group.local, matchByKey[searchMatch.Key])
	}

	groups := make([]tvFetchGroup, 0, len(order))
	for _, providerID := range order {
		group := byProvider[providerID]
		group.keys = sortedUnique(group.keys)
		groups = append(groups, *group)
	}
	return groups
}

func combineTVFetchMatches(matches []TVMatch) TVMatch {
	var out TVMatch
	for _, match := range matches {
		if out.Title == "" {
			out.Title = match.Title
			out.Year = match.Year
		}
		out.Files = append(out.Files, match.Files...)
		out.Episodes = append(out.Episodes, match.Episodes...)
		out.Plans = append(out.Plans, match.Plans...)
		out.Assets = append(out.Assets, match.Assets...)
		out.Subtitles = append(out.Subtitles, match.Subtitles...)
	}
	out.Files = sortedUnique(out.Files)
	out.Subtitles = sortedUnique(out.Subtitles)
	sort.Slice(out.Episodes, func(i, j int) bool {
		if out.Episodes[i].Season == out.Episodes[j].Season {
			if out.Episodes[i].Episode == out.Episodes[j].Episode {
				return out.Episodes[i].Absolute < out.Episodes[j].Absolute
			}
			return out.Episodes[i].Episode < out.Episodes[j].Episode
		}
		return out.Episodes[i].Season < out.Episodes[j].Season
	})
	out.Episodes = uniqueTVEpisodeRefs(out.Episodes)
	sort.Slice(out.Assets, func(i, j int) bool {
		if out.Assets[i].RelPath == out.Assets[j].RelPath {
			return out.Assets[i].Type < out.Assets[j].Type
		}
		return out.Assets[i].RelPath < out.Assets[j].RelPath
	})
	out.Assets = uniqueTVAssets(out.Assets)
	sortTVPlans(out.Plans)
	return out
}

func movieFetchPreview(match MovieSearchMatch, detail *metadata.MediaDetail) MovieFetchPreview {
	if detail == nil {
		return MovieFetchPreview{Key: match.Key, ProviderID: match.ProviderID, Error: "empty detail"}
	}
	collection := ""
	if detail.Collection != nil {
		collection = detail.Collection.Name
	}
	return MovieFetchPreview{
		Key:            match.Key,
		ProviderID:     match.ProviderID,
		Title:          detail.Title,
		Year:           detail.Year,
		ExternalIDs:    detail.ExternalIDs,
		HeyaSlug:       detail.HeyaSlug,
		RuntimeMinutes: detail.RuntimeMinutes,
		Genres:         detail.Genres,
		Collection:     collection,
		Artwork:        len(detail.Artwork),
		Cast:           len(detail.Cast),
		Crew:           len(detail.Crew),
		WouldApply:     movieWouldApply(detail),
		Detail:         detail,
	}
}

func movieWouldApply(detail *metadata.MediaDetail) []string {
	if detail == nil {
		return nil
	}
	var fields []string
	if detail.Title != "" {
		fields = append(fields, "title")
	}
	if detail.Year != "" {
		fields = append(fields, "year")
	}
	if detail.Description != "" {
		fields = append(fields, "description")
	}
	if len(detail.ExternalIDs) > 0 {
		fields = append(fields, "external_ids")
	}
	if detail.RuntimeMinutes > 0 {
		fields = append(fields, "runtime")
	}
	if len(detail.Genres) > 0 {
		fields = append(fields, "genres")
	}
	if detail.PosterURL != "" {
		fields = append(fields, "poster")
	}
	if detail.BackdropURL != "" {
		fields = append(fields, "backdrop")
	}
	if len(detail.Artwork) > 0 {
		fields = append(fields, "artwork")
	}
	if len(detail.Cast) > 0 {
		fields = append(fields, "cast")
	}
	if len(detail.Crew) > 0 {
		fields = append(fields, "crew")
	}
	if detail.Collection != nil && detail.Collection.Name != "" {
		fields = append(fields, "collection")
	}
	sort.Strings(fields)
	return fields
}

func tvFetchPreview(searchMatch TVSearchMatch, localMatch TVMatch, detail *metadata.MediaDetail) TVFetchPreview {
	if detail == nil {
		return TVFetchPreview{Key: searchMatch.Key, ProviderID: searchMatch.ProviderID, Error: "empty detail"}
	}
	seasonEpisodes, absoluteEpisodes, remoteEpisodes := tvRemoteEpisodeIndex(detail)
	mapped := 0
	var missing []TVEpisodeRef
	for _, ref := range localMatch.Episodes {
		if tvRemoteHasEpisode(ref, seasonEpisodes, absoluteEpisodes) {
			mapped++
			continue
		}
		missing = append(missing, ref)
	}
	return TVFetchPreview{
		Key:             searchMatch.Key,
		ProviderID:      searchMatch.ProviderID,
		Title:           detail.Title,
		Year:            detail.Year,
		ExternalIDs:     detail.ExternalIDs,
		HeyaSlug:        detail.HeyaSlug,
		Status:          detail.Status,
		FirstAirDate:    detail.FirstAirDate,
		LastAirDate:     detail.LastAirDate,
		Genres:          detail.Genres,
		Networks:        tvNetworkNames(detail.Networks),
		Seasons:         tvRemoteSeasonCount(detail),
		RemoteEpisodes:  remoteEpisodes,
		PlannedEpisodes: len(localMatch.Episodes),
		MappedEpisodes:  mapped,
		PlannedFiles:    len(localMatch.Files),
		MissingEpisodes: missing,
		Artwork:         len(detail.Artwork),
		Cast:            len(detail.Cast),
		Crew:            len(detail.Crew),
		WouldApply:      tvWouldApply(detail),
		Detail:          detail,
	}
}

func tvWouldApply(detail *metadata.MediaDetail) []string {
	if detail == nil {
		return nil
	}
	var fields []string
	if detail.Title != "" {
		fields = append(fields, "title")
	}
	if detail.Year != "" {
		fields = append(fields, "year")
	}
	if detail.Description != "" {
		fields = append(fields, "description")
	}
	if len(detail.ExternalIDs) > 0 {
		fields = append(fields, "external_ids")
	}
	if len(detail.Genres) > 0 {
		fields = append(fields, "genres")
	}
	if detail.PosterURL != "" {
		fields = append(fields, "poster")
	}
	if detail.BackdropURL != "" {
		fields = append(fields, "backdrop")
	}
	if len(detail.Artwork) > 0 {
		fields = append(fields, "artwork")
	}
	if len(detail.Cast) > 0 {
		fields = append(fields, "cast")
	}
	if len(detail.Crew) > 0 {
		fields = append(fields, "crew")
	}
	if detail.Status != "" {
		fields = append(fields, "status")
	}
	if detail.FirstAirDate != "" {
		fields = append(fields, "first_air_date")
	}
	if detail.LastAirDate != "" {
		fields = append(fields, "last_air_date")
	}
	if len(detail.Networks) > 0 {
		fields = append(fields, "networks")
	}
	if len(detail.CreatedBy) > 0 {
		fields = append(fields, "created_by")
	}
	if tvRemoteSeasonCount(detail) > 0 {
		fields = append(fields, "seasons")
	}
	if tvRemoteEpisodeCount(detail) > 0 {
		fields = append(fields, "episodes")
	}
	sort.Strings(fields)
	return fields
}

func tvRemoteSeasonCount(detail *metadata.MediaDetail) int {
	if detail == nil {
		return 0
	}
	if len(detail.Seasons) > 0 {
		return len(detail.Seasons)
	}
	return detail.NumberOfSeasons
}

func tvRemoteEpisodeCount(detail *metadata.MediaDetail) int {
	if detail == nil {
		return 0
	}
	_, _, count := tvRemoteEpisodeIndex(detail)
	if count > 0 {
		return count
	}
	return detail.NumberOfEpisodes
}

func tvRemoteEpisodeIndex(detail *metadata.MediaDetail) (map[int]map[int]bool, map[int]bool, int) {
	seasonEpisodes := map[int]map[int]bool{}
	absoluteEpisodes := map[int]bool{}
	if detail == nil {
		return seasonEpisodes, absoluteEpisodes, 0
	}
	count := 0
	for _, season := range detail.Seasons {
		for _, episode := range season.Episodes {
			if episode.Number > 0 {
				if seasonEpisodes[season.Number] == nil {
					seasonEpisodes[season.Number] = map[int]bool{}
				}
				if !seasonEpisodes[season.Number][episode.Number] {
					seasonEpisodes[season.Number][episode.Number] = true
					count++
				}
			}
			if episode.AbsoluteNumber > 0 {
				absoluteEpisodes[episode.AbsoluteNumber] = true
			}
		}
	}
	if count == 0 {
		count = detail.NumberOfEpisodes
	}
	return seasonEpisodes, absoluteEpisodes, count
}

func tvRemoteHasEpisode(ref TVEpisodeRef, seasonEpisodes map[int]map[int]bool, absoluteEpisodes map[int]bool) bool {
	if ref.Absolute > 0 && absoluteEpisodes[ref.Absolute] {
		return true
	}
	if ref.Episode > 0 {
		return seasonEpisodes[ref.Season][ref.Episode]
	}
	return false
}

func tvNetworkNames(networks []metadata.NetworkDetail) []string {
	names := make([]string, 0, len(networks))
	for _, network := range networks {
		if network.Name != "" {
			names = append(names, network.Name)
		}
	}
	sort.Strings(names)
	return sortedUnique(names)
}
