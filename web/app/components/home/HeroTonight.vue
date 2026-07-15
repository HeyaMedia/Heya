<template>
  <section class="hero-tonight">
    <div class="tonight-bg" :class="{ 'ambient-extended': ambientEnabled }">
      <LoadingImage
        v-if="bgUrl"
        :src="bgUrl"
        :width="1920"
        :quality="70"
        alt=""
        class="tonight-bg-img"
        @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
      />
      <div class="tonight-bg-gradient" />
    </div>

    <div class="tonight-inner">
      <div class="tonight-lead">
        <div class="tonight-eyebrow">Up next</div>
        <h1 class="tonight-title">Tonight</h1>
        <p class="tonight-sum">
          {{ items.length }} episode{{ items.length === 1 ? '' : 's' }} waiting<span v-if="totalMinutes"> · ≈ {{ fmtTotal }}</span>
        </p>
        <button v-if="items[0]" class="btn-play" @click="$emit('play', items[0])">
          <span class="tri" />
          Start with {{ items[0].title }}
        </button>
      </div>

      <div class="tonight-list">
        <button
          v-for="it in items.slice(0, 4)"
          :key="it.id"
          class="tonight-card"
          @click="$emit('play', it)"
        >
          <div class="tonight-still">
            <LoadingImage
              :src="stillUrl(it)"
              :width="480"
              alt=""
              class="tonight-still-img"
              @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
            />
            <div class="tonight-still-play"><Icon name="play" :size="18" /></div>
          </div>
          <div class="tonight-card-info">
            <div class="tonight-card-show">{{ it.title }}</div>
            <div class="tonight-card-ep">{{ it.episode_label }}</div>
            <div v-if="it.runtime_minutes" class="tonight-card-run">{{ it.runtime_minutes }}m</div>
          </div>
        </button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
// "Tonight" — the up-next queue as a planner: what's waiting, how long it
// runs, one click to start. Same data the Up Next rail uses, framed as a
// session instead of a list.
import type { UpNextItem } from '~/types/home'

const props = defineProps<{ items: UpNextItem[] }>()
defineEmits<{ play: [item: UpNextItem] }>()

const bgUrl = computed(() => props.items[0] ? useBackdropUrl(props.items[0]) : null)

// Ambient extension: with the ambient background on, the top item's backdrop
// becomes the full-page layer — the local `.tonight-bg-img` hides via
// .ambient-extended and the AmbientBackdrop layer follows the queue through
// this watcher.
const { ambientEnabled } = useAppearance()
const background = useBackground()
watch([bgUrl, ambientEnabled], ([url, on]) => {
  if (on && url) background.set(url)
  else background.clear()
}, { immediate: true })

const totalMinutes = computed(() =>
  props.items.slice(0, 4).reduce((sum, it) => sum + (it.runtime_minutes || 0), 0))

const fmtTotal = computed(() => {
  const m = totalMinutes.value
  return m >= 60 ? `${Math.floor(m / 60)}h ${m % 60 ? `${m % 60}m` : ''}`.trim() : `${m}m`
})

function stillUrl(it: UpNextItem) {
  const s = String(it.season_number).padStart(2, '0')
  const e = String(it.episode_number).padStart(2, '0')
  return `/api/media/${useMediaImageKey(it)}/image/still?label=s${s}e${e}`
}
</script>

<style scoped>
.hero-tonight { position: relative; height: 100%; }
.tonight-bg { position: absolute; inset: 0; }
.tonight-bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  filter: blur(18px) brightness(0.5);
  transform: scale(1.08);
}
.tonight-bg-gradient {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 55%, transparent) 55%, color-mix(in srgb, var(--bg-1) 25%, transparent) 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 45%);
}
/* Ambient extension: the AmbientBackdrop layer shows this item's backdrop
   full-page (see the background watcher), so the local copy hides — its
   different crop would seam at the hero edges — and the fade softens so
   the artwork continues past the hero bottom instead of ending at solid
   canvas. */
