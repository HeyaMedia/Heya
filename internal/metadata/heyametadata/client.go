package heyametadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	gen "github.com/karbowiak/heya/clients/heyametadata"
	"github.com/karbowiak/heya/internal/metadata"
)

// ProviderCredentials are forwarded only on the request that needs them. They
// are intentionally never stored in workflow rows, cache keys, or logs.
type ProviderCredentials struct {
	TMDBAPIKey        string
	OMDBAPIKey        string
	TVDBAPIKey        string
	FanartAPIKey      string
	AppleAPIKey       string
	DiscogsAPIKey     string
	LastFMAPIKey      string
	GoogleBooksAPIKey string
	MALClientID       string
}

type Client struct {
	gen     *gen.ClientWithResponses
	baseURL string
}

func NewClient(baseURL, apiKey string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("heyametadata: base URL is required")
	}
	httpClient := &http.Client{
		Timeout: 3 * time.Minute,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          64,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 45 * time.Second,
		},
	}
	generated, err := gen.NewClientWithResponses(
		baseURL,
		gen.WithHTTPClient(httpClient),
		gen.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("User-Agent", "Heya/v2-metadata-client")
			if strings.TrimSpace(apiKey) != "" {
				req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
			}
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("heyametadata: construct generated client: %w", err)
	}
	return &Client{gen: generated, baseURL: baseURL}, nil
}

func (c *Client) ImageURL(imageID string) string {
	if strings.TrimSpace(imageID) == "" {
		return ""
	}
	return c.baseURL + "/api/v2/images/" + imageID
}

type Change struct {
	Sequence          int64
	EntityID          string
	EntityKind        string
	Slug              string
	ChangeType        string
	ChangedScopes     []string
	ProjectionVersion int64
}

type ChangePage struct {
	Entries    []Change
	StreamID   string
	HeadCursor int64
	NextCursor int64
}

type WorkflowEvent struct {
	Sequence    int64
	Kind        string
	ID          string
	State       string
	CompletedAt time.Time
}

type WorkflowEventPage struct {
	Events     []WorkflowEvent
	StreamID   string
	HeadCursor int64
	NextCursor int64
}

// ChangeStreamConflict tells a durable consumer that its saved cursor cannot
// safely continue against the current HeyaMetadata database. The caller must
// adopt StreamID, reset to cursor zero, and replay idempotently.
type ChangeStreamConflict struct {
	Code       string
	StreamID   string
	HeadCursor int64
}

func (e *ChangeStreamConflict) Error() string {
	return fmt.Sprintf("heyametadata change stream conflict %s (stream %s, head %d)", e.Code, e.StreamID, e.HeadCursor)
}

// WorkflowStreamConflict is the workflow completion feed equivalent of
// ChangeStreamConflict. The consumer must clear stream-scoped wake state,
// adopt StreamID, and replay from zero.
type WorkflowStreamConflict struct {
	Code       string
	StreamID   string
	HeadCursor int64
}

func (e *WorkflowStreamConflict) Error() string {
	return fmt.Sprintf("heyametadata workflow stream conflict %s (stream %s, head %d)", e.Code, e.StreamID, e.HeadCursor)
}

// RecordingLyrics is the provider-transparent lyric evidence Heya needs for
// playback. Upstream record IDs and provider names remain inside
// HeyaMetadata; the media server only selects between synchronized and plain
// text attached to a canonical recording UUID.
type RecordingLyrics struct {
	PlainLyrics  string
	SyncedLyrics string
	Instrumental bool
}

