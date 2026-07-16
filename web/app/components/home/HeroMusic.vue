<template>
  <section
    class="hero-music"
    :style="toneVars"
    @mouseenter="onHover(true)"
    @mouseleave="onHover(false)"
    @focusin="onFocus($event, true)"
    @focusout="onFocus($event, false)"
  >
    <!-- Sharp artist backdrop (when present) hard-clipped at the seam with the
         literal-dark 2.0 grade + tone leak — parity with the Featured hero;
         else the cover blurred into ambience. The blurred site-wide underlay is
         the global AmbientBackdrop, fed a graded (v2) claim when ambient is on. -->
    <div class="music-bg">
      <Transition name="mbg">
        <LoadingImage
          v-if="bgUrl"
          :key="bgUrl"
          :src="bgUrl"
          :width="1920"
          :quality="75"
          class="music-bg-img"
          alt=""
        />
        <LoadingImage
          v-else-if="bgFallback"
          :key="`blur:${bgFallback}`"
          :src="bgFallback"
          :width="1280"
          :quality="80"
          class="music-bg-blur"
          alt=""
        />
      </Transition>
      <div class="music-grade" />
      <div class="music-tone" />
    </div>

    <div class="music-inner">
      <!-- Spotlight cover: full hero height, square — THE thing on stage.
           One persistent element; on handoff it FLIP-flies from the promoted
           queue card's rect into place (see advance()). -->
      <NuxtLink
        v-if="spotlight"
        ref="posterLink"
        :to="spotlight.to"
        class="music-poster"
      >
        <LoadingImage
          :key="spotlight.key"
          :src="spotlight.art"
          :width="620"
          :quality="85"
          alt=""
          @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.visibility = 'hidden' }"
        />
      </NuxtLink>

      <div class="music-main">
        <div class="music-top">
          <Transition name="spot" mode="out-in">
            <div v-if="spotlight" :key="spotlight.key" class="music-spot hero-ink">
              <div class="music-eyebrow">New in music</div>
              <NuxtLink :to="spotlight.to" class="music-title-link">
                <h1 class="music-title">{{ spotlight.title }}</h1>
              </NuxtLink>
              <div class="music-meta">
                <span class="chip gold">{{ spotlight.kind }}</span>
                <span class="music-meta-sub">{{ spotlight.sub }}</span>
              </div>
              <div class="music-actions">
                <NuxtLink :to="spotlight.to" class="btn btn-primary music-cta" :style="ctaStyle">
                  {{ spotlight.kindGroup === 'artist' ? 'Go to artist' : 'Go to album' }}
                  <Icon name="chevright" :size="15" />
                </NuxtLink>
                <NuxtLink to="/music" class="btn btn-ghost">Open Music</NuxtLink>
                <NuxtLink to="/music/library" class="btn btn-ghost">Library</NuxtLink>
              </div>
            </div>
          </Transition>

          <!-- Controls ride the deck's top-right cluster beside the mode tabs
               (same slot the other heroes use). On phones the aux slot is
               hidden, so the teleport is disabled and they render inline.
               CycleControls' ring IS the cycle clock: its animationend
               advances the carousel, so ring and rotation can't drift. -->
          <Teleport defer :disabled="isPhone" to="#hero-deck-aux">
            <CycleControls
              v-model:paused="userPaused"
              :ring-paused="hoverPaused || focusPaused"
              :cycle-key="cycleKey"
              :duration="CYCLE_MS"
              item-label="release"
              :inline="isPhone"
              @prev="retreat"
              @next="advance"
            />
          </Teleport>
          <p v-if="summary" class="music-sum">{{ summary }}</p>
        </div>

        <!-- The queue: everything waiting for its turn. As the carousel
             advances, the head card is promoted to the spotlight and the rest
             FLIP-slide one slot left; the outgoing spotlight rejoins at the
             tail. Standard MediaCards — no chrome of their own. -->
        <TransitionGroup ref="feedGroup" name="strip" tag="div" class="music-feed">
          <NuxtLink
            v-for="(ev, i) in stripRows"
            :key="ev.key"
            :to="ev.to"
            class="music-card card-tile"
          >
            <MediaCard
              :idx="i"
              :src="ev.art"
              aspect="1/1"
              :title="ev.title"
              :subtitle="ev.sub"
              :badge-tl="ev.kindShort"
            />
          </NuxtLink>
        </TransitionGroup>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
