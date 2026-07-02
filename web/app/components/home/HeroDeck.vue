<template>
  <section class="hero-deck">
    <!-- Mode tabs: quiet mono rail, gold = active, star pins the default. -->
    <nav class="deck-tabs" v-if="visibleModes.length > 1 && mode !== 'game'">
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
      <HeroGame v-else-if="mode === 'game'" :posters="gamePosters" @exit="setMode((pinned as HeroMode) || 'featured')" />
    </div>
  </section>
</template>

<script setup lang="ts">
import type { MediaItem, Movie } from '~~/shared/types'
import type { HeroItem, HeroPlayInfo } from '~/components/home/HeroA.vue'
import type { UpNextItem } from '~/components/home/UpNextRow.vue'
import type { RecentTVEntry } from '~/components/home/HeroNewIn.vue'

export type HeroMode = 'featured' | 'tonight' | 'new' | 'music' | 'roulette' | 'game'

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
  mode.value = m
}

function togglePin() {
  pinned.value = pinned.value === mode.value ? '' : mode.value
  try { localStorage.setItem(LS_KEY, pinned.value) } catch { /* private mode */ }
  emit('pin', pinned.value)
}

// The pinned mode arrives twice: instantly from localStorage (pre-paint) and
// authoritatively from /api/me/settings (cross-device). Apply either as long
// as the user hasn't already clicked a tab this visit.
let userTouched = false
watch(() => props.pinnedMode, (m) => {
  if (m === undefined) return
  pinned.value = m
  if (!userTouched && m && m !== 'game') mode.value = m as HeroMode
}, { immediate: true })

const stopWatch = watch(mode, (_, old) => {
  if (old !== undefined) userTouched = true
})

// Poster pool for the game — whatever slides we have on hand.
const gamePosters = computed(() =>
  props.items.slice(0, 12).map(i => usePosterUrl(i.id)).filter((s): s is string => !!s))

// ↑↑↓↓←→←→BA — the hero remembers the old ways.
const KONAMI = ['ArrowUp', 'ArrowUp', 'ArrowDown', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'ArrowLeft', 'ArrowRight', 'b', 'a']
let progress = 0
function onKonami(e: KeyboardEvent) {
  if (mode.value === 'game') return
  const k = e.key.length === 1 ? e.key.toLowerCase() : e.key
  progress = k === KONAMI[progress] ? progress + 1 : (k === KONAMI[0] ? 1 : 0)
  if (progress === KONAMI.length) {
    progress = 0
    mode.value = 'game'
  }
}

onMounted(() => {
  try {
    const ls = localStorage.getItem(LS_KEY)
    if (ls && !userTouched && ls !== 'game') { pinned.value = ls; mode.value = ls as HeroMode }
  } catch { /* private mode */ }
  window.addEventListener('keydown', onKonami)
})
onUnmounted(() => {
  window.removeEventListener('keydown', onKonami)
  stopWatch()
})
</script>

<style scoped>
.hero-deck {
  position: relative;
  height: 480px;
  overflow: hidden;
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
  background: rgba(7, 7, 10, 0.55);
  border: 1px solid var(--border);
  backdrop-filter: blur(12px);
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
  background: rgba(230, 185, 74, 0.1);
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
</style>
