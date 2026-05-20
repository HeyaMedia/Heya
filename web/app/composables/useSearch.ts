import type { MediaItem, MediaType } from '~~/shared/types'

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
  const data = ref<QuickSearchResponse | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  let timer: ReturnType<typeof setTimeout> | null = null
  let seq = 0

  async function fetchNow(q: string) {
    const my = ++seq
    loading.value = true
    error.value = null
    try {
      const res = await apiFetch<QuickSearchResponse>(
        `/api/search/quick?q=${encodeURIComponent(q)}`,
      )
      if (my === seq) data.value = res
    } catch (e: any) {
      if (my === seq) error.value = e?.message || 'Search failed'
    } finally {
      if (my === seq) loading.value = false
    }
  }

  watch(query, (q) => {
    if (timer) clearTimeout(timer)
    const trimmed = q.trim()
    if (!trimmed) {
      seq++
      data.value = null
      loading.value = false
      return
    }
    timer = setTimeout(() => fetchNow(trimmed), debounceMs)
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
    if (timer) clearTimeout(timer)
    seq++
    query.value = ''
    data.value = null
    loading.value = false
    error.value = null
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
  const params = new URLSearchParams({ q })
  if (type) params.set('type', type)
  params.set('limit', String(limit))
  params.set('offset', String(offset))
  return apiFetch(`/api/search?${params.toString()}`)
}

export function personImageUrl(personId: number) {
  return `/api/person/${personId}/image`
}

export function albumCoverUrl(album: { id: number, artist_media_item_id?: number }) {
  // Albums don't have their own image endpoint yet — fall back to the artist
  // media item poster. Replace once /api/album/{id}/cover lands.
  if (album.artist_media_item_id) {
    return `/api/media/${album.artist_media_item_id}/image/poster`
  }
  return null
}
