// useActiveSessions exposes the list of live playback sessions to any
// consumer (activity panel, dedicated sessions page later). Backed by
// vue-query for the initial fetch + remount-survives caching, plus an
// event-bus subscription so the cache stays live without polling.
//
// The server emits a payload-less `session.update` ping on every change
// (start/end/heartbeat) — it deliberately does NOT push the session list,
// because the WS stream is unfiltered and that would leak other users'
// sessions. On each ping we invalidate the query so it re-fetches through the
// auth-scoped /api/sessions/active endpoint (own-only for non-admins).

import { useQuery, useQueryClient } from '@tanstack/vue-query'
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
  const queryClient = useQueryClient()
  const { on, connect } = useEventBus()

  const query = useQuery({
    queryKey: SESSIONS_QUERY_KEY,
    queryFn: async () => {
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
  if (import.meta.client) {
    connect()
    const off = on('session.update', () => {
      queryClient.invalidateQueries({ queryKey: SESSIONS_QUERY_KEY })
    })
    onScopeDispose(off)
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
