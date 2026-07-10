<script setup lang="ts">
const props = defineProps<{
  stillUrl: string
  code: string
  title: string
  airDate?: string
  runtimeMinutes?: number
  rating?: string | number
  overview?: string
  watched?: boolean
  hasFile?: boolean
  progressPct?: number
  badge?: string
}>()

const emit = defineEmits<{
  play: []
  toggleWatched: []
}>()

</script>

<template>
  <div class="epc" :class="{ 'epc-watched': watched }">
    <div class="epc-still" @click.prevent="hasFile ? emit('play') : undefined">
      <NuxtImg :src="stillUrl" :width="500" :quality="80" @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }" />
      <div class="epc-gradient" />

      <button v-if="typeof watched === 'boolean'" class="epc-check" :class="{ active: watched }" @click.prevent.stop="emit('toggleWatched')" title="Toggle watched">
        <Icon name="check" :size="12" />
      </button>

      <div v-if="hasFile" class="epc-play-wrap">
        <div class="epc-play"><Icon name="play" :size="18" /></div>
      </div>

      <div v-if="badge" class="epc-badge">{{ badge }}</div>

      <div class="epc-info-overlay">
        <div class="epc-code">{{ code }}</div>
        <div class="epc-title">{{ title }}</div>
        <div class="epc-meta">
          <span v-if="airDate">{{ formatDate(airDate) }}</span>
          <span v-if="runtimeMinutes" class="epc-meta-sep">{{ runtimeMinutes }}m</span>
          <span v-if="rating" class="epc-meta-sep epc-rating"><Icon name="star" :size="9" /> {{ parseFloat(String(rating)).toFixed(1) }}</span>
        </div>
      </div>

      <div v-if="progressPct && progressPct > 0 && !watched" class="epc-progress">
        <div class="epc-progress-fill" :style="{ width: progressPct + '%' }" />
      </div>
    </div>

    <div v-if="overview" class="epc-overview">{{ overview }}</div>
  </div>
</template>

<style scoped>
/* This whole card is a still-image tile — every rgba(0,0,0,*) / rgba(255,255,255,*)
   below (gradient scrim, check/play buttons, badge, title/meta text, progress
   track) is painted directly over the episode still artwork, so they
   deliberately stay literal rather than switching to --ink/--shade. */
.epc {
  border-radius: var(--r-md);
  overflow: visible;
  display: flex; flex-direction: column;
  height: 100%;
}
.epc-watched { opacity: 0.55; }
.epc-watched:hover { opacity: 1; }

.epc-still {
  position: relative;
  aspect-ratio: 16/9;
  background: var(--bg-3);
  cursor: pointer;
  overflow: hidden;
  border-radius: var(--r-md) var(--r-md) 0 0;
}
.epc:not(:has(.epc-overview)) .epc-still { border-radius: var(--r-md); }
.epc-still img { width: 100%; height: 100%; object-fit: cover; display: block; }

.epc-gradient {
  position: absolute; inset: 0;
  background: linear-gradient(0deg, rgba(0,0,0,0.85) 0%, rgba(0,0,0,0.2) 45%, transparent 70%);
  pointer-events: none;
}

.epc-check {
  position: absolute; top: 8px; right: 8px; z-index: 3;
  width: 26px; height: 26px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.5); backdrop-filter: blur(4px);
  border: 1.5px solid rgba(255,255,255,0.15);
  color: rgba(255,255,255,0.35);
  transition: all 0.15s; opacity: 0;
}
.epc:hover .epc-check, .epc-check.active { opacity: 1; }
.epc-check:hover { background: rgba(0,0,0,0.7); color: var(--fg-0); border-color: rgba(255,255,255,0.3); }
.epc-check.active { background: var(--good); border-color: var(--good); color: #fff; }

.epc-play-wrap {
  position: absolute; inset: 0; z-index: 2;
  display: flex; align-items: center; justify-content: center;
  opacity: 0; transition: opacity 0.2s;
}
.epc:hover .epc-play-wrap { opacity: 1; }
.epc-play {
  width: 44px; height: 44px; border-radius: 50%;
  background: rgba(255,255,255,0.12); backdrop-filter: blur(8px);
  border: 1px solid rgba(255,255,255,0.15);
  display: flex; align-items: center; justify-content: center; color: #fff;
  transition: transform 0.2s, background 0.2s;
}
.epc-play:hover { transform: scale(1.1); background: rgba(255,255,255,0.2); }

.epc-badge {
  position: absolute; top: 8px; left: 8px; z-index: 3;
  display: inline-flex; align-items: center; gap: 3px;
  padding: 2px 8px; border-radius: 999px;
  background: rgba(0,0,0,0.55); backdrop-filter: blur(4px);
  font-size: 9px; font-weight: 700; font-family: var(--font-mono);
  color: rgba(255,255,255,0.7); text-transform: uppercase; letter-spacing: 0.06em;
}

.epc-info-overlay {
  position: absolute; bottom: 0; left: 0; right: 0; z-index: 2;
  padding: 10px 12px; pointer-events: none;
}
.epc-code {
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  letter-spacing: 0.08em; color: var(--gold); margin-bottom: 2px;
}
.epc-title {
  font-size: 14px; font-weight: 600; line-height: 1.25; color: #fff;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.epc-meta {
  display: flex; align-items: center; gap: 4px;
  font-size: 11px; color: rgba(255,255,255,0.5); margin-top: 3px;
}
.epc-meta-sep::before { content: '\00b7'; margin-right: 4px; }
.epc-rating { color: var(--gold); display: inline-flex; align-items: center; gap: 2px; }

.epc-progress {
  position: absolute; bottom: 0; left: 0; right: 0; height: 3px; z-index: 3;
  background: rgba(255,255,255,0.1);
}
.epc-progress-fill {
  height: 100%; background: var(--gold); border-radius: 0 2px 2px 0;
  transition: width 0.3s ease;
}

.epc-overview {
  font-size: 12px; color: var(--fg-2); line-height: 1.55; margin: 0;
  padding: 10px 12px 12px;
  background: var(--bg-2); border: 1px solid var(--border); border-top: 0;
  border-radius: 0 0 var(--r-md) var(--r-md);
  flex: 1; min-height: 0;
}

/* Touch: the checkmark and play overlay are hover-revealed on desktop — on a
   touch device there's no hover, so surface both permanently instead of
   leaving them unreachable. Keyed on pointer capability (docs/ui.md
   "Responsive conventions"), not viewport width, since this card also
   renders at desktop width in narrow layouts. */
@media (pointer: coarse) {
  /* `transition: none` isn't just cosmetic: headless Chrome under CDP touch
     emulation (no active compositor pump) can freeze an opacity transition
     at its pre-change value indefinitely — verified empirically via Heya
     Eye. A real device's render loop would finish the 150ms transition
     regardless, but instant removes any doubt for a permanently-on state. */
  .epc-check { opacity: 1; transition: none; }
  .epc-play-wrap { opacity: 1; transition: none; }
}
</style>
