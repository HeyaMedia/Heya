<template>
  <div class="pls page-pad">
    <MusicPageHead title="Playlists">
      <template #subtitle>
        <span>Every playlist you've made or synced. Right-click for actions.</span>
      </template>
      <div class="ms-stat-row">
        <div class="ms-stat">
          <div class="ms-stat-num">{{ playlists.length.toLocaleString() }}</div>
          <div class="ms-stat-lbl">Playlists</div>
        </div>
        <div class="ms-stat">
          <div class="ms-stat-num">{{ syncedCount.toLocaleString() }}</div>
          <div class="ms-stat-lbl">Synced</div>
        </div>
        <div class="ms-stat">
          <div class="ms-stat-num">{{ totalTracks.toLocaleString() }}</div>
          <div class="ms-stat-lbl">Tracks</div>
        </div>
      </div>
    </MusicPageHead>

    <div class="pls-controls">
      <div class="pls-seg">
        <button
          v-for="s in SORTS"
          :key="s.key"
          :class="{ active: sort === s.key }"
          :aria-pressed="sort === s.key"
          @click="sort = s.key"
        >{{ s.label }}</button>
      </div>
      <div v-if="allTags.length" class="pls-tags">
        <button class="pls-tag" :class="{ active: tagFilter === '' }" @click="tagFilter = ''">All</button>
        <button
          v-for="t in allTags"
          :key="t"
          class="pls-tag"
          :class="{ active: tagFilter === t }"
          @click="tagFilter = tagFilter === t ? '' : t"
        >{{ t }}</button>
      </div>
    </div>

    <div v-if="pending && !playlists.length" class="pls-loading">Loading playlists…</div>

    <MusicEmptyState v-else-if="!filtered.length" icon="list" title="No playlists here">
      {{ tagFilter ? `Nothing tagged “${tagFilter}”.` : 'Create a playlist from the sidebar, or link ListenBrainz in Settings → Music services to sync existing ones over.' }}
    </MusicEmptyState>

    <div v-else class="pls-grid">
      <AppContextMenu v-for="p in filtered" :key="p.id" :items="menuFor(p)">
        <NuxtLink :to="`/music/playlist/${p.id}`" class="pls-card card-tile">
          <div class="pls-cover">
            <NuxtImg v-if="coverFor(p)" :src="coverFor(p)!" class="pls-cover-img" width="360" :alt="p.name" />
            <div v-else class="pls-cover-fallback"><Icon name="list" :size="34" /></div>
            <span v-if="p.pinned" class="pls-pin" title="Pinned">
              <Icon name="pin" :size="11" weight="fill" />
            </span>
            <span v-if="p.sync_services?.length" class="pls-sync" :title="`Synced with ${p.sync_services.join(', ')}`">
              <Icon name="refresh" :size="10" /> synced
            </span>
          </div>
          <div class="pls-name">{{ p.name }}</div>
          <div class="pls-meta">
            <span>{{ p.track_count }} {{ p.track_count === 1 ? 'track' : 'tracks' }}</span>
          </div>
          <div v-if="p.tags?.length" class="pls-card-tags">
            <span v-for="t in p.tags.slice(0, 3)" :key="t" class="pls-card-tag">{{ t }}</span>
            <span v-if="p.tags.length > 3" class="pls-card-tag dim">+{{ p.tags.length - 3 }}</span>
          </div>
        </NuxtLink>
      </AppContextMenu>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { ContextMenuItem } from '~~/shared/types'
import { useQuery } from '@pinia/colada'
import { playlistCoverSrc } from '~/utils/playlistCover'

definePageMeta({ layout: 'default' })

interface PlaylistRow {
  id: number
  name: string
  slug: string
  description: string
  cover_path: string
  created_at: string
  updated_at: string
  tags: string[] | null
  track_count: number
  auto_album_slug: string
  auto_artist_slug: string
  has_cover: boolean
  sync_services: string[] | null
  pinned: boolean
  sidebar_pinned: boolean
}

const { $heya } = useNuxtApp()
const playlistMenu = usePlaylistMenu()

const listQuery = useQuery({
  key: ['me', 'playlists', 'full'],
  query: async () => {
    const r = await $heya('/api/me/playlists') as unknown as { items: PlaylistRow[] }
    return r.items ?? []
  },
  staleTime: 1000 * 30,
})
const playlists = computed(() => listQuery.data.value ?? [])
const pending = computed(() => listQuery.isLoading.value)

const syncedCount = computed(() => playlists.value.filter(p => p.sync_services?.length).length)
const totalTracks = computed(() => playlists.value.reduce((n, p) => n + (p.track_count ?? 0), 0))

// --- Sort + tag filter (client-side; the list is small) ---
type SortKey = 'updated' | 'name' | 'created' | 'tracks'
const SORTS: { key: SortKey; label: string }[] = [
  { key: 'updated', label: 'Recently updated' },
  { key: 'name', label: 'Name' },
  { key: 'created', label: 'Newest' },
  { key: 'tracks', label: 'Most tracks' },
]
const sort = ref<SortKey>('updated')
const tagFilter = ref('')

