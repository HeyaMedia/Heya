<!--
  SpotlightSearch — the single, app-wide spotlight overlay (Heya 2.0).

  Consolidates what used to be TWO surfaces: AppTopBar's inline desktop
  dropdown AND AppSearchOverlay's phone fullscreen sheet. One component now
  serves both:

  - Desktop / tablet: a floating glass panel (`.search-panel`) pinned near the
    top of the viewport (top ~84px, width min(760px, 92vw)), over a blurred
    scrim. Close via Escape or clicking the dimmed backdrop.
  - Phone (<=760px): the panel goes full-screen — sticky input with a close
    button, no hints footer, scroll body fills the viewport.

  It is opened from the AppTopBar trigger pill (trusted click) or the app-wide
  Cmd/Ctrl+K "/" hotkey (both owned by AppTopBar, which is always mounted).

  Behavior carried from the two originals (inventory §5 contract):
  - useQuickSearch(180): 180ms debounce, latest-wins, /api/search/quick.
  - Grouped buckets in the DESKTOP order (Movies -> TV -> Artists -> Albums ->
    Tracks -> Books -> Collections -> People). The old phone music-first order
    was UNIFIED to this one — see DEFERRED.md.
  - Full keyboard nav: arrows across the flattened result rows, Enter opens the
    highlighted row (or the /search page when nothing is highlighted), combobox
    aria-activedescendant semantics so a screen reader tracks the selection
    without moving DOM focus off the input.
  - Per-bucket navigation map unchanged (movies -> /movies/:slug ... tracks ->
    album route, collections -> /search page).
  - Hardware-back (phone): opening pushes a dummy history entry so the back
    gesture closes the overlay; popstate only ever closes. Focus trap + focus
    restore + scroll lock + autofocus preserved.

  Everything returned is in-library, so each row carries a neutral type tag
  (Film / Series / Artist / ...). The mockup's external/ghost tier needs
  backend work that does not exist yet — see DEFERRED.md.

  Scoped CSS reaches the teleported content because the <Teleport> lives in THIS
  template (it keeps this SFC's scope id — unlike reka-portaled content).
-->
<template>
  <Teleport to="body">
    <Transition name="spotlight">
      <div
        v-if="open"
        ref="overlayRef"
        class="search-overlay"
        role="dialog"
        aria-modal="true"
        aria-label="Search your library"
        @click.self="onBackdropClick"
        @keydown.tab="onTrapTab"
      >
        <div class="search-panel" role="document">
          <!-- Input row -->
          <div class="search-input">
            <Icon name="search" :size="17" class="si-glyph" />
            <input
              ref="inputRef"
              v-model="search.query.value"
              class="si-field"
              type="text"
              role="combobox"
              aria-label="Search titles, artists, people"
              aria-autocomplete="list"
              aria-haspopup="listbox"
              :aria-expanded="hasResults"
              aria-controls="spotlight-listbox"
              :aria-activedescendant="activeDescendantId"
              autocapitalize="off"
              autocorrect="off"
              spellcheck="false"
              enterkeyhint="search"
              placeholder="Search titles, artists, people…"
              @keydown.down.prevent="move(1)"
              @keydown.up.prevent="move(-1)"
              @keydown.enter.prevent="onEnter"
              @keydown.esc.prevent="requestClose"
            />
            <span class="si-scope">All libraries</span>
            <button type="button" class="search-close" aria-label="Close search" @click="requestClose">
              <Icon name="close" :size="15" />
            </button>
          </div>

          <!-- Body -->
          <div id="spotlight-listbox" class="search-body" role="listbox" aria-label="Search results">
            <div v-if="!search.query.value.trim()" class="si-state">
              <Icon name="search" :size="24" />
              <p>Search your library</p>
            </div>

            <div v-else-if="search.loading.value && !search.data.value" class="si-state">
              <span class="si-spinner" />
              <p>Searching…</p>
            </div>

            <div v-else-if="search.data.value && sections.length === 0" class="si-state">
              <p>No results for <strong>{{ search.data.value.query }}</strong></p>
            </div>

            <template v-else>
              <div v-for="(section, sIdx) in sections" :key="section.key" class="sr-section">
                <div class="sr-group">
                  {{ section.label }}
                  <span class="n">{{ section.bucket.total.toLocaleString() }}</span>
                </div>

                <button
                  v-for="(item, iIdx) in section.bucket.items"
                  :id="optionId(flatIndex(sIdx, iIdx))"
                  :key="section.key + ':' + item.id"
                  type="button"
                  class="sr-row"
                  role="option"
                  tabindex="-1"
                  :class="{ sel: flatIndex(sIdx, iIdx) === selectedIdx }"
                  :aria-selected="flatIndex(sIdx, iIdx) === selectedIdx"
                  @click="goToResult(section.key, item)"
                  @mouseenter="selectedIdx = flatIndex(sIdx, iIdx)"
                >
                  <div class="sr-thumb" :class="section.thumbShape">
                    <LoadingImage
                      v-if="thumbUrl(section.key, item)"
                      :src="thumbUrl(section.key, item)!"
                      :width="92"
                      :quality="80"
                      loading="lazy"
                    />
                    <Icon v-else :name="section.icon" :size="16" />
                  </div>
                  <div class="sr-copy">
                    <div class="t">
                      <template v-for="(seg, i) in highlight(resultTitle(section.key, item))" :key="i">
                        <mark v-if="seg.mark">{{ seg.t }}</mark>
                        <template v-else>{{ seg.t }}</template>
                      </template>
                    </div>
                    <div v-if="resultSub(section.key, item)" class="m">{{ resultSub(section.key, item) }}</div>
                  </div>
                  <span class="tag">{{ section.tag }}</span>
                </button>

                <NuxtLink
                  v-if="section.bucket.total > section.bucket.items.length"
                  :to="`/search?q=${encodeURIComponent(search.query.value)}&type=${section.key}`"
                  class="sr-more"
                  @click="close"
                >
                  View all {{ section.bucket.total.toLocaleString() }} {{ section.label.toLowerCase() }}
                  <Icon name="arrow-right" :size="11" />
                </NuxtLink>
              </div>

              <NuxtLink
                :to="`/search?q=${encodeURIComponent(search.query.value)}`"
                class="sr-seeall"
                @click="close"
              >
                See all results for "{{ search.query.value }}"
                <Icon name="arrow-right" :size="12" />
              </NuxtLink>
            </template>
          </div>

          <!-- Hints (desktop / fine-pointer only) -->
          <div class="search-hints">
            <span><kbd>↑↓</kbd> Navigate</span>
            <span><kbd>↵</kbd> Open</span>
            <span class="r"><kbd>Esc</kbd> Close</span>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
const open = defineModel<boolean>('open', { default: false })

const route = useRoute()
const search = useQuickSearch(180)
const inputRef = ref<HTMLInputElement>()
const overlayRef = ref<HTMLElement>()
const selectedIdx = ref(-1)
const { isCoarse } = useViewport()

interface Section {
  key: 'movies' | 'tv' | 'music' | 'books' | 'albums' | 'tracks' | 'collections' | 'people'
  label: string
  tag: string
  icon: string
  thumbShape: 'poster' | 'square' | 'circle'
  bucket: { items: any[], total: number }
}

// UNIFIED order (desktop): titles first so a generic name ("Peter") surfaces
// the handful of movies/shows above the long tail of people. The old phone
// overlay led with music buckets — that split was collapsed to this single
// order (recorded in DEFERRED.md).
const SECTION_DEFS: Array<Omit<Section, 'bucket'>> = [
  { key: 'movies',      label: 'Films',       tag: 'Film',       icon: 'film',  thumbShape: 'poster' },
  { key: 'tv',          label: 'Series',      tag: 'Series',     icon: 'tv',    thumbShape: 'poster' },
  { key: 'music',       label: 'Artists',     tag: 'Artist',     icon: 'music', thumbShape: 'square' },
  { key: 'albums',      label: 'Albums',      tag: 'Album',      icon: 'music', thumbShape: 'square' },
  { key: 'tracks',      label: 'Tracks',      tag: 'Track',      icon: 'music', thumbShape: 'square' },
  { key: 'books',       label: 'Books',       tag: 'Book',       icon: 'book',  thumbShape: 'poster' },
  { key: 'collections', label: 'Collections', tag: 'Collection', icon: 'film',  thumbShape: 'poster' },
  { key: 'people',      label: 'People',      tag: 'Person',     icon: 'users', thumbShape: 'circle' },
]

const sections = computed<Section[]>(() => {
  const data = search.data.value
  if (!data) return []
  const out: Section[] = []
  for (const def of SECTION_DEFS) {
    const b = (data.buckets as any)[def.key]
    if (b && b.items && b.items.length > 0) out.push({ ...def, bucket: b })
  }
  return out
})

const hasResults = computed(() => sections.value.length > 0)
const totalItems = computed(() =>
  sections.value.reduce((sum, s) => sum + s.bucket.items.length, 0),
)

// ── Keyboard nav across the flattened row list ──────────────────────────
function flatIndex(sIdx: number, iIdx: number) {
  let n = 0
  for (let i = 0; i < sIdx; i++) {
    const s = sections.value[i]
    if (s) n += s.bucket.items.length
  }
  return n + iIdx
}

function move(delta: number) {
  const max = totalItems.value
  if (max === 0) return
  if (selectedIdx.value === -1) {
    selectedIdx.value = delta > 0 ? 0 : max - 1
  } else {
    selectedIdx.value = (selectedIdx.value + delta + max) % max
  }
  nextTick(() => {
    document.getElementById(optionId(selectedIdx.value))?.scrollIntoView({ block: 'nearest' })
  })
}

function selectedItem(): { sectionKey: Section['key'], item: any } | null {
  if (selectedIdx.value < 0) return null
  let n = 0
  for (const s of sections.value) {
    if (selectedIdx.value < n + s.bucket.items.length) {
      return { sectionKey: s.key, item: s.bucket.items[selectedIdx.value - n] }
    }
    n += s.bucket.items.length
  }
  return null
}

function optionId(flat: number): string {
  return `spotlight-opt-${flat}`
}

const activeDescendantId = computed(() =>
  selectedIdx.value >= 0 ? optionId(selectedIdx.value) : undefined,
)

// Reset the highlight when the result set changes so the cursor never points
// at a row that just scrolled out of the model.
watch(() => search.data.value, () => { selectedIdx.value = -1 })

function onEnter() {
  const sel = selectedItem()
  if (sel) {
    goToResult(sel.sectionKey, sel.item)
    return
  }
  const q = search.query.value.trim()
  if (q) {
    navigateTo(`/search?q=${encodeURIComponent(q)}`)
    close()
  }
}

// ── Match highlighting ──────────────────────────────────────────────────
// Split the title on the query tokens and flag the matched slices so the
// template can wrap them in <mark> (gold). Segment-based (no v-html) so we
// never inject markup from data.
function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function highlight(text: string): Array<{ t: string, mark: boolean }> {
  const q = search.query.value.trim()
  if (!q || !text) return [{ t: text, mark: false }]
  const tokens = q.split(/\s+/).filter(Boolean).map(escapeRegExp).sort((a, b) => b.length - a.length)
  if (tokens.length === 0) return [{ t: text, mark: false }]
  const splitRe = new RegExp(`(${tokens.join('|')})`, 'gi')
  const markRe = new RegExp(`^(?:${tokens.join('|')})$`, 'i')
  return text
    .split(splitRe)
    .filter(part => part !== '')
    .map(part => ({ t: part, mark: markRe.test(part) }))
}

// ── Row content + navigation (unchanged map from the originals) ─────────
function thumbUrl(kind: Section['key'], item: any): string | null {
  switch (kind) {
    case 'movies':
    case 'tv':
    case 'music':
    case 'books':
      return usePosterUrl(item)
    case 'people':
      return personImageUrl(item.id)
    case 'albums':
      return albumCoverUrl(item)
    case 'tracks':
      return item.artist_media_item_id
        ? usePosterUrl({ id: item.artist_media_item_id, public_id: item.artist_media_item_public_id })
        : null
    case 'collections':
      return null
  }
}

function resultTitle(kind: Section['key'], item: any): string {
  if (kind === 'people' || kind === 'collections') return item.name
  return item.title
}

function resultSub(kind: Section['key'], item: any): string {
  switch (kind) {
    case 'movies':
    case 'tv':
    case 'music':
    case 'books':
      return item.year || ''
    case 'albums':
      return item.artist_name + (item.year ? ' · ' + item.year : '')
    case 'tracks':
      return [item.artist_name, item.album_title].filter(Boolean).join(' · ')
    case 'people': {
      const parts: string[] = []
      if (item.cast_count) parts.push(`${item.cast_count} role${item.cast_count === 1 ? '' : 's'}`)
      if (item.crew_count) parts.push(`${item.crew_count} credit${item.crew_count === 1 ? '' : 's'}`)
      return parts.join(' · ')
    }
    case 'collections':
      return ''
  }
}

function goToResult(kind: Section['key'], item: any) {
  let path = ''
  switch (kind) {
    case 'movies':
      path = `/movies/${item.slug || slugify(item.title)}`
      break
    case 'tv':
      path = `/tv/${item.slug || slugify(item.title)}`
      break
    case 'music':
      path = `/music/artist/${item.slug || slugify(item.title)}`
      break
    case 'books':
      path = `/books/${item.slug || slugify(item.title)}`
      break
    case 'people':
      path = `/person/${item.slug || item.id}`
      break
    case 'albums':
      path = (item.artist_slug && item.slug)
        ? `/music/artist/${item.artist_slug}/${item.slug}`
        : `/music/artist/${item.artist_slug || slugify(item.artist_name)}`
      break
    case 'tracks':
      path = (item.artist_slug && item.album_slug)
        ? `/music/artist/${item.artist_slug}/${item.album_slug}`
        : `/music/artist/${item.artist_slug || slugify(item.artist_name)}`
      break
    case 'collections':
      path = `/search?q=${encodeURIComponent(item.name)}&type=collections`
      break
  }
  if (path) navigateTo(path)
  close()
}

// ── Close paths ─────────────────────────────────────────────────────────
function close() {
  open.value = false
}

// Backdrop-click (desktop): the dimmed area outside the panel. On phone the
// panel fills the overlay, so `.self` never fires there.
function onBackdropClick() {
  requestClose()
}

// ── Focus trap ──────────────────────────────────────────────────────────
// Result rows are tabindex=-1 (driven by arrows, not Tab — combobox pattern),
// so this only cycles the input, scope-less close button and the see-all
// links. The overlay covers the viewport, so there's nothing legitimate to
// tab out to.
function onTrapTab(e: KeyboardEvent) {
  const root = overlayRef.value
  if (!root) return
  const focusables = Array.from(
    root.querySelectorAll<HTMLElement>(
      'button:not([disabled]):not([tabindex="-1"]), [href], input:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  ).filter(el => el.offsetParent !== null)
  if (focusables.length === 0) return
  const first = focusables[0]!
  const last = focusables[focusables.length - 1]!
  if (e.shiftKey) {
    if (document.activeElement === first || !root.contains(document.activeElement)) {
      e.preventDefault()
      last.focus()
    }
  } else if (document.activeElement === last) {
    e.preventDefault()
    first.focus()
  }
}

// ── Hardware-back / history wiring (phone) ──────────────────────────────
// Only touch-capable clients get a hardware/gesture back button, so we only
// push the dummy history entry there — desktop keeps its history clean and
// closes via Esc / backdrop. The popstate listener is always live but only
// ever CLOSES (never reopens), so it's harmless on desktop.
let pushedState = false

function requestClose() {
  if (pushedState) {
    history.back() // consumes the dummy entry -> onPopState flips open false
  } else {
    close()
  }
}

function onPopState() {
  pushedState = false
  if (open.value) open.value = false
}

// Remembers whatever had focus before opening (the topbar trigger pill, or
// whatever a keyboard user was on when they hit ⌘K) so it can be restored on
// close. The restore lives in onUnmounted, NOT the watch else-branch, on
// purpose: AppTopBar mounts this via `v-if`, so closing unmounts the overlay,
// and removing the still-focused input resets focus to <body> — doing the
// restore AFTER that removal avoids the browser clobbering our focus() call.
let previouslyFocused: HTMLElement | null = null

onMounted(() => {
  window.addEventListener('popstate', onPopState)
})
onUnmounted(() => {
  window.removeEventListener('popstate', onPopState)
  document.documentElement.style.overflow = ''
  previouslyFocused?.focus()
  previouslyFocused = null
})

watch(open, (isOpen) => {
  if (isOpen) {
    previouslyFocused = document.activeElement as HTMLElement | null
    if (isCoarse.value) {
      pushedState = true
      history.pushState({ heyaSpotlight: true }, '')
    }
    document.documentElement.style.overflow = 'hidden'
    // { immediate: true } so the first mount (v-if flips it on already-open)
    // still focuses the field — a non-immediate watch would miss that initial
    // true and leave focus to the unreliable autofocus attr.
    nextTick(() => inputRef.value?.focus())
  } else {
    document.documentElement.style.overflow = ''
    search.reset()
    selectedIdx.value = -1
  }
}, { immediate: true })

// Safety net: any navigation we didn't route through close() still tears the
// overlay down.
watch(() => route.fullPath, () => { if (open.value) close() })
</script>

<style scoped>
/* ── Overlay scrim ──────────────────────────────────────────────────────
   Blurs whatever scrolls behind. Because THIS carries backdrop-filter, the
   panel below must NOT (gotcha #4: an ancestor's backdrop-filter poisons a
   descendant's) — the panel uses a near-opaque fill instead. */
.search-overlay {
  position: fixed;
  inset: 0;
  z-index: 450;
  background: rgb(var(--shade) / 0.62);
  backdrop-filter: blur(var(--glass-blur-md));
  -webkit-backdrop-filter: blur(var(--glass-blur-md));
  overflow-y: auto;
}

.search-panel {
  width: min(760px, 92vw);
  margin: 84px auto 0;
  border-radius: 16px;
  overflow: hidden;
  background: color-mix(in srgb, var(--bg-1) 96%, transparent);
  border: 1px solid var(--hair-strong);
  box-shadow: 0 40px 120px -20px rgb(var(--shade) / 0.7);
}

/* Input row */
.search-input {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--hair-strong);
}
.si-glyph { color: rgb(var(--ink) / 0.45); flex-shrink: 0; }
.si-field {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: 0;
  outline: 0;
  color: var(--fg-0);
  caret-color: var(--gold);
  font-size: 17px;
  font-weight: 550;
  padding: 0;
}
.si-field::placeholder { color: rgb(var(--ink) / 0.4); font-weight: 500; }
.si-scope {
  flex-shrink: 0;
  font: 550 10px var(--font-mono);
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-3);
  border: 1px solid var(--hair-strong);
  padding: 5px 10px;
  border-radius: 6px;
}
.search-close {
  display: none;
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  border-radius: 50%;
  border: 1px solid rgb(var(--ink) / 0.2);
  background: transparent;
  color: var(--fg-2);
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.search-close:hover { color: var(--fg-0); border-color: var(--hair-strong); }

/* Body */
.search-body {
  max-height: 62vh;
  overflow-y: auto;
  padding: 6px 10px 12px;
}

.si-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 56px 24px;
  color: var(--fg-3);
  font-size: 14px;
  text-align: center;
}
.si-state strong { color: var(--fg-1); font-weight: 600; }
.si-spinner {
  width: 16px; height: 16px;
  border: 2px solid var(--border-strong);
  border-top-color: var(--gold);
  border-radius: 50%;
  animation: si-spin 0.7s linear infinite;
}
@keyframes si-spin { to { transform: rotate(360deg); } }

.sr-group {
  display: flex;
  font: 600 9.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--fg-3);
  padding: 16px 14px 8px;
}
.sr-group .n { margin-left: auto; color: var(--fg-4); }

