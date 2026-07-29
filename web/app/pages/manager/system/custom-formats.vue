<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { MOCK_FORMATS } from '~/utils/managerMock'

const trashCount = MOCK_FORMATS.filter(f => f.source === 'TRaSH').length
</script>

<template>
  <div>
    <SettingsContextHero
      title="Custom formats"
      icon="hash"
      eyebrow="Manager · System — mock data"
      description="Regex-backed release scoring — the TRaSH Guides model. Formats match parsed releases (group, codec, HDR, edition…) and their per-profile scores steer the decision engine."
    />

    <SettingsSection
      title="Formats"
      icon="hash"
      :description="`${MOCK_FORMATS.length} formats · ${trashCount} imported from TRaSH Guides. The schema is import-compatible with Radarr/Sonarr custom format JSON — bring your existing *arr setup over as-is.`"
    >
      <template #actions>
        <div class="cf-actions">
          <button type="button" class="cf-import"><Icon name="cloud-download" :size="14" /> Import from *arr / TRaSH</button>
          <button type="button" class="cf-add"><Icon name="plus" :size="14" /> New format</button>
        </div>
      </template>

      <div class="cf-table">
        <div class="cf-head">
          <span>Format</span>
          <span>Matches on</span>
          <span>Conditions</span>
          <span>Scores</span>
          <span>Source</span>
        </div>
        <div v-for="f in MOCK_FORMATS" :key="f.id" class="cf-row">
          <span class="cf-name">{{ f.name }}</span>
          <span class="cf-tags">
            <span v-for="t in f.tags" :key="t" class="cf-tag">{{ t }}</span>
          </span>
          <span class="cf-specs">{{ f.specs }} spec{{ f.specs === 1 ? '' : 's' }}</span>
          <span class="cf-scores">{{ f.scores }}</span>
          <span class="cf-source" :class="{ trash: f.source === 'TRaSH' }">{{ f.source }}</span>
        </div>
      </div>
    </SettingsSection>
  </div>
</template>

<style scoped>
.cf-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.cf-import {
  display: inline-flex; align-items: center; gap: 7px;
  height: 32px; padding: 0 14px;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-1);
  font-size: 12.5px; font-weight: 600;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.cf-import:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.cf-add {
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
.cf-add:hover { background: color-mix(in srgb, var(--gold) 18%, transparent); }

.cf-table { display: flex; flex-direction: column; gap: 6px; }
.cf-head,
.cf-row {
  display: grid;
  grid-template-columns: minmax(160px, 1.6fr) minmax(140px, 1.3fr) 90px minmax(150px, 1.3fr) 70px;
  gap: 14px;
  align-items: center;
}
.cf-head {
  padding: 0 14px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.cf-row {
  padding: 11px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.cf-name { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.cf-tags { display: flex; gap: 4px; flex-wrap: wrap; }
.cf-tag {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  padding: 2px 7px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--border);
  color: var(--fg-2);
  white-space: nowrap;
}
.cf-specs { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-2); }
.cf-scores { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-1); }
.cf-source {
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.cf-source.trash { color: var(--gold); }

@media (max-width: 960px) {
  .cf-head { display: none; }
  .cf-row { grid-template-columns: minmax(0, 1fr) auto; row-gap: 6px; }
  .cf-specs, .cf-tags { display: none; }
  .cf-scores { grid-column: 1; }
}
</style>
