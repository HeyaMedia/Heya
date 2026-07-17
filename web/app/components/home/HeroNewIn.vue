<template>
  <section
    class="hero-newin"
    :style="toneVars"
    @mouseenter="onHover(true)"
    @mouseleave="onHover(false)"
    @focusin="onFocus($event, true)"
    @focusout="onFocus($event, false)"
  >
    <!-- Sharp spotlight backdrop, hard-clipped at the hero's bottom edge (the
         ledger seam) with the literal-dark 2.0 grade + tone leak — parity with
         the Featured hero. The blurred site-wide underlay is the global
         AmbientBackdrop, fed a graded (v2) claim when ambient is on. -->
    <div class="newin-bg">
      <LoadingImage
        v-if="bgUrl"
        :src="bgImg.variant(bgUrl)"
        alt=""
        class="newin-bg-img"
        @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
      />
      <div class="newin-grade" />
      <div class="newin-tone" />
    </div>

    <div class="newin-inner">
      <!-- Spotlight poster: full hero height — THE thing on stage. One
           persistent element; on handoff it FLIP-flies from the promoted
           queue card's rect into place (see advance()). -->
      <NuxtLink
        v-if="spotlight"
        ref="posterLink"
        :to="spotlight.to"
        class="newin-poster"
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

      <!-- Everything else lives right of the poster: text block on top,
           the queue below. -->
      <div class="newin-main">
      <div class="newin-top">
        <Transition name="spot" mode="out-in">
          <div v-if="spotlight" :key="spotlight.key" class="newin-spot hero-ink">
            <div class="newin-eyebrow">New in your library</div>
            <NuxtLink :to="spotlight.to" class="newin-title-link">
              <h1 class="newin-title">{{ spotlight.title }}</h1>
            </NuxtLink>
            <div class="newin-meta">
              <span class="chip gold">{{ spotlight.kind }}</span>
              <span class="newin-meta-sub">{{ spotlight.sub }}</span>
              <span v-if="spotlight.time" class="newin-meta-time">{{ spotlight.time }}</span>
            </div>
            <p v-if="spotlight.description" class="newin-desc">
              {{ spotlight.description.slice(0, 220) }}{{ spotlight.description.length > 220 ? '…' : '' }}
            </p>
            <div class="newin-actions">
              <NuxtLink :to="spotlight.to" class="btn btn-primary newin-cta" :style="ctaStyle">
                {{ spotlight.kindGroup === 'artist' ? 'Go to artist' : 'Go to show' }}
                <Icon name="chevright" :size="15" />
              </NuxtLink>
            </div>
          </div>
        </Transition>

        <!-- Controls ride the deck's top-right cluster beside the mode tabs
             (same slot HeroA's navigator uses). On phones the aux slot is
             hidden, so the teleport is disabled and they render inline —
             touch users must always have a pause control. CycleControls'
             ring IS the cycle clock: its animationend advances the
             carousel, so ring and rotation can't drift. -->
        <Teleport defer :disabled="isPhone" to="#hero-deck-aux">
          <CycleControls
            v-model:paused="userPaused"
            :ring-paused="hoverPaused || focusPaused"
            :cycle-key="cycleKey"
            :duration="CYCLE_MS"
            item-label="arrival"
            :inline="isPhone"
            @prev="retreat"
            @next="advance"
          />
        </Teleport>
        <p v-if="summary" class="newin-sum">{{ summary }}</p>
      </div>

      <!-- The queue: everything waiting for its turn. As the carousel
           advances, the head card is promoted to the spotlight and the rest
           FLIP-slide one slot left; the outgoing spotlight rejoins at the
           tail. Standard MediaCards — no chrome of their own. -->
      <TransitionGroup ref="feedGroup" name="strip" tag="div" class="newin-feed">
        <NuxtLink
          v-for="(ev, i) in stripRows"
          :key="ev.key"
          :to="ev.to"
          class="newin-card card-tile"
        >
          <MediaCard
            :idx="i"
            :src="ev.art"
            :title="ev.title"
            :subtitle="ev.time || ev.sub"
            :badge-tl="ev.kindShort"
          />
        </NuxtLink>
      </TransitionGroup>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
// "New" — the library pulse as a carousel. The newest arrivals queue along
// the bottom; every CYCLE_MS the head of the queue takes the spotlight (big
// poster + blurb + tone-matched CTA, full-page backdrop) and the strip
// slides left, the outgoing spotlight rejoining at the tail. Hovering
// anywhere in the hero pauses the clock. Feeds entirely off data the page
// already fetched.
import type { MediaItem } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'

export interface RecentTVEntry {
  media_item_id: number
  media_item_public_id?: string
  title: string
  slug: string
  kind: 'series' | 'season' | 'episodes' | 'episode'
  season_number: number
  episode_number: number
  episode_title?: string
  season_count: number
  episode_count: number
  added_at: string
  // Kind-resolved server-side: show desc / season overview / episode overview.
  description?: string
}

const props = defineProps<{
  tv: RecentTVEntry[]
  albums: (MediaItem & { sub?: string })[]
  artists: (MediaItem & { sub?: string })[]
}>()

function entrySub(e: RecentTVEntry): string {
  switch (e.kind) {
    case 'series': return e.season_count > 1 ? `${e.season_count} seasons` : `${e.episode_count} episode${e.episode_count === 1 ? '' : 's'}`
    case 'season': return `Season ${e.season_number} · ${e.episode_count} episode${e.episode_count === 1 ? '' : 's'}`
    case 'episodes': return `Season ${e.season_number} · ${e.episode_count} new episodes`
    case 'episode': {
      const code = `S${String(e.season_number).padStart(2, '0')}E${String(e.episode_number).padStart(2, '0')}`
      return e.episode_title ? `${code} · ${e.episode_title}` : code
    }
  }
}

function kindLabel(e: RecentTVEntry): string {
  switch (e.kind) {
    case 'series': return 'New show'
    case 'season': return `New season ${e.season_number}`
    case 'episodes': return 'New episodes'
    case 'episode': return 'New episode'
  }
}

function relTime(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime()
  const h = Math.floor(ms / 3_600_000)
  if (h < 1) return 'just now'
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  if (d < 7) return `${d}d ago`
  return `${Math.floor(d / 7)}w ago`
}

interface FeedRow {
  key: string
  to: string
  art: string
  backdrop: string | null
  title: string
  sub: string
  kind: string
  kindShort: string
  kindGroup: 'tv' | 'artist'
  time: string
  description: string
}

const feed = computed<FeedRow[]>(() => {
  const rows: FeedRow[] = []
  // Biggest event leads: whole new show, then a new season, then everything
  // else in arrival order.
  const tv = [...props.tv].sort((a, b) => {
    const rank = (e: RecentTVEntry) => (e.kind === 'series' ? 0 : e.kind === 'season' ? 1 : 2)
    return rank(a) - rank(b) || new Date(b.added_at).getTime() - new Date(a.added_at).getTime()
  })
  for (const e of tv.slice(0, 9)) {
    const ref = { id: e.media_item_id, public_id: e.media_item_public_id }
    rows.push({
      key: `tv-${e.media_item_id}-${e.kind}-${e.season_number}-${e.episode_number}-${e.added_at}`,
      to: `/tv/${e.slug}`,
      art: usePosterUrl(ref) ?? '',
      backdrop: useBackdropUrl(ref) || null,
      title: e.title,
      sub: entrySub(e),
      kind: kindLabel(e),
      kindShort: e.kind === 'series' ? 'SHOW' : e.kind === 'season' ? 'SEASON' : 'EPISODE',
      kindGroup: 'tv',
      time: relTime(e.added_at),
      description: e.description ?? '',
    })
  }
  for (const a of props.artists.slice(0, 3)) {
    rows.push({
      key: `artist-${a.id}`,
      to: mediaUrl(a),
      art: usePosterUrl(a) ?? '',
      backdrop: useBackdropUrl(a) || null,
      title: a.title,
      sub: (a as MediaItem & { sub?: string }).sub ?? '',
      kind: 'New artist',
      kindShort: 'ARTIST',
      kindGroup: 'artist',
      time: '',
      description: '',
    })
  }
  return rows.slice(0, 10)
})

// ── Carousel clock ──
// CycleControls' ring IS the timer (see that component): its animationend
// calls advance(); every advance/retreat re-keys the ring, restarting the
// countdown. Pausing (hover, keyboard focusin via ring-paused, or the
// sticky button via v-model) freezes it — clock and indicator are the same
// thing, so they can't drift.
const CYCLE_MS = 15_000
const cursor = ref(0)
const cycleKey = ref(0)
// Independent pause sources — composed, never overwriting each other: a
// mouseleave must not cancel a keyboard-focus pause and vice versa.
const hoverPaused = ref(false)
const focusPaused = ref(false)
const userPaused = ref(false)
// Still needed locally: skips the shared-element FLIP flight in advance().
const reducedMotion = import.meta.client
  ? window.matchMedia('(prefers-reduced-motion: reduce)').matches
  : false
const { isPhone } = useViewport()

// Touch guards: a tap fires a synthetic mouseenter with no mouseleave, and
// focus stays on a tapped button forever — either would wedge `paused` on.
// Hover-pause only applies to real hover devices, and the control cluster
// itself never focus-pauses (it IS the pause mechanism; trapping the clock
// while its own buttons hold focus made resume impossible on phones).
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
// The promoted card doesn't fade — it BECOMES the spotlight poster: measure
// its art rect before the swap, preload the art, advance, then fly the big
// poster from the card's rect into place (FLIP via the Web Animations API).
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
  const headCard = elOf(feedGroup.value)?.querySelector('.newin-card .poster')
    ?? elOf(feedGroup.value)?.querySelector('.newin-card')
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

const spotlight = computed<FeedRow | undefined>(() => {
  const f = feed.value
  return f.length ? f[cursor.value % f.length] : undefined
})

/** Everyone except the spotlight, next-up first, wrapping around. */
const stripRows = computed<FeedRow[]>(() => {
  const f = feed.value
  const n = f.length
  if (n <= 1) return []
  const start = (cursor.value % n + 1) % n
  return [...f.slice(start), ...f.slice(0, start)].slice(0, n - 1)
})

const bgUrl = computed(() => spotlight.value?.backdrop ?? null)

// Ambient extension: the spotlight's backdrop becomes the full-page layer —
// the local `.newin-bg-img` hides via .ambient-extended and the
// AmbientBackdrop layer follows the carousel through this watcher.
const { ambientEnabled } = useAppearance()
const background = useBackground()
// Local copy renders the EXACT variant AmbientBackdrop loads (w=1920 q=70,
// pre-resolved, no width/quality props → no srcset) so sharp hero and blurred
// underlay share one cache entry and paint together — see HeroCanvas.vue.
const bgImg = useBackgroundImageTools()
watch([bgUrl, ambientEnabled], ([url, on]) => {
  if (on && url) background.set(url, { grade: 'v2' })
  else background.clear()
}, { immediate: true })

// CTA wears the spotlight backdrop's dominant tone (falls back to theme gold).
const tone = ref<ImageTone | null>(null)
watch(bgUrl, async (url) => {
  tone.value = url ? await sampleImageTone(bgImg.thumb(url)) : null
}, { immediate: true })
const ctaStyle = computed(() =>
  tone.value ? { background: tone.value.main, color: tone.value.ink } : undefined)

// Publish --tone on the hero root so the eyebrow + tone-glow CTA follow the
// spotlight's own dominant color (fill + glow stay in sync). Inherits the page
// tone until the sample lands.
const { toneFollowEnabled } = useAppearance()
const toneVars = computed<Record<string, string> | undefined>(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = tone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return toneStyleVars(t)
})

