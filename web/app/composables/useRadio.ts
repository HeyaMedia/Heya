// useRadio bundles two distinct "radio" concepts under one file:
//
//   1. useRadio()         — Instant Radio + DJ Mix (KNN/Camelot expansion
//      seeded from a track/artist/album/text). Music-library only.
//   2. useRadioActions()  — Internet-radio stations from radio-browser.info
//      via the backend proxy, with ICY metadata streamed back via the
//      event bus.
//
// They share no state but live together because the FE consumes both as
// "radio". Two separate exports keeps the responsibilities clean.

import type { Track } from '~/composables/usePlayer'

// --- 1) Instant Radio + DJ Mix (music library) ------------------------------

export type RadioSeed =
  | { kind: 'track';  track_id: number }
  | { kind: 'artist'; artist_slug?: string; artist_id?: number }
  | { kind: 'album';  album_id: number }
  | { kind: 'text';   text: string }

interface RadioTrackRow {
  track_id: number
  track_title: string
  duration: number
  disc_number: number
  track_number: number
  album_id: number
  album_title: string
  album_slug: string
  album_cover_path: string
  album_year: string
  artist_id: number
  artist_name: string
  artist_slug: string
  distance: number
}

interface RadioResponse {
  seed_track_id: number
  tracks: RadioTrackRow[]
}

function rowToTrack(row: RadioTrackRow): Track {
  return {
    id: row.track_id,
    title: row.track_title,
    artist: row.artist_name,
    album: row.album_title,
    duration: row.duration,
    stream_url: `/api/music/tracks/${row.track_id}/stream`,
    album_id: row.album_id,
    artist_id: row.artist_id,
    artist_slug: row.artist_slug,
    album_slug: row.album_slug,
    poster: useAlbumCoverUrl(row.artist_slug, row.album_slug) ?? undefined,
    source: 'radio',
  }
}

export function useRadio() {
  const { play, queue } = usePlayer()
  const starting = useState('radio_starting', () => false)

  async function startRadio(seed: RadioSeed, seedTrack?: Track) {
    starting.value = true
    try {
      const { $heya } = useNuxtApp()
      const res = await $heya('/api/music/radio', { method: 'POST', body: { seed, limit: 50 } }) as RadioResponse
      const radioTracks = (res.tracks ?? []).map(rowToTrack)
      const tracks: Track[] = []
      if (seedTrack) tracks.push({ ...seedTrack, source: 'radio' })
      tracks.push(...radioTracks)
      if (!tracks.length) return
      queue.value = tracks
      await play(tracks[0])
    } catch (e) { console.warn('startRadio failed:', e) }
    finally { starting.value = false }
  }

  async function startDJMix(seedTrackID: number, seedTrack?: Track) {
    starting.value = true
    try {
      const { $heya } = useNuxtApp()
      const res = await $heya('/api/music/tracks/{id}/mix-to', {
        path: { id: seedTrackID },
        query: { limit: 30 },
      }) as { items: RadioTrackRow[] }
      const mixTracks = (res.items ?? []).map(rowToTrack).map((t) => ({ ...t, source: 'mix' }))
      const tracks: Track[] = []
      if (seedTrack) tracks.push({ ...seedTrack, source: 'mix' })
      tracks.push(...mixTracks)
      if (!tracks.length) return
      queue.value = tracks
      await play(tracks[0])
    } catch (e) { console.warn('startDJMix failed:', e) }
    finally { starting.value = false }
  }

  return { startRadio, startDJMix, starting }
}

// --- 2) Internet-radio station playback (radio-browser via Heya proxy) ------

// RadioStationView is the FE projection of the radio-browser station row.
// Renamed from `RadioStationView` to avoid colliding with the global symbol
// the openapi-typescript client exposes (which has stricter shape).
export interface RadioStationView {
  stationuuid: string
  name: string
  url: string
  url_resolved: string
  favicon: string
  homepage: string
  country: string
  countrycode: string
  language: string
  tags: string
  codec: string
  bitrate: number
  votes: number
  clickcount: number
}

// stationToTrack adapts a radio-browser station into the Track shape the
// player engine consumes. The stream URL goes through our backend proxy so
// ICY metadata can be lifted and emitted via the event bus.
//
// We only set the `url` query param here — usePlayer.resolveStreamUrl()
// adds the auth token on its own. Encoding the URL keeps the upstream's
// query string (some stations have ?token=…) intact through our proxy.
export function stationToTrack(station: RadioStationView): Track {
  const playable = station.url_resolved || station.url
  const params = new URLSearchParams({ url: playable })
  return {
    id: -hashStationUUID(station.stationuuid), // negative so radio rows don't collide with real track ids
    title: station.name,
    artist: station.country || 'Live',
    album: '',
    duration: 0,
    stream_url: `/api/radio/stream?${params.toString()}`,
    poster: station.favicon || undefined,
    isStream: true,
    source: 'radio-station',
  }
}

