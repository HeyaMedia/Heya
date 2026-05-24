// useTrackFacets fetches a track's sonic-analysis facets + waveform.
// Used by Playbar (waveform) and the music UI (BPM/key/mood chips).
//
// Failure modes are silent — when a track hasn't been analyzed yet,
// the endpoint returns 404 and we resolve to nulls so callers can
// render a fallback (e.g. a plain gradient bar in place of the
// waveform).

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

// In-memory caches scoped to one page load. Avoids hammering the
// API when the playbar re-renders or the user toggles a panel that
// re-mounts a chip row.
const facetsCache = new Map<number, TrackFacets | null>()
const waveformCache = new Map<number, number[] | null>()

export async function fetchTrackFacets(trackId: number): Promise<TrackFacets | null> {
  if (facetsCache.has(trackId)) return facetsCache.get(trackId)!
  try {
    const data = await apiFetch<TrackFacets>(`/api/tracks/${trackId}/facets`)
    facetsCache.set(trackId, data)
    return data
  } catch {
    facetsCache.set(trackId, null)
    return null
  }
}

export async function fetchTrackWaveform(trackId: number): Promise<number[] | null> {
  if (waveformCache.has(trackId)) return waveformCache.get(trackId)!
  try {
    const data = await apiFetch<{ waveform: number[] }>(`/api/tracks/${trackId}/waveform`)
    const wf = data.waveform ?? null
    waveformCache.set(trackId, wf)
    return wf
  } catch {
    waveformCache.set(trackId, null)
    return null
  }
}

// useTrackFacets is the reactive flavor — fetches when trackId
// changes, exposes facets + waveform + loading flag.
export function useTrackFacets(trackId: Ref<number | null | undefined>) {
  const facets = ref<TrackFacets | null>(null)
  const waveform = ref<number[] | null>(null)
  const loading = ref(false)

  watch(
    trackId,
    async id => {
      if (!id) {
        facets.value = null
        waveform.value = null
        return
      }
      loading.value = true
      const [f, wf] = await Promise.all([fetchTrackFacets(id), fetchTrackWaveform(id)])
      facets.value = f
      waveform.value = wf
      loading.value = false
    },
    { immediate: true },
  )

  return { facets, waveform, loading }
}