const summary = computed(() => {
  const parts: string[] = []
  const eps = props.tv.filter(e => e.kind === 'episode' || e.kind === 'episodes').length
  const seasons = props.tv.filter(e => e.kind === 'season').length
  const shows = props.tv.filter(e => e.kind === 'series').length
  if (shows) parts.push(`${shows} new show${shows === 1 ? '' : 's'}`)
  if (seasons) parts.push(`${seasons} new season${seasons === 1 ? '' : 's'}`)
  if (eps) parts.push(`${eps} episode drop${eps === 1 ? '' : 's'}`)
  if (props.artists.length) parts.push(`${props.artists.length} artist${props.artists.length === 1 ? '' : 's'}`)
  return parts.length ? `Lately: ${parts.join(' · ')}` : ''
})
</script>

<style scoped>
/* Hero text rides the literal-dark art grade, so --oink keeps it light in
   every theme (parity with the Featured hero). */
.hero-newin { position: relative; height: 100%; --oink: 233 236 242; }
.newin-bg { position: absolute; inset: 0; overflow: hidden; } /* hard clip at the seam */
.newin-bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center 22%;
  transition: opacity 0.6s ease;
}
/* Literal-dark readability grade over raw artwork (CLAUDE.md exception) +
   bottom-left tone leak — mirrors HeroCanvas / the Featured hero. */
