<template>
  <section class="hero-tonight">
    <div class="tonight-bg">
      <NuxtImg
        v-if="bgUrl"
        :src="bgUrl"
        :width="1920"
        :quality="70"
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
        <button v-if="items[0]" class="btn btn-primary" @click="$emit('play', items[0])">
          <Icon name="play" :size="16" />
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
            <NuxtImg
              :src="stillUrl(it)"
              :width="480"
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
import type { UpNextItem } from '~/components/home/UpNextRow.vue'

const props = defineProps<{ items: UpNextItem[] }>()
defineEmits<{ play: [item: UpNextItem] }>()

const bgUrl = computed(() => props.items[0] ? useBackdropUrl(props.items[0]) : null)

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
.tonight-inner {
  position: relative;
  z-index: 2;
  display: grid;
  grid-template-columns: minmax(280px, 1fr) minmax(0, 640px);
  align-items: center;
  gap: 48px;
  height: 100%;
  padding: 48px 40px;
  max-width: 1240px;
}
.tonight-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold);
  margin-bottom: 10px;
}
.tonight-title {
  font-size: 52px;
  font-weight: 600;
  letter-spacing: -0.025em;
  margin: 0 0 8px;
}
.tonight-sum {
  color: var(--fg-1);
  font-size: 15px;
  margin: 0 0 24px;
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
  .tonight-inner { grid-template-columns: 1fr; gap: 20px; padding: 24px 20px; align-content: center; }
  .tonight-title { font-size: 38px; }
  .tonight-list { grid-template-columns: 1fr; gap: 10px; }
  .tonight-card:nth-child(n+3) { display: none; }
}
</style>
