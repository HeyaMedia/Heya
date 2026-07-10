export type MediaImageType = 'poster' | 'backdrop' | 'still' | 'logo' | 'banner' | 'clearart' | 'thumb'

export type MediaImageRef =
  | number
  | string
  | null
  | undefined
  | {
    id?: number | string | null
    public_id?: string | null
    media_item_id?: number | string | null
    media_item_public_id?: string | null
    local_media_item_id?: number | string | null
    local_public_id?: string | null
  }

export function useMediaImageKey(ref: MediaImageRef) {
  if (ref == null || ref === '') return null
  if (typeof ref === 'number' || typeof ref === 'string') return String(ref)
  const key = ref.public_id
    ?? ref.media_item_public_id
    ?? ref.local_public_id
    ?? ref.id
    ?? ref.media_item_id
    ?? ref.local_media_item_id
  return key == null || key === '' ? null : String(key)
}

export function useImageUrl(media: MediaImageRef, type: MediaImageType) {
  const key = useMediaImageKey(media)
  if (!key) return null
  return `/api/media/${key}/image/${type}`
}

export function usePosterUrl(media: MediaImageRef) {
  return useImageUrl(media, 'poster')
}

export function useBackdropUrl(media: MediaImageRef) {
  return useImageUrl(media, 'backdrop')
}

// useAlbumCoverUrl returns the canonical album-cover URL. Use this instead
// of binding `album.cover_path` directly — the raw column may hold a
// `data/...` filesystem path (the Nuxt router treats those as routes and
// renders the SPA shell) or an upstream URL. The endpoint resolves both:
// serves the local file when present, 302-redirects to upstream otherwise.
//
// Takes (artist_slug, album_slug) since album slugs are scoped to an artist
// — the two together address an album uniquely without leaking numeric IDs.
// Every music list row carries both fields; pass them straight through.
export function useAlbumCoverUrl(artistSlug: string | undefined, albumSlug: string | undefined) {
  if (!artistSlug || !albumSlug) return null
  return `/api/music/artists/${artistSlug}/albums/${albumSlug}/cover`
}

export function mediaTypeColor(type: string) {
  const colors: Record<string, string> = {
    movie: 'text-heya-movie',
    tv: 'text-heya-tv',
    anime: 'text-heya-tv',
    music: 'text-heya-music',
    book: 'text-heya-book',
  }
  return colors[type] || 'text-gray-400'
}

export function mediaTypeBg(type: string) {
  const colors: Record<string, string> = {
    movie: 'bg-heya-movie/20 text-heya-movie',
    tv: 'bg-heya-tv/20 text-heya-tv',
    anime: 'bg-heya-tv/20 text-heya-tv',
    music: 'bg-heya-music/20 text-heya-music',
    book: 'bg-heya-book/20 text-heya-book',
  }
  return colors[type] || 'bg-gray-500/20 text-gray-400'
}

export function slugify(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

export function mediaUrl(item: { id: number; public_id?: string; title: string; year?: string; media_type: string; slug?: string }): string {
  // Music artists live under /music/artist/{slug} (siblings of /music/albums,
  // /music/artists, etc.) — keep them out of the typeMap so the slash path
  // doesn't collide with the page-level routes.
  const typeMap: Record<string, string> = {
    movie: 'movies',
    tv: 'tv',
    anime: 'tv',
    music: 'music/artist',
    book: 'books',
  }
  const prefix = typeMap[item.media_type] || 'media'
  const s = item.slug || slugify(item.title) + (item.year ? '-' + item.year : '')
  return `/${prefix}/${s}`
}

// External heya.media page for a title we don't have locally. The aggregator
// fetches on demand from /heya_{kind}:{provider}:{value} paths (same
// construction as the scanner review's upstream link) — so a recommendation
// with any strong provider id can open there in a new tab. Empty string when
// no usable id exists (caller renders an inert card).
export function heyaMediaExternalUrl(mediaType: string, externalIds?: Record<string, string> | null): string {
  const kind = mediaType === 'tv' || mediaType === 'anime' ? 'tv' : mediaType
  for (const provider of ['tmdb', 'imdb', 'tvdb'] as const) {
    const value = externalIds?.[provider]
    if (value) return `https://heya.media/heya_${kind}:${provider}:${value}`
  }
  return ''
}

export function personUrl(person: { id: number; name: string; slug?: string }): string {
  // A person's stable `slug` is only minted by the deep-fetch worker, which is
  // now lazy (runs on first person-page view). Before that lands the slug is
  // empty, and a name-guessed slug won't resolve — so fall back to the numeric
  // id, which GetPerson resolves directly (ParseInt path). Clicking a cast card
  // for an un-deep-fetched person then loads the page AND triggers its backfill.
  const s = person.slug || String(person.id)
  return `/person/${s}`
}

export function mediaTypeLabel(type: string) {
  const labels: Record<string, string> = {
    movie: 'Movie',
    tv: 'TV Show',
    anime: 'Anime',
    music: 'Music',
    book: 'Book',
  }
  return labels[type] || type
}
