<script setup lang="ts">
// Heya 2.0 season-page episode row (heya2.css `.ep`). A hairline-ruled
// ledger row: ghost tabular E-number · 16:9 still · body (title/meta/synopsis)
// · side actions (watched check + play). Navigation is via real NuxtLinks
// (the still + the title) so middle-click / open-in-new-tab work and clicks
// aren't swallowed (reka row-click gotcha); the side controls are real
// <button>s, never nested inside an anchor. The PAGE wraps the whole row in an
// AppContextMenu for the right-click / long-press menu.
const props = withDefaults(defineProps<{
  /** Episode route (NuxtLink target for the still + title). */
  to: string
  stillUrl: string
  episodeNumber: number
  title: string
  airDate?: string
  runtimeMinutes?: number
  rating?: string | number
  overview?: string
  watched?: boolean
  hasFile?: boolean
  /** In-progress fill for the still bottom bar (0 hides it). */
  progressPct?: number
  /** Minutes remaining, rendered in the meta line when in progress. */
  remainingMinutes?: number
}>(), {
  watched: false,
  hasFile: false,
  progressPct: 0,
})

const emit = defineEmits<{
  play: []
  toggleWatched: []
}>()

const numLabel = computed(() => `E${String(props.episodeNumber).padStart(2, '0')}`)

const ratingStr = computed(() => {
  const r = props.rating
  const n = typeof r === 'number' ? r : parseFloat(String(r ?? ''))
  return (!Number.isFinite(n) || n <= 0) ? '' : n.toFixed(1)
})

const inProgress = computed(() => !props.watched && (props.progressPct ?? 0) > 0)

const statusText = computed(() => {
  if (props.watched) return 'Watched'
  if (inProgress.value && props.remainingMinutes) return `${props.remainingMinutes}m left`
  return ''
})

function hideBroken(e: Event | string) {
  if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none'
}
</script>

<template>
  <div class="ep" :class="{ 'is-watched': watched }">
    <div class="num"><b>{{ numLabel }}</b></div>

    <NuxtLink :to="to" class="still" :aria-label="`View ${title}`">
      <LoadingImage :src="stillUrl" :width="500" :quality="80" :alt="title" @error="hideBroken" />
      <span v-if="inProgress" class="prog"><i :style="{ width: Math.min(100, progressPct) + '%' }" /></span>
    </NuxtLink>

    <div class="body">
      <h3><NuxtLink :to="to">{{ title }}</NuxtLink></h3>
      <div class="meta">
        <span v-if="airDate">{{ formatDate(airDate) }}</span>
        <span v-if="runtimeMinutes">{{ runtimeMinutes }}m</span>
        <span v-if="ratingStr" class="rating"><Icon name="star" :size="10" /> {{ ratingStr }}</span>
        <span v-if="statusText" class="status">{{ statusText }}</span>
      </div>
      <p v-if="overview">{{ overview }}</p>
    </div>

    <div class="side">
      <button
        class="chk"
        :class="{ on: watched }"
        :aria-label="watched ? 'Mark as unwatched' : 'Mark as watched'"
        :aria-pressed="watched"
        :title="watched ? 'Mark as unwatched' : 'Mark as watched'"
        @click.stop.prevent="emit('toggleWatched')"
      >
        <Icon name="check" :size="14" />
      </button>
      <button
        class="go"
        :disabled="!hasFile"
        :aria-label="!hasFile ? 'No file' : (inProgress ? 'Resume' : 'Play')"
        :title="!hasFile ? 'No file' : (inProgress ? 'Resume' : 'Play')"
        @click.stop.prevent="hasFile && emit('play')"
      >
        <Icon name="play" :size="15" />
      </button>
    </div>
  </div>
</template>

<style scoped>
/* Rows sit BELOW the hero on the themed ambient/flat background, so they use
   themed --ink (works dark/oled/light). Only the still's progress track is
   painted directly over artwork, so that one stays literal. */
.ep {
  display: grid;
  grid-template-columns: 84px 218px minmax(0, 1fr) auto;
  gap: 26px;
  align-items: center;
  padding: 18px 0;
  border-bottom: 1px solid var(--hair);
  transition: background 0.15s;
}
.ep:hover { background: rgb(var(--ink) / 0.025); }

