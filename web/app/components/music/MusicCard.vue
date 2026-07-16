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
  /** Shows a filled heart on cards whose artist/album is in the heart rating band. */
  hearted?: boolean
  variant?: 'square' | 'circle'
  /** Shows the title + subtitle as a centered caption BELOW the art (heya2.css
   *  `.artist-card`) instead of painted over it. Intended for `variant="circle"`,
   *  whose clipped round art has no room for an embedded label. */
  captioned?: boolean
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
  hearted: false,
  variant: 'square',
  captioned: false,
  noPlay: false,
  progressPct: 0,
  missing: false,
})

const emit = defineEmits<{ play: [] }>()

// Mirrors Poster.vue: on load error fall back to the no-art tile rather than a
// broken image, and reset when the src changes.
const imgError = ref(false)
watch(() => props.src, () => { imgError.value = false })

// Complement caption tint — sample the art's tone and lift its hue-opposite
// into text-grade lightness (sampleImageTone memoizes per URL, so grids pay
// once per cover). Gated by the Appearance "Tinted captions" switch; off →
// the CSS fallback collapses the mix to plain white. Sequence-guarded: a
// slow sample must not land after the card was recycled onto a different
// src (virtual grids reuse instances).
const { tintedCaptionsEnabled } = useAppearance()
const compTint = ref<string | null>(null)
watch(() => [tintedCaptionsEnabled.value, props.src] as const, ([tint, src]) => {
  compTint.value = null
  if (!tint || !src || !import.meta.client) return
  sampleImageTone(src).then((t) => {
    if (t && src === props.src && tintedCaptionsEnabled.value) compTint.value = toneTextVariant(t.complementTriplet)
  })
}, { immediate: true })
const tintStyle = computed(() => (compTint.value ? { '--mc-comp': compTint.value } : undefined))

// No-art tile initials (Heya 2.0), from the title — first letters of the
// first two words, else the first two characters. Falls back to a music icon
// only when there's no usable title.
const initials = computed(() => {
  const t = (props.title || '').trim()
  if (!t) return ''
  const words = t.split(/\s+/).filter(Boolean)
  const chars = words.length >= 2 ? words[0]!.charAt(0) + words[1]!.charAt(0) : t.slice(0, 2)
  return chars.toUpperCase()
})
</script>

<template>
  <div class="mc" :class="[`mc-${variant}`, { 'mc-missing': missing, 'mc-captioned': captioned }]" :style="tintStyle">
    <MediaMissingBadge v-if="missing" />
    <div class="mc-art">
      <LoadingImage
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
        <span v-if="initials" class="mc-initials">{{ initials }}</span>
        <Icon v-else name="music" :size="38" />
      </div>

      <div class="mc-gradient" />

      <div v-if="badgeTl" class="mc-badge mc-badge-tl">{{ badgeTl }}</div>
      <div v-if="badgeTr" class="mc-badge mc-badge-tr">{{ badgeTr }}</div>
      <div v-if="hearted" class="mc-hearted" aria-label="Hearted" title="Hearted">
        <Icon name="heartfill" :size="15" />
      </div>

      <!-- Hover-only play button — centered, glassy, EpisodeCard pattern.
           Wrap is non-interactive (pointer-events: none) so only the circle
           captures clicks; everything else routes through the outer link. -->
      <!-- Not a real <button>: the caller wraps this whole card in a
           NuxtLink (see MusicHome.vue etc.), and a native button nested
           inside an anchor is invalid interactive-in-interactive HTML. A
           span with role="button" gets the same click/keyboard behavior
           without that nesting violation — see CLAUDE.md's "invalid
           nesting" note for the reasoning; a full DOM restructure (pulling
           this out as a sibling of every consuming NuxtLink) was assessed
           as too broad/risky for the ~10 call sites that reuse this card. -->
      <div v-if="!noPlay && !missing" class="mc-play-wrap">
        <span
          role="button"
          tabindex="0"
          class="mc-play"
          :aria-label="`Play ${title}`"
          :title="`Play ${title}`"
          @click.stop.prevent="emit('play')"
          @keydown.enter.stop.prevent="emit('play')"
          @keydown.space.stop.prevent="emit('play')"
        >
          <Icon name="play" :size="18" />
        </span>
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

    <!-- Below-art caption (heya2.css .artist-card) — opt-in via `captioned`,
         used by circles whose round art can't host an embedded label. -->
    <div v-if="captioned" class="mc-caption">
      <div class="mc-caption-nm">{{ title }}</div>
      <div v-if="subtitle" class="mc-caption-meta">{{ subtitle }}</div>
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
  /* Query container so the no-art initials scale to the tile via cqi. */
  container-type: inline-size;
  /* Same elevation + hover lift as the app-wide .card-tile/.poster combo —
     box-shadow follows border-radius, so the circle variant gets a round
     shadow from this same rule. Shadow settles over .28s (Heya 2.0). */
  box-shadow: var(--shadow-card);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.mc:hover .mc-art {
  transform: translateY(-4px);
  box-shadow: var(--shadow-card-hover), 0 0 0 1px rgb(var(--ink) / 0.06);
}
.mc-circle .mc-art { border-radius: 50%; }
.mc-art > img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
/* No-art tile (Heya 2.0): dark surface (the .mc-art --bg-3) under big faint
   mono initials, matching album-sm/.artist-card .noart in the mockup. */
