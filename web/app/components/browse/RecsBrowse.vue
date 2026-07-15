<!--
  RecsBrowse — the full "Recommendations" category page for /movies/recommendations
  and /tv/recommendations. Unlike TagBrowse (a flat tag list), this surfaces the
  personalized engine as a STEERABLE grid: pick a genre and/or a rating floor and
  the engine re-ranks by your taste within that constraint (the "horror binge").
  Each tile shows why it was picked. Non-ML today; the embedding engine plugs in
  behind a config flag without changing this surface.

  Heya 2.0 dress (2026-07-15): LibHead + on-canvas LedgerStrip head, a spotlight-
  style borderless NL search over a hairline, the steer controls reorganized into
  the 2.0 control grammar (mono group labels + hairline rule + tone-tinted facet
  chips), tone-glow AI cluster, and the results sectioned under a SectionHeader.
  The whole surface tone-follows the ambient pool (--tone / --tone-rgb published on
  the root, exactly the --pb-accent precedent).
-->
<template>
  <div class="rb-view scroll" :style="toneVars">
    <LibHead :title="title" :crumbs="crumbs" />
    <LedgerStrip v-if="ledgerCells.length" :cells="ledgerCells" canvas />

    <div class="rb-pad">
      <!-- Spotlight-style natural-language search: a borderless input riding a
           hairline, sparkle glyph, and a tone-glow Ask AI pill on the right. -->
      <div class="rb-search" :class="{ focused: searchFocused }">
        <Icon name="sparkle" :size="16" class="rb-search-icon" />
        <input
          v-model="nlQuery"
          type="text"
          class="rb-search-input"
          aria-label="Describe what you're in the mood for"
          :placeholder="searchPlaceholder"
          @focus="searchFocused = true"
          @blur="searchFocused = false"
          @keydown.enter="askAI"
        >
        <button v-if="nlQuery" class="rb-search-clear" @click="clearSearch">Clear</button>
        <button
          v-if="aiReady"
          class="rb-ai-btn"
          :disabled="nlQuery.trim().length < 2 || aiPending"
          @click="askAI"
        >
          <Icon name="sparkle" :size="13" />
          {{ aiPending ? 'Curating…' : 'Ask AI' }}
        </button>
      </div>

      <!-- Steer panel — 2.0 control grammar: mono group labels, a hairline top
           rule, tone-tinted facet chips (single-select genre) + segmented rating
           floor. Hidden while a semantic/AI search owns the grid. -->
      <div v-if="!searching" class="rb-steer">
        <div class="rb-steer-row">
          <span class="rb-steer-label">Genre</span>
          <div class="rb-chips">
            <button class="rb-chip" :class="{ on: genre === '' }" :aria-pressed="genre === ''" @click="genre = ''">Any</button>
            <button
              v-for="g in genreOptions"
              :key="g"
              class="rb-chip"
              :class="{ on: genre === g }"
              :aria-pressed="genre === g"
              @click="genre = genre === g ? '' : g"
            >{{ g }}</button>
          </div>
        </div>

        <div class="rb-steer-row rb-steer-rating">
          <span class="rb-steer-label">Min rating</span>
          <div class="rb-seg">
            <button
              v-for="opt in ratingOptions"
              :key="opt.value"
              :class="{ active: minRating === opt.value }"
              :aria-pressed="minRating === opt.value"
              @click="minRating = opt.value"
            >
              {{ opt.label }}
            </button>
          </div>
          <button v-if="genre || minRating" class="rb-clear" @click="reset">
            <Icon name="undo" :size="12" />
            Reset
          </button>
        </div>
      </div>

      <div v-if="aiPending" class="rb-note rb-ai-note">
        Curating picks for “{{ aiQ }}” — the AI is searching the library and choosing what fits…
      </div>
      <div v-else-if="aiActive && aiFailed" class="rb-note">
        AI curation failed ({{ aiErrorMsg }}) — showing plain semantic matches instead.
      </div>
      <div v-else-if="searching && !mlReady" class="rb-note">
        Natural-language search needs the embedding engine — enable it (and let the
        model finish downloading) in
        <NuxtLink to="/settings/recommendations" class="rb-link">Settings → Recommendations</NuxtLink>.
      </div>
      <div v-else-if="!searching && !loading && !hasSignal" class="rb-note">
        Heart or watch a few titles and this personalizes to your taste — for now, showing the highest-rated picks.
      </div>

      <!-- AI curation cluster — its own header for the AI grid (tone-lit). -->
      <div v-if="aiShowing" class="rb-ai-summary">
        <div class="rb-ai-summary-head">
          <div class="rb-ai-kicker">
            <Icon name="sparkle" :size="12" />
            <span>AI curation</span>
            <span class="rb-ai-count">{{ aiItemCount }} {{ aiItemCount === 1 ? 'match' : 'matches' }}</span>
          </div>
          <button class="rb-ai-reroll" :disabled="aiPending" @click="rerollAI">
            <Icon name="refresh" :size="12" />
            {{ aiPending ? 'Curating…' : 'Re-roll' }}
          </button>
        </div>
        <p v-if="aiNote" class="rb-ai-summary-note">{{ aiNote }}</p>
        <div class="rb-ai-meta" :title="aiProbesTitle">AI-curated · {{ aiMeta }}</div>
      </div>

      <!-- Non-AI results ride under a mono sec-head with a tone count. -->
      <SectionHeader
        v-if="!aiShowing && (displayItems.length || displayLoading)"
        :title="resultsHeading.title"
        :subtitle="resultsHeading.count"
      />

      <div v-if="displayLoading" class="grid-posters">
        <div v-for="i in 12" :key="i" class="grid-tile">
          <div class="poster" style="aspect-ratio: 2/3; background: var(--bg-3); animation: pulse 1.5s infinite" />
        </div>
      </div>

      <div v-else-if="displayItems.length" class="grid-posters" :class="{ 'rb-ai-grid': aiShowing }">
        <AppContextMenu v-for="(item, i) in displayItems" :key="item.id" :items="contextItemsFor(item)">
          <NuxtLink :to="mediaUrl(item as any)" class="grid-tile card-tile rb-tile">
            <MediaCard
              :idx="i"
              :src="usePosterUrl(item)"
              aspect="2/3"
              :title="item.title"
              :subtitle="item.year"
            />
            <div v-if="item.reason" class="rb-reason" :title="item.reason">
              <Icon name="sparkle" :size="10" class="rb-reason-mark" />
              <span>{{ item.reason }}</span>
            </div>
          </NuxtLink>
        </AppContextMenu>
      </div>

      <!-- Endless steer grid: crossing this sentinel appends the next page of
           picks until the engine's pool runs dry. Hidden for semantic/AI
           results, which are one-shot lists. -->
      <div
        v-if="!searching && !aiShowing && recsQuery.hasNextPage.value"
        ref="recsSentinel"
        class="rb-sentinel"
        aria-hidden="true"
      >
        <span class="rb-sentinel-spin" />
      </div>

      <div v-else-if="!(searching && !mlReady)" class="rb-empty">
        {{ aiShowing ? 'The AI found nothing in the library that fits — try rewording the ask.'
          : searching ? 'No matches for that description.'
            : 'Nothing matches this steer — try another genre or lower the rating floor.' }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import type { Crumb } from '~/components/library/LibHead.vue'
import { useInfiniteQuery, useQuery, useQueryCache } from '@pinia/colada'
import { movieUserStateQuery, seriesUserStateQuery, userListsQuery as userListsOptions } from '~/queries/catalog'
import { forYouInfinite } from '~/queries/rails'

const props = defineProps<{ section: 'movie' | 'tv' }>()

// Hoisted per the useNuxtApp gotcha — never resolve $heya inside async bodies.
const { $heya } = useNuxtApp()
const queryClient = useQueryCache()
const invalidateContinueWatching = useInvalidateContinueWatching()
const { buildItems: buildCardCtxItems } = useCardContextItems()

type RecItem = { id: number; title: string; slug: string; year?: string; media_type: string; reason?: string; available: boolean }

const genre = ref('')
const minRating = ref(0)
const searchFocused = ref(false)

// ── Heya 2.0 head + tone plumbing ───────────────────────────────────────
// Archivo LibHead + mono breadcrumb (MOVIES · RECOMMENDATIONS).
const title = 'Recommendations'
const crumbs = computed<Crumb[]>(() => [
  props.section === 'movie'
    ? { label: 'Movies', to: '/movies' }
    : { label: 'TV', to: '/tv' },
  { label: 'Recommendations' },
])

// The whole surface tone-follows the ambient pool (the --pb-accent precedent):
// eyebrow, ledger tone cells, section count, active chips and the AI pills all
// glide with the backdrop. Falls back to the theme accent when ambient/tone-
// follow is off (--tone/--tone-rgb default to the accent in :root).
const bgTone = useBackgroundTone()
const { toneFollowEnabled } = useAppearance()
const toneVars = computed<Record<string, string> | undefined>(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = bgTone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return { '--tone': t.main, '--tone-rgb': m.slice(0, 3).join(' '), '--tone-ink': t.ink }
})