.sr-row {
  width: 100%;
  display: grid;
  grid-template-columns: 46px minmax(0, 1fr) auto;
  gap: 15px;
  align-items: center;
  padding: 8px 14px;
  border-radius: 10px;
  background: transparent;
  border: 0;
  text-align: left;
  cursor: pointer;
  color: var(--fg-0);
  transition: background 0.1s ease;
}
.sr-row:hover,
.sr-row.sel { background: rgb(var(--ink) / 0.06); }
.sr-row.sel { outline: 1px solid color-mix(in srgb, var(--gold) 45%, transparent); }

.sr-thumb {
  width: 46px;
  height: 62px;
  flex-shrink: 0;
  border-radius: 5px;
  overflow: hidden;
  background: var(--bg-2);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-4);
}
.sr-thumb.square { height: 46px; }
.sr-thumb.circle { height: 46px; border-radius: 50%; }
.sr-thumb :deep(img) { width: 100%; height: 100%; object-fit: cover; display: block; }

.sr-copy { min-width: 0; }
.sr-row .t {
  font-size: 14px;
  font-weight: 600;
  color: var(--fg-0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sr-row .t mark { background: none; color: var(--gold); }
.sr-row .m {
  margin-top: 3px;
  font: 500 10.5px var(--font-mono);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--fg-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sr-row .tag {
  font: 650 9px var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--fg-3);
  border: 1px solid var(--hair-strong);
  padding: 4px 8px;
  border-radius: 5px;
  white-space: nowrap;
}

.sr-more {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 14px 8px;
  font: 500 11px var(--font-mono);
  letter-spacing: 0.04em;
  color: var(--fg-3);
  text-decoration: none;
  transition: color 0.12s ease;
}
.sr-more:hover { color: var(--gold); }

.sr-seeall {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin: 6px 4px 2px;
  padding: 12px 14px;
  border-top: 1px solid var(--hair-strong);
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
  text-decoration: none;
  transition: color 0.12s ease;
}
.sr-seeall:hover { color: var(--gold); }

/* Hints */
.search-hints {
  display: flex;
  gap: 20px;
  padding: 13px 20px;
  border-top: 1px solid var(--hair-strong);
  font: 550 10px var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.search-hints .r { margin-left: auto; }
.search-hints kbd {
  font: inherit;
  color: var(--fg-1);
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--hair-strong);
  border-radius: 4px;
  padding: 2px 6px;
  margin-right: 4px;
}
/* Touch clients don't have a physical keyboard to hint at. */
@media (pointer: coarse) { .search-hints { display: none; } }

/* Entrance */
.spotlight-enter-active { transition: opacity 0.16s ease; }
.spotlight-leave-active { transition: opacity 0.12s ease; }
.spotlight-enter-active .search-panel { transition: transform 0.18s cubic-bezier(0.16, 1, 0.3, 1), opacity 0.18s ease; }
.spotlight-enter-from,
.spotlight-leave-to { opacity: 0; }
.spotlight-enter-from .search-panel { transform: translateY(-8px) scale(0.985); opacity: 0; }

/* ── Tablet ── */
@media (max-width: 1020px) {
  .search-panel { margin-top: 66px; width: min(680px, 94vw); }
}

/* ── Phone: full-screen ── */
@media (max-width: 760px) {
  .search-overlay {
    background: var(--bg-1);
    backdrop-filter: none;
    -webkit-backdrop-filter: none;
    overflow: hidden;
  }
  .search-panel {
    width: 100vw;
    height: 100dvh;
    margin: 0;
    border-radius: 0;
    border: 0;
    box-shadow: none;
    background: transparent;
    display: flex;
    flex-direction: column;
  }
  .search-input {
    position: sticky;
    top: 0;
    z-index: 2;
    padding: calc(env(safe-area-inset-top, 0px) + 12px) 16px 12px;
    background: color-mix(in srgb, var(--bg-1) 92%, transparent);
    backdrop-filter: blur(14px);
    -webkit-backdrop-filter: blur(14px);
  }
  .si-field { font-size: 16px; } /* >=16px so iOS Safari doesn't zoom on focus */
  .si-scope { display: none; }
  .search-close { display: inline-flex; margin-left: auto; }
  .search-body {
    max-height: none;
    flex: 1;
    padding: 4px 6px calc(env(safe-area-inset-bottom, 0px) + 24px);
  }
  .search-hints { display: none; }
  .sr-row { grid-template-columns: 40px minmax(0, 1fr) auto; gap: 12px; padding: 9px 10px; }
  .sr-thumb { width: 40px; height: 54px; }
  .sr-thumb.square, .sr-thumb.circle { height: 40px; }
  .sr-row .t { font-size: 13.5px; }
  .sr-row .tag { font-size: 8px; padding: 3px 6px; letter-spacing: 0.08em; }
}

@media (prefers-reduced-motion: reduce) {
  .spotlight-enter-active,
  .spotlight-leave-active,
  .spotlight-enter-active .search-panel { transition: none; }
  .spotlight-enter-from .search-panel { transform: none; }
  .si-spinner { animation: none; }
}
</style>
