-- name: CreateUserList :one
INSERT INTO user_lists (user_id, name, description, list_type, filter_json, media_type)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateUserList :one
UPDATE user_lists SET name = $2, description = $3, filter_json = $4, icon = $5, updated_at = now()
WHERE id = $1 AND user_id = $6
RETURNING *;

-- name: DeleteUserList :exec
DELETE FROM user_lists WHERE id = $1 AND user_id = $2;

-- name: GetUserListByID :one
SELECT * FROM user_lists WHERE id = $1 AND user_id = $2;

-- name: ListUserLists :many
SELECT ul.*,
       (SELECT count(*) FROM user_list_items li WHERE li.list_id = ul.id)::int AS item_count
FROM user_lists ul
WHERE ul.user_id = $1
ORDER BY ul.updated_at DESC;

-- name: AddToList :one
-- Ownership-guarded: only inserts when list $1 belongs to user $3 (else 0 rows).
INSERT INTO user_list_items (list_id, media_item_id, sort_order)
SELECT $1, $2, COALESCE((SELECT max(sort_order) + 1 FROM user_list_items WHERE list_id = $1), 0)
WHERE EXISTS (SELECT 1 FROM user_lists WHERE id = $1 AND user_id = $3)
ON CONFLICT (list_id, media_item_id) DO NOTHING
RETURNING *;

-- name: RemoveFromList :exec
DELETE FROM user_list_items
WHERE list_id = $1 AND media_item_id = $2
  AND EXISTS (SELECT 1 FROM user_lists WHERE id = $1 AND user_id = $3);

-- name: ListItemsInList :many
SELECT mi.*
FROM media_item_cards mi
JOIN user_list_items li ON li.media_item_id = mi.id
WHERE li.list_id = $1
  AND EXISTS (SELECT 1 FROM user_lists WHERE id = $1 AND user_id = $2)
ORDER BY li.sort_order, li.added_at;

-- name: IsInList :one
SELECT EXISTS(
  SELECT 1 FROM user_list_items WHERE list_id = $1 AND media_item_id = $2
) AS in_list;

-- name: ListsContainingMedia :many
SELECT ul.id, ul.name
FROM user_lists ul
JOIN user_list_items li ON li.list_id = ul.id
WHERE ul.user_id = $1 AND li.media_item_id = $2;

-- name: ReorderListItem :exec
UPDATE user_list_items SET sort_order = $3
WHERE list_id = $1 AND media_item_id = $2
  AND EXISTS (SELECT 1 FROM user_lists WHERE id = $1 AND user_id = $4);
