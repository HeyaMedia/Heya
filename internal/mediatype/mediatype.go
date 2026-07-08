// Package mediatype centralizes the rule that a library's declared media type
// and its runtime behavior can differ. Today the main case is anime: a library
// may be declared 'anime' as a domain signal, while most runtime behavior still
// uses the TV pipeline for seasons, episodes, enrichment, and playback.
package mediatype

import "github.com/karbowiak/heya/internal/database/sqlc"

// Runtime maps a library's declared media type to the type its content is
// actually queried under when callers need the shared TV pipeline. Anime
// collapses to tv; everything else is identity.
func Runtime(mt sqlc.MediaType) sqlc.MediaType {
	if mt == sqlc.MediaTypeAnime {
		return sqlc.MediaTypeTv
	}
	return mt
}

// IsTVLike reports whether a library type is handled by the TV pipeline
// (episodes, seasons, absolute-numbering reconciliation).
func IsTVLike(mt sqlc.MediaType) bool {
	return mt == sqlc.MediaTypeTv || mt == sqlc.MediaTypeAnime
}

// IsVideo reports whether a library type holds playable video (movies or any
// TV-like collection).
func IsVideo(mt sqlc.MediaType) bool {
	return mt == sqlc.MediaTypeMovie || IsTVLike(mt)
}
