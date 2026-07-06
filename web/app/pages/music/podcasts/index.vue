<template>
  <div class="page-pad podcast-page">
    <header class="pp-head">
      <h1 class="pp-title">Podcasts</h1>
      <p class="pp-sub">{{ headlineSub }}</p>
      <div class="pp-search">
        <Icon name="search" :size="16" class="pp-search-icon" />
        <input
          v-model="searchQuery"
          type="search"
          class="pp-search-input"
          placeholder="Search by show, host, topic…"
          @keydown.enter="runSearch"
        />
        <button v-if="searchQuery" class="pp-search-clear" @click="clearSearch">
          <Icon name="close" :size="14" />
        </button>
      </div>
    </header>

    <!-- Discovery strip — hidden during search so results aren't pushed
         below the fold. -->
    <nav v-if="!searchResults.length" class="pp-strip">
      <NuxtLink to="/music/podcasts/categories" class="pp-strip-tile">
        <Icon name="grid" :size="20" />
        <div>
          <div class="pp-strip-title">Browse Categories</div>
          <div class="pp-strip-sub">News, Comedy, Tech, and more</div>
        </div>
      </NuxtLink>
    </nav>

    <section v-if="searchResults.length" class="pp-section">
      <h2 class="section-title-lg">Results for "{{ lastSearch }}"</h2>
      <div class="pp-grid">
        <PodcastCard v-for="p in searchResults" :key="`search-${p.id}`" :podcast="p" />
      </div>
    </section>

    <template v-else>
      <section v-if="subscriptions.length" class="pp-section">
        <h2 class="section-title-lg">Your Subscriptions</h2>
        <div class="pp-grid">
          <PodcastCard
            v-for="s in subscriptions"
            :key="`sub-${s.feed_url}`"
            :podcast="{ feed_url: s.feed_url, title: s.title, author: s.author, artwork_url: s.artwork_url }"
          />
        </div>
      </section>

      <section v-if="continueListening.length" class="pp-section">
        <h2 class="section-title-lg">Continue Listening</h2>
        <div class="pp-continue-list">
          <NuxtLink
            v-for="ep in continueListening"
            :key="`continue-${ep.id}`"
            :to="`/music/podcasts/feed?feed=${encodeURIComponent(ep.feed_url)}`"
            class="pp-continue-row"
          >
            <div class="pp-continue-art">
              <NuxtImg v-if="ep.artwork_url" :src="ep.artwork_url" :alt="ep.title" loading="lazy" />
              <Icon v-else name="mic" :size="24" />
            </div>
            <div class="pp-continue-meta">
              <div class="pp-continue-title">{{ ep.title }}</div>
              <div class="pp-continue-progress">
                <div class="pp-progress-bar">
                  <div class="pp-progress-fill" :style="{ width: progressPercent(ep) + '%' }" />
                </div>
                <span class="mono">{{ formatProgress(ep) }}</span>
              </div>
            </div>
          </NuxtLink>
        </div>
      </section>

      <section class="pp-section">
        <h2 class="section-title-lg">Trending</h2>
        <div v-if="trendingPending" class="pp-loading">Loading…</div>
        <div v-else-if="trendingUnavailable" class="pp-empty">
          <p>Podcast Index isn't configured on this server.</p>
          <p class="pp-empty-hint">Set <code>HEYA_PODCAST_INDEX_KEY</code> + <code>HEYA_PODCAST_INDEX_SECRET</code> (free at <a href="https://api.podcastindex.org" target="_blank" rel="noopener">api.podcastindex.org</a>).</p>
        </div>
        <div v-else class="pp-grid">
          <PodcastCard v-for="p in trending" :key="`trend-${p.id}`" :podcast="p" />
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { Podcast } from '~/composables/usePodcasts'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

const actions = usePodcastActions()
if (import.meta.client) actions.ensureSubscriptionsLoaded()

const { $heya } = useNuxtApp()

const trendingQuery = useQuery({
  queryKey: ['podcasts', 'trending', { max: 30 }],
  queryFn: async () => ((await $heya('/api/podcasts/trending', { query: { max: 30 } })) as { items: Podcast[] }).items ?? [],
  staleTime: 1000 * 60 * 30,
  retry: false, // 503 (PI not configured) shouldn't trigger retries
})
const trending = computed<Podcast[]>(() => trendingQuery.data.value ?? [])
const trendingPending = computed(() => trendingQuery.isPending.value)
// 503 = PI keys not configured. Render a clear setup hint rather than an
// empty grid the user can't make sense of.
const trendingUnavailable = computed(() => {
  const err = trendingQuery.error.value as { statusCode?: number } | null
  return err?.statusCode === 503
})

interface Subscription { feed_url: string; title: string; author: string; artwork_url: string }
interface ContinueEpisode {
  id: number
  feed_url: string
  episode_guid: string
  title: string
  artwork_url: string
  audio_url: string
  progress_seconds: number
  total_seconds: number
  updated_at: string
}

const subscriptionsQuery = useQuery({
  queryKey: ['me', 'podcasts', 'subscriptions'],
  queryFn: async () => ((await $heya('/api/me/podcasts/subscriptions')) as { items: Subscription[] }).items ?? [],
  staleTime: 1000 * 30,
})
const subscriptions = computed<Subscription[]>(() => subscriptionsQuery.data.value ?? [])

