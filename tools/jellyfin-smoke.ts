#!/usr/bin/env bun
/**
 * Jellyfin-compat API smoke test.
 *
 * Walks the exact call sequence a stock Jellyfin client performs against a
 * Heya server with HEYA_JELLYFIN_API_ENABLED=true: discovery → login →
 * views → browse → item detail → PlaybackInfo → stream bytes → playstate →
 * resume verification → favorites → search → websocket keepalive.
 *
 * Zero dependencies — plain fetch + Bun's WebSocket. Usage:
 *
 *   bun tools/jellyfin-smoke.ts [baseUrl] [username] [password]
 *
 * Defaults: http://localhost:8080/jellyfin admin admin. Exits non-zero on the first
 * failed assertion, so it's CI-able against a seeded dev server.
 */

const base = (process.argv[2] ?? 'http://localhost:8080/jellyfin').replace(/\/$/, '')
const username = process.argv[3] ?? 'admin'
const password = process.argv[4] ?? 'admin'

let passed = 0
let failed = 0

function ok(cond: unknown, label: string, detail?: unknown) {
  if (cond) {
    passed++
    console.log(`  ✓ ${label}`)
  } else {
    failed++
    console.error(`  ✗ ${label}`, detail ?? '')
  }
}

function section(name: string) {
  console.log(`\n== ${name} ==`)
}

const authHeader =
  'MediaBrowser Client="HeyaSmoke", Device="bun", DeviceId="heya-smoke-1", Version="1.0.0"'

async function jf(path: string, init: RequestInit = {}, token?: string) {
  const headers = new Headers(init.headers)
  headers.set('X-Emby-Authorization', authHeader)
  if (token) headers.set('X-Emby-Token', token)
  if (init.body) headers.set('Content-Type', 'application/json')
  return fetch(base + path, { ...init, headers })
}

// --- discovery ---
section('discovery')
{
  const res = await jf('/System/Info/Public')
  ok(res.status === 200, 'GET /System/Info/Public → 200')
  const info = await res.json()
  ok(info.ProductName === 'Jellyfin Server', 'ProductName is "Jellyfin Server"', info)
  ok(/^10\.11\./.test(info.Version), `advertises 10.11.x (got ${info.Version})`)
  ok(/^[0-9a-f]{32}$/.test(info.Id), 'server Id is a 32-hex GUID')

  const lower = await jf('/system/info/public')
  ok(lower.status === 200, 'case-insensitive routing (/system/info/public)')
  const emby = await jf('/emby/System/Info/Public')
  ok(emby.status === 200, '/emby prefix alias')
  const qc = await (await jf('/QuickConnect/Enabled')).json()
  ok(qc === false, 'QuickConnect disabled')
}

// --- login ---
section('login')
let token = ''
let userId = ''
{
  const res = await jf('/Users/AuthenticateByName', {
    method: 'POST',
    body: JSON.stringify({ Username: username, Pw: password }),
  })
  ok(res.status === 200, 'POST /Users/AuthenticateByName → 200')
  const auth = await res.json()
  token = auth.AccessToken
  userId = auth.User?.Id
  ok(typeof token === 'string' && token.length >= 32, 'AccessToken issued')
  ok(auth.SessionInfo?.DeviceId === 'heya-smoke-1', 'DeviceId echoed in SessionInfo')
  ok(auth.User?.Policy?.EnableMediaPlayback === true, 'policy allows playback')

  const bad = await jf('/Users/AuthenticateByName', {
    method: 'POST',
    body: JSON.stringify({ Username: username, Pw: 'definitely-wrong' }),
  })
  ok(bad.status === 401, 'bad password → 401')
  const unauth = await jf('/UserViews')
  ok(unauth.status === 401, 'authenticated route without token → 401')

  const me = await (await jf('/Users/Me', {}, token)).json()
  ok(me.Id === userId, '/Users/Me id matches login')
}

