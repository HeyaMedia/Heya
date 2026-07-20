#!/usr/bin/env bun
/*
 * Heya Eye — a Chrome DevTools Protocol driver for visual debugging.
 *
 * Subcommands:
 *   start [--window-size WxH]  Launch headless Chrome with remote debugging.
 *   stop            Kill the running Chrome.
 *   login [user pw] Hit /api/auth/login and stash the token in localStorage.
 *   goto <url>      Navigate the current tab (default: http://localhost:8080/).
 *   shot <out.png>  Capture full-page screenshot.
 *   eval <js>       Run JS in the page; prints the JSON result.
 *   click <selector> Click an element by CSS selector.
 *   dom <selector>  Print outerHTML for the matched element.
 *   style <selector> [prop ...] Print computed-style key=value pairs.
 *   reload          Hard-reload the current tab.
 *   focus <selector> Focus an input (without clicking).
 *   type <text>     Type text into the focused element.
 *   wait <selector> [timeout-ms] Poll until selector appears (or vanishes with !sel).
 *   sleep <ms>      Block for ms milliseconds (cheap settle wait).
 *   viewport <WxH> [--dpr N] [--touch]  Persist a mobile-viewport override
 *                   applied on every subsequent connect(); `viewport off` clears it.
 *
 * State is persisted in /tmp/heya-eye/state.json (debugger ws URL + chrome PID).
 */

import { spawn } from 'node:child_process'
import { existsSync, readFileSync, writeFileSync, mkdirSync, rmSync } from 'node:fs'

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
// HEYA_EYE_PORT isolates concurrent Eye instances (parallel agents each get
// their own Chrome, state, and profile) — everything below is keyed off the
// port so two instances can never fight over one browser or state file.
const PORT = Number(process.env.HEYA_EYE_PORT) || 9223
const STATE_DIR = PORT === 9223 ? '/tmp/heya-eye' : `/tmp/heya-eye-${PORT}`
const STATE_FILE = `${STATE_DIR}/state.json`
const PROFILE_DIR = `${STATE_DIR}/profile`
const DEFAULT_ORIGIN = 'http://localhost:8080'
const DEFAULT_WINDOW_SIZE = { width: 1600, height: 1000 }

interface ViewportOverride {
  width: number
  height: number
  dpr: number
  mobile: boolean
  touch: boolean
}

interface State {
  pid: number
  wsUrl: string
  origin: string
  windowSize?: { width: number; height: number }
  viewport?: ViewportOverride
}

mkdirSync(STATE_DIR, { recursive: true })

function loadState(): State | null {
  if (!existsSync(STATE_FILE)) return null
  try { return JSON.parse(readFileSync(STATE_FILE, 'utf8')) } catch { return null }
}
function saveState(s: State) { writeFileSync(STATE_FILE, JSON.stringify(s, null, 2)) }
function clearState() { try { rmSync(STATE_FILE) } catch {} }

async function sleep(ms: number) { return new Promise(r => setTimeout(r, ms)) }

async function startChrome(windowSize = '1600,1000'): Promise<State> {
  // Kill any zombie chrome on our port first.
  const existing = loadState()
  if (existing) {
    try { process.kill(existing.pid, 0); console.log(`Chrome already running (pid ${existing.pid})`); return existing } catch {}
    clearState()
  }
  const child = spawn(CHROME, [
    '--headless=new',
    `--remote-debugging-port=${PORT}`,
    '--disable-gpu',
    '--no-first-run',
    '--no-default-browser-check',
    '--hide-scrollbars',
    `--user-data-dir=${PROFILE_DIR}`,
    `--window-size=${windowSize}`,
    'about:blank',
  ], { detached: true, stdio: 'ignore' })
  child.unref()
  // Wait for the debugging port to be ready.
  for (let i = 0; i < 40; i++) {
    await sleep(100)
    try {
      const r = await fetch(`http://localhost:${PORT}/json/version`)
      if (r.ok) break
    } catch {}
  }
  const targets = await (await fetch(`http://localhost:${PORT}/json`)).json() as any[]
  const tab = targets.find(t => t.type === 'page') ?? targets[0]
  const [w, h] = windowSize.split(',').map(n => parseInt(n, 10))
  const state: State = {
    pid: child.pid!,
    wsUrl: tab.webSocketDebuggerUrl,
    origin: DEFAULT_ORIGIN,
    windowSize: { width: w, height: h },
  }
  saveState(state)
  console.log(`Chrome started (pid ${state.pid})`)
  return state
}

