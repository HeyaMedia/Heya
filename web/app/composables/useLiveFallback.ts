// useLiveFallback bridges WebSocket-driven pages over connection outages.
//
// Pages like /settings/jobs, /settings/tasks, /settings/dashboard rely on
// `useEventBus()` events to keep their view fresh. When the WS drops — air
// rebuild, network blip, tab backgrounded long enough — those pages would
// otherwise silently freeze. This composable:
//
//   - runs an initial fetch on mount (so the page paints before the first
//     event arrives)
//   - starts a polling fallback at `pollWhileOffline` ms while `connected`
//     is false
//   - fires an explicit refetch the moment `connected` transitions back to
//     true (catches up on events missed during the outage)
//   - tears down the polling timer on unmount
//
// The fallback is additive — pages that have their own polling (Dashboard's
// 5s queue refresh, Logs' SSE backfill) can still use this for the
// reconnect-catchup behaviour by passing pollWhileOffline=0 and immediate=false.

export interface LiveFallbackOptions {
  /**
   * Milliseconds between polls while the WS is offline. Set to 0 to disable
   * the polling fallback entirely (still gives you the reconnect catchup).
   * Defaults to 5000.
   */
  pollWhileOffline?: number
  /**
   * Whether to call refetch() once at mount-time. Set false when the
   * consumer already does its own initial fetch and just wants the
   * reconnect-catchup behaviour. Defaults to true.
   */
  immediate?: boolean
}

export function useLiveFallback(
  refetch: () => void | Promise<void>,
  opts: LiveFallbackOptions = {},
) {
  const { connected } = useEventBus()
  const pollMs = opts.pollWhileOffline ?? 5000
  const immediate = opts.immediate ?? true

  let pollTimer: ReturnType<typeof setInterval> | null = null
  let lastConnected = connected.value

  function startPolling() {
    if (pollTimer || pollMs <= 0) return
    pollTimer = setInterval(refetch, pollMs)
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  // React to WS state transitions:
  //   true  → false : start polling (cover the outage)
  //   false → true  : refetch once + stop polling (catch up, then rely on WS)
  watch(connected, (now) => {
    if (now && !lastConnected) {
      refetch()
      stopPolling()
    } else if (!now && lastConnected) {
      startPolling()
    }
    lastConnected = now
  })

  // Honour the initial state — if the page mounts while WS is offline (rare
  // but possible during a hard refresh into a slow backend), start polling
  // immediately rather than waiting for the next transition.
  if (!connected.value) startPolling()
  if (immediate) refetch()

  onUnmounted(stopPolling)

  return {
    /** Mirrored from useEventBus so callers don't double-import. */
    connected,
    /** Manually trigger a refresh (e.g. from a button). */
    refetch,
  }
}
