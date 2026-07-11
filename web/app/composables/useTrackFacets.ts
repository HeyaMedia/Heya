// useTrackFacets fetches a track's sonic-analysis facets + waveform.
// Used by Playbar (waveform) and the music UI (BPM/key/mood chips).
//
// Backed by Pinia Colada so the cache is shared with the rest of the app and
// dedupes across components — a track that's playing AND visible on a
// detail page only hits the API once. The cache survives route changes
// and (uniquely for facets) cross-component remounts.
//
// Failure modes are silent — when a track hasn't been analyzed yet,
// the endpoint returns 404 and we resolve to nulls so callers can
// render a fallback (e.g. a plain gradient bar in place of the
// waveform).

import { useQuery, useQueryCache } from '@pinia/colada'

export interface TrackKey {
  root: string
  mode: string
  display: string
  camelot: string
  clarity: number
}

export interface TrackGenreScore {
  name: string
  score: number
}

export interface TrackFacets {
  track_id: number
  bpm?: number
  bpm_confidence?: number
  key?: TrackKey
  integrated_lufs?: number
  loudness_range_lu?: number
  true_peak_dbtp?: number
  top_genres?: TrackGenreScore[]
  mood_tags?: Record<string, number>
  analyzed_at?: string
  analyzer_version: number
}

function facetsKey(trackId: number) {
  return ['music', 'track', 'facets', trackId] as const
}

function waveformKey(trackId: number) {
  return ['music', 'track', 'waveform', trackId] as const
}

// Imperative fetchers — used when a caller wants a one-shot result outside
// a setup() context. They ensure and refresh the same Colada entries used by
// the reactive composable below.
//
// Radio + podcast tracks use negative synthetic IDs — they don't have
// music-library facets, so skip the round-trip and the inevitable 422.
export async function fetchTrackFacets(trackId: number): Promise<TrackFacets | null> {
  if (trackId <= 0) return null
  const nuxtApp = useNuxtApp()
  const queryCache = useQueryCache(nuxtApp.$pinia)
  try {
    const entry = queryCache.ensure({
      key: facetsKey(trackId),
      query: async () => {
        return await nuxtApp.$heya('/api/music/tracks/{id}/facets', { path: { id: trackId } }) as TrackFacets
      },
      staleTime: 1000 * 60 * 60,
    })
    return (await queryCache.refresh(entry)).data ?? null
  } catch {
    return null
  }
}

export async function fetchTrackWaveform(trackId: number): Promise<number[] | null> {
  if (trackId <= 0) return null
  const nuxtApp = useNuxtApp()
  const queryCache = useQueryCache(nuxtApp.$pinia)
  try {
    const entry = queryCache.ensure({
      key: waveformKey(trackId),
      query: async () => {
        const data = await nuxtApp.$heya('/api/music/tracks/{id}/waveform', { path: { id: trackId } }) as { waveform: number[] }
        return data.waveform ?? null
      },
      staleTime: 1000 * 60 * 60,
    })
    return (await queryCache.refresh(entry)).data ?? null
  } catch {
    return null
  }
}

// useTrackFacets is the reactive flavor — fetches when trackId
// changes, exposes facets + waveform + loading flag. The two useQuery
// calls share keys with the imperative wrappers above, so the cache is
// uniform regardless of which entry point a consumer used.
export function useTrackFacets(trackId: Ref<number | null | undefined>) {
  const { $heya } = useNuxtApp()
  // `enabled: () => ...` lets us skip negative/null IDs cleanly.
  const enabled = computed(() => typeof trackId.value === 'number' && trackId.value > 0)

  const facetsQuery = useQuery(() => ({
    key: facetsKey(trackId.value ?? 0),
    query: async () => {
      return await $heya('/api/music/tracks/{id}/facets', { path: { id: trackId.value as number } }) as TrackFacets
    },
    enabled: enabled.value,
    staleTime: 1000 * 60 * 60,
    retry: 0,
  }))

  const waveformQuery = useQuery(() => ({
    key: waveformKey(trackId.value ?? 0),
    query: async () => {
      const data = await $heya('/api/music/tracks/{id}/waveform', { path: { id: trackId.value as number } }) as { waveform: number[] }
      return data.waveform ?? null
    },
    enabled: enabled.value,
    staleTime: 1000 * 60 * 60,
    retry: 0,
  }))

  const facets = computed<TrackFacets | null>(() => facetsQuery.data.value ?? null)
  const waveform = computed<number[] | null>(() => waveformQuery.data.value ?? null)
  const loading = computed(() => facetsQuery.isPending.value || waveformQuery.isPending.value)

  return { facets, waveform, loading }
}
