#!/usr/bin/env bun
// Jellyfin API conformance suite — a black-box port of the official server's
// integration tests (jellyfin/jellyfin, tests/Jellyfin.Server.Integration.Tests,
// branch release-10.11.z). Test names, request shapes, and expected statuses
// are kept verbatim wherever possible; run the same suite against a real
// Jellyfin (the oracle) and against Heya's compat surface and compare.
//
//   bun tools/jellyfin-conformance.ts                        # Heya on :8080/jellyfin, admin/admin
//   JF_URL=https://jf.example JF_USER=u JF_PASS=p bun tools/jellyfin-conformance.ts
//   JF_ALLOW_MUTATIONS=1 ...                                 # enable state-changing tests
//   JF_FILTER=UserController ...                             # substring filter on class/test
//
// Deviations from upstream (documented, deliberate):
//  - Upstream boots a pristine in-memory server per test class. This runner
//    targets a LIVE server: the StartupController wizard tests auto-skip when
//    the wizard is already complete, and exact-count assertions (exactly one
//    user, empty public users) are relaxed to shape assertions, marked
//    [relaxed] in the test name.
//  - Mutating tests (library create/delete/rename, user create + password
//    churn, LiveTV tuner-host posts) only run with JF_ALLOW_MUTATIONS=1 —
//    never point that at production.
//  - Tests requiring compiled-in test plugins (Dashboard TestPlugin pages,
//    /Encoder echo endpoints) are not portable and are reported as skips.

const BASE = (process.env.JF_URL ?? 'http://127.0.0.1:8080/jellyfin').replace(/\/$/, '')
const USER = process.env.JF_USER ?? 'admin'
const PASS = process.env.JF_PASS ?? 'admin'
const MUTATE = process.env.JF_ALLOW_MUTATIONS === '1'
const FILTER = process.env.JF_FILTER ?? ''

// Verbatim from AuthHelper.cs.
const AUTH = 'MediaBrowser Client="Jellyfin.Server%20Integration%20Tests", DeviceId="69420", Device="Apple%20II", Version="10.8.0"'

// --- tiny harness (sequential, like upstream's xunit.runner.json) ---

type Result = { cls: string; name: string; status: 'pass' | 'fail' | 'skip'; detail?: string }
const results: Result[] = []
const suite: { cls: string; name: string; fn: () => Promise<void> }[] = []

function test(cls: string, name: string, fn: () => Promise<void>) {
  suite.push({ cls, name, fn })
}
function skip(cls: string, name: string, reason: string) {
  suite.push({ cls, name, fn: async () => { throw new SkipError(reason) } })
}
class SkipError extends Error {}

function assert(cond: boolean, msg: string): asserts cond {
  if (!cond) throw new Error(msg)
}
function assertStatus(res: Response, ...want: number[]) {
  assert(want.includes(res.status), `expected ${want.join('|')}, got ${res.status}`)
}
function assert2xx(res: Response) {
  assert(res.ok, `expected 2xx, got ${res.status}`)
}
function assertJsonUtf8(res: Response) {
  const ct = res.headers.get('content-type') ?? ''
  assert(ct.startsWith('application/json'), `expected application/json, got "${ct}"`)
}

// --- client ---

let token = ''
let myUserId = ''
let rootItemId = ''
let wizardWasFresh = false

function authHeader(withToken = true): Record<string, string> {
  return { Authorization: withToken && token ? `${AUTH}, Token="${token}"` : AUTH }
}

async function req(method: string, path: string, opts: { auth?: boolean; body?: unknown; headers?: Record<string, string>; redirect?: RequestRedirect } = {}) {
  const headers: Record<string, string> = { ...authHeader(opts.auth !== false), ...opts.headers }
  let body: string | undefined
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }
  return fetch(BASE + path, { method, headers, body, redirect: opts.redirect ?? 'follow' })
}
const get = (p: string, o = {}) => req('GET', p, o)
const post = (p: string, o = {}) => req('POST', p, o)
const del = (p: string, o = {}) => req('DELETE', p, o)

const guid = () => crypto.randomUUID() // dashed, like Guid.NewGuid().ToString()

