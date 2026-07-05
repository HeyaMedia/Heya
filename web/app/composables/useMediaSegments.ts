import type { Ref } from 'vue'

export interface MediaSegment {
  type: string // intro | recap | credits | preview | commercial
  start_ms: number
  end_ms: number
  source: string
}

/**
 * Skip segments (intro/recap/credits markers) for a playable file.
 * Sibling of useTrickplay — fetched once per file on playback start,
 * then consulted per timeupdate to decide whether a skip button shows.
 *
 * Times are exposed in seconds to match the <video> element clock.
 */
export function useMediaSegments(fileId: Ref<number>) {
  const { $heya } = useNuxtApp()
  const segments = ref<MediaSegment[]>([])
  const loaded = ref(false)

  async function load() {
    loaded.value = false
    segments.value = []
    try {
      const res = await $heya('/api/stream/{file_id}/segments', {
        path: { file_id: fileId.value },
      })
      segments.value = (res?.segments ?? []) as MediaSegment[]
      loaded.value = true
    }
    catch {
      // No segments is the common case and never worth surfacing.
    }
  }

  /** Segment containing the playhead (seconds), or null. Sub-3s
   *  segments never prompt — jarring for something skipped in a blink. */
  function segmentAt(currentTimeSecs: number): MediaSegment | null {
    const ms = currentTimeSecs * 1000
    for (const seg of segments.value) {
      if (ms >= seg.start_ms && ms < seg.end_ms && seg.end_ms - seg.start_ms >= 3000)
        return seg
    }
    return null
  }

  return { segments, loaded, load, segmentAt }
}
