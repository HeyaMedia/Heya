package heyametadata

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/karbowiak/heya/clients/heyametadata"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

const providerIDPrefix = "heyametadata:v2:"

type Summary struct {
	SchemaVersion     int            `json:"schema_version"`
	ProjectionVersion int64          `json:"projection_version"`
	ID                string         `json:"id"`
	Kind              string         `json:"kind"`
	Slug              string         `json:"slug"`
	Display           SummaryDisplay `json:"display"`
	ExternalIDs       []ExternalID   `json:"external_ids"`
}

type SummaryDisplay struct {
	Title          string   `json:"title"`
	Name           string   `json:"name"`
	OriginalTitle  string   `json:"original_title"`
	Disambiguation string   `json:"disambiguation"`
	Year           int      `json:"year"`
	ImageID        string   `json:"image_id"`
	Aliases        []string `json:"aliases"`
}

type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type selectedReference struct {
	EntityID     string `json:"entity_id,omitempty"`
	CandidateRef string `json:"candidate_ref,omitempty"`
	Kind         string `json:"kind,omitempty"`
}

type workflowStore struct{ q *sqlc.Queries }

func newWorkflowStore(db *pgxpool.Pool) *workflowStore {
	if db == nil {
		return nil
	}
	return &workflowStore{q: sqlc.New(db)}
}

func EncodeEntityProviderID(entityID string) string {
	return providerIDPrefix + "entity:" + entityID
}

func EncodeCandidateProviderID(candidateRef uuid.UUID, kind string) string {
	return providerIDPrefix + "candidate:" + kind + ":" + candidateRef.String()
}

func decodeProviderID(value string) (selectedReference, bool, error) {
	if strings.HasPrefix(value, providerIDPrefix+"entity:") {
		id := strings.TrimPrefix(value, providerIDPrefix+"entity:")
		if _, err := uuid.Parse(id); err != nil {
			return selectedReference{}, true, fmt.Errorf("invalid canonical entity ID %q: %w", id, err)
		}
		return selectedReference{EntityID: id}, true, nil
	}
	if strings.HasPrefix(value, providerIDPrefix+"candidate:") {
		rest := strings.TrimPrefix(value, providerIDPrefix+"candidate:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 || parts[0] == "" {
			return selectedReference{}, true, fmt.Errorf("invalid opaque metadata candidate %q", value)
		}
		candidateRef, err := uuid.Parse(parts[1])
		if err != nil {
			return selectedReference{}, true, fmt.Errorf("invalid opaque metadata candidate %q: %w", value, err)
		}
		return selectedReference{CandidateRef: candidateRef.String(), Kind: parts[0]}, true, nil
	}
	return selectedReference{}, false, nil
}

func (c *Client) Search(ctx context.Context, kind, query string, year int, language string) ([]Summary, error) {
	limit := int64(25)
	q := strings.TrimSpace(query)
	params := &gen.SearchEntitiesParams{Q: &q, Limit: &limit}
	if kind != "" {
		value := gen.SearchEntitiesParamsKind(kind)
		params.Kind = &value
	}
	if year > 0 {
		value := int64(year)
		params.Year = &value
	}
	if language != "" {
		params.AcceptLanguage = &language
	}
	response, err := c.gen.SearchEntitiesWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("search canonical metadata: %w", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, responseError("search canonical metadata", response.StatusCode(), response.Body)
	}
	return decodeSummaries(response.Body)
}

func decodeSummaries(body []byte) ([]Summary, error) {
	var envelope struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode canonical search: %w", err)
	}
	result := make([]Summary, 0, len(envelope.Results))
	for _, raw := range envelope.Results {
		var summary Summary
		if err := json.Unmarshal(raw, &summary); err != nil {
			return nil, fmt.Errorf("decode canonical search result: %w", err)
		}
		result = append(result, summary)
	}
	return result, nil
}