func (c *Client) Changes(ctx context.Context, after, limit int64, streamID string) (ChangePage, error) {
	params := &gen.PublicChangesParams{After: &after, Limit: &limit}
	if strings.TrimSpace(streamID) != "" {
		id, err := uuid.Parse(streamID)
		if err != nil {
			return ChangePage{}, fmt.Errorf("read metadata changes: invalid stream ID %q: %w", streamID, err)
		}
		params.StreamId = &id
	}
	response, err := c.gen.PublicChangesWithResponse(ctx, params)
	if err != nil {
		return ChangePage{}, fmt.Errorf("read metadata changes after %d: %w", after, err)
	}
	if response.StatusCode() == http.StatusConflict && response.ApplicationproblemJSON409 != nil {
		problem := response.ApplicationproblemJSON409
		code := ""
		if problem.Code != nil {
			code = *problem.Code
		}
		if code == "change_cursor_ahead" || code == "change_stream_changed" {
			conflict := &ChangeStreamConflict{Code: code}
			if problem.StreamId != nil {
				conflict.StreamID = problem.StreamId.String()
			}
			if problem.HeadCursor != nil {
				conflict.HeadCursor = *problem.HeadCursor
			}
			return ChangePage{}, conflict
		}
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return ChangePage{}, responseError("read metadata changes", response.StatusCode(), response.Body)
	}
	page := ChangePage{
		StreamID: response.JSON200.StreamId.String(), HeadCursor: response.JSON200.HeadCursor,
		NextCursor: response.JSON200.NextCursor,
	}
	page.Entries = make([]Change, 0, len(response.JSON200.Entries))
	for _, entry := range response.JSON200.Entries {
		var scopes []string
		if entry.ChangedScopes != nil {
			scopes = append(scopes, (*entry.ChangedScopes)...)
		}
		page.Entries = append(page.Entries, Change{
			Sequence: entry.Sequence, EntityID: entry.EntityId.String(), EntityKind: entry.EntityKind,
			Slug: entry.Slug, ChangeType: entry.ChangeType, ChangedScopes: scopes,
			ProjectionVersion: entry.ProjectionVersion,
		})
	}
	return page, nil
}

// WorkflowEvents reads one page from HeyaMetadata's gap-free async workflow
// completion feed. New workflow kinds are kept as strings so consumers can
// ignore families they do not understand without requiring a client update.
func (c *Client) WorkflowEvents(ctx context.Context, after, limit int64, streamID string) (WorkflowEventPage, error) {
	params := &gen.WorkflowEventsParams{After: &after, Limit: &limit}
	if strings.TrimSpace(streamID) != "" {
		id, err := uuid.Parse(streamID)
		if err != nil {
			return WorkflowEventPage{}, fmt.Errorf("read metadata workflow events: invalid stream ID %q: %w", streamID, err)
		}
		params.StreamId = &id
	}
	response, err := c.gen.WorkflowEventsWithResponse(ctx, params)
	if err != nil {
		return WorkflowEventPage{}, fmt.Errorf("read metadata workflow events after %d: %w", after, err)
	}
	if response.StatusCode() == http.StatusConflict && response.ApplicationproblemJSON409 != nil {
		problem := response.ApplicationproblemJSON409
		code := ""
		if problem.Code != nil {
			code = *problem.Code
		}
		if code == "workflow_cursor_ahead" || code == "workflow_stream_changed" {
			conflict := &WorkflowStreamConflict{Code: code}
			if problem.StreamId != nil {
				conflict.StreamID = problem.StreamId.String()
			}
			if problem.HeadCursor != nil {
				conflict.HeadCursor = *problem.HeadCursor
			}
			return WorkflowEventPage{}, conflict
		}
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return WorkflowEventPage{}, responseError("read metadata workflow events", response.StatusCode(), response.Body)
	}
	page := WorkflowEventPage{
		StreamID: response.JSON200.StreamId.String(), HeadCursor: response.JSON200.HeadCursor,
		NextCursor: response.JSON200.NextCursor,
		Events:     make([]WorkflowEvent, 0, len(response.JSON200.Events)),
	}
	for _, event := range response.JSON200.Events {
		completedAt, err := time.Parse(time.RFC3339Nano, event.CompletedAt)
		if err != nil {
			return WorkflowEventPage{}, fmt.Errorf("workflow event %d has invalid completed_at %q: %w", event.Sequence, event.CompletedAt, err)
		}
		page.Events = append(page.Events, WorkflowEvent{
			Sequence: event.Sequence, Kind: string(event.Kind), ID: event.Id.String(),
			State: string(event.State), CompletedAt: completedAt,
		})
	}
	return page, nil
}

func (c *Client) credentialEditor(credentials ProviderCredentials) gen.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		setHeader(req, "X-Heya-TMDB-API-Key", credentials.TMDBAPIKey)
		setHeader(req, "X-Heya-OMDB-API-Key", credentials.OMDBAPIKey)
		setHeader(req, "X-Heya-TVDB-API-Key", credentials.TVDBAPIKey)
		setHeader(req, "X-Heya-Fanart-API-Key", credentials.FanartAPIKey)
		setHeader(req, "X-Heya-Apple-API-Key", credentials.AppleAPIKey)
		setHeader(req, "X-Heya-Discogs-API-Key", credentials.DiscogsAPIKey)
		setHeader(req, "X-Heya-LastFM-API-Key", credentials.LastFMAPIKey)
		setHeader(req, "X-Heya-Google-Books-API-Key", credentials.GoogleBooksAPIKey)
		setHeader(req, "X-Heya-MAL-Client-ID", credentials.MALClientID)
		return nil
	}
}

