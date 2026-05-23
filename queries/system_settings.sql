-- name: GetSystemSetting :one
SELECT value FROM system_settings WHERE key = $1;

-- name: UpsertSystemSetting :exec
INSERT INTO system_settings (key, value, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now();

-- name: DeleteSystemSetting :exec
DELETE FROM system_settings WHERE key = $1;
