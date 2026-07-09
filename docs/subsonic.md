# Subsonic-compatible API

Heya can answer the Subsonic REST protocol (1.16.1) with the OpenSubsonic
extensions, so stock Subsonic music clients — **Symfonium, DSub, play:Sub,
Tempo, Supersonic, Sonixd, substreamer** — can use Heya as their music
server. The surface is music-only by design: Heya's movie/TV/book libraries
don't exist here, and a probing client concludes "music server", never
"broken server".

## Enabling

Off by default. Two ways in, following the standard env-locks-UI provenance:

- `PUT /api/subsonic/config {"enabled": true}` (admin) — live toggle, no
  restart (Settings UI backing).
- `HEYA_SUBSONIC_API_ENABLED=true` — locks the toggle.

## Connecting a client (the auth story)

Subsonic's token auth is `t = md5(password + salt)` — the server must know
the shared secret in clear, which Heya's bcrypt login hashes cannot answer.
So Subsonic clients sign in with a dedicated **app password** (the same
solution Navidrome uses), never the Heya login password:

```bash
# CLI: mint (or read back) a user's app password
./bin/heya subsonic credential admin --rotate
./bin/heya subsonic credential admin            # show current
./bin/heya subsonic credential admin --revoke   # cut clients off

# Native API equivalents (per-user, token-authenticated)
heya api POST   /api/me/subsonic-credential     # create / rotate
heya api GET    /api/me/subsonic-credential     # read back
heya api DELETE /api/me/subsonic-credential     # revoke
```

