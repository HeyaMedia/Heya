<template>
  <section class="hero-deck">
    <!-- Top-right cluster: modes can teleport their own controls (HeroA's
         slide navigator) into #hero-deck-aux, sitting left of the tabs. -->
    <div class="deck-topright">
      <span id="hero-deck-aux" class="deck-aux" />
      <!-- Mode tabs: quiet mono rail, gold = active, star pins the default. -->
      <nav class="deck-tabs" v-if="visibleModes.length > 1">
      <button
        v-for="m in visibleModes"
        :key="m.id"
        class="deck-tab"
        :class="{ active: mode === m.id }"
        @click="setMode(m.id)"
      >{{ m.label }}</button>
      <button
        class="deck-pin"
        :class="{ pinned: pinned === mode }"
        :title="pinned === mode ? 'Unpin — open Featured by default' : 'Open this view by default'"
        @click="togglePin"
      >
        <Icon name="star" :weight="pinned === mode ? 'fill' : 'regular'" :size="13" />
      </button>
      </nav>
    </div>

    <div class="deck-body">
      <HeroA
        v-if="mode === 'featured'"
        :items="items"
        :movies="movies"
        :play-info="playInfo"
        :trailers="trailers"
        @play="(i) => $emit('play', i)"
      />
      <HeroTonight
        v-else-if="mode === 'tonight'"
        :items="upNextItems"
        @play="(i) => $emit('playUpNext', i)"
      />
      <HeroNewIn
        v-else-if="mode === 'new'"
        :tv="tvEntries"
        :albums="albums"
        :artists="artists"
      />
      <HeroMusic v-else-if="mode === 'music'" :albums="albums" :artists="artists" />
    </div>
  </section>
</template>

<script setup lang="ts">
import type { MediaItem, Movie } from '~~/shared/types'
import type { HeroItem, HeroPlayInfo } from '~/components/home/HeroA.vue'
import type { UpNextItem } from '~/types/home'
import type { RecentTVEntry } from '~/components/home/HeroNewIn.vue'

export type HeroMode = 'featured' | 'tonight' | 'new' | 'music'

const props = defineProps<{
  items: HeroItem[]
  movies?: Record<number, Movie>
  playInfo?: Record<number, HeroPlayInfo>
  trailers?: Record<number, number>
  upNextItems: UpNextItem[]
  tvEntries: RecentTVEntry[]
  albums: MediaItem[]
  artists: MediaItem[]
  // Server-persisted pinned mode ('' = featured). Deck mirrors it to
  // localStorage so the right mode paints before /api/me/settings lands.
  pinnedMode?: string
}>()

const emit = defineEmits<{
  play: [item: MediaItem]
  playUpNext: [item: UpNextItem]
  pin: [mode: string]
}>()

const LS_KEY = 'heya-hero-mode'

const mode = ref<HeroMode>('featured')
const pinned = ref<string>('')

const visibleModes = computed(() => {
  const tabs: { id: HeroMode; label: string }[] = [{ id: 'featured', label: 'Featured' }]
  if (props.upNextItems.length) tabs.push({ id: 'tonight', label: 'Tonight' })
  if (props.tvEntries.length || props.albums.length) tabs.push({ id: 'new', label: 'New' })
  if (props.albums.length) tabs.push({ id: 'music', label: 'Music' })
  return tabs
})

function setMode(m: HeroMode) {
  userTouched = true
  mode.value = m
}

function togglePin() {
  pinned.value = pinned.value === mode.value ? '' : mode.value
  try { localStorage.setItem(LS_KEY, pinned.value) } catch { /* private mode */ }
  emit('pin', pinned.value)
}

// The pinned mode arrives twice: instantly from localStorage (pre-paint) and
// authoritatively from /api/me/settings (cross-device). Apply either as long
// as the user hasn't already clicked a tab this visit — but only when the
// mode's tab is actually visible: a pinned "Tonight" with an empty queue
// must not open onto a blank hero. Data loads async, so this re-runs as
// tabs appear and applies the pin the moment its data lands.
let userTouched = false
const visibleIds = computed(() => new Set<string>(visibleModes.value.map(m => m.id)))

