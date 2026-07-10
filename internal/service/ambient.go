package service

import (
	"context"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// AmbientBackdropItem is one candidate image for the app's ambient
// rotating-backdrop background. HasBackdrop=false means the item only has
// a poster (common for books) and the FE should request that instead.
type AmbientBackdropItem struct {
	ID          int64  `json:"id"`
	PublicID    string `json:"public_id"`
	MediaType   string `json:"media_type"`
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	HasBackdrop bool   `json:"has_backdrop"`
}

// SampleAmbientBackdrops returns up to limit random media items of the given
// types that have usable artwork.
func (a *App) SampleAmbientBackdrops(ctx context.Context, types []string, limit int32) ([]AmbientBackdropItem, error) {
	q := sqlc.New(a.db)
	rows, err := q.SampleAmbientBackdrops(ctx, sqlc.SampleAmbientBackdropsParams{
		MediaTypes: types,
		MaxItems:   limit,
	})
	if err != nil {
		return nil, err
	}

	items := make([]AmbientBackdropItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, AmbientBackdropItem{
			ID:          r.ID,
			PublicID:    r.PublicID.String(),
			MediaType:   string(r.MediaType),
			Title:       r.Title,
			Slug:        r.Slug,
			HasBackdrop: r.HasBackdrop.Bool,
		})
	}
	return items, nil
}
