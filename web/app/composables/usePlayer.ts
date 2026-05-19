export interface Track {
  id: number
  title: string
  artist: string
  album: string
  duration: number
  poster?: string
  loved?: boolean
}

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

  let ticker: ReturnType<typeof setInterval> | null = null

  function play(track?: Track) {
    if (track) {
      currentTrack.value = track
      duration.value = track.duration
      position.value = 0
    }
    playing.value = true
    startTicker()
  }

  function pause() {
    playing.value = false
    stopTicker()
  }

  function togglePlay() {
    if (playing.value) pause()
    else play()
  }

  function seek(pct: number) {
    position.value = Math.floor(duration.value * Math.max(0, Math.min(1, pct)))
  }

  function setVolume(v: number) {
    volume.value = Math.max(0, Math.min(100, v))
    if (v > 0) muted.value = false
  }

  function toggleMute() {
    muted.value = !muted.value
  }

  function toggleShuffle() {
    shuffled.value = !shuffled.value
  }

  function cycleRepeat() {
    const modes: Array<'off' | 'all' | 'one'> = ['off', 'all', 'one']
    const idx = modes.indexOf(repeatMode.value)
    repeatMode.value = modes[(idx + 1) % 3]
  }

  function nextTrack() {
    if (!queue.value.length) return
    const idx = queue.value.findIndex(t => t.id === currentTrack.value?.id)
    const next = queue.value[(idx + 1) % queue.value.length]
    if (next) play(next)
  }

  function prevTrack() {
    if (position.value > 3) { position.value = 0; return }
    if (!queue.value.length) return
    const idx = queue.value.findIndex(t => t.id === currentTrack.value?.id)
    const prev = queue.value[(idx - 1 + queue.value.length) % queue.value.length]
    if (prev) play(prev)
  }

  function toggleLoved() {
    if (currentTrack.value) {
      currentTrack.value = { ...currentTrack.value, loved: !currentTrack.value.loved }
    }
  }

  function toggleQueue() { queueOpen.value = !queueOpen.value }
  function toggleLyrics() { lyricsOpen.value = !lyricsOpen.value }

  function startTicker() {
    stopTicker()
    ticker = setInterval(() => {
      if (playing.value && position.value < duration.value) {
        position.value++
      } else if (position.value >= duration.value) {
        nextTrack()
      }
    }, 1000)
  }

  function stopTicker() {
    if (ticker) { clearInterval(ticker); ticker = null }
  }

  function formatTime(s: number) {
    const m = Math.floor(s / 60)
    const sec = Math.floor(s % 60)
    return `${m}:${sec.toString().padStart(2, '0')}`
  }

  return {
    playing, currentTrack, position, duration, volume, muted,
    shuffled, repeatMode, queue, queueOpen, lyricsOpen,
    play, pause, togglePlay, seek, setVolume, toggleMute,
    toggleShuffle, cycleRepeat, nextTrack, prevTrack,
    toggleLoved, toggleQueue, toggleLyrics, formatTime,
  }
}
