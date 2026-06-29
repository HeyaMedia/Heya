import { useAudioEngine } from '~/composables/useAudioEngine'

// Track shape consumed by the player UI. `stream_url` is what the engine
// actually hits — derived from the track row in the caller (Phase A list
// endpoints set this to `/api/music/tracks/{id}/stream`).
export interface Track {
  id: number
  title: string
  artist: string
  album: string
  duration: number
  stream_url?: string
  track_file_id?: number
  album_id?: number
  artist_id?: number
  artist_slug?: string
  album_slug?: string
  poster?: string
  loved?: boolean
  // Origin label for the scrobble's `source` field — 'queue' | 'radio' |
  // 'album' | 'playlist' | 'search' | 'browse' | 'similar' | ''. Free-form;
  // analytics on /api/me/listening-stats can group by this.
  source?: string
  // True when this row is a live ICY stream (internet radio). The player
  // disables shuffle/repeat/prev/next and shows a "LIVE" badge for these.
  isStream?: boolean
  // Per-track replay-gain inputs. When present, engine.setActiveNormalization
  // applies a gain so playback hits the engine's -18 LUFS target. NULL or
  // missing => track plays at the file's native level.
  integrated_lufs?: number | null
  true_peak_db?: number | null
  // False when the track's file is gone from disk. The player refuses to play
  // or enqueue these; list pages should pre-filter, but this is the backstop.
  available?: boolean
}

// Last.fm-style scrobble threshold: a track counts as "played" once the user
// has heard at least this many seconds, OR the track has ended (whichever
// comes first). The backend uses the same idea for /api/me/play-events.
const SCROBBLE_MIN_SECONDS = 30