// --- browse ---
section('browse')
let firstPlayable: string | undefined
{
  const views = await (await jf('/UserViews', {}, token)).json()
  ok(Array.isArray(views.Items), '/UserViews returns Items')
  console.log(`    views: ${views.Items.map((v: any) => `${v.Name} (${v.CollectionType})`).join(', ') || '(none)'}`)

  for (const view of views.Items) {
    const typeByCollection: Record<string, string> = {
      movies: 'Movie', tvshows: 'Series', music: 'MusicArtist', books: 'Book',
    }
    const t = typeByCollection[view.CollectionType]
    if (!t) continue
    const items = await (
      await jf(`/Items?parentId=${view.Id}&includeItemTypes=${t}&recursive=true&sortBy=SortName&limit=3`, {}, token)
    ).json()
    ok(typeof items.TotalRecordCount === 'number', `browse ${view.Name}: TotalRecordCount=${items.TotalRecordCount}`)
    const first = items.Items?.[0]
    if (!first) continue
    ok(first.ServerId && first.ImageTags, `${first.Type} dto has ServerId + ImageTags`)

    const detail = await (await jf(`/Items/${first.Id}`, {}, token)).json()
    ok(detail.Id === first.Id, `item detail round-trips (${detail.Name})`)

    if (first.Type === 'Series') {
      const seasons = await (await jf(`/Shows/${first.Id}/Seasons`, {}, token)).json()
      ok(seasons.Items?.length >= 0, `seasons list (${seasons.Items?.length ?? 0})`)
      const season = seasons.Items?.[0]
      if (season) {
        const eps = await (
          await jf(`/Shows/${first.Id}/Episodes?seasonId=${season.Id}&limit=2`, {}, token)
        ).json()
        ok(eps.Items?.length > 0, `episodes list (${eps.TotalRecordCount} total)`)
        const ep = eps.Items?.[0]
        if (ep) {
          ok(ep.SeriesId === first.Id && ep.IndexNumber >= 0, 'episode carries SeriesId + IndexNumber')
          firstPlayable ??= ep.Id
        }
      }
    }
    if (first.Type === 'Movie') firstPlayable ??= first.Id

    const latest = await (await jf(`/Items/Latest?parentId=${view.Id}&limit=2`, {}, token)).json()
    ok(Array.isArray(latest), '/Items/Latest returns a bare array')
  }

  const resume = await (await jf('/UserItems/Resume', {}, token)).json()
  ok(Array.isArray(resume.Items), '/UserItems/Resume shape')
  const nextUp = await (await jf('/Shows/NextUp', {}, token)).json()
  ok(Array.isArray(nextUp.Items), '/Shows/NextUp shape')
  const filters = await (await jf('/Items/Filters2', {}, token)).json()
  ok(Array.isArray(filters.Genres), '/Items/Filters2 genres')
}

