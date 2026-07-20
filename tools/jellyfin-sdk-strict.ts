#!/usr/bin/env bun
// Replays the request sequence the Wholphin Android TV client makes at login +
// home load against a Heya server, and validates every response body against
// the Jellyfin 10.11 OpenAPI spec with jellyfin-sdk-kotlin's strictness:
//
//   - a property the spec does NOT mark `nullable: true` that is missing or
//     null  → MissingFieldException in kotlinx  → app crash        [HARD]
//   - an enum-typed property whose value is not in the enum list   [HARD]
//   - a JSON type mismatch (string where number, etc.)             [HARD]
//   - unknown keys in our response (SDK ignores; possible typo)    [note]
//   - date-time strings that don't look ISO-8601                   [note]
//
// Usage: bun tools/jellyfin-sdk-strict.ts [base] [token]
//        (token defaults to the heya CLI token cache; base to :8080)

import { readFileSync } from 'fs'
import { gunzipSync } from 'bun'
import { homedir } from 'os'
import { join, dirname } from 'path'

const BASE = (process.argv[2] ?? process.env.JF_URL ?? 'http://127.0.0.1:8080').replace(/\/$/, '')
const TOKEN = process.argv[3] ?? process.env.JF_TOKEN ?? (() => {
  try {
    return readFileSync(join(homedir(), 'Library/Application Support/heya/cli-token'), 'utf8').trim()
  } catch {
    try {
      return readFileSync(join(process.env.XDG_CONFIG_HOME ?? join(homedir(), '.config'), 'heya/cli-token'), 'utf8').trim()
    } catch { return '' }
  }
})()
const SPEC_PATH = join(dirname(new URL(import.meta.url).pathname), '../internal/jellyfin/spec/jellyfin-openapi-10.11.11.json.gz')

const spec = JSON.parse(new TextDecoder().decode(gunzipSync(readFileSync(SPEC_PATH))))
const schemas: Record<string, any> = spec.components.schemas

type Violation = { level: 'HARD' | 'note'; path: string; msg: string }

function resolveRef(s: any): any {
  while (s && s.$ref) {
    const name = s.$ref.replace('#/components/schemas/', '')
    const next = schemas[name]
    if (!next) return s
    s = next
  }
  return s
}

// The Jellyfin spec wraps nullable enum refs as { allOf: [$ref], nullable: true }.
function normalize(s: any): { schema: any; nullable: boolean } {
  let nullable = !!s.nullable
  if (s.allOf && s.allOf.length === 1 && !s.properties && !s.type) {
    const inner = resolveRef(s.allOf[0])
    return { schema: inner, nullable }
  }
  if (s.$ref) {
    const inner = resolveRef(s)
    // NOTE: a bare $ref carries no nullable flag; jellyfin's generator treats
    // bare-$ref object properties as non-nullable only when listed in
    // `required`; kotlin generator makes them nullable unless required.
    return { schema: inner, nullable }
  }
  return { schema: s, nullable }
}

function typeOk(schema: any, v: any): boolean {
  switch (schema.type) {
    case 'string': return typeof v === 'string'
    case 'boolean': return typeof v === 'boolean'
    case 'integer': return typeof v === 'number' && Number.isInteger(v)
    case 'number': return typeof v === 'number'
    case 'array': return Array.isArray(v)
    case 'object': return typeof v === 'object' && v !== null && !Array.isArray(v)
    default: return true
  }
}

const ISO_DT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?$/