func (c *Client) Discover(ctx context.Context, request gen.Request, credentials ProviderCredentials, store *workflowStore) (*gen.DiscoveryResource, error) {
	requestKey := workflowRequestKey("discovery", request)
	if store != nil {
		hints, _ := json.Marshal(request.Hints)
		workflow, err := store.q.UpsertMetadataWorkflow(ctx, sqlc.UpsertMetadataWorkflowParams{
			RequestKey: requestKey, Kind: request.Kind, Query: stringValue(request.Query),
			Hints: hints, SelectedResolution: []byte("{}"), State: "discovering",
		})
		if err == nil && workflow.State == "completed" && workflow.EntityID.Valid {
			return nil, fmt.Errorf("discovery workflow unexpectedly completed as an entity")
		}
		if err == nil && workflow.DiscoveryID.Valid {
			id := uuid.UUID(workflow.DiscoveryID.Bytes)
			var resource *gen.DiscoveryResource
			if _, deferred := metadata.DeferredRemoteWorkDelay(ctx); deferred {
				resource, err = c.checkDiscovery(ctx, id, credentials)
			} else {
				resource, err = c.pollDiscovery(ctx, id, credentials, pollDelay(200*time.Millisecond, nil))
			}
			if resource != nil && (resource.State == gen.DiscoveryResourceStateCompleted || resource.State == gen.DiscoveryResourceStateFailed) {
				_ = store.q.ClearMetadataWorkflowDiscovery(ctx, requestKey)
			}
			return resource, err
		}
	}
	prefer := "wait=5"
	response, err := c.gen.CreateDiscoveryWithResponse(ctx, &gen.CreateDiscoveryParams{Prefer: &prefer}, request, c.credentialEditor(credentials))
	if err != nil {
		if deferred := transientDeferredWorkError(ctx, "retry metadata discovery create after "+err.Error(), nil); deferred != nil {
			return nil, deferred
		}
		return nil, fmt.Errorf("create metadata discovery: %w", err)
	}
	resource := response.JSON200
	if resource == nil {
		resource = response.JSON202
	}
	if resource == nil {
		if deferred := transientDeferredWorkError(ctx, "retry metadata discovery create", response.HTTPResponse); deferred != nil {
			return nil, deferred
		}
		return nil, responseError("create metadata discovery", response.StatusCode(), response.Body)
	}
	discoveryID := resource.Id
	if store != nil {
		_, _ = store.q.MarkMetadataWorkflowDiscovery(ctx, sqlc.MarkMetadataWorkflowDiscoveryParams{
			RequestKey: requestKey, DiscoveryID: pgUUID(discoveryID), State: "discovering",
		})
	}
	if resource.State == gen.DiscoveryResourceStateCompleted || resource.State == gen.DiscoveryResourceStateFailed {
		if store != nil {
			_ = store.q.ClearMetadataWorkflowDiscovery(ctx, requestKey)
		}
		return resource, discoveryTerminalError(resource)
	}
	if err := deferredWorkflowError(ctx, "metadata discovery "+discoveryID.String(), "discovery", discoveryID.String(), response.HTTPResponse); err != nil {
		return nil, err
	}
	return c.pollDiscovery(ctx, discoveryID, credentials, pollDelay(200*time.Millisecond, response.HTTPResponse))
}

func (c *Client) checkDiscovery(ctx context.Context, id uuid.UUID, credentials ProviderCredentials) (*gen.DiscoveryResource, error) {
	response, err := c.gen.GetDiscoveryWithResponse(ctx, id, c.credentialEditor(credentials))
	if err != nil {
		if deferred := transientDeferredWorkflowError(ctx, "retry metadata discovery "+id.String()+" after "+err.Error(), "discovery", id.String(), nil); deferred != nil {
			return nil, deferred
		}
		return nil, fmt.Errorf("poll metadata discovery: %w", err)
	}
	resource := discoveryResource(response)
	if resource != nil && (resource.State == gen.DiscoveryResourceStateCompleted || resource.State == gen.DiscoveryResourceStateFailed) {
		return resource, discoveryTerminalError(resource)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		if deferred := transientDeferredWorkflowError(ctx, "retry metadata discovery "+id.String(), "discovery", id.String(), response.HTTPResponse); deferred != nil {
			return nil, deferred
		}
		return nil, responseError("poll metadata discovery", response.StatusCode(), response.Body)
	}
	return nil, deferredWorkflowError(ctx, "metadata discovery "+id.String(), "discovery", id.String(), response.HTTPResponse)
}

