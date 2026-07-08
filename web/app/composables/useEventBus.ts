export interface WsEvent<T = any> {
  type: string
  ts: string
  payload: T
}

export interface LogPayload {
  level: string
  message: string
  fields?: Record<string, any>
}

export interface ScanPayload {
  library_id: number
  library_name?: string
  discovered?: number
  new?: number
  missing?: number
}

export interface MediaPayload {
  media_item_id: number
  library_id?: number
  title?: string
  media_type?: string
}

// library.deleted — fired by the backend after a library (and its cascade)
// is removed. Consumed globally by plugins/cache-invalidation.client.ts,
// which drops the vue-query catalog cache. The payload is informational; the
// handler invalidates regardless of which library/type it was.
export interface LibraryDeletedPayload {
  library_id: number
  name?: string
  media_type?: string
}

export interface WatchPayload {
  user_id: number
  media_item_id: number
  progress_seconds: number
  total_seconds: number
  completed: boolean
}

export interface QueueStatusPayload {
  pending: number
  running: number
}

export interface ActiveJob {
  id: number
  kind: string
  queue: string
  started_at?: string
  args?: string
}

export interface ActiveJobsPayload {
  jobs: ActiveJob[]
}

export interface LibraryScanProgress {
  library_id: number
  name: string
  total: number
  processed: number
  matched: number
  unmatched: number
  errors: number
}

export interface ScanProgressPayload {
  libraries: LibraryScanProgress[]
}

export interface ScannerEventPayload {
  seq: number
  event: string
  severity?: string
  library_id: number
  library_name?: string
  library_type?: string
  domain?: string
  worker?: string
  phase?: string
  root?: string
  path?: string
  rel_path?: string
  kind?: string
  reason?: string
  message?: string
  data?: Record<string, any>
}

export interface StatsPayload {
  libraries: number
  media_counts: Record<string, number>
  total_media: number
  total_people: number
  total_files: number
  queue_pending: number
  queue_running: number
}

// task.progress carries two complementary signals on the same event:
//   - The periodic emitter in internal/eventhub/periodic.go fires one
//     event per task every 2s with {pending, running, state}, leaving
//     current_item empty.
//   - The per-worker emitter (worker.TaskProgressBroadcaster) fires on
//     demand with {current_item, item_kind}, leaving counts at zero.
// The merge logic in useEventBus stores both halves in the same dict
// entry, so consumers always see the latest counts + the latest item.
export interface TaskProgressPayload {
  task_id: string
  state: string
  pending?: number
  running?: number
  current_item?: string
  item_kind?: string
  // current_stage is a finer "within the current item" label —
  // currently only populated by analyze_track_facets (one event per
  // pipeline stage). Lets the UI show item + stage on two lines.
  current_stage?: string
}

type EventHandler = (event: WsEvent) => void

const listeners = new Map<string, Set<EventHandler>>()
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectDelay = 1000

