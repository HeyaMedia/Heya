# Heya Eye — browser debugging pipeline

When a UI bug needs *eyes* — popovers that don't open, glassy elements that
look wrong, contrast on a specific page — drive headless Chrome via
`tools/eye/eye.ts` instead of guessing from code review. Type-check passing
doesn't prove runtime correctness; this tool does.

It's a single bun script that spawns Chrome with `--remote-debugging-port`,
drives it over the DevTools Protocol, and persists the WebSocket URL between
subprocess calls in `/tmp/heya-eye/state.json`. Every subcommand connects
fresh, runs one operation, exits — so you can shell-pipeline a debugging
session.

## One-time setup per session

```bash
bun tools/eye/eye.ts start          # spawn headless Chrome (pid stashed)
bun tools/eye/eye.ts login          # POST /api/auth/login, stash token in localStorage
bun tools/eye/eye.ts goto /         # navigate (default origin http://localhost:8080)
```

The full dev stack must be running — `make dev` (mprocs) brings it up: the
`heya dev-proxy` front door on `:8080`, the backend on `:3050`, and Nuxt on
`:3000`. You hit `:8080` for everything; the front door routes `/api/*`,
Jellyfin protocol requests, and `/rest/*` to the backend and Heya pages to
Nuxt, so the login command and the page both work against `:8080` directly.

**Concurrent instances**: `HEYA_EYE_PORT=9224 bun tools/eye/eye.ts …` gives
this shell its own Chrome, state file, and profile (`/tmp/heya-eye-9224/`) —
required whenever more than one agent/session drives Eye at the same time;
two instances on one port silently fight over the same browser. Set the env
var on EVERY eye invocation, not just `start`.

## Commands

| Cmd | Purpose |
| --- | --- |
| `start [--window-size WxH]` | Spawn Chrome with remote debugging on `:9223`, user-data-dir `/tmp/heya-eye/profile/`. Window size defaults to `1600x1000` |
| `stop` | Kill it and clear state |
| `viewport <WxH> [--dpr N] [--touch]` | Persist a mobile-viewport override (device metrics emulation) applied on every subsequent command. `viewport off` clears it — prints the active state either way |
| `login [user pw]` | API-login + stash token. Defaults `admin/admin` |
| `goto <path-or-url>` | Navigate; waits for `Page.loadEventFired` + 800 ms settle |
| `reload` | Hard reload (ignores cache) |
| `wait <sel> [ms]` | Poll until selector appears. Prefix with `!` to wait until it vanishes |
| `sleep <ms>` | Block — for SPA hydration / debounce / animation settling |
| `click <sel>` | Real CDP `Input.dispatchMouseEvent` — **trusted** event, so reka-ui handlers fire. JS-synthesized clicks don't trigger reka |
| `focus <sel>` | `.focus()` an input without clicking |
| `type <text>` | Native-setter `value =` + dispatch `input` so Vue v-model sees it |
| `eval <js>` | `Runtime.evaluate`; returns JSON of the expression result |
| `dom <sel>` | Print outerHTML (truncated to 8 KiB) |
| `style <sel> [props…]` | computed-style key/value dump. **Use kebab-case** (`backdrop-filter`, `border-radius`) — `getPropertyValue` won't translate camelCase |
| `shot <out.png> [sel] [pad]` | Screenshot. Pass a selector to clip; default 16 px padding |
| `profile <ms> [out.cpuprofile]` | Sample the page's CPU for `ms` (CDP `Profiler`, 500 µs interval); prints busy %, self-time by script and by function. Optional raw `.cpuprofile` loads in DevTools → Performance |
| `wsmon <ms>` | Count WebSocket frames for `ms`; groups received JSON frames by `type` — measures event-bus chattiness |
| `netlog <url> <ms>` | Navigate and log every response status + whether `/api/` requests carried an `Authorization` header; also instruments `localStorage` get/remove/clear with stacks (finds boot-time auth races) |

## Mobile viewport emulation

`eye viewport` persists a CDP device-metrics override (`{width, height, dpr,
mobile: false, touch}`) into the same `state.json` that already tracks
`origin` — every subcommand re-connects fresh and re-applies it before doing
its own work, so `goto`, `shot`, `click`, `eval`, etc. all see the emulated
size without any extra flags. It stacks with `--touch` for
`Emulation.setTouchEmulationEnabled`, which is what makes
`navigator.maxTouchPoints`/`matchMedia('(pointer: coarse)')` report as a
touch device — the app's `useViewport()`/`isCoarse` composable keys off that.

