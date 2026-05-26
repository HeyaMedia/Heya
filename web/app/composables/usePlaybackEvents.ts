// Unified playback emission. Both the video player and the music player call
// `recordPlayback()` — the backend dispatches by entity_type, so a single FE
// path covers "watch progress" and "music scrobble" semantics:
//
//   - entity_type 'movie' / 'episode' → UPSERTs user_watch_progress
//   - entity_type 'track'             → appends to play_events
//
// Per-engine details (how often to fire, when to flag completed) stay in the
// engine composables; this helper only handles the wire encoding.

export type PlaybackEntityType = 'movie' | 'episode' | 'track'

export interface PlaybackEventInput {
  entity_type: PlaybackEntityType
  entity_id: number
  position_seconds: number
  total_seconds: number
  completed: boolean
  // Origin label — 'queue' | 'radio' | 'album' | 'playlist' | 'search' |
  // 'browse' | 'similar' | ''. Free-form; analytics on listening-stats can
  // group by this once we surface the data.
  source?: string
}

export async function recordPlayback(event: PlaybackEventInput): Promise<void> {
  const { token } = useAuth()
  if (!token.value) return // not signed in — nothing to record against
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/me/playback', {
      method: 'POST',
      body: {
        entity_type: event.entity_type,
        entity_id: event.entity_id,
        position_seconds: Math.max(0, Math.round(event.position_seconds)),
        total_seconds: Math.max(0, Math.round(event.total_seconds)),
        completed: event.completed,
        source: event.source ?? '',
      },
    })
  } catch (e) {
    // Best-effort: a momentary network blip shouldn't tear down playback.
    console.warn('playback event failed:', e)
  }
}