.mc-fallback {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
  background: linear-gradient(150deg, color-mix(in srgb, var(--gold) 8%, transparent), transparent 62%);
}
.mc-initials {
  font-family: var(--font-mono);
  font-weight: 800;
  font-size: min(24cqi, 3rem);
  letter-spacing: 0.02em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.2);
  user-select: none;
}

.mc-gradient {
  position: absolute; inset: 0;
  /* Caption-band scrim ONLY: dense under the two text lines, fully gone
     by ~a third up — the art above stays untinted (the old scrim washed
     the lower half of every cover). Stays literal. */
  background: linear-gradient(0deg, rgba(0,0,0,0.9) 0%, rgba(0,0,0,0.62) 22%, rgba(0,0,0,0.26) 42%, transparent 62%);
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

.mc-hearted {
  position: absolute;
  z-index: 4;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  color: var(--bad);
  background: rgba(0, 0, 0, 0.68);
  backdrop-filter: blur(6px);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.35);
  pointer-events: none;
}
.mc-badge-tr + .mc-hearted { top: 42px; }

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
.mc:hover .mc-play-wrap,
.mc-play-wrap:has(.mc-play:focus-visible) { opacity: 1; }
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
  /* tint-caption cards mix the cover's complement in; without the sample
     the fallback triplet collapses the mix to plain white. Painted over
     the cover art — the literals stay. */
  color: color-mix(in oklab, rgb(var(--mc-comp, 255 255 255)) 78%, rgb(255 255 255));
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  /* multi-layer halo (hero-halo recipe, scaled down) — the scrim alone
     couldn't rescue captions on busy/bright covers */
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7), 0 0 8px rgba(0, 0, 0, 0.55), 0 0 18px rgba(0, 0, 0, 0.4);
}
.mc-sub {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.85); /* caption painted over the cover art — stays literal */
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7), 0 0 8px rgba(0, 0, 0, 0.5);
}

/* Circular variant — keep label outside the clipped art so it remains
   readable. Circles look weird with text overlay painted on them. */
.mc-circle .mc-gradient,
.mc-circle .mc-info { display: none; }

/* Captioned: label sits below the art (heya2.css .artist-card), so the
   over-art gradient + embedded caption are suppressed regardless of variant. */
.mc-captioned .mc-gradient,
.mc-captioned .mc-info { display: none; }
.mc-caption { text-align: center; padding-top: 10px; }
/* Circle captions sit BELOW the round art, so the tile's down-right
   directional card shadow smears under them. A --bg-1 halo keeps the
   letterforms crisp against that shadow (additive only — the shadow itself
   is the approved 2.0 look and stays). */
.mc-caption-nm {
  font-size: 13px;
  font-weight: 620;
  line-height: 1.3;
  color: var(--fg-0);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mc-caption-meta {
  margin-top: 2px;
  font: 500 10px var(--font-mono);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--fg-3);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

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