func setHeader(req *http.Request, name, value string) {
	if value = strings.TrimSpace(value); value != "" {
		req.Header.Set(name, value)
	}
}

func (c *Client) Ready(ctx context.Context) error {
	response, err := c.gen.HealthReadyWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("heyametadata readiness: %w", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return responseError("heyametadata readiness", response.StatusCode(), response.Body)
	}
	return nil
}

// Entity returns the raw canonical document. The public entity response is
// polymorphic, so kind-specific decoding deliberately lives in Heya's adapter
// rather than relying on an untyped generated interface value.
func (c *Client) Entity(ctx context.Context, entityID, language, country string, credentials ProviderCredentials) ([]byte, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("heyametadata entity: invalid UUID %q: %w", entityID, err)
	}
	params := &gen.EntityDetailParams{}
	if language = strings.TrimSpace(language); language != "" {
		params.Language = &language
		params.AcceptLanguage = &language
	}
	if country = strings.TrimSpace(country); country != "" {
		params.Country = &country
	}
	response, err := c.gen.EntityDetailWithResponse(ctx, id, params, c.credentialEditor(credentials))
	if err != nil {
		return nil, fmt.Errorf("read canonical metadata entity %s: %w", entityID, err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, responseError("read canonical metadata entity", response.StatusCode(), response.Body)
	}
	return response.Body, nil
}

// RecordingMetadata fetches the compact recording-scoped semantic slice used
// by Heya's recommendation catalog. The recording-detail response is
// polymorphic in the contract (schema: {}), so the decode is hand-written like
// the entity documents.
func (c *Client) RecordingMetadata(ctx context.Context, entityID string, credentials ProviderCredentials) (metadata.RecordingMetadata, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return metadata.RecordingMetadata{}, fmt.Errorf("heyametadata recording: invalid UUID %q: %w", entityID, err)
	}
	response, err := c.gen.RecordingDetailWithResponse(ctx, id, c.credentialEditor(credentials))
	if err != nil {
		return metadata.RecordingMetadata{}, fmt.Errorf("read canonical recording %s: %w", entityID, err)
	}
	if response.StatusCode() != http.StatusOK {
		return metadata.RecordingMetadata{}, responseError("read canonical recording", response.StatusCode(), response.Body)
	}
	var document struct {
		ID   string `json:"id"`
		Data struct {
			Provider       string `json:"provider"`
			Namespace      string `json:"namespace"`
			ProviderID     string `json:"provider_id"`
			Title          string `json:"title"`
			Disambiguation string `json:"disambiguation"`
			ArtistCredits  []struct {
				Name       string `json:"name"`
				ArtistName string `json:"artist_name"`
			} `json:"artist_credits"`
			Genres []struct {
				Name string `json:"name"`
			} `json:"genres"`
			Tags []struct {
				Name string `json:"name"`
			} `json:"tags"`
			Links []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"links"`
			Credits []struct {
				Role           string   `json:"role"`
				Attributes     []string `json:"attributes"`
				ArtistEntityID string   `json:"artist_entity_id"`
				ArtistProvider string   `json:"artist_provider"`
				ArtistID       string   `json:"artist_id"`
				ArtistName     string   `json:"artist_name"`
			} `json:"credits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &document); err != nil {
		return metadata.RecordingMetadata{}, fmt.Errorf("decode canonical recording %s: %w", entityID, err)
	}
	result := metadata.RecordingMetadata{
		CanonicalID: document.ID, Title: document.Data.Title,
		Disambiguation: document.Data.Disambiguation,
	}
	if result.CanonicalID == "" {
		result.CanonicalID = entityID
	}
	if document.Data.Provider == "musicbrainz" && document.Data.Namespace == "recording" {
		result.RecordingMBID = document.Data.ProviderID
	}
	if len(document.Data.ArtistCredits) > 0 {
		result.ArtistName = firstNonEmpty(document.Data.ArtistCredits[0].Name, document.Data.ArtistCredits[0].ArtistName)
	}
	for _, value := range document.Data.Genres {
		result.Genres = append(result.Genres, value.Name)
	}
	for _, value := range document.Data.Tags {
		result.Tags = append(result.Tags, value.Name)
	}
	for _, value := range document.Data.Links {
		result.Links = append(result.Links, metadata.URLEntry{Type: value.Type, URL: value.URL})
	}
	for _, value := range document.Data.Credits {
		mapped := metadata.RecordingCredit{Role: value.Role, Attributes: value.Attributes, ArtistName: value.ArtistName, ArtistEntityID: value.ArtistEntityID}
		if value.ArtistProvider == "musicbrainz" {
			mapped.ArtistMBID = value.ArtistID
		}
		result.Credits = append(result.Credits, mapped)
	}
	return result, nil
}