// kotlinx (SDK config: ignoreUnknownKeys, explicitNulls=false, coerceInputValues=true):
//  - missing/null property  → crash iff NOT nullable AND no spec default
//  - unknown enum value     → coerced to default (or null if nullable); crash iff
//                             NOT nullable AND no default
function validate(value: any, schemaIn: any, path: string, out: Violation[], depth = 0, propCtx?: { nullable: boolean; hasDefault: boolean }) {
  if (depth > 40) return
  const { schema } = normalize(schemaIn)
  if (value === null || value === undefined) return // nullability judged at the property site

  // The generator collapses ImageType-keyed dictionaries (ImageBlurHashes)
  // back into nullable Map<ImageType, …> — spec object expansion is a mirage.
  if (path.endsWith('.ImageBlurHashes')) return

  if (schema.enum) {
    if (!schema.enum.includes(value)) {
      const rescued = propCtx && (propCtx.nullable || propCtx.hasDefault)
      out.push({
        level: rescued ? 'note' : 'HARD',
        path,
        msg: `enum value ${JSON.stringify(value)} not in [${schema.enum.slice(0, 8).join(', ')}${schema.enum.length > 8 ? ', …' : ''}]${rescued ? ' (coerced to default — data silently lost)' : ''}`,
      })
    }
    return
  }

  if (!typeOk(schema, value)) {
    out.push({ level: 'HARD', path, msg: `type mismatch: expected ${schema.type}, got ${Array.isArray(value) ? 'array' : typeof value} (${JSON.stringify(value)?.slice(0, 60)})` })
    return
  }

  if (schema.type === 'string' && schema.format === 'date-time' && !ISO_DT.test(value)) {
    out.push({ level: 'note', path, msg: `date-time doesn't look ISO-8601: ${JSON.stringify(value)}` })
  }

  if (schema.type === 'array' && schema.items) {
    // Validate up to 25 elements per array to bound output/time.
    for (let i = 0; i < Math.min(value.length, 25); i++) validate(value[i], schema.items, `${path}[${i}]`, out, depth + 1)
    return
  }

  if (schema.type === 'object' || schema.properties) {
    const props = schema.properties ?? {}
    for (const [key, propSchema] of Object.entries<any>(props)) {
      const { nullable } = normalize(propSchema)
      const hasDefault = propSchema.default !== undefined
      const present = key in value && value[key] !== null
      if (!present) {
        // kotlinx: missing/null → default if the spec declares one (the
        // generator emits it), else MissingFieldException for non-nullables.
        const bare = !!propSchema.$ref
        if (!nullable && !hasDefault && !bare) {
          out.push({ level: 'HARD', path: `${path}.${key}`, msg: `non-nullable property (no default) missing or null` })
        }
        continue
      }
      validate(value[key], propSchema, `${path}.${key}`, out, depth + 1, { nullable, hasDefault })
    }
    if (schema.additionalProperties && typeof schema.additionalProperties === 'object') {
      for (const [k, v] of Object.entries(value).slice(0, 25)) {
        if (!(k in props)) validate(v, schema.additionalProperties, `${path}.${k}`, out, depth + 1)
      }
    }
    // Unknown keys: harmless to the SDK but may reveal a misspelled field.
    for (const k of Object.keys(value)) {
      if (props && Object.keys(props).length && !(k in props) && !schema.additionalProperties) {
        out.push({ level: 'note', path: `${path}.${k}`, msg: `key not in spec schema (SDK ignores; check for typo)` })
      }
    }
  }
}

// --- request sequence ---

type Step = { name: string; method?: string; path: string; schemaName: string | null; body?: any }