const continueQuery = useQuery({
  queryKey: ['me', 'podcasts', 'continue', { limit: 8 }],
  queryFn: async () => ((await $heya('/api/me/podcasts/continue', { query: { limit: 8 } })) as { items: ContinueEpisode[] }).items ?? [],
  staleTime: 1000 * 30,
})
const continueListening = computed<ContinueEpisode[]>(() => continueQuery.data.value ?? [])

const searchQuery = ref('')
const lastSearch = ref('')
const searchResults = ref<Podcast[]>([])

async function runSearch() {
  const q = searchQuery.value.trim()
  if (!q) { searchResults.value = []; return }
  lastSearch.value = q
  try {
    const res = await $heya('/api/podcasts/search', { query: { q, max: 30 } }) as { items: Podcast[] }
    searchResults.value = res.items ?? []
  } catch { searchResults.value = [] }
}
function clearSearch() {
  searchQuery.value = ''
  searchResults.value = []
  lastSearch.value = ''
}

const headlineSub = computed(() => {
  if (searchResults.value.length) return `${searchResults.value.length} match${searchResults.value.length === 1 ? '' : 'es'}`
  return 'Subscribe to shows, resume where you left off, discover new ones.'
})

function progressPercent(ep: ContinueEpisode) {
  if (ep.total_seconds <= 0) return 0
  return Math.min(100, Math.round((ep.progress_seconds / ep.total_seconds) * 100))
}
function formatProgress(ep: ContinueEpisode) {
  const fmt = (s: number) => {
    const m = Math.floor(s / 60)
    const ss = Math.floor(s % 60)
    return `${m}:${String(ss).padStart(2, '0')}`
  }
  return `${fmt(ep.progress_seconds)} / ${fmt(ep.total_seconds)}`
}
</script>

<style scoped>
.podcast-page { padding-bottom: 80px; }
.pp-head { margin-bottom: 28px; max-width: 720px; }
.pp-title { font-size: 30px; font-weight: 700; letter-spacing: -0.01em; }
.pp-sub { color: var(--fg-3); font-size: 13px; margin: 4px 0 18px; }
.pp-search { position: relative; display: flex; align-items: center; }
.pp-search-icon { position: absolute; left: 14px; color: var(--fg-3); }
.pp-search-input {
  width: 100%;
  padding: 12px 36px 12px 38px;
  font-size: 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: var(--fg-0);
  outline: none;
  transition: border-color 0.15s;
}
.pp-search-input:focus { border-color: var(--gold); }
.pp-search-clear { position: absolute; right: 10px; background: transparent; border: 0; color: var(--fg-3); padding: 6px; cursor: pointer; border-radius: var(--r-sm); }
.pp-search-clear:hover { color: var(--gold); }

.pp-strip {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
  margin-bottom: 36px;
}
.pp-strip-tile {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  transition: border-color 0.15s, background 0.15s, transform 0.15s;
}
.pp-strip-tile:hover {
  border-color: rgba(99, 102, 241, 0.4);
  background: rgba(99, 102, 241, 0.05);
  transform: translateY(-1px);
}
.pp-strip-tile > :first-child { color: #6366f1; flex-shrink: 0; }
.pp-strip-title { font-size: 13px; font-weight: 600; color: var(--fg-0); }
.pp-strip-sub { font-size: 11px; color: var(--fg-3); margin-top: 2px; }

.pp-section { margin-bottom: 36px; }
.pp-section .section-title-lg { margin-bottom: 16px; }
.pp-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 18px;
}
.pp-loading { color: var(--fg-3); padding: 20px 0; }
.pp-empty { color: var(--fg-2); padding: 20px 0; font-size: 13px; max-width: 540px; }
.pp-empty-hint { color: var(--fg-3); font-size: 12px; margin-top: 8px; }
.pp-empty code { background: var(--bg-2); padding: 1px 6px; border-radius: 4px; font-family: var(--font-mono); font-size: 11px; }
.pp-empty a { color: var(--gold); }

.pp-continue-list { display: flex; flex-direction: column; gap: 8px; }
.pp-continue-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  transition: border-color 0.15s, background 0.15s;
}
.pp-continue-row:hover { border-color: rgba(255, 196, 50, 0.3); background: rgba(255, 196, 50, 0.04); }
.pp-continue-art {
  width: 48px;
  height: 48px;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  flex-shrink: 0;
}
.pp-continue-art img { width: 100%; height: 100%; object-fit: cover; }
.pp-continue-meta { flex: 1; min-width: 0; }
.pp-continue-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-bottom: 4px;
}
.pp-continue-progress { display: flex; align-items: center; gap: 8px; font-size: 10px; color: var(--fg-3); }
.pp-progress-bar { flex: 1; height: 3px; background: rgba(255, 255, 255, 0.06); border-radius: 999px; overflow: hidden; }
.pp-progress-fill { height: 100%; background: var(--gold); }
.mono { font-family: var(--font-mono); }

@media (max-width: 720px) {
  /* music.vue's phone header already reads "Podcasts" directly above this
     page — the live sub line + search box both stay. */
  .pp-title { display: none; }
  .pp-head { margin-bottom: 20px; }
  .pp-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 12px; }
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