.num {
  font: 700 30px var(--font-mono);
  letter-spacing: -0.05em;
  color: rgb(var(--ink) / 0.28);
  font-variant-numeric: tabular-nums;
  text-align: right;
  padding-right: 4px;
  user-select: none;
}
.num b { color: var(--tone); font-weight: 700; }
.ep.is-watched .num b { color: rgb(var(--ink) / 0.28); }

.still {
  position: relative;
  display: block;
  aspect-ratio: 16/9;
  border-radius: 7px;
  overflow: hidden;
  /* Placeholder lives on the container so a missing still (broken img → hidden)
     keeps a neutral 16:9 tile instead of collapsing the grid column. */
  background: var(--bg-2);
  box-shadow: 0 0 0 1px rgb(var(--ink) / 0.09);
}
.still :deep(img) {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: opacity 0.2s;
}
.ep.is-watched .still :deep(img) { opacity: 0.55; }
.prog {
  position: absolute;
  left: 0; right: 0; bottom: 0;
  height: 3px;
  border-radius: 0 0 7px 7px;
  background: rgb(0 0 0 / 0.55);
  overflow: hidden;
}
.prog i { display: block; height: 100%; background: var(--tone); }

.body { min-width: 0; }
.body h3 {
  font-size: 15.5px;
  font-weight: 650;
  letter-spacing: -0.01em;
  line-height: 1.25;
}
.body h3 a { color: inherit; text-decoration: none; transition: color 0.15s; }
.body h3 a:hover { color: var(--tone); }
.ep.is-watched .body h3 a { color: rgb(var(--ink) / 0.6); }

.meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 6px;
  margin: 5px 0 8px;
  font: 500 11px var(--font-mono);
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.5);
}
.meta > *:not(:first-child)::before {
  content: "\00b7";
  margin-right: 6px;
  color: rgb(var(--ink) / 0.3);
}
.meta .rating { display: inline-flex; align-items: center; gap: 3px; color: var(--tone); }
.meta .status { color: var(--tone); }

.body p {
  font-size: 13.5px;
  color: rgb(var(--ink) / 0.62);
  line-height: 1.55;
  max-width: 62ch;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.side { display: flex; align-items: center; gap: 8px; }
.chk {
  width: 30px; height: 30px;
  border-radius: 50%;
  border: 1px solid rgb(var(--ink) / 0.2);
  background: transparent;
  display: flex; align-items: center; justify-content: center;
  color: rgb(var(--ink) / 0.35);
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s, color 0.15s, box-shadow 0.15s;
}
.chk:hover { border-color: rgb(var(--ink) / 0.4); color: rgb(var(--ink) / 0.7); }
.ep.is-watched .chk {
  background: rgb(var(--tone-rgb) / 0.14);
  border-color: rgb(var(--tone-rgb) / 0.4);
  color: var(--tone);
  box-shadow: 0 0 12px rgb(var(--tone-rgb) / 0.25);
}

.go {
  width: 38px; height: 38px;
  border-radius: 50%;
  border: 1px solid rgb(var(--ink) / 0.22);
  background: transparent;
  display: flex; align-items: center; justify-content: center;
  color: rgb(var(--ink) / 0.85);
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s, color 0.15s, box-shadow 0.15s;
}
.go:hover {
  background: var(--tone);
  border-color: var(--tone);
  color: var(--tone-ink, #0a0c10);
  box-shadow: 0 0 18px rgb(var(--tone-rgb) / 0.45);
}
.go[disabled] { opacity: 0.3; cursor: not-allowed; }
.go[disabled]:hover {
  background: transparent;
  border-color: rgb(var(--ink) / 0.22);
  color: rgb(var(--ink) / 0.85);
  box-shadow: none;
}

/* ═══ RESPONSIVE (heya2.css ≤1020 → app 960; ≤560 → app 720) ═══ */
@media (max-width: 960px) {
  .ep { grid-template-columns: 44px 150px minmax(0, 1fr); gap: 16px; }
  .num { font-size: 20px; }
  .side { display: none; }
}
@media (max-width: 720px) {
  .ep {
    grid-template-columns: 116px minmax(0, 1fr);
    gap: 13px;
    padding: 14px 0;
    align-items: start;
  }
  .num { display: none; }
  .body h3 { font-size: 14px; }
  .body p { font-size: 12.5px; }
}
</style>
