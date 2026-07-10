<script setup lang="ts">
// MusicCard — reusable tile for the music home (and elsewhere). Mirrors
// the EpisodeCard pattern: art fills the tile, a bottom gradient hosts the
// title/subtitle text painted on top, top-left holds a category badge, the
// play button overlays on hover. Variants:
//
//   variant="square"  — single cover image (1/1), default
//   variant="circle"  — square art clipped to a circle (used for artists)
//
// The host renders the link wrapper itself; this stays a pure presentation
// component to avoid the routing-vs-events ambiguity that bit the previous
// design (router-link inside button-inside-card).

const props = withDefaults(defineProps<{
  src?: string | null
  alt?: string
  title: string
  subtitle?: string
  /** Top-left badge text — small chip painted on the art (e.g. "EP", "MIX", "2019"). */
  badgeTl?: string
  /** Top-right badge text — small chip painted on the art (e.g. "42 plays"). */
  badgeTr?: string
  variant?: 'square' | 'circle'
  /** Hides the hover play button (use when the tile has no meaningful "play all" action). */
  noPlay?: boolean
  /** 0..100 — renders a progress bar along the bottom edge of the art. */
  progressPct?: number
  /** Renders a trashcan badge and greys + dims the art (missing on disk). */
  missing?: boolean
  /** Base width hint for the resize provider; `densities="1x 2x"` doubles it on
   *  HiDPI. Default 200 fits the ~160-180px scroll-row tiles. */
  width?: number
}>(), {
  src: '',
  alt: '',
  subtitle: '',
  badgeTl: '',
  badgeTr: '',
  variant: 'square',
  noPlay: false,
  progressPct: 0,
  missing: false,
})

const emit = defineEmits<{ play: [] }>()

// Mirrors Poster.vue: on load error fall back to the icon tile rather than a
// broken image, and reset when the src changes.
const imgError = ref(false)
watch(() => props.src, () => { imgError.value = false })
</script>

<template>
  <div class="mc" :class="[`mc-${variant}`, { 'mc-missing': missing }]">
    <MediaMissingBadge v-if="missing" />
    <div class="mc-art">
      <NuxtImg
        v-if="src && !imgError"
        :src="src"
        :alt="alt || title"
        :width="width ?? 200"
        :quality="80"
        densities="1x 2x"
        loading="lazy"
        @error="imgError = true"
      />
      <div v-else class="mc-fallback">
        <Icon name="music" :size="38" />
      </div>

      <div class="mc-gradient" />

      <div v-if="badgeTl" class="mc-badge mc-badge-tl">{{ badgeTl }}</div>
      <div v-if="badgeTr" class="mc-badge mc-badge-tr">{{ badgeTr }}</div>

      <!-- Hover-only play button — centered, glassy, EpisodeCard pattern.
           Wrap is non-interactive (pointer-events: none) so only the circle
           captures clicks; everything else routes through the outer link. -->
      <div v-if="!noPlay && !missing" class="mc-play-wrap">
        <button
          type="button"
          class="mc-play"
          :title="`Play ${title}`"
          @click.stop.prevent="emit('play')"
        >
          <Icon name="play" :size="18" />
        </button>
      </div>

      <!-- Caption painted on the bottom gradient. -->
      <div class="mc-info">
        <div class="mc-title">{{ title }}</div>
        <div v-if="subtitle" class="mc-sub">{{ subtitle }}</div>
      </div>

      <div v-if="progressPct > 0" class="mc-progress">
        <div class="mc-progress-fill" :style="{ width: Math.min(100, progressPct) + '%' }" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.mc {
  display: block;
  height: 100%;
  position: relative;
}
/* Missing-on-disk: grey + dim the art, leaving the trash badge full colour. */
.mc-missing .mc-art > img,
.mc-missing .mc-fallback { filter: grayscale(1); opacity: 0.5; }