function requireState(): State {
  const s = loadState()
  if (!s) throw new Error('Chrome not running. Run: bun tools/eye/eye.ts start')
  return s
}

class CDP {
  ws: WebSocket
  pending = new Map<number, (r: any, err?: any) => void>()
  events = new Map<string, ((p: any) => void)[]>()
  msgId = 1
  ready: Promise<void>
  constructor(wsUrl: string) {
    this.ws = new WebSocket(wsUrl)
    this.ready = new Promise((resolve, reject) => {
      this.ws.addEventListener('open', () => resolve(), { once: true })
      this.ws.addEventListener('error', e => reject(e), { once: true })
    })
    this.ws.addEventListener('message', ev => {
      const m = JSON.parse(ev.data as string)
      if (m.id != null) {
        const cb = this.pending.get(m.id)
        if (cb) { this.pending.delete(m.id); cb(m.result, m.error) }
      } else if (m.method) {
        const subs = this.events.get(m.method)
        if (subs) for (const fn of subs) fn(m.params)
      }
    })
  }
  send(method: string, params: any = {}): Promise<any> {
    const id = this.msgId++
    return new Promise((resolve, reject) => {
      this.pending.set(id, (r, err) => err ? reject(new Error(err.message)) : resolve(r))
      this.ws.send(JSON.stringify({ id, method, params }))
    })
  }
  on(method: string, fn: (p: any) => void) {
    if (!this.events.has(method)) this.events.set(method, [])
    this.events.get(method)!.push(fn)
  }
  close() { this.ws.close() }
}

// If a viewport override is saved in state, apply it to a freshly-connected
// CDP session. Every subcommand re-applies this on connect — there's no
// persistent browser-side state we can rely on since each subcommand opens
// its own CDP session.
//
// Headless Chrome has no real OS window separate from the emulated render
// surface: calling Emulation.setDeviceMetricsOverride actually resizes that
// surface, and it stays resized across separate debugger sessions attaching
// to the same target. `Emulation.clearDeviceMetricsOverride` does NOT
// restore the original launch size in this mode (verified empirically) — so
// the "no override" branch re-asserts the desktop dimensions explicitly
// instead of merely clearing, otherwise `viewport off` (or any desktop
// command run after an emulated one) would keep seeing the stale mobile size.
async function applyViewportOverride(cdp: CDP, s: State) {
  const v = s.viewport
  if (v) {
    await cdp.send('Emulation.setDeviceMetricsOverride', {
      width: v.width,
      height: v.height,
      deviceScaleFactor: v.dpr,
      // Always false — never trust v.mobile from a stale state file. See the
      // comment in cmd_viewport: mobile:true lets content overflow silently
      // widen the layout viewport, which falsifies breakage screenshots.
      mobile: false,
    })
    await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: v.touch })
  } else {
    const desktop = s.windowSize ?? DEFAULT_WINDOW_SIZE
    await cdp.send('Emulation.setDeviceMetricsOverride', {
      width: desktop.width,
      height: desktop.height,
      deviceScaleFactor: 1,
      mobile: false,
    })
    await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: false })
  }
}

async function connect(): Promise<CDP> {
  const s = requireState()
  const cdp = new CDP(s.wsUrl)
  await cdp.ready
  await cdp.send('Page.enable')
  await cdp.send('DOM.enable')
  await cdp.send('Runtime.enable')
  await applyViewportOverride(cdp, s)
  return cdp
}