export function useEventBus() {
  const connected = useState('ws_connected', () => false)
  const activeScans = useState<ScanPayload[]>('ws_active_scans', () => [])
  const activeJobs = useState<ActiveJob[]>('ws_active_jobs', () => [])
  const queueStatus = useState<QueueStatusPayload>('ws_queue_status', () => ({ pending: 0, running: 0 }))
  const scanProgress = useState<Record<number, LibraryScanProgress>>('ws_scan_progress', () => ({}))
  const scannerEvents = useState<Record<number, ScannerEventPayload>>('ws_scanner_events', () => ({}))
  const taskProgress = useState<Record<string, TaskProgressPayload>>('ws_task_progress', () => ({}))

  function connect() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return

    const { token } = useAuth()
    if (!token.value) return

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${location.host}/api/ws?token=${token.value}`

    ws = new WebSocket(url)

    ws.onopen = () => {
      connected.value = true
      reconnectDelay = 1000
    }

    ws.onmessage = (msg) => {
      try {
        const event = JSON.parse(msg.data) as WsEvent
        handleEvent(event, connected, activeScans, activeJobs, queueStatus, scanProgress, scannerEvents, taskProgress)
      } catch {}
    }

    ws.onclose = () => {
      connected.value = false
      ws = null
      scheduleReconnect()
    }

    ws.onerror = () => {
      ws?.close()
    }
  }

  function disconnect() {
    if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null }
    ws?.close()
    ws = null
    connected.value = false
  }

  function scheduleReconnect() {
    if (reconnectTimer) clearTimeout(reconnectTimer)
    const { token } = useAuth()
    if (!token.value) return
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      connect()
      reconnectDelay = Math.min(reconnectDelay * 2, 30000)
    }, reconnectDelay)
  }

  function on(type: string, handler: EventHandler): () => void {
    if (!listeners.has(type)) listeners.set(type, new Set())
    listeners.get(type)!.add(handler)
    return () => { listeners.get(type)?.delete(handler) }
  }

  return {
    connected: readonly(connected),
    activeScans: readonly(activeScans),
    activeJobs: readonly(activeJobs),
    queueStatus: readonly(queueStatus),
    scanProgress: readonly(scanProgress),
    scannerEvents: readonly(scannerEvents),
    taskProgress: readonly(taskProgress),
    connect,
    disconnect,
    on,
  }
}

function handleEvent(
  event: WsEvent,
  connected: Ref<boolean>,
  activeScans: Ref<ScanPayload[]>,
  activeJobs: Ref<ActiveJob[]>,
  queueStatus: Ref<QueueStatusPayload>,
  scanProgress: Ref<Record<number, LibraryScanProgress>>,
  scannerEvents: Ref<Record<number, ScannerEventPayload>>,
  taskProgress: Ref<Record<string, TaskProgressPayload>>,
) {
  switch (event.type) {
    case 'scan.started': {
      const p = event.payload as ScanPayload
      activeScans.value = [...activeScans.value.filter(s => s.library_id !== p.library_id), p]
      break
    }
    case 'scan.completed':
      activeScans.value = activeScans.value.filter(s => s.library_id !== (event.payload as ScanPayload).library_id)
      {
        const id = (event.payload as ScanPayload).library_id
        const next = { ...scannerEvents.value }
        delete next[id]
        scannerEvents.value = next
      }
      break
    case 'queue.status':
      queueStatus.value = event.payload as QueueStatusPayload
      if (queueStatus.value.pending === 0 && queueStatus.value.running === 0) {
        scanProgress.value = {}
      }
      break
    case 'active_jobs':
      activeJobs.value = (event.payload as ActiveJobsPayload).jobs
      break
    case 'scan.progress': {
      const p = event.payload as ScanProgressPayload
      const next: Record<number, LibraryScanProgress> = {}
      for (const lib of p.libraries) {
        next[lib.library_id] = lib
      }
      scanProgress.value = next
      break
    }
    case 'scan.event': {
      const p = event.payload as ScannerEventPayload
      if (p.library_id) {
        scannerEvents.value = { ...scannerEvents.value, [p.library_id]: p }
      }
      break
    }
    case 'task.progress': {
      // Two emitter sources land on the same event: the 2s periodic
      // ticker carries pending+running counts (no current_item), and
      // per-worker emissions carry current_item+item_kind (no counts).
      // Merge into the existing entry so we keep the latest of both.
      const p = event.payload as TaskProgressPayload
      if (p.state === 'idle') {
        const next = { ...taskProgress.value }
        delete next[p.task_id]
        taskProgress.value = next
      } else {
        const prev = taskProgress.value[p.task_id] ?? { task_id: p.task_id, state: 'running' }
        const merged: TaskProgressPayload = {
          ...prev,
          state: p.state,
        }
        // Counts come from the periodic emitter — only present on its
        // events. If undefined here, keep the previous count.
        if (p.pending !== undefined) merged.pending = p.pending
        if (p.running !== undefined) merged.running = p.running
        // current_item comes from per-worker emits — only present there.
        if (p.current_item) {
          merged.current_item = p.current_item
          merged.item_kind = p.item_kind
        }
        // current_stage is the sub-step (Discogs heads, CLAP audio,
        // …) from per-stage emits; only sonic_analysis fires these.
        // Reset to undefined if the current item changed so the
        // stage label doesn't bleed from one track to the next.
        if (p.current_stage !== undefined) {
          merged.current_stage = p.current_stage
        } else if (p.current_item && p.current_item !== prev.current_item) {
          merged.current_stage = undefined
        }
        taskProgress.value = { ...taskProgress.value, [p.task_id]: merged }
      }
      break
    }
  }

  const handlers = listeners.get(event.type)
  if (handlers) handlers.forEach(fn => fn(event))

  const wildcards = listeners.get('*')
  if (wildcards) wildcards.forEach(fn => fn(event))
}
