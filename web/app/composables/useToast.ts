// Global toast notification queue — the app-wide counterpart to the
// settings-scoped useFlash(). Module-level reactive state (not a
// provide/inject context) so any component, anywhere, can call
// `useToast().toast(...)` without needing to sit under a particular
// provider — mirrors the singleton pattern useConfirm() already uses for
// the same reason (ConfirmDialog is mounted once, callers never see it).
//
// Mount <AppToastHost /> once (app.vue) to render the queue; this file only
// owns state + the push/dismiss API.
//
// Usage:
//   const { toast } = useToast()
//   toast('Saved')                          // defaults to tone: 'info'
//   toast({ message: 'Added 12 tracks to Chill Mix', tone: 'ok' })
//   toast.ok('Added 12 tracks to Chill Mix')
//   toast.err('Could not reach server')
//
// Deliberately tiny: no actions/buttons, no queueing beyond the visible cap.
// Every toast auto-dismisses and can be tapped away early.

export type ToastTone = 'ok' | 'err' | 'info'

export interface ToastOptions {
  message: string
  tone?: ToastTone
  /** Icon name — must exist in Icon.vue's nameMap. Defaults per tone. */
  icon?: string
  /** Auto-dismiss delay in ms. */
  duration?: number
}

export interface ToastItem {
  id: number
  message: string
  tone: ToastTone
  icon: string
  duration: number
}

// Icons already mapped in components/icons/Icon.vue — not introducing new
// ones here.
const DEFAULT_ICON: Record<ToastTone, string> = {
  ok: 'check',
  err: 'warning',
  info: 'info',
}

const DEFAULT_DURATION = 3200
const MAX_VISIBLE = 3

// Oldest first, newest last — AppToastHost renders the array in order so
// the newest toast lands at the bottom of the stack.
const toasts = ref<ToastItem[]>([])
const timers = new Map<number, ReturnType<typeof setTimeout>>()
let nextId = 0

function clearTimer(id: number) {
  const t = timers.get(id)
  if (t !== undefined) {
    clearTimeout(t)
    timers.delete(id)
  }
}

function dismiss(id: number) {
  clearTimer(id)
  const idx = toasts.value.findIndex(t => t.id === id)
  if (idx !== -1) toasts.value.splice(idx, 1)
}

function push(opts: ToastOptions): number {
  const id = ++nextId
  const tone = opts.tone ?? 'info'
  const duration = opts.duration ?? DEFAULT_DURATION
  toasts.value.push({
    id,
    message: opts.message,
    tone,
    icon: opts.icon ?? DEFAULT_ICON[tone],
    duration,
  })

  // Cap visible toasts: drop the oldest overflow immediately so the queue
  // never grows past MAX_VISIBLE. Vue's TransitionGroup still animates the
  // removal since it's a normal splice.
  while (toasts.value.length > MAX_VISIBLE) {
    const oldest = toasts.value[0]
    if (!oldest) break
    dismiss(oldest.id)
  }

  timers.set(id, setTimeout(() => dismiss(id), duration))
  return id
}

type ToastCall = (opts: ToastOptions | string) => number
export interface ToastFn extends ToastCall {
  ok(message: string, opts?: Omit<ToastOptions, 'message' | 'tone'>): number
  err(message: string, opts?: Omit<ToastOptions, 'message' | 'tone'>): number
  info(message: string, opts?: Omit<ToastOptions, 'message' | 'tone'>): number
}

const toast = ((opts: ToastOptions | string) => {
  return push(typeof opts === 'string' ? { message: opts } : opts)
}) as ToastFn

toast.ok = (message, opts) => push({ ...opts, message, tone: 'ok' })
toast.err = (message, opts) => push({ ...opts, message, tone: 'err' })
toast.info = (message, opts) => push({ ...opts, message, tone: 'info' })

export function useToast() {
  return { toast, toasts, dismiss }
}
