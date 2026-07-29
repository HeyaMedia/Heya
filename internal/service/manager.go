package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

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

type ManagerQualityItem struct {
	Quality string `json:"quality"`
	Allowed bool   `json:"allowed"`
}

type ManagerQualityProfileView struct {
	ID              int64                `json:"id"`
	Name            string               `json:"name"`
	Domain          string               `json:"domain"`
	Items           []ManagerQualityItem `json:"items"`
	Cutoff          string               `json:"cutoff"`
	UpgradesEnabled bool                 `json:"upgrades_enabled"`
	InUseCount      int64                `json:"in_use_count"`
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
		ID:              row.ID,
		Name:            row.Name,
		Domain:          row.Domain,
		Items:           []ManagerQualityItem{},
		Cutoff:          row.Cutoff,
		UpgradesEnabled: row.UpgradesEnabled,
		InUseCount:      inUse,
	}
	_ = json.Unmarshal(row.Items, &view.Items)
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
	Name            string               `json:"name"`
	Domain          string               `json:"domain"`
	Items           []ManagerQualityItem `json:"items"`
	Cutoff          string               `json:"cutoff"`
	UpgradesEnabled *bool                `json:"upgrades_enabled,omitempty"`
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
		if item.Quality == "" {
			return fmt.Errorf("quality keys must be non-empty")
		}
		if item.Allowed {
			anyAllowed = true
		}
		if item.Quality == input.Cutoff {
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
	row, err := sqlc.New(a.db).CreateManagerQualityProfile(ctx, sqlc.CreateManagerQualityProfileParams{
		Name:            input.Name,
		Domain:          input.Domain,
		Items:           items,
		Cutoff:          input.Cutoff,
		UpgradesEnabled: upgrades,
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
	row, err := q.UpdateManagerQualityProfile(ctx, sqlc.UpdateManagerQualityProfileParams{
		ID:              id,
		Name:            input.Name,
		Items:           items,
		Cutoff:          input.Cutoff,
		UpgradesEnabled: upgrades,
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
