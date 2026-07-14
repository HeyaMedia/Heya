# Responsive / mobile plan

Goal: make the desktop-only Nuxt SPA work on phones and tablets — music first
(Spotify-like mobile player), then the rest of the app, then PWA install
support. Android is the test target; iOS quirks are explicitly deferred.

**Prime guardrail: desktop does not change.** Every mobile behavior lands
behind a media query or an `isPhone`/`isCoarse` conditional. Desktop
screenshots before/after a package must be pixel-identical (spot-checked via
Heya Eye at 1600×1000).

Research basis (2026-07-05 codebase survey): no breakpoint convention exists
(7 stray `max-width` values clustering at ~700/900/1100); the shell is locked
to `100vh` + `body{overflow:hidden}`; the Playbar mounts inside
`pages/music.vue` (not the layout) while all audio state lives in the global
`usePlayer()` singleton; `useMediaSession()` (lock-screen/OS transport) is
already wired; reka-ui 2.10 ships an unused `Drawer*` family (swipe-dismiss
bottom sheet); Heya Eye cannot emulate a phone viewport yet.

## Breakpoints (ratified convention)

Three literal values, chosen to match the existing ad-hoc clusters. CSS custom
properties cannot appear in media queries, so these are documented literals —
new code uses exactly these numbers:

| Name   | Query                      | Meaning                          |
| ------ | -------------------------- | -------------------------------- |
| phone  | `@media (max-width: 720px)`  | single-column, bottom nav, sheets |
| tablet | `@media (max-width: 960px)`  | collapse side panels, keep top nav |
| narrow | `@media (max-width: 1200px)` | desktop, tightened padding       |

Touch affordances key off `@media (pointer: coarse)` / `isCoarse`, not width.

JS side: `useViewport()` composable (new, wraps VueUse `useBreakpoints`):

```ts
const { isPhone, isTablet, isDesktop, isCoarse } = useViewport()
// isPhone: <720, isTablet: 720–959, isDesktop: >=960
// isCoarse: matchMedia('(pointer: coarse)')
```

Existing stray breakpoints (900/1100/etc.) are folded onto these values
opportunistically when a package touches the file — no big-bang rewrite.

## Foundation changes (Wave 0)

### W0a — Heya Eye mobile viewport (tools/eye/eye.ts, docs/eye.md)

Eye hardcodes `--window-size=1600,1000` and never calls CDP Emulation. Add:

- `eye viewport <w>x<h> [--dpr N] [--touch]` — persists
  `{width,height,dpr,mobile:true,touch}` into the existing `state.json`;
  `eye viewport off` clears it.
- On **every** `connect()` (each subcommand connects fresh), if viewport state
  exists, apply `Emulation.setDeviceMetricsOverride({width,height,
  deviceScaleFactor,mobile:true})` and, when `touch`,
  `Emulation.setTouchEmulationEnabled({enabled:true})`.
- Document in docs/eye.md: mobile testing recipe is
  `eye viewport 390x844 --dpr 3 --touch` → `eye goto …` → `eye shot`.

### W0b — Tokens, composable, sheet primitive (web/)

1. **dvh + safe areas** (`assets/css/heya.css`): `.app` height `100vh` →
   `100dvh` (with `100vh` fallback line before it); add
   `--safe-bottom: env(safe-area-inset-bottom, 0px)` to `:root`; add
   `viewport-fit=cover` to the viewport meta in `nuxt.config.ts`.
2. **`useViewport()`** (`app/composables/useViewport.ts`): as specced above.
   Singleton via `createSharedComposable` or module-level state.