// Run a one-shot CDP session that captures all console.* output and JS
// exceptions for `durationMs` while running the navigation in `nav()`.
async function captureConsole(nav: (cdp: CDP) => Promise<void>, durationMs = 4000): Promise<Array<{ kind: string; text: string }>> {
  const s = requireState()
  const cdp = new CDP(s.wsUrl)
  await cdp.ready
  await cdp.send('Runtime.enable')
  await cdp.send('Page.enable')
  await applyViewportOverride(cdp, s)
  const lines: Array<{ kind: string; text: string }> = []
  cdp.on('Runtime.consoleAPICalled', (p: any) => {
    const text = (p.args ?? []).map((a: any) => a.value ?? a.description ?? a.unserializableValue ?? '').join(' ')
    lines.push({ kind: p.type, text })
  })
  cdp.on('Runtime.exceptionThrown', (p: any) => {
    const ex = p.exceptionDetails
    lines.push({ kind: 'exception', text: ex?.exception?.description ?? ex?.text ?? JSON.stringify(p) })
  })
  await nav(cdp)
  await sleep(durationMs)
  cdp.close()
  return lines
}

async function waitForLoad(cdp: CDP, timeoutMs = 5000) {
  // Wait for Page.loadEventFired or timeout. Then a small settle delay for SPA hydration.
  await new Promise<void>(resolve => {
    let done = false
    const t = setTimeout(() => { if (!done) { done = true; resolve() } }, timeoutMs)
    cdp.on('Page.loadEventFired', () => {
      if (!done) { done = true; clearTimeout(t); resolve() }
    })
  })
  await sleep(800)
}

async function evalJs(cdp: CDP, expression: string, awaitPromise = false): Promise<any> {
  const r = await cdp.send('Runtime.evaluate', {
    expression,
    returnByValue: true,
    awaitPromise,
    userGesture: true,
  })
  if (r.exceptionDetails) {
    throw new Error(r.exceptionDetails.exception?.description ?? r.exceptionDetails.text)
  }
  return r.result?.value
}

async function cmd_start(...args: string[]) {
  let windowSize = '1600,1000'
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--window-size') {
      const val = args[++i]
      const m = val?.match(/^(\d+)x(\d+)$/)
      if (!m) throw new Error(`start: --window-size expects "<width>x<height>", got "${val}"`)
      windowSize = `${m[1]},${m[2]}`
    }
  }
  await startChrome(windowSize)
}

async function cmd_stop() {
  const s = loadState()
  if (!s) { console.log('Chrome not running.'); return }
  try { process.kill(s.pid, 9) } catch {}
  clearState()
  console.log('Chrome stopped.')
}

async function cmd_viewport(spec?: string, ...rest: string[]) {
  const s = requireState()
  if (!spec || spec === 'off') {
    delete s.viewport
    saveState(s)
    console.log('Viewport override: off (desktop default)')
    return
  }
  const m = spec.match(/^(\d+)x(\d+)$/)
  if (!m) throw new Error(`viewport: expected "<width>x<height>" or "off", got "${spec}"`)
  let dpr = 2
  let touch = false
  for (let i = 0; i < rest.length; i++) {
    if (rest[i] === '--dpr') dpr = parseFloat(rest[++i])
    else if (rest[i] === '--touch') touch = true
  }
  // mobile:false is deliberate. With mobile:true, whenever page content
  // overflows the requested width (e.g. a shell with a larger min-content
  // width than the emulated viewport), Chrome's mobile emulation zooms out
  // to fit: the layout viewport silently BECOMES the content width, media
  // queries evaluate in the wrong band, and screenshots show a "working"
  // desktop layout at a width that is actually broken. mobile:false pins the
  // layout viewport at exactly WxH and lets overflow be overflow — which is
  // the whole point of viewport verification. Touch emulation is orthogonal
  // and still driven by --touch.
  s.viewport = { width: parseInt(m[1], 10), height: parseInt(m[2], 10), dpr, mobile: false, touch }
  saveState(s)
  console.log(`Viewport override: ${s.viewport.width}x${s.viewport.height} @${dpr}x${touch ? ', touch' : ''}`)
}

