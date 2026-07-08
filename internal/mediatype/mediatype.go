// Package mediatype centralizes the rule that a library's declared media type
// and its runtime behavior can differ. Today the only such case is anime: a
// library may be declared 'anime' (a domain signal the v2 scanner will use),
// but every runtime path — matcher, enrich, storage, Jellyfin — treats it as
// 'tv'. Matched media_items are therefore always persisted as 'tv', never
// 'anime'. Keep the anime → tv mapping here so callers don't hand-roll it.
package mediatype

import "github.com/karbowiak/heya/internal/database/sqlc"

// Runtime maps a library's declared media type to the type its content is
// actually stored and queried under. Anime collapses to tv; everything else is
// identity. Use this whenever a library's media_type feeds a media_item query
// or kind decision — media_items never carry 'anime'.
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
