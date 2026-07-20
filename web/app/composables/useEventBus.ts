export interface WsEvent<T = any> {
  type: string
  ts: string
  payload: T
}

export interface LogPayload {
  time?: string
  source?: 'serve' | 'worker' | string
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
// which drops the Pinia Colada catalog cache. The payload is informational; the
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
  /** Resolved server-side from args.library_id so the UI never shows a raw id. */
  library_name?: string
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
  libraries: LibraryScanProgress[] | null
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
  detail?: string
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

const scanProgressState = shallowRef<Record<number, LibraryScanProgress>>({})
const scannerEventsState = shallowRef<Record<number, ScannerEventPayload>>({})
const taskProgressState = shallowRef<Record<string, TaskProgressPayload>>({})
const scanActivityCount = shallowRef(0)
const taskActivityCount = shallowRef(0)

const listeners = new Map<string, Set<EventHandler>>()
const optInEventTypes = new Set(['log'])
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectDelay = 1000

const VISIBLE_PROGRESS_FLUSH_MS = 250
const HIDDEN_PROGRESS_FLUSH_MS = 1500
let scannerEventsRef: Ref<Record<number, ScannerEventPayload>> | null = null
let scanProgressRef: Ref<Record<number, LibraryScanProgress>> | null = null
let taskProgressRef: Ref<Record<string, TaskProgressPayload>> | null = null
let pendingScannerEvents: Record<number, ScannerEventPayload> = {}
let pendingScanProgress: ScanProgressPayload | null = null
let pendingTaskEvents: Record<string, Partial<TaskProgressPayload>> = {}
let pendingTaskDeletes = new Set<string>()
let progressFlushTimer: ReturnType<typeof setTimeout> | null = null
let visibilityFlushWired = false

function syncEventSubscriptions() {
  if (!ws || ws.readyState !== WebSocket.OPEN) return
  const events = [...optInEventTypes].filter(type => (listeners.get(type)?.size ?? 0) > 0)
  ws.send(JSON.stringify({ type: 'subscribe', events }))
}

function progressFlushDelay() {
  if (typeof document === 'undefined') return VISIBLE_PROGRESS_FLUSH_MS
  return document.visibilityState === 'visible' ? VISIBLE_PROGRESS_FLUSH_MS : HIDDEN_PROGRESS_FLUSH_MS
}

function scheduleProgressFlush() {
  if (typeof document !== 'undefined' && !visibilityFlushWired) {
    visibilityFlushWired = true
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') flushProgressEvents()
    })
  }
  if (progressFlushTimer) return
  progressFlushTimer = setTimeout(flushProgressEvents, progressFlushDelay())
}

function mergeTaskProgress(
  prev: Partial<TaskProgressPayload> | undefined,
  p: TaskProgressPayload,
): Partial<TaskProgressPayload> {
  const merged: Partial<TaskProgressPayload> = {
    ...prev,
    task_id: p.task_id,
    state: p.state,
  }
  if (p.pending !== undefined) merged.pending = p.pending
  if (p.running !== undefined) merged.running = p.running
  if (p.current_item) {
    if (p.current_item !== prev?.current_item && p.current_stage === undefined) merged.current_stage = undefined
    merged.current_item = p.current_item
    merged.item_kind = p.item_kind
  }
  if (p.current_stage !== undefined) merged.current_stage = p.current_stage
  return merged
}

function queueScannerEvent(scannerEvents: Ref<Record<number, ScannerEventPayload>>, p: ScannerEventPayload) {
  if (!p.library_id) return
  scannerEventsRef = scannerEvents
  pendingScannerEvents[p.library_id] = p
  scheduleProgressFlush()
}

function queueScanProgress(scanProgress: Ref<Record<number, LibraryScanProgress>>, p: ScanProgressPayload) {
  scanProgressRef = scanProgress
  pendingScanProgress = p
  scheduleProgressFlush()
}

function queueTaskEvent(taskProgress: Ref<Record<string, TaskProgressPayload>>, p: TaskProgressPayload) {
  taskProgressRef = taskProgress
  if (p.state === 'idle') {
    pendingTaskDeletes.add(p.task_id)
    delete pendingTaskEvents[p.task_id]
  } else {
    pendingTaskDeletes.delete(p.task_id)
    pendingTaskEvents[p.task_id] = mergeTaskProgress(pendingTaskEvents[p.task_id], p)
  }
  scheduleProgressFlush()
}

function updateScanActivityCount() {
  scanActivityCount.value = new Set([
    ...Object.keys(scanProgressState.value),
    ...Object.keys(scannerEventsState.value),
  ]).size
}

function updateTaskActivityCount() {
  taskActivityCount.value = Object.keys(taskProgressState.value).length
}

