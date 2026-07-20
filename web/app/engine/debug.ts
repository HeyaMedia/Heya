// Audio-engine debug logging. Audio behaviour is invisible — you can't see a
// deck swap, a crossfade, or a normalization gain — so this narrates the engine
// to the console. Default ON during development; flip at runtime without a
// rebuild:
//
//   heyaAudio.debug(false)   // silence it
//   heyaAudio.debug(true)    // turn it back on
//   heyaAudio.enabled        // current state
//
// A persisted override in localStorage ('heya_audio_debug') wins over the
// default, so a preference survives reloads.

declare global {
  interface Window {
    heyaAudio?: { debug: (v?: boolean) => boolean; readonly enabled: boolean }
  }
}

let enabled = true

if (import.meta.client) {
  try {
    const stored = localStorage.getItem('heya_audio_debug')
    if (stored !== null) enabled = stored === '1'
  } catch { /* localStorage unavailable — keep the default */ }

  window.heyaAudio = {
    debug(v = true) {
      enabled = !!v
      try { localStorage.setItem('heya_audio_debug', enabled ? '1' : '0') } catch { /* ignore */ }
      // eslint-disable-next-line no-console
      console.log(`%c♪ audio debug ${enabled ? 'ON' : 'OFF'}`, 'color:#e6b94a;font-weight:bold')
      return enabled
    },
    get enabled() { return enabled },
  }
}

export function audioDebugEnabled() { return enabled }

// Scoped log line. `scope` is a short tag — 'player' | 'sched' | 'xfade' |
// 'deck' | 'norm' | 'dsp' | 'engine' | 'scrobble'. Colour-coded so a stream of
// them is scannable.
const SCOPE_COLOR: Record<string, string> = {
  player: '#e6b94a',
  sched: '#6ad1e6',
  xfade: '#c47aef',
  deck: '#7ae6a4',
  norm: '#e67a9c',
  dsp: '#e6a14a',
  engine: '#9ca3af',
  scrobble: '#a4e67a',
}

export function alog(scope: string, msg: string, data?: unknown) {
  if (!enabled || import.meta.server) return
  const color = SCOPE_COLOR[scope] ?? '#e6b94a'
  const style = `color:${color};font-weight:bold`
  // eslint-disable-next-line no-console
  if (data !== undefined) console.log(`%c♪ ${scope}`, style, msg, data)
  // eslint-disable-next-line no-console
  else console.log(`%c♪ ${scope}`, style, msg)
}

// Strip capability/session-routing query parameters so logged URLs stay readable.
export function shortUrl(u?: string | null): string {
  if (!u) return String(u)
  return u.split('?')[0] ?? u
}
