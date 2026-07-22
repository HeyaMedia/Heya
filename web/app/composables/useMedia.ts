import { reactive } from 'vue'

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
    /** Durable server-side artwork revision. Most media DTOs already expose
     *  updated_at; images_enriched_at is preferred where available. */
    image_revision?: number | string | null
    images_enriched_at?: string | null
    updated_at?: string | null
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

// Image routes are stable by design, but an explicit metadata-editor choice
// replaces the bytes behind that route. Keep a tiny client-side revision per
// local/public identity so every visible consumer receives a new URL as soon
// as the selected image has landed—without reloading the page.
const mediaImageRevisions = reactive(new Map<string, number>())

export function bumpMediaImageRevision(...refs: MediaImageRef[]) {
  for (const ref of refs) {
    const key = useMediaImageKey(ref)
    if (key) mediaImageRevisions.set(key, (mediaImageRevisions.get(key) || 0) + 1)
  }
}

function durableMediaImageRevision(ref: MediaImageRef) {
  if (ref == null || typeof ref === 'number' || typeof ref === 'string') return ''
  const revision = ref.image_revision ?? ref.images_enriched_at ?? ref.updated_at
  return revision == null ? '' : String(revision)
}

export function withMediaImageRevision(url: string, ref: MediaImageRef) {
  const key = useMediaImageKey(ref)
  const localRevision = key ? (mediaImageRevisions.get(key) || 0) : 0
  const durableRevision = durableMediaImageRevision(ref)
  if (!localRevision && !durableRevision) return url
  const separator = url.includes('?') ? '&' : '?'
  return `${url}${separator}image_revision=${encodeURIComponent(`${durableRevision}:${localRevision}`)}`
}

export function metadataImageProxyUrl(source: string | null | undefined) {
  if (!source) return ''
  const match = source.match(/\/api\/v2\/images\/([0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12})(?=[/?#]|$)/i)
  return match?.[1] ? `/api/metadata/images/${match[1]}` : source
}

export function useImageUrl(media: MediaImageRef, type: MediaImageType) {
  const key = useMediaImageKey(media)
  if (!key) return null
  return withMediaImageRevision(`/api/media/${key}/image/${type}`, media)
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

/** Canonical route for a TV/anime season. Season zero is exposed as the
 * human-readable `specials` segment used by the season and episode pages. */
export function seasonDetailUrl(item: { slug: string; season_number: number }): string {
  const season = item.season_number === 0 ? 'specials' : String(item.season_number)
  return `/tv/${item.slug}/season/${season}`
}

/** Canonical route for a TV/anime episode. */
export function episodeUrl(item: { slug: string; season_number: number; episode_number: number }): string {
  return `${seasonDetailUrl(item)}/episode/${item.episode_number}`
}

/** Destination for a grouped Recently Added TV event. A singular season or
 * episode has its own page; show and multi-episode summary cards stay at the
 * series level. */
export function recentTVEntryUrl(item: {
  id: number
  title: string
  slug: string
  media_type: string
  kind: 'series' | 'season' | 'episodes' | 'episode'
  season_number: number
  episode_number: number
}): string {
  if (item.kind === 'episode') return episodeUrl(item)
  if (item.kind === 'season') return seasonDetailUrl(item)
  return mediaUrl(item)
}

// Public provider page for a title we don't have locally. HeyaMetadata's UUID
// is machine identity, not a user-facing web route, so unavailable library
// recommendations link to their strongest public source instead.
export function externalProviderUrl(mediaType: string, externalIds?: Record<string, string> | null): string {
  const ids = externalIds ?? {}
  if (ids.imdb) return `https://www.imdb.com/title/${ids.imdb}/`
  if (ids.tmdb) {
    const kind = mediaType === 'movie' ? 'movie' : 'tv'
    return `https://www.themoviedb.org/${kind}/${ids.tmdb}`
  }
  if (ids.tvdb) return `https://thetvdb.com/dereferrer/series/${ids.tvdb}`
  if (ids.anidb) return `https://anidb.net/anime/${ids.anidb}`
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