// AuthHelper.CompleteStartupAsync — verbatim flow on a fresh server, direct
// credential login on a configured one.
async function bootstrap() {
  const su = await fetch(BASE + '/Startup/User', { headers: authHeader(false) })
  let username = USER
  let password = PASS
  if (su.ok) {
    wizardWasFresh = true
    const dto = await su.json()
    username = dto.Name
    password = dto.Password ?? ''
    const done = await fetch(BASE + '/Startup/Complete', { method: 'POST', headers: authHeader(false) })
    if (done.status !== 204) throw new Error(`POST /Startup/Complete: ${done.status}`)
  }
  const res = await fetch(BASE + '/Users/AuthenticateByName', {
    method: 'POST',
    headers: { ...authHeader(false), 'Content-Type': 'application/json' },
    body: JSON.stringify({ Username: username, Pw: password }),
  })
  if (!res.ok) throw new Error(`AuthenticateByName failed: ${res.status} ${await res.text()}`)
  const auth = await res.json()
  token = auth.AccessToken
  const me = await get('/Users/Me')
  if (!me.ok) throw new Error(`GET /Users/Me failed: ${me.status}`)
  myUserId = (await me.json()).Id
  const root = await get(`/Users/${myUserId}/Items/Root`)
  if (root.ok) rootItemId = (await root.json()).Id
}

// =========================== ActivityLogControllerTests ===========================

test('ActivityLogController', 'ActivityLog_GetEntries_Ok', async () => {
  const res = await get('/System/ActivityLog/Entries')
  assertStatus(res, 200)
  assertJsonUtf8(res)
})

// =========================== BrandingControllerTests ===========================

test('BrandingController', 'GetConfiguration_ReturnsCorrectResponse', async () => {
  const res = await get('/Branding/Configuration', { auth: false })
  assertStatus(res, 200)
  assertJsonUtf8(res)
  const body = await res.json()
  assert(typeof body === 'object' && body !== null, 'BrandingOptions object expected')
})

for (const url of ['/Branding/Css', '/Branding/Css.css']) {
  test('BrandingController', `GetCss_ReturnsCorrectResponse (${url})`, async () => {
    const res = await get(url, { auth: false })
    assert2xx(res)
    const ct = res.headers.get('content-type') ?? ''
    assert(ct.startsWith('text/css'), `expected text/css, got "${ct}"`)
  })
}

// =========================== DashboardControllerTests ===========================

skip('DashboardController', 'GetDashboardConfigurationPage_ExistingPage_CorrectPage', 'needs compiled-in TestPlugin')
skip('DashboardController', 'GetDashboardConfigurationPage_BrokenPage_NotFound', 'needs compiled-in TestPlugin')

test('DashboardController', 'GetDashboardConfigurationPage_NonExistingPage_NotFound', async () => {
  const res = await get('/web/ConfigurationPage?name=ThisPageDoesntExists', { auth: false })
  assertStatus(res, 404)
})

test('DashboardController', 'GetConfigurationPages_NoParams_AllConfigurationPages', async () => {
  const res = await get('/web/ConfigurationPages')
  assertStatus(res, 200)
  assert(Array.isArray(await res.json()), 'ConfigurationPageInfo[] expected')
})

test('DashboardController', 'GetConfigurationPages_True_MainMenuConfigurationPages [relaxed]', async () => {
  const res = await get('/web/ConfigurationPages?enableInMainMenu=true')
  assertStatus(res, 200)
  assertJsonUtf8(res)
  assert(Array.isArray(await res.json()), 'array expected') // upstream: empty on pristine server
})

// =========================== ItemsControllerTests ===========================

test('ItemsController', 'GetItems_NoApiKeyOrUserId_Success', async () => {
  assertStatus(await get('/Items'), 200)
})

for (const sub of ['Items', 'Items/Resume']) {
  test('ItemsController', `GetUserItems_NonexistentUserId_NotFound (/Users/{g}/${sub})`, async () => {
    assertStatus(await get(`/Users/${guid()}/${sub}`), 404)
  })
}

