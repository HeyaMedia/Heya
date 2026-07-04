# UI vocabulary

Frontend conventions for the Nuxt 4 SPA — shared primitives, surface chrome,
and the gotchas that keep biting if you don't know them.

## `surface.css` — anything that floats

Floating elements (popovers, dropdowns, dialogs, tooltips, context menus, the
search panel) all share **`surface.css`** (`.surface` + the `surface-*` inner
classes):

- Glass background via `color-mix(in oklab, var(--bg-2) 92%, transparent)` +
  `backdrop-filter`.
- A single border + shadow recipe.
- A scale-in animation tied to reka's `[data-state="open"]`.
- A surface-scoped cascade that maps `--fg-3: var(--fg-2)`, lifting muted-text
  contrast inside any floating panel so subtitles stay legible against bright
  backdrops (the Calvin-Harris-page contrast fix).

Reach for a shared primitive instead of hand-rolling a dropdown / dialog /
tooltip / context menu / etc. — each wraps reka-ui primitives, uses
`defineModel` for v-model, and applies the surface chrome. They all live in
`web/app/components/ui/App*.vue`.

## Shared `App*` primitives

| Primitive | Wraps | Use when |
| --- | --- | --- |
| **`AppSurface`** | — (raw `.surface` element) | Any floating panel that *isn't* a reka popover (the rest already use it). Use the `as` prop with reka `as-child` so the surface element is also the positioned popper. |
| **`MediaCard`** | wraps `Poster` | Any tile that shows a poster/cover/backdrop with text. Paints title + subtitle over the image on a bottom gradient (same treatment as MusicCard / EpisodeCard). Props: `src`, `idx`, `title`, `subtitle`, `aspect` (default `2/3`), `badgeTl`/`badgeTr`, `progressPct`, `missing`. Use the `#badges` slot for custom overlays (watched checkmark, resolution chip, hover-only action buttons). Slotted elements absolutely-position inside the Poster — they need `z-index: 3` to sit above the gradient. **Don't roll your own `Poster + .grid-tile-meta` pattern** — this primitive is the unified treatment. Skip it for circular avatars (cast/crew) where text overlay reads badly. |
| **`AppMenu`** | `DropdownMenuRoot/Trigger/Portal/Content` | Anchored action menus — user avatar dropdown, activity, anything where the trigger has a fixed position. Slot `#trigger` for the button content, default slot for `DropdownMenuItem` rows. |
| **`AppSelect`** | `SelectRoot/Trigger/Portal/Content/Item` | Value-picking dropdowns. Pass `:options="[{value,label,meta?}]"`. Use a non-empty sentinel like `'default'` for zero-state rows — reka treats `""` as no-value. `customBaseline` triggers a gold "explicit override" tint when value isn't the default. |
| **`AppContextMenu`** | `ContextMenuRoot/Trigger/Portal/Content/Item/Sub*` | Right-click menus. Wrap each contextable element; the `items` prop builds the menu lazily on right-click. One level of submenu supported. |
| **`AppSwitch`** | `SwitchRoot/Thumb` | Boolean toggles outside of bulk-settings panes. `size="sm"` for inline use, `"md"` for settings rows. |
| **`AppTooltip`** | `TooltipRoot/Trigger/Portal/Content/Arrow` | Hover labels on icon-only buttons. Wrap the trigger; pass `label` (or use `#content` slot for richer body). The default layout already mounts `<TooltipProvider :delay-duration="400" :skip-delay-duration="200">` so all instances share the same hover-delay feel. |
| **`AppSlider`** | `SliderRoot/Track/Range/Thumb` | Linear value inputs (volume, gain). Single-value `v-model:number`. `bipolar` styles the fill from centre outward — pair with symmetric min/max like ±12 dB. |
| **`AppDialog`** | `DialogRoot/Portal/Overlay/Content/Title/Close` | Generic modals — "Add to list", video-player popups, search panels. `title`, `description`, `size` (sm/md/lg/xl/full), `closable`, slots: default body + optional `#footer`. Pass `prevent-auto-focus` for display-only dialogs (video player) where reka's default focus-on-first-button is distracting. |
| **`AppSheet`** | `DrawerRoot/Portal/Overlay/Content/Handle/Title` | Bottom sheet for phone/tablet (music now-playing, queue, filter panels). `v-model:open`, `title?`, `size` (`'auto'` = content height capped at `92dvh`, `'full'` = `92dvh`), `handle` (default `true`). Slots: default body (wrapped in `.scroll`), `#header` (replaces the title row). Swipe-down-to-dismiss is the Drawer primitive's own behavior — no extra wiring. z-index 400 (above `.surface`'s 200 and the playbar's 40, below `AppSelect`'s 5100). |
| **`ConfirmDialog`** + **`useConfirm()`** | `AlertDialogRoot/...` | Destructive confirms only. Promise-based: `await confirm({title, message, destructive: true})`. ConfirmDialog is mounted once in the default layout; you only call `useConfirm()`. |
| **`PathBrowser`** | (uses `useElementBounding`, `onClickOutside`) | Local filesystem path picker. Renders inline (not portaled) so it doesn't trip a parent modal's click-outside. |

