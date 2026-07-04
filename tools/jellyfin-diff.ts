#!/usr/bin/env bun
// Differential test: real Jellyfin vs Heya's compat surface.
// For each endpoint both servers answer, compare the JSON *structurally*:
// which keys exist and what JSON type they hold. Decoders break on missing
// keys and type mismatches, not on differing values.
//
// Config via env (never hardcode credentials here):
//   JF_REAL_URL / JF_REAL_USER / JF_REAL_PASS   — reference Jellyfin server
//   JF_HEYA_URL / JF_HEYA_USER / JF_HEYA_PASS   — Heya under test
// Defaults: Heya at http://127.0.0.1:8080 with admin/admin.

const REAL = process.env.JF_REAL_URL ?? 'http://127.0.0.1:8097'
const HEYA = process.env.JF_HEYA_URL ?? 'http://127.0.0.1:8080'
const AUTH = 'MediaBrowser Client="DiffProbe", Device="bun", DeviceId="diff-1", Version="1.0.0"'

type Ctx = { base: string; token: string; userId: string }

function creds(which: 'REAL' | 'HEYA') {
  return {
    user: process.env[`JF_${which}_USER`] ?? 'admin',
    pass: process.env[`JF_${which}_PASS`] ?? 'admin',
  }
}

// Jellyfin 12 rejects X-Emby-Authorization (400); the modern Authorization:
// MediaBrowser form works on 10.x and 12.x both, and on Heya.
async function loginRaw(base: string, which: 'REAL' | 'HEYA'): Promise<{ ctx: Ctx; envelope: any }> {
  const c = creds(which)
  const res = await fetch(base + '/Users/AuthenticateByName', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: AUTH },
    body: JSON.stringify({ Username: c.user, Pw: c.pass }),
  })
  if (!res.ok) throw new Error(`login ${base} failed: ${res.status} ${await res.text()}`)
  const j = await res.json()
  return { ctx: { base, token: j.AccessToken, userId: j.User.Id }, envelope: j }
}

async function get(ctx: Ctx, path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers)
  headers.set('Authorization', AUTH + `, Token="${ctx.token}"`)
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
    for (const m of missing.slice(0, 80)) console.log(`    - ${m}`)
  }
  if (typeMismatch.length) {
    console.log(`  TYPE MISMATCH (${typeMismatch.length}):`)
    for (const m of typeMismatch) console.log(`    ! ${m}`)
  }
  if (extra.length) console.log(`  (heya-only keys: ${extra.length} — harmless)`)
}

const { ctx: real, envelope: rAuth } = await loginRaw(REAL, 'REAL')
const { ctx: heya, envelope: hAuth } = await loginRaw(HEYA, 'HEYA')
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
  diff('GET /Items/Latest (movies)', rl.json, hl.json)
}

// TV show flow — the exact request sequence Infuse fires when opening a show:
// series detail → Seasons → Episodes → episode detail. Plus NextUp + Latest.
{
  const rTv = rViews.Items.find((v: any) => v.CollectionType === 'tvshows')
  const hTv = hViews.Items.find((v: any) => v.CollectionType === 'tvshows')
  if (rTv && hTv) {
    const seriesQS = (id: string) => `/Items?parentId=${id}&includeItemTypes=Series&recursive=true&sortBy=SortName&limit=5&fields=PrimaryImageAspectRatio,Overview`
    const [rs, hs] = await Promise.all([get(real, seriesQS(rTv.Id)), get(heya, seriesQS(hTv.Id))])
    diff('GET /Items (series grid)', rs.json, hs.json)

    const rSeries = rs.json.Items[0]
    const hSeries = hs.json.Items[0]
    const [rd, hd] = await Promise.all([get(real, `/Items/${rSeries.Id}`), get(heya, `/Items/${hSeries.Id}`)])
    diff('GET /Items/{id} (series detail)', rd.json, hd.json)

    const [rse, hse] = await Promise.all([
      get(real, `/Shows/${rSeries.Id}/Seasons?fields=PrimaryImageAspectRatio,Overview`),
      get(heya, `/Shows/${hSeries.Id}/Seasons?fields=PrimaryImageAspectRatio,Overview`),
    ])
    diff('GET /Shows/{id}/Seasons', rse.json, hse.json)

    const [rep, hep] = await Promise.all([
      get(real, `/Shows/${rSeries.Id}/Episodes?fields=PrimaryImageAspectRatio,Overview,MediaSources`),
      get(heya, `/Shows/${hSeries.Id}/Episodes?fields=PrimaryImageAspectRatio,Overview,MediaSources`),
    ])
    diff('GET /Shows/{id}/Episodes', rep.json, hep.json)

    const rEp = rep.json?.Items?.[0]
    const hEp = hep.json?.Items?.[0]
    if (rEp && hEp) {
      const [red, hed] = await Promise.all([get(real, `/Items/${rEp.Id}`), get(heya, `/Items/${hEp.Id}`)])
      diff('GET /Items/{id} (episode detail)', red.json, hed.json)
    }

    const [rnu, hnu] = await Promise.all([
      get(real, `/Shows/NextUp?fields=PrimaryImageAspectRatio,Overview&limit=10`),
      get(heya, `/Shows/NextUp?fields=PrimaryImageAspectRatio,Overview&limit=10`),
    ])
    diff('GET /Shows/NextUp', rnu.json, hnu.json)

    const [rl, hl] = await Promise.all([
      get(real, `/Items/Latest?parentId=${rTv.Id}&limit=3`),
      get(heya, `/Items/Latest?parentId=${hTv.Id}&limit=3`),
    ])
    diff('GET /Items/Latest (tv)', rl.json, hl.json)
  } else {
    console.log('\n(skipping TV flow — one side has no tvshows view)')
  }
}

const [rr, hr] = await Promise.all([get(real, '/UserItems/Resume'), get(heya, '/UserItems/Resume')])
diff('GET /UserItems/Resume', rr.json, hr.json)