.mc-art {
  position: relative;
  aspect-ratio: 1 / 1;
  background: var(--bg-3);
  overflow: hidden;
  border-radius: var(--r-md);
  box-shadow: 0 8px 18px rgb(var(--shade) / 0.45);
}
.mc-circle .mc-art { border-radius: 50%; }
.mc-art > img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.mc-fallback {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
  background: linear-gradient(135deg, color-mix(in srgb, var(--gold) 10%, transparent), color-mix(in srgb, var(--gold) 2%, transparent));
}

.mc-gradient {
  position: absolute; inset: 0;
  /* scrim over the cover art for caption legibility — stays literal */
  background: linear-gradient(0deg, rgba(0,0,0,0.85) 0%, rgba(0,0,0,0.25) 45%, transparent 75%);
  pointer-events: none;
}

/* Badges (top-left, top-right). Same chip shape, just different anchors. */
.mc-badge {
  position: absolute; z-index: 3;
  display: inline-flex; align-items: center; gap: 3px;
  padding: 3px 9px;
  border-radius: 999px;
  /* badge painted over the cover art — stays literal */
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(6px);
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  color: rgba(255, 255, 255, 0.85);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  max-width: calc(100% - 16px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  pointer-events: none;
}
.mc-badge-tl { top: 8px; left: 8px; }
.mc-badge-tr { top: 8px; right: 8px; color: var(--gold); }

/* Hover play button — centered, gold. The wrap is `pointer-events: none`
   so clicks on the rest of the art still bubble to the outer NuxtLink for
   navigation; only the centered circle captures clicks for the play event.
   Single consistent look (no swap on inner hover) — the swap caused a
   white→gold stutter because backdrop-filter blends jaggedly under opacity. */
.mc-play-wrap {
  position: absolute; inset: 0; z-index: 2;
  display: flex; align-items: center; justify-content: center;
  opacity: 0;
  transition: opacity 0.18s ease-out;
  background: transparent;
  border: 0;
  padding: 0;
  pointer-events: none;
}
.mc:hover .mc-play-wrap { opacity: 1; }
.mc-play {
  width: 48px; height: 48px;
  border-radius: 50%;
  background: var(--gold);
  color: var(--bg-0);
  border: 0;
  display: flex; align-items: center; justify-content: center;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.4); /* button painted over the cover art — stays literal */
  cursor: pointer;
  pointer-events: auto;
  transition: transform 0.15s ease-out;
}
.mc-play:hover { transform: scale(1.08); }

/* Touch: the hover-only play button never gets a hover state on coarse
   pointers, so it would otherwise just sit there as dead chrome. Tap keeps
   its existing meaning (navigate via the outer NuxtLink); playing lives in
   the long-press context menu the pages already wrap every card with
   (AppContextMenu — see useMusicActions forAlbum/forArtist/forMix/forPlaylist,
   each of which has a "Play"/"Play Top Tracks"/"Play Mix" row). Do NOT make
   bare tap play — that breaks navigation. */
@media (pointer: coarse) {
  .mc-play-wrap { display: none; }
}

.mc-info {
  position: absolute; bottom: 0; left: 0; right: 0; z-index: 2;
  padding: 10px 12px 12px;
  pointer-events: none;
}
.mc-title {
  font-size: 14px;
  font-weight: 700;
  line-height: 1.25;
  color: #fff; /* caption painted over the cover art — stays literal */
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-shadow: 0 1px 4px rgba(0, 0, 0, 0.4);
}
.mc-sub {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.65); /* caption painted over the cover art — stays literal */
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Circular variant — keep label outside the clipped art so it remains
   readable. Circles look weird with text overlay painted on them. */
.mc-circle .mc-art { box-shadow: 0 8px 18px rgb(var(--shade) / 0.45); }
.mc-circle .mc-gradient,
.mc-circle .mc-info { display: none; }

.mc-progress {
  position: absolute; bottom: 0; left: 0; right: 0;
  height: 3px; z-index: 3;
  background: rgba(255, 255, 255, 0.1); /* track painted over the cover art — stays literal */
  pointer-events: none;
}
.mc-progress-fill {
  height: 100%;
  background: var(--gold);
  border-radius: 0 2px 2px 0;
  transition: width 0.3s ease;
}
</style>
