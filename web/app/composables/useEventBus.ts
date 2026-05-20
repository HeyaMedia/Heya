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

export interface StatsPayload {
  libraries: number
  media_counts: Record<string, number>
  total_media: number
  total_people: number
  total_files: number
  queue_pending: number
  queue_running: number
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
        handleEvent(event, connected, activeScans, activeJobs, queueStatus, scanProgress)
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
) {
  switch (event.type) {
    case 'scan.started': {
      const p = event.payload as ScanPayload
      activeScans.value = [...activeScans.value.filter(s => s.library_id !== p.library_id), p]
      break
    }
    case 'scan.completed':
      activeScans.value = activeScans.value.filter(s => s.library_id !== (event.payload as ScanPayload).library_id)
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
  }

  const handlers = listeners.get(event.type)
  if (handlers) handlers.forEach(fn => fn(event))

  const wildcards = listeners.get('*')
  if (wildcards) wildcards.forEach(fn => fn(event))
}