func (c *Client) pollDiscovery(ctx context.Context, id uuid.UUID, credentials ProviderCredentials, delay time.Duration) (*gen.DiscoveryResource, error) {
	backoff := 200 * time.Millisecond
	for {
		if err := waitContext(ctx, delay); err != nil {
			return nil, err
		}
		response, err := c.gen.GetDiscoveryWithResponse(ctx, id, c.credentialEditor(credentials))
		if err != nil {
			return nil, fmt.Errorf("poll metadata discovery: %w", err)
		}
		resource := discoveryResource(response)
		if resource != nil && (resource.State == gen.DiscoveryResourceStateCompleted || resource.State == gen.DiscoveryResourceStateFailed) {
			return resource, discoveryTerminalError(resource)
		}
		if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return nil, responseError("poll metadata discovery", response.StatusCode(), response.Body)
		}
		backoff = growPollBackoff(backoff)
		delay = pollDelay(backoff, response.HTTPResponse)
	}
}

// HeyaMetadata returns a failed discovery as a structured 503 response. Keep
// that terminal resource visible to durable callers so they can discard the
// stale discovery ID and create a fresh run instead of snoozing forever.
func discoveryResource(response *gen.GetDiscoveryResponse) *gen.DiscoveryResource {
	if response == nil {
		return nil
	}
	if response.JSON200 != nil {
		return response.JSON200
	}
	return response.JSON503
}

func discoveryTerminalError(resource *gen.DiscoveryResource) error {
	if resource == nil || resource.State != gen.DiscoveryResourceStateFailed {
		return nil
	}
	if resource.Error != nil && *resource.Error != "" {
		return fmt.Errorf("metadata discovery failed: %s", *resource.Error)
	}
	return errors.New("metadata discovery failed")
}

func (c *Client) Resolve(ctx context.Context, candidateRef uuid.UUID, kind string, credentials ProviderCredentials, store *workflowStore) (string, error) {
	input := gen.ResolutionInputBody{CandidateRef: candidateRef}
	requestKey := workflowRequestKey("resolution", input)
	resolutionBody, _ := json.Marshal(input)
	if store != nil {
		workflow, err := store.q.UpsertMetadataWorkflow(ctx, sqlc.UpsertMetadataWorkflowParams{
			RequestKey: requestKey, Kind: kind, SelectedResolution: resolutionBody,
			Hints: []byte("{}"), State: "resolving",
		})
		if err == nil {
			if workflow.EntityID.Valid {
				return uuid.UUID(workflow.EntityID.Bytes).String(), nil
			}
			if workflow.JobID.Valid {
				if _, deferred := metadata.DeferredRemoteWorkDelay(ctx); deferred {
					return c.checkResolutionJob(ctx, requestKey, workflow.JobID.Int64, store, credentials)
				}
				return c.pollResolutionJob(ctx, requestKey, workflow.JobID.Int64, store, credentials, pollDelay(200*time.Millisecond, nil))
			}
		}
	}
	prefer := "wait=5"
	response, err := c.gen.ResolveEntityWithResponse(ctx, &gen.ResolveEntityParams{Prefer: &prefer}, input, c.credentialEditor(credentials))
	if err != nil {
		if deferred := transientDeferredWorkError(ctx, "retry canonical metadata resolution after "+err.Error(), nil); deferred != nil {
			return "", deferred
		}
		return "", fmt.Errorf("resolve canonical metadata: %w", err)
	}
	body := response.JSON200
	if body == nil {
		body = response.JSON202
	}
	if body == nil {
		if deferred := transientDeferredWorkError(ctx, "retry canonical metadata resolution", response.HTTPResponse); deferred != nil {
			return "", deferred
		}
		return "", responseError("resolve canonical metadata", response.StatusCode(), response.Body)
	}
	if body.EntityId != nil {
		return c.completeResolution(ctx, requestKey, body.EntityId.String(), store)
	}
	if body.Job == nil {
		return "", fmt.Errorf("resolve canonical metadata: success response has neither entity_id nor job")
	}
	if store != nil {
		_, _ = store.q.MarkMetadataWorkflowResolving(ctx, sqlc.MarkMetadataWorkflowResolvingParams{
			RequestKey: requestKey, SelectedResolution: resolutionBody,
			JobID: pgtype.Int8{Int64: body.Job.Id, Valid: true},
		})
	}
	if err := deferredWorkError(ctx, "metadata resolution job "+strconv.FormatInt(body.Job.Id, 10), response.HTTPResponse); err != nil {
		return "", err
	}
	return c.pollResolutionJob(ctx, requestKey, body.Job.Id, store, credentials, pollDelay(200*time.Millisecond, response.HTTPResponse))
}