async function cmd_login(user = 'admin', pw = 'admin') {
  const s = requireState()
  // 1) Get a token via the API (independent of browser state).
  const res = await fetch(`${s.origin}/api/auth/login`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ username: user, password: pw }),
  })
  if (!res.ok) throw new Error(`login failed: ${res.status} ${await res.text()}`)
  const { token } = await res.json() as { token: string }
  // 2) Park the tab on /login (any same-origin URL works; /login won't bounce).
  const cdp = await connect()
  await cdp.send('Page.navigate', { url: `${s.origin}/login` })
  await waitForLoad(cdp, 4000)
  // 3) Stash the token; subsequent goto / will let useAuth().hydrate() pick it up.
  await evalJs(cdp, `localStorage.setItem('heya_token', ${JSON.stringify(token)})`)
  console.log(`Logged in as ${user}; token stashed (${token.slice(0, 8)}…).`)
  cdp.close()
}

async function cmd_goto(url: string) {
  const cdp = await connect()
  const target = url.startsWith('http') ? url : `${requireState().origin}${url.startsWith('/') ? url : '/' + url}`
  await cdp.send('Page.navigate', { url: target })
  await waitForLoad(cdp, 6000)
  console.log(`Navigated to ${target}`)
  cdp.close()
}

async function cmd_shot(out = '/tmp/heya-eye/shot.png', selector?: string, padStr = '16') {
  const cdp = await connect()
  const params: any = { format: 'png', captureBeyondViewport: true }
  if (selector) {
    const pad = parseInt(padStr, 10) || 0
    const rect = await evalJs(cdp, `
      (() => {
        const el = document.querySelector(${JSON.stringify(selector)});
        if (!el) return null;
        const r = el.getBoundingClientRect();
        return { x: r.left, y: r.top, w: r.width, h: r.height };
      })()
    `)
    if (rect) {
      params.clip = {
        x: Math.max(0, Math.floor(rect.x - pad)),
        y: Math.max(0, Math.floor(rect.y - pad)),
        width: Math.ceil(rect.w + pad * 2),
        height: Math.ceil(rect.h + pad * 2),
        scale: 1,
      }
    } else {
      console.error(`shot: selector ${selector} not found, taking full page`)
    }
  }
  const r = await cdp.send('Page.captureScreenshot', params)
  writeFileSync(out, Buffer.from(r.data, 'base64'))
  console.log(`Screenshot → ${out}${params.clip ? ` (clip ${params.clip.width}×${params.clip.height})` : ''}`)
  cdp.close()
}

async function cmd_eval(expr: string) {
  const cdp = await connect()
  // Always await promises so async IIFEs work transparently — the
  // evaluator unwraps a Promise return into its resolved value instead
  // of returning {} (the JSON-serialised Promise object).
  const v = await evalJs(cdp, expr, true)
  console.log(JSON.stringify(v, null, 2))
  cdp.close()
}

async function cmd_hover(selector: string, nthStr?: string) {
  const cdp = await connect()
  const nth = nthStr ? parseInt(nthStr, 10) : 1
  // Resolve target coords first, then use real CDP mouse-move so reka sees
  // a trusted pointerover (its tooltip primitive ignores JS-dispatched
  // events). Move away first to guarantee a fresh pointerenter on arrival.
  const rect = await evalJs(cdp, `
    (() => {
      const els = document.querySelectorAll(${JSON.stringify(selector)});
      const el = els[${nth - 1}];
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { x: Math.round(r.left + r.width/2), y: Math.round(r.top + r.height/2) };
    })()
  `)
  if (!rect) { console.log('not found'); cdp.close(); return }
  // Park cursor away from any UI so pointerleave fires on the prior target.
  await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: 1, y: 1 })
  await sleep(50)
  await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: rect.x, y: rect.y })
  console.log(`hovered at ${rect.x},${rect.y}`)
  cdp.close()
}