.newin-grade {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background:
    linear-gradient(90deg, rgb(10 12 16 / 0.82), rgb(10 12 16 / 0.34) 42%, rgb(10 12 16 / 0.08) 72%),
    linear-gradient(to top, rgb(10 12 16 / 0.8) 0%, rgb(10 12 16 / 0.34) 26%, rgb(10 12 16 / 0.12) 58%, rgb(10 12 16 / 0.34) 100%);
}
.newin-tone {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: radial-gradient(90% 70% at 8% 100%, rgb(var(--tone-rgb) / 0.18), transparent 60%);
}

.newin-inner {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: stretch;
  height: 100%;
  padding: 30px 40px 16px;
  gap: 32px;
}
.newin-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 14px;
}

/* ── Spotlight ── */
.newin-top {
  display: flex;
  align-items: flex-start;
  gap: 28px;
  flex: 1;
  min-height: 0;
}
.newin-poster {
  flex-shrink: 0;
  align-self: stretch;
  aspect-ratio: 2 / 3;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-3);
  box-shadow: 0 24px 64px rgb(var(--shade) / 0.55), 0 0 0 1px rgb(var(--ink) / 0.06);
  display: block;
}
.newin-poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.newin-spot {
  position: relative;
  min-width: 0;
  max-width: 620px;
  align-self: center;
}
.newin-eyebrow {
  font-family: var(--font-mono);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
  margin-bottom: 10px;
  text-shadow: 0 0 12px rgb(0 0 0 / 0.5);
}
.newin-title-link { color: inherit; text-decoration: none; }
.newin-title-link:hover .newin-title { color: var(--tone); }
.newin-title {
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
.newin-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
/* 2.0 mono tone-tinted pill (heya2.css-flavoured). */
.newin-meta .chip {
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
.newin-meta-sub {
  font-family: var(--font-mono);
  font-size: 12px;
  color: rgb(var(--oink) / 0.72);
  text-shadow: 0 0 12px rgb(0 0 0 / 0.55);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.newin-meta-time {
  font-family: var(--font-mono);
  font-size: 12px;
  color: rgb(var(--oink) / 0.55);
  text-shadow: 0 0 12px rgb(0 0 0 / 0.55);
  flex-shrink: 0;
}
.newin-desc {
  font-size: 13.5px;
  line-height: 1.55;
  color: rgb(var(--oink) / 0.78);
  max-width: 560px;
  margin: 10px 0 0;
  text-shadow: 0 0 12px rgb(0 0 0 / 0.5);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}
.newin-actions { margin-top: 16px; }
.newin-cta {
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
.newin-cta:hover {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 40px rgb(var(--tone-rgb) / 0.6),
    8px 14px 48px -8px rgb(var(--tone-rgb) / 0.9);
}
.newin-sum {
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
.newin-feed {
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
.newin-feed::-webkit-scrollbar { display: none; }
.newin-card {
  width: 118px;
  flex-shrink: 0;
  color: inherit;
  text-decoration: none;
}

/* Carousel choreography: the promoted head vanishes INSTANTLY — its visual
   continuation is the big poster flying out of its rect (see advance()) —
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
  .newin-inner { padding: 18px 20px 12px; }
  .newin-title { font-size: 26px; }
  .newin-sum { display: none; }
  .newin-poster { display: none; }
  .newin-card { width: 100px; }
}
</style>
