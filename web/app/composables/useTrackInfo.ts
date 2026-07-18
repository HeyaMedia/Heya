import type { RecordingCredit, TrackFile } from '~~/shared/types'

// ── Shared "Track information" dialog channel ────────────────────────────────
// A singleton (module-scope) store + a single globally-mounted
// <TrackInfoDialog> (see app.vue), mirroring the useConfirm / useLightbox
// pattern. The CENTRAL track context-menu builder (useMusicActions.forTrack)
// pushes a "Track info" item that calls open(id); the dialog then fetches
// /api/music/tracks/{id} (MusicTrackDetail) for the universal shape.
//
// Pages that already hold a rich album row can `prime()` its metadata keyed by
// track id. MusicTrackDetail remains authoritative for physical file paths.
export interface TrackInfoPrefetch {
  file_path?: string
  recording_mbid?: string
  isrc?: string
  explicit?: boolean
  files?: TrackFile[]
  credits?: RecordingCredit[]
}

interface TrackInfoState {
  open: boolean
  trackId: number | null
  prefetch: TrackInfoPrefetch | null
}

const state = ref<TrackInfoState>({ open: false, trackId: null, prefetch: null })

// id → extra fields a page primed. Kept outside reactive state on purpose: it's
// a lookup table consulted at open() time, not something the UI renders.
const primed = new Map<number, TrackInfoPrefetch>()

export function useTrackInfo() {
  function open(trackId: number, prefetch?: TrackInfoPrefetch) {
    state.value = {
      open: true,
      trackId,
      prefetch: prefetch ?? primed.get(trackId) ?? null,
    }
  }

  function close() {
    state.value = { ...state.value, open: false }
  }

  // Register richer rows (album page) so the central open(id) can reuse their
  // MBIDs/files while the detail request is in flight.
  function prime(rows: Array<{ id: number } & TrackInfoPrefetch>) {
    for (const r of rows) {
      primed.set(r.id, {
        file_path: r.file_path,
        recording_mbid: r.recording_mbid,
        isrc: r.isrc,
        explicit: r.explicit,
        files: r.files,
        credits: r.credits,
      })
    }
  }

  function unprime(ids: number[]) {
    for (const id of ids) primed.delete(id)
  }

  return { state, open, close, prime, unprime }
}