async function run() {
  const headers: Record<string, string> = {
    Authorization: `MediaBrowser Client="Wholphin", Device="strict-check", DeviceId="strict-1", Version="0.5", Token="${TOKEN}"`,
    'Content-Type': 'application/json',
  }

  const allViolations: { step: string; v: Violation }[] = []
  let failedSteps = 0

  async function step(s: Step): Promise<any> {
    const res = await fetch(BASE + s.path, { method: s.method ?? 'GET', headers, body: s.body ? JSON.stringify(s.body) : undefined })
    if (!res.ok) {
      console.log(`✗ ${s.name}: HTTP ${res.status}`)
      failedSteps++
      return null
    }
    const ct = res.headers.get('content-type') ?? ''
    if (!ct.includes('json')) { console.log(`• ${s.name}: ${res.status} (${ct || 'no body'})`); return null }
    const body = await res.json()
    if (s.schemaName) {
      const out: Violation[] = []
      const schema = s.schemaName === '[UserDto]'
        ? { type: 'array', items: { $ref: '#/components/schemas/UserDto' } }
        : s.schemaName === '[BaseItemDto]'
          ? { type: 'array', items: { $ref: '#/components/schemas/BaseItemDto' } }
          : { $ref: `#/components/schemas/${s.schemaName}` }
      validate(body, schema, '$', out)
      const hard = out.filter(v => v.level === 'HARD')
      const notes = out.filter(v => v.level === 'note')
      // Dedup by (level, msg-with-normalized-array-indexes)
      const seen = new Set<string>()
      const dedup = (vs: Violation[]) => vs.filter(v => {
        const key = v.level + v.path.replace(/\[\d+\]/g, '[]') + v.msg
        if (seen.has(key)) return false
        seen.add(key); return true
      })
      const h = dedup(hard), n = dedup(notes)
      console.log(`${h.length ? '✗' : '✓'} ${s.name}: ${h.length} hard, ${n.length} notes`)
      for (const v of h) { console.log(`    [CRASH] ${v.path}: ${v.msg}`); allViolations.push({ step: s.name, v }) }
      for (const v of n.slice(0, 6)) console.log(`    [note]  ${v.path}: ${v.msg}`)
      if (n.length > 6) console.log(`    [note]  … ${n.length - 6} more notes`)
    } else {
      console.log(`✓ ${s.name}: ${res.status}`)
    }
    return body
  }

  // 1. Pre-auth
  await step({ name: 'GET /System/Info/Public', path: '/System/Info/Public', schemaName: 'PublicSystemInfo' })
  await step({ name: 'GET /Users/Public', path: '/Users/Public', schemaName: '[UserDto]' })
  await step({ name: 'GET /QuickConnect/Enabled', path: '/QuickConnect/Enabled', schemaName: null })

  // 2. Session establishment (as after auth)
  const me = await step({ name: 'GET /Users/Me', path: '/Users/Me', schemaName: 'UserDto' })
  const myId = me?.Id
  await step({ name: 'POST /Sessions/Capabilities/Full', method: 'POST', path: '/Sessions/Capabilities/Full', schemaName: null, body: { PlayableMediaTypes: ['Video', 'Audio'], SupportedCommands: [], SupportsMediaControl: true } })
  await step({ name: `GET /DisplayPreferences/usersettings`, path: `/DisplayPreferences/usersettings?userId=${myId}&client=wholphin`, schemaName: 'DisplayPreferencesDto' })

  // 3. Home load
  const views = await step({ name: 'GET /UserViews', path: `/UserViews?userId=${myId}`, schemaName: 'BaseItemDtoQueryResult' })
  const FIELDS = 'PrimaryImageAspectRatio,Overview,CanDelete,MediaSourceCount,SortName,ParentId,MediaSources,Genres,ChildCount,Chapters,Trickplay,Width,Height,CustomRating,SeasonUserData'
  await step({ name: 'GET /Shows/NextUp', path: `/Shows/NextUp?userId=${myId}&fields=${FIELDS}&limit=20&enableResumable=false`, schemaName: 'BaseItemDtoQueryResult' })
  await step({ name: 'GET /UserItems/Resume', path: `/UserItems/Resume?userId=${myId}&fields=${FIELDS}&limit=20`, schemaName: 'BaseItemDtoQueryResult' })

  const sampleByType: Record<string, string> = {}
  for (const view of (views?.Items ?? [])) {
    const latest = await step({ name: `GET /Items/Latest [${view.Name}]`, path: `/Items/Latest?userId=${myId}&parentId=${view.Id}&fields=${FIELDS}&limit=16`, schemaName: '[BaseItemDto]' })
    const items = await step({ name: `GET /Items [${view.Name}]`, path: `/Items?userId=${myId}&parentId=${view.Id}&fields=${FIELDS}&limit=40&recursive=false&sortBy=SortName`, schemaName: 'BaseItemDtoQueryResult' })
    for (const it of [...(latest ?? []), ...(items?.Items ?? [])]) {
      if (it?.Type && !sampleByType[it.Type]) sampleByType[it.Type] = it.Id
    }
  }

  // 4. Detail + playback surfaces for one item of each kind we saw
  for (const [type, id] of Object.entries(sampleByType)) {
    await step({ name: `GET /Items/{id} [${type}]`, path: `/Items/${id}?userId=${myId}&fields=${FIELDS}`, schemaName: 'BaseItemDto' })
    if (type === 'Series') {
      const eps = await step({ name: 'GET /Shows/{id}/Episodes', path: `/Shows/${id}/Episodes?userId=${myId}&fields=${FIELDS}&limit=30`, schemaName: 'BaseItemDtoQueryResult' })
      const ep = eps?.Items?.[0]
      if (ep && !sampleByType.Episode) sampleByType.Episode = ep.Id
    }
  }
  for (const type of ['Movie', 'Episode']) {
    const id = sampleByType[type]
    if (!id) continue
    await step({ name: `POST /Items/{id}/PlaybackInfo [${type}]`, method: 'POST', path: `/Items/${id}/PlaybackInfo?userId=${myId}`, schemaName: 'PlaybackInfoResponse', body: { DeviceProfile: { MaxStreamingBitrate: 120000000 } } })
    await step({ name: `GET /MediaSegments/{id} [${type}]`, path: `/MediaSegments/${id}`, schemaName: 'MediaSegmentDtoQueryResult' })
  }

  // 5. Misc surfaces Wholphin touches early
  await step({ name: 'GET /Items/Filters', path: `/Items/Filters?userId=${myId}`, schemaName: 'QueryFiltersLegacy' })
  await step({ name: 'GET /Genres', path: `/Genres?userId=${myId}&limit=30`, schemaName: 'BaseItemDtoQueryResult' })
  await step({ name: 'GET /Persons', path: `/Persons?userId=${myId}&limit=30`, schemaName: 'BaseItemDtoQueryResult' })
  await step({ name: 'GET /Sessions', path: `/Sessions`, schemaName: null })
  await step({ name: 'GET /Items/Suggestions', path: `/Items/Suggestions?userId=${myId}&limit=10`, schemaName: 'BaseItemDtoQueryResult' })

  console.log(`\n=== ${allViolations.length} potential SDK crash sites across ${failedSteps} failed + rest ok steps ===`)
}

await run()