async function cmd_rclick(selector: string, nthStr?: string) {
  const cdp = await connect()
  const nth = nthStr ? parseInt(nthStr, 10) : 1
  // CDP's Input.dispatchMouseEvent doesn't synthesize a follow-up
  // `contextmenu` event in --headless=new, so reka's ContextMenuTrigger
  // never sees the right-click. Dispatch the event directly from JS —
  // reka doesn't require isTrusted for contextmenu (unlike click).
  const result = await evalJs(cdp, `
    (() => {
      const els = document.querySelectorAll(${JSON.stringify(selector)});
      const el = els[${nth - 1}];
      if (!el) return null;
      const r = el.getBoundingClientRect();
      const x = r.left + r.width/2, y = r.top + r.height/2;
      el.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true, cancelable: true, view: window,
        button: 2, buttons: 2, clientX: x, clientY: y,
      }));
      return { ok: true, x, y, tag: el.tagName };
    })()
  `)
  if (!result) { console.log('not found'); cdp.close(); return }
  await sleep(300)
  console.log(JSON.stringify(result, null, 2))
  cdp.close()
}

async function cmd_click(selector: string, nthStr?: string) {
  const cdp = await connect()
  // Locate the element and get its center point. nth (1-based) picks the
  // nth match of querySelectorAll when there are multiple — handy for option
  // lists where reka doesn't stamp the value as a queryable data attribute.
  const nth = nthStr ? parseInt(nthStr, 10) : 1
  const rect = await evalJs(cdp, `
    (() => {
      const els = document.querySelectorAll(${JSON.stringify(selector)});
      const el = els[${nth - 1}];
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { x: r.left + r.width/2, y: r.top + r.height/2, tag: el.tagName, classes: el.className };
    })()
  `)
  if (!rect) { console.log('not found'); cdp.close(); return }
  // Dispatch a real input-level mouse click via CDP. This walks the same code
  // path as a user click — reka's PointerEventsCheckLevel sees a genuine
  // trusted event, which the JS-dispatched PointerEvent doesn't satisfy.
  await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: rect.x, y: rect.y })
  await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', x: rect.x, y: rect.y, button: 'left', clickCount: 1, buttons: 1 })
  await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: rect.x, y: rect.y, button: 'left', clickCount: 1 })
  await sleep(300)
  console.log(JSON.stringify({ ok: true, ...rect }, null, 2))
  cdp.close()
}

async function cmd_dom(selector: string) {
  const cdp = await connect()
  const v = await evalJs(cdp, `
    (() => {
      const el = document.querySelector(${JSON.stringify(selector)});
      if (!el) return null;
      // Truncate huge subtrees so the log doesn't explode.
      const html = el.outerHTML;
      return html.length > 8000 ? html.slice(0, 8000) + '... [truncated]' : html;
    })()
  `)
  console.log(v ?? '(not found)')
  cdp.close()
}

async function cmd_style(selector: string, ...props: string[]) {
  const cdp = await connect()
  const v = await evalJs(cdp, `
    (() => {
      const el = document.querySelector(${JSON.stringify(selector)});
      if (!el) return null;
      const cs = getComputedStyle(el);
      const props = ${JSON.stringify(props)};
      const out = {};
      if (props.length === 0) {
        // No filter: dump a useful default set.
        for (const k of ['display','position','width','height','background','backgroundColor','backdropFilter','webkitBackdropFilter','filter','transform','opacity','zIndex','overflow','border','borderRadius','boxShadow','color','pointerEvents']) {
          out[k] = cs.getPropertyValue(k);
        }
      } else {
        for (const k of props) out[k] = cs.getPropertyValue(k);
      }
      return out;
    })()
  `)
  console.log(JSON.stringify(v, null, 2))
  cdp.close()
}

