<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { MOCK_LISTS } from '~/utils/managerMock'
</script>

<template>
  <div>
    <SettingsContextHero
      title="Import lists"
      icon="list"
      eyebrow="Manager · System — mock data"
      description="Lists sync both ways: items are added and monitored here, the manager grabs what's missing, and the list itself surfaces on the server — so your Trakt watchlist shows everything you have and everything it's actively fetching."
    />

    <SettingsSection
      title="Connected lists"
      icon="list"
      description="Each list maps to a target library and a behavior. 'Grab missing' feeds Wanted; the mirrored server list keeps in-progress items visible while they download."
    >
      <template #actions>
        <button type="button" class="il-add"><Icon name="plus" :size="14" /> Add list</button>
      </template>

      <div class="il-grid">
        <div v-for="l in MOCK_LISTS" :key="l.id" class="il-card">
          <div class="il-icon"><Icon name="list" :size="16" /></div>
          <div class="il-body">
            <div class="il-name">
              {{ l.name }}
              <StatusBadge :state="l.state === 'ok' ? 'ok' : 'warn'">{{ l.state === 'ok' ? `synced ${l.lastSync}` : l.lastSync }}</StatusBadge>
            </div>
            <div class="il-kind">{{ l.kind }} → {{ l.target }}</div>
            <div class="il-meta">
              <span class="il-behavior">{{ l.behavior }}</span>
              <span>· {{ l.have }}/{{ l.items }} in library</span>
            </div>
            <div class="il-bar">
              <div class="il-bar-fill" :style="{ width: `${Math.round((l.have / l.items) * 100)}%` }" />
            </div>
          </div>
          <div class="il-actions">
            <AppTooltip label="Sync now">
              <button type="button" class="il-btn"><Icon name="refresh" :size="14" /></button>
            </AppTooltip>
            <AppTooltip label="View as list on the server">
              <button type="button" class="il-btn"><Icon name="arrow-right" :size="14" /></button>
            </AppTooltip>
          </div>
        </div>
      </div>
    </SettingsSection>
  </div>
</template>

<style scoped>
.il-add {
  display: inline-flex; align-items: center; gap: 7px;
  height: 32px; padding: 0 14px;
  border-radius: var(--r-sm);
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 40%, transparent);
  color: var(--gold-bright);
  font-size: 12.5px; font-weight: 600;
  cursor: pointer;
  transition: background 0.12s;
}
.il-add:hover { background: color-mix(in srgb, var(--gold) 18%, transparent); }

.il-grid { display: flex; flex-direction: column; gap: 8px; }
.il-card {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.il-icon {
  width: 36px; height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  color: var(--gold);
  flex-shrink: 0;
}
.il-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.il-name {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 14px; font-weight: 500; color: var(--fg-0);
}
.il-kind { font-size: 12px; color: var(--fg-2); }
.il-meta {
  font-size: 11.5px; color: var(--fg-3);
  display: flex; flex-wrap: wrap; gap: 6px;
}
.il-behavior {
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--gold);
}
.il-bar {
  height: 4px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.08);
  overflow: hidden;
  margin-top: 4px;
  max-width: 420px;
}
.il-bar-fill {
  height: 100%;
  border-radius: 999px;
  background: var(--gold);
}

.il-actions { display: flex; gap: 4px; flex-shrink: 0; }
.il-btn {
  width: 30px; height: 30px;
  display: flex; align-items: center; justify-content: center;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.il-btn:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
</style>
