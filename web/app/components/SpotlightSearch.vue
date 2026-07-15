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

  V2 (spotlight redesign):
  - Integrated, borderless input row (search icon + naked field + "All
    libraries" chip + Esc chip + phone ✕) over a single hairline; focus glows
    the hairline gold, no box outline.
  - Rich empty state instead of a lone placeholder: Quick Actions grid, Recent
    Searches (localStorage MRU), and a compact For-You strip from the existing
    /api/me/recommendations engine.
  - Keyboard arrow-nav is UNIFIED across both modes via `flatActions`: in the
    empty state it walks quick actions -> recents -> for-you; in results mode it
    walks the flattened result rows. Enter activates whatever's highlighted.

  Behavior carried from the two originals (inventory §5 contract):
  - useQuickSearch(180): 180ms debounce, latest-wins, /api/search/quick.
  - Grouped buckets in the DESKTOP order (Movies -> TV -> Artists -> Albums ->
    Tracks -> Books -> Collections -> People).
  - combobox aria-activedescendant semantics so a screen reader tracks the
    selection without moving DOM focus off the input.
  - Per-bucket navigation map unchanged.
  - Hardware-back (phone): opening pushes a dummy history entry so the back
    gesture closes the overlay; popstate only ever closes. Focus trap + focus
    restore + scroll lock + autofocus preserved.

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
          <!-- Input row — borderless, over a single hairline. Focus glows the
               hairline (see .search-input:focus-within), never a box outline. -->
          <div class="search-input">
            <Icon name="search" :size="18" class="si-glyph" />
            <input
              ref="inputRef"
              v-model="search.query.value"
              class="si-field"
              type="text"
              role="combobox"
              aria-label="Search titles, artists, people"
              aria-autocomplete="list"
              aria-haspopup="listbox"
              :aria-expanded="listboxExpanded"
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
            <kbd class="si-esc">Esc</kbd>
            <button type="button" class="search-close" aria-label="Close search" @click="requestClose">
              <Icon name="close" :size="16" />
            </button>
          </div>

          <!-- Body — listbox scroll region. Not tagged `.scroll` (it has its own
               max-height), so it opts into the overlay scrollbar explicitly. -->
          <div
            id="spotlight-listbox"
            v-overlay-scrollbar
            class="search-body"
            role="listbox"
            :aria-label="isEmptyQuery ? 'Search suggestions' : 'Search results'"
          >
            <!-- ── Empty state: quick actions + recents + for-you ── -->
            <template v-if="isEmptyQuery">
              <!-- QUICK ACTIONS -->
              <div class="es-group">
                <div class="sr-group">Quick Actions</div>
                <div class="qa-grid">
                  <button
                    v-for="(qa, i) in quickActions"
                    :id="optionId(i)"
                    :key="qa.label"
                    type="button"
                    class="qa-tile"
                    role="option"
                    tabindex="-1"
                    :class="{ sel: i === selectedIdx }"
                    :aria-selected="i === selectedIdx"
                    :title="qa.title"
                    @click="runQuickAction(qa)"
                    @mouseenter="selectedIdx = i"
                  >
                    <span class="qa-ico"><Icon :name="qa.icon" :size="19" /></span>
                    <span class="qa-label">{{ qa.label }}</span>
                  </button>
                </div>
              </div>

              <!-- RECENT SEARCHES -->
              <div v-if="recentItems.length" class="es-group">
                <div class="sr-group">
                  Recent Searches
                  <button type="button" class="sr-group-action" @click="recent.clear()">Clear</button>
                </div>
                <button
                  v-for="(q, j) in recentItems"
                  :id="optionId(recentIndex(j))"
                  :key="q"
                  type="button"
                  class="rs-row"
                  role="option"
                  tabindex="-1"
                  :class="{ sel: recentIndex(j) === selectedIdx }"
                  :aria-selected="recentIndex(j) === selectedIdx"
                  @click="runRecent(q)"
                  @mouseenter="selectedIdx = recentIndex(j)"
                >
                  <Icon name="clock" :size="14" class="rs-ico" />
                  <span class="rs-text">{{ q }}</span>
                  <span
                    class="rs-remove"
                    aria-label="Remove recent search"
                    @click.stop="recent.remove(q)"
                  >
                    <Icon name="close" :size="12" />
                  </span>
                </button>
              </div>

              <!-- FOR YOU -->
              <div v-if="forYouItems.length" class="es-group">
                <div class="sr-group">For You</div>
                <div class="fy-strip">
                  <button
                    v-for="(it, k) in forYouItems"
                    :id="optionId(forYouIndex(k))"
                    :key="it.id"
                    type="button"
                    class="fy-card"
                    role="option"
                    tabindex="-1"
                    :class="{ sel: forYouIndex(k) === selectedIdx }"
                    :aria-selected="forYouIndex(k) === selectedIdx"
                    :title="it.title"
                    @click="runForYou(it)"
                    @mouseenter="selectedIdx = forYouIndex(k)"
                  >
                    <div class="fy-thumb">
                      <LoadingImage
                        v-if="usePosterUrl(it)"
                        :src="usePosterUrl(it)!"
                        :width="120"
                        :quality="80"
                        loading="lazy"
                      />
                      <Icon v-else name="film" :size="16" />
                    </div>
                    <span class="fy-title">{{ it.title }}</span>
                  </button>
                </div>
              </div>
            </template>

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
                  @click="onSeeAll"
                >
                  View all {{ section.bucket.total.toLocaleString() }} {{ section.label.toLowerCase() }}
                  <Icon name="arrow-right" :size="11" />
                </NuxtLink>
              </div>

              <NuxtLink
                :to="`/search?q=${encodeURIComponent(search.query.value)}`"
                class="sr-seeall"
                @click="onSeeAll"
              >
                See all results for "{{ search.query.value }}"
                <Icon name="arrow-right" :size="12" />
              </NuxtLink>
            </template>
          </div>

          <!-- Hints (keyboard-capable machines only — see media query) -->
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
import { useQuery } from '@pinia/colada'
import { spotlightForYouQuery, type SpotlightRecItem } from '~/queries/search'

const open = defineModel<boolean>('open', { default: false })

const route = useRoute()
const search = useQuickSearch(180)
const inputRef = ref<HTMLInputElement>()
const overlayRef = ref<HTMLElement>()
const selectedIdx = ref(-1)
const { isCoarse } = useViewport()

// ── Empty-state data sources ────────────────────────────────────────────
const recent = useRecentSearches()
const recentItems = computed(() => recent.items.value)

// Reuse the same recommendations engine the home "For You" rail pages — just a
// tiny, availability-filtered strip. Fetches once when the spotlight first
// mounts (it's deferred-mounted on open) and is cached thereafter.
const forYouRecs = useQuery(spotlightForYouQuery())
const forYouItems = computed<SpotlightRecItem[]>(() => forYouRecs.data.value ?? [])

const isEmptyQuery = computed(() => !search.query.value.trim())

interface QuickAction { icon: string, label: string, title: string, to: string }

// App destinations for the empty state. Continue Watching / Up Next are home
// sections (no dedicated route, and the home page belongs to another surface),
// so they land on Home for now — see DEFERRED.md for deep-scroll anchors.
const quickActions: QuickAction[] = [
  { icon: 'play',    label: 'Continue', title: 'Continue Watching', to: '/' },
  { icon: 'queue',   label: 'Up Next',  title: 'Up Next',           to: '/' },
  { icon: 'film',    label: 'Movies',   title: 'Browse Movies A–Z', to: '/movies/all' },
  { icon: 'tv',      label: 'TV',       title: 'Browse TV A–Z',     to: '/tv/all' },
  { icon: 'music',   label: 'Music',    title: 'Music',             to: '/music' },
  { icon: 'shuffle', label: 'Roulette', title: 'Movie Roulette',    to: '/movies/roulette' },
  { icon: 'heart',   label: 'Loved',    title: 'Loved Movies',      to: '/movies/loved' },
  { icon: 'search',  label: 'Search',   title: 'Advanced Search',   to: '/search' },
]

// Flattened empty-state index offsets: quick actions [0..N), then recents,
// then for-you. Mirrors the order flatActions builds them in.
function recentIndex(j: number) { return quickActions.length + j }
function forYouIndex(k: number) { return quickActions.length + recentItems.value.length + k }

interface Section {
  key: 'movies' | 'tv' | 'music' | 'books' | 'albums' | 'tracks' | 'collections' | 'people'
  label: string
  tag: string
  icon: string
  thumbShape: 'poster' | 'square' | 'circle'
  bucket: { items: any[], total: number }
}

// UNIFIED order (desktop): titles first so a generic name ("Peter") surfaces
// the handful of movies/shows above the long tail of people.
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

// ── Unified keyboard-nav model ──────────────────────────────────────────
// One flat list of activation callbacks for whichever mode is showing. Arrow
// keys walk it, Enter fires the highlighted entry. In results mode the order
// matches `flatIndex(sIdx, iIdx)`; in the empty state it matches
// quickActions -> recents -> for-you (the recentIndex/forYouIndex helpers).
const flatActions = computed<Array<() => void>>(() => {
  if (!isEmptyQuery.value) {
    const acts: Array<() => void> = []
    for (const s of sections.value) {
      for (const item of s.bucket.items) acts.push(() => goToResult(s.key, item))
    }
    return acts
  }
  const acts: Array<() => void> = []
  for (const qa of quickActions) acts.push(() => runQuickAction(qa))
  for (const q of recentItems.value) acts.push(() => runRecent(q))
  for (const it of forYouItems.value) acts.push(() => runForYou(it))
  return acts
})

// The listbox always carries options in the empty state (quick actions), so
// aria-expanded tracks whether ANY option exists, not just search results.
const listboxExpanded = computed(() => flatActions.value.length > 0)

function flatIndex(sIdx: number, iIdx: number) {
  let n = 0
  for (let i = 0; i < sIdx; i++) {
    const s = sections.value[i]
    if (s) n += s.bucket.items.length
  }
  return n + iIdx
}

function move(delta: number) {
  const max = flatActions.value.length
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

function optionId(flat: number): string {
  return `spotlight-opt-${flat}`
}

const activeDescendantId = computed(() =>
  selectedIdx.value >= 0 ? optionId(selectedIdx.value) : undefined,
)

// Reset the highlight whenever the query text changes (mode switch or new
// keystroke) so the cursor never points at a row that just left the model.
watch(() => search.query.value, () => { selectedIdx.value = -1 })
// Clamp if the visible option count shrinks under the cursor (e.g. a recent
// removed, or recs resolving late).
watch(() => flatActions.value.length, (len) => { if (selectedIdx.value >= len) selectedIdx.value = -1 })

function onEnter() {
  if (selectedIdx.value >= 0) {
    flatActions.value[selectedIdx.value]?.()
    return
  }
  const q = search.query.value.trim()
  if (q) {
    recent.record(q)
    navigateTo(`/search?q=${encodeURIComponent(q)}`)
    close()
  }
}

// ── Empty-state actions ─────────────────────────────────────────────────
function runQuickAction(qa: QuickAction) {
  navigateTo(qa.to)
  close()
}

// Fill the field + let the debounce fire the live search. No navigation, no
// close — the results replace the empty state in place.
function runRecent(q: string) {
  search.query.value = q
  nextTick(() => inputRef.value?.focus())
}

function runForYou(it: SpotlightRecItem) {
  navigateTo(mediaUrl(it))
  close()
}

// Recorded onto the recents MRU when a query actually goes somewhere.
function onSeeAll() {
  recent.record(search.query.value)
  close()
}

// ── Match highlighting ──────────────────────────────────────────────────
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
  if (path) {
    recent.record(search.query.value)
    navigateTo(path)
  }
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
// Result/action rows are tabindex=-1 (driven by arrows, not Tab — combobox
// pattern), so this only cycles the input, close button, "Clear" and see-all
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

// Remembers whatever had focus before opening so it can be restored on close.
// The restore lives in onUnmounted (not the watch else-branch) because closing
// unmounts the overlay, and removing the still-focused input resets focus to
// <body> — doing the restore AFTER that removal avoids the browser clobbering
// our focus() call.
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

/* ── Input row ──────────────────────────────────────────────────────────
   Borderless bar; the ONLY line is the bottom hairline. Focus lifts that
   hairline gold (a glow, not a box). */
.search-input {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 17px 20px;
  border-bottom: 1px solid var(--hair-strong);
  transition: border-color 0.18s ease, box-shadow 0.18s ease;
}
.search-input:focus-within {
  border-bottom-color: color-mix(in srgb, var(--gold) 60%, var(--hair-strong));
  box-shadow:
    0 1px 0 0 color-mix(in srgb, var(--gold) 55%, transparent),
    0 10px 26px -14px color-mix(in srgb, var(--gold) 42%, transparent);
}
.si-glyph { color: rgb(var(--ink) / 0.45); flex-shrink: 0; transition: color 0.18s ease; }
.search-input:focus-within .si-glyph { color: color-mix(in srgb, var(--gold) 70%, var(--fg-2)); }
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
/* The designed focus treatment here is the hairline glow + gold caret, NOT the
   global a11y box ring — suppress that ring on THIS field only. Focus is never
   ambiguous: the field is the sole auto-focused target in the modal, and the
   glowing bottom rule is a clear, non-outline focus indicator. */
.si-field:focus,
.si-field:focus-visible { outline: none !important; }

/* "All libraries" scope + Esc are integrated affordances — flat, tinted, no
   hard outline (the old bordered chip is what made the row feel bolted-on). */
.si-scope {
  flex-shrink: 0;
  font: 550 10px var(--font-mono);
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-3);
  background: rgb(var(--ink) / 0.05);
  padding: 5px 10px;
  border-radius: 6px;
}
.si-esc {
  flex-shrink: 0;
  display: none;
  font: 600 10px var(--font-mono);
  letter-spacing: 0.08em;
  line-height: 1;
  color: var(--fg-3);
  background: rgb(var(--ink) / 0.05);
  border-radius: 6px;
  padding: 5px 8px;
}
/* Esc chip is a keyboard hint — only surface it where there's a keyboard to
   press (positive form, same heuristic as the hints footer / ⌘K chip). */
@media (hover: hover) and (pointer: fine) { .si-esc { display: inline-block; } }

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
  max-height: 66vh;
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

/* Mono section head — shared by results groups AND empty-state groups. */
.sr-group {
  display: flex;
  align-items: center;
  font: 600 9.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--fg-3);
  padding: 16px 14px 8px;
}
.sr-group .n { margin-left: auto; color: var(--fg-4); }
.sr-group-action {
  margin-left: auto;
  font: inherit;
  letter-spacing: 0.14em;
  color: var(--fg-4);
  background: transparent;
  border: 0;
  padding: 0;
  cursor: pointer;
  transition: color 0.12s ease;
}
.sr-group-action:hover { color: var(--gold); }

