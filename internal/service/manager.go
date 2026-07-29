package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/manager/prowlarr"
	"github.com/karbowiak/heya/internal/manager/sabnzbd"
	"github.com/karbowiak/heya/internal/manager/torznab"
	"github.com/rs/zerolog/log"
)

// Heya Media Manager, phase 1: connection + policy config (indexers, download
// clients, quality profiles). API keys never leave the server — views carry
// api_key_set instead, and updates with an empty key keep the stored one.

var ErrManagerNotFound = errors.New("manager: not found")

// ErrManagerProfileInUse guards profile deletion while media items reference it.
var ErrManagerProfileInUse = errors.New("manager: quality profile is assigned to media items")

const (
	IndexerKindProwlarr = "prowlarr"
	IndexerKindTorznab  = "torznab"
	IndexerKindNewznab  = "newznab"

	DownloadClientKindSABnzbd = "sabnzbd"
)

type ManagerIndexerView struct {
	ID            int64                `json:"id"`
	Name          string               `json:"name"`
	Kind          string               `json:"kind"`
	Enabled       bool                 `json:"enabled"`
	BaseURL       string               `json:"base_url"`
	APIKeySet     bool                 `json:"api_key_set"`
	Protocol      string               `json:"protocol"`
	Priority      int32                `json:"priority"`
	Categories    []int32              `json:"categories"`
	Source        string               `json:"source"`
	ParentID      *int64               `json:"parent_id,omitempty"`
	LastTestAt    *string              `json:"last_test_at,omitempty"`
	LastTestOK    bool                 `json:"last_test_ok"`
	LastTestError string               `json:"last_test_error"`
	Children      []ManagerIndexerView `json:"children,omitempty"`
}

type ManagerPathMapping struct {
	Remote string `json:"remote"`
	Local  string `json:"local"`
}

type ManagerDownloadClientView struct {
	ID            int64                `json:"id"`
	Name          string               `json:"name"`
	Kind          string               `json:"kind"`
	Enabled       bool                 `json:"enabled"`
	Protocol      string               `json:"protocol"`
	BaseURL       string               `json:"base_url"`
	APIKeySet     bool                 `json:"api_key_set"`
	Username      string               `json:"username"`
	Category      string               `json:"category"`
	Priority      int32                `json:"priority"`
	PathMappings  []ManagerPathMapping `json:"path_mappings"`
	LastTestAt    *string              `json:"last_test_at,omitempty"`
	LastTestOK    bool                 `json:"last_test_ok"`
	LastTestError string               `json:"last_test_error"`
}

// ManagerQualityItem is one ladder rung: a single quality, or a named group
// of qualities that rank equally (set Group + Qualities, leave Quality empty).
type ManagerQualityItem struct {
	Quality   string   `json:"quality,omitempty"`
	Group     string   `json:"group,omitempty"`
	Qualities []string `json:"qualities,omitempty"`
	Allowed   bool     `json:"allowed"`
}

// ManagerFormatScore assigns a profile's score to one custom format. Only
// non-zero scores are stored.
type ManagerFormatScore struct {
	FormatID int64 `json:"format_id"`
	Score    int32 `json:"score"`
}

type ManagerQualityProfileView struct {
	ID                int64                `json:"id"`
	Name              string               `json:"name"`
	Domain            string               `json:"domain"`
	Items             []ManagerQualityItem `json:"items"`
	Cutoff            string               `json:"cutoff"`
	UpgradesEnabled   bool                 `json:"upgrades_enabled"`
	MinFormatScore    int32                `json:"min_format_score"`
	CutoffFormatScore int32                `json:"cutoff_format_score"`
	MinUpgradeScore   int32                `json:"min_upgrade_score"`
	FormatScores      []ManagerFormatScore `json:"format_scores"`
	Source            string               `json:"source"`
	InUseCount        int64                `json:"in_use_count"`
}

// ManagerTestResult is the outcome of a connection test, persisted on the row
// and returned to the caller.
type ManagerTestResult struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

func managerIndexerView(row sqlc.ManagerIndexer) ManagerIndexerView {
	view := ManagerIndexerView{
		ID:            row.ID,
		Name:          row.Name,
		Kind:          row.Kind,
		Enabled:       row.Enabled,
		BaseURL:       row.BaseUrl,
		APIKeySet:     row.ApiKey != "",
		Protocol:      row.Protocol,
		Priority:      row.Priority,
		Categories:    row.Categories,
		Source:        row.Source,
		LastTestOK:    row.LastTestOk,
		LastTestError: row.LastTestError,
	}
	if row.ParentID.Valid {
		id := row.ParentID.Int64
		view.ParentID = &id
	}
	if row.LastTestAt.Valid {
		at := row.LastTestAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		view.LastTestAt = &at
	}
	return view
}

func managerDownloadClientView(row sqlc.ManagerDownloadClient) ManagerDownloadClientView {
	view := ManagerDownloadClientView{
		ID:            row.ID,
		Name:          row.Name,
		Kind:          row.Kind,
		Enabled:       row.Enabled,
		Protocol:      row.Protocol,
		BaseURL:       row.BaseUrl,
		APIKeySet:     row.ApiKey != "",
		Username:      row.Username,
		Category:      row.Category,
		Priority:      row.Priority,
		PathMappings:  []ManagerPathMapping{},
		LastTestOK:    row.LastTestOk,
		LastTestError: row.LastTestError,
	}
	// Mapping rows were validated on write; a decode failure here would mean
	// hand-edited DB contents, surfaced as the empty default rather than a 500.
	_ = json.Unmarshal(row.PathMappings, &view.PathMappings)
	if row.LastTestAt.Valid {
		at := row.LastTestAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		view.LastTestAt = &at
	}
	return view
}