const ratingOptions = [
  { label: 'Any', value: 0 },
  { label: '6+', value: 6 },
  { label: '7+', value: 7 },
  { label: '8+', value: 8 },
]

// Available genres for the steer chips, most-common first.
const genresQuery = useQuery({
  key: ['genres-all'],
  query: async () => (await $heya('/api/genres')) as { genre: string; count: number }[],
  staleTime: 1000 * 60 * 30,
})
const genreOptions = computed(() =>
  [...(genresQuery.data.value ?? [])].sort((a, b) => b.count - a.count).map(g => g.genre).slice(0, 30),
)

// Reactive key — changing genre/minRating pages a fresh steer. Infinite:
// scrolling the grid appends deeper picks until the engine's re-rank pool
// (~200) runs dry.
const recsQuery = useInfiniteQuery(() => forYouInfinite({
  section: props.section,
  genre: genre.value || undefined,
  minRating: minRating.value || undefined,
}))
const loadMoreRecs = railLoadMore(recsQuery)

const items = computed<RecItem[]>(() =>
  (recsQuery.data.value?.pages ?? []).flatMap(p => p.items as RecItem[]))
const hasSignal = computed(() => recsQuery.data.value?.pages[0]?.has_signal ?? true)
const loading = computed(() => recsQuery.isPending.value)

