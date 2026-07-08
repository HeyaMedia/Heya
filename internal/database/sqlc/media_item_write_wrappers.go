package sqlc

import "context"

type CreateMediaItemParams = CreateMediaItemRawParams
type UpdateMediaItemParams = UpdateMediaItemRawParams

func (q *Queries) CreateMediaItem(ctx context.Context, arg CreateMediaItemParams) (MediaItemCard, error) {
	id, err := q.CreateMediaItemRaw(ctx, arg)
	if err != nil {
		return MediaItemCard{}, err
	}
	return q.GetMediaItemByID(ctx, id)
}

func (q *Queries) UpdateMediaItem(ctx context.Context, arg UpdateMediaItemParams) (MediaItemCard, error) {
	id, err := q.UpdateMediaItemRaw(ctx, arg)
	if err != nil {
		return MediaItemCard{}, err
	}
	return q.GetMediaItemByID(ctx, id)
}