func managerQualityProfileView(row sqlc.ManagerQualityProfile, inUse int64) ManagerQualityProfileView {
	view := ManagerQualityProfileView{
		ID:                row.ID,
		Name:              row.Name,
		Domain:            row.Domain,
		Items:             []ManagerQualityItem{},
		Cutoff:            row.Cutoff,
		UpgradesEnabled:   row.UpgradesEnabled,
		MinFormatScore:    row.MinFormatScore,
		CutoffFormatScore: row.CutoffFormatScore,
		MinUpgradeScore:   row.MinUpgradeScore,
		FormatScores:      []ManagerFormatScore{},
		Source:            row.Source,
		InUseCount:        inUse,
	}
	_ = json.Unmarshal(row.Items, &view.Items)
	_ = json.Unmarshal(row.FormatScores, &view.FormatScores)
	return view
}

// notifyManagerChanged fires the thin admin-only invalidation signal the
// /manager System pages listen for. Delivery failure is inconsequential.
func (a *App) notifyManagerChanged(ctx context.Context, area string) {
	if err := eventhub.Notify(ctx, a.db, eventhub.EventManagerChanged, eventhub.ManagerChangedPayload{Area: area}); err != nil {
		log.Warn().Err(err).Str("area", area).Msg("manager change notify failed")
	}
}

func validateManagerBaseURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("base_url must be an http(s) URL")
	}
	return trimmed, nil
}

// ── Indexers ─────────────────────────────────────────────────────────────

// ListManagerIndexers returns hand-added rows and Prowlarr app rows at the top
// level, with synced per-indexer children nested under their app row.
func (a *App) ListManagerIndexers(ctx context.Context) ([]ManagerIndexerView, error) {
	rows, err := sqlc.New(a.db).ListManagerIndexers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manager indexers: %w", err)
	}
	top := make([]ManagerIndexerView, 0, len(rows))
	childrenByParent := make(map[int64][]ManagerIndexerView)
	for _, row := range rows {
		view := managerIndexerView(row)
		if view.ParentID != nil {
			childrenByParent[*view.ParentID] = append(childrenByParent[*view.ParentID], view)
			continue
		}
		top = append(top, view)
	}
	for i := range top {
		top[i].Children = childrenByParent[top[i].ID]
	}
	return top, nil
}

type ManagerIndexerInput struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Enabled    *bool   `json:"enabled,omitempty"`
	BaseURL    string  `json:"base_url"`
	APIKey     string  `json:"api_key"`
	Protocol   string  `json:"protocol"`
	Priority   *int32  `json:"priority,omitempty"`
	Categories []int32 `json:"categories"`
}

func (input *ManagerIndexerInput) normalize() error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch input.Kind {
	case IndexerKindProwlarr:
		input.Protocol = ""
	case IndexerKindTorznab, IndexerKindNewznab:
		if input.Protocol != "usenet" && input.Protocol != "torrent" {
			return fmt.Errorf("protocol must be usenet or torrent")
		}
	default:
		return fmt.Errorf("kind must be prowlarr, torznab, or newznab")
	}
	base, err := validateManagerBaseURL(input.BaseURL)
	if err != nil {
		return err
	}
	input.BaseURL = base
	if input.Categories == nil {
		input.Categories = []int32{}
	}
	return nil
}

func (a *App) CreateManagerIndexer(ctx context.Context, input ManagerIndexerInput) (ManagerIndexerView, error) {
	if err := input.normalize(); err != nil {
		return ManagerIndexerView{}, err
	}
	enabled := input.Enabled == nil || *input.Enabled
	priority := int32(25)
	if input.Priority != nil {
		priority = *input.Priority
	}
	row, err := sqlc.New(a.db).CreateManagerIndexer(ctx, sqlc.CreateManagerIndexerParams{
		Name:       input.Name,
		Kind:       input.Kind,
		Enabled:    enabled,
		BaseUrl:    input.BaseURL,
		ApiKey:     input.APIKey,
		Protocol:   input.Protocol,
		Priority:   priority,
		Categories: input.Categories,
		Source:     "",
		SourceRef:  "",
		ParentID:   pgtype.Int8{},
		Settings:   []byte("{}"),
	})
	if err != nil {
		return ManagerIndexerView{}, fmt.Errorf("create manager indexer: %w", err)
	}
	a.notifyManagerChanged(ctx, "indexers")
	return managerIndexerView(row), nil
}

func (a *App) UpdateManagerIndexer(ctx context.Context, id int64, input ManagerIndexerInput) (ManagerIndexerView, error) {
	q := sqlc.New(a.db)
	existing, err := q.GetManagerIndexer(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManagerIndexerView{}, ErrManagerNotFound
		}
		return ManagerIndexerView{}, fmt.Errorf("get manager indexer: %w", err)
	}
	// Synced children only accept the toggles that make sense to override
	// locally; identity fields stay owned by the Prowlarr sync.
	if existing.ParentID.Valid {
		input.Name = existing.Name
		input.Kind = existing.Kind
		input.BaseURL = existing.BaseUrl
		input.Protocol = existing.Protocol
	} else {
		input.Kind = existing.Kind
	}
	if err := input.normalize(); err != nil {
		return ManagerIndexerView{}, err
	}
	apiKey := input.APIKey
	if apiKey == "" {
		apiKey = existing.ApiKey
	}
	enabled := existing.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	priority := existing.Priority
	if input.Priority != nil {
		priority = *input.Priority
	}
	row, err := q.UpdateManagerIndexer(ctx, sqlc.UpdateManagerIndexerParams{
		ID:         id,
		Name:       input.Name,
		Enabled:    enabled,
		BaseUrl:    input.BaseURL,
		ApiKey:     apiKey,
		Protocol:   input.Protocol,
		Priority:   priority,
		Categories: input.Categories,
	})
	if err != nil {
		return ManagerIndexerView{}, fmt.Errorf("update manager indexer: %w", err)
	}
	a.notifyManagerChanged(ctx, "indexers")
	return managerIndexerView(row), nil
}

