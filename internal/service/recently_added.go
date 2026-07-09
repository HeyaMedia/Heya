package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// The recently-added rails group raw file arrivals into Plex-style entries:
// a whole new show collapses to one "new series" card, a season drop to a
// "new season" card, and a lone episode stays an episode card. Grouping is
// derived entirely from library_files.created_at — there is no separate
// activity log to maintain.
const (
	// recentFileWindow bounds how many of the newest files feed the grouper.
	// Big enough to cover a multi-season import burst, small enough that the
	// top-N index scan stays cheap.
	recentFileWindow = 500
	// recentBurstGap splits one show's (or artist's) file arrivals into
	// bursts: a gap longer than this starts a new event. 12h keeps an
	// overnight import together while a daily show still gets one card per
	// episode. The same tolerance decides "did this entity first appear with
	// this burst" — consistent because two arrivals closer than the gap
	// would have been the same burst unless the window truncated it.
	recentBurstGap = 12 * time.Hour
)

// RecentlyAddedTVEntry is one card on the home "Recently Added TV" rail.
type RecentlyAddedTVEntry struct {
	MediaItemID       int64     `json:"media_item_id"`
	MediaItemPublicID string    `json:"media_item_public_id,omitempty"`
	Title             string    `json:"title"`
	Slug              string    `json:"slug"`
	Kind              string    `json:"kind" enum:"series,season,episodes,episode" doc:"series = brand-new show, season = brand-new season, episodes = several episodes added to an existing season, episode = a single new episode"`
	SeasonNumber      int32     `json:"season_number"`
	EpisodeNumber     int32     `json:"episode_number"`
	EpisodeTitle      string    `json:"episode_title,omitempty"`
	SeasonCount       int32     `json:"season_count"`
	EpisodeCount      int32     `json:"episode_count"`
	AddedAt           time.Time `json:"added_at"`
}

type seasonEpisode struct {
	season  int32
	episode int32
}

type recentTVFile struct {
	createdAt time.Time
	episodes  []seasonEpisode
}