async function cmd_reload() {
  const cdp = await connect()
  await cdp.send('Page.reload', { ignoreCache: true })
  await waitForLoad(cdp, 6000)
  console.log('Reloaded.')
  cdp.close()
}

async function cmd_focus(selector: string) {
  const cdp = await connect()
  await evalJs(cdp, `document.querySelector(${JSON.stringify(selector)})?.focus()`)
  cdp.close()
}

async function cmd_wait(selector: string, timeoutMs = 8000) {
  const cdp = await connect()
  const negate = selector.startsWith('!')
  const sel = negate ? selector.slice(1) : selector
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    const found = await evalJs(cdp, `!!document.querySelector(${JSON.stringify(sel)})`)
    if (negate ? !found : found) {
      console.log(`${negate ? 'gone' : 'found'}: ${sel} (${Date.now() - start}ms)`)
      cdp.close()
      return
    }
    await sleep(150)
  }
  cdp.close()
  throw new Error(`timed out waiting for ${selector}`)
}

async function cmd_sleep(msStr: string) {
  await sleep(parseInt(msStr, 10))
}

async function cmd_console(urlOrAction = '/', durationStr = '4000') {
  const lines = await captureConsole(async (cdp) => {
    const s = requireState()
    const url = urlOrAction.startsWith('http') ? urlOrAction : `${s.origin}${urlOrAction.startsWith('/') ? urlOrAction : '/' + urlOrAction}`
    await cdp.send('Page.navigate', { url })
  }, parseInt(durationStr, 10))
  for (const l of lines) {
    console.log(`[${l.kind}] ${l.text}`)
  }
  console.log(`(${lines.length} entries)`)
}

// Sample the page's JS CPU for `durationMs` and print a self-time summary.
// Optionally writes the raw .cpuprofile (loadable in Chrome DevTools >
// Performance > Load profile) when an output path is given.
async function cmd_profile(durationStr = '10000', out?: string) {
  const s = requireState()
  const cdp = new CDP(s.wsUrl)
  await cdp.ready
  await cdp.send('Profiler.enable')
  await cdp.send('Profiler.setSamplingInterval', { interval: 500 })
  await cdp.send('Profiler.start')
  const ms = parseInt(durationStr, 10)
  await sleep(ms)
  const { profile } = await cdp.send('Profiler.stop')
  cdp.close()
  if (out) { writeFileSync(out, JSON.stringify(profile)); console.log(`Raw profile → ${out}`) }

  const samples: number[] = profile.samples ?? []
  const deltas: number[] = profile.timeDeltas ?? []
  const selfTime = new Map<number, number>() // node id → µs
  for (let i = 0; i < samples.length; i++) {
    selfTime.set(samples[i]!, (selfTime.get(samples[i]!) ?? 0) + (deltas[i] ?? 0))
  }
  const nodes = new Map<number, any>((profile.nodes as any[]).map(n => [n.id, n]))
  const byFrame = new Map<string, number>()
  const byScript = new Map<string, number>()
  let total = 0
  let idle = 0
  for (const [id, t] of selfTime) {
    total += t
    const n = nodes.get(id)
    if (!n) continue
    const f = n.callFrame
    const name = f.functionName || '(anonymous)'
    if (name === '(idle)') { idle += t; continue }
    const url = (f.url ?? '').replace(/^https?:\/\/[^/]+/, '')
    const frameKey = url ? `${name} — ${url}:${f.lineNumber + 1}` : name
    byFrame.set(frameKey, (byFrame.get(frameKey) ?? 0) + t)
    const scriptKey = url || `(${name})`
    byScript.set(scriptKey, (byScript.get(scriptKey) ?? 0) + t)
  }
  const busy = total - idle
  const fmt = (t: number) => `${(t / 1000).toFixed(1)}ms`
  const pct = (t: number) => `${((t / total) * 100).toFixed(1)}%`
  console.log(`\nSampled ${fmt(total)} wall, busy ${fmt(busy)} (${pct(busy)} of wall), idle ${pct(idle)}\n`)
  console.log('=== Self time by script ===')
  for (const [k, t] of [...byScript.entries()].sort((a, b) => b[1] - a[1]).slice(0, 15)) {
    console.log(`${fmt(t).padStart(10)}  ${pct(t).padStart(6)}  ${k}`)
  }
  console.log('\n=== Self time by function (top 30) ===')
  for (const [k, t] of [...byFrame.entries()].sort((a, b) => b[1] - a[1]).slice(0, 30)) {
    console.log(`${fmt(t).padStart(10)}  ${pct(t).padStart(6)}  ${k}`)
  }
}