// Sentinel-driven append: only the plain recs grid pages (semantic + AI
// results are single-shot lists).
const recsSentinel = ref<HTMLElement | null>(null)
useIntersectionObserver(recsSentinel, ([entry]) => {
  if (entry?.isIntersecting) loadMoreRecs()
}, { rootMargin: '600px' })

// Natural-language "vibe" search (ML engine). When a query is active it replaces
// the facet-ranked grid with semantic KNN hits.
const nlQuery = ref('')
const activeQ = ref('')
let debTimer: ReturnType<typeof setTimeout> | null = null
watch(nlQuery, (v) => {
  if (debTimer) clearTimeout(debTimer)
  debTimer = setTimeout(() => { activeQ.value = v.trim() }, 400)
})
const semanticQuery = useQuery({
  key: () => ['semantic', props.section, activeQ.value],
  query: async () => (await $heya('/api/search/semantic', {
    query: { q: activeQ.value, type: props.section, limit: 60 },
  })) as { items: RecItem[]; ml_ready: boolean },
  enabled: computed(() => activeQ.value.length > 1),
  staleTime: 1000 * 60 * 5,
})
const searching = computed(() => activeQ.value.length > 1)
const mlReady = computed(() => semanticQuery.data.value?.ml_ready ?? true)

// AI curation — explicit (Enter / button), never keystroke-triggered: it costs
// two LLM round-trips. Availability comes from the shape-minimal /api/ai/ready.
const aiReadyQuery = useQuery({
  key: ['ai-ready'],
  query: async () => (await $heya('/api/ai/ready')) as { ready: boolean; mode: string },
  staleTime: 1000 * 60 * 10,
})
const aiReady = computed(() => aiReadyQuery.data.value?.ready === true)