test('ItemsController', 'GetItems_UserId_Ok', async () => {
  const res = await get(`/Items?userId=${myUserId}`)
  assertStatus(res, 200)
  const body = await res.json()
  assert(Array.isArray(body.Items) && typeof body.TotalRecordCount === 'number', 'QueryResult shape expected')
})

test('ItemsController', 'GetUserItems_UserId_Ok (/Users/{me}/Items)', async () => {
  const res = await get(`/Users/${myUserId}/Items`)
  assertStatus(res, 200)
  assert(Array.isArray((await res.json()).Items), 'QueryResult shape expected')
})

test('ItemsController', 'GetUserItemsResume_UserId_Ok (/Users/{me}/Items/Resume)', async () => {
  const res = await get(`/Users/${myUserId}/Items/Resume`)
  assertStatus(res, 200)
  assert(Array.isArray((await res.json()).Items), 'QueryResult shape expected')
})

// =========================== LibraryControllerTests ===========================

const libraryNotFoundPaths = [
  (g: string) => `/Items/${g}/File`,
  (g: string) => `/Items/${g}/ThemeSongs`,
  (g: string) => `/Items/${g}/ThemeVideos`,
  (g: string) => `/Items/${g}/ThemeMedia`,
  (g: string) => `/Items/${g}/Ancestors`,
  (g: string) => `/Items/${g}/Download`,
  (g: string) => `/Artists/${g}/Similar`,
  (g: string) => `/Items/${g}/Similar`,
  (g: string) => `/Albums/${g}/Similar`,
  (g: string) => `/Shows/${g}/Similar`,
  (g: string) => `/Movies/${g}/Similar`,
  (g: string) => `/Trailers/${g}/Similar`,
]
for (const mk of libraryNotFoundPaths) {
  const label = mk('{g}')
  test('LibraryController', `Get_NonexistentItemId_NotFound (${label})`, async () => {
    assertStatus(await get(mk(guid())), 404)
  })
}

test('LibraryController', 'Delete_NonexistentItemId_Unauthorised (DELETE /Items/{g})', async () => {
  assertStatus(await del(`/Items/${guid()}`, { auth: false }), 401)
})
test('LibraryController', 'Delete_NonexistentItemId_Unauthorised (DELETE /Items?ids={g})', async () => {
  assertStatus(await del(`/Items?ids=${guid()}`, { auth: false }), 401)
})
test('LibraryController', 'Delete_NonexistentItemId_NotFound (DELETE /Items/{g})', async () => {
  assertStatus(await del(`/Items/${guid()}`), 404)
})
test('LibraryController', 'Delete_NonexistentItemId_NotFound (DELETE /Items?ids={g})', async () => {
  assertStatus(await del(`/Items?ids=${guid()}`), 404)
})

// =========================== LibraryStructureControllerTests ===========================
// Ordered scenario, mutating: creates library "test", updates its options,
// deletes it. Gated.

if (MUTATE) {
  test('LibraryStructureController', 'Post_NewVirtualFolder_Success', async () => {
    const res = await post('/Library/VirtualFolders?name=test&refreshLibrary=true', {
      body: { LibraryOptions: { Enabled: false } },
    })
    assertStatus(res, 204)
  })

  test('LibraryStructureController', 'UpdateLibraryOptions_Invalid_NotFound', async () => {
    const res = await post('/Library/VirtualFolders/LibraryOptions', {
      body: { Id: guid(), LibraryOptions: {} },
    })
    assertStatus(res, 404)
  })

  test('LibraryStructureController', 'UpdateLibraryOptions_Valid_Success', async () => {
    await new Promise(r => setTimeout(r, 2000)) // upstream waits for async library creation
    const list = await get('/Library/VirtualFolders')
    assertStatus(list, 200)
    const folders = await list.json()
    const lib = folders.find((f: any) => f.Name === 'test')
    assert(lib, 'library "test" should exist')
    assert(lib.LibraryOptions.Enabled === false, 'Enabled should be false before update')
    lib.LibraryOptions.Enabled = true
    const res = await post('/Library/VirtualFolders/LibraryOptions', {
      body: { Id: lib.ItemId, LibraryOptions: lib.LibraryOptions },
    })
    assertStatus(res, 204)
  })

  test('LibraryStructureController', 'DeleteLibrary_Invalid_NotFound', async () => {
    assertStatus(await del('/Library/VirtualFolders?name=doesntExist'), 404)
  })

  test('LibraryStructureController', 'DeleteLibrary_Valid_Success', async () => {
    assertStatus(await del('/Library/VirtualFolders?name=test&refreshLibrary=true'), 204)
  })
} else {
  for (const n of ['Post_NewVirtualFolder_Success', 'UpdateLibraryOptions_Invalid_NotFound', 'UpdateLibraryOptions_Valid_Success', 'DeleteLibrary_Invalid_NotFound', 'DeleteLibrary_Valid_Success']) {
    skip('LibraryStructureController', n, 'mutating — set JF_ALLOW_MUTATIONS=1')
  }
}