## Reka primitives used directly (no `App*` wrapper)

When a use case is one-off enough that a wrapper would be ceremony:

- **`TabsRoot` / `TabsList` / `TabsTrigger` / `TabsContent`** — for tabbed
  content within a page (cast/crew on detail pages, the
  videos/extras/seasons switcher). Style `.tab-btn` with
  `[data-state="active"]` for the selected look; don't hand-roll
  `:class="{ active: x === y }"`.
- **`CollapsibleRoot` / `CollapsibleTrigger` / `CollapsibleContent`** — for
  disclosure widgets (music sidebar groups, album track-list reveal,
  PlaybackPrefs collapse). Animate via `--reka-collapsible-content-height`
  keyframes instead of measuring in JS.
- **`DialogRoot` / `DialogPortal` / `DialogOverlay` / `DialogContent`** — for
  the few specialised modals that pre-date AppDialog (CreatePlaylistModal,
  MetadataEditorModal, Lightbox, TaskItemsModal, UserSettingsModal). New
  modals should use `AppDialog` instead unless they need genuinely different
  chrome.

## Conventions earned the hard way

These are bug-avoidance rules that aren't obvious from reading the code.

### Never call `useNuxtApp()` inside `computed()` or async bodies

`useNuxtApp()` inside a `computed()` or inside the body of an async function
that runs after setup **silently hangs requests** when the Vue instance isn't
the active one — the request goes out, but the response never resolves to the
local closure. Hoist `const { $heya } = useNuxtApp()` to script-setup top
level.

### Scoped CSS doesn't reach portaled / child-owned elements

Scoped CSS doesn't reach buttons rendered by `AppMenu` (or any other reka
primitive that owns its trigger element) — the rendered button carries
AppMenu's `data-v-*`, not the consumer's, so `[data-v-X].my-class` selectors
don't match. Rules that need to land on the trigger live in an unscoped
`<style>` block. Same constraint applies to anything portaled (`AppDialog`
content, `AppMenu` content, `AppContextMenu` content).

### Reka popovers ignore JS-dispatched events

Clicks must be **trusted** (CDP `Input.dispatchMouseEvent`). `contextmenu` and
`pointerenter` (for tooltips) are exceptions and accept JS-dispatched events.
See the eye-tool `click` / `rclick` / `hover` commands in `docs/eye.md`.

### `backdrop-filter` is poisoned by an ancestor with its own `backdrop-filter`

An ancestor with its own `backdrop-filter` causes a descendant's
`backdrop-filter` to render ~30% opaque regardless of background opacity. Fix
is either to drop the ancestor's backdrop-filter or to portal the child out of
that subtree (e.g., the search dropdown teleports out of `.topbar` for this
reason).

