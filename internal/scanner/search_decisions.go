package scanner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type SearchDecision struct {
	Key         string            `json:"key"`
	Status      string            `json:"status"`
	ProviderID  string            `json:"provider_id,omitempty"`
	Provider    string            `json:"provider,omitempty"`
	Title       string            `json:"title,omitempty"`
	Year        string            `json:"year,omitempty"`
	Confidence  float64           `json:"confidence,omitempty"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
}

type SearchDecisions map[string]SearchDecision

// scannerSearchMatcherRevision versions automatic accept decisions. Bump it
// whenever normalization, candidate scoring, evidence weighting, or automatic
// acceptance policy changes in a way that should reconsider prior matches.
// Manual approve/reject/ignore decisions are revision-independent.
const scannerSearchMatcherRevision int32 = 4

func LoadScannerSearchDecisions(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library) (SearchDecisions, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := sqlc.New(db).ListScannerSearchDecisionsByLibrary(ctx, sqlc.ListScannerSearchDecisionsByLibraryParams{
		LibraryID:       lib.ID,
		MediaType:       lib.MediaType,
		ReviewStatuses:  []string{"accepted", "rejected", "ignored"},
		MatcherRevision: scannerSearchMatcherRevision,
	})
	if err != nil {
		return nil, fmt.Errorf("load scanner search decisions: %w", err)
	}
	decisions := make(SearchDecisions, len(rows))
	for _, row := range rows {
		decision := SearchDecision{
			Key:         row.IdentityKey,
			Status:      row.ReviewStatus,
			ProviderID:  firstNonEmpty(row.ProviderID, row.MetadataProviderID),
			Provider:    firstNonEmpty(row.ProviderName, "heya"),
			Title:       row.Title,
			Year:        row.Year,
			Confidence:  numericFloat64(row.Score),
			ExternalIDs: jsonStringMap(row.ExternalIds),
		}
		if decision.Status == "accepted" && decision.ProviderID == "" {
			continue
		}
		if decision.Status == "accepted" && decision.Confidence == 0 {
			decision.Confidence = 1
		}
		decisions[decision.Key] = decision
	}
	return decisions, nil
}

func optionalSearchDecisions(values []SearchDecisions) SearchDecisions {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

func jsonStringMap(data []byte) map[string]string {
	if len(data) == 0 {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func numericFloat64(value pgtype.Numeric) float64 {
	if !value.Valid {
		return 0
	}
	fv, err := value.Float64Value()
	if err != nil || !fv.Valid {
		return 0
	}
	return fv.Float64
}
