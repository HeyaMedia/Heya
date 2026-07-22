-- name: ListScheduledTasks :many
SELECT * FROM scheduled_tasks ORDER BY category, display_name;

-- name: GetScheduledTask :one
SELECT * FROM scheduled_tasks WHERE id = $1;

-- name: UpdateScheduledTaskConfig :one
UPDATE scheduled_tasks
SET enabled = $2, interval_hours = $3,
    daily_start_time = $4, daily_end_time = $5,
    max_runtime_minutes = $6, next_run_at = $7, updated_at = now()
WHERE id = $1 RETURNING *;

-- name: UpdateScheduledTaskRun :exec
UPDATE scheduled_tasks
SET last_run_at = $2, last_run_result = $3, last_run_duration_sec = $4,
    last_run_items_processed = $5, last_run_items_total = $6,
    next_run_at = $7, last_run_error = $8, updated_at = now()
WHERE id = $1;
