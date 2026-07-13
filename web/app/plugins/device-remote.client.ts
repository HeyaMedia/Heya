// Makes every browser a named, remotely controllable Heya output. Identity is
// installation-scoped (localStorage), while the socket registration is live:
// stale clients age out shortly after their heartbeat stops.
export default defineNuxtPlugin((nuxtApp) => {
  const bus = useEventBus()
  const player = usePlayerStore()
  const qs = useQueueStore()
  const video = useVideoRenderer()
  const id = clientDeviceID()

  function state() {
    if (video.snapshot.value) return video.snapshot.value
    if (qs.targetDeviceID !== qs.outputID) return { controlling_device_id: qs.targetDeviceID }
    return {
      media_kind: 'audio',
      state: player.playing ? 'playing' : 'paused',
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
        capabilities: [
          'play', 'pause', 'seek', 'volume', 'next', 'previous', 'stop',
          'playback.local.audio', 'playback.local.video',
        ],
        state: state(),
      },
    })
  }
  function heartbeat() {
    bus.send({ type: 'device.heartbeat', device_id: id, state: state() })
  }

  let announceTimer: ReturnType<typeof setTimeout> | null = null
  function announceSoon() {
    if (announceTimer) return
    announceTimer = setTimeout(() => {
      announceTimer = null
      heartbeat()
    }, 400)
  }

  nuxtApp.hook('app:mounted', () => {
    watch(bus.connected, (up) => { if (up) hello() }, { immediate: true })
    setInterval(heartbeat, 10_000)
    watch(video.snapshot, announceSoon, { deep: true })
  })

  bus.on('device.command', async (ev) => {
    const p = ev.payload as { target_device_id: string, action: string, args?: Record<string, unknown> }
    if (p.target_device_id !== id) return
    const args = p.args ?? {}
    if (p.action === 'play_video') {
      const fileID = String(args.file_id ?? '')
      if (!fileID) return
      const current = video.snapshot.value
      const position = Number(args.position_seconds ?? 0)
      if (current?.file_id === fileID) {
        if (position > 0) await video.execute('seek', { seconds: position })
        await video.execute('resume')
      } else {
        const query: Record<string, string> = {
          media_item_id: String(args.media_item_id ?? ''),
          title: String(args.title ?? ''),
          entity_type: String(args.entity_type ?? 'movie'),
          entity_id: String(args.entity_id ?? args.media_item_id ?? ''),
        }
        if (position > 0) query.t = String(position)
        await navigateTo({ path: `/watch/${encodeURIComponent(fileID)}`, query })
      }
      await nextTick()
      heartbeat()
      return
    }

    if (video.snapshot.value && await video.execute(p.action, args)) {
      await nextTick()
      heartbeat()
      return
    }
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