3. **`AppSheet.vue`** (`app/components/ui/AppSheet.vue`) — bottom sheet on
   reka's `Drawer*` family, reusing `.surface` chrome:
   - Props: `v-model:open`, `title?`, `size?: 'auto' | 'full'` (auto =
     content height capped at `92dvh`, full = `92dvh`), `handle?: boolean`
     (default true).
   - Slots: default body (internally wrapped in a `.scroll` region), `#header`.
   - `DrawerRoot > DrawerPortal > DrawerOverlay + DrawerContent`; drag handle;
     swipe-down dismiss; rounded top corners; `padding-bottom:
     var(--safe-bottom)`; overlay dim like AppDialog.
   - Content is portaled to body → no ancestor `backdrop-filter` poisoning
     (docs/ui.md gotcha #4). Do NOT reuse the popover scale-in animation —
     the drawer translates.
   - z-index above playbar/bottom-nav (≥300, below AppSelect's 5100).
4. **AppContextMenu on touch** (`app/components/ui/AppContextMenu.vue`):
   reka/Radix ContextMenu supports long-press natively on touch — verify it
   fires, then make the popper usable on coarse pointers via an unscoped
   `@media (pointer: coarse)` block: row min-height 44px, wider max-width.
   If long-press does NOT open it, add a pointerdown/450ms long-press handler
   on the trigger wrapper that opens the same menu. (Sheet-style presentation
   is later polish, not this package.)
5. **docs/ui.md**: add a "Responsive conventions" section — the three
   breakpoints, `useViewport()`, AppSheet row in the primitives table,
   desktop-unchanged guardrail.

## Wave 1 — Shell + mobile player (the "little Spotify")

### W1a — Top bar + bottom nav (AppTopBar.vue, layouts/default.vue, new BottomNav.vue)

- Extract the hardcoded `tabs` array (AppTopBar.vue:622–628) to
  `app/composables/useNavTabs.ts` (or `app/utils/nav.ts`) so top bar and
  bottom nav share one source, including the `/media/*`→Movies match rule.
- **Phone (≤720)**: hide `.topbar-tabs`; top bar = brand + search icon +
  activity ring + avatar. Search opens as a full-width overlay/sheet (the
  current teleported dropdown is fixed `width:460px` — on phone make it
  `width:100vw` minus padding, anchored under the top bar; sheet optional).
  Activity panel content gets `max-width:100vw` treatment.
- **New `BottomNav.vue`**: fixed bottom tab bar, 5 tabs from the shared nav
  source, icon + tiny label, active = gold (same `isActive` logic),
  `padding-bottom: var(--safe-bottom)`, hidden `@media (min-width: 721px)`,
  hidden on `/watch`. Mounted in `layouts/default.vue` (and
  `layouts/settings.vue`). Add `--bottomnav-h` token; on phone, content
  regions get bottom padding so nothing hides behind it.
- Settings stays reachable via the avatar dropdown (as on desktop).

### W1b — Mobile player components (new files only; no mounting)

Three new components under `app/components/music/mobile/`, all driven
exclusively by the existing `usePlayer()` API (do not touch the engine):

- **`MiniPlayer.vue`** — compact bar (~64px): 44px artwork, title/artist
  (single line, ellipsis), play/pause + next buttons, 2px progress line along
  the top edge (`position/duration`). Whole bar tap (except buttons) emits
  `expand`. Sits above BottomNav.
- **`NowPlayingSheet.vue`** — full-height AppSheet (`size="full"`): large
  artwork, title/artist/album as NuxtLinks (use `track.artist_slug` — note
  the desktop NowPlayingView.vue:189 hardcodes `artistSlug=''`, a known bug;
  do it right here and fix that line in passing), drag-seek scrubber
  (AppSlider bound to `position/duration`, calls `seek(fraction)`),
  transport row (shuffle / prev / play / next / repeat with repeat-one
  badge), time labels via `formatTime`, secondary row: queue button (opens
  QueueSheet), lyrics toggle (reuse the lyrics fetch pattern from
  QueuePanel.vue — `/api/music/tracks/{id}/lyrics`), volume slider.
- **`QueueSheet.vue`** — AppSheet listing Played (faded, tap = `jumpTo`),
  Now Playing (highlighted), Up Next rows: tap = `jumpTo`, explicit ↑/↓
  reorder buttons (`moveInQueue`) and ✕ remove (`removeFromQueue`) — always
  visible, 44px targets; HTML5 drag is desktop-only so don't rely on it.
  "Clear" → `clearUpcoming()`. Shuffle/repeat chips as in QueuePanel.

Everything needed exists on `usePlayer()`: `playing, currentTrack, position,
duration, volume, muted, shuffled, repeatMode, queue, upcomingTracks,
playedTracks, currentIndex, play, pause, togglePlay, seek(0..1), setVolume,
toggleMute, toggleShuffle, cycleRepeat, nextTrack, prevTrack, jumpTo,
moveInQueue, removeFromQueue, clearUpcoming, formatTime`.

### W1c — Music shell collapse + player mounting (pages/music.vue, MusicSidebar.vue, layouts/default.vue)

Runs after W1a + W1b (touches the same files/mount points).

- **Phone (≤720)** in `pages/music.vue`: hide `MusicSidebar` and `QueuePanel`
  entirely; hide desktop `Playbar`; render `MiniPlayer` + `NowPlayingSheet` +
  `QueueSheet` instead. Music sub-nav (sidebar sections + playlists) moves
  into an AppSheet opened from a compact header row (hamburger/chips) at the
  top of the music main column.
- **Tablet (≤960)**: keep `MusicSidebar`; `QueuePanel` becomes the
  QueueSheet (hide the 320px dock).
- **Global mini-player**: mount `MiniPlayer` (+ `NowPlayingSheet`) in
  `layouts/default.vue` on phone when `currentTrack` is non-null and route is
  NOT under `/music` (under `/music` the music shell owns it) — music keeps
  playing app-wide today (engine is global); this makes it visible app-wide.
  Positioned directly above BottomNav.
- EQ panel / visualizer / BigCover: desktop-only for now — don't render their
  toggles on phone.

## Wave 2 — Music pages

### W2a — Shared TrackList component + table pages

There is no shared track-row component; 8 pages hand-roll fixed
`grid-template-columns` tables that cannot fit a phone. Extract
`app/components/music/TrackList.vue`:

- Props: `tracks`, `columns` config (per-page desktop column templates are
  preserved exactly — pixel parity), `contextItems` fn (feeds
  `AppContextMenu`, i.e. `useMusicActions.forTrack`), active-row VuMeter,
  StarRating, TrackQualityPicker slots as needed.
- **Phone (≤720)**: rows collapse to a 2-line layout — 44px art (or index),
  title + artist/album stacked, duration, always-visible `⋯` button opening
  the same action items (long-press also works via AppContextMenu). No
  hover-only affordances.
- Migrate in this package: `music/songs.vue`, `music/loved.vue`,
  `music/my/favorites.vue`, `music/browse/[kind]/[key].vue`.

### W2b — Cards, grids, rails (parallel with W2a)

- `MusicCard.vue`: hover-only `.mc-play` gets a coarse-pointer alternative
  (tap = navigate as today; play via `⋯`/long-press — do not make bare tap
  play, it breaks navigation).
- Grid density: `.grid-posters` (heya.css:305) and the inline
  `minmax(160–180px)` grids get a phone override (`minmax(110px,1fr)`,
  tighter gaps) — one rule in heya.css plus the stragglers.
- `MusicScrollRow.vue` + `home/ContentRow.vue`: hide scroll-arrow buttons on
  coarse pointers, add `scroll-snap-type: x proximity`, ensure momentum
  scroll. (ContentRow included here because the music home rails use it.)

### W2c — Music detail + remaining pages (after W2a)

- Adopt `TrackList` in: `artist/[slug]/[album].vue`, `playlist/[id].vue`,
  `mix/[slug].vue`, `podcasts/feed.vue`.
- Stack the heroes on phone: album (220px cover), playlist (200px), mix
  (220px), podcast feed (180px), `MusicArtistDetail.vue` — cover centers,
  meta below, actions become a wrap row.
- `music/search.vue`, `music/stats.vue` (already has @media — align to 720),
  `music/library/index.vue`, `music/my/index.vue` hubs: stack stat rows,
  tune grids. `stations/builder.vue`: stacked wizard on phone (side-by-side
  panes stack vertically; functional > pretty for v1).

## Wave 3 — Rest of the app (parallel packages)

- **W3a Home**: HeroDeck phone variant (shorter, poster-forward),
  ContinueWatching/UpNext rails (ContentRow already done in W2b).
- **W3b Libraries** (movies/tv/books): `LibrarySidebar` (240px) → filter
  sheet on phone (button in toolbar); `FilterBar`/`LibraryToolbar` wrap;
  `usePosterGrid` phone column math; list view-mode table → stacked cards;
  `MediaCard` gets coarse-pointer `⋯` opening the existing context items.
- **W3c Detail pages**: movie/tv/person/season/episode — the `260px 1fr
  260px` bodies stack (some @media exists at 900/1100; extend to 720
  properly). Consolidation onto `MediaDetailView` is explicitly OUT of scope
  (risk); just add breakpoint CSS per page.
- **W3d Settings**: `layouts/settings.vue` sidebar → sheet/dropdown on
  phone; form pages mostly stack fine once the sidebar collapses; the 8
  wide-table pages (jobs, tasks, logs, database, metadata, tokens,
  transcoding, lists) get stacked-card mobile rows; dashboards reflow tiles.
  `metadata-editor` (Miller columns) is desktop-only for now — show a
  "desktop only" note on phone.
- **W3e Watch player** (deferred until after W4 unless prioritized): touch
  controls, orientation, safe areas for `player/VideoPlayer.vue`.

## Wave 4 — PWA

- `@vite-pwa/nuxt`: manifest (name Heya, `display: standalone`,
  `theme_color/background_color #0a0a12`, portrait orientation), icon set
  incl. maskable (generate from the existing logo asset).
- Service worker: precache app shell (built assets); **never cache `/api/*`**
  (network-only) — streams, images, and auth all go through it.
- Verify: Android Chrome install prompt, standalone launch, lock-screen
  controls still work (Media Session), audio continues in background.

## Delegation + verification protocol

- Implementation agents run on Sonnet with this doc + their package section
  as the spec. Packages are file-disjoint within a wave; cross-wave order is
  W0 → W1a/W1b → W1c → W2/W3 (parallel) → W4.
- Repo rules bind agents: bun only (never npm/npx), no `go build -o`, no new
  deps without approval (AppSheet uses the already-installed reka Drawer),
  follow docs/ui.md gotchas (scoped CSS vs portals, backdrop-filter
  poisoning, App* reuse, unconditional image URLs).
- Every package: `cd web && bun run typecheck` must pass (0 errors). No
  commits from agents — the orchestrator reviews, runs the visual gate, and
  commits per package.
- Visual gate (orchestrator, per package): Eye at `390x844 dpr3 touch`
  (phone), `820x1180` (tablet), `1600x1000` (desktop unchanged check) —
  screenshots actually looked at, per CLAUDE.md.