func (a *App) DeleteManagerIndexer(ctx context.Context, id int64) error {
	if err := sqlc.New(a.db).DeleteManagerIndexer(ctx, id); err != nil {
		return fmt.Errorf("delete manager indexer: %w", err)
	}
	a.notifyManagerChanged(ctx, "indexers")
	return nil
}

// TestManagerIndexer runs the kind-appropriate connectivity check, persists
// the outcome on the row, and — for Prowlarr app rows — syncs the per-indexer
// children on success.
func (a *App) TestManagerIndexer(ctx context.Context, id int64) (ManagerTestResult, error) {
	q := sqlc.New(a.db)
	row, err := q.GetManagerIndexer(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManagerTestResult{}, ErrManagerNotFound
		}
		return ManagerTestResult{}, fmt.Errorf("get manager indexer: %w", err)
	}

	result := ManagerTestResult{}
	switch row.Kind {
	case IndexerKindProwlarr:
		client := prowlarr.New(row.BaseUrl, row.ApiKey)
		status, err := client.SystemStatus(ctx)
		if err != nil {
			result.Error = err.Error()
			break
		}
		synced, err := a.syncProwlarrIndexers(ctx, row, client)
		if err != nil {
			result.Error = fmt.Sprintf("connected to %s %s but indexer sync failed: %v", status.AppName, status.Version, err)
			break
		}
		result.OK = true
		result.Detail = fmt.Sprintf("%s %s · %d indexers synced", status.AppName, status.Version, synced)
	case IndexerKindTorznab, IndexerKindNewznab:
		caps, err := torznab.New(row.BaseUrl, row.ApiKey).Caps(ctx)
		if err != nil {
			result.Error = err.Error()
			break
		}
		modes := make([]string, 0, len(caps.Searching))
		for _, mode := range caps.Searching {
			if mode.Available {
				modes = append(modes, mode.Name)
			}
		}
		result.OK = true
		result.Detail = fmt.Sprintf("%s · supports %s", caps.ServerTitle, strings.Join(modes, ", "))
	default:
		result.Error = fmt.Sprintf("unknown indexer kind %q", row.Kind)
	}

	if err := q.SetManagerIndexerTestResult(ctx, sqlc.SetManagerIndexerTestResultParams{
		ID:            id,
		LastTestOk:    result.OK,
		LastTestError: result.Error,
	}); err != nil {
		return result, fmt.Errorf("persist manager indexer test result: %w", err)
	}
	a.notifyManagerChanged(ctx, "indexers")
	return result, nil
}

// syncProwlarrIndexers materializes one torznab child row per Prowlarr
// indexer and prunes children Prowlarr no longer has.
func (a *App) syncProwlarrIndexers(ctx context.Context, app sqlc.ManagerIndexer, client *prowlarr.Client) (int, error) {
	indexers, err := client.Indexers(ctx)
	if err != nil {
		return 0, err
	}
	q := sqlc.New(a.db)
	keepRefs := make([]string, 0, len(indexers))
	for _, indexer := range indexers {
		ref := strconv.Itoa(indexer.ID)
		keepRefs = append(keepRefs, ref)
		_, err := q.UpsertManagerIndexerChild(ctx, sqlc.UpsertManagerIndexerChildParams{
			Name:       indexer.Name,
			Kind:       IndexerKindTorznab,
			Enabled:    indexer.Enable,
			BaseUrl:    client.TorznabURL(indexer.ID),
			ApiKey:     app.ApiKey,
			Protocol:   indexer.Protocol,
			Priority:   int32(indexer.Priority),
			Categories: []int32{},
			Source:     IndexerKindProwlarr,
			SourceRef:  ref,
			ParentID:   pgtype.Int8{Int64: app.ID, Valid: true},
		})
		if err != nil {
			return 0, fmt.Errorf("upserting indexer %q: %w", indexer.Name, err)
		}
	}
	if err := q.DeleteStaleManagerIndexerChildren(ctx, sqlc.DeleteStaleManagerIndexerChildrenParams{
		ParentID: pgtype.Int8{Int64: app.ID, Valid: true},
		KeepRefs: keepRefs,
	}); err != nil {
		return 0, fmt.Errorf("pruning stale indexers: %w", err)
	}
	return len(indexers), nil
}

// ManagerIndexerStatsView is one synced indexer's live counters, pulled from
// the Prowlarr app on demand (not persisted — Prowlarr owns this history).
type ManagerIndexerStatsView struct {
	// ID is the Heya child-row id (0 when the indexer isn't synced locally).
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Queries       int    `json:"queries"`
	RssQueries    int    `json:"rss_queries"`
	Grabs         int    `json:"grabs"`
	FailedQueries int    `json:"failed_queries"`
	FailedRss     int    `json:"failed_rss"`
	FailedGrabs   int    `json:"failed_grabs"`
	AvgResponseMs int    `json:"avg_response_ms"`
	// DisabledTill is set when Prowlarr has the indexer in failure backoff.
	DisabledTill  string `json:"disabled_till,omitempty"`
	RecentFailure string `json:"recent_failure,omitempty"`
}