// --- playback ---
section('playback')
if (firstPlayable) {
  const res = await jf(`/Items/${firstPlayable}/PlaybackInfo`, {
    method: 'POST',
    body: JSON.stringify({
      DeviceProfile: {
        DirectPlayProfiles: [
          { Container: 'mp4,m4v,mkv', VideoCodec: 'h264,hevc', AudioCodec: 'aac,ac3,eac3,flac', Type: 'Video' },
        ],
        TranscodingProfiles: [
          { Container: 'ts', Type: 'Video', VideoCodec: 'h264', AudioCodec: 'aac', Protocol: 'hls' },
        ],
      },
    }),
  }, token)
  ok(res.status === 200, 'POST PlaybackInfo → 200')
  const pi = await res.json()
  const src = pi.MediaSources?.[0]
  ok(!!pi.PlaySessionId, 'PlaySessionId issued')
  ok(!!src, 'MediaSource present', pi)
  if (src) {
    ok(Array.isArray(src.MediaStreams), `MediaStreams (${src.MediaStreams.length})`)
    ok(src.SupportsDirectPlay || src.TranscodingUrl, 'either direct play or a TranscodingUrl')

    if (src.SupportsDirectPlay) {
      const head = await jf(`/Videos/${firstPlayable}/stream?static=true&mediaSourceId=${src.Id}`, { method: 'HEAD' }, token)
      ok(head.status === 200, 'HEAD /Videos/{id}/stream → 200')
      const range = await jf(`/Videos/${firstPlayable}/stream?static=true`, { headers: { Range: 'bytes=0-1023' } }, token)
      ok(range.status === 206, 'Range request → 206')
    }
    if (src.TranscodingUrl) {
      const master = await fetch(base + src.TranscodingUrl)
      ok(master.status === 200, 'TranscodingUrl master playlist → 200')
      ok((await master.text()).includes('#EXTM3U'), 'master playlist is HLS')
    }
  }

  // playstate: start → progress → verify resume → stop
  const ps = pi.PlaySessionId
  const start = await jf('/Sessions/Playing', {
    method: 'POST',
    body: JSON.stringify({ ItemId: firstPlayable, PositionTicks: 0, PlaySessionId: ps }),
  }, token)
  ok(start.status === 204, 'playback start reported')
  const prog = await jf('/Sessions/Playing/Progress', {
    method: 'POST',
    body: JSON.stringify({ ItemId: firstPlayable, PositionTicks: 6_000_000_000, PlaySessionId: ps, IsPaused: false }),
  }, token)
  ok(prog.status === 204, 'progress reported (10min)')
  const item = await (await jf(`/Items/${firstPlayable}`, {}, token)).json()
  ok(item.UserData?.PlaybackPositionTicks === 6_000_000_000, 'resume position round-trips', item.UserData)
  const stop = await jf('/Sessions/Playing/Stopped', {
    method: 'POST',
    body: JSON.stringify({ ItemId: firstPlayable, PositionTicks: 6_000_000_000, PlaySessionId: ps }),
  }, token)
  ok(stop.status === 204, 'playback stop reported')
} else {
  console.log('  (no playable item in any library — skipping playback flow)')
}

// --- userdata ---
section('userdata')
if (firstPlayable) {
  const fav = await (await jf(`/UserFavoriteItems/${firstPlayable}`, { method: 'POST' }, token)).json()
  ok(fav.IsFavorite === true, 'favorite set')
  const item = await (await jf(`/Items/${firstPlayable}`, {}, token)).json()
  ok(item.UserData?.IsFavorite === true, 'favorite visible on item')
  const unfav = await (await jf(`/UserFavoriteItems/${firstPlayable}`, { method: 'DELETE' }, token)).json()
  ok(unfav.IsFavorite === false, 'favorite cleared')
}

// --- search ---
section('search')
{
  const hints = await (await jf('/Search/Hints?searchTerm=a&limit=5', {}, token)).json()
  ok(Array.isArray(hints.SearchHints), `/Search/Hints (${hints.TotalRecordCount} hits)`)
}

// --- websocket ---
section('websocket')
await new Promise<void>((resolve) => {
  const wsUrl = base.replace(/^http/, 'ws') + `/socket?api_key=${token}&deviceId=heya-smoke-1`
  const ws = new WebSocket(wsUrl)
  const timer = setTimeout(() => {
    ok(false, 'websocket: timed out waiting for ForceKeepAlive')
    ws.close()
    resolve()
  }, 5000)
  ws.onmessage = (ev) => {
    const msg = JSON.parse(String(ev.data))
    if (msg.MessageType === 'ForceKeepAlive') {
      ok(true, 'ForceKeepAlive received')
      ws.send(JSON.stringify({ MessageType: 'KeepAlive' }))
    } else if (msg.MessageType === 'KeepAlive') {
      ok(true, 'KeepAlive acked')
      clearTimeout(timer)
      ws.close()
      resolve()
    }
  }
  ws.onerror = () => {
    ok(false, 'websocket error')
    clearTimeout(timer)
    resolve()
  }
})

// --- logout ---
section('logout')
{
  const res = await jf('/Sessions/Logout', { method: 'POST' }, token)
  ok(res.status === 204, 'POST /Sessions/Logout → 204')
  const after = await jf('/Users/Me', {}, token)
  ok(after.status === 401, 'token revoked after logout')
}

console.log(`\n${passed} passed, ${failed} failed`)
process.exit(failed === 0 ? 0 : 1)