// hashStationUUID maps a stationuuid to a stable positive integer so we
// can stuff it into Track.id (which is typed `number`) without colliding
// with real track ids. Negated by the caller so the player can identify
// stream rows by id<0 if needed.
function hashStationUUID(uuid: string): number {
  let hash = 5381
  for (let i = 0; i < uuid.length; i++) {
    hash = ((hash << 5) + hash) ^ uuid.charCodeAt(i)
  }
  // Clamp to a safe positive int range.
  return Math.abs(hash) % 1_000_000_000
}

// useRadioActions packages the play/favorite/recents flows so each page
// doesn't re-implement them. The store ref is reactive — favorites change
// across pages without a refetch.
export function useRadioActions() {
  const { play, queue } = usePlayer()
  const favoriteUUIDs = useState<Set<string>>('radio_favorite_uuids', () => new Set())
  const favoritesLoaded = useState('radio_favorites_loaded', () => false)
  const loadingStationUUID = useState<string | null>('radio_loading_uuid', () => null)

  async function ensureFavoritesLoaded() {
    if (favoritesLoaded.value) return
    favoritesLoaded.value = true
    try {
      const { $heya } = useNuxtApp()
      const res = await $heya('/api/me/radio/favorites') as { items: Array<{ stationuuid: string }> }
      favoriteUUIDs.value = new Set((res.items ?? []).map((s) => s.stationuuid))
    } catch {
      // best-effort — favorites just stay empty for this session
    }
  }

  async function toggleFavorite(station: RadioStationView) {
    const { $heya } = useNuxtApp()
    const has = favoriteUUIDs.value.has(station.stationuuid)
    if (has) {
      try {
        await $heya('/api/me/radio/favorites/{uuid}', {
          method: 'DELETE',
          path: { uuid: station.stationuuid },
        })
        favoriteUUIDs.value.delete(station.stationuuid)
        favoriteUUIDs.value = new Set(favoriteUUIDs.value) // trigger reactivity
      } catch (e) { console.warn('unfavorite failed:', e) }
    } else {
      try {
        await $heya('/api/me/radio/favorites', { method: 'POST', // Cast through `any` because the generated openapi-fetch body type adds a
// `$schema?` field RadioStationView doesn't carry — runtime shape is fine.
body: station as any /* eslint-disable-line @typescript-eslint/no-explicit-any */ })
        favoriteUUIDs.value.add(station.stationuuid)
        favoriteUUIDs.value = new Set(favoriteUUIDs.value)
      } catch (e) { console.warn('favorite failed:', e) }
    }
  }

  function isFavorited(uuid: string) {
    return favoriteUUIDs.value.has(uuid)
  }

  // playStation: build a single-track queue with the stream, kick playback,
  // and record the click (best-effort). Live streams don't have prev/next.
  async function playStation(station: RadioStationView) {
    loadingStationUUID.value = station.stationuuid
    try {
      const track = stationToTrack(station)
      queue.value = [track]
      await play(track)
      // Fire-and-forget recents + upstream click
      try {
        const { $heya } = useNuxtApp()
        await $heya('/api/me/radio/play', { method: 'POST', // Cast through `any` because the generated openapi-fetch body type adds a
// `$schema?` field RadioStationView doesn't carry — runtime shape is fine.
body: station as any /* eslint-disable-line @typescript-eslint/no-explicit-any */ })
      } catch { /* not critical */ }
    } finally {
      loadingStationUUID.value = null
    }
  }

  return {
    ensureFavoritesLoaded,
    toggleFavorite,
    isFavorited,
    playStation,
    loadingStationUUID,
    favoriteUUIDs,
  }
}

// useRadioNowPlaying subscribes to ICY metadata events emitted by the
// backend stream proxy. When the active track is an ICY stream and the
// event's stream_url matches what we're playing, we surface (artist, title)
// for the FE to overlay on the "Now Playing" card.
export function useRadioNowPlaying() {
  const meta = useState<{ artist: string; title: string; streamUrl: string } | null>(
    'radio_now_playing',
    () => null,
  )
  const bus = useEventBus()
  // Connect lazily — only fires when a component actually needs the meta.
  // Once connected stays connected for the page lifetime.
  let off: (() => void) | null = null
  function ensureSubscribed() {
    if (off) return
    bus.connect()
    off = bus.on('radio.icy', (event) => {
      const payload = event.payload as { artist?: string; title?: string; stream_url?: string } | undefined
      if (!payload) return
      meta.value = {
        artist: payload.artist ?? '',
        title: payload.title ?? '',
        streamUrl: payload.stream_url ?? '',
      }
    })
  }
  return { meta, ensureSubscribed }
}