// Navigate to a URL and log every network response (status + url) for
// `durationMs` — for finding which boot-time request fails.
async function cmd_netlog(urlOrPath = '/', durationStr = '6000') {
  const s = requireState()
  const cdp = new CDP(s.wsUrl)
  await cdp.ready
  await cdp.send('Network.enable')
  await cdp.send('Page.enable')
  const rows: string[] = []
  const reqHeaders = new Map<string, any>()
  cdp.on('Network.requestWillBeSent', (p: any) => {
    reqHeaders.set(p.requestId, p.request.headers)
  })
  cdp.on('Network.responseReceived', (p: any) => {
    const r = p.response
    const h = reqHeaders.get(p.requestId) ?? {}
    const auth = h.Authorization ?? h.authorization
    const authNote = auth ? ` auth=${String(auth).slice(0, 18)}…` : ' auth=NONE'
    rows.push(`${r.status}  ${r.url.slice(0, 120)}${r.url.includes('/api/') ? authNote : ''}`)
  })
  await cdp.send('Runtime.enable')
  cdp.on('Runtime.consoleAPICalled', (p: any) => {
    const text = (p.args ?? []).map((a: any) => a.value ?? a.description ?? '').join(' ')
    if (text.startsWith('[eye]')) rows.push(text)
  })
  const hookSource = `
      console.log('[eye] doc-start token?', !!localStorage.getItem('heya_token'), 'frame', location.href.slice(0, 60));
      const stack = () => new Error().stack.split('\\n').slice(2, 5).join(' | ');
      const origGet = Storage.prototype.getItem;
      Storage.prototype.getItem = function(k) {
        const v = origGet.call(this, k);
        if (k === 'heya_token') console.log('[eye] getItem(heya_token) ->', v ? 'present' : 'NULL', 'at', performance.now().toFixed(0) + 'ms', v ? '' : stack());
        return v;
      };
      const origRemove = Storage.prototype.removeItem;
      Storage.prototype.removeItem = function(k) {
        console.log('[eye] removeItem(' + k + ') at', performance.now().toFixed(0) + 'ms', stack());
        return origRemove.call(this, k);
      };
      const origClear = Storage.prototype.clear;
      Storage.prototype.clear = function() {
        console.log('[eye] CLEAR at', performance.now().toFixed(0) + 'ms', stack());
        return origClear.call(this);
      };
    `
  await cdp.send('Page.addScriptToEvaluateOnNewDocument', { source: hookSource })
  // Hook the CURRENT document too, so unload-time storage mutations from the
  // page we're navigating away from get logged as well.
  await cdp.send('Runtime.evaluate', { expression: hookSource.replace('doc-start', 'old-doc-hook') })
  const url = urlOrPath.startsWith('http') ? urlOrPath : `${s.origin}${urlOrPath.startsWith('/') ? urlOrPath : '/' + urlOrPath}`
  await cdp.send('Page.navigate', { url })
  await sleep(parseInt(durationStr, 10))
  cdp.close()
  for (const r of rows) console.log(r)
  console.log(`(${rows.length} responses)`)
}