// ManagerIndexerStats fetches live per-indexer counters for a Prowlarr app
// row, keyed back onto the synced child rows.
func (a *App) ManagerIndexerStats(ctx context.Context, id int64) ([]ManagerIndexerStatsView, error) {
	q := sqlc.New(a.db)
	row, err := q.GetManagerIndexer(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrManagerNotFound
		}
		return nil, fmt.Errorf("get manager indexer: %w", err)
	}
	if row.Kind != IndexerKindProwlarr {
		return []ManagerIndexerStatsView{}, nil
	}
	client := prowlarr.New(row.BaseUrl, row.ApiKey)
	stats, err := client.Stats(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching indexer stats: %w", err)
	}
	statuses, err := client.Statuses(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching indexer statuses: %w", err)
	}
	statusByID := make(map[int]prowlarr.IndexerStatus, len(statuses))
	for _, status := range statuses {
		statusByID[status.IndexerID] = status
	}
	// Map Prowlarr indexer ids back to Heya child rows via source_ref.
	rows, err := q.ListManagerIndexers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manager indexers: %w", err)
	}
	childIDByRef := make(map[string]int64)
	for _, child := range rows {
		if child.ParentID.Valid && child.ParentID.Int64 == row.ID {
			childIDByRef[child.SourceRef] = child.ID
		}
	}
	views := make([]ManagerIndexerStatsView, 0, len(stats))
	for _, stat := range stats {
		view := ManagerIndexerStatsView{
			ID:            childIDByRef[strconv.Itoa(stat.IndexerID)],
			Name:          stat.IndexerName,
			Queries:       stat.NumberOfQueries,
			RssQueries:    stat.NumberOfRssQueries,
			Grabs:         stat.NumberOfGrabs,
			FailedQueries: stat.NumberOfFailedQueries,
			FailedRss:     stat.NumberOfFailedRssQueries,
			FailedGrabs:   stat.NumberOfFailedGrabs,
			AvgResponseMs: stat.AverageResponseTime,
		}
		if status, ok := statusByID[stat.IndexerID]; ok {
			view.DisabledTill = status.DisabledTill
			view.RecentFailure = status.MostRecentFailure
		}
		views = append(views, view)
	}
	return views, nil
}

// ManagerIndexerHistoryDay is one day's bucketed activity.
type ManagerIndexerHistoryDay struct {
	Date    string `json:"date"` // YYYY-MM-DD
	Queries int    `json:"queries"`
	Failed  int    `json:"failed"`
	Grabs   int    `json:"grabs"`
}

// ManagerIndexerHistoryView is the Prowlarr query/grab log bucketed per day —
// overall and per synced child — plus who's been driving the searches.
type ManagerIndexerHistoryView struct {
	// CoveredFrom is the oldest event actually fetched (the log is paged; we
	// cap the fetch, so the window is honest rather than fixed).
	CoveredFrom string                               `json:"covered_from"`
	Days        []ManagerIndexerHistoryDay           `json:"days"`
	ByIndexer   map[int64][]ManagerIndexerHistoryDay `json:"by_indexer"`
	BySource    map[string]int                       `json:"by_source"`
}

const (
	managerHistoryMaxPages = 5
	managerHistoryPageSize = 1000
	managerHistoryMaxDays  = 30
)

// ManagerIndexerHistory fetches and buckets the Prowlarr history log for one
// app row. Read-only against Prowlarr; nothing is persisted.
func (a *App) ManagerIndexerHistory(ctx context.Context, id int64) (*ManagerIndexerHistoryView, error) {
	q := sqlc.New(a.db)
	row, err := q.GetManagerIndexer(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrManagerNotFound
		}
		return nil, fmt.Errorf("get manager indexer: %w", err)
	}
	if row.Kind != IndexerKindProwlarr {
		return &ManagerIndexerHistoryView{Days: []ManagerIndexerHistoryDay{}, ByIndexer: map[int64][]ManagerIndexerHistoryDay{}, BySource: map[string]int{}}, nil
	}

	rows, err := q.ListManagerIndexers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manager indexers: %w", err)
	}
	childIDByRef := make(map[string]int64)
	for _, child := range rows {
		if child.ParentID.Valid && child.ParentID.Int64 == row.ID {
			childIDByRef[child.SourceRef] = child.ID
		}
	}

	client := prowlarr.New(row.BaseUrl, row.ApiKey)
	cutoff := time.Now().UTC().AddDate(0, 0, -managerHistoryMaxDays)
	type dayKey struct {
		child int64
		date  string
	}
	overall := make(map[string]*ManagerIndexerHistoryDay)
	perChild := make(map[dayKey]*ManagerIndexerHistoryDay)
	bySource := make(map[string]int)
	coveredFrom := time.Now().UTC()