export function usePlayer() {
  const playing = useState('player_playing', () => false)
  const currentTrack = useState<Track | null>('player_track', () => null)
  const position = useState('player_position', () => 0)
  const duration = useState('player_duration', () => 0)
  const volume = useState('player_volume', () => 80)
  const muted = useState('player_muted', () => false)
  const shuffled = useState('player_shuffled', () => false)
  const repeatMode = useState<'off' | 'all' | 'one'>('player_repeat', () => 'off')
  const queue = useState<Track[]>('player_queue', () => [])
  const queueOpen = useState('player_queue_open', () => false)
  const lyricsOpen = useState('player_lyrics_open', () => false)
  const engineWired = useState('player_engine_wired', () => false)
  // Tracks the last track ID we already scrobbled this session so the 30s
  // watcher + handleEnded don't double-fire for the same play.
  const scrobbledTrackId = useState<number | null>('player_scrobbled_track', () => null)

  // Engine creation touches AudioContext, which the browser refuses to
  // instantiate before a user gesture. Defer it to the first play() call so
  // the autoplay-policy warning never fires on mount.
  function ensureEngine() {
    const e = useAudioEngine()
    if (import.meta.client && !engineWired.value) {
      engineWired.value = true
      e.setOnEnded(() => handleEnded())
      e.setOnError(() => { playing.value = false })
      watch(e.isPlaying, (v) => { playing.value = v })
      watch(e.currentTime, (v) => { position.value = v })
      watch(e.duration, (v) => {
        if (Number.isFinite(v) && v > 0) duration.value = v
      })
      e.setVolume(muted.value ? 0 : volume.value / 100)
      // Apply persisted audio settings (EQ / crossfade) the moment the
      // engine exists. The bridge is idempotent so re-apply on every change
      // re-uses the same path.
      const settings = useAudioSettings()
      settings.registerEngineBridge(() => applyAudioSettingsToEngine(e, settings))

      // Scrobble watcher: fires POST /api/me/play-events once per track when
      // the listener has heard ≥30s. handleEnded covers the "track shorter
      // than 30s but played to completion" case so short songs still count.
      watch(position, (pos) => {
        const t = currentTrack.value
        if (!t || t.id <= 0) return
        if (pos < SCROBBLE_MIN_SECONDS) return
        if (scrobbledTrackId.value === t.id) return
        scrobbledTrackId.value = t.id
        void scrobbleTrack(t, pos, false)
      })
    }
    return e
  }

  // Scrobble through the unified /api/me/playback endpoint. Music tracks land
  // in the play_events history log server-side; videos go through the same
  // helper but with entity_type 'movie'/'episode' (see useVideoPlayer).
  async function scrobbleTrack(track: Track, listenedSeconds: number, completed: boolean) {
    if (track.id <= 0) return
    await recordPlayback({
      entity_type: 'track',
      entity_id: track.id,
      position_seconds: listenedSeconds,
      total_seconds: track.duration || 0,
      completed,
      source: track.source ?? '',
    })
  }

  // The <audio> element can't carry an Authorization header, so the session
  // token has to ride in the query string. The auth middleware already
  // accepts ?token= as an alternative to Bearer.
  //
  // For /stream URLs (smart endpoint that picks best playable + transcodes
  // if needed) we also append the audio caps so the server can match what
  // this browser will actually decode. /file/{id} URLs are bit-perfect and
  // don't need the caps decoration.
  function resolveStreamUrl(t: Track): string | undefined {
    const base = t.stream_url ?? (t.id > 0 ? `/api/music/tracks/${t.id}/stream` : undefined)
    if (!base) return undefined

    const params = new URLSearchParams()
    const { token } = useAuth()
    if (token.value) params.set('token', token.value)

    // Smart-pick endpoint? Decorate with audio caps so the server can pick a
    // file the browser supports (or fall back to AAC-256 transcode).
    if (import.meta.client && base.endsWith('/stream')) {
      const caps = useClientCaps()
      for (const [key, val] of Object.entries(caps)) {
        if (key.startsWith('supports_') && val) params.set(key, '1')
      }
    }

    const sep = base.includes('?') ? '&' : '?'
    return params.toString() ? `${base}${sep}${params.toString()}` : base
  }

  async function play(track?: Track) {
    const e = ensureEngine()
    if (track) {
      // Never play a track whose file was removed from disk.
      if (track.available === false) return
      currentTrack.value = track
      position.value = 0
      // Reset the scrobble guard so the new track can fire its own event.
      scrobbledTrackId.value = null
      if (track.duration && Number.isFinite(track.duration)) duration.value = track.duration
      const url = resolveStreamUrl(track)
      if (!url) return
      // Apply loudness normalization before starting so the first sample
      // doesn't blast through at the file's native level.
      if (track.integrated_lufs != null && track.true_peak_db != null) {
        e.setActiveNormalization(track.integrated_lufs, track.true_peak_db)
      } else {
        e.resetActiveNormalization()
      }
      try {
        await e.play(url)
      } catch {
        playing.value = false
      }
      return
    }
    // No track passed — resume current
    if (!currentTrack.value) return
    try {
      await e.resume()
    } catch {
      playing.value = false
    }
  }

  function pause() {
    if (!engineWired.value) return
    ensureEngine().pause()
  }

  async function togglePlay() {
    if (playing.value) pause()
    else await play()
  }

  // seek takes a 0-1 fraction (legacy API the UI uses).
  function seek(pct: number) {
    const target = Math.max(0, Math.min(1, pct)) * (duration.value || 0)
    if (engineWired.value) ensureEngine().seek(target)
    position.value = target
  }

  function setVolume(v: number) {
    const clamped = Math.max(0, Math.min(100, v))
    volume.value = clamped
    if (clamped > 0) muted.value = false
    if (engineWired.value) ensureEngine().setVolume(muted.value ? 0 : clamped / 100)
  }

  function toggleMute() {
    muted.value = !muted.value
    if (engineWired.value) ensureEngine().setVolume(muted.value ? 0 : volume.value / 100)
  }

  function toggleShuffle() { shuffled.value = !shuffled.value }

  function cycleRepeat() {
    const modes: Array<'off' | 'all' | 'one'> = ['off', 'all', 'one']
    const idx = modes.indexOf(repeatMode.value)
    repeatMode.value = modes[(idx + 1) % modes.length]!
  }

  function pickNextTrack(): Track | null {
    if (!queue.value.length) return null
    const idx = queue.value.findIndex(t => t.id === currentTrack.value?.id)
    if (shuffled.value) {
      const pool = queue.value.filter((_, i) => i !== idx)
      if (!pool.length) return repeatMode.value === 'all' ? (queue.value[0] ?? null) : null
      return pool[Math.floor(Math.random() * pool.length)] ?? null
    }
    const next = queue.value[idx + 1]
    if (next) return next
    return repeatMode.value === 'all' ? (queue.value[0] ?? null) : null
  }

  async function nextTrack() {
    const next = pickNextTrack()
    if (next) await play(next)
  }

  async function prevTrack() {
    if (position.value > 3) {
      if (engineWired.value) ensureEngine().seek(0)
      position.value = 0
      return
    }
    if (!queue.value.length) return
    const idx = queue.value.findIndex(t => t.id === currentTrack.value?.id)
    const prev = queue.value[(idx - 1 + queue.value.length) % queue.value.length]
    if (prev) await play(prev)
  }

  async function handleEnded() {
    // Short tracks (under SCROBBLE_MIN_SECONDS) never trigger the position
    // watcher, but a completed playthrough still counts as a scrobble. Fire
    // here if the watcher hasn't already.
    const finished = currentTrack.value
    if (finished && finished.id > 0 && scrobbledTrackId.value !== finished.id) {
      scrobbledTrackId.value = finished.id
      void scrobbleTrack(finished, position.value || finished.duration, true)
    }
    if (repeatMode.value === 'one' && currentTrack.value) {
      await play(currentTrack.value)
      return
    }
    await nextTrack()
  }

  function toggleLoved() {
    if (currentTrack.value) {
      currentTrack.value = { ...currentTrack.value, loved: !currentTrack.value.loved }
    }
  }

  function toggleQueue() { queueOpen.value = !queueOpen.value }
  function toggleLyrics() { lyricsOpen.value = !lyricsOpen.value }

  function formatTime(s: number) {
    if (!Number.isFinite(s)) return '0:00'
    const m = Math.floor(s / 60)
    const sec = Math.floor(s % 60)
    return `${m}:${sec.toString().padStart(2, '0')}`
  }

  // --- Queue management (Played / Now Playing / Up Next semantics) ---
  // Lifted from hibiki's player store so the right sidebar can render the
  // three sections plus drag / remove. `currentIndex` is the index of the
  // playing track in `queue`; -1 when nothing is playing. We derive it from
  // `currentTrack.id` rather than storing both — single source of truth.
  const currentIndex = computed(() => {
    const t = currentTrack.value
    if (!t) return -1
    return queue.value.findIndex((x) => x.id === t.id)
  })
  const playedTracks = computed(() => {
    const idx = currentIndex.value
    return idx > 0 ? queue.value.slice(0, idx) : []
  })
  const upcomingTracks = computed(() => {
    const idx = currentIndex.value
    return idx >= 0 ? queue.value.slice(idx + 1) : []
  })
  const upcomingCount = computed(() => upcomingTracks.value.length)

  // jumpTo plays the queue item at absolute index, treating the queue as
  // the authoritative ordering. Used by the right-sidebar rows.
  async function jumpTo(index: number) {
    const t = queue.value[index]
    if (!t) return
    await play(t)
  }

  // removeFromQueue drops one upcoming track. Guard against removing the
  // current or played items — the sidebar only exposes the up-next bucket
  // anyway, but the guard keeps callers honest.
  function removeFromQueue(index: number) {
    if (index <= currentIndex.value) return
    if (index >= queue.value.length) return
    queue.value.splice(index, 1)
  }

  // moveInQueue reorders an upcoming track. Same guards as remove.
  function moveInQueue(from: number, to: number) {
    if (from <= currentIndex.value || to <= currentIndex.value) return
    if (from >= queue.value.length || to >= queue.value.length) return
    if (from === to) return
    const next = queue.value.slice()
    const [item] = next.splice(from, 1)
    if (item) next.splice(to, 0, item)
    queue.value = next
  }

  // addToQueue appends one or more tracks to the end of the queue. When the
  // queue is empty the first added track becomes "now playing" so the user
  // gets immediate feedback. De-dupes by track id against the up-next slice
  // so spamming "Add to Queue" doesn't pile up duplicates.
  async function addToQueue(tracks: Track | Track[]) {
    const list = (Array.isArray(tracks) ? tracks : [tracks]).filter((t) => t.available !== false)
    if (!list.length) return
    if (!queue.value.length) {
      queue.value = list
      await play(list[0]!)
      return
    }
    const upcomingIds = new Set(upcomingTracks.value.map((t) => t.id))
    const fresh = list.filter((t) => !upcomingIds.has(t.id))
    if (!fresh.length) return
    queue.value = [...queue.value, ...fresh]
  }

  // playNext inserts one or more tracks immediately after the currently-
  // playing one (or at the head if nothing's playing). Mirrors Spotify /
  // Apple Music "Play Next". Same de-dupe behavior as addToQueue.
  async function playNext(tracks: Track | Track[]) {
    const list = (Array.isArray(tracks) ? tracks : [tracks]).filter((t) => t.available !== false)
    if (!list.length) return
    if (!queue.value.length) {
      queue.value = list
      await play(list[0]!)
      return
    }
    const upcomingIds = new Set(upcomingTracks.value.map((t) => t.id))
    const fresh = list.filter((t) => !upcomingIds.has(t.id))
    if (!fresh.length) return
    const idx = currentIndex.value
    const insertAt = idx < 0 ? 0 : idx + 1
    const next = queue.value.slice()
    next.splice(insertAt, 0, ...fresh)
    queue.value = next
  }

  // clearUpcoming empties everything after the current track. Used by the
  // sidebar's "Clear" button on the Up Next header.
  function clearUpcoming() {
    const idx = currentIndex.value
    if (idx < 0) {
      queue.value = []
      return
    }
    queue.value = queue.value.slice(0, idx + 1)
  }

  // stop unloads the engine + clears state. Used by the playbar long-press.
  function stop() {
    if (engineWired.value) ensureEngine().stop()
    playing.value = false
    currentTrack.value = null
    queue.value = []
    position.value = 0
    duration.value = 0
  }

  return {
    playing, currentTrack, position, duration, volume, muted,
    shuffled, repeatMode, queue, queueOpen, lyricsOpen,
    currentIndex, playedTracks, upcomingTracks, upcomingCount,
    play, pause, togglePlay, seek, setVolume, toggleMute, stop,
    toggleShuffle, cycleRepeat, nextTrack, prevTrack,
    toggleLoved, toggleQueue, toggleLyrics, formatTime,
    jumpTo, removeFromQueue, moveInQueue, clearUpcoming,
    addToQueue, playNext,
  }
}

