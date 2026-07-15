import type { MediaItem, MediaType } from '~~/shared/types'
import { useQuery } from '@pinia/colada'
import { quickSearchQuery } from '~/queries/search'

export interface SearchPerson {
  id: number
  name: string
  slug: string
  profile_path: string
  cast_count: number
  crew_count: number
  popularity?: number | string
}

export interface SearchAlbum {
  id: number
  artist_id: number
  title: string
  year: string
  cover_path: string
  artist_media_item_id: number
  artist_media_item_public_id?: string
  artist_name: string
  artist_slug: string
}

export interface SearchTrack {
  id: number
  album_id: number
  title: string
  album_title: string
  album_cover_path: string
  artist_media_item_id: number
  artist_media_item_public_id?: string
  artist_name: string
  artist_slug: string
  disc_number: number
  track_number: number
  duration_ms: number
}

export interface SearchCollection {
  id: number
  name: string
  overview: string
  poster_path: string
  backdrop_path: string
}

export interface SearchBucket<T> {
  items: T[]
  total: number
}

export interface QuickSearchResponse {
  query: string
  buckets: {
    movies?: SearchBucket<MediaItem>
    tv?: SearchBucket<MediaItem>
    music?: SearchBucket<MediaItem>
    books?: SearchBucket<MediaItem>
    people?: SearchBucket<SearchPerson>
    albums?: SearchBucket<SearchAlbum>
    tracks?: SearchBucket<SearchTrack>
    collections?: SearchBucket<SearchCollection>
  }
}

export type SearchType =
  | MediaType
  | 'people'
  | 'albums'
  | 'tracks'
  | 'collections'

// Local debounced live search — wraps /api/search/quick. Latest-wins
// (drops earlier responses if a newer query has fired) and exposes an empty
// state once cleared.
export function useQuickSearch(debounceMs = 200) {
  const query = ref('')
  const requestQuery = ref('')
  const result = useQuery(() => ({
    ...quickSearchQuery(requestQuery.value),
    enabled: !!requestQuery.value,
  }))
  const data = computed<QuickSearchResponse | null>(() => requestQuery.value ? result.data.value ?? null : null)
  const loading = computed(() => !!requestQuery.value && result.isPending.value)
  const error = computed(() => result.error.value
    ? (result.error.value instanceof Error ? result.error.value.message : 'Search failed')
    : null)

  const debouncedFetch = useDebounceFn((value: string) => { requestQuery.value = value }, debounceMs)

  watch(query, (q) => {
    const trimmed = q.trim()
    if (!trimmed) {
      requestQuery.value = ''
      return
    }
    debouncedFetch(trimmed)
  })

  const isEmpty = computed(() => {
    if (!data.value) return true
    return Object.keys(data.value.buckets).length === 0
  })

  const totalHits = computed(() => {
    if (!data.value) return 0
    return Object.values(data.value.buckets).reduce(
      (sum, b) => sum + (b?.total ?? 0),
      0,
    )
  })

  function reset() {
    query.value = ''
    requestQuery.value = ''
  }

  return { query, data, loading, error, isEmpty, totalHits, reset }
}

// Paginated single-type search for the /search page.
export async function fetchSearch<T = any>(
  q: string,
  type: SearchType | '' = '',
  limit = 60,
  offset = 0,
): Promise<SearchBucket<T> | T[]> {
  const { $heya } = useNuxtApp()
  return $heya('/api/search', {
    query: { q, type: type || undefined, limit, offset } as any,
  }) as unknown as Promise<SearchBucket<T> | T[]>
}

// Platform-aware label for the spotlight-search hotkey chip shown on the
// topbar trigger. macOS uses ⌘; everything else Ctrl. Client-only (navigator
// is undefined on the server) — call sites guard against a hydration mismatch
// by only rendering the chip after mount.
export function searchShortcutLabel(): string {
  if (import.meta.server || typeof navigator === 'undefined') return 'Ctrl K'
  const mac = /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent)
  return mac ? '⌘K' : 'Ctrl K'
}

export function personImageUrl(personId: number) {
  return `/api/person/${personId}/image`
}

export function albumCoverUrl(album: { id: number, artist_media_item_id?: number, artist_media_item_public_id?: string }) {
  // Albums don't have their own image endpoint yet — fall back to the artist
  // media item poster. Replace once /api/album/{id}/cover lands.
  if (album.artist_media_item_id || album.artist_media_item_public_id) {
    return usePosterUrl({ id: album.artist_media_item_id, public_id: album.artist_media_item_public_id })
  }
  return null
}
