<!--
  MusicCollectionHero — the shared detail hero for track-collection pages
  (playlist detail, Loved Songs). Same grammar as the artist page: a SHARP
  full-bleed art layer via HeroCanvas (which also claims the global ambient
  layer with the same image, so everything below the LedgerStrip seam sits
  on the blurred continuation of the exact artwork shown up top), the house
  dark grade, mono eyebrow, Archivo display title, metaline, and the
  .btn-play / .pill action grammar.

  Tone: the PAGE publishes --tone/--tone-rgb/--tone-ink/--tone-comp on its
  root (same as the artist/album pages — sample via useBackgroundTone() +
  local fallback); this component only consumes the vars.

  Slots:
    #art      — content of the square record-card (LoadingImage /
                MixCollage / fallback icon). The card provides the
                gradient placeholder + shadow.
    #stats    — the mono metaline under the title.
    #actions  — the action buttons row (.btn-play / .pill markup).
-->
<template>
  <header class="mch">
    <HeroCanvas :src="aSrc || ''" :src-b="bSrc" :show-a="showA" object-position="center 25%" />

    <!-- Backdrop tools — expand-to-lightbox + the shared prev/pause/next
         ring, top-right (same cluster as the artist/movie/TV heroes). The
         ring IS the rotation clock: its animationend fires @next. -->
    <div v-if="images.length > 0 || backdrop" class="hero-tools mch-tools">
      <button class="hero-expand" aria-label="Expand backdrop" @click="openLightbox">
        <Icon name="expand" :size="13" />
      </button>
      <CycleControls
        v-if="images.length > 1"
        v-model:paused="paused"
        :cycle-key="cycleKey"
        :duration="BACKDROP_INTERVAL"
        item-label="backdrop"
        @prev="retreat"
        @next="advance"
      />
    </div>

    <div class="mch-inner">
      <div class="mch-card">
        <slot name="art" />
      </div>
      <div class="mch-meta">
        <div class="eyebrow">{{ kind }}</div>
        <h1 class="m-title">{{ title }}</h1>
        <p v-if="description" class="m-sub">{{ description }}</p>
        <div class="mch-stats"><slot name="stats" /></div>
        <div class="m-actions"><slot name="actions" /></div>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
const props = withDefaults(defineProps<{
  /** Mono uppercase eyebrow ("Playlist", "Collection"). */
  kind: string
  title: string
  description?: string
  /** Rotating hero image pool (the collection's artists). The component
   *  owns the carousel: random start, A/B crossfade, the CycleControls
   *  ring as the clock, lightbox expand. HeroCanvas mirrors the shown
   *  image to the ambient layer, keeping the blur below the ledger in
   *  sync automatically. */
  images?: string[]
  /** Single fallback image when the pool is empty (custom cover / healed
   *  collage pick). */
  backdrop?: string | null
}>(), {
  images: () => [],
  backdrop: null,
})

/** The image currently on screen — pages sample it for tone fallback. */
const emit = defineEmits<{ image: [src: string | null] }>()

// Hoisted at setup — the factory touches useNuxtApp() (docs/ui.md gotcha #1).
const bgImg = useBackgroundImageTools()
const lightbox = useLightbox()

const showA = ref(true)
const aSrc = ref<string | null>(null)
const bSrc = ref<string | null>(null)
const idx = ref(0)
const paused = ref(false)
const cycleKey = ref(0)

const current = computed(() => (showA.value ? aSrc.value : bSrc.value))
watch(current, (s) => emit('image', s), { immediate: true })

function warmNext(i: number) {
  const n = props.images.length
  if (n > 1) bgImg.warm(props.images[(i + 1) % n]!)
}

function showIdx(i: number) {
  idx.value = i
  const url = props.images[i]!
  if (showA.value) bSrc.value = url
  else aSrc.value = url
  showA.value = !showA.value
  cycleKey.value++
  warmNext(i)
}

function advance() {
  const n = props.images.length
  if (n > 1) showIdx((idx.value + 1) % n)
}
function retreat() {
  const n = props.images.length
  if (n > 1) showIdx((idx.value - 1 + n) % n)
}

// (Re)seed on pool changes: random start, next image preloaded into the B
// layer so the first crossfade lands hot.
watch(() => props.images, (urls) => {
  if (!urls.length) {
    aSrc.value = props.backdrop ?? null
    bSrc.value = null
    showA.value = true
    cycleKey.value++
    return
  }
  idx.value = Math.floor(Math.random() * urls.length)
  aSrc.value = urls[idx.value]!
  bSrc.value = urls.length > 1 ? urls[(idx.value + 1) % urls.length]! : null
  showA.value = true
  cycleKey.value++
  warmNext(idx.value)
}, { immediate: true })

// Fallback-only heroes follow backdrop changes (e.g. the collage heals).
watch(() => props.backdrop, (b) => {
  if (!props.images.length && showA.value) aSrc.value = b ?? null
})

function openLightbox() {
  if (props.images.length) lightbox.open(props.images, idx.value)
  else if (props.backdrop) lightbox.open(props.backdrop)
}
</script>

<style scoped>
.mch {
  --oink: 233 236 242;
  position: relative;
  min-height: 46vh;
  display: flex;
  align-items: flex-end;
  overflow: hidden; /* THE hard clip at the ledger seam */
}

