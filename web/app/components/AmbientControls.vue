<template>
  <div v-if="visible" class="ambient-ctls" :class="{ revealed: ctl.reveal }">
    <!-- The three round buttons stack vertically on the left so the poster's
         growth (rightward, next to them) never shifts them around. The eye
         renders in BOTH modes; shuffle/pause only where there's a pool to
         steer — in art mode the owning hero rotates its own artwork. -->
    <div class="actl-stack">
      <button
        class="actl"
        :class="{ on: ctl.reveal }"
        :aria-pressed="ctl.reveal"
        :aria-label="ctl.reveal ? 'Bring the page back (Esc)' : 'Show the background'"
        :title="ctl.reveal ? 'Bring the page back (Esc)' : 'Show the background'"
        @click="ctl.reveal = !ctl.reveal"
      >
        <Icon :name="ctl.reveal ? 'eye-slash' : 'eye'" :size="13" />
      </button>
      <button v-if="ctl.mode === 'pool'" class="actl actl-shuffle" aria-label="New background" title="New background" @click="ctl.shuffleReq++">
        <!-- Ring = time until the next automatic switch. Re-keys on every new
             rotation window; duration is bound to BG_ROTATE_MS so the ring and
             the layer's timer can't drift. -->
        <svg v-if="ctl.rotating && !reducedMotion" class="actl-ring" viewBox="0 0 26 26" aria-hidden="true">
          <circle
            :key="ctl.cycle"
            class="actl-ring-fill"
            :style="{ animationDuration: `${BG_ROTATE_MS}ms` }"
            cx="13" cy="13" r="11.5"
          />
        </svg>
        <Icon name="shuffle" :size="12" />
      </button>
      <button
        v-if="ctl.mode === 'pool'"
        class="actl"
        :class="{ on: ctl.paused }"
        :aria-pressed="ctl.paused"
        :aria-label="ctl.paused ? 'Resume rotation' : 'Pause rotation'"
        :title="ctl.paused ? 'Resume rotation' : 'Pause rotation'"
        @click="ctl.paused = !ctl.paused"
      >
        <Icon :name="ctl.paused ? 'play' : 'pause'" :size="12" />
      </button>
    </div>
    <!-- The backdrop's identity: the button IS the poster — a 26px sliver
         that grows out of the corner on hover (and says hello). Click goes
         to the item. Keyed per item so a failed load's state never leaks
         onto the next poster; when an item's poster 404s the whole button
         hides rather than leaving a ghost box. -->
    <NuxtLink
      v-if="ctl.current && !posterFailed"
      :key="ctl.current.poster"
      class="actl-poster"
      :to="currentTo"
      :aria-label="`Go to ${ctl.current.title}`"
    >
      <LoadingImage
        :src="ctl.current.poster"
        :width="240"
        :quality="80"
        alt=""
        class="actl-poster-img"
        @error="posterFailed = true"
      />
      <span class="actl-poster-name">{{ ctl.current.title }}</span>
    </NuxtLink>
  </div>
</template>

<script setup lang="ts">
// Bottom-left ambient-background cluster (an EVE-KILL homage): the eye
// fades the whole app away to admire the artwork, shuffle jumps to a random
// pool image (its ring counts down to the next automatic switch), and
// play/pause freezes the rotation. State lives in useBackgroundControls —
// shared with the AmbientBackdrop layer and persistent across navigation
// (pause survives reloads too).
//
// Renders whenever the layer is showing anything. In pool mode the full
// cluster appears; in art mode (home hero deck, detail pages) only the eye
// — the owner rotates its own artwork, so shuffle/pause would be lies, and
// the poster button would just point at the page you're on.
//
// /music exception: the POOL cluster next to the Playbar read as clutter
// (hidden by request), but art mode — artist/album detail — keeps the lone
// eye like every other detail page, offset above the Playbar via heya.css.
const ctl = useBackgroundControls()
const route = useRoute()
const visible = computed(() => {
  if (ctl.value.mode === 'off') return false
  if (ctl.value.mode === 'pool' && route.path.startsWith('/music')) return false
  return true
})

// Route for the poster button — mediaUrl handles the per-type prefixes
// (movie → /movies, music → /music/artist, …).
const currentTo = computed(() => {
  const c = ctl.value.current
  if (!c) return ''
  return mediaUrl({ id: 0, title: c.title, media_type: c.mediaType, slug: c.slug })
})