// "Music" — the same carousel language as the "New" hero, tuned for music:
// the newest albums and artist events queue along the bottom; every CYCLE
// the head takes the spotlight (big square cover + tone-matched CTA) and the
// strip slides left. The background is the spotlight's ARTIST backdrop when
// one exists — probed first, since music backdrops are often missing — else
// the cover art blurred. No layer of its own paints over the page canvas,
// so ambient mode extends seamlessly.
import type { MediaItem } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'

type Albumish = MediaItem & { sub?: string; poster_src?: string; artist_slug?: string; album_slug?: string }
type Artistish = MediaItem & { sub?: string }

const props = defineProps<{
  albums: MediaItem[]
  artists: Artistish[]
}>()

const { currentTrack, playing } = usePlayerBindings()

interface MusicRow {
  key: string
  to: string
  art: string
  backdrop: string | null
  title: string
  sub: string
  kind: string
  kindShort: string
  kindGroup: 'album' | 'artist'
}

const feed = computed<MusicRow[]>(() => {
  const rows: MusicRow[] = []
  for (const raw of props.albums.slice(0, 7)) {
    const al = raw as Albumish
    // Albums have no media item of their own — art comes via the album-cover
    // endpoint (poster_src), and the backdrop belongs to the ARTIST, whose
    // slug the image endpoint resolves directly.
    rows.push({
      key: `album-${al.id}`,
      to: al.artist_slug && al.album_slug ? `/music/artist/${al.artist_slug}/${al.album_slug}` : '/music/library',
      art: al.poster_src ?? '',
      backdrop: al.artist_slug ? `/api/media/${al.artist_slug}/image/backdrop` : null,
      title: al.title,
      sub: [al.sub, al.year].filter(Boolean).join(' · '),
      kind: 'New album',
      kindShort: 'ALBUM',
      kindGroup: 'album',
    })
  }
  for (const a of props.artists.slice(0, 3)) {
    rows.push({
      key: `artist-${a.id}`,
      to: mediaUrl(a),
      art: usePosterUrl(a) ?? '',
      backdrop: useBackdropUrl(a) || null,
      title: a.title,
      sub: a.sub ?? '',
      kind: a.sub === 'New artist' ? 'New artist' : 'New music',
      kindShort: 'ARTIST',
      kindGroup: 'artist',
    })
  }
  return rows.slice(0, 10)
})

// ── Backdrop probing ──
// Validate each row's backdrop URL once at thumbnail size; until (and unless)
// it resolves, the blurred cover stands in. Keeps the full-bleed layer real
// instead of flashing broken loads on artists without backdrops.
const bgOk = reactive<Record<string, boolean>>({})
const probing = new Set<string>()
function ensureProbe(url: string | null) {
  if (!url || url in bgOk || probing.has(url) || import.meta.server) return
  probing.add(url)
  const img = new Image()
  img.onload = () => { bgOk[url] = true }
  img.onerror = () => { bgOk[url] = false }
  img.src = `${url}?w=64`
}
watch(feed, (rows) => { for (const r of rows) ensureProbe(r.backdrop) }, { immediate: true })

// ── Carousel clock ──
// Identical mechanics to HeroNewIn: CycleControls' ring IS the timer;
// advance/retreat re-key it. Hover, keyboard focus (ring-paused), and the
// sticky button (v-model) pause independently and compose.
const CYCLE_MS = 15_000
const cursor = ref(0)
const cycleKey = ref(0)
const hoverPaused = ref(false)
const focusPaused = ref(false)
const userPaused = ref(false)
// Still needed locally: skips the shared-element FLIP flight in advance().
const reducedMotion = import.meta.client
  ? window.matchMedia('(prefers-reduced-motion: reduce)').matches
  : false
