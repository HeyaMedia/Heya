// Makes every browser a named, remotely controllable Heya output. Identity is
// installation-scoped (localStorage), while the socket registration is live:
// stale clients age out shortly after their heartbeat stops.
export default defineNuxtPlugin((nuxtApp) => {
  const bus = useEventBus()
  const player = usePlayerStore()
  const qs = useQueueStore()
  const id = clientDeviceID()

  function state() {
    if (qs.targetDeviceID !== qs.outputID) return { controlling_device_id: qs.targetDeviceID }
    return {
      playing: player.playing,
      position_seconds: player.position,
      volume: player.volume,
      track_id: player.currentTrack?.id ?? 0,
      active_output: qs.activeOutput,
    }
  }
  function hello() {
    bus.send({
      type: 'device.hello',
      device: {
        id,
        name: clientDeviceName(),
        kind: clientDeviceKind(),
        capabilities: ['play', 'pause', 'seek', 'volume', 'next', 'previous', 'stop'],
        state: state(),
      },
    })
  }
  function heartbeat() {
    bus.send({ type: 'device.heartbeat', device_id: id, state: state() })
  }

  nuxtApp.hook('app:mounted', () => {
    watch(bus.connected, (up) => { if (up) hello() }, { immediate: true })
    setInterval(heartbeat, 10_000)
  })

  bus.on('device.command', async (ev) => {
    const p = ev.payload as { target_device_id: string, action: string, args?: Record<string, unknown> }
    if (p.target_device_id !== id) return
    const args = p.args ?? {}
    switch (p.action) {
      case 'play': {
        const trackID = Number(args.track_id ?? 0)
        let track = player.queue.find((t) => t.id === trackID)
        if (!track && trackID > 0) {
          const { $heya } = useNuxtApp()
          const d = await $heya('/api/music/tracks/{id}', { path: { id: trackID } }) as any
          track = { id: d.id, title: d.title, artist: d.artist_name ?? '', album: d.album_title ?? '', duration: d.duration ?? 0, album_id: d.album_id, artist_id: d.artist_id, artist_slug: d.artist_slug, album_slug: d.album_slug }
        }
        if (track) {
          await player.play(track, { skipQueueSync: true })
          const seconds = Number(args.position_seconds ?? 0)
          if (seconds > 0 && player.duration > 0) player.seek(seconds / player.duration)
        } else await player.play()
        break
      }
      case 'pause': player.pause(); break
      case 'resume': await player.play(); break
      case 'seek': {
        const seconds = Number(args.seconds ?? 0)
        if (player.duration > 0) player.seek(seconds / player.duration)
        break
      }
      case 'volume': player.setVolume(Number(args.level ?? player.volume)); break
      case 'next': await player.nextTrack(); break
      case 'previous': await player.prevTrack(); break
      case 'stop': player.stop(); break
    }
    heartbeat()
  })
})