/* These heroes don't ride under the fixed topbar (no hero-flush) — pin the
   tools to the hero's own top edge instead of heya.css's topbar offset. */
.mch-tools { top: 14px; }

.mch-inner {
  position: relative;
  z-index: 2;
  width: 100%;
  display: flex;
  align-items: flex-end;
  gap: 40px;
  padding: 92px var(--pad-fluid) 36px;
}

/* Square record-card — same directional shadow as the album postercard. */
.mch-card {
  flex: 0 0 200px;
  width: 200px;
  height: 200px;
  border-radius: var(--r-md);
  /* Accent-derived placeholder (same pair as the avatar / Loved tile). */
  background: linear-gradient(135deg, var(--gold-deep, var(--gold)), var(--gold));
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(255, 255, 255, 0.9); /* icon on the generated placeholder art — stays literal */
  overflow: hidden;
  box-shadow:
    0 0 0 1px rgb(var(--oink) / 0.14),
    10px 18px 34px -12px rgb(0 0 0 / 0.8),
    24px 44px 90px -20px rgb(0 0 0 / 0.95);
}
.mch-card :deep(img) { width: 100%; height: 100%; object-fit: cover; }

.mch-meta { flex: 1; min-width: 0; }

/* mono eyebrow — complement-colored over the dark grade (artist grammar). */
.eyebrow {
  font: 600 11.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone-comp, var(--tone));
  margin-bottom: 14px;
}

/* Archivo display title (heya2 detail-page identity). On-artwork ink. */
.m-title {
  font-family: var(--font-display);
  font-size: clamp(2.4rem, 5vw, 4rem);
  font-weight: 800;
  line-height: 0.98;
  letter-spacing: 0;
  margin: 0 0 10px;
  color: rgb(var(--oink) / 0.98);
  text-shadow: 0 2px 30px rgb(0 0 0 / 0.45); /* on artwork — stays literal */
}
.m-sub {
  color: rgb(var(--oink) / 0.82);
  margin: 0 0 12px;
  max-width: 72ch;
  font-size: 13.5px;
  line-height: 1.5;
  text-shadow: 0 1px 8px rgb(0 0 0 / 0.5); /* on artwork — stays literal */
}
.mch-stats {
  display: flex; align-items: center; gap: 8px;
  font: 500 12px var(--font-mono);
  color: rgb(var(--oink) / 0.72);
  margin-bottom: 18px;
  text-shadow: 0 1px 8px rgb(0 0 0 / 0.5); /* on artwork — stays literal */
}
.mch-stats :deep(.dot) { color: rgb(var(--oink) / 0.4); }

.m-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }

/* Phone (<=720px): stack the hero, center the art, wrap the action row. */
@media (max-width: 720px) {
  .mch { min-height: 0; }
  .mch-inner {
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: 24px 20px 20px;
    gap: 14px;
  }
  .mch-card { flex-basis: auto; width: min(55vw, 240px); height: min(55vw, 240px); }
  .mch-meta { width: 100%; }
  .mch-stats { justify-content: center; }
  .m-actions { justify-content: center; }
}
</style>

<!-- Action-button grammar (heya2.css .btn-play / .pill, lifted from the
     artist page). Unscoped with a .mch prefix: the buttons arrive through
     the #actions slot, so they carry the PAGE's scope attribute — scoped
     rules here would never match them. -->
<style>
.mch .btn-play {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 13px 26px 13px 20px;
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
  transition: transform 0.15s ease, box-shadow 0.15s ease,
    background 0.9s cubic-bezier(0.22, 1, 0.36, 1), color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
.mch .btn-play:hover:not([disabled]) {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 40px rgb(var(--tone-rgb) / 0.6),
    8px 14px 48px -8px rgb(var(--tone-rgb) / 0.9);
}
.mch .btn-play[disabled] { cursor: not-allowed; opacity: 0.4; box-shadow: 0 0 0 1px rgb(233 236 242 / 0.14); transform: none; }
.mch .btn-play .tri {
  width: 0; height: 0;
  border-left: 11px solid var(--tone-ink, #0a0c10);
  border-top: 7px solid transparent;
  border-bottom: 7px solid transparent;
}
.mch .btn-play small { font: 500 11px var(--font-mono); opacity: 0.72; letter-spacing: 0.06em; }

.mch .pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 11px 18px;
  border-radius: 999px;
  cursor: pointer;
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  background: rgb(var(--tone-rgb) / 0.08);
  color: rgb(233 236 242 / 0.9);
  font: 550 13px var(--font-sans);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.14), 5px 8px 22px -10px rgb(0 0 0 / 0.7);
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s, transform 0.15s, color 0.15s;
}
.mch .pill:hover:not([disabled]) {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  color: rgb(233 236 242);
  box-shadow: 0 0 24px rgb(var(--tone-rgb) / 0.28), 6px 10px 26px -10px rgb(0 0 0 / 0.75);
  transform: translateY(-1px);
}
.mch .pill[disabled] { cursor: not-allowed; opacity: 0.4; }
.mch .pill.icon { width: 42px; height: 42px; padding: 0; justify-content: center; }

@media (max-width: 720px) {
  .mch .btn-play { flex: 1 1 100%; justify-content: center; height: 48px; }
  .mch .pill:not(.icon) { flex: 1 1 auto; justify-content: center; height: 46px; }
  .mch .pill.icon { width: 46px; height: 46px; }
}
</style>
