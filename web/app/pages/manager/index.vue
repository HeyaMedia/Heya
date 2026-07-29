<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { MOCK_HISTORY, MOCK_QUEUE, MOCK_WANTED } from '~/utils/managerMock'

const activeDownloads = MOCK_QUEUE.filter(q => q.status === 'downloading' || q.status === 'importing').length
const missingCount = MOCK_WANTED.filter(w => w.reason === 'missing').length
const cutoffCount = MOCK_WANTED.filter(w => w.reason === 'cutoff').length
const recent = MOCK_HISTORY.slice(0, 6)

const EVENT_META: Record<string, { icon: string, cls: string }> = {
  grabbed: { icon: 'cloud-download', cls: 'ev-grab' },
  imported: { icon: 'check', cls: 'ev-ok' },
  upgraded: { icon: 'sparkle', cls: 'ev-ok' },
  failed: { icon: 'warning', cls: 'ev-bad' },
  renamed: { icon: 'pencil', cls: 'ev-dim' },
  deleted: { icon: 'trash', cls: 'ev-dim' },
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Activity"
      icon="pulse"
      eyebrow="Manager · Preview — mock data"
      description="Acquisition at a glance: what's downloading, what's wanted, and what the pipeline did recently. Everything on these pages is a static stub until the backend lands."
    />

    <div class="tiles">
      <MetricTile label="Active downloads" :value="activeDownloads" icon="cloud-download" tone="good" sub="2 clients connected" />
      <MetricTile label="Missing" :value="missingCount" icon="target" tone="warn" sub="monitored, no file yet" />
      <MetricTile label="Cutoff unmet" :value="cutoffCount" icon="sort" tone="neutral" sub="upgrade candidates" />
      <MetricTile label="Last RSS sync" value="4m ago" icon="refresh" tone="good" sub="7 indexers · next in 11m" />
    </div>

    <SettingsSection
      title="Recent activity"
      icon="pulse"
      description="The pipeline's latest grabs, imports, upgrades and failures across every library."
    >
      <template #actions>
        <NuxtLink to="/manager/history" class="mgr-more-link">
          Full history <Icon name="arrow-right" :size="12" />
        </NuxtLink>
      </template>

      <ul class="feed">
        <li v-for="ev in recent" :key="ev.id" class="feed-row">
          <span class="feed-icon" :class="EVENT_META[ev.event]?.cls"><Icon :name="EVENT_META[ev.event]?.icon ?? 'info'" :size="14" /></span>
          <div class="feed-body">
            <div class="feed-title">
              <span class="feed-event">{{ ev.event }}</span>
              {{ ev.title }}
              <ManagerLibraryChip :library="ev.library" />
            </div>
            <div class="feed-detail">{{ ev.detail }}</div>
          </div>
          <span class="feed-time">{{ ev.at }}</span>
        </li>
      </ul>
    </SettingsSection>

    <SettingsSection
      title="Needs attention"
      icon="warning"
      description="Stalled grabs and health problems the pipeline can't resolve on its own."
    >
      <ul class="feed">
        <li class="feed-row">
          <span class="feed-icon ev-bad"><Icon name="warning" :size="14" /></span>
          <div class="feed-body">
            <div class="feed-title">Shōgun S01 season pack stalled <ManagerLibraryChip library="tv" /></div>
            <div class="feed-detail">No seeders for 6h — blocklist and search per-episode instead?</div>
          </div>
          <NuxtLink to="/manager/queue" class="mgr-more-link">Queue <Icon name="arrow-right" :size="12" /></NuxtLink>
        </li>
        <li class="feed-row">
          <span class="feed-icon ev-bad"><Icon name="warning" :size="14" /></span>
          <div class="feed-body">
            <div class="feed-title">NZBGeek rejected the API key</div>
            <div class="feed-detail">Indexer disabled until the credential is fixed.</div>
          </div>
          <NuxtLink to="/manager/system/indexers" class="mgr-more-link">Indexers <Icon name="arrow-right" :size="12" /></NuxtLink>
        </li>
      </ul>
    </SettingsSection>
  </div>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 10px;
  margin-bottom: 28px;
}
@media (max-width: 720px) {
  .tiles { grid-template-columns: repeat(2, 1fr); }
}

.feed { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.feed-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.feed-icon {
  width: 30px; height: 30px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
  color: var(--fg-2);
}
.feed-icon.ev-ok { color: var(--good); }
.feed-icon.ev-bad { color: var(--bad); }
.feed-icon.ev-grab { color: var(--gold); }
.feed-icon.ev-dim { color: var(--fg-3); }
.feed-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.feed-title {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 13.5px; font-weight: 500; color: var(--fg-0);
}
.feed-event {
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.feed-detail {
  font-size: 12px; color: var(--fg-2);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.feed-time {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
  flex-shrink: 0;
  padding-top: 2px;
}

.mgr-more-link {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--gold);
  white-space: nowrap;
}
.mgr-more-link:hover { color: var(--gold-bright); }
</style>
