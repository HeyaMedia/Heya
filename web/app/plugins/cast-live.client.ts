// Cast session live mirror. The server owns cast playback and broadcasts
// every state edge on `cast.state` (global, household-scoped — see
// Manager.emitSession); this plugin folds those events into the cast store
// and, while THIS tab has the cast output engaged, mirrors transport state
// into the player store so the playbar just works: play/pause/position/
// volume all reflect the receiver.
//
// Position is interpolated client-side between events (the server doesn't
// tick every second) — a 500ms ticker advances the scrubber while playing.
import type { CastStateEvent } from '~/composables/useCast'

export default defineNuxtPlugin((nuxtApp) => {
  const { on, connected } = useEventBus()
  const { token } = useAuth()
  const { $heya } = useNuxtApp() // hoisted — never resolve inside async bodies
  const cast = useCastStore()
  const player = usePlayerStore()
  const { toast } = useToast()

  // Boot: adopt a session that's already running (page load while the
  // house is casting) and warm the device list so the UI knows whether to
  // show the cast button at all. Deferred to app:mounted — plugins load
  // alphabetically, so a $heya call made during THIS plugin's setup runs
  // before heyaApi.client.ts registers the bearer-token hook: the request
  // goes out unauthenticated, 401s, and the global handler force-logs-out.
  nuxtApp.hook('app:mounted', () => {
    watch(token, (t) => {
      if (!t) return
      void cast.adoptExisting().then(() => syncFromSession())
      void cast.refreshDevices()
    }, { immediate: true })

    // WS reconnect: events were lost while offline — re-pull the session
    // snapshot so a track that ended (or a session that died) meanwhile
    // doesn't leave the mirror playing forever.
    watch(connected, (up, wasUp) => {
      if (up && wasUp === false && token.value && !cast.connecting) {
        void cast.adoptExisting().then(() => syncFromSession())
      }
    })
  })

  on('cast.state', (ev) => {
    const p = ev.payload as CastStateEvent
    const outcome = cast.applyEvent(p)
    if (outcome === 'ended') {
      // A track this tab started finished on the receiver — advance the
      // queue (server already scrobbled it with source "cast").
      void player.castTrackEnded()
      return
    }
    if (outcome === 'failed') {
      toast.err(`Cast to ${p.device_name} failed`)
      if (cast.engaged) {
        cast.engagedDeviceId = null
        player.playing = false
      }
      return
    }
    syncFromSession()
  })

  function syncFromSession() {
    // Mirror only when this tab routes its output to the cast device —
    // an un-engaged tab (someone else casting) must not have its local
    // playback state overwritten.
    if (!cast.engaged) return
    const s = cast.session
    if (!s) {
      // Session gone without a natural-end advance (deliberate stop, or a
      // foreign tab owns the queue): the transport is simply not playing.
      player.playing = false
      return
    }
    // 'starting' counts as playing: pause→resume and seek respawn the
    // sender (~2-3s) and the button shouldn't bounce through it.
    player.playing = s.state === 'playing' || s.state === 'starting'
    player.position = cast.livePositionSec()
    if (s.duration_sec && s.duration_sec > 0) player.duration = s.duration_sec
    // The slider shows the device stream volume while casting. Skip the
    // echo of our own mute (volume 0) so unmute remembers the level.
    if (!(player.muted && s.volume === 0) && player.volume !== s.volume) {
      player.volume = s.volume
    }
    // A tab that didn't start this playback (adopted session / another
    // client's cast) has no matching currentTrack — hydrate one so the
    // playbar shows what's actually playing.
    if (s.track_id && player.currentTrack?.id !== s.track_id) {
      void hydrateTrack(s.track_id)
    }
  }

  async function hydrateTrack(id: number) {
    try {
      const d = await $heya('/api/music/tracks/{id}', { path: { id } }) as {
        id: number
        title: string
        artist_name?: string
        album_title?: string
        duration?: number
        album_id?: number
        artist_id?: number
        artist_slug?: string
        album_slug?: string
      }
      // Stale guard: the session may have moved on during the fetch.
      if (cast.session?.track_id !== id) return
      player.currentTrack = {
        id: d.id,
        title: d.title,
        artist: d.artist_name ?? '',
        album: d.album_title ?? '',
        duration: d.duration ?? 0,
        album_id: d.album_id,
        artist_id: d.artist_id,
        artist_slug: d.artist_slug,
        album_slug: d.album_slug,
        poster: useAlbumCoverUrl(d.artist_slug, d.album_slug) ?? undefined,
      }
      if (d.duration && d.duration > 0) player.duration = d.duration
    } catch { /* display-only — the title/artist off the WS payload still show */ }
  }

  // Scrubber ticker: advance the interpolated position while the receiver
  // plays. 500ms is smooth enough against multi-minute tracks.
  let ticker: ReturnType<typeof setInterval> | null = null
  watch(
    () => cast.engaged && cast.session?.state === 'playing',
    (active) => {
      if (active && ticker === null) {
        ticker = setInterval(() => {
          if (cast.engaged) player.position = cast.livePositionSec()
        }, 500)
      } else if (!active && ticker !== null) {
        clearInterval(ticker)
        ticker = null
      }
    },
    { immediate: true },
  )
})
