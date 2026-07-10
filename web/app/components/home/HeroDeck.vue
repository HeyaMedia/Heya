<template>
  <section class="hero-deck">
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
      <HeroRoulette v-else-if="mode === 'roulette'" />
    </div>
  </section>
</template>

<script setup lang="ts">
import type { MediaItem, Movie } from '~~/shared/types'
import type { HeroItem, HeroPlayInfo } from '~/components/home/HeroA.vue'
import type { UpNextItem } from '~/components/home/UpNextRow.vue'
import type { RecentTVEntry } from '~/components/home/HeroNewIn.vue'

export type HeroMode = 'featured' | 'tonight' | 'new' | 'music' | 'roulette'

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
  tabs.push({ id: 'roulette', label: 'Roulette' })
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
.deck-tabs {
  position: absolute;
  top: 18px;
  right: 24px;
  z-index: 10;
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  border-radius: 999px;
  /* Theme-aware glass (same recipe as .surface): the pill sits over
     artwork in most hero modes but over the bare canvas in Roulette —
     a literal dark glass was unreadable on the light theme's paper. */
  background: color-mix(in oklab, var(--bg-2) 88%, transparent);
  border: 1px solid var(--border);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
.deck-tab {
  font-family: var(--font-mono);
  font-size: 10.5px;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--fg-2);
  padding: 5px 11px;
  border-radius: 999px;
  transition: color 0.15s, background 0.15s;
}
.deck-tab:hover { color: var(--fg-0); }
.deck-tab.active {
  color: var(--gold);
  background: color-mix(in srgb, var(--accent) 10%, transparent);
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
  .deck-tabs { right: 12px; top: 12px; }
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
  .deck-tabs {
    left: 12px;
    right: 12px;
    max-width: none;
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
