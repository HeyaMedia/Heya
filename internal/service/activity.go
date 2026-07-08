package service

import (
	"context"
	"strconv"
	"time"
)

// ActivityItem represents a single entry in the activity feed.
type ActivityItem struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle,omitempty"`
	MediaID   int64     `json:"media_id,omitempty"`
	MediaType string    `json:"media_type,omitempty"`
	Slug      string    `json:"slug,omitempty"`
	ImageURL  string    `json:"image_url,omitempty"`
}

// GetActivityFeed returns recent media additions and scan completions from
// the last 7 days, sorted by timestamp descending, limited to 30 items.
func (a *App) GetActivityFeed(ctx context.Context) []ActivityItem {
	// Non-nil so the JSON encodes as [] not null — the FE feed does
	// items.length and a null payload crashes the dashboard render.
	items := make([]ActivityItem, 0)

	if mediaRows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.title, mi.media_type, mi.slug, mi.created_at
		FROM media_item_cards mi
		WHERE mi.created_at > now() - interval '7 days'
		ORDER BY mi.created_at DESC
		LIMIT 30`); err == nil {
		for mediaRows.Next() {
			var id int64
			var title, mediaType, slug string
			var createdAt time.Time
			if err := mediaRows.Scan(&id, &title, &mediaType, &slug, &createdAt); err != nil {
				continue
			}
			items = append(items, ActivityItem{
				Type:      "media_added",
				Timestamp: createdAt,
				Title:     title,
				Subtitle:  mediaType,
				MediaID:   id,
				MediaType: mediaType,
				Slug:      slug,
				ImageURL:  "/api/media/" + strconv.FormatInt(id, 10) + "/image/poster",
			})
		}
		mediaRows.Close()
	}

	if scanRows, err := a.db.Query(ctx, `
		SELECT rj.finalized_at, rj.args->>'library_id' AS lib_id
		FROM river_job rj
		WHERE rj.kind IN ('apply_library_scan', 'kickoff_library_scan') AND rj.state = 'completed'
		  AND rj.finalized_at > now() - interval '7 days'
		ORDER BY rj.finalized_at DESC
		LIMIT 10`); err == nil {
		libNames := map[string]string{}
		for scanRows.Next() {
			var finalizedAt time.Time
			var libID *string
			if err := scanRows.Scan(&finalizedAt, &libID); err != nil || libID == nil {
				continue
			}
			name, ok := libNames[*libID]
			if !ok {
				row := a.db.QueryRow(ctx, "SELECT name FROM libraries WHERE id = $1", *libID)
				if row.Scan(&name) != nil {
					name = "Library"
				}
				libNames[*libID] = name
			}
			items = append(items, ActivityItem{
				Type:      "scan_completed",
				Timestamp: finalizedAt,
				Title:     name,
				Subtitle:  "Scan completed",
			})
		}
		scanRows.Close()
	}

	// Sort all items by timestamp descending (insertion sort, small list)
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Timestamp.After(items[j-1].Timestamp); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}

	if len(items) > 30 {
		items = items[:30]
	}

	return items
}
