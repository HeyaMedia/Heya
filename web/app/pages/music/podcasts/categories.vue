<template>
  <div class="page-pad pc-page">
    <header class="pc-head">
      <NuxtLink to="/music/podcasts" class="pc-back">
        <Icon name="chevleft" :size="14" /> Podcasts
      </NuxtLink>
      <MusicPageHead title="Browse Categories" subtitle="Pick a topic to surface the trending shows in that bucket." />
    </header>

    <!-- Drilldown: trending podcasts in the selected category. -->
    <template v-if="selectedCategory">
      <div class="pc-drill-head">
        <button class="pc-drill-back" @click="clearSelection">
          <Icon name="chevleft" :size="16" /> All categories
        </button>
        <h2 class="section-title-lg pc-drill-title">{{ selectedCategory }}</h2>
      </div>
      <div v-if="podcastsLoading" class="pc-loading">Loading…</div>
      <div v-else-if="!trending.length" class="pc-empty">No trending shows in this category.</div>
      <div v-else class="pc-grid">
        <PodcastCard v-for="p in trending" :key="`p-${p.id}`" :podcast="p" />
      </div>
    </template>

    <!-- Category grid. -->
    <template v-else>
      <div v-if="!categories.length" class="pc-loading">Loading categories…</div>
      <div v-else-if="unavailable" class="pc-empty">
        <p>Podcast Index isn't configured on this server.</p>
        <p class="pc-empty-hint">Set <code>HEYA_PODCAST_INDEX_KEY</code> + <code>HEYA_PODCAST_INDEX_SECRET</code> in <code>.env</code>.</p>
      </div>
      <div v-else class="pc-cat-grid">
        <button
          v-for="(c, i) in categories"
          :key="c.id"
          class="pc-cat-tile"
          :style="{ background: categoryGradient(i) }"
          @click="selectCategory(c.name)"
        >
          <Icon :name="categoryIcon(c.name)" :size="22" />
          <span class="pc-cat-name">{{ c.name }}</span>
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { Podcast } from '~/composables/usePodcasts'
import { useQuery } from '@pinia/colada'

definePageMeta({ layout: 'default' })

interface Category { id: number; name: string }

const route = useRoute()
const router = useRouter()
const { $heya } = useNuxtApp()

const categoriesQuery = useQuery({
  key: ['podcasts', 'categories'],
  query: async () => ((await $heya('/api/podcasts/categories')) as { items: Category[] }).items ?? [],
  staleTime: 1000 * 60 * 60 * 24, // 24h — Podcast Index categories rarely change
  retry: 0,
})
const categories = computed<Category[]>(() => categoriesQuery.data.value ?? [])
const unavailable = computed(() => {
  const err = categoriesQuery.error.value as { statusCode?: number } | null
  return err?.statusCode === 503
})

const selectedCategory = computed(() => (route.query.cat as string | undefined) ?? '')

const trendingQuery = useQuery({
  key: () => ['podcasts', 'trending', { category: selectedCategory.value }],
  query: async () => ((await $heya('/api/podcasts/trending', { query: { max: 40, category: selectedCategory.value } })) as { items: Podcast[] }).items ?? [],
  enabled: () => selectedCategory.value.length > 0,
  staleTime: 1000 * 60 * 30,
})
await Promise.all([waitForQuery(categoriesQuery), waitForQuery(trendingQuery)])
const trending = computed<Podcast[]>(() => trendingQuery.data.value ?? [])
const podcastsLoading = computed(() => trendingQuery.isLoading.value)

function selectCategory(name: string) {
  router.replace({ query: { cat: name } })
}

function clearSelection() {
  router.replace({ query: {} })
}

// Deterministic per-name gradient so the same tile stays the same color
// across visits — uses a stable hash of the name to pick from the palette.
// Fixed categorical colors (like artwork), not canvas chrome — stay literal
// in both themes so a category keeps the same hue.
const PALETTE = [
  'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)',
  'linear-gradient(135deg, #ec4899 0%, #be185d 100%)',
  'linear-gradient(135deg, #6366f1 0%, #4338ca 100%)',
  'linear-gradient(135deg, #84cc16 0%, #4d7c0f 100%)',
  'linear-gradient(135deg, #06b6d4 0%, #0e7490 100%)',
  'linear-gradient(135deg, #a855f7 0%, #6b21a8 100%)',
  'linear-gradient(135deg, #ef4444 0%, #dc2626 100%)',
  'linear-gradient(135deg, #14b8a6 0%, #0d9488 100%)',
  'linear-gradient(135deg, #f97316 0%, #c2410c 100%)',
  'linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%)',
]
function categoryGradient(i: number) {
  return PALETTE[i % PALETTE.length]
}

// Best-effort icon mapping. Categories without a match fall through to a
// generic folder — the gradient does the visual work either way.
const ICONS: Record<string, string> = {
  News: 'newspaper', 'True Crime': 'shield', Comedy: 'smile', Business: 'briefcase',
  Education: 'book', Technology: 'code', Music: 'music', Health: 'heart',
  Fitness: 'activity', Society: 'users', History: 'clock', Science: 'flask',
  Sports: 'trophy', 'TV & Film': 'film', Books: 'book', Politics: 'megaphone',
  Religion: 'star', Spirituality: 'star', Arts: 'palette', Kids: 'heart',
  Family: 'users',
}
function categoryIcon(name: string) {
  return ICONS[name] ?? 'mic'
}
</script>

<style scoped>
.pc-page { padding-bottom: 80px; }
.pc-head { margin-bottom: 28px; }
.pc-back { color: var(--fg-3); font-size: 12px; text-decoration: none; display: inline-flex; align-items: center; gap: 4px; }
.pc-back:hover { color: var(--gold); }

@media (pointer: coarse) {
  .pc-back { min-height: 44px; padding: 10px 0; }
}

.pc-cat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 12px;
}
.pc-cat-tile {
  aspect-ratio: 4 / 3;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 14px 16px;
  border: 0;
  border-radius: var(--r-md);
  color: #fff; /* on the fixed gradient tile — stays literal */
  text-align: left;
  cursor: pointer;
  font-family: inherit;
  box-shadow: 0 6px 14px rgb(var(--shade) / 0.3);
  transition: transform 0.15s, box-shadow 0.15s;
}
.pc-cat-tile:hover {
  transform: translateY(-2px);
  box-shadow: 0 10px 20px rgb(var(--shade) / 0.45);
}
.pc-cat-name { font-size: 16px; font-weight: 700; letter-spacing: -0.005em; }

.pc-drill-head { display: flex; align-items: center; gap: 14px; margin-bottom: 20px; }
.pc-drill-back {
  background: transparent; border: 0; font-size: 12px; color: var(--fg-2); cursor: pointer;
  padding: 6px 10px; border-radius: var(--r-sm); display: inline-flex; align-items: center; gap: 4px;
}
.pc-drill-back:hover { color: var(--gold); background: color-mix(in srgb, var(--gold) 6%, transparent); }
.pc-drill-title { margin: 0; }

.pc-loading, .pc-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; max-width: 540px; text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1); }
.pc-empty-hint { color: var(--fg-3); font-size: 12px; margin-top: 8px; text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1); }
.pc-empty code { background: var(--bg-2); padding: 1px 6px; border-radius: 4px; font-family: var(--font-mono); font-size: 11px; }

.pc-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 18px;
}

@media (max-width: 720px) {
  .pc-cat-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 10px; }
  .pc-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 12px; }
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
