<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { metadataPoliciesQuery } from '~/queries/settings'
import type { LibrarySettings as Settings } from '~/queries/settings'

const policiesData = useQuery(metadataPoliciesQuery())
const libraries = computed(() => policiesData.data.value?.libraries ?? [])
const settingsByLib = computed(() => new Map<number, Settings>(
  Object.entries(policiesData.data.value?.settings ?? {}).map(([id, settings]) => [Number(id), settings]),
))
const loading = computed(() => policiesData.isLoading.value)
const error = computed(() => policiesData.error.value?.message ?? null)

const aggregateCounts = computed(() => {
  const total = libraries.value.length
  let nfo = 0, images = 0, ratings = 0, autoCollections = 0, watch = 0
  for (const l of libraries.value) {
    const s = settingsByLib.value.get(l.id)
    if (!s) continue
    if (s.save_nfo)         nfo++
    if (s.save_images)      images++
    if (s.fetch_ratings)    ratings++
    if (s.auto_collections) autoCollections++
    if (s.watch)            watch++
  }
  return { total, nfo, images, ratings, autoCollections, watch }
})

function libraryIcon(kind: string): string {
  switch (kind) {
    case 'movie': return 'film'
    case 'tv':
    case 'anime': return 'tv'
    case 'music': return 'music'
    case 'book':  return 'book'
    default:      return 'folder'
  }
}

</script>

<template>
  <div>
    <SettingsContextHero
      title="Metadata policies"
      icon="refresh"
      eyebrow="Media · Enrichment"
      description="Review how every library refreshes metadata, writes sidecars and artwork, fetches ratings, and groups collections."
    />

    <div class="tiles">
      <MetricTile
        label="Libraries"
        :value="aggregateCounts.total"
        icon="folder"
      />
      <MetricTile
        label="Writing NFO"
        :value="`${aggregateCounts.nfo} / ${aggregateCounts.total}`"
        icon="clipboard"
        :tone="aggregateCounts.nfo > 0 ? 'good' : 'neutral'"
      />
      <MetricTile
        label="Saving images"
        :value="`${aggregateCounts.images} / ${aggregateCounts.total}`"
        icon="image"
        :tone="aggregateCounts.images > 0 ? 'good' : 'neutral'"
      />
      <MetricTile
        label="Fetching ratings"
        :value="`${aggregateCounts.ratings} / ${aggregateCounts.total}`"
        icon="pulse"
      />
      <MetricTile
        label="Auto collections"
        :value="`${aggregateCounts.autoCollections} / ${aggregateCounts.total}`"
        icon="layers"
      />
      <MetricTile
        label="Watched"
        :value="`${aggregateCounts.watch} / ${aggregateCounts.total}`"
        icon="eye"
        :tone="aggregateCounts.watch === aggregateCounts.total ? 'good' : 'warn'"
      />
    </div>

    <SettingsSection title="Per-library metadata policy" icon="folder"
      description="One row per library. NFO and images are written back to the library path. Refresh is automatic — active titles re-fetch every 14 days, ended or cancelled ones every 180 days.">
      <template #actions>
        <NuxtLink to="/settings/libraries" class="link-arrow">
          Edit on Libraries <Icon name="chevright" :size="11" />
        </NuxtLink>
      </template>

      <div v-if="loading" class="loading-state"><Icon name="spinner" :size="14" /> Loading…</div>
      <div v-else-if="error" class="empty-state err"><Icon name="warning" :size="14" /> {{ error }}</div>
      <div v-else-if="libraries.length === 0" class="empty-state"><Icon name="info" :size="14" /> No libraries yet.</div>
      <div v-else class="lib-table">
        <div class="lib-head">
          <span class="col-lib">Library</span>
          <span class="col-bool">NFO</span>
          <span class="col-bool">Images</span>
          <span class="col-bool">Ratings</span>
          <span class="col-bool">Auto-collections</span>
          <span class="col-bool">Watch</span>
          <span class="col-days">Refresh</span>
          <span class="col-locale">Locale</span>
        </div>
        <div v-for="l in libraries" :key="l.id" class="lib-row">
          <span class="col-lib">
            <Icon :name="libraryIcon(l.media_type)" :size="14" class="lib-icon" />
            <span class="lib-name">{{ l.name }}</span>
            <span class="lib-type mono">{{ l.media_type }}</span>
          </span>
          <span class="col-bool">
            <span class="col-label">NFO</span>
            <StatusBadge :state="settingsByLib.get(l.id)?.save_nfo ? 'ok' : 'idle'">
              {{ settingsByLib.get(l.id)?.save_nfo ? 'on' : 'off' }}
            </StatusBadge>
          </span>
          <span class="col-bool">
            <span class="col-label">Images</span>
            <StatusBadge :state="settingsByLib.get(l.id)?.save_images ? 'ok' : 'idle'">
              {{ settingsByLib.get(l.id)?.save_images ? 'on' : 'off' }}
            </StatusBadge>
          </span>
          <span class="col-bool">
            <span class="col-label">Ratings</span>
            <StatusBadge :state="settingsByLib.get(l.id)?.fetch_ratings ? 'ok' : 'idle'">
              {{ settingsByLib.get(l.id)?.fetch_ratings ? 'on' : 'off' }}
            </StatusBadge>
          </span>
          <span class="col-bool">
            <span class="col-label">Auto-collections</span>
            <StatusBadge :state="settingsByLib.get(l.id)?.auto_collections ? 'ok' : 'idle'">
              {{ settingsByLib.get(l.id)?.auto_collections ? 'on' : 'off' }}
            </StatusBadge>
          </span>
          <span class="col-bool">
            <span class="col-label">Watch</span>
            <StatusBadge :state="settingsByLib.get(l.id)?.watch ? 'ok' : 'idle'">
              {{ settingsByLib.get(l.id)?.watch ? 'on' : 'off' }}
            </StatusBadge>
          </span>
          <span class="col-days mono" title="Automatic — active titles refresh every 14 days, ended/cancelled every 180 days">auto</span>
          <span class="col-locale mono">
            {{ settingsByLib.get(l.id)?.preferred_language || '—' }}<span v-if="settingsByLib.get(l.id)?.preferred_country">·{{ settingsByLib.get(l.id)?.preferred_country }}</span>
          </span>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Direct metadata editor" icon="pencil"
      description="Browse the entire library and rewrite metadata by hand — fix titles, swap posters, re-match items, edit episode order.">
      <NuxtLink to="/settings/metadata-editor" class="big-link">
        <div class="big-link-icon"><Icon name="pencil" :size="22" /></div>
        <div class="big-link-body">
          <div class="big-link-title">Open metadata editor</div>
          <div class="big-link-desc">
            Split-pane workspace — column browser on the left, editor on the
            right. Best for bulk title fixes, poster swaps, and re-matching
            misidentified items.
          </div>
        </div>
        <Icon name="chevright" :size="16" class="big-link-chev" />
      </NuxtLink>
    </SettingsSection>
  </div>