// =========================== LiveTvControllerTests ===========================

test('LiveTvController', 'AddTunerHost_Unauthorized_ReturnsUnauthorized', async () => {
  const res = await post('/LiveTv/TunerHosts', { auth: false, body: { Type: 'm3u', Url: 'Test Data/dummy.m3u8' } })
  assertStatus(res, 401)
})
skip('LiveTvController', 'AddTunerHost_Valid_ReturnsCorrectResponse', 'needs dummy.m3u8 on server disk')
if (MUTATE) {
  test('LiveTvController', 'AddTunerHost_InvalidType_ReturnsNotFound', async () => {
    assertStatus(await post('/LiveTv/TunerHosts', { body: { Type: 'invalid', Url: 'Test Data/dummy.m3u8' } }), 404)
  })
  test('LiveTvController', 'AddTunerHost_InvalidUrl_ReturnsNotFound', async () => {
    assertStatus(await post('/LiveTv/TunerHosts', { body: { Type: 'm3u', Url: 'thisgoesnowhere' } }), 404)
  })
} else {
  skip('LiveTvController', 'AddTunerHost_InvalidType_ReturnsNotFound', 'posts to live config — set JF_ALLOW_MUTATIONS=1')
  skip('LiveTvController', 'AddTunerHost_InvalidUrl_ReturnsNotFound', 'posts to live config — set JF_ALLOW_MUTATIONS=1')
}

// =========================== MediaInfoControllerTests ===========================

test('MediaInfoController', 'BitrateTest_Default_Ok', async () => {
  const res = await get('/Playback/BitrateTest')
  assertStatus(res, 200)
  const ct = res.headers.get('content-type') ?? ''
  assert(ct.startsWith('application/octet-stream'), `expected octet-stream, got "${ct}"`)
})

test('MediaInfoController', 'BitrateTest_WithValidParam_Ok', async () => {
  const res = await get('/Playback/BitrateTest?size=102400')
  assertStatus(res, 200)
  const body = await res.arrayBuffer()
  assert(body.byteLength >= 102400, `expected >= 102400 bytes, got ${body.byteLength}`)
})

for (const size of [0, -102400, 1000000000]) {
  test('MediaInfoController', `BitrateTest_InvalidValue_BadRequest (size=${size})`, async () => {
    assertStatus(await get(`/Playback/BitrateTest?size=${size}`), 400)
  })
}

// =========================== MediaStructureControllerTests ===========================

