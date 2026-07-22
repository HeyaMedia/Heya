import { acceptHMRUpdate, defineStore } from 'pinia'

// Windowed mirror of the server-owned play queue (docs/queue-plan.md
// Phase B). The server materializes and owns the queue; this store holds
// a contiguous window around the pointer plus the transport meta, applies
// mutations optimistically, and reconciles from the per-user
// `queue.changed` WS events (plugins/queue-live.client.ts). usePlayer
// keeps its public shape and delegates queue semantics here — components
// never talk to this store directly.

export interface QueueItem {
  item_id: number
  ord: number
  track_id: number
  title: string
  duration: number
  disc_number: number
  track_number: number
  album_id: number
  album_title: string
  album_slug: string
  artist_id: number
  artist_name: string
  artist_slug: string
  dj_generated: boolean
  dj_mode?: DJMode
}

export type DJMode = 'off' | 'echo' | 'flow' | 'voyage' | 'encore' | 'spotlight' | 'timewarp'

export const DJ_MODE_LABELS: Record<DJMode, string> = {
  off: 'Off',
  echo: 'Echo',
  flow: 'Flow',
  voyage: 'Voyage',
  encore: 'Encore',
  spotlight: 'Spotlight',
  timewarp: 'Timewarp',
}

export interface QueueViewPayload {
  version: number
  current_item_id?: number
  current_index: number
  total: number
  position_seconds: number
  playing: boolean
  repeat_mode: string
  shuffled: boolean
  dj_mode: DJMode
  active_output?: string
  items: QueueItem[]
  window_start_index: number
}

// Shape of the queue.changed WS payload (eventhub.QueueChangedPayload).
export interface QueueChangedEvent {
  device_id: string
  version: number
  kind: 'replaced' | 'items' | 'pointer' | 'modes' | 'transport' | 'output'
  current_item_id?: number
  track_id?: number
  position_sec: number
  playing: boolean
  repeat_mode: string
  shuffled: boolean
  dj_mode: DJMode
  active_output?: string
}

export interface QueueSourceInput {
  kind: 'album' | 'artist' | 'playlist' | 'genre' | 'library' | 'tracks'
  id?: number
  genre?: string
  track_ids?: number[]
}

// Per-TAB output identity (sessionStorage is per-tab): the queue has one
// active renderer at a time; everyone else mirrors.
function tabOutputID(): string {
  return clientDeviceID()
}