### Stacking-context audit one-liner

Anything along a parent chain with `transform`, `filter`, `backdrop-filter`,
`will-change`, `contain`, or `isolation: isolate` other than `none/auto`
creates a containing block for absolutely-positioned descendants and can break
child `backdrop-filter`, `position: fixed`, or stacking-context assumptions.
Run this in the eye `eval` to walk the chain:

```js
let el = document.querySelector('.foo'), chain = []
while (el) { const cs = getComputedStyle(el);
  chain.push({tag: el.tagName, classes: el.className,
    transform: cs.transform, filter: cs.filter,
    backdropFilter: cs.backdropFilter,
    willChange: cs.willChange, contain: cs.contain,
    isolation: cs.isolation});
  el = el.parentElement }
chain
```

### Image URLs are unconditional

Always emit `/api/media/{id}/image/{type}` (or `usePosterUrl(id)` /
`useBackdropUrl(id)` / `useAlbumCoverUrl(id)` composables) on the FE — don't
gate on `poster_path` / `backdrop_path` / `cover_path` being non-empty. The
endpoint walks `media_assets` first before falling back to
`media_items.poster_path`, so the column being empty doesn't mean no image.
The `<Poster>` component's `imgError` handler renders the gradient placeholder
on a real 404.

Past bug: `MusicHome.vue` gated on `a.poster_path` and skipped the request
entirely, so freshly-scanned artists with `media_assets` rows but unmirrored
columns rendered blank tiles even though the image existed.

### Slug-first addressing

Anything with a stable slug is addressed by slug, not numeric ID, in the URL.
Artists use their `media_items.slug`; albums use the
`(artist_slug, album_slug)` pair (album slugs are unique within an artist, not
globally). Tracks have no slug so they stay ID-addressed.
`useAlbumCoverUrl(artistSlug, albumSlug)` is the canonical FE composable —
every list row already carries both fields, so call sites just pass them
through.

## Responsive conventions

Full plan in [docs/responsive-plan.md](responsive-plan.md). The ratified
pieces every package builds on:

### Breakpoints

Three literal `max-width` values — CSS custom properties can't appear inside
a media query, so these numbers are hardcoded at every call site, not derived
from a token:

| Name   | Query                        | Meaning                           |
| ------ | ---------------------------- | ---------------------------------- |
| phone  | `@media (max-width: 720px)`  | single-column, bottom nav, sheets  |
| tablet | `@media (max-width: 960px)`  | collapse side panels, keep top nav |
| narrow | `@media (max-width: 1200px)` | desktop, tightened padding         |

Touch affordances key off `@media (pointer: coarse)`, not width — a touch
laptop at desktop width still wants bigger tap targets, and a mouse-driven
narrow window doesn't.

### `useViewport()`

`app/composables/useViewport.ts` wraps VueUse's `useMediaQuery` as a shared
singleton (one set of `matchMedia` listeners for the whole app, module-level
cache):

```ts
const { isPhone, isTablet, isDesktop, isCoarse } = useViewport()
// isPhone: <=720px, isTablet: 720.02-960px, isDesktop: >960px — mutually
// exclusive JS tiers whose edges sit exactly on the CSS breakpoints above
// (the CSS queries overlap by design for progressive tightening; the JS
// tiers partition). isCoarse: matchMedia('(pointer: coarse)'), width-
// independent.
```

Guards `import.meta.server` like the rest of the SSR-sensitive composables
(`useMediaSession`, `usePlayer`) even though the app is `ssr:false` — Nuxt
still evaluates composables during the shell's prerender pass.

### Desktop-unchanged guardrail

Every mobile behavior lands behind a breakpoint media query or an
`isPhone`/`isCoarse` conditional — never as a change to the unconditional
desktop rule. Before/after screenshots of a page at desktop width must be
pixel-identical for any package that touches shared chrome (`heya.css`,
layouts, `App*` primitives). Spot-check with Heya Eye at `1600×1000`.