// Count WebSocket frames received/sent by the page for `durationMs`; groups
// received JSON frames by their type/event field to expose chatty topics.
async function cmd_wsmon(durationStr = '10000') {
  const s = requireState()
  const cdp = new CDP(s.wsUrl)
  await cdp.ready
  await cdp.send('Network.enable')
  const ms = parseInt(durationStr, 10)
  let rx = 0, tx = 0, rxBytes = 0
  const byType = new Map<string, number>()
  const sample: string[] = []
  cdp.on('Network.webSocketFrameReceived', (p: any) => {
    rx++
    const payload: string = p.response?.payloadData ?? ''
    rxBytes += payload.length
    let key = '(non-json)'
    try {
      const m = JSON.parse(payload)
      key = m.type ?? m.event ?? m.kind ?? '(json, no type field)'
    } catch {}
    byType.set(key, (byType.get(key) ?? 0) + 1)
    if (sample.length < 5) sample.push(payload.slice(0, 300))
  })
  cdp.on('Network.webSocketFrameSent', () => { tx++ })
  await sleep(ms)
  cdp.close()
  const secs = ms / 1000
  console.log(`\n${secs}s window: ${rx} frames received (${(rx / secs).toFixed(2)}/s, ${rxBytes} bytes), ${tx} sent`)
  if (byType.size) {
    console.log('\n=== Received frames by type ===')
    for (const [k, n] of [...byType.entries()].sort((a, b) => b[1] - a[1])) {
      console.log(`${String(n).padStart(6)}  ${k}`)
    }
  }
  if (sample.length) {
    console.log('\n=== First payloads (truncated) ===')
    for (const p of sample) console.log(`  ${p}`)
  }
}

async function cmd_type(text: string) {
  const cdp = await connect()
  // Insert text directly into the focused element; works for input/textarea
  // and dispatches input events so Vue reactivity sees it.
  await evalJs(cdp, `
    (() => {
      const el = document.activeElement;
      if (!el || (el.tagName !== 'INPUT' && el.tagName !== 'TEXTAREA')) return false;
      const setter = Object.getOwnPropertyDescriptor(el.tagName === 'INPUT' ? HTMLInputElement.prototype : HTMLTextAreaElement.prototype, 'value').set;
      setter.call(el, ${JSON.stringify(text)});
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    })()
  `)
  cdp.close()
}

async function main() {
  const [sub, ...rest] = process.argv.slice(2)
  switch (sub) {
    case 'start':  await cmd_start(...rest); break
    case 'stop':   await cmd_stop(); break
    case 'viewport': await cmd_viewport(rest[0], ...rest.slice(1)); break
    case 'login':  await cmd_login(rest[0], rest[1]); break
    case 'goto':   await cmd_goto(rest[0] ?? '/'); break
    case 'shot':   await cmd_shot(rest[0], rest[1], rest[2]); break
    case 'eval':   await cmd_eval(rest.join(' ')); break
    case 'click':  await cmd_click(rest[0], rest[1]); break
    case 'rclick': await cmd_rclick(rest[0], rest[1]); break
    case 'hover':  await cmd_hover(rest[0], rest[1]); break
    case 'dom':    await cmd_dom(rest[0]); break
    case 'style':  await cmd_style(rest[0], ...rest.slice(1)); break
    case 'reload': await cmd_reload(); break
    case 'focus':  await cmd_focus(rest[0]); break
    case 'type':   await cmd_type(rest.join(' ')); break
    case 'wait':   await cmd_wait(rest[0], rest[1] ? parseInt(rest[1], 10) : undefined); break
    case 'sleep':   await cmd_sleep(rest[0]); break
    case 'console': await cmd_console(rest[0], rest[1]); break
    case 'profile': await cmd_profile(rest[0], rest[1]); break
    case 'wsmon':   await cmd_wsmon(rest[0]); break
    case 'netlog':  await cmd_netlog(rest[0], rest[1]); break
    default:
      console.error('usage: eye <start|stop|login|goto|shot|eval|click|dom|style|reload|focus|type|wait|sleep|viewport> [args]')
      process.exit(1)
  }
}

main().catch(err => { console.error(err); process.exit(1) })