test('MediaStructureController', 'RenameVirtualFolder_WhiteSpaceName_ReturnsBadRequest', async () => {
  assertStatus(await post('/Library/VirtualFolders/Name?name=+&newName=test'), 400)
})
test('MediaStructureController', 'RenameVirtualFolder_WhiteSpaceNewName_ReturnsBadRequest', async () => {
  assertStatus(await post('/Library/VirtualFolders/Name?name=test&newName=+'), 400)
})
test('MediaStructureController', 'RenameVirtualFolder_NameDoesntExist_ReturnsNotFound', async () => {
  assertStatus(await post('/Library/VirtualFolders/Name?name=doesnt+exist&newName=test'), 404)
})
test('MediaStructureController', 'AddMediaPath_PathDoesntExist_ReturnsNotFound', async () => {
  assertStatus(await post('/Library/VirtualFolders/Paths', { body: { Name: 'Test', Path: '/this/path/doesnt/exist' } }), 404)
})
test('MediaStructureController', 'UpdateMediaPath_WhiteSpaceName_ReturnsBadRequest', async () => {
  assertStatus(await post('/Library/VirtualFolders/Paths/Update', { body: { Name: ' ', PathInfo: { Path: 'test' } } }), 400)
})
test('MediaStructureController', 'RemoveMediaPath_WhiteSpaceName_ReturnsBadRequest', async () => {
  assertStatus(await del('/Library/VirtualFolders/Paths?name=+'), 400)
})
test('MediaStructureController', 'RemoveMediaPath_PathDoesntExist_ReturnsNotFound', async () => {
  assertStatus(await del('/Library/VirtualFolders/Paths?name=none&path=%2Fthis%2Fpath%2Fdoesnt%2Fexist'), 404)
})

// =========================== MusicGenreControllerTests ===========================

test('MusicGenreController', 'MusicGenres_FakeMusicGenre_NotFound', async () => {
  assertStatus(await get('/MusicGenres/Fake-MusicGenre'), 404)
})

// =========================== PersonsControllerTests ===========================

test('PersonsController', 'GetPerson_DoesntExist_NotFound', async () => {
  assertStatus(await get('/Persons/DoesntExist'), 404)
})

// =========================== PlaystateControllerTests ===========================

test('PlaystateController', 'DeleteMarkUnplayedItem_NonexistentUserId_NotFound', async () => {
  assertStatus(await del(`/Users/${guid()}/PlayedItems/${guid()}`), 404)
})
test('PlaystateController', 'PostMarkPlayedItem_NonexistentUserId_NotFound', async () => {
  assertStatus(await post(`/Users/${guid()}/PlayedItems/${guid()}`), 404)
})
test('PlaystateController', 'DeleteMarkUnplayedItem_NonexistentItemId_NotFound', async () => {
  assertStatus(await del(`/Users/${myUserId}/PlayedItems/${guid()}`), 404)
})
test('PlaystateController', 'PostMarkPlayedItem_NonexistentItemId_NotFound', async () => {
  assertStatus(await post(`/Users/${myUserId}/PlayedItems/${guid()}`), 404)
})

// =========================== PluginsControllerTests ===========================

test('PluginsController', 'GetPlugins_Unauthorized_ReturnsUnauthorized', async () => {
  assertStatus(await get('/Plugins', { auth: false }), 401)
})
test('PluginsController', 'GetPlugins_Authorized_ReturnsCorrectResponse', async () => {
  const res = await get('/Plugins')
  assertStatus(res, 200)
  assertJsonUtf8(res)
  assert(Array.isArray(await res.json()), 'PluginInfo[] expected')
})

// =========================== StartupControllerTests ===========================
// Pre-wizard scenario: only runnable on a pristine server. The bootstrap
// completes the wizard when it finds one, so on a fresh server the only test
// still verifiable afterwards is the lock-out; on a configured server that's
// also the one meaningful assertion.

skip('StartupController', 'Configuration_EditConfig_Success', 'needs pristine pre-wizard server')
skip('StartupController', 'User_DefaultUser_NameWithoutPassword', 'needs pristine pre-wizard server')
skip('StartupController', 'User_EditUser_Success', 'needs pristine pre-wizard server')
skip('StartupController', 'CompleteWizard_Success', wizardWasFresh ? 'exercised by bootstrap' : 'needs pristine pre-wizard server')

test('StartupController', 'GetFirstUser_CompleteWizard_Unauthorized', async () => {
  const res = await get('/Startup/User', { auth: false })
  assertStatus(res, 401)
})

// =========================== UserControllerTests ===========================

test('UserController', 'GetPublicUsers_Valid_Success [relaxed]', async () => {
  const res = await get('/Users/Public', { auth: false })
  assertStatus(res, 200)
  assert(Array.isArray(await res.json()), 'UserDto[] expected') // upstream: empty on pristine server
})