// ListRecentlyAddedTV builds the grouped TV rail.
func (a *App) ListRecentlyAddedTV(ctx context.Context, limit int32) ([]RecentlyAddedTVEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := sqlc.New(a.db)

	rows, err := q.ListRecentlyAddedTVFiles(ctx, recentFileWindow)
	if err != nil {
		return nil, fmt.Errorf("recent tv files: %w", err)
	}
	if len(rows) == 0 {
		return []RecentlyAddedTVEntry{}, nil
	}

	type showInfo struct {
		libraryID int64
		publicID  string
		title     string
		slug      string
		files     []recentTVFile
	}
	shows := map[int64]*showInfo{}
	showIDs := []int64{}
	for _, r := range rows {
		if !r.MediaItemID.Valid || r.SeasonNumber < 0 {
			continue
		}
		var epNums []int32
		if err := json.Unmarshal(r.EpisodeNumbers, &epNums); err != nil || len(epNums) == 0 {
			continue // season packs / extras without episode numbers can't be grouped
		}
		id := r.MediaItemID.Int64
		s := shows[id]
		if s == nil {
			s = &showInfo{libraryID: r.LibraryID, publicID: r.PublicID.String(), title: r.Title, slug: r.Slug}
			shows[id] = s
			showIDs = append(showIDs, id)
		}
		f := recentTVFile{createdAt: r.CreatedAt.Time}
		for _, e := range epNums {
			f.episodes = append(f.episodes, seasonEpisode{season: r.SeasonNumber, episode: e})
		}
		s.files = append(s.files, f)
	}
	if len(showIDs) == 0 {
		return []RecentlyAddedTVEntry{}, nil
	}

	firstRows, err := q.ListTVEpisodeFirstAdded(ctx, showIDs)
	if err != nil {
		return nil, fmt.Errorf("tv first-added: %w", err)
	}
	// Episode-level firsts, plus derived season- and show-level firsts.
	epFirst := map[int64]map[seasonEpisode]time.Time{}
	seasonFirst := map[int64]map[int32]time.Time{}
	showFirst := map[int64]time.Time{}
	for _, r := range firstRows {
		if !r.MediaItemID.Valid {
			continue
		}
		id := r.MediaItemID.Int64
		t := r.FirstAdded.Time
		if epFirst[id] == nil {
			epFirst[id] = map[seasonEpisode]time.Time{}
			seasonFirst[id] = map[int32]time.Time{}
		}
		epFirst[id][seasonEpisode{r.SeasonNumber, r.EpisodeNumber}] = t
		if cur, ok := seasonFirst[id][r.SeasonNumber]; !ok || t.Before(cur) {
			seasonFirst[id][r.SeasonNumber] = t
		}
		if cur, ok := showFirst[id]; !ok || t.Before(cur) {
			showFirst[id] = t
		}
	}

	entries := []RecentlyAddedTVEntry{}
	for id, show := range shows {
		sort.Slice(show.files, func(i, j int) bool { return show.files[i].createdAt.Before(show.files[j].createdAt) })
		for _, burst := range splitBursts(show.files) {
			burstStart := burst[0].createdAt
			threshold := burstStart.Add(-recentBurstGap)

			// Only episodes whose first-ever file landed in this burst count
			// as "added" — later files for a known episode are upgrades or
			// extra versions and shouldn't resurface it.
			newEps := map[seasonEpisode]time.Time{} // episode -> newest file time in burst
			for _, f := range burst {
				for _, ep := range f.episodes {
					if first, ok := epFirst[id][ep]; ok && first.Before(burstStart) {
						continue // existed before this burst → upgrade / extra version
					}
					if cur, ok := newEps[ep]; !ok || f.createdAt.After(cur) {
						newEps[ep] = f.createdAt
					}
				}
			}
			if len(newEps) == 0 {
				continue
			}

			if showFirst[id].After(threshold) {
				// The show itself appeared with this burst → one series card.
				seasons := map[int32]bool{}
				newest := time.Time{}
				for ep, t := range newEps {
					seasons[ep.season] = true
					if t.After(newest) {
						newest = t
					}
				}
				entries = append(entries, RecentlyAddedTVEntry{
					MediaItemID: id, MediaItemPublicID: show.publicID, Title: show.title, Slug: show.slug,
					Kind: "series", SeasonCount: int32(len(seasons)), EpisodeCount: int32(len(newEps)),
					AddedAt: newest,
				})
				continue
			}

			// Existing show → one card per season touched by the burst.
			bySeason := map[int32][]seasonEpisode{}
			newestBySeason := map[int32]time.Time{}
			for ep, t := range newEps {
				bySeason[ep.season] = append(bySeason[ep.season], ep)
				if cur, ok := newestBySeason[ep.season]; !ok || t.After(cur) {
					newestBySeason[ep.season] = t
				}
			}
			for season, eps := range bySeason {
				e := RecentlyAddedTVEntry{
					MediaItemID: id, MediaItemPublicID: show.publicID, Title: show.title, Slug: show.slug,
					SeasonNumber: season, EpisodeCount: int32(len(eps)),
					AddedAt: newestBySeason[season],
				}
				switch {
				case seasonFirst[id][season].After(threshold):
					e.Kind = "season"
				case len(eps) > 1:
					e.Kind = "episodes"
				default:
					e.Kind = "episode"
					e.EpisodeNumber = eps[0].episode
				}
				entries = append(entries, e)
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].AddedAt.After(entries[j].AddedAt) })
	if len(entries) > int(limit) {
		entries = entries[:limit]
	}

	// Episode titles only for the cards that survived the cut — a handful of
	// unique-index point lookups.
	for i := range entries {
		if entries[i].Kind != "episode" {
			continue
		}
		brief, err := q.GetTVEpisodeBrief(ctx, sqlc.GetTVEpisodeBriefParams{
			MediaItemID:   entries[i].MediaItemID,
			SeasonNumber:  entries[i].SeasonNumber,
			EpisodeNumber: entries[i].EpisodeNumber,
		})
		if err == nil {
			entries[i].EpisodeTitle = brief.Title
		}
	}

	// Preferred-language title overlay, batched like the flat rails.
	stubs := make([]sqlc.MediaItemCard, 0, len(entries))
	for _, e := range entries {
		stubs = append(stubs, sqlc.MediaItemCard{ID: e.MediaItemID, LibraryID: shows[e.MediaItemID].libraryID})
	}
	overlay := a.preferredTitleOverlay(ctx, q, stubs)
	for i := range entries {
		if t := overlay[entries[i].MediaItemID]; t != "" {
			entries[i].Title = t
		}
	}

	return entries, nil
}