const { isPhone } = useViewport()

const canHover = import.meta.client
  ? window.matchMedia('(hover: hover)').matches
  : true
function onHover(state: boolean) {
  if (canHover) hoverPaused.value = state
}
function onFocus(e: FocusEvent, state: boolean) {
  if ((e.target as HTMLElement | null)?.closest?.('.cyc-ctls')) return
  focusPaused.value = state
}

function retreat() {
  const f = feed.value
  if (f.length <= 1) return
  cursor.value = (cursor.value - 1 + f.length) % f.length
  cycleKey.value++
}

// ── Shared-element handoff ──
// The promoted card doesn't fade — it BECOMES the spotlight cover: measure
// its rect, preload the art, advance, then fly the big cover from the card's
// rect into place (FLIP via the Web Animations API).
const posterLink = ref<{ $el?: HTMLElement } | HTMLElement | null>(null)
const feedGroup = ref<{ $el?: HTMLElement } | null>(null)
function elOf(r: { $el?: HTMLElement } | HTMLElement | null): HTMLElement | null {
  if (!r) return null
  return r instanceof HTMLElement ? r : (r.$el ?? null)
}

function advance() {
  const f = feed.value
  if (f.length <= 1) return
  const next = f[(cursor.value + 1) % f.length]!
  const headCard = elOf(feedGroup.value)?.querySelector('.music-card .poster')
    ?? elOf(feedGroup.value)?.querySelector('.music-card')
  const from = headCard?.getBoundingClientRect() ?? null

  // Never fly a blank: swap only once the incoming art is decoded.
  const img = new Image()
  const go = async () => {
    cursor.value = (cursor.value + 1) % f.length
    cycleKey.value++
    await nextTick()
    const posterEl = elOf(posterLink.value)
    const to = posterEl?.getBoundingClientRect()
    if (!posterEl || !from || !to || reducedMotion) return
    const sx = from.width / to.width
    const sy = from.height / to.height
    posterEl.animate([
      {
        transform: `translate(${from.left - to.left}px, ${from.top - to.top}px) scale(${sx}, ${sy})`,
        opacity: 0.85,
      },
      { transform: 'none', opacity: 1 },
    ], { duration: 620, easing: 'cubic-bezier(0.22, 1, 0.36, 1)' })
  }
  img.onload = go
  img.onerror = go
  img.src = next.art
}

const spotlight = computed<MusicRow | undefined>(() => {
  const f = feed.value
  return f.length ? f[cursor.value % f.length] : undefined
})

/** Everyone except the spotlight, next-up first, wrapping around. */
const stripRows = computed<MusicRow[]>(() => {
  const f = feed.value
  const n = f.length
  if (n <= 1) return []
  const start = (cursor.value % n + 1) % n
  return [...f.slice(start), ...f.slice(0, start)].slice(0, n - 1)
})

const bgUrl = computed(() => {
  const b = spotlight.value?.backdrop
  return b && bgOk[b] ? b : null
})
const bgFallback = computed(() => (bgUrl.value ? null : spotlight.value?.art || null))

// Ambient extension: whatever the hero shows becomes the full-page layer —
// the local copies hide via .ambient-extended and the AmbientBackdrop layer
// follows the carousel through this watcher.
const { ambientEnabled } = useAppearance()
const background = useBackground()
watch([bgUrl, bgFallback, ambientEnabled], ([bg, fb, on]) => {
  const url = bg ?? fb
  if (on && url) background.set(url, { grade: 'v2' })
  else background.clear()
}, { immediate: true })

// CTA wears the spotlight art's dominant tone (falls back to theme gold).
const tone = ref<ImageTone | null>(null)
watch(() => spotlight.value?.art ?? null, async (url) => {
  tone.value = url ? await sampleImageTone(url) : null
}, { immediate: true })
const ctaStyle = computed(() =>
  tone.value ? { background: tone.value.main, color: tone.value.ink } : undefined)

