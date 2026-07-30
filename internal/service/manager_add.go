package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
)

// ManagerLookupResult is one provider search hit for the add-new flow,
// annotated with whether the library already has it.
type ManagerLookupResult struct {
	ProviderName     string            `json:"provider_name"`
	ProviderID       string            `json:"provider_id"`
	Title            string            `json:"title"`
	Year             string            `json:"year,omitempty"`
	Description      string            `json:"description,omitempty"`
	PosterURL        string            `json:"poster_url,omitempty"`
	HeyaSlug         string            `json:"heya_slug,omitempty"`
	ExternalIDs      map[string]string `json:"external_ids,omitempty"`
	AlreadyInLibrary bool              `json:"already_in_library"`
	ExistingItemID   int64             `json:"existing_item_id,omitempty"`
}

// ManagerLookup searches the metadata provider for new items to add,
// scoped by the target library (its media type picks the search kind).
func (a *App) ManagerLookup(ctx context.Context, libraryID int64, query string) ([]ManagerLookupResult, error) {
	q := sqlc.New(a.db)
	lib, err := q.GetLibraryByID(ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("library %d: %w", libraryID, err)
	}
	settings := metadata.ParseSettings(lib.Settings)

	var kind metadata.MediaKind
	switch lib.MediaType {
	case sqlc.MediaTypeMovie:
		kind = metadata.KindMovie
	case sqlc.MediaTypeTv, sqlc.MediaTypeAnime:
		kind = metadata.KindTV
	case sqlc.MediaTypeMusic:
		kind = metadata.KindMusic
	case sqlc.MediaTypeBook:
		kind = metadata.KindBook
	default:
		return nil, fmt.Errorf("library %d has unsupported media type %s", libraryID, lib.MediaType)
	}

	results, err := a.heya.Search(ctx, kind, metadata.SearchQuery{
		Title: query, Language: settings.PreferredLanguage, Country: settings.PreferredCountry,
	})
	if err != nil {
		return nil, fmt.Errorf("provider search: %w", err)
	}

	out := make([]ManagerLookupResult, 0, len(results))
	for _, hit := range results {
		view := ManagerLookupResult{
			ProviderName: hit.ProviderName, ProviderID: hit.ProviderID,
			Title: hit.Title, Year: hit.Year, Description: hit.Description,
			PosterURL: hit.PosterURL, HeyaSlug: hit.HeyaSlug, ExternalIDs: hit.ExternalIDs,
		}
		// Already-in-library only when confidently resolvable: match by
		// heya_slug, else by any shared external id.
		if hit.HeyaSlug != "" {
			var existing int64
			err := a.db.QueryRow(ctx,
				`SELECT id FROM media_items WHERE library_id = $1 AND heya_slug = $2`,
				libraryID, hit.HeyaSlug).Scan(&existing)
			if err == nil {
				view.AlreadyInLibrary = true
				view.ExistingItemID = existing
			}
		}
		if !view.AlreadyInLibrary && len(hit.ExternalIDs) > 0 {
			for provider, id := range hit.ExternalIDs {
				var existing int64
				err := a.db.QueryRow(ctx, `
					SELECT media_item_id FROM media_item_external_ids
					WHERE library_id = $1 AND provider = $2 AND external_id = $3`,
					libraryID, strings.ToLower(provider), id).Scan(&existing)
				if err == nil {
					view.AlreadyInLibrary = true
					view.ExistingItemID = existing
					break
				}
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// ManagerAddInput describes the chosen lookup result to add.
type ManagerAddInput struct {
	LibraryID        int64             `json:"library_id"`
	Title            string            `json:"title" minLength:"1"`
	Year             string            `json:"year,omitempty"`
	Description      string            `json:"description,omitempty"`
	PosterURL        string            `json:"poster_url,omitempty"`
	HeyaSlug         string            `json:"heya_slug,omitempty"`
	ExternalIDs      map[string]string `json:"external_ids,omitempty"`
	Monitored        bool              `json:"monitored"`
	QualityProfileID int64             `json:"quality_profile_id,omitempty"`
}

type ManagerAddResult struct {
	MediaItemID int64  `json:"media_item_id"`
	LibraryID   int64  `json:"library_id"`
	Title       string `json:"title"`
	RunID       int64  `json:"run_id"`
}

// ManagerAdd creates a fileless (announced) media item from a lookup
// result: hidden from public browse until real files materialize, visible
// and searchable in the manager immediately. Metadata fills through the
// normal enrichment pipeline (the same forced-enrich the metadata editor
// uses); the scanner adopts the row by (library_id, heya_slug) when files
// eventually land, and the materialization triggers flip visibility.
func (a *App) ManagerAdd(ctx context.Context, input ManagerAddInput) (*ManagerAddResult, error) {
	q := sqlc.New(a.db)
	lib, err := q.GetLibraryByID(ctx, input.LibraryID)
	if err != nil {
		return nil, fmt.Errorf("library %d: %w", input.LibraryID, err)
	}
	if input.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	// Idempotency: an existing row with the same provider identity is a
	// conflict, not a duplicate insert.
	if input.HeyaSlug != "" {
		var existing int64
		if err := a.db.QueryRow(ctx,
			`SELECT id FROM media_items WHERE library_id = $1 AND heya_slug = $2`,
			input.LibraryID, input.HeyaSlug).Scan(&existing); err == nil {
			return nil, fmt.Errorf("already in the library (item %d)", existing)
		}
	}

	heyaSlug := input.HeyaSlug
	if heyaSlug == "" {
		// No canonical identity from the provider (cold hit): a
		// deterministic title+year fingerprint keeps scanner adoption
		// possible for same-named folders.
		heyaSlug = fmt.Sprintf("manager:%s", slug.Generate(input.Title, input.Year))
	}
	externalIDs := map[string]string{}
	for provider, id := range input.ExternalIDs {
		externalIDs[strings.ToLower(provider)] = id
	}
	idsDoc, _ := json.Marshal(externalIDs)

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback-on-error path
	qtx := sqlc.New(tx)

	item, err := qtx.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: input.LibraryID, MediaType: lib.MediaType,
		ProviderKind: "heyametadata", HeyaSlug: heyaSlug,
		Title: input.Title, SortTitle: input.Title, Year: input.Year,
		Description: input.Description, PosterPath: input.PosterURL,
		ExternalIds: idsDoc,
	})
	if err != nil {
		return nil, fmt.Errorf("creating media item: %w", err)
	}

	// Announced: hidden from public surfaces until files materialize; the
	// manager owns it meanwhile. (CreateMediaItem predates the column, so
	// the DEFAULT stamped it — clear it explicitly.)
	if _, err := tx.Exec(ctx, `
		UPDATE media_items
		SET materialized_at = NULL, added_source = 'manager',
		    monitored = $2, quality_profile_id = NULLIF($3, 0)
		WHERE id = $1`,
		item.ID, input.Monitored, input.QualityProfileID); err != nil {
		return nil, fmt.Errorf("marking announced: %w", err)
	}

	publicSlug := slug.GenerateUnique(ctx, input.Title, input.Year, item.ID,
		func(ctx context.Context, s string, excludeID int64) (bool, error) {
			return qtx.MediaItemSlugExists(ctx, sqlc.MediaItemSlugExistsParams{Slug: s, ID: excludeID})
		})
	if err := qtx.UpdateMediaItemSlug(ctx, sqlc.UpdateMediaItemSlugParams{ID: item.ID, Slug: publicSlug}); err != nil {
		return nil, fmt.Errorf("setting slug: %w", err)
	}

	for provider, id := range externalIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO media_item_external_ids (media_item_id, library_id, provider, external_id, source)
			VALUES ($1, $2, $3, $4, 'manager')
			ON CONFLICT (media_item_id, provider) DO UPDATE SET external_id = EXCLUDED.external_id`,
			item.ID, input.LibraryID, provider, id); err != nil {
			return nil, fmt.Errorf("stamping external id %s: %w", provider, err)
		}
	}

	// Music: enrichment requires the artist row to exist (enrichMusic fails
	// the item otherwise); create it in the same transaction.
	if lib.MediaType == sqlc.MediaTypeMusic {
		if _, err := tx.Exec(ctx, `
			INSERT INTO artists (media_item_id, name, sort_name, musicbrainz_id)
			VALUES ($1, $2, $2, COALESCE($3, ''))`,
			item.ID, input.Title, externalIDs["musicbrainz"]); err != nil {
			return nil, fmt.Errorf("creating artist row: %w", err)
		}
	}

	// The add itself is an accountability event.
	scope, _ := json.Marshal(map[string]any{
		"media_item_id": item.ID, "title": input.Title, "year": input.Year,
		"library_id": input.LibraryID, "monitored": input.Monitored,
	})
	run, err := qtx.CreateManagerRun(ctx, sqlc.CreateManagerRunParams{
		Kind: "add", Source: "api", Scope: scope,
	})
	if err != nil {
		return nil, fmt.Errorf("recording add run: %w", err)
	}
	stats, _ := json.Marshal(map[string]any{"created_item": item.ID})
	if _, err := qtx.FinishManagerRun(ctx, sqlc.FinishManagerRunParams{
		ID: run.ID, Status: "completed", Stats: stats, Errors: []byte("[]"),
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Structure metadata (episodes / discography / artwork) fills through
	// the normal forced-enrich pipeline — the same path the metadata editor
	// uses after re-identify. Failure leaves enrichment_status resumable via
	// the ordinary refresh flow.
	if err := worker.EnqueueEnrichForce(ctx, a.river, item.ID, item.MediaType, worker.EnrichSourceForced); err != nil {
		log.Warn().Err(err).Int64("item", item.ID).Msg("manager add: enrich enqueue failed (retry via media refresh)")
	}

	a.notifyManagerChanged(ctx, "library")
	return &ManagerAddResult{
		MediaItemID: item.ID, LibraryID: input.LibraryID, Title: input.Title, RunID: run.ID,
	}, nil
}
