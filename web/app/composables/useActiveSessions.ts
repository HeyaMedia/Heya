// useActiveSessions exposes the list of live playback sessions to any
// consumer (activity panel, dedicated sessions page later). Backed by
// Pinia Colada for the initial fetch + remount-survives caching, plus an
// event-bus subscription so the cache stays live without polling.
//
// The server emits a payload-less `session.update` ping on every change
// (start/end/heartbeat) — it deliberately does NOT push the session list,
// because the WS stream is unfiltered and that would leak other users'
// sessions. On each ping we invalidate the query so it re-fetches through the
// auth-scoped /api/sessions/active endpoint (own-only for non-admins).

import { useQuery, useQueryCache } from '@pinia/colada'
import { formatTime } from './useHeyaPlayer'

export interface ActiveSession {
  session_id: string
  user_id: number
  username: string
  file_id: number
  media_item_id: number
  media_title: string
  /** Type-aware secondary line — "S01E03 · Episode title" for episodes,
   *  "Artist — Album" for tracks, empty for movies. */
  media_subtitle?: string
  media_type: string
  entity_type?: string
  entity_id?: number
  season_number?: number
  episode_number?: number
  episode_title?: string
  artist_name?: string
  album_title?: string
  position_seconds: number
  total_seconds: number
  paused: boolean
  playback_action?: string
  video_codec?: string
  audio_codec?: string
  container?: string
  width?: number
  height?: number
  bitrate_kbps?: number
  client_user_agent?: string
  client_ip?: string
  started_at: string
  last_heartbeat_at: string
}

const SESSIONS_QUERY_KEY = ['sessions', 'active'] as const

export function useActiveSessions() {
  const { $heya } = useNuxtApp()
  const queryClient = useQueryCache()
  const { on, connect } = useEventBus()

  const query = useQuery({
    key: SESSIONS_QUERY_KEY,
    query: async () => {
      const res = await $heya('/api/sessions/active') as { items: ActiveSession[] }
      return res.items ?? []
    },
    // Sessions are live data driven by WS events — the cache stays fresh
    // through pushes, not polling. 5-minute staleTime keeps remount-reads
    // instant; the WS path overrides whenever something actually changes.
    staleTime: 1000 * 60 * 5,
  })

  // The session.update event is a payload-less change signal — refetch through
  // the auth-scoped endpoint rather than trusting anything on the wire.
  // We connect once per consumer; useEventBus.connect is idempotent.
  //
  // Hidden tabs defer the refetch: session.update arrives every ~10s while
  // anyone on the server is playing, and a backgrounded phone has no business
  // waking its radio for an activity panel nobody can see. One catch-up
  // invalidation fires when the tab becomes visible again.
  if (import.meta.client) {
    connect()
    let hiddenPending = false
    const invalidate = () => queryClient.invalidateQueries({ key: SESSIONS_QUERY_KEY })
    const off = on('session.update', () => {
      if (document.visibilityState === 'hidden') {
        hiddenPending = true
        return
      }
      invalidate()
    })
    const onVisibility = () => {
      if (document.visibilityState !== 'visible' || !hiddenPending) return
      hiddenPending = false
      invalidate()
    }
    document.addEventListener('visibilitychange', onVisibility)
    onScopeDispose(() => {
      off()
      document.removeEventListener('visibilitychange', onVisibility)
    })
  }

  const sessions = computed<ActiveSession[]>(() => query.data.value ?? [])

  function progressPct(s: ActiveSession): number {
    if (s.total_seconds <= 0) return 0
    return Math.min(100, Math.round((s.position_seconds / s.total_seconds) * 100))
  }

  function transcodeLabel(s: ActiveSession): string {
    if (!s.playback_action) return ''
    if (s.playback_action === 'direct_play') return 'Direct play'
    if (s.playback_action === 'remux') return 'Remux'
    if (s.playback_action === 'transcode') return 'Transcoding'
    return s.playback_action
  }

  return {
    sessions,
    isPending: query.isPending,
    formatTime,
    progressPct,
    transcodeLabel,
  }
}