test('UserController', 'GetUsers_Valid_Success [relaxed]', async () => {
  const res = await get('/Users')
  assertStatus(res, 200)
  const users = await res.json()
  assert(Array.isArray(users) && users.length >= 1, 'at least one user expected') // upstream: exactly 1
  assert(users.every((u: any) => typeof u.HasConfiguredPassword === 'boolean'), 'HasConfiguredPassword expected')
})

test('UserController', 'Me_Valid_Success', async () => {
  const res = await get('/Users/Me')
  assertStatus(res, 200)
  assert(typeof (await res.json()).Id === 'string', 'UserDto.Id expected')
})

test('UserController', 'Delete_DoesntExist_NotFound (DELETE /User/{g} — routing 404)', async () => {
  assertStatus(await del(`/User/${guid()}`), 404)
})

if (MUTATE) {
  let testUserId = ''
  test('UserController', 'New_Valid_Success', async () => {
    const res = await post('/Users/New', { body: { Name: 'testUser01' } })
    assertStatus(res, 200)
    const user = await res.json()
    assert(user.Name === 'testUser01', 'Name should round-trip')
    assert(user.HasPassword === false && user.HasConfiguredPassword === false, 'fresh user has no password')
    testUserId = user.Id
  })

  for (const name of [null, '', '   ', '‼️']) {
    test('UserController', `New_Invalid_Fail (${JSON.stringify(name)})`, async () => {
      assertStatus(await post('/Users/New', { body: { Name: name } }), 400)
    })
  }

  test('UserController', 'UpdateUserPassword_Valid_Success', async () => {
    const idN = testUserId.replaceAll('-', '') // upstream posts the "N" (dashless) GUID format
    assertStatus(await post(`/Users/${idN}/Password`, { body: { NewPw: '4randomPa$$word' } }), 204)
    const users = await (await get('/Users')).json()
    const u = users.find((x: any) => x.Id === testUserId)
    assert(u?.HasPassword === true && u?.HasConfiguredPassword === true, 'password should be set')
  })

  test('UserController', 'UpdateUserPassword_Empty_RemoveSetPassword', async () => {
    const idN = testUserId.replaceAll('-', '')
    assertStatus(await post(`/Users/${idN}/Password`, { body: { CurrentPw: '4randomPa$$word' } }), 204)
    const users = await (await get('/Users')).json()
    const u = users.find((x: any) => x.Id === testUserId)
    assert(u?.HasPassword === false && u?.HasConfiguredPassword === false, 'password should be removed')
  })

  test('UserController', 'Cleanup_DeleteTestUser (not upstream — restores state)', async () => {
    assertStatus(await del(`/Users/${testUserId}`), 204)
  })
} else {
  for (const n of ['New_Valid_Success', 'New_Invalid_Fail', 'UpdateUserPassword_Valid_Success', 'UpdateUserPassword_Empty_RemoveSetPassword']) {
    skip('UserController', n, 'mutating — set JF_ALLOW_MUTATIONS=1')
  }
}

// =========================== UserLibraryControllerTests ===========================

test('UserLibraryController', 'GetRootFolder_NonexistentUserId_NotFound', async () => {
  assertStatus(await get(`/Users/${guid()}/Items/Root`), 404)
})

test('UserLibraryController', 'GetRootFolder_UserId_Valid', async () => {
  const res = await get(`/Users/${myUserId}/Items/Root`)
  assertStatus(res, 200)
  assert(typeof (await res.json()).Id === 'string', 'BaseItemDto expected')
})

const userLibrarySubs = ['', '/Intros', '/LocalTrailers', '/SpecialFeatures', '/Lyrics']
for (const sub of userLibrarySubs) {
  test('UserLibraryController', `GetItem_NonexistentUserId_NotFound (/Users/{g}/Items/{root}${sub})`, async () => {
    const item = rootItemId || guid()
    assertStatus(await get(`/Users/${guid()}/Items/${item}${sub}`), 404)
  })
}
for (const sub of userLibrarySubs) {
  test('UserLibraryController', `GetItem_NonexistentItemId_NotFound (/Users/{me}/Items/{g}${sub})`, async () => {
    assertStatus(await get(`/Users/${myUserId}/Items/${guid()}${sub}`), 404)
  })
}