watch(() => props.pinnedMode, (m) => {
  if (m === undefined) return
  pinned.value = m
  // Reconcile the device mirror with the server (pin changed elsewhere).
  try { localStorage.setItem(LS_KEY, m) } catch { /* private mode */ }
}, { immediate: true })

watchEffect(() => {
  if (userTouched) return
  const want = pinned.value
  if (want && visibleIds.value.has(want)) mode.value = want as HeroMode
})

// Never sit on a mode whose tab has disappeared (its data emptied out or a
// stale pin points at something this library doesn't have) — fall back to
// Featured instead of rendering an empty shell.
watch(visibleIds, (ids) => {
  if (!ids.has(mode.value)) mode.value = 'featured'
})

onMounted(() => {
  try {
    // Seed from the device mirror; the guarded watchEffect above applies it
    // once (and only when) the mode's tab is visible.
    const ls = localStorage.getItem(LS_KEY)
    if (ls && !pinned.value) pinned.value = ls
  } catch { /* private mode */ }
})
</script>

<style scoped>
.hero-deck {
  position: relative;
  height: 480px;
  /* No overflow clipping here — the poster's drop shadow bleeds past the
     deck bottom by design. Image/trailer clipping is handled by the modes'
     own .hero-bg (overflow: hidden, heya.css). */
  overflow: visible;
}
.deck-body {
  position: absolute;
  inset: 0;
}
.deck-topright {
  position: absolute;
  /* The deck rides flush under the fixed glass topbar (`.hero-flush` on the
     page root), so top:18px would tuck the tabs + slide navigator behind the
     bar. Drop the whole cluster clear of it. */
  top: calc(var(--topbar-h) + 14px);
  right: var(--pad-fluid);
  z-index: 10;
  display: flex;
  align-items: center;
  gap: 10px;
}
.deck-aux { display: flex; align-items: center; }
.deck-tabs {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  border-radius: 999px;
  /* Theme-aware glass (same recipe as .surface): the pill sits over
     artwork, but a literal dark glass was unreadable on the light
     theme's paper — mix from the theme's own surface color instead. */
  background: color-mix(in oklab, var(--bg-2) 84%, transparent);
  border: 1px solid var(--hair-strong);
  box-shadow: 0 8px 26px -14px rgb(0 0 0 / 0.7);
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
}
.deck-tab {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.55);
  padding: 6px 12px;
  border-radius: 999px;
  transition: color 0.15s, background 0.15s;
}
.deck-tab:hover { color: rgb(var(--ink) / 0.9); }
.deck-tab.active {
  color: var(--gold);
  background: rgb(227 179 65 / 0.12);
}
.deck-pin {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  margin-left: 2px;
  border-radius: 50%;
  color: var(--fg-3);
  transition: color 0.15s;
}
.deck-pin:hover { color: var(--gold-bright); }
.deck-pin.pinned { color: var(--gold); }
@media (max-width: 900px) {
  /* Still clear the fixed glass topbar (home is hero-flush). */
  .deck-topright { right: 12px; top: calc(var(--topbar-h) + 10px); }
  .deck-tab { padding: 5px 8px; }
}
/* Phone (W3a): the desktop 480px band is already ~57vh on a 390x844 phone,
   but content inside HeroA/etc. was vertically *centered* rather than
   bottom-anchored, so most of that height read as dead black space above the
   rails. Re-express the height as a capped vh range (the sub-heroes'
   bottom-alignment does the rest) and let the mode-chip row scroll
   horizontally instead of silently overflowing — five labels + the pin
   button can exceed a 390px screen once "Tonight" joins the set. */
@media (max-width: 720px) {
  .hero-deck {
    height: 64vh;
    height: 64dvh;
    min-height: 440px;
    max-height: 580px;
  }
  .deck-topright {
    left: 12px;
    right: 12px;
    max-width: none;
  }
  /* Phone: the aux slot (slide navigator) yields — tabs own the row. */
  .deck-aux { display: none; }
  .deck-tabs {
    flex: 1;
    min-width: 0;
    overflow-x: auto;
    overflow-y: hidden;
    -webkit-overflow-scrolling: touch;
    scrollbar-width: none;
    justify-content: flex-start;
  }
  .deck-tabs::-webkit-scrollbar { display: none; }
  .deck-tab, .deck-pin { flex-shrink: 0; }
}
</style>
