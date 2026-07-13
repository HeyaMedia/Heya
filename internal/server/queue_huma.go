package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/service"
)

// registerQueueRoutes mounts the server-owned play queue
// (docs/queue-plan.md): one queue per user under /api/me/queue, windowed
// reads, every mutation mirrored to the user's clients over the
// queue.changed WS event. All routes are per-user (secured, not admin).
func registerQueueRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodGet, "/api/me/queue", "queue-get", "The user's play queue (windowed)", "Queue")),
		func(ctx context.Context, in *struct {
			Around int64 `query:"around" doc:"Window anchor ord; 0 anchors on the current item"`
			Limit  int   `query:"limit" doc:"Window size (default 100, max 500)"`
		}) (*JSONOutput[service.QueueView], error) {
			var around *int64
			if in.Around != 0 {
				around = &in.Around
			}
			view, err := app.GetQueue(ctx, userFrom(ctx).ID, around, in.Limit)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(view), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/queue", "queue-replace", "Replace the queue from a source and start playing", "Queue")),
		func(ctx context.Context, in *struct {
			Body struct {
				Source       service.QueueSource `json:"source"`
				StartTrackID int64               `json:"start_track_id,omitempty" doc:"Track to point at first (0 = head)"`
				Shuffle      bool                `json:"shuffle,omitempty"`
				Output       string              `json:"output,omitempty" doc:"Claiming output id, e.g. local:<client_id>"`
			}
		}) (*JSONOutput[service.QueueView], error) {
			view, err := app.ReplaceQueue(ctx, userFrom(ctx).ID, in.Body.Source, in.Body.StartTrackID, in.Body.Shuffle, in.Body.Output)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return noStoreJSON(view), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/queue", "queue-clear", "Clear the queue", "Queue")),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			if err := app.ClearQueue(ctx, userFrom(ctx).ID); err != nil {
				return nil, humaServiceError(err)
			}
			return &struct{}{}, nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/queue/upcoming", "queue-clear-upcoming", "Drop everything after the current track", "Queue")),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			if err := app.ClearUpcoming(ctx, userFrom(ctx).ID); err != nil {
				return nil, humaServiceError(err)
			}
			return &struct{}{}, nil
		})

	type addedBody struct {
		Added int64 `json:"added"`
	}
	huma.Register(api, secured(op(http.MethodPost, "/api/me/queue/items", "queue-enqueue", "Append or play-next tracks", "Queue")),
		func(ctx context.Context, in *struct {
			Body struct {
				TrackIDs []int64 `json:"track_ids" minItems:"1"`
				At       string  `json:"at,omitempty" enum:"end,next" default:"end"`
			}
		}) (*JSONOutput[addedBody], error) {
			n, err := app.EnqueueTracks(ctx, userFrom(ctx).ID, in.Body.TrackIDs, in.Body.At)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return noStoreJSON(addedBody{Added: n}), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/queue/items/{id}", "queue-remove-item", "Remove one queue item", "Queue")),
		func(ctx context.Context, in *struct {
			ID int64 `path:"id"`
		}) (*struct{}, error) {
			if err := app.RemoveQueueItem(ctx, userFrom(ctx).ID, in.ID); err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return &struct{}{}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/queue/items/{id}/move", "queue-move-item", "Reorder one queue item", "Queue")),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				AfterItemID int64 `json:"after_item_id,omitempty" doc:"Place after this item (0 = right after the current track)"`
			}
		}) (*struct{}, error) {
			if err := app.MoveQueueItem(ctx, userFrom(ctx).ID, in.ID, in.Body.AfterItemID); err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return &struct{}{}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/queue/jump", "queue-jump", "Point the queue at an item", "Queue")),
		func(ctx context.Context, in *struct {
			Body struct {
				ItemID int64 `json:"item_id" minimum:"1"`
			}
		}) (*JSONOutput[service.QueueView], error) {
			view, err := app.JumpToQueueItem(ctx, userFrom(ctx).ID, in.Body.ItemID)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return noStoreJSON(view), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/queue/advance", "queue-advance", "Report a track boundary (renderer)", "Queue")),
		func(ctx context.Context, in *struct {
			Body struct {
				FromItemID int64  `json:"from_item_id" minimum:"1" doc:"The item this renderer just finished/skipped — makes double-fires no-ops"`
				Reason     string `json:"reason" enum:"ended,skip,prev" default:"ended"`
			}
		}) (*JSONOutput[service.QueueView], error) {
			view, err := app.AdvanceQueue(ctx, userFrom(ctx).ID, in.Body.FromItemID, in.Body.Reason)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return noStoreJSON(view), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/queue/shuffle", "queue-shuffle", "Toggle server-side shuffle", "Queue")),
		func(ctx context.Context, in *struct {
			Body struct {
				On bool `json:"on"`
			}
		}) (*struct{}, error) {
			if err := app.SetQueueShuffle(ctx, userFrom(ctx).ID, in.Body.On); err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return &struct{}{}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/queue/repeat", "queue-repeat", "Set the repeat mode", "Queue")),
		func(ctx context.Context, in *struct {
			Body struct {
				Mode string `json:"mode" enum:"off,all,one"`
			}
		}) (*struct{}, error) {
			if err := app.SetQueueRepeat(ctx, userFrom(ctx).ID, in.Body.Mode); err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return &struct{}{}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/queue/heartbeat", "queue-heartbeat", "Renderer position heartbeat", "Queue")),
		func(ctx context.Context, in *struct {
			Body struct {
				Output          string  `json:"output" minLength:"1" doc:"This renderer's output id"`
				PositionSeconds float64 `json:"position_seconds" minimum:"0"`
				Playing         bool    `json:"playing"`
			}
		}) (*struct{}, error) {
			err := app.QueueHeartbeat(ctx, userFrom(ctx).ID, in.Body.Output, in.Body.PositionSeconds, in.Body.Playing)
			if errors.Is(err, service.ErrQueueNotActiveOutput) {
				return nil, huma.Error409Conflict("another output owns playback")
			}
			if err != nil {
				return nil, humaServiceError(err)
			}
			return &struct{}{}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/queue/claim", "queue-claim", "Become the active output", "Queue")),
		func(ctx context.Context, in *struct {
			Body struct {
				Output string `json:"output" minLength:"1" doc:"local:<client_id> or cast:<device_id>"`
			}
		}) (*struct{}, error) {
			if err := app.ClaimQueueOutput(ctx, userFrom(ctx).ID, in.Body.Output); err != nil {
				return nil, humaServiceError(err)
			}
			return &struct{}{}, nil
		})
}