type AIRecResult = { items: RecItem[]; note?: string; probes?: string[]; model?: string; mode: string; duration_ms: number }
const aiQ = ref('')
const aiQuery = useQuery({
  key: () => ['ai-recs', props.section, aiQ.value],
  query: async () => (await $heya('/api/ai/recommend', {
    method: 'POST',
    body: { query: aiQ.value, type: props.section } as any,
  })) as AIRecResult,
  enabled: computed(() => aiQ.value.length > 1),
  staleTime: 1000 * 60 * 10,
  retry: 0, // expensive call — never auto-retry
})

function askAI() {
  const q = nlQuery.value.trim()
  if (!aiReady.value || q.length < 2 || aiPending.value) return
  if (aiQ.value === q) {
    // Same ask again — the ref won't change, so refetch explicitly. This is
    // both the retry path after a failure and a deliberate re-roll.
    aiQuery.refetch()
    return
  }
  aiQ.value = q
}
function rerollAI() {
  if (!aiPending.value) aiQuery.refetch()
}
function clearSearch() {
  nlQuery.value = ''
  aiQ.value = ''
}

// AI results own the grid while the input still says what was asked; editing
// the text falls back to live semantic matches until the next Enter.
const aiActive = computed(() => aiQ.value.length > 1 && nlQuery.value.trim() === aiQ.value)
// isLoading (not isPending) so a retry-after-error shows "Curating…" again
// instead of the stale error note.
const aiPending = computed(() => aiActive.value && aiQuery.isLoading.value)
const aiFailed = computed(() => aiQuery.status.value === 'error' && !aiQuery.isLoading.value)
const aiShowing = computed(() => aiActive.value && !!aiQuery.data.value && aiQuery.status.value !== 'error')
const aiErrorMsg = computed(() => {
  const e = aiQuery.error.value as any
  return e?.data?.detail || e?.message || 'request failed'
})
const aiMeta = computed(() => {
  const d = aiQuery.data.value
  return d ? `${d.model || d.mode} · ${(d.duration_ms / 1000).toFixed(1)}s` : ''
})
const aiNote = computed(() => aiQuery.data.value?.note ?? '')
const aiItemCount = computed(() => aiQuery.data.value?.items.length ?? 0)
const aiProbesTitle = computed(() => {
  const probes = aiQuery.data.value?.probes
  return probes?.length ? `Searched: ${probes.join(' · ')}` : ''
})
const searchPlaceholder = computed(() => aiReady.value
  ? 'Describe what you\'re in the mood for… press Enter and the AI curates'
  : 'Describe what you\'re in the mood for…  e.g. “a dark psychological thriller”')

const displayItems = computed(() => {
  if (aiShowing.value) return aiQuery.data.value?.items ?? []
  if (searching.value) return semanticQuery.data.value?.items ?? []
  return items.value
})
const displayLoading = computed(() => {
  if (aiPending.value) return true
  return searching.value ? semanticQuery.isPending.value : loading.value
})

// Section header for the non-AI grid — title by mode, tone count.
const resultsHeading = computed(() => {
  if (searching.value) {
    const n = displayItems.value.length
    return { title: 'Semantic search', count: `${n} ${n === 1 ? 'match' : 'matches'}` }
  }
  const n = items.value.length
  const more = recsQuery.hasNextPage.value ? '+' : ''
  return { title: 'For you', count: `${n}${more} ${n === 1 ? 'title' : 'titles'}` }
})

// Signature ledger — user-facing facts only (PLAN cardinal rule 2): whether the
// ranking is personalized, how many genres you can steer by, and whether the AI
// curator is available. No pool count (infinite/unknowable) and no ops
// telemetry. Fades in once the recs engine has answered.
const ledgerCells = computed<LedgerCell[]>(() => {
  if (loading.value) return []
  const cells: LedgerCell[] = [
    { k: 'Ranking', v: hasSignal.value ? 'Your taste' : 'Top rated', tone: hasSignal.value },
  ]
  const gc = genreOptions.value.length
  if (gc) cells.push({ k: 'Genres', v: String(gc), sub: 'to steer' })
  cells.push({ k: 'AI curator', v: aiReady.value ? 'Ready' : 'Off', tone: aiReady.value })
  return cells
})

