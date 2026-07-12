// The one way to turn a playlist list-row into a renderable cover URL.
//
// `cover_path` on the row is the custom cover's DISK path (server bookkeeping,
// never renderable) — the servable bytes live behind the cover endpoint, keyed
// by has_cover. Without a custom cover, the generated representation is the
// first track's album cover, addressed canonically by the (artist, album)
// slug pair (image URLs are unconditional — no cover_path gating).
export function playlistCoverSrc(p: {
  id: number
  has_cover?: boolean
  updated_at?: string
  auto_artist_slug?: string
  auto_album_slug?: string
}): string | null {
  if (p.has_cover) {
    // updated_at busts stale caches after a cover replace.
    const v = p.updated_at ? (Date.parse(p.updated_at) || 0) : 0
    return `/api/me/playlists/${p.id}/cover?v=${v}`
  }
  if (p.auto_artist_slug && p.auto_album_slug) {
    return useAlbumCoverUrl(p.auto_artist_slug, p.auto_album_slug)
  }
  return null
}