fetch:
	for page := 1; page <= managerHistoryMaxPages; page++ {
		records, err := client.History(ctx, page, managerHistoryPageSize)
		if err != nil {
			return nil, fmt.Errorf("fetching prowlarr history: %w", err)
		}
		if len(records) == 0 {
			break
		}
		for _, record := range records {
			at, err := time.Parse(time.RFC3339, record.Date)
			if err != nil {
				continue
			}
			if at.Before(cutoff) {
				break fetch
			}
			if at.Before(coveredFrom) {
				coveredFrom = at
			}
			date := at.Format("2006-01-02")
			childID := childIDByRef[strconv.Itoa(record.IndexerID)]
			bump := func(day *ManagerIndexerHistoryDay) {
				switch record.EventType {
				case "releaseGrabbed":
					day.Grabs++
				default:
					if record.Successful {
						day.Queries++
					} else {
						day.Failed++
					}
				}
			}
			if overall[date] == nil {
				overall[date] = &ManagerIndexerHistoryDay{Date: date}
			}
			bump(overall[date])
			key := dayKey{child: childID, date: date}
			if perChild[key] == nil {
				perChild[key] = &ManagerIndexerHistoryDay{Date: date}
			}
			bump(perChild[key])
			if record.EventType != "releaseGrabbed" && record.Data.Source != "" {
				bySource[record.Data.Source]++
			}
		}
		if len(records) < managerHistoryPageSize {
			break
		}
	}

	view := &ManagerIndexerHistoryView{
		CoveredFrom: coveredFrom.Format("2006-01-02"),
		Days:        make([]ManagerIndexerHistoryDay, 0, len(overall)),
		ByIndexer:   make(map[int64][]ManagerIndexerHistoryDay),
		BySource:    bySource,
	}
	// Emit a contiguous day axis from coveredFrom to today so charts show
	// quiet days as gaps-with-zero instead of skipping them.
	for cursor := coveredFrom; !cursor.After(time.Now().UTC()); cursor = cursor.AddDate(0, 0, 1) {
		date := cursor.Format("2006-01-02")
		if day := overall[date]; day != nil {
			view.Days = append(view.Days, *day)
		} else {
			view.Days = append(view.Days, ManagerIndexerHistoryDay{Date: date})
		}
		for _, childID := range childIDByRef {
			day := perChild[dayKey{child: childID, date: date}]
			if day == nil {
				day = &ManagerIndexerHistoryDay{Date: date}
			}
			view.ByIndexer[childID] = append(view.ByIndexer[childID], *day)
		}
	}
	return view, nil
}

// ── Download clients ─────────────────────────────────────────────────────

func (a *App) ListManagerDownloadClients(ctx context.Context) ([]ManagerDownloadClientView, error) {
	rows, err := sqlc.New(a.db).ListManagerDownloadClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manager download clients: %w", err)
	}
	views := make([]ManagerDownloadClientView, 0, len(rows))
	for _, row := range rows {
		views = append(views, managerDownloadClientView(row))
	}
	return views, nil
}

type ManagerDownloadClientInput struct {
	Name         string               `json:"name"`
	Kind         string               `json:"kind"`
	Enabled      *bool                `json:"enabled,omitempty"`
	BaseURL      string               `json:"base_url"`
	APIKey       string               `json:"api_key"`
	Username     string               `json:"username"`
	Password     string               `json:"password"`
	Category     string               `json:"category"`
	Priority     *int32               `json:"priority,omitempty"`
	PathMappings []ManagerPathMapping `json:"path_mappings"`
}

func (input *ManagerDownloadClientInput) normalize() (protocol string, err error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	switch input.Kind {
	case DownloadClientKindSABnzbd:
		protocol = "usenet"
	default:
		return "", fmt.Errorf("kind must be sabnzbd")
	}
	base, err := validateManagerBaseURL(input.BaseURL)
	if err != nil {
		return "", err
	}
	input.BaseURL = base
	if strings.TrimSpace(input.Category) == "" {
		input.Category = "heya"
	}
	if input.PathMappings == nil {
		input.PathMappings = []ManagerPathMapping{}
	}
	for i, mapping := range input.PathMappings {
		input.PathMappings[i].Remote = strings.TrimRight(strings.TrimSpace(mapping.Remote), "/")
		input.PathMappings[i].Local = strings.TrimRight(strings.TrimSpace(mapping.Local), "/")
		if input.PathMappings[i].Remote == "" || input.PathMappings[i].Local == "" {
			return "", fmt.Errorf("path mappings need both remote and local")
		}
	}
	return protocol, nil
}

func (a *App) CreateManagerDownloadClient(ctx context.Context, input ManagerDownloadClientInput) (ManagerDownloadClientView, error) {
	protocol, err := input.normalize()
	if err != nil {
		return ManagerDownloadClientView{}, err
	}
	enabled := input.Enabled == nil || *input.Enabled
	priority := int32(1)
	if input.Priority != nil {
		priority = *input.Priority
	}
	mappings, err := json.Marshal(input.PathMappings)
	if err != nil {
		return ManagerDownloadClientView{}, fmt.Errorf("encoding path mappings: %w", err)
	}
	row, err := sqlc.New(a.db).CreateManagerDownloadClient(ctx, sqlc.CreateManagerDownloadClientParams{
		Name:         input.Name,
		Kind:         input.Kind,
		Enabled:      enabled,
		Protocol:     protocol,
		BaseUrl:      input.BaseURL,
		ApiKey:       input.APIKey,
		Username:     input.Username,
		Password:     input.Password,
		Category:     input.Category,
		Priority:     priority,
		PathMappings: mappings,
		Settings:     []byte("{}"),
	})
	if err != nil {
		return ManagerDownloadClientView{}, fmt.Errorf("create manager download client: %w", err)
	}
	a.notifyManagerChanged(ctx, "download_clients")
	return managerDownloadClientView(row), nil
}