// Upstream marks these three Skip("flaky") — ported but kept as skips for parity.
skip('UserLibraryController', 'GetItem_UserIdAndItemId_Valid', 'skipped upstream ("flaky after refactor")')
skip('UserLibraryController', 'GetIntros_UserIdAndItemId_Valid', 'skipped upstream ("flaky after refactor")')
skip('UserLibraryController', 'LocalTrailersAndSpecialFeatures_UserIdAndItemId_Valid', 'skipped upstream ("flaky after refactor")')

// =========================== VideosControllerTests ===========================

test('VideosController', 'DeleteAlternateSources_NonexistentItemId_NotFound', async () => {
  assertStatus(await del(`/Videos/${guid()}`), 404)
})

// =========================== EncodedQueryStringTest / OpenApiSpecTests ===========================

skip('EncodedQueryStringTest', 'Ensure_Decoding_Of_Urls_Is_Working', 'needs test-only /Encoder echo controller')
skip('EncodedQueryStringTest', 'Ensure_Array_Decoding_Of_Urls_Is_Working', 'needs test-only /Encoder echo controller')

test('OpenApiSpecTests', 'GetSpec_ReturnsCorrectResponse', async () => {
  const res = await get('/api-docs/openapi.json', { auth: false })
  assert2xx(res)
  assertJsonUtf8(res)
})

// =========================== RobotsRedirectionMiddlewareTests ===========================

test('RobotsRedirectionMiddleware', 'RobotsDotTxtRedirects', async () => {
  const res = await get('/robots.txt', { auth: false, redirect: 'manual' })
  assertStatus(res, 302)
  const loc = res.headers.get('location') ?? ''
  assert(loc.endsWith('web/robots.txt'), `expected Location web/robots.txt, got "${loc}"`)
})

// =========================== WebSocketTests ===========================

test('WebSocketTests', 'WebSocket_Unauthenticated_Rejected', async () => {
  const wsURL = BASE.replace(/^http/, 'ws') + '/websocket'
  const outcome = await new Promise<'open' | 'refused'>(resolve => {
    const ws = new WebSocket(wsURL)
    const timer = setTimeout(() => { ws.close(); resolve('refused') }, 4000)
    ws.onopen = () => { clearTimeout(timer); ws.close(); resolve('open') }
    ws.onerror = () => { clearTimeout(timer); resolve('refused') }
    ws.onclose = () => { clearTimeout(timer); resolve('refused') }
  })
  assert(outcome === 'refused', 'unauthenticated websocket must be rejected')
})

// --- run ---

console.log(`jellyfin-conformance → ${BASE} (mutations ${MUTATE ? 'ENABLED' : 'off'})\n`)
try {
  await bootstrap()
} catch (err) {
  console.error(`bootstrap failed: ${err}`)
  process.exit(2)
}

for (const t of suite) {
  if (FILTER && !`${t.cls}.${t.name}`.toLowerCase().includes(FILTER.toLowerCase())) continue
  try {
    await t.fn()
    results.push({ cls: t.cls, name: t.name, status: 'pass' })
  } catch (err) {
    if (err instanceof SkipError) {
      results.push({ cls: t.cls, name: t.name, status: 'skip', detail: err.message })
    } else {
      results.push({ cls: t.cls, name: t.name, status: 'fail', detail: String(err instanceof Error ? err.message : err) })
    }
  }
}

let lastCls = ''
for (const r of results) {
  if (r.cls !== lastCls) {
    console.log(`\n${r.cls}`)
    lastCls = r.cls
  }
  const icon = r.status === 'pass' ? '  ✓' : r.status === 'skip' ? '  −' : '  ✗'
  console.log(`${icon} ${r.name}${r.detail ? ` — ${r.detail}` : ''}`)
}

const pass = results.filter(r => r.status === 'pass').length
const fail = results.filter(r => r.status === 'fail').length
const skipped = results.filter(r => r.status === 'skip').length
console.log(`\n${pass} passed, ${fail} failed, ${skipped} skipped (${results.length} total)`)
process.exit(fail > 0 ? 1 : 0)