/* ── Empty state ────────────────────────────────────────────────────────*/
.es-group + .es-group { margin-top: 2px; }

/* Quick actions — icon tiles, 4 across. */
.qa-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
  padding: 2px 4px 6px;
}
.qa-tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 9px;
  padding: 16px 8px;
  border-radius: 12px;
  background: rgb(var(--ink) / 0.03);
  border: 1px solid transparent;
  cursor: pointer;
  color: var(--fg-1);
  transition: background 0.12s ease, border-color 0.12s ease, transform 0.12s ease;
}
.qa-tile:hover,
.qa-tile.sel { background: rgb(var(--ink) / 0.07); }
.qa-tile.sel {
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
  background: color-mix(in srgb, var(--gold) 8%, rgb(var(--ink) / 0.05));
}
.qa-ico {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border-radius: 11px;
  background: var(--gold-soft);
  color: var(--gold);
}
.qa-label {
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.01em;
  color: var(--fg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}

/* Recent searches — clock + query + hover-✕. */
.rs-row {
  position: relative;
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 9px 14px;
  border-radius: 10px;
  background: transparent;
  border: 0;
  text-align: left;
  cursor: pointer;
  color: var(--fg-1);
  transition: background 0.1s ease;
}
.rs-row:hover,
.rs-row.sel { background: rgb(var(--ink) / 0.06); }
.rs-row.sel { outline: 1px solid color-mix(in srgb, var(--gold) 45%, transparent); }
.rs-ico { color: var(--fg-4); flex-shrink: 0; }
.rs-text {
  flex: 1;
  min-width: 0;
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.rs-remove {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 6px;
  color: var(--fg-4);
  opacity: 0;
  transition: opacity 0.12s ease, color 0.12s ease, background 0.12s ease;
}
.rs-row:hover .rs-remove,
.rs-row.sel .rs-remove { opacity: 1; }
.rs-remove:hover { color: var(--fg-0); background: rgb(var(--ink) / 0.08); }

/* For You — compact poster strip. */
.fy-strip {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 10px;
  padding: 4px 6px 8px;
}
.fy-card {
  display: flex;
  flex-direction: column;
  gap: 7px;
  padding: 6px;
  border-radius: 10px;
  background: transparent;
  border: 1px solid transparent;
  cursor: pointer;
  text-align: left;
  transition: background 0.12s ease, border-color 0.12s ease;
}
.fy-card:hover,
.fy-card.sel { background: rgb(var(--ink) / 0.06); }
.fy-card.sel { border-color: color-mix(in srgb, var(--gold) 45%, transparent); }
.fy-thumb {
  aspect-ratio: 2 / 3;
  border-radius: 6px;
  overflow: hidden;
  background: var(--bg-2);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-4);
}
.fy-thumb :deep(img) { width: 100%; height: 100%; object-fit: cover; display: block; }
.fy-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-1);
  line-height: 1.3;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

/* ── Results ────────────────────────────────────────────────────────────*/
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

/* Hints — keyboard-capable machines only. Default hidden; the media query
   below reveals them where a physical keyboard is present (positive form of
   the old `pointer: coarse` hide). */
.search-hints {
  display: none;
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
@media (hover: hover) and (pointer: fine) { .search-hints { display: flex; } }

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
  .si-scope, .si-esc { display: none; }
  .search-close { display: inline-flex; margin-left: auto; }
  .search-body {
    max-height: none;
    flex: 1;
    padding: 4px 6px calc(env(safe-area-inset-bottom, 0px) + 24px);
  }
  .search-hints { display: none; }
  /* Empty-state responsive tweaks. */
  .qa-grid { gap: 6px; }
  .qa-tile { padding: 14px 6px; gap: 7px; }
  .qa-ico { width: 36px; height: 36px; }
  .fy-strip { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .rs-remove { opacity: 1; } /* no hover on touch — keep the ✕ reachable */
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