const userListsQuery = useQuery(userListsOptions())
const moviesStateQuery = useQuery(() => ({ ...movieUserStateQuery(), enabled: props.section === 'movie' }))
const seriesStateQuery = useQuery(() => ({ ...seriesUserStateQuery(), enabled: props.section === 'tv' }))

const watchedSet = ref<Set<number>>(new Set())
const favoritedSet = ref<Set<number>>(new Set())

watchEffect(() => {
  if (props.section === 'movie') {
    watchedSet.value = new Set(moviesStateQuery.data.value?.watched ?? [])
    favoritedSet.value = new Set(moviesStateQuery.data.value?.favorited ?? [])
    return
  }
  watchedSet.value = new Set((seriesStateQuery.data.value?.shows ?? [])
    .filter(s => s.total_episodes > 0 && s.watched_episodes >= s.total_episodes)
    .map(s => s.media_item_id))
  favoritedSet.value = new Set(seriesStateQuery.data.value?.favorited ?? [])
})

function contextItemsFor(item: RecItem) {
  return buildCardCtxItems(item as unknown as MediaItem, {
    watchedSet: watchedSet.value,
    favoritedSet: favoritedSet.value,
    userLists: userListsQuery.data.value ?? [],
    onToggleWatched: async (id: number, watched: boolean) => {
      try {
        await $heya('/api/me/watched/media/{id}', {
          method: 'POST',
          path: { id },
          body: { watched } as any,
        })
        const next = new Set(watchedSet.value)
        if (watched) next.add(id)
        else next.delete(id)
        watchedSet.value = next
        invalidateContinueWatching()
        queryClient.invalidateQueries({ key: ['me', 'state'] })
      } catch { /* ignore */ }
    },
    onToggleFavorite: async (id: number, favorited: boolean) => {
      try {
        await $heya('/api/me/favorites', {
          method: 'POST',
          body: { entity_type: 'media_item', entity_id: id } as any,
        })
        const next = new Set(favoritedSet.value)
        if (favorited) next.add(id)
        else next.delete(id)
        favoritedSet.value = next
        queryClient.invalidateQueries({ key: ['me', 'state'] })
      } catch { /* ignore */ }
    },
    onAddToList: async (listId: number, mediaId: number) => {
      try {
        await $heya('/api/me/lists/{id}/items', {
          method: 'POST',
          path: { id: listId },
          body: { media_item_id: mediaId } as any,
        })
      } catch { /* ignore */ }
    },
  })
}

function reset() {
  genre.value = ''
  minRating.value = 0
}
</script>

<style scoped>
.rb-view { height: 100%; }
/* Content gutter mirrors BrowseView's .rec-pad so the Browse and Recommendations
   landings share the exact left edge under the shared LibHead + LedgerStrip. */
.rb-pad { padding: 22px 32px 80px; }

/* ── Spotlight NL search — a borderless input riding a hairline (not a boxed
   field), sparkle glyph, tone-glow Ask AI pill. The hairline warms to --tone on
   focus so the whole row lifts with the ambient. ──────────────────────────── */
.rb-search {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 2px 14px;
  margin-bottom: 20px;
  border-bottom: 1px solid var(--hair-strong);
  transition: border-color 0.2s ease;
}
.rb-search.focused { border-bottom-color: rgb(var(--tone-rgb) / 0.55); }
.rb-search-icon { color: var(--tone); flex-shrink: 0; transition: color 0.9s cubic-bezier(0.22, 1, 0.36, 1); }
.rb-search-input {
  flex: 1; min-width: 0;
  background: transparent; border: 0; outline: none;
  color: var(--fg-0); font-size: 17px; font-weight: 500;
  text-shadow: 0 1px 2px var(--bg-1);
}
.rb-search-input::placeholder { color: var(--fg-3); font-weight: 400; }
.rb-search-clear {
  flex-shrink: 0;
  font-family: var(--font-mono); font-size: 10px; font-weight: 600;
  letter-spacing: 0.1em; text-transform: uppercase;
  color: var(--fg-3); padding: 5px 8px; cursor: pointer;
  transition: color 0.12s;
}
.rb-search-clear:hover { color: var(--fg-0); }

