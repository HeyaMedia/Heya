package service

import (
	"context"
	"encoding/json"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func (a *App) GetSystemSetting(ctx context.Context, key string) (json.RawMessage, error) {
	q := sqlc.New(a.db)
	return q.GetSystemSetting(ctx, key)
}

func (a *App) SetSystemSetting(ctx context.Context, key string, value json.RawMessage) error {
	q := sqlc.New(a.db)
	return q.UpsertSystemSetting(ctx, sqlc.UpsertSystemSettingParams{Key: key, Value: value})
}