func (a *App) UpdateManagerDownloadClient(ctx context.Context, id int64, input ManagerDownloadClientInput) (ManagerDownloadClientView, error) {
	q := sqlc.New(a.db)
	existing, err := q.GetManagerDownloadClient(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManagerDownloadClientView{}, ErrManagerNotFound
		}
		return ManagerDownloadClientView{}, fmt.Errorf("get manager download client: %w", err)
	}
	input.Kind = existing.Kind
	if _, err := input.normalize(); err != nil {
		return ManagerDownloadClientView{}, err
	}
	apiKey := input.APIKey
	if apiKey == "" {
		apiKey = existing.ApiKey
	}
	password := input.Password
	if password == "" {
		password = existing.Password
	}
	enabled := existing.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	priority := existing.Priority
	if input.Priority != nil {
		priority = *input.Priority
	}
	mappings, err := json.Marshal(input.PathMappings)
	if err != nil {
		return ManagerDownloadClientView{}, fmt.Errorf("encoding path mappings: %w", err)
	}
	row, err := q.UpdateManagerDownloadClient(ctx, sqlc.UpdateManagerDownloadClientParams{
		ID:           id,
		Name:         input.Name,
		Enabled:      enabled,
		BaseUrl:      input.BaseURL,
		ApiKey:       apiKey,
		Username:     input.Username,
		Password:     password,
		Category:     input.Category,
		Priority:     priority,
		PathMappings: mappings,
	})
	if err != nil {
		return ManagerDownloadClientView{}, fmt.Errorf("update manager download client: %w", err)
	}
	a.notifyManagerChanged(ctx, "download_clients")
	return managerDownloadClientView(row), nil
}

func (a *App) DeleteManagerDownloadClient(ctx context.Context, id int64) error {
	if err := sqlc.New(a.db).DeleteManagerDownloadClient(ctx, id); err != nil {
		return fmt.Errorf("delete manager download client: %w", err)
	}
	a.notifyManagerChanged(ctx, "download_clients")
	return nil
}

func (a *App) TestManagerDownloadClient(ctx context.Context, id int64) (ManagerTestResult, error) {
	q := sqlc.New(a.db)
	row, err := q.GetManagerDownloadClient(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManagerTestResult{}, ErrManagerNotFound
		}
		return ManagerTestResult{}, fmt.Errorf("get manager download client: %w", err)
	}

	result := ManagerTestResult{}
	switch row.Kind {
	case DownloadClientKindSABnzbd:
		client := sabnzbd.New(row.BaseUrl, row.ApiKey)
		version, err := client.Version(ctx)
		if err != nil {
			result.Error = err.Error()
			break
		}
		// Queue is the authenticated call — version alone answers on bad keys.
		queue, err := client.Queue(ctx)
		if err != nil {
			result.Error = err.Error()
			break
		}
		result.OK = true
		result.Detail = fmt.Sprintf("SABnzbd %s · %d in queue", version, len(queue.Slots))
	default:
		result.Error = fmt.Sprintf("unknown download client kind %q", row.Kind)
	}

	if err := q.SetManagerDownloadClientTestResult(ctx, sqlc.SetManagerDownloadClientTestResultParams{
		ID:            id,
		LastTestOk:    result.OK,
		LastTestError: result.Error,
	}); err != nil {
		return result, fmt.Errorf("persist manager download client test result: %w", err)
	}
	a.notifyManagerChanged(ctx, "download_clients")
	return result, nil
}

// ManagerClientQueueItem is one in-flight download, normalized from the
// client's own shapes (SAB reports sizes as decimal-string MB).
type ManagerClientQueueItem struct {
	Name       string  `json:"name"`
	Category   string  `json:"category"`
	Status     string  `json:"status"`
	SizeMB     float64 `json:"size_mb"`
	SizeLeftMB float64 `json:"size_left_mb"`
	Percentage int     `json:"percentage"`
	TimeLeft   string  `json:"time_left"`
}

type ManagerClientHistoryItem struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Category    string `json:"category"`
	Bytes       int64  `json:"bytes"`
	Storage     string `json:"storage"`
	FailMessage string `json:"fail_message,omitempty"`
	CompletedAt int64  `json:"completed_at"`
}

// ManagerClientCategory is one download category with its remote landing dir
// and — when a path mapping covers it — the path this process would read.
type ManagerClientCategory struct {
	Name      string `json:"name"`
	RemoteDir string `json:"remote_dir"`
	LocalDir  string `json:"local_dir,omitempty"`
	Priority  int    `json:"priority"`
	Script    string `json:"script,omitempty"`
}

type ManagerClientWarning struct {
	Text string `json:"text"`
	At   int64  `json:"at"`
}

// ManagerClientActivityView is the live picture of what a download client is
// doing: current queue, recent history, categories, and transfer counters.
type ManagerClientActivityView struct {
	Paused          bool                       `json:"paused"`
	SpeedKBps       float64                    `json:"speed_kbps"`
	DiskFreeGB      float64                    `json:"disk_free_gb"`
	Queue           []ManagerClientQueueItem   `json:"queue"`
	History         []ManagerClientHistoryItem `json:"history"`
	Categories      []ManagerClientCategory    `json:"categories"`
	Warnings        []ManagerClientWarning     `json:"warnings"`
	DownloadedDay   int64                      `json:"downloaded_day"`
	DownloadedWeek  int64                      `json:"downloaded_week"`
	DownloadedMonth int64                      `json:"downloaded_month"`
	DownloadedTotal int64                      `json:"downloaded_total"`
}