function flushProgressEvents() {
  if (progressFlushTimer) {
    clearTimeout(progressFlushTimer)
    progressFlushTimer = null
  }
  if (scanProgressRef && pendingScanProgress) {
    const next: Record<number, LibraryScanProgress> = {}
    const libraries = Array.isArray(pendingScanProgress.libraries) ? pendingScanProgress.libraries : []
    pendingScanProgress = null
    for (const lib of libraries) {
      next[lib.library_id] = lib
    }
    scanProgressRef.value = next
    updateScanActivityCount()
  }

  if (scannerEventsRef && Object.keys(pendingScannerEvents).length > 0) {
    const next = { ...scannerEventsRef.value, ...pendingScannerEvents }
    pendingScannerEvents = {}
    scannerEventsRef.value = next
    updateScanActivityCount()
  }

  if (taskProgressRef && (pendingTaskDeletes.size > 0 || Object.keys(pendingTaskEvents).length > 0)) {
    const next = { ...taskProgressRef.value }
    for (const id of pendingTaskDeletes) delete next[id]
    pendingTaskDeletes.clear()
    for (const [id, pending] of Object.entries(pendingTaskEvents)) {
      next[id] = mergeTaskProgress(next[id], pending as TaskProgressPayload) as TaskProgressPayload
    }
    pendingTaskEvents = {}
    taskProgressRef.value = next
    updateTaskActivityCount()
  }
}

export function useEventBus() {
  const connected = useState('ws_connected', () => false)
  const activeScans = useState<ScanPayload[]>('ws_active_scans', () => [])
  const activeJobs = useState<ActiveJob[]>('ws_active_jobs', () => [])
  const queueStatus = useState<QueueStatusPayload>('ws_queue_status', () => ({ pending: 0, running: 0 }))
  const scanProgress = scanProgressState
  const scannerEvents = scannerEventsState
  const taskProgress = taskProgressState

  function connect() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return

    const { token } = useAuth()
    if (!token.value) return

    const url = new URL('/api/ws', location.origin)
    url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    url.searchParams.set('subscriptions', '1')
    // Spoofable transport metadata only; WebSocket authentication continues
    // to rely on the same-origin session cookie.
    if (getClientSurface() === 'tauri') {
      url.searchParams.set(CLIENT_SURFACE_WS_PARAM, 'tauri')
    }

    ws = new WebSocket(url.toString())

    ws.onopen = () => {
      connected.value = true
      reconnectDelay = 1000
      syncEventSubscriptions()
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
    if (optInEventTypes.has(type)) syncEventSubscriptions()
    return () => {
      listeners.get(type)?.delete(handler)
      if (optInEventTypes.has(type)) syncEventSubscriptions()
    }
  }

  function send(message: Record<string, unknown>): boolean {
    if (!ws || ws.readyState !== WebSocket.OPEN) return false
    ws.send(JSON.stringify(message))
    return true
  }

  return {
    connected: readonly(connected),
    activeScans: readonly(activeScans),
    activeJobs: readonly(activeJobs),
    queueStatus: readonly(queueStatus),
    scanProgress: readonly(scanProgress),
    scannerEvents: readonly(scannerEvents),
    taskProgress: readonly(taskProgress),
    scanActivityCount: readonly(scanActivityCount),
    taskActivityCount: readonly(taskActivityCount),
    connect,
    disconnect,
    on,
    send,
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
        delete pendingScannerEvents[id]
        const next = { ...scannerEvents.value }
        delete next[id]
        scannerEvents.value = next
        updateScanActivityCount()
      }
      break
    case 'queue.status':
      queueStatus.value = event.payload as QueueStatusPayload
      if (queueStatus.value.pending === 0 && queueStatus.value.running === 0) {
        pendingScanProgress = null
        scanProgress.value = {}
        updateScanActivityCount()
      }
      break
    case 'active_jobs':
      activeJobs.value = (event.payload as ActiveJobsPayload).jobs
      break
    case 'scan.progress': {
      const p = event.payload as ScanProgressPayload
      queueScanProgress(scanProgress, p)
      break
    }
    case 'scan.event': {
      const p = event.payload as ScannerEventPayload
      queueScannerEvent(scannerEvents, p)
      break
    }
    case 'task.progress': {
      // Two emitter sources land on the same event: the 2s periodic
      // ticker carries pending+running counts (no current_item), and
      // per-worker emissions carry current_item+item_kind (no counts).
      // Merge into the existing entry so we keep the latest of both.
      const p = event.payload as TaskProgressPayload
      queueTaskEvent(taskProgress, p)
      break
    }
  }

  const handlers = listeners.get(event.type)
  if (handlers) handlers.forEach(fn => fn(event))

  const wildcards = listeners.get('*')
  if (wildcards) wildcards.forEach(fn => fn(event))
}