func (c *Client) checkResolutionJob(ctx context.Context, requestKey string, jobID int64, store *workflowStore, credentials ProviderCredentials) (string, error) {
	response, err := c.gen.JobStatusWithResponse(ctx, jobID, c.credentialEditor(credentials))
	if err != nil {
		if deferred := transientDeferredWorkError(ctx, "retry metadata resolution job "+strconv.FormatInt(jobID, 10)+" after "+err.Error(), nil); deferred != nil {
			return "", deferred
		}
		return "", fmt.Errorf("poll metadata resolution job %d: %w", jobID, err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		if deferred := transientDeferredWorkError(ctx, "retry metadata resolution job "+strconv.FormatInt(jobID, 10), response.HTTPResponse); deferred != nil {
			return "", deferred
		}
		return "", responseError("poll metadata resolution job", response.StatusCode(), response.Body)
	}
	job := response.JSON200
	if job.EntityId != nil {
		return c.completeResolution(ctx, requestKey, job.EntityId.String(), store)
	}
	switch strings.ToLower(job.State) {
	case "failed", "cancelled", "discarded":
		message := "metadata resolution job " + strconv.FormatInt(jobID, 10) + " failed"
		if job.Error != nil && *job.Error != "" {
			message += ": " + *job.Error
		}
		if store != nil {
			_, _ = store.q.FailMetadataWorkflow(ctx, sqlc.FailMetadataWorkflowParams{RequestKey: requestKey, LastError: message})
		}
		return "", errors.New(message)
	}
	return "", deferredWorkError(ctx, "metadata resolution job "+strconv.FormatInt(jobID, 10), response.HTTPResponse)
}

func (c *Client) pollResolutionJob(ctx context.Context, requestKey string, jobID int64, store *workflowStore, credentials ProviderCredentials, delay time.Duration) (string, error) {
	backoff := 200 * time.Millisecond
	for {
		if err := waitContext(ctx, delay); err != nil {
			return "", err
		}
		response, err := c.gen.JobStatusWithResponse(ctx, jobID, c.credentialEditor(credentials))
		if err != nil {
			return "", fmt.Errorf("poll metadata resolution job %d: %w", jobID, err)
		}
		if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return "", responseError("poll metadata resolution job", response.StatusCode(), response.Body)
		}
		job := response.JSON200
		if job.EntityId != nil {
			return c.completeResolution(ctx, requestKey, job.EntityId.String(), store)
		}
		switch strings.ToLower(job.State) {
		case "failed", "cancelled", "discarded":
			message := "metadata resolution job " + strconv.FormatInt(jobID, 10) + " failed"
			if job.Error != nil && *job.Error != "" {
				message += ": " + *job.Error
			}
			if store != nil {
				_, _ = store.q.FailMetadataWorkflow(ctx, sqlc.FailMetadataWorkflowParams{RequestKey: requestKey, LastError: message})
			}
			return "", errors.New(message)
		}
		backoff = growPollBackoff(backoff)
		delay = pollDelay(backoff, response.HTTPResponse)
	}
}