// splitBursts cuts a show's chronologically-sorted file list wherever the
// gap between consecutive arrivals exceeds recentBurstGap.
func splitBursts(files []recentTVFile) [][]recentTVFile {
	bursts := [][]recentTVFile{}
	start := 0
	for i := 1; i < len(files); i++ {
		if files[i].createdAt.Sub(files[i-1].createdAt) > recentBurstGap {
			bursts = append(bursts, files[start:i])
			start = i
		}
	}
	if start < len(files) {
		bursts = append(bursts, files[start:])
	}
	return bursts
}

// RecentArtistEntry is one card on the "Recently Added Artists" rail: either
// a brand-new artist or an existing artist that just gained releases.
type RecentArtistEntry struct {
	ID               int64     `json:"id"`
	MediaItemID      int64     `json:"media_item_id"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	AlbumCount       int64     `json:"album_count"`
	TrackCount       int64     `json:"track_count"`
	Kind             string    `json:"kind" enum:"new,updated" doc:"new = artist first appeared with this event, updated = new releases were added to an existing artist"`
	NewAlbumCount    int32     `json:"new_album_count"`
	LatestAlbumTitle string    `json:"latest_album_title,omitempty"`
	LatestAlbumSlug  string    `json:"latest_album_slug,omitempty"`
	AddedAt          time.Time `json:"added_at"`
}

type recentMusicFile struct {
	createdAt  time.Time
	albumID    int64
	albumTitle string
	albumSlug  string
}

// listRecentArtistEvents derives the artists rail from music file arrivals.
// One entry per artist (their newest burst), classified new vs updated.
func (a *App) listRecentArtistEvents(ctx context.Context, q *sqlc.Queries, limit int32) ([]RecentArtistEntry, error) {
	rows, err := q.ListRecentlyAddedMusicFiles(ctx, recentFileWindow*4)
	if err != nil {
		return nil, fmt.Errorf("recent music files: %w", err)
	}
	if len(rows) == 0 {
		return []RecentArtistEntry{}, nil
	}

	type artistAgg struct {
		mediaItemID int64
		files       []recentMusicFile
	}
	artists := map[int64]*artistAgg{}
	artistIDs := []int64{}
	mediaItemIDs := []int64{}
	albumIDSet := map[int64]bool{}
	for _, r := range rows {
		if !r.MediaItemID.Valid {
			continue
		}
		ag := artists[r.ArtistID]
		if ag == nil {
			ag = &artistAgg{mediaItemID: r.MediaItemID.Int64}
			artists[r.ArtistID] = ag
			artistIDs = append(artistIDs, r.ArtistID)
			mediaItemIDs = append(mediaItemIDs, r.MediaItemID.Int64)
		}
		ag.files = append(ag.files, recentMusicFile{
			createdAt: r.CreatedAt.Time, albumID: r.AlbumID,
			albumTitle: r.AlbumTitle, albumSlug: r.AlbumSlug,
		})
		albumIDSet[r.AlbumID] = true
	}
	if len(artistIDs) == 0 {
		return []RecentArtistEntry{}, nil
	}
	albumIDs := make([]int64, 0, len(albumIDSet))
	for id := range albumIDSet {
		albumIDs = append(albumIDs, id)
	}

	artistFirstRows, err := q.ListArtistFirstAdded(ctx, mediaItemIDs)
	if err != nil {
		return nil, fmt.Errorf("artist first-added: %w", err)
	}
	artistFirst := map[int64]time.Time{} // by media_item_id
	for _, r := range artistFirstRows {
		if r.MediaItemID.Valid {
			artistFirst[r.MediaItemID.Int64] = r.FirstAdded.Time
		}
	}
	albumFirstRows, err := q.ListAlbumFirstAdded(ctx, albumIDs)
	if err != nil {
		return nil, fmt.Errorf("album first-added: %w", err)
	}
	albumFirst := map[int64]time.Time{}
	for _, r := range albumFirstRows {
		albumFirst[r.AlbumID] = r.FirstAdded.Time
	}

	entries := map[int64]RecentArtistEntry{} // by artist id
	for artistID, ag := range artists {
		sort.Slice(ag.files, func(i, j int) bool { return ag.files[i].createdAt.Before(ag.files[j].createdAt) })
		// Newest burst only — one card per artist keeps the rail readable.
		burst := ag.files
		for i := len(ag.files) - 1; i > 0; i-- {
			if ag.files[i].createdAt.Sub(ag.files[i-1].createdAt) > recentBurstGap {
				burst = ag.files[i:]
				break
			}
		}
		burstStart := burst[0].createdAt
		threshold := burstStart.Add(-recentBurstGap)

		// Albums whose first-ever file landed in this burst.
		type albumRef struct {
			title, slug string
			newest      time.Time
		}
		newAlbums := map[int64]*albumRef{}
		newest := time.Time{}
		for _, f := range burst {
			if f.createdAt.After(newest) {
				newest = f.createdAt
			}
			if first, ok := albumFirst[f.albumID]; !ok || first.Before(burstStart) {
				continue
			}
			ref := newAlbums[f.albumID]
			if ref == nil {
				ref = &albumRef{title: f.albumTitle, slug: f.albumSlug}
				newAlbums[f.albumID] = ref
			}
			if f.createdAt.After(ref.newest) {
				ref.newest = f.createdAt
			}
		}

		e := RecentArtistEntry{ID: artistID, MediaItemID: ag.mediaItemID, AddedAt: newest}
		if artistFirst[ag.mediaItemID].After(threshold) {
			e.Kind = "new"
			e.NewAlbumCount = int32(len(newAlbums))
		} else {
			if len(newAlbums) == 0 {
				continue // upgrades / re-downloads only — not an event
			}
			e.Kind = "updated"
			e.NewAlbumCount = int32(len(newAlbums))
		}
		var latest *albumRef
		for _, ref := range newAlbums {
			if latest == nil || ref.newest.After(latest.newest) {
				latest = ref
			}
		}
		if latest != nil {
			e.LatestAlbumTitle = latest.title
			e.LatestAlbumSlug = latest.slug
		}
		entries[artistID] = e
	}
	if len(entries) == 0 {
		return []RecentArtistEntry{}, nil
	}

	surfaced := make([]int64, 0, len(entries))
	for id := range entries {
		surfaced = append(surfaced, id)
	}
	briefs, err := q.ListArtistsBriefByIDs(ctx, surfaced)
	if err != nil {
		return nil, fmt.Errorf("artist briefs: %w", err)
	}
	out := make([]RecentArtistEntry, 0, len(briefs))
	for _, b := range briefs {
		e := entries[b.ID]
		e.Name = b.Name
		e.Slug = b.Slug
		e.AlbumCount = b.AlbumCount
		e.TrackCount = b.TrackCount
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AddedAt.After(out[j].AddedAt) })
	if len(out) > int(limit) {
		out = out[:limit]
	}
	return out, nil
}
