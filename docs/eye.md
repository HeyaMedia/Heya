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
`:3000`. You hit `:8080` for everything; the front door routes `/api/*` to the
backend and the SPA to Nuxt, so the login command and the page both work
against `:8080` directly.

## Commands

| Cmd | Purpose |
| --- | --- |
| `start` | Spawn Chrome with remote debugging on `:9223`, user-data-dir `/tmp/heya-eye/profile/` |
| `stop` | Kill it and clear state |
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