// applyPathMappings rewrites a client-reported path into this process's view
// via the client row's mappings; empty when no mapping covers it.
func applyPathMappings(mappings []ManagerPathMapping, remote string) string {
	for _, mapping := range mappings {
		if remote == mapping.Remote || strings.HasPrefix(remote, mapping.Remote+"/") {
			return mapping.Local + strings.TrimPrefix(remote, mapping.Remote)
		}
	}
	return ""
}

func parseSABFloat(raw string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value
}

// ManagerDownloadClientActivity fetches the client's live queue, recent
// history, and transfer totals. Read-only against the client — nothing is
// persisted.
func (a *App) ManagerDownloadClientActivity(ctx context.Context, id int64) (*ManagerClientActivityView, error) {
	row, err := sqlc.New(a.db).GetManagerDownloadClient(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrManagerNotFound
		}
		return nil, fmt.Errorf("get manager download client: %w", err)
	}
	if row.Kind != DownloadClientKindSABnzbd {
		return nil, fmt.Errorf("activity not supported for client kind %q", row.Kind)
	}

	client := sabnzbd.New(row.BaseUrl, row.ApiKey)
	queue, err := client.Queue(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching queue: %w", err)
	}
	stats, err := client.ServerStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching server stats: %w", err)
	}
	history, err := client.History(ctx, 10)
	if err != nil {
		return nil, fmt.Errorf("fetching history: %w", err)
	}
	config, err := client.Config(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching config: %w", err)
	}
	warnings, err := client.Warnings(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching warnings: %w", err)
	}

	var mappings []ManagerPathMapping
	_ = json.Unmarshal(row.PathMappings, &mappings)

	view := &ManagerClientActivityView{
		Paused:          queue.Paused,
		SpeedKBps:       parseSABFloat(queue.SpeedKBps),
		DiskFreeGB:      parseSABFloat(queue.DiskspaceFreeGB),
		Queue:           make([]ManagerClientQueueItem, 0, len(queue.Slots)),
		History:         make([]ManagerClientHistoryItem, 0, len(history)),
		Categories:      make([]ManagerClientCategory, 0, len(config.Categories)),
		Warnings:        []ManagerClientWarning{},
		DownloadedDay:   stats.Day,
		DownloadedWeek:  stats.Week,
		DownloadedMonth: stats.Month,
		DownloadedTotal: stats.Total,
	}
	for _, category := range config.Categories {
		remote := category.Dir
		if remote == "" {
			remote = config.CompleteDir
		} else if !strings.HasPrefix(remote, "/") {
			remote = strings.TrimRight(config.CompleteDir, "/") + "/" + remote
		}
		view.Categories = append(view.Categories, ManagerClientCategory{
			Name:      category.Name,
			RemoteDir: remote,
			LocalDir:  applyPathMappings(mappings, remote),
			Priority:  category.Priority,
			Script:    category.Script,
		})
	}
	// Newest first, capped — the card shows problems, not the whole log.
	for i := len(warnings) - 1; i >= 0 && len(view.Warnings) < 5; i-- {
		view.Warnings = append(view.Warnings, ManagerClientWarning{Text: warnings[i].Text, At: warnings[i].Time})
	}
	for _, slot := range queue.Slots {
		view.Queue = append(view.Queue, ManagerClientQueueItem{
			Name:       slot.Filename,
			Category:   slot.Category,
			Status:     slot.Status,
			SizeMB:     parseSABFloat(slot.SizeMB),
			SizeLeftMB: parseSABFloat(slot.SizeLeftMB),
			Percentage: int(parseSABFloat(slot.Percentage)),
			TimeLeft:   slot.TimeLeft,
		})
	}
	for _, slot := range history {
		view.History = append(view.History, ManagerClientHistoryItem{
			Name:        slot.Name,
			Status:      slot.Status,
			Category:    slot.Category,
			Bytes:       slot.Bytes,
			Storage:     slot.Storage,
			FailMessage: slot.FailMessage,
			CompletedAt: slot.Completed,
		})
	}
	return view, nil
}

// ── Quality profiles ─────────────────────────────────────────────────────

func (a *App) ListManagerQualityProfiles(ctx context.Context) ([]ManagerQualityProfileView, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListManagerQualityProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manager quality profiles: %w", err)
	}
	views := make([]ManagerQualityProfileView, 0, len(rows))
	for _, row := range rows {
		inUse, err := q.CountMediaItemsByQualityProfile(ctx, pgtype.Int8{Int64: row.ID, Valid: true})
		if err != nil {
			return nil, fmt.Errorf("count profile assignments: %w", err)
		}
		views = append(views, managerQualityProfileView(row, inUse))
	}
	return views, nil
}

type ManagerQualityProfileInput struct {
	Name              string               `json:"name"`
	Domain            string               `json:"domain"`
	Items             []ManagerQualityItem `json:"items"`
	Cutoff            string               `json:"cutoff"`
	UpgradesEnabled   *bool                `json:"upgrades_enabled,omitempty"`
	MinFormatScore    *int32               `json:"min_format_score,omitempty"`
	CutoffFormatScore *int32               `json:"cutoff_format_score,omitempty"`
	MinUpgradeScore   *int32               `json:"min_upgrade_score,omitempty"`
	// nil keeps the stored scores on update; an empty list clears them.
	FormatScores []ManagerFormatScore `json:"format_scores,omitempty"`
}

