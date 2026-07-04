#!/usr/bin/env bun
// Differential test: real Jellyfin vs Heya's compat surface.
// For each endpoint both servers answer, compare the JSON *structurally*:
// which keys exist and what JSON type they hold. Decoders break on missing
// keys and type mismatches, not on differing values.

const REAL = 'http://127.0.0.1:8097'
const HEYA = 'http://127.0.0.1:8099'
const AUTH = 'MediaBrowser Client="DiffProbe", Device="bun", DeviceId="diff-1", Version="1.0.0"'

type Ctx = { base: string; token: string; userId: string }

async function login(base: string): Promise<Ctx> {
  const res = await fetch(base + '/Users/AuthenticateByName', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Emby-Authorization': AUTH },
    body: JSON.stringify({ Username: 'admin', Pw: 'admin' }),
  })
  const j = await res.json()
  return { base, token: j.AccessToken, userId: j.User.Id }
}

async function get(ctx: Ctx, path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers)
  headers.set('Authorization', AUTH.replace(/"$/, `", Token="${ctx.token}"`).replace('MediaBrowser ', 'MediaBrowser ')  )
  headers.set('X-Emby-Authorization', AUTH + `, Token="${ctx.token}"`)
  if (init.body) headers.set('Content-Type', 'application/json')
  const res = await fetch(ctx.base + path, { ...init, headers })
  const text = await res.text()
  try { return { status: res.status, json: JSON.parse(text) } }
  catch { return { status: res.status, json: undefined } }
}

// Collect JSON paths → set of types seen. Arrays are traversed per-element
// under the same path so heterogeneous items merge.
function typemap(v: any, path = '$', out = new Map<string, Set<string>>()) {
  const t = v === null ? 'null' : Array.isArray(v) ? 'array' : typeof v
  if (!out.has(path)) out.set(path, new Set())
  out.get(path)!.add(t)
  if (Array.isArray(v)) for (const el of v) typemap(el, path + '[]', out)
  else if (t === 'object') for (const [k, val] of Object.entries(v)) typemap(val, path + '.' + k, out)
  return out
}

function diff(label: string, real: any, mine: any) {
  const rm = typemap(real)
  const mm = typemap(mine)
  const missing: string[] = []
  const typeMismatch: string[] = []
  for (const [path, rTypes] of rm) {
    const mTypes = mm.get(path)
    if (!mTypes) { missing.push(`${path} (${[...rTypes].join('|')})`); continue }
    // null on the real side means the field is nullable — anything goes.
    const rSolid = [...rTypes].filter(t => t !== 'null')
    if (rSolid.length && ![...mTypes].some(t => rSolid.includes(t) || t === 'null')) {
      typeMismatch.push(`${path}: real=${[...rTypes].join('|')} heya=${[...mTypes].join('|')}`)
    }
  }
  const extra: string[] = []
  for (const path of mm.keys()) if (!rm.has(path)) extra.push(path)

  console.log(`\n== ${label} ==`)
  if (!missing.length && !typeMismatch.length) console.log('  structurally compatible ✓')
  if (missing.length) {
    console.log(`  MISSING in Heya (${missing.length}):`)
    for (const m of missing.slice(0, 60)) console.log(`    - ${m}`)
  }
  if (typeMismatch.length) {
    console.log(`  TYPE MISMATCH (${typeMismatch.length}):`)
    for (const m of typeMismatch) console.log(`    ! ${m}`)
  }
  if (extra.length) console.log(`  (heya-only keys: ${extra.length} — harmless)`)
}

// One login per server — Jellyfin keeps a single session per DeviceId, so a
// second AuthenticateByName with the same DeviceId invalidates the first
// token (Heya is lenient here; noted as a semantic difference).
async function loginRaw(base: string): Promise<{ ctx: Ctx; envelope: any }> {
  const res = await fetch(base + '/Users/AuthenticateByName', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Emby-Authorization': AUTH },
    body: JSON.stringify({ Username: 'admin', Pw: 'admin' }),
  })
  const j = await res.json()
  return { ctx: { base, token: j.AccessToken, userId: j.User.Id }, envelope: j }
}

const { ctx: real, envelope: rAuth } = await loginRaw(REAL)
const { ctx: heya, envelope: hAuth } = await loginRaw(HEYA)
diff('POST /Users/AuthenticateByName', rAuth, hAuth)

for (const path of ['/System/Info/Public', '/UserViews', '/UserViews/GroupingOptions', '/Users/Me']) {
  const [r, h] = await Promise.all([get(real, path), get(heya, path)])
  diff(`GET ${path} (real=${r.status} heya=${h.status})`, r.json, h.json)
}

// Library-scoped browse: first movie view on each side.
const rViews = (await get(real, '/UserViews')).json
const hViews = (await get(heya, '/UserViews')).json
const rView = rViews.Items.find((v: any) => v.CollectionType === 'movies')
const hView = hViews.Items.find((v: any) => v.CollectionType === 'movies')

const itemsQS = (id: string) => `/Items?parentId=${id}&includeItemTypes=Movie&recursive=true&sortBy=SortName&limit=5&fields=PrimaryImageAspectRatio,MediaSourceCount,Overview`
{
  const [r, h] = await Promise.all([get(real, itemsQS(rView.Id)), get(heya, itemsQS(hView.Id))])
  diff('GET /Items (movie grid)', r.json, h.json)

  const rMovie = r.json.Items[0]
  const hMovie = h.json.Items[0]
  const [rd, hd] = await Promise.all([get(real, `/Items/${rMovie.Id}`), get(heya, `/Items/${hMovie.Id}`)])
  diff('GET /Items/{id} (movie detail)', rd.json, hd.json)

  const profile = JSON.stringify({ DeviceProfile: { DirectPlayProfiles: [{ Container: 'mp4,mkv', VideoCodec: 'h264,hevc', AudioCodec: 'aac,flac', Type: 'Video' }] } })
  const [rp, hp] = await Promise.all([
    get(real, `/Items/${rMovie.Id}/PlaybackInfo`, { method: 'POST', body: profile }),
    get(heya, `/Items/${hMovie.Id}/PlaybackInfo`, { method: 'POST', body: profile }),
  ])
  diff('POST /Items/{id}/PlaybackInfo', rp.json, hp.json)

  const [rl, hl] = await Promise.all([
    get(real, `/Items/Latest?parentId=${rView.Id}&limit=3`),
    get(heya, `/Items/Latest?parentId=${hView.Id}&limit=3`),
  ])
  diff('GET /Items/Latest', rl.json, hl.json)
}

const [rr, hr] = await Promise.all([get(real, '/UserItems/Resume'), get(heya, '/UserItems/Resume')])
diff('GET /UserItems/Resume', rr.json, hr.json)