// RecordingCredits retains the narrow compatibility surface for callers that
// only render credits. New enrichment code should use RecordingMetadata so the
// same network response also feeds semantic facets.
func (c *Client) RecordingCredits(ctx context.Context, entityID string, credentials ProviderCredentials) ([]metadata.RecordingCredit, error) {
	result, err := c.RecordingMetadata(ctx, entityID, credentials)
	return result.Credits, err
}

func (c *Client) Credits(ctx context.Context, entityID string, credentials ProviderCredentials) ([]credit, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("heyametadata credits: invalid UUID %q: %w", entityID, err)
	}
	const pageSize = int64(5000)
	var result []credit
	for offset := int64(0); ; {
		limit := pageSize
		response, err := c.gen.EntityCreditsWithResponse(ctx, id, &gen.EntityCreditsParams{Offset: &offset, Limit: &limit}, c.credentialEditor(credentials))
		if err != nil {
			return nil, fmt.Errorf("read canonical metadata credits %s: %w", entityID, err)
		}
		if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return nil, responseError("read canonical metadata credits", response.StatusCode(), response.Body)
		}
		var page struct {
			Results []credit `json:"results"`
			Total   int64    `json:"total"`
			Offset  int64    `json:"offset"`
			Limit   int64    `json:"limit"`
		}
		if err := json.Unmarshal(response.Body, &page); err != nil {
			return nil, fmt.Errorf("decode canonical metadata credits: %w", err)
		}
		result = append(result, page.Results...)
		if int64(len(result)) >= page.Total {
			return result, nil
		}
		if len(page.Results) == 0 {
			return nil, fmt.Errorf("read canonical metadata credits: page at offset %d returned no results before total %d", offset, page.Total)
		}
		offset += int64(len(page.Results))
	}
}

func (c *Client) Images(ctx context.Context, entityID, language, country string, credentials ...ProviderCredentials) (*gen.EntityImagesOutputBody, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("heyametadata images: invalid UUID %q: %w", entityID, err)
	}
	limit := int64(100)
	params := &gen.EntityImagesParams{Limit: &limit}
	if language = strings.TrimSpace(language); language != "" {
		params.Language = &language
		params.AcceptLanguage = &language
	}
	if country = strings.TrimSpace(country); country != "" {
		params.Country = &country
	}
	response, err := c.gen.EntityImagesWithResponse(ctx, id, params, c.credentialEditor(firstCredentials(credentials)))
	if err != nil {
		return nil, fmt.Errorf("read canonical metadata images %s: %w", entityID, err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, responseError("read canonical metadata images", response.StatusCode(), response.Body)
	}
	return response.JSON200, nil
}

func (c *Client) Ratings(ctx context.Context, entityID string, credentials ...ProviderCredentials) ([]rating, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("heyametadata ratings: invalid UUID %q: %w", entityID, err)
	}
	const pageSize = int64(250)
	var result []rating
	for offset := int64(0); ; {
		limit := pageSize
		response, err := c.gen.EntityRatingsWithResponse(ctx, id, &gen.EntityRatingsParams{Offset: &offset, Limit: &limit}, c.credentialEditor(firstCredentials(credentials)))
		if err != nil {
			return nil, fmt.Errorf("read canonical metadata ratings %s: %w", entityID, err)
		}
		if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return nil, responseError("read canonical metadata ratings", response.StatusCode(), response.Body)
		}
		var page struct {
			Results []rating `json:"results"`
			Total   int64    `json:"total"`
		}
		if err := json.Unmarshal(response.Body, &page); err != nil {
			return nil, fmt.Errorf("decode canonical metadata ratings: %w", err)
		}
		result = append(result, page.Results...)
		if int64(len(result)) >= page.Total {
			return result, nil
		}
		if len(page.Results) == 0 {
			return nil, fmt.Errorf("read canonical metadata ratings: page at offset %d returned no results before total %d", offset, page.Total)
		}
		offset += int64(len(page.Results))
	}
}