export const useQueueStore = defineStore('playQueue', () => {
  const version = ref(0)
  const items = ref<QueueItem[]>([])
  const windowStart = ref(0) // absolute index of items[0]
  const total = ref(0)
  const currentItemID = ref(0)
  const currentIndex = ref(-1) // absolute
  const positionSeconds = ref(0) // server-coarse (heartbeats)
  const playing = ref(false) // server transport state
  const repeatMode = ref<'off' | 'all' | 'one'>('off')
  const shuffled = ref(false)
  const djMode = ref<DJMode>('off')
  const activeOutput = ref('')
  const loaded = ref(false)

  const outputID = tabOutputID()
  const targetDeviceID = ref(outputID)
  async function queueAPI(path: any, options: any = {}) {
    const { $heya } = useNuxtApp()
    return await ($heya as any)(path, {
      ...options,
      query: { ...(options.query ?? {}), device_id: targetDeviceID.value },
    })
  }
  async function selectTarget(deviceID?: string | null) {
    targetDeviceID.value = deviceID || outputID
    loaded.value = false
    await refetch()
  }
  // '' = unclaimed; treat as "ours to take".
  const isActiveOutput = computed(() => activeOutput.value === '' || activeOutput.value === outputID)

  // Index of the current item WITHIN the window (-1 when outside it) —
  // what usePlayer's played/upcoming slices key on.
  const currentWindowIndex = computed(() => {
    const id = currentItemID.value
    if (!id) return -1
    return items.value.findIndex((i) => i.item_id === id)
  })

  function applyView(v: QueueViewPayload) {
    version.value = v.version
    items.value = v.items ?? []
    windowStart.value = v.window_start_index
    total.value = v.total
    currentItemID.value = v.current_item_id ?? 0
    currentIndex.value = v.current_index
    positionSeconds.value = v.position_seconds
    playing.value = v.playing
    repeatMode.value = (v.repeat_mode as typeof repeatMode.value) || 'off'
    shuffled.value = v.shuffled
    djMode.value = v.dj_mode || 'off'
    activeOutput.value = v.active_output ?? ''
    loaded.value = true
  }

  async function refetch(aroundOrd?: number) {
    const view = await queueAPI('/api/me/queue', {
      query: aroundOrd ? { around: aroundOrd } : {},
    }) as QueueViewPayload
    applyView(view)
    return view
  }

  async function replace(source: QueueSourceInput, startTrackID: number, shuffle: boolean, output?: string) {
    const view = await queueAPI('/api/me/queue', {
      method: 'POST',
      body: {
        source,
        start_track_id: startTrackID,
        shuffle,
        output: output ?? outputID,
      },
    }) as QueueViewPayload
    applyView(view)
    return view
  }

  async function enqueue(trackIDs: number[], at: 'end' | 'next') {
    const res = await queueAPI('/api/me/queue/items', {
      method: 'POST',
      body: { track_ids: trackIDs, at },
    }) as { added: number }
    await refetch()
    return res.added
  }

  async function removeItem(itemID: number) {
    // Optimistic: drop from the window; WS/refetch reconciles.
    const idx = items.value.findIndex((i) => i.item_id === itemID)
    if (idx >= 0) {
      items.value = items.value.toSpliced(idx, 1)
      total.value = Math.max(0, total.value - 1)
    }
    try {
      await queueAPI('/api/me/queue/items/{id}', { method: 'DELETE', path: { id: itemID } })
    } catch {
      await refetch()
    }
  }

  async function moveItem(itemID: number, afterItemID: number) {
    // Optimistic local reorder.
    const from = items.value.findIndex((i) => i.item_id === itemID)
    if (from >= 0) {
      const next = items.value.toSpliced(from, 1)
      const anchor = afterItemID === 0
        ? next.findIndex((i) => i.item_id === currentItemID.value)
        : next.findIndex((i) => i.item_id === afterItemID)
      next.splice(anchor + 1, 0, items.value[from]!)
      items.value = next
    }
    try {
      await queueAPI('/api/me/queue/items/{id}/move', {
        method: 'POST',
        path: { id: itemID },
        body: { after_item_id: afterItemID },
      })
    } catch {
      await refetch()
    }
  }

  async function jump(itemID: number) {
    const view = await queueAPI('/api/me/queue/jump', {
      method: 'POST',
      body: { item_id: itemID },
    }) as QueueViewPayload
    applyView(view)
    return view
  }

  async function advance(fromItemID: number, reason: 'ended' | 'skip' | 'prev') {
    const view = await queueAPI('/api/me/queue/advance', {
      method: 'POST',
      body: { from_item_id: fromItemID, reason },
    }) as QueueViewPayload
    applyView(view)
    return view
  }

  async function setShuffle(on: boolean) {
    const previous = shuffled.value
    shuffled.value = on // optimistic; the items event refetches the order
    try {
      await queueAPI('/api/me/queue/shuffle', { method: 'POST', body: { on } })
    } catch (error) {
      shuffled.value = previous
      throw error
    }
  }

  async function setDJMode(mode: DJMode) {
    const view = await queueAPI('/api/me/queue/dj', {
      method: 'POST',
      body: { mode },
    }) as QueueViewPayload
    applyView(view)
    return view
  }

  async function setRepeat(mode: 'off' | 'all' | 'one') {
    repeatMode.value = mode
    await queueAPI('/api/me/queue/repeat', { method: 'POST', body: { mode } })
  }

  async function clearUpcoming() {
    const idx = currentWindowIndex.value
    if (idx >= 0) {
      items.value = items.value.slice(0, idx + 1)
      total.value = windowStart.value + idx + 1
    }
    try {
      await queueAPI('/api/me/queue/upcoming', { method: 'DELETE' })
    } catch {
      await refetch()
    }
  }

  async function clearAll() {
    items.value = []
    total.value = 0
    currentItemID.value = 0
    currentIndex.value = -1
    playing.value = false
    djMode.value = 'off'
    try {
      await queueAPI('/api/me/queue', { method: 'DELETE' })
    } catch { /* already gone */ }
  }

  async function claim() {
    activeOutput.value = outputID // optimistic
    await queueAPI('/api/me/queue/claim', { method: 'POST', body: { output: outputID } })
  }

  // Fire-and-forget renderer heartbeat. A 409 means another output took
  // over while we were playing — the caller's WS mirror handles the stop.
  function heartbeat(posSeconds: number, isPlaying: boolean, keepalive = false) {
    void queueAPI('/api/me/queue/heartbeat', {
      method: 'POST',
      keepalive,
      body: { output: outputID, position_seconds: Math.max(0, posSeconds), playing: isPlaying },
    }).catch(() => { /* not the active output (or offline) — mirror handles it */ })
  }

  // WS entry point. Returns 'refetch' when the caller should await a
  // window refetch (structural change or version gap), null otherwise.
  function applyEvent(p: QueueChangedEvent): 'refetch' | null {
    if (p.device_id !== targetDeviceID.value) return null
    if (p.kind === 'transport') {
      // No version bump on heartbeats by design.
      positionSeconds.value = p.position_sec
      playing.value = p.playing
      return null
    }
    const gap = p.version > version.value + 1
    if (p.version <= version.value) return null // stale/echo
    version.value = p.version
    repeatMode.value = (p.repeat_mode as typeof repeatMode.value) || repeatMode.value
    shuffled.value = p.shuffled
    djMode.value = p.dj_mode || 'off'
    activeOutput.value = p.active_output ?? ''
    positionSeconds.value = p.position_sec
    playing.value = p.playing

    if (p.kind === 'pointer' || p.kind === 'modes' || p.kind === 'output') {
      const newCurrent = p.current_item_id ?? 0
      const inWindow = items.value.some((i) => i.item_id === newCurrent)
      currentItemID.value = newCurrent
      if (gap || (newCurrent !== 0 && !inWindow)) return 'refetch'
      // Keep the absolute index in step for in-window pointer moves.
      const wIdx = currentWindowIndex.value
      if (wIdx >= 0) currentIndex.value = windowStart.value + wIdx
      return null
    }
    // replaced | items — structural, window is stale.
    return 'refetch'
  }

  return {
    version, items, windowStart, total, currentItemID, currentIndex,
    positionSeconds, playing, repeatMode, shuffled, djMode, activeOutput, loaded,
    outputID, targetDeviceID, isActiveOutput, currentWindowIndex,
    refetch, replace, enqueue, removeItem, moveItem, jump, advance,
    setShuffle, setRepeat, setDJMode, clearUpcoming, clearAll, claim, heartbeat,
    applyEvent, applyView, selectTarget,
  }
})

if (import.meta.hot) import.meta.hot.accept(acceptHMRUpdate(useQueueStore, import.meta.hot))
