<!--
  RecsBrowse — the full "Recommendations" category page for /movies/recommendations
  and /tv/recommendations. Unlike TagBrowse (a flat tag list), this surfaces the
  personalized engine as a STEERABLE grid: pick a genre and/or a rating floor and
  the engine re-ranks by your taste within that constraint (the "horror binge").
  Each tile shows why it was picked. Non-ML today; the embedding engine plugs in
  behind a config flag without changing this surface.
-->
<template>
  <div class="scroll page-pad" style="height: 100%">
    <header class="rb-head">
      <div class="rb-eyebrow">For You</div>
      <h1 class="rb-title">Recommendations</h1>
      <div class="rb-meta">{{ steerSummary }}</div>
    </header>

    <div class="rb-search">
      <div class="rb-search-box">
        <Icon name="sparkle" :size="15" class="rb-search-icon" />
        <input
          v-model="nlQuery"
          type="text"
          class="rb-search-input"
          :placeholder="searchPlaceholder"
          @keydown.enter="askAI"
        >
        <button v-if="nlQuery" class="rb-search-clear" @click="clearSearch">Clear</button>
      </div>
      <button
        v-if="aiReady"
        class="rb-ai-btn"
        :disabled="nlQuery.trim().length < 2 || aiPending"
        @click="askAI"
      >
        <Icon name="sparkle" :size="12" />
        {{ aiPending ? 'Curating…' : 'Ask AI' }}
      </button>
    </div>

    <div v-if="!searching" class="rb-controls">
      <AppMenu trigger-class="btn-ghost-sm" :width="240" align="start">
        <template #trigger>
          {{ genre || 'Any genre' }}
          <Icon name="chevdown" :size="10" class="rb-caret" />
        </template>
        <DropdownMenuItem class="surface-item rb-item" :class="{ active: genre === '' }" @select="genre = ''">
          Any genre
        </DropdownMenuItem>
        <DropdownMenuItem
          v-for="g in genreOptions"
          :key="g"
          class="surface-item rb-item"
          :class="{ active: genre === g }"
          @select="genre = g"
        >
          {{ g }}
          <Icon v-if="genre === g" name="check" :size="12" class="rb-check" />
        </DropdownMenuItem>
      </AppMenu>

      <div class="rb-seg">
        <button
          v-for="opt in ratingOptions"
          :key="opt.value"
          :class="{ active: minRating === opt.value }"
          @click="minRating = opt.value"
        >
          {{ opt.label }}
        </button>
      </div>

      <div class="rb-spacer" />
      <button v-if="genre || minRating" class="btn-ghost-sm" @click="reset">Clear</button>
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

    <div v-if="aiShowing" class="rb-ai-meta" :title="aiProbesTitle">
      AI-curated · {{ aiMeta }}
    </div>

    <div v-if="displayLoading" class="grid-posters">
      <div v-for="i in 12" :key="i" class="grid-tile">
        <div class="poster" style="aspect-ratio: 2/3; background: var(--bg-3); animation: pulse 1.5s infinite" />
      </div>
    </div>

    <div v-else-if="displayItems.length" class="grid-posters">
      <AppContextMenu v-for="(item, i) in displayItems" :key="item.id" :items="contextItemsFor(item)">
        <NuxtLink :to="mediaUrl(item as any)" class="grid-tile card-tile">
          <MediaCard
            :idx="i"
            :src="usePosterUrl(item)"
            aspect="2/3"
            :title="item.title"
            :subtitle="item.reason || item.year"
          />
        </NuxtLink>
      </AppContextMenu>
    </div>

    <div v-else-if="!(searching && !mlReady)" class="rb-empty">
      {{ aiShowing ? 'The AI found nothing in the library that fits — try rewording the ask.'
        : searching ? 'No matches for that description.'
          : 'Nothing matches this steer — try another genre or lower the rating floor.' }}
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem, UserList } from '~~/shared/types'
import { DropdownMenuItem } from 'reka-ui'
import { useQuery, useQueryClient } from '@tanstack/vue-query'

const props = defineProps<{ section: 'movie' | 'tv' }>()

// Hoisted per the useNuxtApp gotcha — never resolve $heya inside async bodies.
const { $heya } = useNuxtApp()
const queryClient = useQueryClient()
const invalidateContinueWatching = useInvalidateContinueWatching()
const { buildItems: buildCardCtxItems } = useCardContextItems()

