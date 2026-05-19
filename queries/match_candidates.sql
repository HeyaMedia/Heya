-- name: CreateMatchCandidate :one
INSERT INTO match_candidates (library_file_id, provider_name, provider_id, title, year, description, poster_url, confidence, raw_data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (library_file_id, provider_id) DO UPDATE
SET title = EXCLUDED.title, year = EXCLUDED.year, description = EXCLUDED.description,
    poster_url = EXCLUDED.poster_url, confidence = EXCLUDED.confidence, raw_data = EXCLUDED.raw_data
RETURNING *;

-- name: ListMatchCandidatesByFile :many
SELECT * FROM match_candidates
WHERE library_file_id = $1
ORDER BY confidence DESC;

-- name: GetMatchCandidateByID :one
SELECT * FROM match_candidates WHERE id = $1;

-- name: ChooseMatchCandidate :exec
UPDATE match_candidates
SET chosen = (id = @chosen_id)
WHERE library_file_id = @library_file_id;

-- name: DeleteMatchCandidatesByFile :exec
DELETE FROM match_candidates WHERE library_file_id = $1;

-- name: CountUnmatchedWithCandidates :one
SELECT count(DISTINCT lf.id)
FROM library_files lf
JOIN match_candidates mc ON mc.library_file_id = lf.id
WHERE lf.library_id = $1 AND lf.status = 'unmatched';
