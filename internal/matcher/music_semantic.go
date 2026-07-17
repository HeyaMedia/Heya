package matcher

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/musicsemantic"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// HydrateMissingMusicSemanticCatalog bootstraps existing libraries from the
// canonical recording IDs already persisted on local tracks and provider top
// tracks. Normal artist refreshes keep new rows current; this bounded sweep is
// the upgrade/backstop path and intentionally does not crawl artist graphs.
func (m *Matcher) HydrateMissingMusicSemanticCatalog(ctx context.Context, limit int) (int, error) {
	if m.heya == nil || limit <= 0 {
		return 0, nil
	}
	if limit > 2000 {
		limit = 2000
	}
	candidates, err := m.q.ListMusicCatalogHydrationCandidates(ctx, int32(limit))
	if err != nil {
		return 0, err
	}
	var hydrated atomic.Int64
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for _, row := range candidates {
		if !row.RecordingEntityID.Valid {
			continue
		}
		row := row
		entityID := uuid.UUID(row.RecordingEntityID.Bytes).String()
		group.Go(func() error {
			recording, fetchErr := m.heya.RecordingMetadata(groupCtx, entityID)
			if fetchErr != nil {
				log.Debug().Err(fetchErr).Str("recording", entityID).Msg("semantic catalog bootstrap fetch failed")
				return nil
			}
			top := metadata.TopTrackEntry{
				Rank: int(row.Rank), Provider: row.Provider, Title: row.Title,
				MBID: row.Mbid, RecordingEntityID: entityID,
				Playcount: row.Playcount, Listeners: row.Listeners, URL: row.Url,
			}
			if storeErr := m.storeRecordingSemanticMetadata(groupCtx, row.SourceArtistID, recording, &top); storeErr != nil {
				log.Warn().Err(storeErr).Str("recording", entityID).Msg("semantic catalog bootstrap store failed")
				return nil
			}
			hydrated.Add(1)
			return nil
		})
	}
	_ = group.Wait()

	// Similar-artist rows are artist-level evidence, so expand only a small
	// number per sweep and take a small top-track slice from each. Once those
	// recording IDs are known, similarity itself remains track-metadata KNN;
	// the related artist's name/bio never enters the embedding document.
	expansionLimit := min(24, max(4, limit/20))
	expansions, err := m.q.ListMusicCatalogArtistExpansionCandidates(ctx, int32(expansionLimit))
	if err != nil {
		return int(hydrated.Load()), err
	}
	expansionGroup, expansionCtx := errgroup.WithContext(ctx)
	expansionGroup.SetLimit(3)
	for _, expansion := range expansions {
		expansion := expansion
		expansionGroup.Go(func() error {
			detail, _, fetchErr := m.heya.FetchByKindID(expansionCtx, "artist", "musicbrainz:"+expansion.RelatedArtistMbid)
			if fetchErr != nil && expansion.RelatedArtistName != "" {
				if hit, searchErr := m.heya.SearchArtistBest(expansionCtx, expansion.RelatedArtistName); searchErr == nil && hit != nil {
					detail, fetchErr = m.heya.GetDetail(expansionCtx, hit.ID, nil)
				}
			}
			if fetchErr != nil || detail == nil {
				log.Debug().Err(fetchErr).Str("artist", expansion.RelatedArtistName).Msg("semantic catalog related-artist expansion failed")
				return nil
			}
			for index, top := range detail.ArtistTopTracks {
				if index >= 12 {
					break
				}
				if top.RecordingEntityID == "" {
					continue
				}
				recording, recordingErr := m.heya.RecordingMetadata(expansionCtx, top.RecordingEntityID)
				if recordingErr != nil {
					continue
				}
				if storeErr := m.storeRecordingSemanticMetadata(expansionCtx, expansion.SourceArtistID, recording, &top); storeErr != nil {
					log.Warn().Err(storeErr).Str("recording", top.RecordingEntityID).Msg("semantic catalog related track store failed")
					continue
				}
				hydrated.Add(1)
			}
			if detail.ArtistTopTracksLoaded || len(detail.ArtistTopTracks) > 0 {
				if markErr := m.q.MarkMusicCatalogArtistExpansion(
					expansionCtx,
					sqlc.MarkMusicCatalogArtistExpansionParams(expansion),
				); markErr != nil {
					log.Warn().Err(markErr).Str("artist", expansion.RelatedArtistName).Msg("mark semantic catalog artist expansion")
				}
			}
			return nil
		})
	}
	_ = expansionGroup.Wait()
	return int(hydrated.Load()), nil
}

// storeRecordingSemanticMetadata keeps canonical recording facts in the
// recommendation catalog independently of whether a playable track exists.
func (m *Matcher) storeRecordingSemanticMetadata(ctx context.Context, sourceArtistID int64, value metadata.RecordingMetadata, top *metadata.TopTrackEntry) error {
	entityID, err := uuid.Parse(value.CanonicalID)
	if err != nil {
		return err
	}
	facets := musicsemantic.FromRecording(value)
	provider, providerURL := "heyametadata", firstRecordingLink(value.Links)
	providerRank := int32(0)
	playcount, listeners := int64(0), int64(0)
	if top != nil {
		provider = firstNonEmptyString(top.Provider, provider)
		providerURL = firstNonEmptyString(top.URL, providerURL)
		providerRank = int32(top.Rank)
		playcount, listeners = top.Playcount, top.Listeners
		value.Title = firstNonEmptyString(value.Title, top.Title)
		value.RecordingMBID = firstNonEmptyString(value.RecordingMBID, top.MBID)
	}
	return m.q.UpsertMusicCatalogRecording(ctx, sqlc.UpsertMusicCatalogRecordingParams{
		RecordingEntityID:    entityID,
		RecordingMbid:        value.RecordingMBID,
		Title:                value.Title,
		ArtistName:           value.ArtistName,
		SourceArtistID:       pgtype.Int8{Int64: sourceArtistID, Valid: sourceArtistID > 0},
		Provider:             provider,
		ProviderRank:         providerRank,
		ProviderUrl:          providerURL,
		Playcount:            playcount,
		Listeners:            listeners,
		Genres:               facets.Genres,
		Tags:                 facets.Tags,
		Moods:                facets.Moods,
		Instrumentation:      facets.Instrumentation,
		VocalCharacteristics: facets.VocalCharacteristics,
		RecordingAttributes:  facets.RecordingAttributes,
	})
}

func firstRecordingLink(values []metadata.URLEntry) string {
	for _, preferred := range []string{"spotify", "apple_music", "deezer", "bandcamp", "lastfm"} {
		for _, value := range values {
			if strings.EqualFold(value.Type, preferred) && value.URL != "" {
				return value.URL
			}
		}
	}
	for _, value := range values {
		if value.URL != "" {
			return value.URL
		}
	}
	return ""
}