// applyAudioSettingsToEngine pushes the persisted EQ + crossfade state into
// the engine. Idempotent — settings re-apply on every mutation.
function applyAudioSettingsToEngine(engine: ReturnType<typeof useAudioEngine>, settings: ReturnType<typeof useAudioSettings>) {
  // The SSR stub lacks the chain block accessors; bail when they're missing.
  const e = engine as ReturnType<typeof useAudioEngine> & {
    equalizer?: { enabled: boolean; setAllBands: (b: number[]) => void }
    preamp?: { enabled: boolean; setGain: (db: number) => void }
    postgain?: { enabled: boolean; setGain: (db: number) => void }
    signalChain?: { rebuild: () => void }
    scheduler?: { setMode: (m: 'gapless' | 'crossfade') => void; setCrossfadeDuration: (s: number) => void }
  }
  const eq = settings.eq.value
  if (e.equalizer) {
    e.equalizer.setAllBands(eq.bands)
    e.equalizer.enabled = eq.enabled
  }
  if (e.preamp) {
    e.preamp.setGain(eq.preamp)
    e.preamp.enabled = eq.enabled
  }
  if (e.postgain) {
    e.postgain.setGain(eq.postgain)
    e.postgain.enabled = eq.enabled
  }
  // Toggling block.enabled requires a chain rebuild for the bypass to take.
  e.signalChain?.rebuild()
  const cf = settings.crossfade.value
  if (e.scheduler) {
    e.scheduler.setMode(cf.mode)
    e.scheduler.setCrossfadeDuration(cf.durationSeconds)
  }
}