/* Ask AI — tone-glow filled pill (heya2 .btn-play recipe): the fill + glow ride
   --tone so they glide as the ambient rotates. */
.rb-ai-btn {
  flex-shrink: 0;
  display: inline-flex; align-items: center; gap: 7px;
  padding: 9px 17px; border: 0; border-radius: 999px; cursor: pointer;
  background: var(--tone); color: var(--tone-ink, var(--accent-ink));
  font: 650 13px var(--font-sans); letter-spacing: 0.01em; white-space: nowrap;
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.45),
    0 0 22px rgb(var(--tone-rgb) / 0.38),
    5px 9px 30px -8px rgb(var(--tone-rgb) / 0.7);
  transition: transform 0.15s ease, box-shadow 0.15s ease, opacity 0.12s ease, filter 0.12s ease,
              background 0.9s cubic-bezier(0.22, 1, 0.36, 1), color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
.rb-ai-btn:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 36px rgb(var(--tone-rgb) / 0.55),
    7px 12px 40px -8px rgb(var(--tone-rgb) / 0.85);
}
.rb-ai-btn:disabled { opacity: 0.5; filter: saturate(0.5); cursor: default; box-shadow: 0 0 0 1px rgb(var(--tone-rgb) / 0.28); }

/* ── Steer panel — 2.0 control grammar ──────────────────────────────────── */
.rb-steer {
  display: flex;
  flex-direction: column;
  gap: 14px;
  margin-bottom: 24px;
}
.rb-steer-row { display: flex; align-items: flex-start; gap: 16px; }
.rb-steer-rating { align-items: center; }
.rb-steer-label {
  flex: 0 0 78px;
  padding-top: 5px;
  font: 600 10px var(--font-mono); letter-spacing: 0.2em; text-transform: uppercase;
  color: rgb(var(--ink) / 0.45);
  text-shadow: 0 0 10px var(--bg-1), 0 1px 2px var(--bg-1);
}
.rb-steer-rating .rb-steer-label { padding-top: 0; }
.rb-chips { display: flex; flex-wrap: wrap; gap: 7px; min-width: 0; }

/* Facet chip — mono, hairline, glass so it reads over ambient art; the active
   one wears the --tone tint (heya2 .seasontabs.on recipe). */
.rb-chip {
  font-family: var(--font-mono); font-size: 11px; letter-spacing: 0.04em;
  color: var(--fg-1);
  padding: 6px 13px; border-radius: 999px;
  border: 1px solid var(--border);
  background: color-mix(in oklab, var(--bg-2) 72%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  cursor: pointer;
  transition: color 0.15s, border-color 0.15s, background 0.15s, box-shadow 0.15s;
}
.rb-chip:hover { color: var(--fg-0); border-color: var(--border-strong); }
.rb-chip.on {
  color: var(--tone);
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.12);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.16);
}

/* Rating segmented — glassed so it holds over ambient art, tone-tinted active. */
.rb-seg {
  display: inline-flex; gap: 2px; padding: 2px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  border-radius: 999px;
  box-shadow: var(--shadow-el);
}
.rb-seg button {
  padding: 5px 13px; border-radius: 999px;
  font: 600 11px var(--font-mono); letter-spacing: 0.06em;
  color: var(--fg-2); cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}
.rb-seg button:hover { color: var(--fg-0); }
.rb-seg button.active {
  background: rgb(var(--tone-rgb) / 0.14);
  color: var(--tone);
  box-shadow: 0 0 14px rgb(var(--tone-rgb) / 0.14);
}

.rb-clear {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 12px; border-radius: 999px;
  font-family: var(--font-mono); font-size: 10px; font-weight: 600;
  letter-spacing: 0.1em; text-transform: uppercase;
  color: var(--fg-2); border: 1px solid var(--border);
  background: color-mix(in oklab, var(--bg-2) 72%, transparent);
  backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px);
  cursor: pointer; transition: color 0.15s, border-color 0.15s;
}
.rb-clear:hover { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); }