The secret is server-generated (20 chars, no look-alike glyphs), stored
retrievably (that's the point — md5 token verification needs it), unique
per user, and rotating it never touches the real account password. All
three protocol auth forms verify against it:

- `u` + `p=<plain>` (and `p=enc:<hex>`)
- `u` + `t=md5(secret+salt)` + `s=<salt>` — what most clients use
- `apiKey=<secret>` — the OpenSubsonic `apiKeyAuthentication` extension
  (error 43 when combined with `u`/`p`/`t`, 44 for unknown keys)

### Symfonium walkthrough

1. Heya: Settings → enable the Subsonic API, then create your app password
   (or `heya subsonic credential <user> --rotate`).
2. Symfonium: Add media provider → **Subsonic**.
3. Server address: `http://your-host:8080/subsonic` (the `/subsonic`
   suffix matters).
4. Username: your Heya username. Password: the **app password**.
5. Leave "Force plaintext password" off — token auth works.

Same recipe for DSub / Tempo / play:Sub / Supersonic: server URL ends in
`/subsonic`, password is the app password.

## Architecture

Everything lives in `internal/subsonic/` (see the package comment in
`subsonic.go` for the constraints). The high-order bits:

- **Mount**: `/subsonic/*` in `internal/server/server.go`, exactly like the
  `/jellyfin` mount — always mounted, per-request enabled check, disabled
  surface falls through to 404. The dev front door (`heya dev-proxy`)
  forwards `/subsonic/*` to the backend.
- **Routing**: every endpoint answers at `/subsonic/rest/<name>` and
  `<name>.view`, GET or POST (OpenSubsonic `formPost`), case-insensitive.
  Unknown `/rest/` views answer in-protocol error 0, never SPA HTML.
- **Envelope**: one DTO set with dual `xml:`/`json:` tags
  (`envelope.go`/`dto.go`); XML default, `f=json`, `f=jsonp&callback=`
  (identifier-safe callbacks only). HTTP status is always 200 — errors ride
  the envelope, per protocol. Optionals are pointer+`omitempty` (goccy
  ignores `omitzero` — same scar tissue as the Jellyfin layer).
- **Ids**: typed strings — `ar-<artistId>`, `al-<albumId>`, `tr-<trackId>`,
  `mf-<libraryId>`, `pl-<playlistId>` — so the protocol's single `id`
  param routes by prefix (`ids.go`). Foreign ids answer error 70.
- **Service boundary**: handlers consume a `Backend` interface satisfied by
  `*service.App` — the generic JF listers (`queries/jellyfin.sql`) do the
  heavy lifting; Subsonic-specific reads (genre aggregation, album-list
  rankings, best-file decoration, play queue) live in
  `internal/service/subsonic_query.go` as raw-pgx service methods (no sqlc
  codegen, no collision with concurrent query work).
- **Delivery**: `stream`/`download` serve the track's best file directly
  (range-capable, SMB-aware) — raw bytes; `maxBitRate`/`format` are
  accepted and ignored. `getCoverArt` dispatches in-process to the native
  image pipeline (`/api/media/{id}/image/poster`, album cover endpoint) and
  resolves redirects server-side, mirroring the Jellyfin layer.
- **User state is shared state**: star/unstar = Heya loved hearts,
  setRating = Heya 1..10 ratings (stars × 2), scrobble = `play_events`
  (history + stats), now-playing reports mirror into the live session store
  (activity panel + `getNowPlaying`). Playlists are the same
  `user_playlists` rows the web sidebar shows.
- **Persistence**: migration `00009_subsonic.sql` — `subsonic_credentials`
  (app passwords) and `subsonic_play_queues` (`getPlayQueue` /
  `savePlayQueue` cross-device resume).

## Coverage

Subsonic has no machine spec to vendor, so the endpoint universe (the
api.jsp reference table + OpenSubsonic additions) is checked in as
`spec.go`, triaged in `manifest.go`; tests fail on untriaged endpoints and
on manifest/route drift. Inspect with:

```bash
./bin/heya subsonic coverage          # implemented + stubbed
./bin/heya subsonic coverage --all    # everything incl. unsupported
```

Current score: **49 implemented · 12 stubbed · 21 unsupported · 82 total**.

Statuses: `implemented` (real behavior), `stubbed` (correct "feature
absent" answers — empty shares/podcasts/radio/chat, refused user
mutations), `unsupported` (unregistered; answers in-protocol error 0 —
jukebox, HLS, sharing/podcast/radio mutations, bookmarks).

Notable triage calls:

- Folder-style endpoints (`getIndexes`, `getMusicDirectory`,
  `getAlbumList`, `search2`, `getStarred`) serve ID3-shaped data — Heya
  has no folder tree, and this is exactly what Navidrome does.
- `getSimilarSongs*` runs the sonic-embedding KNN; `getTopSongs` joins the
  Last.fm top-tracks rail to local files; `getArtistInfo*` serves the
  artist biography + locally-present similar artists.
- User mutations (`createUser`, `changePassword`, ...) validate then
  refuse with error 50 — accounts are managed in Heya, and a lying 200
  would be worse.
- `getPodcasts`/`getInternetRadioStations` answer empty: Heya's podcast +
  radio features are per-user and live on the native API. Bridging them is
  a real follow-up, not stub-forever.

## Testing

- `go test ./internal/subsonic/` — envelope (XML/JSON/JSONP), auth (all
  three forms + error codes), id mapping, browse/search/annotation
  handlers against a fake backend, and the coverage-manifest cross-checks.
- `bun tools/subsonic-smoke.ts [base] [user] [pw]` — protocol walk against
  a live server (defaults `http://localhost:8080 admin admin`):
  bootstraps its own app password through the native API, then extension
  discovery → ping (p=, t/s, apiKey, wrong-password 40) → browse → lists →
  search3 → stream bytes (Range) → cover art → star/rating/scrobble →
  playlist CRUD → play queue → getUser.

Dev-topology note: in `make dev` the front door forwards `/subsonic/*`;
run protocol tests against `http://127.0.0.1:8080/subsonic` (or the
backend directly at `http://127.0.0.1:3050/subsonic`).

## Known gaps / follow-ups

- **No transcoding**: `stream` always serves original bytes. Wiring
  `maxBitRate`/`format` to the shared AAC session manager
  (`transcoder.AudioSessionManager`, as `/Audio/{id}/universal` does on
  the Jellyfin side) is the obvious next step for cellular listening.
- `getScanStatus` reports `scanning=false` + track count (no live scan
  progress bridge yet); `startScan` really enqueues music library scans.
- Podcasts + internet radio answer empty (see above).
- `getAlbumList2 type=highest` serves starred (no community-rating rank);
  `alphabeticalByArtist` sorts by album title.
- Multi-artist credits: Subsonic's OpenSubsonic `artists` arrays aren't
  emitted; primary artist only.