const allTags = computed(() => {
  const seen = new Set<string>()
  for (const p of playlists.value) for (const t of p.tags ?? []) seen.add(t)
  return [...seen].sort((a, b) => a.localeCompare(b))
})

const filtered = computed(() => {
  let out = playlists.value
  if (tagFilter.value) out = out.filter(p => p.tags?.includes(tagFilter.value))
  // Pinned playlists float above the rest regardless of the chosen sort.
  return [...out].sort((a, b) => Number(b.pinned) - Number(a.pinned) || bySort(a, b))
})

function bySort(a: PlaylistRow, b: PlaylistRow): number {
  return ((): number => {
    switch (sort.value) {
      case 'name': return a.name.localeCompare(b.name)
      case 'created': return Date.parse(b.created_at) - Date.parse(a.created_at)
      case 'tracks': return (b.track_count ?? 0) - (a.track_count ?? 0)
      default: return Date.parse(b.updated_at) - Date.parse(a.updated_at)
    }
  })()
}

function coverFor(p: PlaylistRow) {
  return playlistCoverSrc(p)
}

// Menus + mutations live in usePlaylistMenu (shared with the sidebar, home
// shelf, and My Music); its invalidation prefix-hits this page's
// ['me','playlists','full'] query, so the grid re-sorts on pin/rename/etc.
function menuFor(p: PlaylistRow): ContextMenuItem[] {
  return playlistMenu.menuFor({ id: p.id, name: p.name, track_count: p.track_count, slug: p.slug })
}
</script>

<style scoped>
.pls { max-width: 1400px; }

/* Stat chips — same visual as My Music's header stats. */
.ms-stat-row { display: flex; gap: 8px; }
.ms-stat {
  min-width: 100px;
  padding: 12px 20px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  border-radius: var(--r-md);
  text-align: center;
}
.ms-stat-num {
  font-size: 22px; font-weight: 700; color: var(--fg-0); letter-spacing: -0.01em;
  display: flex; align-items: center; justify-content: center; min-height: 28px;
}
.ms-stat-lbl {
  font-size: 10px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.1em;
  color: var(--fg-3); margin-top: 4px;
}

.pls-controls { display: flex; align-items: center; gap: 18px; margin-bottom: 24px; flex-wrap: wrap; }
/* Solid glass containers — the page floats over the ambient backdrop, so a
   4%-ink wash isn't enough for the labels to stay readable. Match the
   ms-stat chips: opaque-ish bg-2 glass + blur + elevation. */
.pls-seg {
  display: inline-flex; gap: 2px; padding: 3px;
  background: color-mix(in oklab, var(--bg-2) 88%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  box-shadow: var(--shadow-el);
}
.pls-seg button {
  padding: 6px 13px; border-radius: 4px;
  font-size: 12px; font-weight: 600; color: var(--fg-1); cursor: pointer;
  transition: all 0.15s;
}
.pls-seg button:hover { color: var(--fg-0); }
.pls-seg button.active { background: var(--gold-soft); color: var(--gold-bright, var(--gold)); }

.pls-tags { display: flex; gap: 6px; flex-wrap: wrap; }
.pls-tag {
  padding: 5px 12px; border-radius: 999px;
  background: color-mix(in oklab, var(--bg-2) 88%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  font-size: 11.5px; font-weight: 600; color: var(--fg-1); cursor: pointer;
  transition: all 0.15s;
}
.pls-tag:hover { color: var(--fg-0); border-color: var(--gold-soft); }
.pls-tag.active { background: var(--gold-soft); border-color: transparent; color: var(--gold-bright, var(--gold)); }

.pls-loading { color: var(--fg-2); font-size: 13px; padding: 40px 0; text-align: center; }

.pls-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 22px 18px;
}
.pls-card { display: block; min-width: 0; }
.pls-cover {
  position: relative; aspect-ratio: 1;
  border-radius: var(--r-md); overflow: hidden;
  background: var(--bg-2); border: 1px solid var(--border);
}
.pls-cover-img { width: 100%; height: 100%; object-fit: cover; }
.pls-cover-fallback {
  width: 100%; height: 100%;
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-4);
}
.pls-pin {
  position: absolute; top: 8px; left: 8px;
  display: inline-flex; align-items: center; justify-content: center;
  width: 22px; height: 22px; border-radius: 999px;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(6px);
  color: var(--gold);
}

.pls-sync {
  position: absolute; top: 8px; right: 8px;
  display: inline-flex; align-items: center; gap: 4px;
  padding: 3px 8px; border-radius: 999px;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(6px);
  font-size: 9.5px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--gold);
}
.pls-name {
  margin-top: 10px;
  font-size: 14px; font-weight: 600; color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.pls-meta { margin-top: 2px; font-size: 11.5px; font-family: var(--font-mono); color: var(--fg-3); }
.pls-card-tags { margin-top: 6px; display: flex; gap: 4px; flex-wrap: wrap; }
.pls-card-tag {
  padding: 2px 8px; border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  font-size: 10px; color: var(--fg-2);
}
.pls-card-tag.dim { color: var(--fg-4); }

@media (max-width: 720px) {
  .pls-grid { grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 16px 12px; }
  .ms-stat { min-width: 0; padding: 10px 12px; }
}
</style>