.tonight-bg.ambient-extended .tonight-bg-img { display: none; }
.tonight-bg.ambient-extended .tonight-bg-gradient { display: none; }
.tonight-inner {
  position: relative;
  z-index: 2;
  display: grid;
  grid-template-columns: minmax(280px, 1fr) minmax(0, 640px);
  align-items: center;
  gap: 48px;
  height: 100%;
  /* Top padding clears the glass topbar; the grid is vertically centred. */
  padding: 84px var(--pad-fluid) 44px;
  max-width: 1240px;
}
.tonight-eyebrow {
  font-family: var(--font-mono);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
  margin-bottom: 12px;
  text-shadow: 0 0 12px rgb(0 0 0 / 0.5);
}
.tonight-title {
  font-family: var(--font-display);
  font-size: clamp(2.6rem, 5vw, 3.4rem);
  font-weight: 800;
  font-variation-settings: "wdth" 115;
  letter-spacing: -0.022em;
  line-height: 0.99;
  margin: 0 0 10px;
  text-shadow: 0 2px 30px rgb(0 0 0 / 0.45);
}
.tonight-sum {
  color: var(--fg-1);
  font-size: 14px;
  margin: 0 0 24px;
}

/* tone-glow primary (heya2.css .btn-play) */
.btn-play {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 13px 24px 13px 20px;
  border: 0;
  border-radius: 999px;
  cursor: pointer;
  background: var(--tone);
  color: var(--tone-ink, #0a0c10);
  font: 650 14px var(--font-sans);
  letter-spacing: 0.01em;
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.45),
    0 0 24px rgb(var(--tone-rgb) / 0.4),
    6px 10px 36px -8px rgb(var(--tone-rgb) / 0.75);
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.btn-play:hover {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 40px rgb(var(--tone-rgb) / 0.6),
    8px 14px 48px -8px rgb(var(--tone-rgb) / 0.9);
}
.btn-play .tri {
  width: 0; height: 0;
  border-left: 11px solid var(--tone-ink, #0a0c10);
  border-top: 7px solid transparent;
  border-bottom: 7px solid transparent;
}
.tonight-list {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}
.tonight-card {
  display: flex;
  gap: 12px;
  align-items: center;
  text-align: left;
  padding: 10px;
  border-radius: var(--r-md);
  background: rgba(7, 7, 10, 0.5); /* on artwork — stays literal */
  border: 1px solid var(--border);
  transition: background 0.15s, border-color 0.15s, transform 0.15s;
}
.tonight-card:hover {
  background: rgba(19, 19, 24, 0.75); /* on artwork — stays literal */
  border-color: var(--border-strong);
  transform: translateY(-1px);
}
.tonight-still {
  position: relative;
  width: 128px;
  aspect-ratio: 16 / 9;
  border-radius: var(--r-sm);
  overflow: hidden;
  background: var(--bg-3);
  flex-shrink: 0;
}
.tonight-still-img { width: 100%; height: 100%; object-fit: cover; }
.tonight-still-play {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-0);
  background: rgba(0, 0, 0, 0.35); /* on artwork — stays literal */
  opacity: 0;
  transition: opacity 0.15s;
}
.tonight-card:hover .tonight-still-play { opacity: 1; }
.tonight-card-info { min-width: 0; }
.tonight-card-show {
  font-weight: 600;
  font-size: 14px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tonight-card-ep {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-2);
  margin-top: 3px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tonight-card-run {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
  margin-top: 3px;
}
@media (max-width: 900px) {
  .tonight-inner { grid-template-columns: 1fr; gap: 20px; padding: 84px var(--pad-fluid) 28px; align-content: center; }
  .tonight-title { font-size: clamp(2rem, 8vw, 2.6rem); }
  .tonight-list { grid-template-columns: 1fr; gap: 10px; }
  .tonight-card:nth-child(n+3) { display: none; }
}
</style>