`mobile` is deliberately **false**: with `mobile: true`, any page whose
content min-width exceeds the requested width makes Chrome's mobile
emulation zoom out to fit — the layout viewport silently becomes the content
width, media queries evaluate in the wrong band, and the screenshot shows a
"working" desktop layout at a width that is actually broken (this burned a
tablet-band survey once: 744/821 shots came back rendered at ~1151px).
`mobile: false` pins the layout viewport at exactly the requested size and
lets overflow show as overflow. Sanity-check any new size with
`eye eval 'window.innerWidth'` — it must echo the width you asked for.

Mobile testing recipe:

```bash
bun tools/eye/eye.ts viewport 390x844 --dpr 3 --touch   # iPhone-ish phone
bun tools/eye/eye.ts goto /music
bun tools/eye/eye.ts shot /tmp/heya-eye/phone.png
```

`eye viewport off` returns to the desktop default — every following command
goes back to the `start`-time window size (`1600x1000` unless overridden via
`eye start --window-size`). Other useful sizes: `820x1180` for the tablet
breakpoint, or drop back to `1600x1000` explicitly for a desktop-unchanged
spot check.

Headless Chrome has no separate "real OS window" behind the emulated
surface — `Emulation.setDeviceMetricsOverride` actually resizes the render
surface itself, and it stays resized across separate debugger sessions
attached to the same target. Because of that, `eye viewport off` doesn't rely
on `Emulation.clearDeviceMetricsOverride` (verified empirically to *not*
restore the original size in headless mode) — it re-asserts the desktop
dimensions explicitly instead.

`shot` already captures at the emulated size correctly: `captureBeyondViewport:
true` (used for every `shot`) sizes the PNG in physical pixels as `CSS width ×
dpr`, and extends past the viewport height to cover the full scrollable page
— a 390×844 @3x override with 2000px of scrollable content yields an
1170×6000 PNG, not a viewport-height-clipped one.

## Performance gathering

`profile` + `wsmon` + `eval` cover most "why is the CPU busy" questions:

- **Attribute compositor load**: run `profile`, note `(program)` %. Then
  `eval` a `<style>*{animation:none!important}</style>` injection and re-run —
  the delta is what infinite CSS animations cost. `document.getAnimations()`
  censuses what's running (2026-07: 32 stuck `heya-loading-image` spinners
  after scrolling /music was worth ~12% of a core).
- **rAF/timer wake-ups**: wrap `requestAnimationFrame`/`setTimeout` counters
  in an `eval` for 5 s to see scheduling rates.
- **WS pressure**: `wsmon 20000` during server activity; pair with a
  `performance.getEntriesByType('resource')` diff to count triggered refetches.
- **Against prod**: Eye can target `https://heya.drum-ray.ts.net` directly —
  log in through the real form (`focus`/`type`/`click`). Injecting a token
  into localStorage on the login page does NOT survive: the unauthenticated
  app's background heartbeat 401s and the `$heya` interceptor's `logout()`
  wipes the injected token before navigation.

## Patterns that come up

- **Driving reka popovers**: use `click <selector>` — JS-dispatched events fail
  because reka v2 checks `event.isTrusted`. Real CDP input is the only way.
- **Verifying a dropdown opened**:
  `eval "document.querySelector('.foo-btn').getAttribute('data-state')"` →
  `"open"` / `"closed"`. Reka stamps this on the trigger button.
- **Why does my CSS look wrong**: combine `dom <sel>` (does the class even
  land?), `style <sel> background backdrop-filter border-radius` (does the
  rule apply?), and clipped `shot` (what does it actually look like?). This
  three-step diagnosis caught the search-dropdown `.topbar` backdrop-filter
  compositing issue that pure code review missed.
- **Reading screenshots**: use the Read tool — PNGs render inline so you can
  see what the page looks like without bouncing through external viewers.
- **Tracking stacking-context bugs**: walk the parent chain in one eval —
  recipe in [`ui.md`](./ui.md#stacking-context-audit-one-liner).

## Don't trust agent summaries

Run a screenshot at the end and *look at it*. Tool output that says "found",
"open", or "200 OK" doesn't mean the thing visually rendered correctly.
