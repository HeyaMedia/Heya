#!/usr/bin/env bun
/**
 * Subsonic-compat API smoke test.
 *
 * Walks the call sequence a stock Subsonic/OpenSubsonic client performs
 * against a Heya server with the Subsonic API enabled: extension discovery →
 * ping (xml + json + token auth) → browse (folders / artists / artist /
 * album / song / directory) → lists (albumList2, random, genres) → search3 →
 * stream bytes → cover art → annotation (star / rating / scrobble) →
 * playlists CRUD → play queue → getUser.
 *
 * Bootstraps its own credentials through the NATIVE API: logs in with a
 * Heya username/password, enables the Subsonic API (admin), and rotates the
 * user's Subsonic app password. Zero dependencies — plain fetch. Usage:
 *
 *   bun tools/subsonic-smoke.ts [baseUrl] [username] [password]
 *
 * Defaults: http://localhost:8080 admin admin. baseUrl is the Heya root.
 * Exits non-zero on the first failed assertion.
 */

import { createHash } from 'node:crypto'

const base = (process.argv[2] ?? 'http://localhost:8080').replace(/\/$/, '')
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

// --- native bootstrap: login, enable API, mint app password ---
section('bootstrap (native API)')
let appPassword = ''
{
  const login = await fetch(`${base}/api/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  ok(login.status === 200, 'native login → 200')
  const token = (await login.json()).token as string

  const enable = await fetch(`${base}/api/subsonic/config`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ enabled: true }),
  })
  ok(enable.status === 200, 'PUT /api/subsonic/config {enabled:true} → 200')

  const cred = await fetch(`${base}/api/me/subsonic-credential`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  })
  ok(cred.status === 200, 'POST /api/me/subsonic-credential → 200')
  appPassword = (await cred.json()).secret
  ok(typeof appPassword === 'string' && appPassword.length >= 16, 'app password minted')
}

const sub = `${base}/rest`
const authQ = `u=${encodeURIComponent(username)}&p=${encodeURIComponent(appPassword)}&v=1.16.1&c=heya-smoke`

async function ss(endpoint: string, extra = '', format = 'json'): Promise<any> {
  const res = await fetch(`${sub}/${endpoint}?${authQ}&f=${format}${extra ? '&' + extra : ''}`)
  if (format !== 'json') return res
  const body = await res.json()
  return body['subsonic-response']
}

// --- protocol basics ---
section('protocol basics')
{
  const ext = await fetch(`${sub}/getOpenSubsonicExtensions?f=json`) // NO auth on purpose
  ok(ext.status === 200, 'getOpenSubsonicExtensions without credentials → 200')
  const extBody = (await ext.json())['subsonic-response']
  ok(extBody.openSubsonic === true, 'envelope carries openSubsonic:true')
  const names = (extBody.openSubsonicExtensions ?? []).map((e: any) => e.name)
  ok(names.includes('formPost') && names.includes('apiKeyAuthentication'), `extensions advertised: ${names.join(', ')}`)

  const pingJson = await ss('ping')
  ok(pingJson.status === 'ok' && pingJson.version === '1.16.1', 'ping (json, p= auth)')
  ok(pingJson.type === 'heya' && typeof pingJson.serverVersion === 'string', 'OpenSubsonic envelope fields present')

  const pingXml = await fetch(`${sub}/ping.view?${authQ}`)
  const xmlText = await pingXml.text()
  ok(pingXml.headers.get('content-type')?.includes('xml') && xmlText.includes('status="ok"'), 'ping.view (xml default)')
  ok(xmlText.includes('xmlns="http://subsonic.org/restapi"'), 'xml namespace correct')

  // Token auth: t = md5(password + salt)
  const salt = 'smoke123'
  const token = createHash('md5').update(appPassword + salt).digest('hex')
  const tokRes = await fetch(`${sub}/ping?u=${encodeURIComponent(username)}&t=${token}&s=${salt}&v=1.16.1&c=heya-smoke&f=json`)
  const tokBody = (await tokRes.json())['subsonic-response']
  ok(tokBody.status === 'ok', 'ping (t/s token auth)')

  // apiKey auth
  const keyRes = await fetch(`${sub}/ping?apiKey=${encodeURIComponent(appPassword)}&v=1.16.1&c=heya-smoke&f=json`)
  ok(((await keyRes.json())['subsonic-response']).status === 'ok', 'ping (apiKey auth)')

  const bad = await fetch(`${sub}/ping?u=${encodeURIComponent(username)}&p=wrong&v=1.16.1&c=heya-smoke&f=json`)
  const badBody = (await bad.json())['subsonic-response']
  ok(bad.status === 200 && badBody.status === 'failed' && badBody.error?.code === 40, 'wrong password → HTTP 200 + error 40')

  const lic = await ss('getLicense')
  ok(lic.license?.valid === true, 'getLicense valid')
}

// --- browse ---
section('browse')
let artistId = ''
let albumId = ''
let songId = ''
{
  const folders = await ss('getMusicFolders')
  ok(Array.isArray(folders.musicFolders?.musicFolder), 'getMusicFolders shape')
  console.log(`    folders: ${(folders.musicFolders?.musicFolder ?? []).map((f: any) => f.name).join(', ') || '(none)'}`)

  const artists = await ss('getArtists')
  ok(typeof artists.artists?.ignoredArticles === 'string', 'getArtists carries ignoredArticles')
  const index = artists.artists?.index ?? []
  ok(Array.isArray(index), `getArtists index buckets: ${index.length}`)
  artistId = index[0]?.artist?.[0]?.id
  if (artistId) {
    const artist = await ss('getArtist', `id=${artistId}`)
    ok(artist.artist?.id === artistId, `getArtist round-trips (${artist.artist?.name})`)
    albumId = artist.artist?.album?.[0]?.id
  }
  if (albumId) {
    const album = await ss('getAlbum', `id=${albumId}`)
    ok(album.album?.id === albumId, `getAlbum round-trips (${album.album?.name})`)
    ok(Array.isArray(album.album?.song), `album songs: ${album.album?.song?.length ?? 0}`)
    songId = album.album?.song?.[0]?.id
  }
  if (songId) {
    const song = await ss('getSong', `id=${songId}`)
    ok(song.song?.id === songId, `getSong round-trips (${song.song?.title})`)
    ok(song.song?.suffix && song.song?.contentType, 'song carries suffix + contentType')
  }

  const dir = await ss('getMusicDirectory', `id=${artistId}`)
  ok(dir.directory?.child !== undefined, 'getMusicDirectory (artist) answers')

  const foreign = await ss('getArtist', 'id=zz-999')
  ok(foreign.status === 'failed' && foreign.error?.code === 70, 'foreign id → error 70')
}

// --- lists + search ---
section('lists + search')
{
  const newest = await ss('getAlbumList2', 'type=newest&size=5')
  ok(Array.isArray(newest.albumList2?.album), `getAlbumList2 newest: ${newest.albumList2?.album?.length ?? 0}`)
  const alpha = await ss('getAlbumList2', 'type=alphabeticalByName&size=5')
  ok(Array.isArray(alpha.albumList2?.album), 'getAlbumList2 alphabeticalByName')
  const legacy = await ss('getAlbumList', 'type=newest&size=5')
  ok(Array.isArray(legacy.albumList?.album), 'getAlbumList (legacy) answers')

  const random = await ss('getRandomSongs', 'size=5')
  ok(Array.isArray(random.randomSongs?.song), `getRandomSongs: ${random.randomSongs?.song?.length ?? 0}`)

  const genres = await ss('getGenres')
  ok(Array.isArray(genres.genres?.genre), `getGenres: ${genres.genres?.genre?.length ?? 0}`)
  const firstGenre = genres.genres?.genre?.[0]?.value
  if (firstGenre) {
    const byGenre = await ss('getSongsByGenre', `genre=${encodeURIComponent(firstGenre)}&count=5`)
    ok(Array.isArray(byGenre.songsByGenre?.song), `getSongsByGenre(${firstGenre})`)
  }

  const search = await ss('search3', 'query=a&songCount=5&albumCount=5&artistCount=5')
  ok(search.searchResult3 !== undefined, 'search3 answers')
  const starred = await ss('getStarred2')
  ok(Array.isArray(starred.starred2?.song), 'getStarred2 shape')
}

// --- media ---
section('media')
if (songId) {
  const range = await fetch(`${sub}/stream?${authQ}&id=${songId}`, { headers: { Range: 'bytes=0-1023' } })
  ok(range.status === 206 || range.status === 200, `stream bytes → ${range.status}`)
  if (range.status === 206) {
    const buf = await range.arrayBuffer()
    ok(buf.byteLength === 1024, 'range request honored (1024 bytes)')
  }
  const dl = await fetch(`${sub}/download?${authQ}&id=${songId}`, { headers: { Range: 'bytes=0-0' } })
  ok(dl.headers.get('content-disposition')?.includes('attachment') ?? false, 'download sets attachment disposition')

  const art = await fetch(`${sub}/getCoverArt?${authQ}&id=${albumId}&size=64`)
  ok(art.status === 200 || art.status === 404, `getCoverArt → ${art.status} (${art.headers.get('content-type')})`)

  const lyrics = await ss('getLyricsBySongId', `id=${songId}`)
  ok(Array.isArray(lyrics.lyricsList?.structuredLyrics), 'getLyricsBySongId shape')
} else {
  console.log('  (no songs in library — skipping media flow)')
}

// --- annotation ---
section('annotation')
if (songId) {
  ok((await ss('star', `id=${songId}`)).status === 'ok', 'star song')
  const starred = await ss('getStarred2')
  ok((starred.starred2?.song ?? []).some((c: any) => c.id === songId), 'starred song visible in getStarred2')
  ok((await ss('unstar', `id=${songId}`)).status === 'ok', 'unstar song')

  ok((await ss('setRating', `id=${songId}&rating=5`)).status === 'ok', 'setRating 5')
  ok((await ss('setRating', `id=${songId}&rating=0`)).status === 'ok', 'setRating 0 clears')

  ok((await ss('scrobble', `id=${songId}&submission=false`)).status === 'ok', 'scrobble now-playing')
  ok((await ss('scrobble', `id=${songId}`)).status === 'ok', 'scrobble submission')
  const np = await ss('getNowPlaying')
  ok(Array.isArray(np.nowPlaying?.entry), 'getNowPlaying shape')
}

// --- playlists ---
section('playlists')
if (songId) {
  const created = await ss('createPlaylist', `name=smoke-playlist&songId=${songId}`)
  ok(created.playlist?.name === 'smoke-playlist', 'createPlaylist')
  const plid = created.playlist?.id
  ok((created.playlist?.entry ?? []).length === 1, 'created playlist has the song')

  const listed = await ss('getPlaylists')
  ok((listed.playlists?.playlist ?? []).some((p: any) => p.id === plid), 'playlist listed')

  ok((await ss('updatePlaylist', `playlistId=${plid}&name=smoke-renamed`)).status === 'ok', 'updatePlaylist rename')
  const detail = await ss('getPlaylist', `id=${plid}`)
  ok(detail.playlist?.name === 'smoke-renamed', 'rename visible')

  ok((await ss('updatePlaylist', `playlistId=${plid}&songIndexToRemove=0`)).status === 'ok', 'remove song by index')
  const after = await ss('getPlaylist', `id=${plid}`)
  ok((after.playlist?.entry ?? []).length === 0, 'song removed')

  ok((await ss('deletePlaylist', `id=${plid}`)).status === 'ok', 'deletePlaylist')
  const gone = await ss('getPlaylist', `id=${plid}`)
  ok(gone.status === 'failed' && gone.error?.code === 70, 'deleted playlist → 70')
}

// --- play queue + user ---
section('play queue + user')
if (songId) {
  ok((await ss('savePlayQueue', `id=${songId}&current=${songId}&position=1000`)).status === 'ok', 'savePlayQueue')
  const q = await ss('getPlayQueue')
  ok(q.playQueue?.current === songId && q.playQueue?.position === 1000, 'getPlayQueue round-trips')
}
{
  const user = await ss('getUser', `username=${encodeURIComponent(username)}`)
  ok(user.user?.username === username, 'getUser')
  ok(user.user?.streamRole === true, 'streamRole granted')
  const scan = await ss('getScanStatus')
  ok(typeof scan.scanStatus?.count === 'number', `getScanStatus (count=${scan.scanStatus?.count})`)
}

console.log(`\n${passed} passed, ${failed} failed`)
process.exit(failed === 0 ? 0 : 1)
