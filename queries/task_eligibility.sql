-- Task-eligibility queries. All read the baseline eligibility views —
-- trickplay_eligible_files / thumbnail_eligible_extras — so the eligibility
-- predicate exists exactly once (in the view), and the Settings counts, the
-- task item listings, and the kickoff enqueues can never drift apart.

-- name: CountTrickplayEligible :one
SELECT count(*)::int AS total,
       (count(*) FILTER (WHERE has_trickplay))::int AS complete
FROM trickplay_eligible_files;

-- name: ListTrickplayEligibleItems :many
-- status: '' = all, 'complete', or 'pending'.
SELECT id, path, has_trickplay
FROM trickplay_eligible_files
WHERE (@status::text = ''
   OR (@status::text = 'complete' AND has_trickplay)
   OR (@status::text = 'pending' AND NOT has_trickplay))
ORDER BY has_trickplay ASC, path ASC
LIMIT @row_limit OFFSET @row_offset;

-- name: ListTrickplayPendingKickoff :many
SELECT id, path
FROM trickplay_eligible_files
WHERE NOT has_trickplay;

-- name: CountThumbnailEligible :one
SELECT count(*)::int AS total,
       (count(*) FILTER (WHERE thumbnail_path != ''))::int AS complete
FROM thumbnail_eligible_extras;

-- name: ListThumbnailEligibleItems :many
-- status: '' = all, 'complete', or 'pending'.
SELECT id, title, file_path, thumbnail_path, extra_type::text AS extra_type, media_title
FROM thumbnail_eligible_extras
WHERE (@status::text = ''
   OR (@status::text = 'complete' AND thumbnail_path != '')
   OR (@status::text = 'pending' AND thumbnail_path = ''))
ORDER BY (thumbnail_path = '') DESC, media_title ASC, title ASC
LIMIT @row_limit OFFSET @row_offset;

-- name: ListThumbnailPendingKickoff :many
SELECT id, title::text AS title, file_path
FROM thumbnail_eligible_extras
WHERE thumbnail_path = '';