func (c *Client) TopTracks(ctx context.Context, entityID string, credentials ...ProviderCredentials) ([]gen.TopTrack, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("heyametadata top tracks: invalid UUID %q: %w", entityID, err)
	}
	const pageSize = int64(100)
	var result []gen.TopTrack
	for offset := int64(0); ; {
		limit := pageSize
		response, err := c.gen.ArtistTopTracksWithResponse(ctx, id, &gen.ArtistTopTracksParams{Offset: &offset, Limit: &limit}, c.credentialEditor(firstCredentials(credentials)))
		if err != nil {
			return nil, fmt.Errorf("read canonical artist top tracks %s: %w", entityID, err)
		}
		if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return nil, responseError("read canonical artist top tracks", response.StatusCode(), response.Body)
		}
		page := response.JSON200
		pageTracks := []gen.TopTrack(nil)
		if page.Results != nil {
			pageTracks = *page.Results
		}
		result = append(result, pageTracks...)
		if int64(len(result)) >= page.Total {
			return result, nil
		}
		if len(pageTracks) == 0 {
			return nil, fmt.Errorf("read canonical artist top tracks: page at offset %d returned no results before total %d", offset, page.Total)
		}
		offset += int64(len(pageTracks))
	}
}

func (c *Client) RecordingLyrics(ctx context.Context, entityID string, credentials ...ProviderCredentials) ([]RecordingLyrics, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("heyametadata recording lyrics: invalid UUID %q: %w", entityID, err)
	}
	response, err := c.gen.RecordingLyricsWithResponse(ctx, id, c.credentialEditor(firstCredentials(credentials)))
	if err != nil {
		return nil, fmt.Errorf("read canonical recording lyrics %s: %w", entityID, err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, responseError("read canonical recording lyrics", response.StatusCode(), response.Body)
	}
	if response.JSON200.Items == nil {
		return []RecordingLyrics{}, nil
	}
	items := make([]RecordingLyrics, 0, len(*response.JSON200.Items))
	for _, item := range *response.JSON200.Items {
		mapped := RecordingLyrics{Instrumental: item.Instrumental}
		if item.PlainLyrics != nil {
			mapped.PlainLyrics = *item.PlainLyrics
		}
		if item.SyncedLyrics != nil {
			mapped.SyncedLyrics = *item.SyncedLyrics
		}
		items = append(items, mapped)
	}
	return items, nil
}

func (c *Client) Relations(ctx context.Context, entityID, relationType string, offset, limit int64, credentials ...ProviderCredentials) (*gen.EntityRelationsOutputBody, error) {
	params := &gen.EntityRelationsParams{Offset: &offset, Limit: &limit}
	if relationType != "" {
		params.Type = &relationType
	}
	response, err := c.gen.EntityRelationsWithResponse(ctx, entityID, params, c.credentialEditor(firstCredentials(credentials)))
	if err != nil {
		return nil, fmt.Errorf("read canonical metadata relations %s: %w", entityID, err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, responseError("read canonical metadata relations", response.StatusCode(), response.Body)
	}
	return response.JSON200, nil
}

func (c *Client) Release(ctx context.Context, entityID string, credentials ...ProviderCredentials) ([]byte, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("heyametadata release: invalid UUID %q: %w", entityID, err)
	}
	response, err := c.gen.ReleaseDetailWithResponse(ctx, id, c.credentialEditor(firstCredentials(credentials)))
	if err != nil {
		return nil, fmt.Errorf("read canonical release %s: %w", entityID, err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, responseError("read canonical release", response.StatusCode(), response.Body)
	}
	return response.Body, nil
}

type APIError struct {
	Operation string
	Status    int
	Body      string
	Problem   *gen.ErrorModel
}

func (e *APIError) Error() string {
	if e.Problem != nil {
		message := firstNonEmpty(stringValue(e.Problem.Detail), stringValue(e.Problem.Title))
		if message != "" {
			return fmt.Sprintf("%s: upstream status %d: %s", e.Operation, e.Status, message)
		}
	}
	if e.Body == "" {
		return fmt.Sprintf("%s: upstream status %d", e.Operation, e.Status)
	}
	return fmt.Sprintf("%s: upstream status %d: %s", e.Operation, e.Status, e.Body)
}

func responseError(operation string, status int, body []byte) error {
	text := strings.TrimSpace(string(body))
	if len(text) > 1024 {
		text = text[:1024]
	}
	apiErr := &APIError{Operation: operation, Status: status, Body: text}
	var problem gen.ErrorModel
	if json.Unmarshal(body, &problem) == nil && (problem.Type != nil || problem.Title != nil || problem.Detail != nil || problem.Status != nil || problem.Errors != nil) {
		apiErr.Problem = &problem
	}
	return apiErr
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return true
	}
	return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500
}

func firstCredentials(values []ProviderCredentials) ProviderCredentials {
	if len(values) == 0 {
		return ProviderCredentials{}
	}
	return values[0]
}