func (input *ManagerQualityProfileInput) normalize() error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch input.Domain {
	case "video", "music", "book":
	default:
		return fmt.Errorf("domain must be video, music, or book")
	}
	if len(input.Items) == 0 {
		return fmt.Errorf("at least one quality is required")
	}
	anyAllowed := false
	cutoffKnown := false
	for _, item := range input.Items {
		isGroup := item.Group != "" && len(item.Qualities) > 0
		if !isGroup && item.Quality == "" {
			return fmt.Errorf("ladder items need a quality key or a group with members")
		}
		if item.Allowed {
			anyAllowed = true
		}
		if (item.Quality != "" && item.Quality == input.Cutoff) || (item.Group != "" && item.Group == input.Cutoff) {
			cutoffKnown = true
		}
	}
	if !anyAllowed {
		return fmt.Errorf("at least one quality must be allowed")
	}
	if input.Cutoff == "" || !cutoffKnown {
		return fmt.Errorf("cutoff must be one of the profile's qualities")
	}
	return nil
}

func (a *App) CreateManagerQualityProfile(ctx context.Context, input ManagerQualityProfileInput) (ManagerQualityProfileView, error) {
	if err := input.normalize(); err != nil {
		return ManagerQualityProfileView{}, err
	}
	items, err := json.Marshal(input.Items)
	if err != nil {
		return ManagerQualityProfileView{}, fmt.Errorf("encoding profile items: %w", err)
	}
	upgrades := input.UpgradesEnabled == nil || *input.UpgradesEnabled
	scores, err := json.Marshal(nonZeroFormatScores(input.FormatScores))
	if err != nil {
		return ManagerQualityProfileView{}, fmt.Errorf("encoding format scores: %w", err)
	}
	row, err := sqlc.New(a.db).CreateManagerQualityProfile(ctx, sqlc.CreateManagerQualityProfileParams{
		Name:              input.Name,
		Domain:            input.Domain,
		Items:             items,
		Cutoff:            input.Cutoff,
		UpgradesEnabled:   upgrades,
		MinFormatScore:    valueOrZero(input.MinFormatScore),
		CutoffFormatScore: valueOrZero(input.CutoffFormatScore),
		MinUpgradeScore:   valueOr(input.MinUpgradeScore, 1),
		FormatScores:      scores,
		Source:            "",
	})
	if err != nil {
		return ManagerQualityProfileView{}, fmt.Errorf("create manager quality profile: %w", err)
	}
	a.notifyManagerChanged(ctx, "quality_profiles")
	return managerQualityProfileView(row, 0), nil
}

func (a *App) UpdateManagerQualityProfile(ctx context.Context, id int64, input ManagerQualityProfileInput) (ManagerQualityProfileView, error) {
	q := sqlc.New(a.db)
	existing, err := q.GetManagerQualityProfile(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManagerQualityProfileView{}, ErrManagerNotFound
		}
		return ManagerQualityProfileView{}, fmt.Errorf("get manager quality profile: %w", err)
	}
	input.Domain = existing.Domain
	if err := input.normalize(); err != nil {
		return ManagerQualityProfileView{}, err
	}
	items, err := json.Marshal(input.Items)
	if err != nil {
		return ManagerQualityProfileView{}, fmt.Errorf("encoding profile items: %w", err)
	}
	upgrades := existing.UpgradesEnabled
	if input.UpgradesEnabled != nil {
		upgrades = *input.UpgradesEnabled
	}
	scoresJSON := existing.FormatScores
	if input.FormatScores != nil {
		scoresJSON, err = json.Marshal(nonZeroFormatScores(input.FormatScores))
		if err != nil {
			return ManagerQualityProfileView{}, fmt.Errorf("encoding format scores: %w", err)
		}
	}
	row, err := q.UpdateManagerQualityProfile(ctx, sqlc.UpdateManagerQualityProfileParams{
		ID:                id,
		Name:              input.Name,
		Items:             items,
		Cutoff:            input.Cutoff,
		UpgradesEnabled:   upgrades,
		MinFormatScore:    valueOr(input.MinFormatScore, existing.MinFormatScore),
		CutoffFormatScore: valueOr(input.CutoffFormatScore, existing.CutoffFormatScore),
		MinUpgradeScore:   valueOr(input.MinUpgradeScore, existing.MinUpgradeScore),
		FormatScores:      scoresJSON,
	})
	if err != nil {
		return ManagerQualityProfileView{}, fmt.Errorf("update manager quality profile: %w", err)
	}
	inUse, err := q.CountMediaItemsByQualityProfile(ctx, pgtype.Int8{Int64: id, Valid: true})
	if err != nil {
		return ManagerQualityProfileView{}, fmt.Errorf("count profile assignments: %w", err)
	}
	a.notifyManagerChanged(ctx, "quality_profiles")
	return managerQualityProfileView(row, inUse), nil
}

func (a *App) DeleteManagerQualityProfile(ctx context.Context, id int64) error {
	q := sqlc.New(a.db)
	inUse, err := q.CountMediaItemsByQualityProfile(ctx, pgtype.Int8{Int64: id, Valid: true})
	if err != nil {
		return fmt.Errorf("count profile assignments: %w", err)
	}
	if inUse > 0 {
		return ErrManagerProfileInUse
	}
	if err := q.DeleteManagerQualityProfile(ctx, id); err != nil {
		return fmt.Errorf("delete manager quality profile: %w", err)
	}
	a.notifyManagerChanged(ctx, "quality_profiles")
	return nil
}