// Publish --tone on the hero root so the eyebrow + tone-glow CTA follow the
// spotlight cover's own dominant color (fill + glow stay in sync). Inherits the
// page tone until the sample lands.
const { toneFollowEnabled } = useAppearance()
const toneVars = computed<Record<string, string> | undefined>(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = tone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return toneStyleVars(t)
})

// Right-hand summary: now-playing when a track is live (the hero no longer
// takes over for playback — the Playbar owns that), else the library pulse.
const summary = computed(() => {
  const t = currentTrack.value
  if (playing.value && t) return `Now playing: ${t.title} — ${t.artist}`
  const parts: string[] = []
  if (props.albums.length) parts.push(`${props.albums.length} new album${props.albums.length === 1 ? '' : 's'}`)
  if (props.artists.length) parts.push(`${props.artists.length} artist${props.artists.length === 1 ? '' : 's'}`)
  return parts.length ? `Lately: ${parts.join(' · ')}` : ''
})
</script>

<style scoped>
/* Hero text rides the literal-dark art grade, so --oink keeps it light in
   every theme (parity with the Featured hero). */
.hero-music { position: relative; height: 100%; --oink: 233 236 242; }
.music-bg { position: absolute; inset: 0; overflow: hidden; } /* hard clip at the seam */
.music-bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center 22%;
}
.music-bg-blur {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  filter: blur(42px) saturate(1.2);
  transform: scale(1.15);
}
/* Crossfade between slides' backdrops (both frames stay absolute, so the
   outgoing image fades under the incoming one). */
.mbg-enter-active, .mbg-leave-active { transition: opacity 0.9s ease; }
.mbg-enter-from, .mbg-leave-to { opacity: 0; }
/* Literal-dark readability grade over raw artwork (CLAUDE.md exception) +
   bottom-left tone leak — mirrors HeroCanvas / the Featured hero. */
.music-grade {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background:
    linear-gradient(90deg, rgb(10 12 16 / 0.82), rgb(10 12 16 / 0.34) 42%, rgb(10 12 16 / 0.08) 72%),
    linear-gradient(to top, rgb(10 12 16 / 0.8) 0%, rgb(10 12 16 / 0.34) 26%, rgb(10 12 16 / 0.12) 58%, rgb(10 12 16 / 0.34) 100%);
}
.music-tone {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: radial-gradient(90% 70% at 8% 100%, rgb(var(--tone-rgb) / 0.18), transparent 60%);
}

.music-inner {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: stretch;
  height: 100%;
  padding: 30px 40px 16px;
  gap: 32px;
}
.music-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 14px;
}

/* ── Spotlight ── */
.music-top {
  display: flex;
  align-items: flex-start;
  gap: 28px;
  flex: 1;
  min-height: 0;
}
.music-poster {
  flex-shrink: 0;
  align-self: stretch;
  aspect-ratio: 1 / 1;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-3);
  box-shadow: 0 24px 64px rgb(var(--shade) / 0.55), 0 0 0 1px rgb(var(--ink) / 0.06);
  display: block;
}
.music-poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.music-spot {
  position: relative;
  min-width: 0;
  max-width: 620px;
  align-self: center;
}
.music-eyebrow {
  font-family: var(--font-mono);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
  margin-bottom: 10px;
  text-shadow: 0 0 12px rgb(0 0 0 / 0.5);
}
.music-title-link { color: inherit; text-decoration: none; }
.music-title-link:hover .music-title { color: var(--tone); }
.music-title {
  font-family: var(--font-display);
  font-size: clamp(2rem, 3.6vw, 2.6rem);
  font-weight: 800;
  font-variation-settings: "wdth" 115;
  letter-spacing: -0.022em;
  line-height: 1.02;
  margin: 0 0 10px;
  text-wrap: balance;
  transition: color 0.15s;
  color: rgb(var(--oink) / 0.98);
  text-shadow: 0 2px 30px rgb(0 0 0 / 0.45);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}
