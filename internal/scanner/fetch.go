package scanner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/titlematch"
)

type MovieDetailProvider interface {
	GetDetail(context.Context, string, *metadata.FetchOptions) (*metadata.MediaDetail, error)
}

type TVDetailProvider interface {
	GetDetail(context.Context, string, *metadata.FetchOptions) (*metadata.MediaDetail, error)
}

type MusicDetailProvider interface {
	GetDetail(context.Context, string, *metadata.FetchOptions) (*metadata.MediaDetail, error)
}

type BookDetailProvider interface {
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

type MusicFetchPreview struct {
	Key                  string                          `json:"key"`
	ProviderID           string                          `json:"provider_id"`
	SearchProviderID     string                          `json:"search_provider_id,omitempty"`
	SelectionReason      string                          `json:"selection_reason,omitempty"`
	Artist               string                          `json:"artist,omitempty"`
	SortName             string                          `json:"sort_name,omitempty"`
	ExternalIDs          map[string]string               `json:"external_ids,omitempty"`
	HeyaSlug             string                          `json:"heya_slug,omitempty"`
	LocalAlbums          int                             `json:"local_albums,omitempty"`
	MappedAlbums         int                             `json:"mapped_albums,omitempty"`
	RemoteAlbums         int                             `json:"remote_albums,omitempty"`
	LocalTracks          int                             `json:"local_tracks,omitempty"`
	MappedTracks         int                             `json:"mapped_tracks,omitempty"`
	RemoteTracks         int                             `json:"remote_tracks,omitempty"`
	Artwork              int                             `json:"artwork,omitempty"`
	Tags                 int                             `json:"tags,omitempty"`
	AlbumMappings        []MusicAlbumFetchMatch          `json:"album_mappings,omitempty"`
	CandidateEvaluations []MusicCandidateFetchEvaluation `json:"candidate_evaluations,omitempty"`
	Issues               []string                        `json:"issues,omitempty"`
	WouldApply           []string                        `json:"would_apply,omitempty"`
	Error                string                          `json:"error,omitempty"`
	Detail               *metadata.MediaDetail           `json:"-"`
}

type BookFetchPreview struct {
	Key         string                `json:"key"`
	ProviderID  string                `json:"provider_id"`
	Title       string                `json:"title,omitempty"`
	Author      string                `json:"author,omitempty"`
	Year        string                `json:"year,omitempty"`
	Format      string                `json:"format,omitempty"`
	ExternalIDs map[string]string     `json:"external_ids,omitempty"`
	HeyaSlug    string                `json:"heya_slug,omitempty"`
	PosterURL   string                `json:"poster_url,omitempty"`
	PageCount   int                   `json:"page_count,omitempty"`
	Publisher   string                `json:"publisher,omitempty"`
	PublishDate string                `json:"publish_date,omitempty"`
	Language    string                `json:"language,omitempty"`
	Subjects    []string              `json:"subjects,omitempty"`
	Issues      []string              `json:"issues,omitempty"`
	WouldApply  []string              `json:"would_apply,omitempty"`
	Error       string                `json:"error,omitempty"`
	Detail      *metadata.MediaDetail `json:"-"`
}

type MusicCandidateFetchEvaluation struct {
	ProviderID   string  `json:"provider_id"`
	Artist       string  `json:"artist,omitempty"`
	Confidence   float64 `json:"confidence,omitempty"`
	MappedAlbums int     `json:"mapped_albums,omitempty"`
	LocalAlbums  int     `json:"local_albums,omitempty"`
	MappedTracks int     `json:"mapped_tracks,omitempty"`
	LocalTracks  int     `json:"local_tracks,omitempty"`
	Selected     bool    `json:"selected,omitempty"`
	Error        string  `json:"error,omitempty"`
}

type MusicAlbumFetchMatch struct {
	Key               string                 `json:"key"`
	LocalAlbum        string                 `json:"local_album"`
	LocalYear         string                 `json:"local_year,omitempty"`
	LocalKind         string                 `json:"local_kind,omitempty"`
	RemoteAlbum       string                 `json:"remote_album,omitempty"`
	RemoteYear        int                    `json:"remote_year,omitempty"`
	RemoteKind        string                 `json:"remote_kind,omitempty"`
	LocalExternalIDs  map[string]string      `json:"local_external_ids,omitempty"`
	RemoteExternalIDs map[string]string      `json:"remote_external_ids,omitempty"`
	Confidence        float64                `json:"confidence,omitempty"`
	Reason            string                 `json:"reason,omitempty"`
	LocalTracks       int                    `json:"local_tracks,omitempty"`
	MappedTracks      int                    `json:"mapped_tracks,omitempty"`
	RemoteTracks      int                    `json:"remote_tracks,omitempty"`
	TrackMappings     []MusicTrackFetchMatch `json:"track_mappings,omitempty"`
	Issues            []string               `json:"issues,omitempty"`
}

type MusicTrackFetchMatch struct {
	RelPath     string  `json:"rel_path"`
	LocalTitle  string  `json:"local_title"`
	LocalDisc   int     `json:"local_disc,omitempty"`
	LocalTrack  int     `json:"local_track,omitempty"`
	RemoteTitle string  `json:"remote_title,omitempty"`
	RemoteDisc  int     `json:"remote_disc,omitempty"`
	RemoteTrack int     `json:"remote_track,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Reason      string  `json:"reason,omitempty"`
	Matched     bool    `json:"matched"`
	Issue       string  `json:"issue,omitempty"`
}

type tvFetchGroup struct {
	search TVSearchMatch
	keys   []string
	local  []TVMatch
}

const musicFetchConcurrency = 4
const musicMetadataFetchTimeout = 3 * time.Minute
const musicFetchCandidateLimit = 5

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

func FetchBookMetadataPreviews(ctx context.Context, search []BookSearchMatch, provider BookDetailProvider, emit Emitter) ([]BookFetchPreview, error) {
	if provider == nil {
		return nil, fmt.Errorf("book detail provider is required")
	}

	var previews []BookFetchPreview
	for _, match := range search {
		if err := ctx.Err(); err != nil {
			return previews, err
		}
		if !match.Accepted {
			continue
		}
		preview := BookFetchPreview{Key: match.Key, ProviderID: match.ProviderID, Format: match.Format}
		emit.Emit(Event{
			Event: "metadata.fetch",
			Kind:  "book",
			Data: map[string]any{
				"key":         match.Key,
				"provider_id": match.ProviderID,
				"title":       match.Title,
				"author":      match.Author,
				"year":        match.Year,
				"format":      match.Format,
			},
		})

		detail, err := provider.GetDetail(ctx, match.ProviderID, nil)
		if err != nil {
			preview.Error = err.Error()
			emit.Emit(Event{
				Event:    "metadata.fetch_failed",
				Severity: SeverityWarn,
				Kind:     "book",
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

		preview = bookFetchPreview(match, detail)
		previews = append(previews, preview)
		emit.Emit(Event{
			Event: "metadata.preview",
			Kind:  "book",
			Data: map[string]any{
				"key":          preview.Key,
				"provider_id":  preview.ProviderID,
				"title":        preview.Title,
				"author":       preview.Author,
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
	emit.Emit(Event{Event: "metadata.preview_summary", Data: map[string]any{"domain": "book", "previews": len(previews), "failures": failures}})
	return previews, nil
}

func FetchTVMetadataPreviews(ctx context.Context, search []TVSearchMatch, matches []TVMatch, provider TVDetailProvider, emit Emitter) ([]TVFetchPreview, error) {
	return fetchTVLikeMetadataPreviews(ctx, search, matches, provider, emit, "tv")
}

func FetchAnimeMetadataPreviews(ctx context.Context, search []TVSearchMatch, matches []TVMatch, provider TVDetailProvider, emit Emitter) ([]TVFetchPreview, error) {
	return fetchTVLikeMetadataPreviews(ctx, search, matches, provider, emit, "anime")
}

func FetchMusicMetadataPreviews(ctx context.Context, search []MusicSearchMatch, artists []MusicArtistPlan, provider MusicDetailProvider, emit Emitter) ([]MusicFetchPreview, error) {
	if provider == nil {
		return nil, fmt.Errorf("music detail provider is required")
	}

	artistByKey := map[string]MusicArtistPlan{}
	for _, artist := range artists {
		artistByKey[artist.Key] = artist
	}

	var accepted []MusicSearchMatch
	for _, match := range search {
		if match.Accepted {
			accepted = append(accepted, match)
		}
	}

	previews := make([]MusicFetchPreview, len(accepted))
	sem := make(chan struct{}, musicFetchConcurrency)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var runErr error
	setErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		defer errMu.Unlock()
		if runErr == nil {
			runErr = err
		}
	}

	for i, match := range accepted {
		if err := ctx.Err(); err != nil {
			return previews, err
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int, match MusicSearchMatch) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := ctx.Err(); err != nil {
				setErr(err)
				return
			}
			previews[i] = fetchOneMusicMetadataPreview(ctx, match, artistByKey[match.Key], provider, emit)
		}(i, match)
	}
	wg.Wait()
	if runErr != nil {
		return previews, runErr
	}

	failures := 0
	for _, preview := range previews {
		if preview.Error != "" {
			failures++
		}
	}
	emit.Emit(Event{Event: "metadata.preview_summary", Data: map[string]any{"domain": "music", "previews": len(previews), "failures": failures}})
	return previews, nil
}

func fetchOneMusicMetadataPreview(ctx context.Context, match MusicSearchMatch, artist MusicArtistPlan, provider MusicDetailProvider, emit Emitter) MusicFetchPreview {
	preview := MusicFetchPreview{
		Key:         match.Key,
		ProviderID:  match.ProviderID,
		Artist:      firstNonEmpty(match.Artist, artist.Artist, match.Query.Artist),
		LocalAlbums: len(artist.Albums),
		LocalTracks: countMusicArtistTracks(artist),
		ExternalIDs: cloneStringMap(match.ExternalIDs),
	}
	emit.Emit(Event{
		Event: "metadata.fetch",
		Kind:  "music",
		Data: map[string]any{
			"key":         match.Key,
			"provider_id": match.ProviderID,
			"artist":      preview.Artist,
		},
	})

	candidates := musicFetchCandidates(match, musicFetchCandidateLimit)
	if len(candidates) == 0 {
		candidates = []MusicSearchCandidate{{
			ProviderID:  match.ProviderID,
			Provider:    match.Provider,
			Artist:      firstNonEmpty(match.Artist, artist.Artist, match.Query.Artist),
			Confidence:  match.Confidence,
			ExternalIDs: match.ExternalIDs,
		}}
	}

	var best MusicFetchPreview
	var bestSet bool
	for i, candidate := range candidates {
		if candidate.ProviderID == "" {
			continue
		}
		primaryCandidate := candidate.ProviderID == match.ProviderID
		if !primaryCandidate && !musicFetchCandidateEligibleForRerank(candidate) {
			continue
		}
		candidatePreview := fetchMusicCandidatePreview(ctx, match, artist, candidate, provider, emit)
		evaluation := musicCandidateFetchEvaluation(candidate, candidatePreview)
		preview.CandidateEvaluations = append(preview.CandidateEvaluations, evaluation)
		if candidatePreview.Error != "" {
			if primaryCandidate {
				best = candidatePreview
				bestSet = true
				break
			}
			if i == 0 && !bestSet {
				best = candidatePreview
				bestSet = true
			}
			continue
		}
		if !bestSet || musicFetchPreviewBetter(candidatePreview, best, candidate, match) {
			best = candidatePreview
			bestSet = true
		}
		if i == 0 && musicFetchPreviewComplete(candidatePreview) {
			break
		}
	}

	if !bestSet {
		preview.Error = "music metadata candidates could not be fetched"
		return preview
	}
	for i := range preview.CandidateEvaluations {
		preview.CandidateEvaluations[i].Selected = preview.CandidateEvaluations[i].ProviderID == best.ProviderID
	}
	best.CandidateEvaluations = preview.CandidateEvaluations
	best.SearchProviderID = ""
	best.SelectionReason = "search_selected"
	if best.ProviderID != match.ProviderID {
		best.SearchProviderID = match.ProviderID
		best.SelectionReason = "discography_reranked"
		emit.Emit(Event{
			Event:    "metadata.selection_replaced",
			Severity: SeverityWarn,
			Kind:     "music",
			Reason:   "discography_reranked",
			Data: map[string]any{
				"key":                  match.Key,
				"previous_provider_id": match.ProviderID,
				"provider_id":          best.ProviderID,
				"artist":               best.Artist,
				"mapped_albums":        best.MappedAlbums,
				"local_albums":         best.LocalAlbums,
				"mapped_tracks":        best.MappedTracks,
				"local_tracks":         best.LocalTracks,
			},
		})
	}
	preview = best
	emit.Emit(Event{
		Event: "metadata.preview",
		Kind:  "music",
		Data: map[string]any{
			"key":           preview.Key,
			"provider_id":   preview.ProviderID,
			"artist":        preview.Artist,
			"external_ids":  preview.ExternalIDs,
			"local_albums":  preview.LocalAlbums,
			"mapped_albums": preview.MappedAlbums,
			"local_tracks":  preview.LocalTracks,
			"mapped_tracks": preview.MappedTracks,
			"issues":        len(preview.Issues),
		},
	})
	return preview
}

func fetchMusicCandidatePreview(ctx context.Context, match MusicSearchMatch, artist MusicArtistPlan, candidate MusicSearchCandidate, provider MusicDetailProvider, emit Emitter) MusicFetchPreview {
	fetchCtx, cancel := context.WithTimeout(ctx, musicMetadataFetchTimeout)
	detail, err := provider.GetDetail(fetchCtx, candidate.ProviderID, nil)
	cancel()
	if err != nil {
		emit.Emit(Event{
			Event:    "metadata.fetch_failed",
			Severity: SeverityWarn,
			Kind:     "music",
			Reason:   "detail_fetch_failed",
			Message:  err.Error(),
			Data: map[string]any{
				"key":         match.Key,
				"provider_id": candidate.ProviderID,
			},
		})
		return MusicFetchPreview{
			Key:         match.Key,
			ProviderID:  candidate.ProviderID,
			Artist:      firstNonEmpty(candidate.Artist, match.Artist, artist.Artist, match.Query.Artist),
			LocalAlbums: len(artist.Albums),
			LocalTracks: countMusicArtistTracks(artist),
			ExternalIDs: cloneStringMap(candidate.ExternalIDs),
			Error:       err.Error(),
		}
	}
	candidateMatch := match
	candidateMatch.ProviderID = candidate.ProviderID
	candidateMatch.Artist = firstNonEmpty(candidate.Artist, match.Artist)
	candidateMatch.ExternalIDs = candidate.ExternalIDs
	return musicFetchPreview(candidateMatch, artist, detail)
}

func musicFetchCandidates(match MusicSearchMatch, limit int) []MusicSearchCandidate {
	byProvider := map[string]MusicSearchCandidate{}
	add := func(candidate MusicSearchCandidate) {
		if strings.TrimSpace(candidate.ProviderID) == "" {
			return
		}
		if _, ok := byProvider[candidate.ProviderID]; ok {
			return
		}
		byProvider[candidate.ProviderID] = candidate
	}
	add(MusicSearchCandidate{
		ProviderID:  match.ProviderID,
		Provider:    match.Provider,
		Artist:      match.Artist,
		Confidence:  match.Confidence,
		ExternalIDs: match.ExternalIDs,
	})
	for _, candidate := range match.Candidates {
		add(candidate)
	}

	candidates := make([]MusicSearchCandidate, 0, len(byProvider))
	for _, candidate := range byProvider {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		iPrimary := candidates[i].ProviderID == match.ProviderID
		jPrimary := candidates[j].ProviderID == match.ProviderID
		if iPrimary != jPrimary {
			return iPrimary
		}
		iExact := strings.EqualFold(strings.TrimSpace(candidates[i].Artist), strings.TrimSpace(match.Query.Artist))
		jExact := strings.EqualFold(strings.TrimSpace(candidates[j].Artist), strings.TrimSpace(match.Query.Artist))
		if iExact != jExact {
			return iExact
		}
		iCase := strings.TrimSpace(candidates[i].Artist) == strings.TrimSpace(match.Query.Artist)
		jCase := strings.TrimSpace(candidates[j].Artist) == strings.TrimSpace(match.Query.Artist)
		if iCase != jCase {
			return iCase
		}
		if iCount, jCount := len(candidates[i].ExternalIDs), len(candidates[j].ExternalIDs); iCount != jCount {
			return iCount > jCount
		}
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		if rankI, rankJ := musicCandidateProviderRank(candidates[i]), musicCandidateProviderRank(candidates[j]); rankI != rankJ {
			return rankI < rankJ
		}
		return candidates[i].ProviderID < candidates[j].ProviderID
	})
	if len(candidates) > limit {
		return candidates[:limit]
	}
	return candidates
}

func musicCandidateFetchEvaluation(candidate MusicSearchCandidate, preview MusicFetchPreview) MusicCandidateFetchEvaluation {
	return MusicCandidateFetchEvaluation{
		ProviderID:   candidate.ProviderID,
		Artist:       firstNonEmpty(preview.Artist, candidate.Artist),
		Confidence:   candidate.Confidence,
		MappedAlbums: preview.MappedAlbums,
		LocalAlbums:  preview.LocalAlbums,
		MappedTracks: preview.MappedTracks,
		LocalTracks:  preview.LocalTracks,
		Error:        preview.Error,
	}
}

func musicFetchPreviewComplete(preview MusicFetchPreview) bool {
	return preview.Error == "" &&
		preview.LocalAlbums > 0 &&
		preview.MappedAlbums == preview.LocalAlbums &&
		preview.LocalTracks > 0 &&
		preview.MappedTracks == preview.LocalTracks
}

func musicFetchPreviewBetter(candidate, current MusicFetchPreview, searchCandidate MusicSearchCandidate, searchMatch MusicSearchMatch) bool {
	if candidate.Error != "" {
		return false
	}
	if candidate.ProviderID != searchMatch.ProviderID && !musicFetchCandidateEligibleForRerank(searchCandidate) {
		return false
	}
	if current.Error != "" {
		return musicFetchPreviewHasLocalCoverage(candidate)
	}
	if candidate.MappedAlbums != current.MappedAlbums {
		return candidate.MappedAlbums > current.MappedAlbums
	}
	if candidate.MappedTracks != current.MappedTracks {
		return candidate.MappedTracks > current.MappedTracks
	}
	return false
}

func musicFetchCandidateEligibleForRerank(candidate MusicSearchCandidate) bool {
	return candidate.Confidence >= musicArtistAutoMatchThreshold
}

func musicFetchPreviewHasLocalCoverage(preview MusicFetchPreview) bool {
	if preview.LocalAlbums == 0 && preview.LocalTracks == 0 {
		return preview.Error == ""
	}
	return preview.MappedAlbums > 0 || preview.MappedTracks > 0
}

func musicCandidateProviderRank(candidate MusicSearchCandidate) int {
	if len(candidate.ExternalIDs) > 0 {
		return musicExternalIDsProviderRank(candidate.ExternalIDs)
	}
	return musicProviderRank(musicSearchProviderFromID(candidate.ProviderID))
}

func fetchTVLikeMetadataPreviews(ctx context.Context, search []TVSearchMatch, matches []TVMatch, provider TVDetailProvider, emit Emitter, domain string) ([]TVFetchPreview, error) {
	if provider == nil {
		return nil, fmt.Errorf("%s detail provider is required", domain)
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
			Kind:  domain,
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
				Kind:     domain,
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
			Kind:  domain,
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
	emit.Emit(Event{Event: "metadata.preview_summary", Data: map[string]any{"domain": domain, "previews": len(previews), "failures": failures}})
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

func bookFetchPreview(match BookSearchMatch, detail *metadata.MediaDetail) BookFetchPreview {
	if detail == nil {
		return BookFetchPreview{Key: match.Key, ProviderID: match.ProviderID, Format: match.Format, Error: "empty detail"}
	}
	return BookFetchPreview{
		Key:         match.Key,
		ProviderID:  match.ProviderID,
		Title:       firstNonEmpty(detail.Title, match.Title),
		Author:      firstNonEmpty(detail.AuthorName, match.Author),
		Year:        firstNonEmpty(detail.Year, match.Year),
		Format:      match.Format,
		ExternalIDs: detail.ExternalIDs,
		HeyaSlug:    detail.HeyaSlug,
		PosterURL:   detail.PosterURL,
		PageCount:   detail.PageCount,
		Publisher:   detail.Publisher,
		PublishDate: detail.PublishDate,
		Language:    detail.Language,
		Subjects:    detail.Subjects,
		Issues:      bookFetchIssues(match, detail),
		WouldApply:  bookWouldApply(detail),
		Detail:      detail,
	}
}

func bookFetchIssues(match BookSearchMatch, detail *metadata.MediaDetail) []string {
	if detail == nil {
		return nil
	}
	var issues []string
	if match.Query.Title != "" && detail.Title != "" && !bookTitleAcceptable(match.Query.Title, detail.Title) {
		issues = append(issues, fmt.Sprintf("title_mismatch remote=%q", detail.Title))
	}
	localAuthor := firstNonEmpty(match.Query.Author, match.Author)
	if localAuthor != "" && detail.AuthorName != "" && !bookAuthorMatches(localAuthor, detail.AuthorName) {
		issues = append(issues, fmt.Sprintf("author_mismatch remote=%q", detail.AuthorName))
	}
	if match.Query.Year != "" && detail.Year != "" && match.Query.Year != detail.Year {
		issues = append(issues, fmt.Sprintf("year_mismatch remote=%s", detail.Year))
	}
	return issues
}

func bookAuthorMatches(local, remote string) bool {
	localNorm := normalizeSearchTitle(local)
	remoteNorm := normalizeSearchTitle(remote)
	if localNorm == "" || remoteNorm == "" {
		return false
	}
	return localNorm == remoteNorm || strings.Contains(localNorm, remoteNorm) || strings.Contains(remoteNorm, localNorm)
}

func bookWouldApply(detail *metadata.MediaDetail) []string {
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
	if detail.AuthorName != "" {
		fields = append(fields, "author")
	}
	if detail.AuthorBio != "" {
		fields = append(fields, "author_bio")
	}
	if detail.ISBN != "" {
		fields = append(fields, "isbn")
	}
	if detail.PageCount > 0 {
		fields = append(fields, "page_count")
	}
	if detail.Publisher != "" {
		fields = append(fields, "publisher")
	}
	if detail.PublishDate != "" {
		fields = append(fields, "publish_date")
	}
	if len(detail.Subjects) > 0 {
		fields = append(fields, "subjects")
	}
	if detail.Language != "" {
		fields = append(fields, "language")
	}
	if detail.PosterURL != "" {
		fields = append(fields, "poster")
	}
	if detail.SeriesName != "" {
		fields = append(fields, "series")
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

func musicFetchPreview(match MusicSearchMatch, artist MusicArtistPlan, detail *metadata.MediaDetail) MusicFetchPreview {
	if detail == nil {
		return MusicFetchPreview{Key: match.Key, ProviderID: match.ProviderID, Error: "empty detail"}
	}
	preview := MusicFetchPreview{
		Key:          match.Key,
		ProviderID:   match.ProviderID,
		Artist:       firstNonEmpty(detail.ArtistName, detail.Title, match.Artist, artist.Artist),
		SortName:     detail.ArtistSortName,
		ExternalIDs:  detail.ExternalIDs,
		HeyaSlug:     detail.HeyaSlug,
		LocalAlbums:  len(artist.Albums),
		RemoteAlbums: len(detail.Albums),
		LocalTracks:  countMusicArtistTracks(artist),
		RemoteTracks: countRemoteMusicTracks(detail.Albums),
		Artwork:      len(detail.ArtistImages),
		Tags:         len(detail.ArtistTags),
		WouldApply:   musicWouldApply(detail),
		Detail:       detail,
	}
	preview.Issues = append(preview.Issues, musicArtistFetchIssues(artist, detail)...)

	for _, album := range artist.Albums {
		mapping := mapMusicAlbumFetch(album, detail.Albums)
		preview.AlbumMappings = append(preview.AlbumMappings, mapping)
		if mapping.RemoteAlbum != "" {
			preview.MappedAlbums++
		}
		preview.MappedTracks += mapping.MappedTracks
		for _, issue := range mapping.Issues {
			preview.Issues = append(preview.Issues, fmt.Sprintf("%s: %s", album.Album, issue))
		}
	}
	preview.Issues = sortedUnique(preview.Issues)
	sort.Slice(preview.AlbumMappings, func(i, j int) bool {
		if preview.AlbumMappings[i].LocalYear == preview.AlbumMappings[j].LocalYear {
			return preview.AlbumMappings[i].LocalAlbum < preview.AlbumMappings[j].LocalAlbum
		}
		return preview.AlbumMappings[i].LocalYear < preview.AlbumMappings[j].LocalYear
	})
	return preview
}

func mapMusicAlbumFetch(local MusicAlbumPlan, remoteAlbums []metadata.AlbumEntry) MusicAlbumFetchMatch {
	mapping := MusicAlbumFetchMatch{
		Key:              local.Key,
		LocalAlbum:       local.Album,
		LocalYear:        local.Year,
		LocalKind:        local.ReleaseKind,
		LocalExternalIDs: copyMusicExternalIDs(local.ExternalIDs),
		LocalTracks:      len(local.Tracks),
	}

	remote, confidence, reason, ok := findMusicRemoteAlbum(local, remoteAlbums)
	if !ok {
		mapping.Issues = append(mapping.Issues, "remote_album_not_found")
		return mapping
	}

	mapping.RemoteAlbum = remote.Title
	mapping.RemoteYear = remote.Year
	mapping.RemoteKind = normalizeMusicReleaseKind(resolveMusicAlbumType(remote.Type, remote.SecondaryTypes))
	mapping.RemoteExternalIDs = copyMusicExternalIDs(remote.ExternalIDs)
	mapping.Confidence = confidence
	mapping.Reason = reason
	mapping.RemoteTracks = len(remote.Tracks)
	if mapping.RemoteTracks == 0 && remote.TrackCount > 0 {
		mapping.RemoteTracks = remote.TrackCount
	}

	mapping.Issues = append(mapping.Issues, musicAlbumFetchIssues(local, remote, reason)...)
	for _, track := range local.Tracks {
		trackMapping := mapMusicTrackFetch(track, remote.Tracks)
		mapping.TrackMappings = append(mapping.TrackMappings, trackMapping)
		if trackMapping.Matched {
			mapping.MappedTracks++
		}
		if trackMapping.Issue != "" {
			mapping.Issues = append(mapping.Issues, fmt.Sprintf("%s: %s", track.TrackTitle, trackMapping.Issue))
		}
	}
	mapping.Issues = sortedUnique(mapping.Issues)
	sort.Slice(mapping.TrackMappings, func(i, j int) bool {
		if mapping.TrackMappings[i].LocalDisc == mapping.TrackMappings[j].LocalDisc {
			if mapping.TrackMappings[i].LocalTrack == mapping.TrackMappings[j].LocalTrack {
				return mapping.TrackMappings[i].RelPath < mapping.TrackMappings[j].RelPath
			}
			return mapping.TrackMappings[i].LocalTrack < mapping.TrackMappings[j].LocalTrack
		}
		return mapping.TrackMappings[i].LocalDisc < mapping.TrackMappings[j].LocalDisc
	})
	return mapping
}

func findMusicRemoteAlbum(local MusicAlbumPlan, remoteAlbums []metadata.AlbumEntry) (metadata.AlbumEntry, float64, string, bool) {
	for _, remote := range remoteAlbums {
		if key, ok := musicAlbumSharedExternalID(local.ExternalIDs, remote.ExternalIDs); ok {
			return remote, 1, "external_id:" + key, true
		}
	}

	bestScore := 0.0
	bestReason := ""
	var best metadata.AlbumEntry
	for _, remote := range remoteAlbums {
		score, reason := scoreMusicAlbumFetchCandidate(local, remote)
		if score > bestScore {
			bestScore = score
			bestReason = reason
			best = remote
		}
	}
	if bestScore >= 0.86 {
		return best, bestScore, bestReason, true
	}
	return metadata.AlbumEntry{}, bestScore, bestReason, false
}

func scoreMusicAlbumFetchCandidate(local MusicAlbumPlan, remote metadata.AlbumEntry) (float64, string) {
	bestTitle := musicNameSimilarity(local.Album, remote.Title)
	for _, alias := range local.Aliases {
		if score := musicNameSimilarity(alias, remote.Title); score > bestTitle {
			bestTitle = score
		}
	}
	score := bestTitle * 0.85
	reason := "title"
	if local.Year != "" && remote.Year > 0 {
		localYear := atoiDigits(local.Year)
		if localYear == remote.Year {
			score += 0.10
			reason = "title_year"
		} else if absInt(localYear-remote.Year) <= 1 {
			score += 0.05
			reason = "title_near_year"
		}
	}
	if local.ReleaseKind != "" {
		remoteKind := normalizeMusicReleaseKind(resolveMusicAlbumType(remote.Type, remote.SecondaryTypes))
		if local.ReleaseKind == remoteKind {
			score += 0.05
			if strings.Contains(reason, "year") {
				reason += "_kind"
			} else {
				reason = "title_kind"
			}
		}
	}
	if titlematch.FuzzyEqual(local.Album, remote.Title) && score < 0.90 {
		score = 0.90
	}
	if score > 1 {
		score = 1
	}
	return score, reason
}

func mapMusicTrackFetch(local MusicTrackPlan, remoteTracks []metadata.TrackDetail) MusicTrackFetchMatch {
	mapping := MusicTrackFetchMatch{
		RelPath:    local.RelPath,
		LocalTitle: local.TrackTitle,
		LocalDisc:  local.DiscNumber,
		LocalTrack: local.TrackNumber,
		Matched:    false,
		Confidence: 0,
	}
	if len(remoteTracks) == 0 {
		mapping.Issue = "remote_album_has_no_tracks"
		return mapping
	}

	remote, score, reason, ok := findMusicRemoteTrack(local, remoteTracks)
	if !ok {
		mapping.Issue = "remote_track_not_found"
		return mapping
	}

	mapping.Matched = true
	mapping.RemoteTitle = remote.Title
	mapping.RemoteDisc = remote.DiscNumber
	mapping.RemoteTrack = remote.TrackNumber
	mapping.Confidence = score
	mapping.Reason = reason
	if reason == "disc_track" && !musicLocalTrackTitleWeak(local.TrackTitle) && remote.Title != "" && musicNameSimilarity(local.TrackTitle, remote.Title) < 0.55 {
		mapping.Issue = fmt.Sprintf("track_title_mismatch remote=%q", remote.Title)
	}
	return mapping
}

func findMusicRemoteTrack(local MusicTrackPlan, remoteTracks []metadata.TrackDetail) (metadata.TrackDetail, float64, string, bool) {
	if local.TrackNumber > 0 {
		localDisc := local.DiscNumber
		if localDisc == 0 {
			localDisc = 1
		}
		for _, remote := range remoteTracks {
			remoteDisc := remote.DiscNumber
			if remoteDisc == 0 {
				remoteDisc = 1
			}
			if remoteDisc == localDisc && remote.TrackNumber == local.TrackNumber {
				if local.TrackTitle != "" && remote.Title != "" && musicNameSimilarity(local.TrackTitle, remote.Title) < 0.55 {
					if titleRemote, titleScore, ok := bestMusicRemoteTrackTitleMatch(local, remoteTracks); ok {
						return titleRemote, titleScore, "title", true
					}
				}
				return remote, 1, "disc_track", true
			}
		}
	}

	if remote, score, ok := bestMusicRemoteTrackTitleMatch(local, remoteTracks); ok {
		return remote, score, "title", true
	}
	return metadata.TrackDetail{}, 0, "title", false
}

func bestMusicRemoteTrackTitleMatch(local MusicTrackPlan, remoteTracks []metadata.TrackDetail) (metadata.TrackDetail, float64, bool) {
	bestScore := 0.0
	var best metadata.TrackDetail
	for _, remote := range remoteTracks {
		score := musicNameSimilarity(local.TrackTitle, remote.Title)
		if score > bestScore {
			bestScore = score
			best = remote
		}
	}
	if bestScore >= 0.82 {
		return best, bestScore, true
	}
	return metadata.TrackDetail{}, bestScore, false
}

func musicArtistFetchIssues(local MusicArtistPlan, detail *metadata.MediaDetail) []string {
	if detail == nil {
		return nil
	}
	return musicExternalIDConflictIssues("artist", local.ExternalIDs, detail.ExternalIDs, []musicIDCompare{
		{Local: []string{"mbid", "musicbrainz"}, Remote: []string{"mbid", "musicbrainz"}},
		{Local: []string{"apple"}, Remote: []string{"apple"}},
		{Local: []string{"discogs"}, Remote: []string{"discogs"}},
		{Local: []string{"deezer"}, Remote: []string{"deezer"}},
	})
}

func musicAlbumFetchIssues(local MusicAlbumPlan, remote metadata.AlbumEntry, reason string) []string {
	issues := musicExternalIDConflictIssues("album", local.ExternalIDs, remote.ExternalIDs, []musicIDCompare{
		{Local: []string{"musicbrainz_release_group"}, Remote: []string{"mb_release_group", "musicbrainz_release_group"}},
		{Local: []string{"musicbrainz_album"}, Remote: []string{"mb_release", "musicbrainz_album", "mbid"}},
		{Local: []string{"itunes_album"}, Remote: []string{"apple", "itunes_album"}},
		{Local: []string{"audiodb_album"}, Remote: []string{"audiodb", "audiodb_album"}},
		{Local: []string{"deezer_album"}, Remote: []string{"deezer", "deezer_album"}},
		{Local: []string{"discogs_album"}, Remote: []string{"discogs", "discogs_album"}},
	})
	if local.Year != "" && remote.Year > 0 {
		localYear := atoiDigits(local.Year)
		if localYear > 0 && localYear != remote.Year {
			issues = append(issues, fmt.Sprintf("album_year_mismatch remote=%d", remote.Year))
		}
	}
	if strings.HasPrefix(reason, "external_id:") {
		titleScore := musicNameSimilarity(local.Album, remote.Title)
		if titleScore < 0.55 {
			issues = append(issues, fmt.Sprintf("album_title_mismatch remote=%q", remote.Title))
		}
	}
	return issues
}

type musicIDCompare struct {
	Local  []string
	Remote []string
}

func musicExternalIDConflictIssues(label string, localIDs, remoteIDs map[string]string, compares []musicIDCompare) []string {
	var issues []string
	for _, compare := range compares {
		localKey, localValue := firstMusicID(localIDs, compare.Local)
		remoteKey, remoteValue := firstMusicID(remoteIDs, compare.Remote)
		if localValue == "" || remoteValue == "" || strings.EqualFold(localValue, remoteValue) {
			continue
		}
		issues = append(issues, fmt.Sprintf("%s_external_id_conflict local_%s=%s remote_%s=%s", label, localKey, localValue, remoteKey, remoteValue))
	}
	return issues
}

func musicAlbumSharedExternalID(localIDs, remoteIDs map[string]string) (string, bool) {
	for _, compare := range []musicIDCompare{
		{Local: []string{"musicbrainz_release_group"}, Remote: []string{"mb_release_group", "musicbrainz_release_group"}},
		{Local: []string{"musicbrainz_album"}, Remote: []string{"mb_release", "musicbrainz_album", "mbid"}},
		{Local: []string{"itunes_album"}, Remote: []string{"apple", "itunes_album"}},
		{Local: []string{"audiodb_album"}, Remote: []string{"audiodb", "audiodb_album"}},
		{Local: []string{"deezer_album"}, Remote: []string{"deezer", "deezer_album"}},
		{Local: []string{"discogs_album"}, Remote: []string{"discogs", "discogs_album"}},
	} {
		localKey, localValue := firstMusicID(localIDs, compare.Local)
		if localValue == "" {
			continue
		}
		remoteKey, remoteValue := firstMusicID(remoteIDs, compare.Remote)
		if remoteValue != "" && strings.EqualFold(localValue, remoteValue) {
			return localKey + "=" + remoteKey, true
		}
	}
	return "", false
}

func firstMusicID(values map[string]string, keys []string) (string, string) {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return key, value
		}
	}
	return "", ""
}

func countRemoteMusicTracks(albums []metadata.AlbumEntry) int {
	n := 0
	for _, album := range albums {
		if len(album.Tracks) > 0 {
			n += len(album.Tracks)
			continue
		}
		n += album.TrackCount
	}
	return n
}

func resolveMusicAlbumType(primary string, secondaries []string) string {
	for _, secondary := range secondaries {
		switch strings.ToLower(strings.TrimSpace(secondary)) {
		case "compilation":
			return "compilation"
		case "soundtrack":
			return "soundtrack"
		case "remix":
			return "remix"
		case "live":
			return "live"
		case "demo":
			return "demo"
		case "audio drama", "audiobook", "spokenword":
			return "other"
		}
	}
	return strings.ToLower(strings.TrimSpace(primary))
}

func musicWouldApply(detail *metadata.MediaDetail) []string {
	if detail == nil {
		return nil
	}
	var fields []string
	if firstNonEmpty(detail.ArtistName, detail.Title) != "" {
		fields = append(fields, "artist")
	}
	if detail.ArtistSortName != "" {
		fields = append(fields, "sort_name")
	}
	if detail.ArtistBio != "" || detail.Description != "" {
		fields = append(fields, "biography")
	}
	if len(detail.ExternalIDs) > 0 {
		fields = append(fields, "external_ids")
	}
	if detail.PosterURL != "" {
		fields = append(fields, "poster")
	}
	if len(detail.ArtistImages) > 0 {
		fields = append(fields, "artist_images")
	}
	if len(detail.Genres) > 0 || len(detail.ArtistTags) > 0 {
		fields = append(fields, "genres")
	}
	if len(detail.Albums) > 0 {
		fields = append(fields, "albums")
	}
	if countRemoteMusicTracks(detail.Albums) > 0 {
		fields = append(fields, "tracks")
	}
	if detail.ArtistListeners > 0 || detail.ArtistPlaycount > 0 || detail.ArtistPopularity > 0 {
		fields = append(fields, "popularity")
	}
	sort.Strings(fields)
	return fields
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
