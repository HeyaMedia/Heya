-- name: CreateAuthor :one
INSERT INTO authors (name, openlibrary_id, biography, birth_date, death_date)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAuthorByID :one
SELECT * FROM authors WHERE id = $1;

-- name: GetAuthorByName :one
SELECT * FROM authors WHERE lower(name) = lower($1);

-- name: GetAuthorByOpenLibraryID :one
SELECT * FROM authors WHERE openlibrary_id = $1;

-- name: UpdateAuthor :one
UPDATE authors
SET name = $2, openlibrary_id = $3, biography = $4, birth_date = $5, death_date = $6
WHERE id = $1
RETURNING *;

-- name: CreateBook :one
INSERT INTO books (media_item_id, author_id, isbn, openlibrary_id, page_count, publisher, publish_date, file_path,
    subjects, language, series_name, series_number, format, description)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: GetBookByMediaItemID :one
SELECT * FROM books WHERE media_item_id = $1;

-- name: GetBookByISBN :one
SELECT * FROM books WHERE isbn = $1 AND isbn != '';

-- name: ListBooksByAuthor :many
SELECT * FROM books WHERE author_id = $1 ORDER BY publish_date ASC;

-- name: UpdateBook :one
UPDATE books
SET author_id = $2, isbn = $3, openlibrary_id = $4, page_count = $5,
    publisher = $6, publish_date = $7, file_path = $8,
    subjects = $9, language = $10, series_name = $11, series_number = $12, format = $13, description = $14
WHERE id = $1
RETURNING *;