.music-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
/* 2.0 mono tone-tinted pill. */
.music-meta .chip {
  font: 600 10px var(--font-mono);
  letter-spacing: 0.14em;
  text-transform: uppercase;
  padding: 5px 10px;
  border-radius: 999px;
  border: 1px solid rgb(var(--tone-rgb) / 0.35);
  background: rgb(var(--tone-rgb) / 0.14);
  color: var(--tone);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  flex-shrink: 0;
}
.music-meta-sub {
  font-family: var(--font-mono);
  font-size: 12px;
  color: rgb(var(--oink) / 0.72);
  text-shadow: 0 0 12px rgb(0 0 0 / 0.55);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.music-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 16px;
}
.music-cta {
  height: 40px;
  padding: 0 20px;
  font-size: 13px;
  font-weight: 650;
  border-radius: 999px;
  gap: 6px;
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.45),
    0 0 24px rgb(var(--tone-rgb) / 0.4),
    6px 10px 36px -8px rgb(var(--tone-rgb) / 0.75);
  transition: background 0.9s cubic-bezier(0.22, 1, 0.36, 1),
              color 0.9s cubic-bezier(0.22, 1, 0.36, 1),
              box-shadow 0.15s ease, transform 0.15s ease;
}
.music-cta:hover {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 40px rgb(var(--tone-rgb) / 0.6),
    8px 14px 48px -8px rgb(var(--tone-rgb) / 0.9);
}
/* Secondary pills — tone-tinted glass (heya2.css .pill). */
.music-actions .btn-ghost {
  height: 40px;
  padding: 0 16px;
  border-radius: 999px;
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  background: rgb(var(--tone-rgb) / 0.08);
  color: rgb(var(--oink) / 0.9);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  transition: border-color 0.15s, background 0.15s, color 0.15s, transform 0.15s;
}
.music-actions .btn-ghost:hover {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  color: rgb(var(--oink));
  transform: translateY(-1px);
}
.music-sum {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-1);
  margin: 0 0 0 auto;
  text-align: right;
  flex-shrink: 0;
  align-self: flex-start;
  /* Clear the deck tab/nav cluster, now dropped below the glass topbar. */
  padding-top: 74px;
  color: rgb(var(--oink) / 0.7);
  text-shadow: 0 0 12px rgb(0 0 0 / 0.5);
}
/* Spotlight handoff crossfade. */
.spot-enter-active { transition: opacity 0.35s ease, transform 0.35s ease; }
.spot-leave-active { transition: opacity 0.18s ease; }
.spot-enter-from { opacity: 0; transform: translateY(6px); }
.spot-leave-to { opacity: 0; }

/* ── The queue ── */
.music-feed {
  position: relative; /* absolute-positioned leavers need this anchor */
  display: flex;
  gap: 14px;
  overflow-x: auto;
  scrollbar-width: none;
  flex-shrink: 0;
  /* Shadow-escape padding (layout-neutral) — sized for --shadow-card's
     full reach so nothing clips at the scroller box. */
  padding: 14px 36px 48px;
  margin: -4px -36px -42px;
  scroll-padding-left: 36px;
}
.music-feed::-webkit-scrollbar { display: none; }
.music-card {
  width: 132px;
  flex-shrink: 0;
  color: inherit;
  text-decoration: none;
}

/* Carousel choreography: the promoted head vanishes INSTANTLY — its visual
   continuation is the big cover flying out of its rect (see advance()) —
   while the rest FLIP-slide one slot left and the outgoing spotlight fades
   in at the tail. */
.strip-move { transition: transform 0.62s cubic-bezier(0.22, 1, 0.36, 1); }
.strip-enter-active { transition: opacity 0.5s ease 0.25s, transform 0.5s cubic-bezier(0.22, 1, 0.36, 1) 0.25s; }
.strip-enter-from { opacity: 0; transform: translateX(24px); }
.strip-leave-active {
  position: absolute;
  transition: opacity 0.01s linear;
}
.strip-leave-to { opacity: 0; }

@media (max-width: 900px) {
  .music-inner { padding: 18px 20px 12px; }
  .music-title { font-size: 26px; }
  .music-sum { display: none; }
  .music-poster { display: none; }
  .music-card { width: 108px; }
  .music-actions { flex-wrap: wrap; }
}
</style>
