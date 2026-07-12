<template>
  <article class="rs-card card-tile">
    <div class="rs-art" :class="{ 'rs-art-fallback': !station.favicon }">
      <NuxtImg
        v-if="station.favicon"
        :src="station.favicon"
        :alt="station.name"
        loading="lazy"
        @error="imgError = true"
        v-show="!imgError"
      />
      <Icon v-if="!station.favicon || imgError" name="radio" :size="38" />
      <button
        class="rs-play"
        :disabled="loading"
        @click="$emit('play', station)"
        :title="`Play ${station.name}`"
      >
        <Icon :name="loading ? 'spinner' : 'play'" :size="20" :class="{ 'rs-spin': loading }" />
      </button>
      <button
        class="rs-fav"
        :class="{ active: favorited }"
        :aria-pressed="favorited"
        @click.stop="$emit('toggle-favorite', station)"
        :title="favorited ? 'Remove from favorites' : 'Save to favorites'"
      >
        <Icon :name="favorited ? 'heartfill' : 'heart'" :size="14" />
      </button>
    </div>
    <div class="rs-meta">
      <div class="rs-name" :title="station.name">{{ station.name }}</div>
      <div class="rs-sub">
        <span v-if="station.country">{{ station.country }}</span>
        <span v-if="station.country && station.bitrate" class="dot">·</span>
        <span v-if="station.bitrate" class="mono">{{ station.bitrate }}k</span>
      </div>
      <div v-if="topTags" class="rs-tags">{{ topTags }}</div>
    </div>
  </article>
</template>

<script setup lang="ts">
import type { RadioStationView } from '~/composables/useRadio'

const props = defineProps<{
  station: RadioStationView
  favorited?: boolean
  loading?: boolean
}>()

defineEmits<{
  play: [station: RadioStationView]
  'toggle-favorite': [station: RadioStationView]
}>()

const imgError = ref(false)

// Tags come back as a single comma-separated string. Take the first 3 for
// the chip-style readout — anything more bloats the card and isn't useful
// since the bigger filtering happens via the tag picker on the Radio page.
const topTags = computed(() => {
  if (!props.station.tags) return ''
  return props.station.tags
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
    .slice(0, 3)
    .join(' · ')
})
</script>

<style scoped>
.rs-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  text-decoration: none;
  color: inherit;
}
.rs-art {
  aspect-ratio: 1 / 1;
  border-radius: var(--r-md);
  background: var(--bg-3);
  position: relative;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  /* card-tile's hover lift applies to the root; its shadow swap targets
     .poster children only, so mirror it here for the art tile. */
  box-shadow: var(--shadow-card);
  transition: box-shadow 0.18s ease;
}
.rs-card:hover .rs-art { box-shadow: var(--shadow-card-hover), 0 0 0 1px rgb(var(--ink) / 0.06); }
.rs-art img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  /* Some favicons are tiny pixels — render with smooth scaling */
  image-rendering: auto;
}
.rs-art-fallback {
  background: linear-gradient(135deg, color-mix(in srgb, var(--gold) 15%, transparent), color-mix(in srgb, var(--gold) 4%, transparent));
}
.rs-play {
  position: absolute;
  bottom: 8px;
  right: 8px;
  width: 38px;
  height: 38px;
  border-radius: 50%;
  background: var(--gold);
  color: var(--bg-0);
  border: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  opacity: 0;
  transform: translateY(6px);
  transition: opacity 0.2s, transform 0.2s;
  box-shadow: 0 6px 14px rgba(0, 0, 0, 0.45); /* button painted over the station art — stays literal */
}
.rs-art:hover .rs-play { opacity: 1; transform: translateY(0); }
.rs-spin { animation: rs-spin 0.9s linear infinite; }
@keyframes rs-spin { from { transform: rotate(0); } to { transform: rotate(360deg); } }

.rs-fav {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 30px;
  height: 30px;
  border-radius: 50%;
  /* button painted over the station art — stays literal */
  background: rgba(0, 0, 0, 0.5);
  color: rgba(255, 255, 255, 0.85);
  border: 0;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity 0.2s, background 0.15s, color 0.15s;
  backdrop-filter: blur(6px);
}
.rs-fav.active {
  opacity: 1;
  color: var(--gold);
}
.rs-art:hover .rs-fav { opacity: 1; }
.rs-fav:hover { color: var(--gold); background: rgba(0, 0, 0, 0.7); }

/* Touch: hover never fires on coarse pointers and the card has no tap
   target of its own (no navigation — stations only play), so the hover-
   revealed play/favorite buttons are the ONLY actions. Keep them always
   visible there instead of hiding them like MusicCard's overlay — hiding
   would leave radio grids completely inert on phones. */
@media (pointer: coarse) {
  .rs-play { opacity: 1; transform: translateY(0); }
  .rs-fav { opacity: 1; }
}

.rs-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 0 2px;
}
.rs-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.rs-sub {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--fg-2);
}
.rs-sub .dot { color: var(--fg-4); }
.rs-tags {
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-transform: capitalize;
}
.mono { font-family: var(--font-mono); }
</style>
