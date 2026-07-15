<script setup lang="ts">
// MusicMixCard — the Heya 2.0 gradient "mix" tile (heya2.css .mix-card). A wide
// 16/10 card washed in a deterministic per-mix gradient (computed by the host,
// never random at render), with a mono "MIX · N tracks" eyebrow, an Archivo
// display name, and a mono seed-artist line. Dark ink rides the bright gradient
// (token-clean via --shade = 0 0 0, the same "content over a painted surface"
// exception the album-label scrims use).
//
// Pure presentation, exactly like MusicCard: the host owns the NuxtLink +
// AppContextMenu wrapper, so this stays free of the router-in-button nesting
// trap. The centred hover circle is the only interactive element and emits
// `play`; on touch, play lives in the host's long-press context menu.
defineProps<{
  name: string
  trackCount: number
  /** Seed / representative artists, already uppercased & de-duped by the host. */
  artists?: string
  /** Full CSS background value (a linear-gradient) built deterministically upstream. */
  gradient: string
  /** Hides the hover play affordance (e.g. an empty mix). */
  noPlay?: boolean
}>()

const emit = defineEmits<{ play: [] }>()
</script>

<template>
  <div class="mix-card" :style="{ background: gradient }">
    <div v-if="!noPlay" class="mix-play-wrap">
      <span
        role="button"
        tabindex="0"
        class="mix-play"
        :aria-label="`Play ${name}`"
        :title="`Play ${name}`"
        @click.stop.prevent="emit('play')"
        @keydown.enter.stop.prevent="emit('play')"
        @keydown.space.stop.prevent="emit('play')"
      >
        <Icon name="play" :size="16" />
      </span>
    </div>

    <span class="mix-k">Mix &middot; {{ trackCount }} {{ trackCount === 1 ? 'track' : 'tracks' }}</span>
    <h3 class="mix-name">{{ name }}</h3>
    <div v-if="artists" class="mix-meta">{{ artists }}</div>
  </div>
</template>

<style scoped>
/* heya2.css .mix-card — bright decorative gradient tile, dark ink on top.
   --shade is the app's 0 0 0 token, so these dark values stay token-clean and
   theme-agnostic (the card is bright in every theme by design). */
.mix-card {
  position: relative;
  height: 100%;
  aspect-ratio: 16 / 10;
  border-radius: var(--r-md);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  padding: 18px;
  border: 1px solid rgb(var(--ink) / 0.1);
  box-shadow: var(--shadow-card);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.mix-card:hover {
  transform: translateY(-4px);
  box-shadow: var(--shadow-card-hover);
}

.mix-k {
  font: 650 9.5px var(--font-mono);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: rgb(var(--shade) / 0.55);
}
.mix-name {
  margin: 2px 0 0;
  font-family: var(--font-display);
  font-weight: 800;
  font-variation-settings: 'wdth' 118;
  font-size: 22px;
  line-height: 1.05;
  letter-spacing: -0.01em;
  color: rgb(var(--shade) / 0.92);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mix-meta {
  margin-top: 5px;
  font: 550 10.5px var(--font-mono);
  letter-spacing: 0.04em;
  color: rgb(var(--shade) / 0.62);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Hover play — a dark disc riding the bright gradient (literal-dark over a
   painted surface, same exception as the card label scrims). */
.mix-play-wrap {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 2;
  opacity: 0;
  transition: opacity 0.18s ease-out;
}
.mix-card:hover .mix-play-wrap,
.mix-play-wrap:has(.mix-play:focus-visible) { opacity: 1; }
.mix-play {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: rgb(var(--shade) / 0.58);
  color: #fff; /* icon on a fixed dark disc — stays literal */
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: transform 0.15s ease-out, background 0.15s;
}
.mix-play:hover { transform: scale(1.08); background: rgb(var(--shade) / 0.72); }

/* Touch: no hover; play lives in the host long-press menu (like MusicCard). */
@media (pointer: coarse) {
  .mix-play-wrap { display: none; }
}
</style>