func (c *Client) completeResolution(ctx context.Context, requestKey, entityID string, store *workflowStore) (string, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return "", fmt.Errorf("metadata resolution returned invalid entity ID: %w", err)
	}
	if store != nil {
		if _, err := store.q.CompleteMetadataWorkflow(ctx, sqlc.CompleteMetadataWorkflowParams{RequestKey: requestKey, EntityID: pgUUID(id)}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("persist completed metadata resolution: %w", err)
		}
	}
	return id.String(), nil
}

func workflowRequestKey(prefix string, value any) string {
	body, _ := json.Marshal(value)
	// Completed discovery results belong to HeyaMetadata's versioned cache,
	// not to an eternal Heya-side decision cache. The version also invalidates
	// workflows created before terminal discovery IDs were cleared.
	digest := sha256.Sum256(append([]byte("v2:"+prefix+":"), body...))
	return hex.EncodeToString(digest[:])
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func growPollBackoff(delay time.Duration) time.Duration {
	delay *= 2
	if delay > 2*time.Second {
		return 2 * time.Second
	}
	return delay
}

// pollDelay applies bounded jitter to every poll and treats Retry-After as a
// minimum. This keeps many scanners resuming at once from synchronizing their
// requests while still honoring the service's explicit pacing.
func pollDelay(backoff time.Duration, response *http.Response) time.Duration {
	if retry := retryAfterDuration(response, time.Now()); retry > 0 {
		return retry + randomDuration(retry/5)
	}
	spread := backoff / 5
	return backoff - spread + randomDuration(2*spread)
}

func retryAfterDuration(response *http.Response, now time.Time) time.Duration {
	if response == nil {
		return 0
	}
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		return 0
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

func deferredWorkError(ctx context.Context, operation string, response *http.Response) error {
	delay, deferred := metadata.DeferredRemoteWorkDelay(ctx)
	if !deferred {
		return nil
	}
	if retryAfter := retryAfterDuration(response, time.Now()); retryAfter > delay {
		delay = retryAfter
	}
	return &metadata.DeferredWorkError{Operation: operation, RetryAfter: delay}
}

func deferredWorkflowError(ctx context.Context, operation, workflowKind, workflowID string, response *http.Response) error {
	err := deferredWorkError(ctx, operation, response)
	var deferred *metadata.DeferredWorkError
	if errors.As(err, &deferred) {
		deferred.WorkflowKind = workflowKind
		deferred.WorkflowID = workflowID
	}
	return err
}

func transientDeferredWorkError(ctx context.Context, operation string, response *http.Response) error {
	if _, deferred := metadata.DeferredRemoteWorkDelay(ctx); !deferred {
		return nil
	}
	if response != nil {
		status := response.StatusCode
		if status != http.StatusRequestTimeout && status != http.StatusTooEarly && status != http.StatusTooManyRequests && status < http.StatusInternalServerError {
			return nil
		}
	}
	return deferredWorkError(ctx, operation, response)
}

func transientDeferredWorkflowError(ctx context.Context, operation, workflowKind, workflowID string, response *http.Response) error {
	err := transientDeferredWorkError(ctx, operation, response)
	var deferred *metadata.DeferredWorkError
	if errors.As(err, &deferred) {
		deferred.WorkflowKind = workflowKind
		deferred.WorkflowID = workflowID
	}
	return err
}

func randomDuration(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	upper := new(big.Int).Add(big.NewInt(int64(max)), big.NewInt(1))
	value, err := cryptorand.Int(cryptorand.Reader, upper)
	if err != nil {
		return max / 2
	}
	return time.Duration(value.Int64())
}

func pgUUID(value uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(value), Valid: true}
}