/* ── Notes ──────────────────────────────────────────────────────────────── */
.rb-note {
  font-size: 13px; color: var(--fg-2);
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--hair); border-radius: var(--r-md);
  padding: 11px 15px; margin-bottom: 20px;
  box-shadow: var(--shadow-el);
}
.rb-ai-note { color: var(--tone); border-color: rgb(var(--tone-rgb) / 0.35); }
.rb-empty { padding: 60px 0; text-align: center; color: var(--fg-3); font-size: 14px; }

/* ── AI curation cluster — tone-lit summary card (heya2 .upnext-ish). ──────── */
.rb-ai-summary {
  background: linear-gradient(110deg, rgb(var(--tone-rgb) / 0.06), rgb(var(--ink) / 0.018) 55%);
  border: 1px solid rgb(var(--tone-rgb) / 0.28);
  border-radius: var(--r-md);
  padding: 14px 16px;
  margin-bottom: 22px;
  box-shadow: 0 0 22px rgb(var(--tone-rgb) / 0.08), var(--shadow-el);
}
.rb-ai-summary-head {
  display: flex; align-items: center; justify-content: space-between; gap: 16px;
  margin-bottom: 9px;
}
.rb-ai-kicker {
  display: flex; align-items: center; gap: 7px;
  color: var(--tone); font-family: var(--font-mono); font-size: 10px;
  font-weight: 700; letter-spacing: 0.16em; text-transform: uppercase;
}
.rb-ai-count {
  color: var(--fg-3); font-weight: 500; letter-spacing: 0.04em;
  text-transform: none;
}
/* Re-roll — tone-tinted ghost pill (heya2 .pill). */
.rb-ai-reroll {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 11px; border-radius: 999px;
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  color: rgb(var(--ink) / 0.9); background: rgb(var(--tone-rgb) / 0.08);
  font: 550 11px var(--font-mono); letter-spacing: 0.04em; cursor: pointer;
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s;
}
.rb-ai-reroll:hover:not(:disabled) {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  box-shadow: 0 0 18px rgb(var(--tone-rgb) / 0.18);
}
.rb-ai-reroll:disabled { opacity: 0.5; cursor: default; }
.rb-ai-grid {
  grid-template-columns: repeat(auto-fill, minmax(160px, 190px));
  justify-content: start;
}
.rb-ai-summary-note {
  font-size: 13px; line-height: 1.55; color: var(--fg-1);
  margin: 0 0 7px;
}
.rb-ai-meta {
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-3);
  cursor: default;
}

/* ── Reason plinth — a slab tucked under the poster so the "why" reads as part
   of the card. Hairline + solid --bg-2 foot (no elevation of its own; the
   poster owns the directional card shadow). A small tone sparkle marks it as
   the engine's voice. ─────────────────────────────────────────────────────── */
.rb-tile { display: flex; flex-direction: column; }
.rb-tile :deep(.mediac) { position: relative; z-index: 1; height: auto; }
.rb-reason {
  margin-top: -10px;
  padding: 18px 12px 11px;
  background: var(--bg-2);
  border: 1px solid var(--hair);
  border-top: 0;
  border-radius: 0 0 var(--r-md) var(--r-md);
  font-size: 11.5px; line-height: 1.45; color: var(--fg-2);
  display: flex;
  gap: 6px;
}
.rb-reason > span {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.rb-reason-mark { color: var(--tone); flex-shrink: 0; margin-top: 2px; }
.rb-link { color: var(--tone); text-decoration: none; }
.rb-link:hover { text-decoration: underline; }

.rb-sentinel {
  display: flex;
  justify-content: center;
  padding: 24px 0 8px;
}
.rb-sentinel-spin {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 2px solid rgb(var(--ink) / 0.15);
  border-top-color: var(--tone);
  animation: rb-spin 0.8s linear infinite;
}
@keyframes rb-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 720px) {
  .rb-pad { padding: 16px 16px 60px; }
  .rb-search-input { font-size: 16px; }
  .rb-steer-row { flex-direction: column; gap: 8px; }
  .rb-steer-label { flex-basis: auto; padding-top: 0; }
  .rb-ai-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); }
}
</style>