// One failed poster hides the button for THAT item only.
const posterFailed = ref(false)
watch(() => ctl.value.current?.poster, () => { posterFailed.value = false })

const reducedMotion = import.meta.client
  ? window.matchMedia('(prefers-reduced-motion: reduce)').matches
  : false

// Escape is the panic exit from reveal — the faded page can't be clicked.
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && ctl.value.reveal) ctl.value.reveal = false
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<style scoped>
.ambient-ctls {
  position: fixed;
  left: 16px;
  bottom: 16px;
  z-index: 40;
  display: flex;
  align-items: flex-end;
  gap: 6px;
  /* Quiet furniture: static in the corner (no ducking — geometry that
     moves under the cursor is geometry that's hard to hit), just dimmed
     until approached. */
  opacity: 0.4;
  transition: opacity 0.25s ease;
}
/* Generous invisible hit halo — easier to reach into the corner, and the
   opacity wake-up can't shimmer when grazing the buttons' edges. */
.ambient-ctls::before {
  content: '';
  position: absolute;
  inset: -16px;
}
.ambient-ctls:hover,
.ambient-ctls:focus-within,
.ambient-ctls.revealed {
  opacity: 1;
}

/* Vertical rail for the three round buttons — the poster sits to their
   right and grows away from them, so they hold perfectly still. */
.actl-stack {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.actl {
  position: relative;
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-1);
  background: color-mix(in oklab, var(--bg-2) 78%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  transition: background 0.12s, color 0.12s;
}
.actl:hover { background: var(--bg-3); color: var(--fg-0); }
.actl.on { color: var(--gold); }
/* Touch devices wider than the 1200px cutoff below (e.g. iPad Pro
   landscape) still show this cluster — grow the round buttons to a real
   touch target under a coarse pointer without touching the mouse-driven
   26px visual (min-width/height only wins here because it's larger than
   the explicit 26px above). */
@media (pointer: coarse) {
  .actl { min-width: 44px; min-height: 44px; }
}

/* Cycle-progress ring, same recipe as the hero carousels: fills from
   12 o'clock; full = next image. */
.actl-ring {
  position: absolute;
  inset: -1px;
  transform: rotate(-90deg);
  pointer-events: none;
}
.actl-ring-fill {
  fill: none;
  stroke: var(--gold);
  stroke-width: 2;
  stroke-linecap: round;
  stroke-dasharray: 72.3; /* 2π · r(11.5) */
  stroke-dashoffset: 72.3;
  animation: actl-ring-fill linear forwards; /* duration bound inline = BG_ROTATE_MS */
}
@keyframes actl-ring-fill { to { stroke-dashoffset: 0; } }

/* The backdrop's poster IS the button: the link itself is the artwork
   (img just fills it), 26px wide at rest, growing to a full poster on
   hover. In-flow on purpose — the sibling buttons politely make room. The
   cluster is bottom-anchored so growth goes up and to the right. */
.actl-poster {
  position: relative;
  display: block;
  width: 26px;
  aspect-ratio: 2 / 3;
  flex-shrink: 0;
  border-radius: 5px;
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  background: var(--bg-3);
  transition: width 0.28s cubic-bezier(0.22, 1, 0.36, 1),
              border-radius 0.28s ease, box-shadow 0.28s ease;
}
.actl-poster:hover,
.actl-poster:focus-visible {
  width: 118px;
  border-radius: var(--r-md);
  box-shadow: var(--shadow-card);
}
.actl-poster-img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: inherit;
}
.actl-poster-name {
  position: absolute;
  left: 0;
  /* Rides the top edge as the poster grows. */
  bottom: calc(100% + 8px);
  max-width: 220px;
  padding: 4px 10px;
  border-radius: 999px;
  background: color-mix(in oklab, var(--bg-2) 88%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  font-size: 11px;
  font-family: var(--font-mono);
  letter-spacing: 0.02em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--fg-0);
  opacity: 0;
  transform: translateY(4px);
  pointer-events: none;
  transition: opacity 0.2s ease 0.12s, transform 0.2s ease 0.12s;
}
.actl-poster:hover .actl-poster-name,
.actl-poster:focus-visible .actl-poster-name {
  opacity: 1;
  transform: none;
}

/* Phones, foldables, tablets (the compact band): the corner belongs to
   BottomNav/MiniPlayer and touch has no hover to un-duck with. */
@media (max-width: 1200px) {
  .ambient-ctls { display: none; }
}
</style>