</template>

<style scoped>
.inline-link { color: var(--gold); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.loading-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.empty-state.err { color: var(--bad); background: color-mix(in srgb, var(--bad) 6%, transparent); border-color: color-mix(in srgb, var(--bad) 25%, transparent); }

.lib-table {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}
.lib-head, .lib-row {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) 60px 70px 70px 110px 60px 70px 90px;
  gap: 10px;
  align-items: center;
  padding: 8px 14px;
  font-size: 12px;
}
.lib-head {
  background: var(--bg-1);
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--fg-3);
  border-bottom: 1px solid var(--border);
  padding: 9px 14px;
}
.lib-row { border-bottom: 1px solid var(--border); }
.lib-row:last-child { border-bottom: 0; }
.lib-row:hover { background: rgb(var(--ink) / 0.02); }

.col-lib { display: flex; align-items: center; gap: 8px; min-width: 0; }
.lib-icon { color: var(--fg-3); flex-shrink: 0; }
.lib-name { font-weight: 500; color: var(--fg-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.lib-type {
  font-size: 10px; padding: 1px 6px;
  border-radius: var(--r-xs);
  background: var(--bg-0); color: var(--fg-3);
}
.mono { font-family: var(--font-mono); font-size: 11px; }
.col-days, .col-locale { color: var(--fg-2); }

.big-link {
  display: flex; align-items: center; gap: 14px;
  padding: 16px 18px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  transition: border-color 0.12s, background 0.12s;
}
.big-link:hover {
  border-color: var(--gold);
  background: var(--gold-soft);
}
.big-link-icon {
  width: 40px; height: 40px;
  border-radius: var(--r-md);
  background: var(--bg-0);
  color: var(--gold);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.big-link-body { flex: 1; }
.big-link-title { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.big-link-desc  { font-size: 12px; color: var(--fg-3); margin-top: 2px; line-height: 1.4; }
.big-link-desc .mono { font-family: var(--font-mono); font-size: 11px; color: var(--fg-2); }
.big-link-chev { color: var(--fg-3); flex-shrink: 0; }

.link-arrow {
  display: inline-flex; align-items: center; gap: 2px;
  font-size: 11px;
  color: var(--fg-3);
  text-decoration: none;
}
.link-arrow:hover { color: var(--gold); }

/* Desktop: the column header row already supplies each badge's meaning, so
   the inline label stays hidden until the phone regrid below needs it. */
.col-label { display: none; }

/* Phone: 8 columns can't fit 390px. Pure-CSS regrid — library name gets its
   own top line, the five on/off badges wrap as a labelled meta row (the
   header row that normally supplies the column meaning is hidden, so each
   badge grows a small inline label), refresh/locale trail on their own
   line. .col-label only exists for this breakpoint. */
@media (max-width: 900px) {
  .lib-head { display: none; }
  .lib-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 12px;
    padding: 12px 14px;
  }
  .col-lib { flex: 1 1 100%; }
  .col-bool {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .col-label {
    display: inline;
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-4);
  }
  .col-days, .col-locale { margin-left: 0; }
}
</style>