type RecItem = { id: number; title: string; slug: string; year?: string; media_type: string; reason?: string; available: boolean }

const genre = ref('')
const minRating = ref(0)

const ratingOptions = [
  { label: 'Any', value: 0 },
  { label: '6+', value: 6 },
  { label: '7+', value: 7 },
  { label: '8+', value: 8 },
]

// Available genres for the steer dropdown, most-common first.
const genresQuery = useQuery({
  queryKey: ['genres-all'],
  queryFn: async () => (await $heya('/api/genres')) as { genre: string; count: number }[],
  staleTime: 1000 * 60 * 30,
})
const genreOptions = computed(() =>
  [...(genresQuery.data.value ?? [])].sort((a, b) => b.count - a.count).map(g => g.genre).slice(0, 30),
)

// Reactive key — changing genre/minRating refetches with the new steer.
const recsQuery = useQuery({
  queryKey: ['for-you-browse', props.section, genre, minRating],
  queryFn: async () => (await $heya('/api/me/recommendations', {
    query: {
      type: props.section,
      genre: genre.value || undefined,
      min_rating: minRating.value || undefined,
      limit: 60,
    },
  })) as { items: RecItem[]; has_signal: boolean },
  staleTime: 1000 * 60 * 5,
})

const items = computed(() => recsQuery.data.value?.items ?? [])
const hasSignal = computed(() => recsQuery.data.value?.has_signal ?? true)
const loading = computed(() => recsQuery.isPending.value)

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
  queryKey: ['semantic', props.section, activeQ],
  queryFn: async () => (await $heya('/api/search/semantic', {
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
  queryKey: ['ai-ready'],
  queryFn: async () => (await $heya('/api/ai/ready')) as { ready: boolean; mode: string },
  staleTime: 1000 * 60 * 10,
})
const aiReady = computed(() => aiReadyQuery.data.value?.ready === true)

type AIRecResult = { items: RecItem[]; probes?: string[]; model?: string; mode: string; duration_ms: number }
const aiQ = ref('')
const aiQuery = useQuery({
  queryKey: ['ai-recs', props.section, aiQ],
  queryFn: async () => (await $heya('/api/ai/recommend', {
    method: 'POST',
    body: { query: aiQ.value, type: props.section } as any,
  })) as AIRecResult,
  enabled: computed(() => aiQ.value.length > 1),
  staleTime: 1000 * 60 * 10,
  retry: false, // expensive call — never auto-retry
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
function clearSearch() {
  nlQuery.value = ''
  aiQ.value = ''
}

// AI results own the grid while the input still says what was asked; editing
// the text falls back to live semantic matches until the next Enter.
const aiActive = computed(() => aiQ.value.length > 1 && nlQuery.value.trim() === aiQ.value)
// isFetching (not isPending) so a retry-after-error shows "Curating…" again
// instead of the stale error note.
const aiPending = computed(() => aiActive.value && aiQuery.isFetching.value)
const aiFailed = computed(() => aiQuery.isError.value && !aiQuery.isFetching.value)
const aiShowing = computed(() => aiActive.value && !!aiQuery.data.value && !aiQuery.isError.value)
const aiErrorMsg = computed(() => {
  const e = aiQuery.error.value as any
  return e?.data?.detail || e?.message || 'request failed'
})
const aiMeta = computed(() => {
  const d = aiQuery.data.value
  return d ? `${d.model || d.mode} · ${(d.duration_ms / 1000).toFixed(1)}s` : ''
})
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

const userListsQuery = useQuery({
  queryKey: ['me', 'lists'],
  queryFn: async () => (await $heya('/api/me/lists')) as UserList[],
  staleTime: 1000 * 60,
})
const moviesStateQuery = useQuery({
  queryKey: ['me', 'state', 'movies'],
  queryFn: async () => fetchUserState('movies'),
  staleTime: 1000 * 30,
  enabled: props.section === 'movie',
})
const seriesStateQuery = useQuery({
  queryKey: ['me', 'state', 'series'],
  queryFn: async () => fetchUserState('series'),
  staleTime: 1000 * 30,
  enabled: props.section === 'tv',
})

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
        queryClient.invalidateQueries({ queryKey: ['me', 'state'] })
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
        queryClient.invalidateQueries({ queryKey: ['me', 'state'] })
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

const steerSummary = computed(() => {
  const bits: string[] = []
  if (genre.value) bits.push(genre.value)
  if (minRating.value) bits.push(`${minRating.value}+ rating`)
  const scope = props.section === 'movie' ? 'films' : 'shows'
  return bits.length ? `${bits.join(' · ')} — ranked for you` : `${scope[0]!.toUpperCase()}${scope.slice(1)}, ranked for you`
})

function reset() {
  genre.value = ''
  minRating.value = 0
}
</script>

<style scoped>
.rb-head { margin-bottom: 20px; }
.rb-eyebrow {
  font-size: 10px; font-family: var(--font-mono); font-weight: 700;
  letter-spacing: 0.18em; text-transform: uppercase; color: var(--gold); margin-bottom: 8px;
}
.rb-title { font-size: 36px; font-weight: 600; letter-spacing: -0.02em; margin: 0 0 6px; }
.rb-meta { font-size: 12px; font-family: var(--font-mono); color: var(--fg-3); }

.rb-controls { display: flex; align-items: center; gap: 10px; margin-bottom: 20px; }
.rb-spacer { flex: 1; }
.rb-caret { opacity: 0.45; margin-left: 4px; }

/* Rating segmented control — mirrors TagBrowse's .tb-seg / FilterBar's .fb-seg. */
.rb-seg {
  display: inline-flex; gap: 2px; padding: 2px;
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.rb-seg button {
  padding: 5px 12px; border-radius: 4px; font-size: 12px; font-weight: 500;
  color: var(--fg-2); cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}
.rb-seg button:hover { color: var(--fg-0); }
.rb-seg button.active { background: var(--gold-soft); color: var(--gold-bright); }

.rb-note {
  font-size: 13px; color: var(--fg-2); background: rgba(255,255,255,0.03);
  border: 1px solid var(--border); border-radius: var(--r-sm);
  padding: 10px 14px; margin-bottom: 20px;
}
.rb-empty { padding: 60px 0; text-align: center; color: var(--fg-3); font-size: 14px; }

.rb-search { display: flex; align-items: stretch; gap: 10px; margin-bottom: 14px; }
.rb-search-box { position: relative; flex: 1; display: flex; align-items: center; }
.rb-search-icon { position: absolute; left: 14px; color: var(--gold); pointer-events: none; }
.rb-search-input {
  width: 100%; padding: 12px 16px 12px 40px;
  background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md);
  color: var(--fg-0); font-size: 14px; outline: none; transition: border-color 0.15s;
}
.rb-search-input:focus { border-color: var(--gold); }
.rb-search-input::placeholder { color: var(--fg-4); }
.rb-search-clear { position: absolute; right: 10px; background: transparent; border: 0; color: var(--fg-3); font-size: 12px; cursor: pointer; padding: 4px 8px; }
.rb-search-clear:hover { color: var(--fg-0); }

.rb-ai-btn {
  display: inline-flex; align-items: center; gap: 6px; padding: 0 16px;
  background: var(--gold-soft); color: var(--gold-bright);
  border: 1px solid transparent; border-radius: var(--r-md);
  font-size: 13px; font-weight: 600; white-space: nowrap; cursor: pointer;
  transition: filter 0.12s ease, opacity 0.12s ease;
}
.rb-ai-btn:hover:not(:disabled) { filter: brightness(1.15); }
.rb-ai-btn:disabled { opacity: 0.45; cursor: default; }

.rb-ai-note { color: var(--gold-bright); border-color: var(--gold-soft); }
.rb-ai-meta {
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-3);
  margin-bottom: 12px; cursor: default;
}
.rb-link { color: var(--gold); text-decoration: none; }
.rb-link:hover { text-decoration: underline; }

@media (max-width: 720px) {
  .page-pad { padding: 20px 16px 60px; }
  .rb-title { font-size: 26px; }
  .rb-controls { flex-wrap: wrap; }
}
</style>

<style>
/* AppMenu portals items out of scoped reach (docs/ui.md). */
.rb-item { justify-content: space-between; }
.rb-item.active { color: var(--gold); }
.rb-check { color: var(--gold); }
</style>
